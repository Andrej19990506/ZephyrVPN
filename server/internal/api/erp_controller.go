package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/segmentio/kafka-go"
	"gorm.io/gorm"
	"google.golang.org/protobuf/proto"
	"zephyrvpn/server/internal/models"
	"zephyrvpn/server/internal/pb"
	"zephyrvpn/server/internal/services"
	"zephyrvpn/server/internal/utils"
)

type ERPController struct {
	redisUtil          *utils.RedisClient
	kafkaBrokers       string
	slotService        *services.SlotService
	revenueService     *services.RevenueService
	dailyPlanService   *services.DailyPlanService
	kitchenLoadService *services.KitchenLoadService
	stationAssignService *services.StationAssignmentService
}

func NewERPController(redisUtil *utils.RedisClient, kafkaBrokers string, db interface{}, openHour, openMin, closeHour, closeMin int) *ERPController {
	// Преобразуем db в *gorm.DB если возможно
	var gormDB *gorm.DB
	if db != nil {
		if gdb, ok := db.(*gorm.DB); ok {
			gormDB = gdb
		}
	}
	slotService := services.NewSlotService(redisUtil, gormDB, openHour, openMin, closeHour, closeMin)
	revenueService := services.NewRevenueService(redisUtil, gormDB)
	dailyPlanService := services.NewDailyPlanService(redisUtil)
	kitchenLoadService := services.NewKitchenLoadService(slotService)
	stationAssignService := services.NewStationAssignmentService(gormDB, redisUtil)
	return &ERPController{
		redisUtil:           redisUtil,
		kafkaBrokers:        kafkaBrokers,
		slotService:         slotService,
		revenueService:      revenueService,
		dailyPlanService:    dailyPlanService,
		kitchenLoadService:  kitchenLoadService,
		stationAssignService: stationAssignService,
	}
}

// GetOrders получает все АКТИВНЫЕ заказы для ERP системы (те, что висят на планшете)
// Поддерживает фильтрацию по роли: ?role=kitchen|courier|admin
func (ec *ERPController) GetOrders(c *gin.Context) {
	orders := make([]models.PizzaOrder, 0)
	
	if ec.redisUtil == nil {
		// Если Redis недоступен, возвращаем пустой список
		c.JSON(http.StatusOK, gin.H{
			"system": "ЕРПИ ТЕСТ",
			"orders": orders,
			"count":  0,
			"message": "Redis not available, returning empty list",
		})
		return
	}

	// Получаем роль из query параметра (kitchen, courier, admin)
	role := c.Query("role")
	if role == "" {
		role = "kitchen" // По умолчанию для кухни
	}

	// Получаем station_id из query параметра (для фильтрации заказов по станции)
	stationID := c.Query("station_id")

	// Проверяем ожидающие заказы и добавляем их в активные, если наступило время показа
	ec.checkAndActivatePendingOrders()

	// Получаем список ID АКТИВНЫХ заказов (из множества)
	orderIDs, err := ec.redisUtil.SMembers("erp:orders:active")
	if err != nil {
		log.Printf("❌ GetOrders: ошибка получения активных заказов из Redis: %v", err)
		// Если ошибка, возвращаем пустой список
		c.JSON(http.StatusOK, gin.H{
			"system": "ЕРПИ ТЕСТ",
			"orders": orders,
			"count":  0,
			"message": "No active orders found",
		})
		return
	}

	// Логируем только если есть заказы или при проблемах
	if len(orderIDs) > 0 {
		log.Printf("📊 GetOrders: получено из Redis erp:orders:active = %d заказов", len(orderIDs))
	}

	// Получаем детали каждого заказа и фильтруем по роли и VisibleAt
	now := time.Now().UTC()
	notFoundCount := 0
	visibleAtNotReachedCount := 0
	
	for _, orderID := range orderIDs {
		order, err := ec.getOrderFromRedis(orderID)
		if err != nil {
			notFoundCount++
			continue // Пропускаем если заказ не найден
		}
		
		// Фильтруем заказы по VisibleAt (время показа на планшете)
		// Показываем только заказы, у которых VisibleAt уже наступило
		if !order.VisibleAt.IsZero() {
			// Если время показа еще не наступило, пропускаем заказ
			if now.Before(order.VisibleAt) {
				visibleAtNotReachedCount++
				continue
			}
		}
		
		// Если заказ в active, но имеет статус "pending", обновляем на "accepted"
		if order.Status == "pending" {
			order.Status = "accepted"
			// Сохраняем обновленный заказ обратно в Redis
			orderJSON, _ := json.Marshal(order)
			orderKey := fmt.Sprintf("erp:order:%s", orderID)
			ec.redisUtil.SetBytes(orderKey, orderJSON, 24*time.Hour)
		}
		
		// Фильтруем заказы по станции (если указан station_id)
		if stationID != "" && ec.stationAssignService != nil {
			stationOrder, canWork, err := ec.stationAssignService.GetOrderForStation(order, stationID)
			if err != nil {
				log.Printf("⚠️ GetOrders: ошибка получения заказа для станции %s: %v", stationID, err)
				continue
			}
			if stationOrder == nil {
				// Заказ не виден для этой станции
				continue
			}
			// Используем отфильтрованный заказ и устанавливаем флаг canWork
			order = stationOrder
			order.CanWork = canWork
		}
		
		// Фильтруем данные в зависимости от роли
		filteredOrder := ec.filterOrderByRole(*order, role)
		orders = append(orders, filteredOrder)
	}
	
	// Логируем статистику фильтрации только если есть проблемы
	if notFoundCount > 0 || visibleAtNotReachedCount > 0 || len(orders) != len(orderIDs) {
		log.Printf("📊 GetOrders фильтрация (role=%s): всего ID в Redis=%d, показано=%d, не найдено в Redis=%d, VisibleAt не наступило=%d", 
			role, len(orderIDs), len(orders), notFoundCount, visibleAtNotReachedCount)
	}

	c.JSON(http.StatusOK, gin.H{
		"system": "ЕРПИ ТЕСТ",
		"orders": orders,
		"count":  len(orders),
		"role":   role,
	})
}

// filterOrderByRole фильтрует данные заказа в зависимости от роли
func (ec *ERPController) filterOrderByRole(order models.PizzaOrder, role string) models.PizzaOrder {
	filtered := order
	
	switch role {
	case "kitchen": // Повара - только информация для готовки
		// Оставляем только: items с ингредиентами и exclude_ingredients
		// Убираем: delivery_address, customer_phone, payment_method, discount, final_price
		filtered.DeliveryAddress = ""
		filtered.CustomerPhone = ""
		filtered.CallBeforeMinutes = 0
		filtered.PaymentMethod = ""
		filtered.IsPickup = false
		filtered.DiscountAmount = 0
		filtered.DiscountPercent = 0
		filtered.FinalPrice = 0
		filtered.Notes = ""
		
	case "courier": // Курьеры - информация для доставки
		// Оставляем: delivery_address, customer_phone, call_before_minutes, payment_method, is_pickup
		// Убираем: exclude_ingredients (детали готовки), discount, final_price
		for i := range filtered.Items {
			filtered.Items[i].ExcludeIngredients = nil
		}
		filtered.DiscountAmount = 0
		filtered.DiscountPercent = 0
		filtered.FinalPrice = 0
		
	case "admin": // Админы - полная информация
		// Оставляем всё как есть - полная информация
		// Ничего не убираем
		
	default:
		// По умолчанию как для кухни
		filtered.DeliveryAddress = ""
		filtered.CustomerPhone = ""
		filtered.CallBeforeMinutes = 0
		filtered.PaymentMethod = ""
		filtered.IsPickup = false
		filtered.DiscountAmount = 0
		filtered.DiscountPercent = 0
		filtered.FinalPrice = 0
		filtered.Notes = ""
	}
	
	return filtered
}

