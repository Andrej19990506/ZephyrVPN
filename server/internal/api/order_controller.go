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
	"gorm.io/gorm"
	"zephyrvpn/server/internal/models"
	"zephyrvpn/server/internal/services"
	"zephyrvpn/server/internal/utils"
)

type OrderController struct {
	redisUtil            *utils.RedisClient
	slotService          *services.SlotService
	stockService         *services.StockService
	stationAssignService *services.StationAssignmentService
}

func NewOrderController(redisUtil *utils.RedisClient, stockService *services.StockService, db interface{}, openHour, openMin, closeHour, closeMin int) *OrderController {
	// Преобразуем db в *gorm.DB если возможно
	var gormDB *gorm.DB
	if db != nil {
		if gdb, ok := db.(*gorm.DB); ok {
			gormDB = gdb
		}
	}
	slotService := services.NewSlotService(redisUtil, gormDB, openHour, openMin, closeHour, closeMin)
	stationAssignService := services.NewStationAssignmentService(gormDB, redisUtil)
	return &OrderController{
		redisUtil:            redisUtil,
		slotService:           slotService,
		stockService:          stockService,
		stationAssignService: stationAssignService,
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
	BranchID          string             `json:"branch_id,omitempty"` // ID филиала для проверки остатков
	Items             []models.PizzaItem `json:"items" binding:"required"`
	IsSet             bool               `json:"is_set"`
	SetName           string             `json:"set_name,omitempty"`
	DeliveryFee       int                `json:"delivery_fee,omitempty"` // Цена доставки в рублях
	DiscountAmount    int                `json:"discount_amount,omitempty"` // Сумма скидки в рублях
	DiscountPercent   int                `json:"discount_percent,omitempty"` // Процент скидки
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

	// Проверка остатков перед созданием заказа
	if oc.stockService != nil && req.BranchID != "" {
		if err := oc.checkInventoryAvailability(req.Items, req.BranchID); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":   "Недостаточно ингредиентов для выполнения заказа",
				"details": err.Error(),
			})
			return
		}
	}

	// Вычисляем общую стоимость товаров (без доставки и скидок)
	itemsPrice := 0
	items := make([]models.PizzaItem, len(req.Items))
	for i, item := range req.Items {
		pizza, _ := models.GetPizza(item.PizzaName)
		// Цена пиццы без допов
		pizzaPrice := pizza.Price
		
		// Цена допов за единицу
		extrasPrice := 0
		if len(item.Extras) > 0 {
			log.Printf("   🔍 Обработка допов для '%s': %v", item.PizzaName, item.Extras)
			allExtras := models.GetAllExtras()
			log.Printf("   📋 Доступные допы в меню (%d шт): %v", len(allExtras), func() []string {
				names := make([]string, 0, len(allExtras))
				for name := range allExtras {
					names = append(names, name)
				}
				return names
			}())
		}
		for _, extraName := range item.Extras {
			extra, exists := models.GetExtra(extraName)
			if exists {
				extrasPrice += extra.Price
				log.Printf("   ✅ Доп '%s' найден, цена: %d руб", extraName, extra.Price)
			} else {
				log.Printf("   ❌ Доп '%s' НЕ найден в меню!", extraName)
			}
		}
		if extrasPrice > 0 {
			log.Printf("   💰 Итого допы: %d руб", extrasPrice)
		}
		
		// Общая цена за единицу (пицца + допы)
		pricePerUnit := pizzaPrice + extrasPrice
		
		// Общая цена за все количество
		itemPrice := pricePerUnit * item.Quantity
		itemsPrice += itemPrice
		
		// Копируем item (включая поля SetName и IsSetItem)
		items[i] = item
		// Устанавливаем цены
		items[i].Price = pricePerUnit       // Общая цена за единицу
		items[i].PizzaPrice = pizzaPrice    // Цена пиццы без допов
		items[i].ExtrasPrice = extrasPrice  // Цена допов
		
		// Берем дозировки ингредиентов из модели пиццы
		if pizza, exists := models.GetPizza(item.PizzaName); exists {
			if pizza.IngredientAmounts != nil {
				items[i].IngredientAmounts = pizza.IngredientAmounts
			} else {
				// Fallback: если дозировки нет в модели, используем стандартные
				items[i].IngredientAmounts = generateIngredientAmounts(item.Ingredients)
			}
		} else {
			// Fallback: если пицца не найдена, используем стандартные дозировки
			items[i].IngredientAmounts = generateIngredientAmounts(item.Ingredients)
		}
	}
	
	// Рассчитываем цену доставки (только если не самовывоз)
	// TODO: В будущем будет расчет на основе суммы заказа и геолокации клиента
	// Пока что доставка бесплатная для теста
	deliveryFee := 0
	if !req.IsPickup && req.DeliveryFee > 0 {
		deliveryFee = req.DeliveryFee
	}
	// Если delivery_fee не передан, доставка бесплатная (0)
	
	// Рассчитываем скидку
	discountAmount := req.DiscountAmount
	if req.DiscountPercent > 0 && discountAmount == 0 {
		// Если передан процент скидки, рассчитываем сумму скидки от суммы товаров
		discountAmount = (itemsPrice * req.DiscountPercent) / 100
	}
	
	// Итоговая цена: товары + доставка - скидка
	totalPrice := itemsPrice + deliveryFee
	finalPrice := totalPrice - discountAmount

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
	
	// Логируем информацию о товарах и цене для отладки
	log.Printf("🛒 Детали заказа: %d позиций товаров, всего единиц: %d", len(items), itemsCount)
	for i, item := range items {
		log.Printf("   [%d] %s x%d = %d руб (допы: %v)", i+1, item.PizzaName, item.Quantity, 
			item.Price, item.Extras)
	}
	log.Printf("💰 Расчет цены: товары=%d руб, доставка=%d руб, скидка=%d руб, итого=%d руб (финальная=%d руб)", 
		itemsPrice, deliveryFee, discountAmount, totalPrice, finalPrice)
	
	// Передаем итоговую сумму заказа (с доставкой) и количество элементов для расчета времени подготовки
	slotID, slotStartTime, visibleAt, err := oc.slotService.AssignSlot(fullID, finalPrice, itemsCount)
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
		TotalPrice:         itemsPrice, // Цена товаров без доставки
		DiscountAmount:    discountAmount,
		DiscountPercent:    req.DiscountPercent,
		FinalPrice:         finalPrice, // Итоговая цена: товары + доставка - скидка
		CreatedAt:          time.Now(),
		Status:             "pending",
		TargetSlotID:       slotID,        // 🎯 Сохраняем ID слота в заказе
		TargetSlotStartTime: slotStartTime, // 🎯 Сохраняем время начала слота (UTC)
		VisibleAt:          visibleAt,     // 🎯 Сохраняем время показа заказа на планшете (UTC)
	}

	// Сохраняем в Redis и отправляем в ERP в фоне (используем указатель для эффективности)
	go func(o *models.PizzaOrder) {
		oc.saveOrder(o)
		// Распределяем заказ по станциям
		if oc.stationAssignService != nil {
			if err := oc.stationAssignService.AssignOrderToStations(o); err != nil {
				log.Printf("⚠️ CreateOrder: ошибка распределения заказа по станциям: %v", err)
			}
		}
		oc.sendToERP(o)
	}(&order)
	
	log.Printf("🎯 Slot assigned: заказ %s назначен на слот %s (время: %s)", 
		fullID, slotID, slotStartTime.Format("15:04"))
	
	// Логируем итоговую информацию о заказе
	log.Printf("✅ Заказ создан: ID=%s, товары=%d руб, доставка=%d руб, скидка=%d руб, итого=%d руб", 
		order.ID, order.TotalPrice, deliveryFee, order.DiscountAmount, order.FinalPrice)

	c.JSON(http.StatusOK, gin.H{
		"order_id":     order.ID,
		"display_id":   order.DisplayID,
		"total_price":  order.TotalPrice,  // Цена товаров без доставки (в рублях)
		"final_price":  order.FinalPrice,  // Итоговая цена: товары + доставка - скидка (в рублях)
		"delivery_fee": deliveryFee,       // Цена доставки (в рублях, сейчас 0 - бесплатно)
		"items_count":  itemsCount,        // Количество единиц товара
		"items_price":  itemsPrice,        // Цена всех товаров (для отладки)
		"status":       "accepted",
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

// checkInventoryAvailability проверяет доступность ингредиентов для всех позиций заказа
// Best Practice: Строгая валидация - заказ не создается, если ингредиентов недостаточно
func (oc *OrderController) checkInventoryAvailability(items []models.PizzaItem, branchID string) error {
	if oc.stockService == nil {
		// Если StockService не инициализирован, пропускаем проверку (для обратной совместимости)
		log.Printf("⚠️ StockService не инициализирован, проверка остатков пропущена")
		return nil
	}

	if branchID == "" {
		// Если branchID не указан, не можем проверить остатки
		return fmt.Errorf("branch_id не указан, невозможно проверить остатки")
	}

	for _, item := range items {
		// Получаем recipeID для пиццы (best practice: поиск через NomenclatureItem)
		recipeID, err := oc.getRecipeIDByPizzaName(item.PizzaName)
		if err != nil {
			// Best Practice: Если рецепт не найден, это критическая ошибка - пицца не может быть приготовлена
			return fmt.Errorf("пицца '%s': рецепт не найден - %w. Пицца должна иметь связанный Recipe для проверки остатков", item.PizzaName, err)
		}

		// Проверяем доступность ингредиентов для пиццы
		if err := oc.stockService.CheckRecipeAvailability(recipeID, float64(item.Quantity), branchID); err != nil {
			return fmt.Errorf("пицца '%s' (x%d): %w", item.PizzaName, item.Quantity, err)
		}

		// Проверяем доступность допов
		for _, extraName := range item.Extras {
			extra, exists := models.GetExtra(extraName)
			if !exists {
				// Best Practice: Доп должен существовать в меню
				return fmt.Errorf("доп '%s' не найден в меню для пиццы '%s'", extraName, item.PizzaName)
			}

			// Проверяем остатки для допа
			if extra.ID == 0 {
				// Best Practice: Доп должен иметь ID для связи с номенклатурой/рецептом
				return fmt.Errorf("доп '%s' не имеет ID - невозможно проверить остатки. Доп должен быть создан через Technologist Workspace", extraName)
			}

			if err := oc.stockService.CheckExtraAvailability(extra.ID, item.Quantity, branchID); err != nil {
				return fmt.Errorf("доп '%s' для пиццы '%s' (x%d): %w", extraName, item.PizzaName, item.Quantity, err)
			}
		}
	}

	return nil
}

// getRecipeIDByPizzaName находит Recipe ID по названию пиццы
// Best Practice: Поиск через NomenclatureItem (IsSaleable=true) -> Recipe (MenuItemID)
// Это гарантирует связь между меню и рецептом через единую номенклатуру
func (oc *OrderController) getRecipeIDByPizzaName(pizzaName string) (string, error) {
	if oc.stockService == nil {
		return "", fmt.Errorf("stock service не инициализирован")
	}

	db := oc.stockService.GetDB()

	// Шаг 1: Ищем NomenclatureItem по имени (IsSaleable=true - товар для продажи)
	var nomenclature models.NomenclatureItem
	if err := db.Where("name = ? AND is_saleable = true AND is_ready_for_sale = true AND is_active = true AND deleted_at IS NULL", pizzaName).
		First(&nomenclature).Error; err != nil {
		// Если не найдено через NomenclatureItem, пробуем прямой поиск Recipe по имени (fallback для старых данных)
		log.Printf("⚠️ NomenclatureItem не найден для '%s', пробуем прямой поиск Recipe", pizzaName)
		var recipe models.Recipe
		if err := db.Where("name = ? AND is_active = true AND deleted_at IS NULL", pizzaName).
			First(&recipe).Error; err != nil {
			return "", fmt.Errorf("рецепт не найден для пиццы '%s': не найден ни NomenclatureItem (IsSaleable=true), ни Recipe с таким именем", pizzaName)
		}
		return recipe.ID, nil
	}

	// Шаг 2: Ищем Recipe, связанный с этим NomenclatureItem через MenuItemID
	var recipe models.Recipe
	if err := db.Where("menu_item_id = ? AND is_active = true AND deleted_at IS NULL", nomenclature.ID).
		First(&recipe).Error; err != nil {
		// Если Recipe не найден через MenuItemID, пробуем прямой поиск по имени (fallback)
		log.Printf("⚠️ Recipe не найден через MenuItemID для '%s', пробуем прямой поиск", pizzaName)
		if err := db.Where("name = ? AND is_active = true AND deleted_at IS NULL", pizzaName).
			First(&recipe).Error; err != nil {
			return "", fmt.Errorf("рецепт не найден для пиццы '%s': NomenclatureItem найден (ID: %s), но связанный Recipe не найден", pizzaName, nomenclature.ID)
		}
	}

	return recipe.ID, nil
}
