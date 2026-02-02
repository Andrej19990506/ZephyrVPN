package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"zephyrvpn/server/internal/models"
	"zephyrvpn/server/internal/services"
	"zephyrvpn/server/internal/utils"
)

type OrderController struct {
	redisUtil  *utils.RedisClient
	slotService *services.SlotService
}

func NewOrderController(redisUtil *utils.RedisClient, openHour, closeHour, closeMin int) *OrderController {
	slotService := services.NewSlotService(redisUtil, openHour, closeHour, closeMin)
	return &OrderController{
		redisUtil:   redisUtil,
		slotService: slotService,
	}
}

type CreateOrderRequest struct {
	CustomerID        int                `json:"customer_id,omitempty"`
	CustomerFirstName string             `json:"customer_first_name,omitempty"`
	CustomerLastName  string             `json:"customer_last_name,omitempty"`
	CustomerPhone     string             `json:"customer_phone,omitempty"`
	DeliveryAddress   string             `json:"delivery_address,omitempty"`
	IsPickup          bool               `json:"is_pickup"`
	PickupLocationID  string             `json:"pickup_location_id,omitempty"`
	Items             []models.PizzaItem `json:"items" binding:"required"`
	IsSet             bool               `json:"is_set"`
	SetName           string             `json:"set_name,omitempty"`
}

func (oc *OrderController) CreateOrder(c *gin.Context) {
	var req CreateOrderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid data", "details": err.Error()})
		return
	}

	// Валидация пицц
	for _, item := range req.Items {
		if _, exists := models.GetPizza(item.PizzaName); !exists {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": fmt.Sprintf("Пицца '%s' не найдена в меню", item.PizzaName),
			})
			return
		}
	}

	// Вычисляем общую стоимость и добавляем дозировки ингредиентов
	totalPrice := 0
	items := make([]models.PizzaItem, len(req.Items))
	for i, item := range req.Items {
		pizza, _ := models.GetPizza(item.PizzaName)
		itemPrice := pizza.Price * item.Quantity
		
		// Добавляем стоимость допов
		for _, extraName := range item.Extras {
			if extra, exists := models.GetExtra(extraName); exists {
				itemPrice += extra.Price * item.Quantity
			}
		}
		
		totalPrice += itemPrice
		
		// Копируем item (включая поля SetName и IsSetItem)
		items[i] = item
		
		// Берем дозировки ингредиентов из модели пиццы
		if pizza, exists := models.GetPizza(item.PizzaName); exists && pizza.IngredientAmounts != nil {
			items[i].IngredientAmounts = pizza.IngredientAmounts
		} else {
			// Fallback: если дозировки нет в модели, используем стандартные
			items[i].IngredientAmounts = generateIngredientAmounts(item.Ingredients)
		}
	}

	// Генерируем полный ID
	fullID := uuid.New().String()
	// Извлекаем только цифры из UUID и берем последние 4
	re := regexp.MustCompile(`\d+`)
	digits := re.FindAllString(fullID, -1)
	digitsOnly := strings.Join(digits, "")
	if len(digitsOnly) < 4 {
		digitsOnly = "0000" // Fallback если цифр мало
	}
	displayID := digitsOnly[len(digitsOnly)-4:] // Последние 4 цифры

	// 🎯 Capacity-Based Slot Scheduling: назначаем слот ПЕРЕД созданием заказа
	// Считаем общее количество элементов (пицц) в заказе
	itemsCount := 0
	for _, item := range items {
		itemsCount += item.Quantity
	}
	
	// Передаем сумму заказа и количество элементов для расчета времени подготовки
	slotID, slotStartTime, visibleAt, err := oc.slotService.AssignSlot(fullID, totalPrice, itemsCount)
	if err != nil {
		// Если не удалось назначить слот, возвращаем ошибку
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "Не удалось назначить временной слот для заказа",
			"details": err.Error(),
		})
		return
	}

	// Создаем заказ с назначенным слотом
	order := models.PizzaOrder{
		ID:                 fullID,
		DisplayID:          displayID, // Последние 4 цифры
		CustomerID:         req.CustomerID,
		CustomerFirstName: req.CustomerFirstName,
		CustomerLastName:  req.CustomerLastName,
		CustomerPhone:      req.CustomerPhone,
		DeliveryAddress:    req.DeliveryAddress,
		IsPickup:           req.IsPickup,
		PickupLocationID:   req.PickupLocationID,
		Items:              items,
		IsSet:              req.IsSet,
		SetName:            req.SetName,
		TotalPrice:         totalPrice,
		CreatedAt:          time.Now(),
		Status:             "pending",
		TargetSlotID:       slotID,        // 🎯 Сохраняем ID слота в заказе
		TargetSlotStartTime: slotStartTime, // 🎯 Сохраняем время начала слота (UTC)
		VisibleAt:          visibleAt,     // 🎯 Сохраняем время показа заказа на планшете (UTC)
	}

	// Сохраняем в Redis и отправляем в ERP в фоне (используем указатель для эффективности)
	go func(o *models.PizzaOrder) {
		oc.saveOrder(o)
		oc.sendToERP(o)
	}(&order)
	
	log.Printf("🎯 Slot assigned: заказ %s назначен на слот %s (время: %s)", 
		fullID, slotID, slotStartTime.Format("15:04"))

	c.JSON(http.StatusOK, gin.H{
		"order_id":    order.ID,
		"total_price": order.TotalPrice,
		"status":      "accepted",
	})
}