// GetOrder получает конкретный заказ по ID
func (ec *ERPController) GetOrder(c *gin.Context) {
	orderID := c.Param("id")
	log.Printf("🔍 GetOrder вызван: orderID=%s, URL=%s", orderID, c.Request.URL.Path)
	
	// Проверяем, не является ли это запросом к /orders/pending
	if orderID == "pending" {
		log.Printf("⚠️ GetOrder: обнаружен запрос к /orders/pending, перенаправляем в GetPendingOrders")
		ec.GetPendingOrders(c)
		return
	}
	
	if ec.redisUtil == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Redis not available"})
		return
	}

	order, err := ec.getOrderFromRedis(orderID)
	if err != nil {
		log.Printf("❌ GetOrder: заказ %s не найден в Redis", orderID)
		c.JSON(http.StatusNotFound, gin.H{"error": "Order not found"})
		return
	}

	c.JSON(http.StatusOK, order)
}

// GetStats получает статистику для ERP (расширенная версия с выручкой)
func (ec *ERPController) GetStats(c *gin.Context) {
	var total, today, pending string = "0", "0", "0"
	processed := 0
	
	if ec.redisUtil != nil {
		totalVal, _ := ec.redisUtil.Get("orders:total")
		todayVal, _ := ec.redisUtil.Get("orders:today:" + time.Now().Format("2006-01-02"))
		pendingVal, _ := ec.redisUtil.Get("erp:orders:pending")
		
		if totalVal != "" {
			total = totalVal
		}
		if todayVal != "" {
			today = todayVal
		}
		if pendingVal != "" {
			pending = pendingVal
		}
		
		// Считаем РЕАЛЬНОЕ количество обработанных заказов через Set (быстро! O(1))
		processedCount, _ := ec.redisUtil.SCard("erp:processed:set")
		processed = int(processedCount)
		
		// Обновляем счетчик для совместимости (опционально)
		ec.redisUtil.Set("erp:orders:processed", fmt.Sprintf("%d", processed), 0)
	}

	// Получаем выручку за сегодня
	var revenue *services.RevenueStats
	if ec.revenueService != nil {
		revenue, _ = ec.revenueService.GetRevenueForToday()
	}

	// Получаем план на сегодня
	var dailyPlan float64 = 500000.0
	if ec.dailyPlanService != nil {
		dailyPlan, _ = ec.dailyPlanService.GetDailyPlanForToday()
	}

	response := gin.H{
		"total_orders":     total,
		"today_orders":     today,
		"pending_orders":   pending,
		"processed_orders": fmt.Sprintf("%d", processed),
		"system":           "ЕРПИ ТЕСТ",
		"timestamp":        time.Now().Format(time.RFC3339),
	}

	// Добавляем выручку если есть
	if revenue != nil {
		response["revenue"] = gin.H{
			"total":            revenue.Total,
			"cash":             revenue.Cash,
			"cashless":         revenue.Cashless,
			"online":           revenue.Online,
			"discounts":         revenue.Discounts,
			"completed_orders": revenue.CompletedOrders,
			"change":           revenue.Change,
		}
	}

	// Добавляем план на день
	response["daily_plan"] = dailyPlan

	c.JSON(http.StatusOK, response)
}

// GetOrdersBatch получает следующую партию АКТИВНЫХ заказов (по 50 штук)
// Поддерживает фильтрацию по роли: ?role=kitchen|courier|admin
func (ec *ERPController) GetOrdersBatch(c *gin.Context) {
	if ec.redisUtil == nil {
		c.JSON(http.StatusOK, gin.H{
			"orders": []models.PizzaOrder{},
			"count":  0,
			"processed": 0,
			"has_more": false,
		})
		return
	}

	// Получаем роль из query параметра
	role := c.Query("role")
	if role == "" {
		role = "kitchen" // По умолчанию для кухни
	}

	// Проверяем ожидающие заказы и добавляем их в активные, если наступило время показа
	ec.checkAndActivatePendingOrders()

	// Получаем все АКТИВНЫЕ заказы (те, что висят на планшете)
	activeOrderIDs, err := ec.redisUtil.SMembers("erp:orders:active")
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"orders": []models.PizzaOrder{},
			"count":  0,
			"processed": 0,
			"has_more": false,
		})
		return
	}
	
	if len(activeOrderIDs) > 0 {
		log.Printf("📊 GetOrdersBatch: получено из Redis erp:orders:active = %d заказов", len(activeOrderIDs))
	}
	
	totalActive := len(activeOrderIDs)
	batchSize := 50
	
	// Получаем только первые batchSize активных заказов
	now := time.Now().UTC()
	
	orders := make([]models.PizzaOrder, 0)
	notFoundCount := 0
	visibleAtNotReachedCount := 0
	
	for i, orderID := range activeOrderIDs {
		if i >= batchSize {
			break
		}
		
		order, err := ec.getOrderFromRedis(orderID)
		if err != nil {
			notFoundCount++
			continue
		}
		
		// Фильтруем заказы по VisibleAt (время показа на планшете)
		// Показываем только заказы, у которых VisibleAt уже наступило
		if !order.VisibleAt.IsZero() {
			// Если время показа еще не наступило, пропускаем заказ
			if now.Before(order.VisibleAt) {
				visibleAtNotReachedCount++
				continue
			}
		}
		
		// Если заказ в active, но имеет статус "pending", обновляем на "accepted"
		if order.Status == "pending" {
			order.Status = "accepted"
			// Сохраняем обновленный заказ обратно в Redis
			orderJSON, _ := json.Marshal(order)
			orderKey := fmt.Sprintf("erp:order:%s", orderID)
			ec.redisUtil.SetBytes(orderKey, orderJSON, 24*time.Hour)
		}
		
		// Фильтруем данные по роли
		filteredOrder := ec.filterOrderByRole(*order, role)
		orders = append(orders, filteredOrder)
	}
	
	// Логируем статистику фильтрации только если есть проблемы
	if notFoundCount > 0 || visibleAtNotReachedCount > 0 || len(orders) != len(activeOrderIDs) {
		log.Printf("📊 GetOrdersBatch фильтрация (role=%s): всего ID=%d, показано=%d, не найдено=%d, VisibleAt не наступило=%d", 
			role, len(activeOrderIDs), len(orders), notFoundCount, visibleAtNotReachedCount)
	}
	
	// Проверяем есть ли еще активные заказы
	hasMore := totalActive > batchSize

	c.JSON(http.StatusOK, gin.H{
		"orders":    orders,
		"count":     len(orders),
		"total":     totalActive,
		"has_more":  hasMore,
		"role":      role,
	})
}

// GetPendingOrders получает все ОТЛОЖЕННЫЕ (будущие) заказы для админки
// Это заказы, у которых VisibleAt еще не наступило (находятся в erp:orders:pending_slots)
func (ec *ERPController) GetPendingOrders(c *gin.Context) {
	log.Printf("📋 GetPendingOrders вызван: URL=%s, Method=%s", c.Request.URL.Path, c.Request.Method)
	
	orders := make([]models.PizzaOrder, 0)
	
	if ec.redisUtil == nil {
		log.Printf("⚠️ GetPendingOrders: Redis недоступен")
		c.JSON(http.StatusOK, gin.H{
			"system": "ЕРПИ ТЕСТ",
			"orders": orders,
			"count":  0,
			"message": "Redis not available, returning empty list",
		})
		return
	}

	// Получаем роль из query параметра (kitchen, courier, admin)
	role := c.Query("role")
	if role == "" {
		role = "admin" // По умолчанию для админа
	}
	log.Printf("📋 GetPendingOrders: role=%s", role)

	// Получаем список ID ОТЛОЖЕННЫХ заказов (из множества pending_slots)
	pendingOrderIDs, err := ec.redisUtil.SMembers("erp:orders:pending_slots")
	if err != nil {
		log.Printf("❌ GetPendingOrders: ошибка получения отложенных заказов из Redis: %v", err)
		c.JSON(http.StatusOK, gin.H{
			"system": "ЕРПИ ТЕСТ",
			"orders": orders,
			"count":  0,
			"message": "No pending orders found",
		})
		return
	}

	log.Printf("📊 GetPendingOrders: получено из Redis erp:orders:pending_slots = %d заказов", len(pendingOrderIDs))

	// Получаем детали каждого заказа и фильтруем по роли
	notFoundCount := 0
	deadOrderIDs := make([]string, 0) // Список "мертвых" заказов для очистки
	
	for _, orderID := range pendingOrderIDs {
		order, err := ec.getOrderFromRedis(orderID)
		if err != nil {
			notFoundCount++
			// Заказ не найден в Redis - добавляем в список для удаления из pending_slots
			deadOrderIDs = append(deadOrderIDs, orderID)
			continue // Пропускаем если заказ не найден
		}
		
		// Для отложенных заказов показываем все, даже если VisibleAt еще не наступило
		// (это и есть их особенность - они отложенные)
		
		// Фильтруем данные в зависимости от роли
		filteredOrder := ec.filterOrderByRole(*order, role)
		orders = append(orders, filteredOrder)
	}
	
	// Очищаем "мертвые" заказы из pending_slots (заказы, которых уже нет в Redis)
	if len(deadOrderIDs) > 0 {
		for _, deadID := range deadOrderIDs {
			ec.redisUtil.SRem("erp:orders:pending_slots", deadID)
		}
		log.Printf("🧹 GetPendingOrders: удалено %d несуществующих заказов из pending_slots", len(deadOrderIDs))
	}
	
	// Логируем статистику
	log.Printf("📊 GetPendingOrders фильтрация (role=%s): всего ID=%d, показано=%d, не найдено в Redis=%d", 
		role, len(pendingOrderIDs), len(orders), notFoundCount)

	c.JSON(http.StatusOK, gin.H{
		"system": "ЕРПИ ТЕСТ",
		"orders": orders,
		"count":  len(orders),
		"role":   role,
	})
	log.Printf("✅ GetPendingOrders: возвращено %d заказов", len(orders))
}

// MarkOrderReady отмечает заказ как готовый (повар нажал "Готово")
// Удаляет заказ из активных и переносит в архив
func (ec *ERPController) MarkOrderReady(c *gin.Context) {
	if ec.redisUtil == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Redis not available"})
		return
	}

	orderID := c.Param("id")
	
	// 1. Проверяем существование заказа в Redis
	_, err := ec.getOrderFromRedis(orderID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Order not found"})
		return
	}
	
	// 2. Удаляем заказ из АКТИВНЫХ (убираем с планшета)
	ec.redisUtil.SRem("erp:orders:active", orderID)
	
	// 3. Добавляем заказ в АРХИВ (для истории) - только ID для статистики
	ec.redisUtil.RPush("erp:orders:archive", orderID)
	
	// 4. Обновляем счетчики
	ec.redisUtil.Increment("erp:orders:processed")
	ec.redisUtil.Decrement("erp:orders:pending")
	
	// 5. Удаляем заказ из Redis после обработки (источник истины - Kafka)
	orderKey := fmt.Sprintf("erp:order:%s", orderID)
	ec.redisUtil.Delete(orderKey)
	ec.redisUtil.Delete(fmt.Sprintf("order:%s", orderID))
	
	// 6. Отправляем обновление через WebSocket всем ERP клиентам
	BroadcastERPUpdate("order_processed", map[string]interface{}{
		"order_id": orderID,
		"message": "Заказ обработан",
	})

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"order_id": orderID,
		"message": "Заказ готов! Удален с планшета и перенесен в архив",
	})
}

// MarkOrderProcessed - оставляем для обратной совместимости, но теперь это алиас для MarkOrderReady
func (ec *ERPController) MarkOrderProcessed(c *gin.Context) {
	ec.MarkOrderReady(c)
}