func (oc *OrderController) saveOrder(order *models.PizzaOrder) {
	if oc.redisUtil == nil {
		return
	}

	// Создаем Pipeline — это пачка команд, которые отправятся ОДНИМ выстрелом
	pipe := oc.redisUtil.Pipeline()

	orderJSON, _ := json.Marshal(order)
	orderKey := fmt.Sprintf("order:%s", order.ID)
	todayKey := "orders:today:" + time.Now().Format("2006-01-02")
	
	ctx := oc.redisUtil.Context()
	
	// Накидываем команды в пачку (они еще не ушли в сеть!)
	pipe.Set(ctx, orderKey, string(orderJSON), 24*time.Hour)
	pipe.Set(ctx, fmt.Sprintf("orders:list:%s", order.ID), order.ID, 24*time.Hour)
	pipe.Incr(ctx, "orders:total")
	pipe.Incr(ctx, todayKey)
	pipe.LPush(ctx, "kitchen:orders:queue", order.ID)
	
	// Отправляем ВСЁ ОДНИМ выстрелом (экономия сетевых вызовов!)
	_, err := pipe.Exec(ctx)
	if err != nil {
		log.Printf("⚠️ Pipeline error при сохранении заказа %s: %v", order.ID, err)
	}
}

func (oc *OrderController) sendToERP(order *models.PizzaOrder) {
	if oc.redisUtil == nil {
		return
	}

	// Сохраняем заказ для ERP (используем указатель)
	orderJSON, _ := json.Marshal(order)
	
	// Сохраняем заказ с ключом для быстрого доступа
	oc.redisUtil.Set(fmt.Sprintf("erp:order:%s", order.ID), string(orderJSON), 7*24*time.Hour)
	
	// НЕ добавляем заказ в активные сразу - он появится только когда наступит VisibleAt
	// Сохраняем заказ в отдельный список ожидающих заказов
	if !order.VisibleAt.IsZero() {
		// Сохраняем время начала слота и время показа для проверки
		oc.redisUtil.Set(fmt.Sprintf("order:slot:start:%s", order.ID), order.TargetSlotStartTime.Format(time.RFC3339), 24*time.Hour)
		oc.redisUtil.Set(fmt.Sprintf("order:visible_at:%s", order.ID), order.VisibleAt.Format(time.RFC3339), 24*time.Hour)
		
		// Добавляем в список ожидающих заказов (не в активные!)
		oc.redisUtil.SAdd("erp:orders:pending_slots", order.ID)
		
		log.Printf("📅 Заказ %s назначен на слот %s (время начала: %s UTC, будет показан: %s UTC)", 
			order.ID, order.TargetSlotID, order.TargetSlotStartTime.Format("15:04:05"), order.VisibleAt.Format("15:04:05"))
	} else {
		// Если нет VisibleAt, добавляем сразу в активные (старая логика для обратной совместимости)
		oc.redisUtil.SAdd("erp:orders:active", order.ID)
		oc.redisUtil.Increment("erp:orders:pending")
	}
	
	// Отправляем обновление в ERP через WebSocket
	BroadcastERPUpdate("new_order", map[string]interface{}{
		"order_id": order.ID,
		"display_id": order.DisplayID,
		"message": "Новый заказ создан",
	})
	
	// НЕ добавляем в очередь воркеров - обработка только вручную через ERP
}

func GetAvailablePizzas() map[string]models.Pizza {
	return models.GetAllPizzas() // Потокобезопасная копия
}

func GetAvailableExtras() map[string]models.Extra {
	return models.GetAllExtras() // Потокобезопасная копия
}

func GetAvailableSets() map[string]models.PizzaSet {
	return models.GetAllSets() // Потокобезопасная копия
}

// generateIngredientAmounts генерирует дозировки ингредиентов в граммах
// Сыр моцарелла всегда 150г, остальные ингредиенты имеют стандартные дозировки
func generateIngredientAmounts(ingredients []string) map[string]int {
	amounts := make(map[string]int)
	
	// Стандартные дозировки ингредиентов (в граммах)
	standardAmounts := map[string]int{
		"сыр моцарелла":     150, // Всегда 150г
		"бекон":             80,
		"яйцо":              100, // 1 яйцо ~50г, но на пиццу обычно 2
		"помидоры":          120,
		"лук":               60,
		"соус":              80,
		"колбаса":           100,
		"огурцы маринованные": 80,
		"оливки":            50,
		"пепперони":         100,
		"острый перец":      30,
		"базилик":           10,
		"грибы":             100,
		"ветчина":           80,
		"колбаса охотничья": 100,
		"курица":            120,
	}
	
	for _, ing := range ingredients {
		// Приводим к нижнему регистру для сравнения
		ingLower := strings.ToLower(ing)
		if amount, exists := standardAmounts[ingLower]; exists {
			amounts[ing] = amount
		} else {
			// Если ингредиент не найден, используем стандартную дозировку 80г
			amounts[ing] = 80
		}
	}
	
	return amounts
}