// checkAndActivatePendingOrders проверяет ожидающие заказы и добавляет их в активные, когда наступает VisibleAt
func (ec *ERPController) checkAndActivatePendingOrders() {
	if ec.redisUtil == nil {
		return
	}

	// Получаем список ожидающих заказов
	pendingOrderIDs, err := ec.redisUtil.SMembers("erp:orders:pending_slots")
	if err != nil {
		return
	}

	// Получаем количество активных заказов для статистики
	activeCount, _ := ec.redisUtil.SCard("erp:orders:active")
	
	now := time.Now().UTC()
	activatedCount := 0
	
	// Логируем только если есть что активировать
	if activatedCount > 0 {
		log.Printf("📊 checkAndActivatePendingOrders: активировано=%d (pending было=%d, active стало=%d)", 
			activatedCount, len(pendingOrderIDs), activeCount+int64(activatedCount))
	}

	for _, orderID := range pendingOrderIDs {
		// Получаем VisibleAt из Redis (приоритет) или вычисляем из времени начала слота
		visibleAtKey := fmt.Sprintf("order:visible_at:%s", orderID)
		visibleAtStr, err := ec.redisUtil.Get(visibleAtKey)
		
		var visibleAt time.Time
		if err == nil && visibleAtStr != "" {
			// Используем сохраненное VisibleAt
			visibleAt, err = time.Parse(time.RFC3339, visibleAtStr)
			if err != nil {
				continue
			}
		} else {
			// Fallback: вычисляем из времени начала слота (для старых заказов)
			slotStartKey := fmt.Sprintf("order:slot:start:%s", orderID)
			slotStartStr, err := ec.redisUtil.Get(slotStartKey)
			if err != nil || slotStartStr == "" {
				continue
			}
			
			slotStartTime, err := time.Parse(time.RFC3339, slotStartStr)
			if err != nil {
				continue
			}
			
			// Для старых заказов используем фиксированные 15 минут
			visibleAt = slotStartTime.Add(-15 * time.Minute)
		}

		// Если время показа наступило, добавляем заказ в активные
		if !now.Before(visibleAt) {
			// Проверяем, существует ли заказ в Redis перед активацией
			orderKey := fmt.Sprintf("erp:order:%s", orderID)
			exists, _ := ec.redisUtil.Exists(orderKey)
			if !exists {
				// Заказ не существует в Redis - удаляем из pending_slots
				ec.redisUtil.SRem("erp:orders:pending_slots", orderID)
				log.Printf("🧹 checkAndActivatePendingOrders: удален несуществующий заказ %s из pending_slots", orderID)
				continue
			}
			
			// Проверяем, не активирован ли уже заказ (защита от повторной активации при параллельных запросах)
			isActive, _ := ec.redisUtil.SIsMember("erp:orders:active", orderID)
			if isActive {
				// Заказ уже активирован другим запросом, просто удаляем из pending
				ec.redisUtil.SRem("erp:orders:pending_slots", orderID)
				continue
			}
			
			// Получаем заказ из Redis и обновляем статус на "accepted"
			order, err := ec.getOrderFromRedis(orderID)
			if err == nil {
				// Обновляем статус заказа на "accepted" (принят)
				if order.Status == "pending" {
					order.Status = "accepted"
					// Сохраняем обновленный заказ обратно в Redis
					orderJSON, _ := json.Marshal(order)
					orderKey := fmt.Sprintf("erp:order:%s", orderID)
					ec.redisUtil.SetBytes(orderKey, orderJSON, 24*time.Hour)
					log.Printf("✅ Заказ %s: статус обновлен с 'pending' на 'accepted'", orderID)
				}
			}
			
			// Добавляем в активные
			ec.redisUtil.SAdd("erp:orders:active", orderID)
			// Уменьшаем счетчик ожидающих (не увеличиваем!)
			ec.redisUtil.Decrement("erp:orders:pending")
			
			// Удаляем из ожидающих
			ec.redisUtil.SRem("erp:orders:pending_slots", orderID)
			
			activatedCount++
			
			// Отправляем уведомление через WebSocket
			BroadcastERPUpdate("new_order", map[string]interface{}{
				"order_id": orderID,
				"message": "Заказ готов к приготовлению",
			})
		}
	}

	if activatedCount > 0 {
		log.Printf("📅 checkAndActivatePendingOrders: активировано=%d (pending было=%d, active стало=%d)", 
			activatedCount, len(pendingOrderIDs), activeCount+int64(activatedCount))
	}
}

// getOrderFromRedis читает заказ из Redis с поддержкой Protobuf и JSON
func (ec *ERPController) getOrderFromRedis(orderID string) (*models.PizzaOrder, error) {
	orderKey := "erp:order:" + orderID
	orderBytes, err := ec.redisUtil.GetBytes(orderKey)
	if err != nil {
		return nil, err
	}

	// Пробуем сначала Protobuf (быстрее!)
	pbOrder := &pb.PizzaOrder{}
	if err := proto.Unmarshal(orderBytes, pbOrder); err == nil {
		// Успешно распарсили Protobuf - конвертируем в models.PizzaOrder
		order := &models.PizzaOrder{
			ID:               pbOrder.Id,
			DisplayID:        pbOrder.DisplayId,
			CustomerID:       int(pbOrder.CustomerId),
			CustomerFirstName: pbOrder.CustomerFirstName,
			CustomerLastName:  pbOrder.CustomerLastName,
			CustomerPhone:     pbOrder.CustomerPhone,
			DeliveryAddress:   pbOrder.DeliveryAddress,
			IsPickup:          pbOrder.IsPickup,
			PickupLocationID:  pbOrder.PickupLocationId,
			TotalPrice:        int(pbOrder.TotalPrice),
			CreatedAt:         time.Unix(0, pbOrder.CreatedAt),
			Status:            pbOrder.Status,
			IsSet:             pbOrder.IsSet,
			SetName:           pbOrder.SetName,
			TargetSlotID:      pbOrder.TargetSlotId,
			DiscountAmount:    int(pbOrder.DiscountAmount),
			DiscountPercent:   int(pbOrder.DiscountPercent),
			FinalPrice:        int(pbOrder.FinalPrice),
		}
		// Конвертируем Items если есть
		for _, pbItem := range pbOrder.Items {
			// Вычисляем цену пиццы и допов из доступных данных
			// В protobuf пока нет отдельных полей, поэтому вычисляем
			pizzaPrice := int(pbItem.Price)
			extrasPrice := 0
			
			// Если есть допы, пытаемся вычислить их цену
			if len(pbItem.Extras) > 0 {
				// Получаем цену пиццы из меню
				if pizza, exists := models.GetPizza(pbItem.PizzaName); exists {
					pizzaPrice = pizza.Price
					// Вычисляем цену допов: общая цена - цена пиццы
					extrasPrice = int(pbItem.Price) - pizza.Price
					if extrasPrice < 0 {
						extrasPrice = 0
					}
				}
			}
			
			order.Items = append(order.Items, models.PizzaItem{
				PizzaName:   pbItem.PizzaName,
				Ingredients: pbItem.Ingredients,
				Extras:      pbItem.Extras,
				Quantity:    int(pbItem.Quantity),
				Price:       int(pbItem.Price),
				PizzaPrice:  pizzaPrice,
				ExtrasPrice: extrasPrice,
			})
		}
		
		// Получаем VisibleAt из protobuf, если есть
		if pbOrder.VisibleAt != "" {
			if visibleAt, err := time.Parse(time.RFC3339, pbOrder.VisibleAt); err == nil {
				order.VisibleAt = visibleAt
			}
		}
		
		// Если есть TargetSlotID, но нет времени начала слота, получаем его из Redis или SlotService
		if order.TargetSlotID != "" && order.TargetSlotStartTime.IsZero() {
			// Сначала пробуем получить из Redis (быстрее)
			slotStartKey := fmt.Sprintf("order:slot:start:%s", orderID)
			if slotStartStr, err := ec.redisUtil.Get(slotStartKey); err == nil && slotStartStr != "" {
				if slotStartTime, err := time.Parse(time.RFC3339, slotStartStr); err == nil {
					order.TargetSlotStartTime = slotStartTime
				}
			} else if ec.slotService != nil {
				// Fallback: получаем из SlotService
				slotInfo, err := ec.slotService.GetSlotInfo(order.TargetSlotID)
				if err == nil && !slotInfo.StartTime.IsZero() {
					order.TargetSlotStartTime = slotInfo.StartTime
				}
			}
		}
		
		// Если нет VisibleAt, получаем его из Redis или вычисляем
		if order.VisibleAt.IsZero() {
			visibleAtKey := fmt.Sprintf("order:visible_at:%s", orderID)
			if visibleAtStr, err := ec.redisUtil.Get(visibleAtKey); err == nil && visibleAtStr != "" {
				if visibleAt, err := time.Parse(time.RFC3339, visibleAtStr); err == nil {
					order.VisibleAt = visibleAt
				}
			} else if !order.TargetSlotStartTime.IsZero() {
				// Fallback: вычисляем из времени начала слота (для старых заказов)
				order.VisibleAt = order.TargetSlotStartTime.Add(-15 * time.Minute)
			}
		}
		
		// Если FinalPrice не задано или равно 0, используем TotalPrice как fallback
		if order.FinalPrice == 0 {
			order.FinalPrice = order.TotalPrice
		}
		
		return order, nil
	}

	// Fallback на JSON для обратной совместимости
	var order models.PizzaOrder
	if err := json.Unmarshal(orderBytes, &order); err != nil {
		return nil, err
	}
	
	// Вычисляем pizza_price и extras_price для каждого item, если они не установлены
	for i := range order.Items {
		if order.Items[i].PizzaPrice == 0 && order.Items[i].ExtrasPrice == 0 {
			// Получаем цену пиццы из меню
			if pizza, exists := models.GetPizza(order.Items[i].PizzaName); exists {
				order.Items[i].PizzaPrice = pizza.Price
				// Вычисляем цену допов: общая цена - цена пиццы
				order.Items[i].ExtrasPrice = order.Items[i].Price - pizza.Price
				if order.Items[i].ExtrasPrice < 0 {
					order.Items[i].ExtrasPrice = 0
				}
			} else {
				// Если пицца не найдена, используем общую цену как цену пиццы
				order.Items[i].PizzaPrice = order.Items[i].Price
				order.Items[i].ExtrasPrice = 0
			}
		}
	}
	
	// Если FinalPrice не задано или равно 0, используем TotalPrice как fallback
	if order.FinalPrice == 0 {
		order.FinalPrice = order.TotalPrice
	}
	
	// Если есть TargetSlotID, но нет времени начала слота, получаем его из Redis или SlotService
	if order.TargetSlotID != "" && order.TargetSlotStartTime.IsZero() {
		// Сначала пробуем получить из Redis (быстрее)
		slotStartKey := fmt.Sprintf("order:slot:start:%s", orderID)
		if slotStartStr, err := ec.redisUtil.Get(slotStartKey); err == nil && slotStartStr != "" {
			if slotStartTime, err := time.Parse(time.RFC3339, slotStartStr); err == nil {
				order.TargetSlotStartTime = slotStartTime
			}
		} else if ec.slotService != nil {
			// Fallback: получаем из SlotService
			slotInfo, err := ec.slotService.GetSlotInfo(order.TargetSlotID)
			if err == nil && !slotInfo.StartTime.IsZero() {
				order.TargetSlotStartTime = slotInfo.StartTime
			}
		}
	}
	
	// Если нет VisibleAt, получаем его из Redis или вычисляем
	if order.VisibleAt.IsZero() {
		visibleAtKey := fmt.Sprintf("order:visible_at:%s", orderID)
		if visibleAtStr, err := ec.redisUtil.Get(visibleAtKey); err == nil && visibleAtStr != "" {
			if visibleAt, err := time.Parse(time.RFC3339, visibleAtStr); err == nil {
				order.VisibleAt = visibleAt
			}
		} else if !order.TargetSlotStartTime.IsZero() {
			// Fallback: вычисляем из времени начала слота (для старых заказов)
			order.VisibleAt = order.TargetSlotStartTime.Add(-15 * time.Minute)
		}
	}
	
	return &order, nil
}

// GetKafkaOrdersCount получает количество заказов из Kafka топика
func (ec *ERPController) GetKafkaOrdersCount(c *gin.Context) {
	if ec.kafkaBrokers == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "Kafka not configured",
			"count": 0,
		})
		return
	}

	brokers := strings.Split(ec.kafkaBrokers, ",")
	brokerAddr := brokers[0]
	
	conn, err := kafka.Dial("tcp", brokerAddr)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": fmt.Sprintf("Failed to connect to Kafka: %v", err),
			"count": 0,
		})
		return
	}
	defer conn.Close()

	// Получаем метаданные топика для подсчета сообщений
	partitions, err := conn.ReadPartitions("pizza-orders")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("Failed to read partitions: %v", err),
			"count": 0,
		})
		return
	}

	var totalKafkaOrders int64
	for _, p := range partitions {
		// Используем DialLeader вместо DialPartition
		partitionConn, err := kafka.DialLeader(context.Background(), "tcp", brokerAddr, "pizza-orders", p.ID)
		if err != nil {
			log.Printf("⚠️ Ошибка подключения к партиции %d: %v", p.ID, err)
			continue
		}
		
		// Получаем границы (first и last offset) для партиции
		first, last, err := partitionConn.ReadOffsets()
		partitionConn.Close()
		if err != nil {
			log.Printf("⚠️ Ошибка чтения offset для партиции %d: %v", p.ID, err)
			continue
		}
		// last offset = количество сообщений в партиции (offset начинается с 0)
		messagesCount := last - first
		totalKafkaOrders += messagesCount
		log.Printf("📊 Партиция %d: first=%d, last=%d, сообщений=%d", p.ID, first, last, messagesCount)
	}

	c.JSON(http.StatusOK, gin.H{
		"topic":        "pizza-orders",
		"total_orders": totalKafkaOrders,
		"partitions":   len(partitions),
		"timestamp":    time.Now().Format(time.RFC3339),
	})
}

// GetKafkaOrdersSample получает несколько последних заказов из Kafka (для проверки)
func (ec *ERPController) GetKafkaOrdersSample(c *gin.Context) {
	if ec.kafkaBrokers == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Kafka not configured"})
		return
	}

	brokers := strings.Split(ec.kafkaBrokers, ",")
	if len(brokers) == 0 || brokers[0] == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "Kafka broker address is empty",
		})
		return
	}
	brokerAddr := strings.TrimSpace(brokers[0])
	
	// Пробуем подключиться к Kafka
	conn, err := kafka.Dial("tcp", brokerAddr)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": fmt.Sprintf("Failed to connect to Kafka: %v", err),
		})
		return
	}
	defer conn.Close()

	// Используем DialLeader вместо DialPartition (более надежный способ)
	partitionConn, err := kafka.DialLeader(context.Background(), "tcp", brokerAddr, "pizza-orders", 0)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("Failed to connect to partition: %v", err),
		})
		return
	}
	defer partitionConn.Close()

	// Получаем границы (first и last offset) для партиции 0
	_, last, err := partitionConn.ReadOffsets()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("Failed to read offset: %v", err),
		})
		return
	}
	lastOffset := last

	// Читаем последние 10 сообщений
	limit := 10
	startOffset := lastOffset - int64(limit)
	if startOffset < 0 {
		startOffset = 0
	}

	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:   brokers,
		Topic:     "pizza-orders",
		Partition: 0,
		MinBytes:  1,
		MaxBytes:  10e6,
	})
	defer reader.Close()

	reader.SetOffset(startOffset)
	orders := make([]map[string]interface{}, 0)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	for i := 0; i < limit && int64(i) < lastOffset; i++ {
		msg, err := reader.FetchMessage(ctx)
		if err != nil {
			break
		}

		// Пробуем распарсить Protobuf
		pbOrder := &pb.PizzaOrder{}
		if err := proto.Unmarshal(msg.Value, pbOrder); err == nil {
			orderData := map[string]interface{}{
				"id":          pbOrder.Id,
				"display_id":  pbOrder.DisplayId,
				"customer_id": pbOrder.CustomerId,
				"status":      pbOrder.Status,
				"created_at":  time.Unix(0, pbOrder.CreatedAt).Format(time.RFC3339),
				"size_bytes":  len(msg.Value),
			}
			
			// Добавляем информацию о слоте и времени показа
			if pbOrder.TargetSlotId != "" {
				orderData["target_slot_id"] = pbOrder.TargetSlotId
			}
			if pbOrder.VisibleAt != "" {
				orderData["visible_at"] = pbOrder.VisibleAt
			}
			if pbOrder.TotalPrice > 0 {
				orderData["total_price"] = pbOrder.TotalPrice
			}
			
			orders = append(orders, orderData)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"orders":      orders,
		"count":       len(orders),
		"total_in_kafka": lastOffset,
		"topic":       "pizza-orders",
		"format":      "protobuf",
	})
}

// GetSlots получает список всех слотов с их загрузкой
func (ec *ERPController) GetSlots(c *gin.Context) {
	if ec.slotService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "SlotService not available",
		})
		return
	}

	slots, err := ec.slotService.GetAllSlots()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	// Конвертируем слоты в формат с правильными временами (ISO 8601 строки)
	type OrderResponse struct {
		ID      string `json:"id"`
		Total   int    `json:"total"`
		IsPickup bool  `json:"is_pickup"`
	}
	
	type SlotResponse struct {
		SlotID        string          `json:"slot_id"`
		StartTime     string          `json:"start_time"`     // ISO 8601 строка
		EndTime       string          `json:"end_time"`       // ISO 8601 строка
		CurrentLoad   int             `json:"current_load"`
		MaxCapacity   int             `json:"max_capacity"`
		OrdersCount   int             `json:"orders_count"`
		DeliveryCount int             `json:"delivery_count"`
		PickupCount   int             `json:"pickup_count"`
		DeliveryPlan  int             `json:"delivery_plan"`  // План для доставки (85% от max_capacity)
		PickupPlan    int             `json:"pickup_plan"`     // План для самовывоза (15% от max_capacity)
		Disabled      bool            `json:"disabled"`        // Отключен ли слот
		Orders        []OrderResponse `json:"orders"`
	}

	slotResponses := make([]SlotResponse, len(slots))
	for i, slot := range slots {
		// Конвертируем заказы (если slot.Orders == nil, создаем пустой массив)
		orders := slot.Orders
		if orders == nil {
			orders = make([]services.OrderInfo, 0)
		}
		orderResponses := make([]OrderResponse, len(orders))
		for j, order := range orders {
			orderResponses[j] = OrderResponse{
				ID:       order.ID,
				Total:    order.Total,
				IsPickup: order.IsPickup,
			}
		}
		
		// Убеждаемся, что orderResponses не nil (даже если пустой)
		// КРИТИЧНО: Всегда инициализируем как пустой массив, чтобы поле всегда было в JSON
		if orderResponses == nil {
			orderResponses = make([]OrderResponse, 0)
		}
		
		// КРИТИЧНО: Используем планы из SlotInfo (они уже загружены из Redis в GetAllSlots)
		// Если планы = 0, это может быть либо сохраненное значение 0, либо отсутствие в Redis
		// Поэтому проверяем Redis напрямую, и только если там нет - вычисляем по умолчанию
		deliveryPlan := slot.DeliveryPlan
		pickupPlan := slot.PickupPlan
		
		// Проверяем, есть ли планы в Redis для этого слота
		// Если оба плана = 0, проверяем Redis - возможно, они просто не были установлены
		if deliveryPlan == 0 && pickupPlan == 0 && slot.MaxCapacity > 0 {
			// Пробуем загрузить из Redis
			redisDeliveryPlan, redisPickupPlan, err := ec.slotService.GetSlotPlan(slot.SlotID)
			if err == nil {
				// Если в Redis есть хотя бы один план - используем их
				if redisDeliveryPlan > 0 || redisPickupPlan > 0 {
					deliveryPlan = redisDeliveryPlan
					pickupPlan = redisPickupPlan
				} else {
					// В Redis тоже 0 - вычисляем по умолчанию
					deliveryPlan = int(float64(slot.MaxCapacity) * 0.85)
					pickupPlan = int(float64(slot.MaxCapacity) * 0.15)
				}
			} else {
				// Ошибка загрузки из Redis - вычисляем по умолчанию
				deliveryPlan = int(float64(slot.MaxCapacity) * 0.85)
				pickupPlan = int(float64(slot.MaxCapacity) * 0.15)
			}
		}
		
		// КРИТИЧНО: Явно устанавливаем Disabled, даже если slot.Disabled = false
		// Это гарантирует, что поле всегда будет в JSON ответе
		disabledValue := slot.Disabled
		
		// КРИТИЧНО: Явно инициализируем Orders как пустой массив, если он nil
		// Это гарантирует, что поле всегда будет в JSON ответе (даже как пустой массив [])
		finalOrders := orderResponses
		if finalOrders == nil {
			finalOrders = make([]OrderResponse, 0)
		}
		
		slotResponses[i] = SlotResponse{
			SlotID:        slot.SlotID,
			StartTime:     slot.StartTime.Format(time.RFC3339),
			EndTime:       slot.EndTime.Format(time.RFC3339),
			CurrentLoad:   slot.CurrentLoad,
			MaxCapacity:   slot.MaxCapacity,
			OrdersCount:   slot.OrdersCount,
			DeliveryCount: slot.DeliveryCount,
			PickupCount:   slot.PickupCount,
			DeliveryPlan:  deliveryPlan,
			PickupPlan:    pickupPlan,
			Disabled:      disabledValue, // Явно устанавливаем значение
			Orders:        finalOrders,   // КРИТИЧНО: Всегда не-nil массив
		}
		
		// ОТЛАДКА: Логируем orders для слотов с заказами
		if len(finalOrders) > 0 {
			log.Printf("📦 GetSlots: слот %s имеет %d заказов: %+v", slot.SlotID, len(finalOrders), finalOrders)
		}
		
		// Логируем для отладки (только для первого слота)
		if i == 0 {
			log.Printf("🔍 GetSlots: первый слот - Orders count: %d, orderResponses len: %d, Disabled: %v", 
				len(orders), len(orderResponses), slot.Disabled)
		}
		
		// КРИТИЧНО: Логируем disabled статус для всех слотов, чтобы проверить, что он правильно устанавливается
		if slot.Disabled {
			log.Printf("🔴 GetSlots: слот %s отключен (Disabled=true)", slot.SlotID)
		}
	}

	// КРИТИЧНО: Логируем первый слот для отладки disabled поля
	if len(slotResponses) > 0 {
		log.Printf("🔍 GetSlots: первый слот в ответе - Disabled: %v, SlotID: %s", 
			slotResponses[0].Disabled, slotResponses[0].SlotID)
	}
	
	// КРИТИЧНО: Логируем disabled статус и orders для всех слотов перед отправкой
	for i, slotResp := range slotResponses {
		if slotResp.Disabled {
			log.Printf("🔴 GetSlots: отправляем слот %s с disabled=true", slotResp.SlotID)
		}
		if i < 3 {
			log.Printf("🔍 GetSlots: слот %s - Disabled=%v, Orders count=%d (в SlotResponse)", 
				slotResp.SlotID, slotResp.Disabled, len(slotResp.Orders))
		}
		// Логируем слоты с заказами
		if len(slotResp.Orders) > 0 {
			log.Printf("📦 GetSlots: слот %s имеет %d заказов: %+v", 
				slotResp.SlotID, len(slotResp.Orders), slotResp.Orders)
		}
	}
	
	// КРИТИЧНО: Проверяем, что orders сериализуются правильно
	// Создаем тестовый JSON для проверки
	if len(slotResponses) > 0 {
		testJSON, _ := json.Marshal(slotResponses[0])
		log.Printf("🔍 GetSlots: тестовая сериализация первого слота: %s", string(testJSON))
	}
	
	c.JSON(http.StatusOK, gin.H{
		"slots": slotResponses,
		"count": len(slotResponses),
	})
}

// GetSlotConfig получает текущую конфигурацию слотов (максимальная емкость)
func (ec *ERPController) GetSlotConfig(c *gin.Context) {
	if ec.slotService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "SlotService not available",
		})
		return
	}

	// Получаем текущую емкость через GetSlotInfo для первого слота
	now := time.Now()
	slotStart := ec.slotService.GetSlotStartTime(now)
	slotID := ec.slotService.GenerateSlotID(slotStart)
	
	slotInfo, err := ec.slotService.GetSlotInfo(slotID)
	if err != nil {
			// Если слот не существует, используем дефолтное значение из SlotService
		c.JSON(http.StatusOK, gin.H{
				"max_capacity": 10000, // Дефолт (устанавливается через UpdateSlotConfig)
			"slot_duration_minutes": 15,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"max_capacity": slotInfo.MaxCapacity,
		"slot_duration_minutes": 15,
	})
}

// UpdateSlotConfig обновляет максимальную емкость слотов
func (ec *ERPController) UpdateSlotConfig(c *gin.Context) {
	if ec.slotService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "SlotService not available",
		})
		return
	}

	var req struct {
		MaxCapacity int `json:"max_capacity" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid request",
			"details": err.Error(),
		})
		return
	}

	if req.MaxCapacity <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "max_capacity must be greater than 0",
		})
		return
	}

	ec.slotService.SetMaxCapacity(req.MaxCapacity)

	// Отправляем обновление через WebSocket
	BroadcastERPUpdate("slot_config_updated", map[string]interface{}{
		"max_capacity": req.MaxCapacity,
		"message": "Конфигурация слотов обновлена",
	})

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"max_capacity": req.MaxCapacity,
		"message": "Slot capacity updated successfully",
	})
}

// ToggleSlot отключает/включает слот (использует SetSlotDisabled из SlotService)
func (ec *ERPController) ToggleSlot(c *gin.Context) {
	slotID := c.Param("slot_id")
	
	var req struct {
		Disabled bool `json:"disabled" binding:"required"`
	}
	
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid request",
			"details": err.Error(),
		})
		return
	}
	
	if ec.slotService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "SlotService not available",
		})
		return
	}
	
	err := ec.slotService.SetSlotDisabled(slotID, req.Disabled)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}
	
	// Отправляем обновление через WebSocket
	BroadcastERPUpdate("slot_toggled", map[string]interface{}{
		"slot_id": slotID,
		"disabled": req.Disabled,
		"message": fmt.Sprintf("Слот %s", map[bool]string{true: "отключен", false: "включен"}[req.Disabled]),
	})
	
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"slot_id": slotID,
		"disabled": req.Disabled,
		"message": fmt.Sprintf("Слот %s", map[bool]string{true: "отключен", false: "включен"}[req.Disabled]),
	})
}

// UpdateSlotPlan обновляет план для слота (delivery_plan и pickup_plan)
func (ec *ERPController) UpdateSlotPlan(c *gin.Context) {
	slotID := c.Param("slot_id")
	log.Printf("🔍 UpdateSlotPlan: получен slot_id = %s", slotID)
	
	var req struct {
		DeliveryPlan int `json:"delivery_plan" binding:"required"`
		PickupPlan   int `json:"pickup_plan" binding:"required"`
	}
	
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid request",
			"details": err.Error(),
		})
		return
	}
	
	if req.DeliveryPlan < 0 || req.PickupPlan < 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "plans must be non-negative",
		})
		return
	}
	
	if ec.slotService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "SlotService not available",
		})
		return
	}
	
	err := ec.slotService.SetSlotPlan(slotID, req.DeliveryPlan, req.PickupPlan)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}
	
	// Отправляем обновление через WebSocket
	BroadcastERPUpdate("slot_plan_updated", map[string]interface{}{
		"slot_id":       slotID,
		"delivery_plan": req.DeliveryPlan,
		"pickup_plan":   req.PickupPlan,
		"message":       "План слота обновлен",
	})
	
	c.JSON(http.StatusOK, gin.H{
		"success":       true,
		"slot_id":       slotID,
		"delivery_plan": req.DeliveryPlan,
		"pickup_plan":   req.PickupPlan,
		"message":       "План слота обновлен",
	})
}

// UpdateSlotsPlanBatch обновляет планы для нескольких слотов сразу (батч)
func (ec *ERPController) UpdateSlotsPlanBatch(c *gin.Context) {
	var req struct {
		Slots []struct {
			SlotID       string `json:"slot_id" binding:"required"`
			DeliveryPlan int    `json:"delivery_plan" binding:"required"`
			PickupPlan   int    `json:"pickup_plan" binding:"required"`
		} `json:"slots" binding:"required,min=1"`
	}
	
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid request",
			"details": err.Error(),
		})
		return
	}
	
	if ec.slotService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "SlotService not available",
		})
		return
	}
	
	// Обновляем планы для всех слотов
	updatedSlots := make([]map[string]interface{}, 0, len(req.Slots))
	errors := make([]string, 0)
	
	for _, slotReq := range req.Slots {
		if slotReq.DeliveryPlan < 0 || slotReq.PickupPlan < 0 {
			errors = append(errors, fmt.Sprintf("slot %s: plans must be non-negative", slotReq.SlotID))
			continue
		}
		
		err := ec.slotService.SetSlotPlan(slotReq.SlotID, slotReq.DeliveryPlan, slotReq.PickupPlan)
		if err != nil {
			errors = append(errors, fmt.Sprintf("slot %s: %v", slotReq.SlotID, err))
			continue
		}
		
		updatedSlots = append(updatedSlots, map[string]interface{}{
			"slot_id":       slotReq.SlotID,
			"delivery_plan": slotReq.DeliveryPlan,
			"pickup_plan":   slotReq.PickupPlan,
		})
		
		// Отправляем обновление через WebSocket для каждого слота
		BroadcastERPUpdate("slot_plan_updated", map[string]interface{}{
			"slot_id":       slotReq.SlotID,
			"delivery_plan": slotReq.DeliveryPlan,
			"pickup_plan":   slotReq.PickupPlan,
			"message":       fmt.Sprintf("План слота %s обновлен", slotReq.SlotID),
		})
	}
	
	log.Printf("✅ UpdateSlotsPlanBatch: обновлено %d из %d слотов", len(updatedSlots), len(req.Slots))
	
	if len(errors) > 0 {
		log.Printf("⚠️ UpdateSlotsPlanBatch: ошибки при обновлении: %v", errors)
	}
	
	c.JSON(http.StatusOK, gin.H{
		"success":      len(errors) == 0,
		"updated":      len(updatedSlots),
		"total":        len(req.Slots),
		"updated_slots": updatedSlots,
		"errors":       errors,
	})
}

// UpdateSlotDisabled обновляет статус отключения слота
func (ec *ERPController) UpdateSlotDisabled(c *gin.Context) {
	slotID := c.Param("slot_id")
	// Декодируем slot_id, так как он может быть закодирован (содержит двоеточие)
	decodedSlotID, err := url.QueryUnescape(slotID)
	if err == nil && decodedSlotID != slotID {
		log.Printf("🔍 UpdateSlotDisabled: декодирован slot_id: %s -> %s", slotID, decodedSlotID)
		slotID = decodedSlotID
	}
	log.Printf("🔍 UpdateSlotDisabled: получен slot_id = %s (raw: %s)", slotID, c.Param("slot_id"))
	
	// КРИТИЧНО: Читаем тело запроса для диагностики
	bodyBytes, _ := c.GetRawData()
	log.Printf("📥 UpdateSlotDisabled: тело запроса (raw): %s", string(bodyBytes))
	
	// Восстанавливаем тело для дальнейшей обработки
	c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
	
	// КРИТИЧНО: Используем указатель на bool, чтобы отличить отсутствие поля от false
	// binding:"required" для bool не работает правильно, когда значение false
	var req struct {
		Disabled *bool `json:"disabled" binding:"required"`
	}
	
	// Используем = вместо :=, так как err уже объявлена выше
	if err = c.ShouldBindJSON(&req); err != nil {
		log.Printf("❌ UpdateSlotDisabled: ошибка парсинга JSON: %v", err)
		log.Printf("📥 UpdateSlotDisabled: тело запроса было: %s", string(bodyBytes))
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid request",
			"details": err.Error(),
		})
		return
	}
	
	// Проверяем, что поле было передано
	if req.Disabled == nil {
		log.Printf("❌ UpdateSlotDisabled: поле disabled не передано")
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid request",
			"details": "field 'disabled' is required",
		})
		return
	}
	
	disabledValue := *req.Disabled
	log.Printf("✅ UpdateSlotDisabled: успешно распарсен запрос, disabled = %v", disabledValue)
	
	if ec.slotService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "SlotService not available",
		})
		return
	}
	
	// Используем = вместо :=, так как err уже объявлена выше
	err = ec.slotService.SetSlotDisabled(slotID, disabledValue)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}
	
	// Отправляем обновление через WebSocket
	BroadcastERPUpdate("slot_disabled_updated", map[string]interface{}{
		"slot_id":  slotID,
		"disabled": disabledValue,
		"message":  fmt.Sprintf("Слот %s", map[bool]string{true: "отключен", false: "включен"}[disabledValue]),
	})
	
	c.JSON(http.StatusOK, gin.H{
		"success":  true,
		"slot_id":  slotID,
		"disabled": disabledValue,
		"message":  fmt.Sprintf("Слот %s", map[bool]string{true: "отключен", false: "включен"}[disabledValue]),
	})
}

// UpdateSlotCapacity обновляет лимит конкретного слота
func (ec *ERPController) UpdateSlotCapacity(c *gin.Context) {
	slotID := c.Param("slot_id")
	
	var req struct {
		MaxCapacity int `json:"max_capacity" binding:"required"`
	}
	
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid request",
			"details": err.Error(),
		})
		return
	}
	
	if req.MaxCapacity <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "max_capacity must be greater than 0",
		})
		return
	}
	
	// Сохраняем лимит слота в Redis
	key := fmt.Sprintf("slot:%s:max_capacity", slotID)
	
	if err := ec.redisUtil.Set(key, fmt.Sprintf("%d", req.MaxCapacity), 0); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "failed to update slot capacity",
		})
		return
	}
	
	// Отправляем обновление через WebSocket
	BroadcastERPUpdate("slot_capacity_updated", map[string]interface{}{
		"slot_id": slotID,
		"max_capacity": req.MaxCapacity,
		"message": "Лимит слота обновлен",
	})
	
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"slot_id": slotID,
		"max_capacity": req.MaxCapacity,
		"message": "Лимит слота обновлен",
	})
}

// GetRevenue получает выручку за указанную дату или за сегодня
// GET /api/v1/erp/revenue?date=2006-01-02 (опционально)
func (ec *ERPController) GetRevenue(c *gin.Context) {
	if ec.revenueService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "Revenue service not available",
		})
		return
	}

	date := c.DefaultQuery("date", "")
	revenue, err := ec.revenueService.GetRevenueForDate(date)
	if err != nil {
		log.Printf("❌ GetRevenue: ошибка получения выручки: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Ошибка получения выручки",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, revenue)
}

// GetDailyPlan получает план на день
// GET /api/v1/erp/daily-plan?date=2006-01-02 (опционально)
func (ec *ERPController) GetDailyPlan(c *gin.Context) {
	if ec.dailyPlanService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "Daily plan service not available",
		})
		return
	}

	date := c.DefaultQuery("date", "")
	plan, err := ec.dailyPlanService.GetDailyPlan(date)
	if err != nil {
		log.Printf("❌ GetDailyPlan: ошибка получения плана: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Ошибка получения плана",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"date": date,
		"plan": plan,
	})
}

// SetDailyPlan устанавливает план на день
// PUT /api/v1/erp/daily-plan
// Body: {"date": "2006-01-02", "plan": 500000.0}
func (ec *ERPController) SetDailyPlan(c *gin.Context) {
	if ec.dailyPlanService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "Daily plan service not available",
		})
		return
	}

	var req struct {
		Date string  `json:"date"` // Опционально, по умолчанию сегодня
		Plan float64 `json:"plan" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid request",
			"details": err.Error(),
		})
		return
	}

	if req.Plan < 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "plan must be greater than or equal to 0",
		})
		return
	}

	err := ec.dailyPlanService.SetDailyPlan(req.Date, req.Plan)
	if err != nil {
		log.Printf("❌ SetDailyPlan: ошибка установки плана: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Ошибка установки плана",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"date":    req.Date,
		"plan":    req.Plan,
		"message": "План на день установлен",
	})
}

// GetKitchenLoad получает загрузку кухни
// GET /api/v1/erp/kitchen-load?window=next (window: current, next, shift)
func (ec *ERPController) GetKitchenLoad(c *gin.Context) {
	if ec.kitchenLoadService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "Kitchen load service not available",
		})
		return
	}

	// По умолчанию используем "next" (оперативное управление - текущий + следующий слот)
	window := c.DefaultQuery("window", "next")
	
	// Валидация window
	validWindows := map[string]bool{
		"current":    true,
		"next":       true,
		"operational": true,
		"shift":      true,
	}
	if !validWindows[window] {
		window = "next" // Fallback на оперативное управление
	}

	loadStats, err := ec.kitchenLoadService.GetKitchenLoad(window)
	if err != nil {
		log.Printf("❌ GetKitchenLoad: ошибка получения загрузки кухни: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Ошибка получения загрузки кухни",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, loadStats)
}

// GetRevenueForecast получает прогноз выручки на конец дня
// GET /api/v1/erp/revenue/forecast
func (ec *ERPController) GetRevenueForecast(c *gin.Context) {
	if ec.revenueService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "Revenue service not available",
		})
		return
	}

	forecast, err := ec.revenueService.GetRevenueForecast()
	if err != nil {
		log.Printf("❌ GetRevenueForecast: ошибка получения прогноза: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Ошибка получения прогноза выручки",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, forecast)
}