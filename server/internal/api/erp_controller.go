package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/segmentio/kafka-go"
	"google.golang.org/protobuf/proto"
	"zephyrvpn/server/internal/models"
	"zephyrvpn/server/internal/pb"
	"zephyrvpn/server/internal/services"
	"zephyrvpn/server/internal/utils"
)

type ERPController struct {
	redisUtil   *utils.RedisClient
	kafkaBrokers string
	slotService *services.SlotService
}

func NewERPController(redisUtil *utils.RedisClient, kafkaBrokers string, openHour, closeHour, closeMin int) *ERPController {
	slotService := services.NewSlotService(redisUtil, openHour, closeHour, closeMin)
	return &ERPController{
		redisUtil:   redisUtil,
		kafkaBrokers: kafkaBrokers,
		slotService: slotService,
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

// GetStats получает статистику для ERP
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

	c.JSON(http.StatusOK, gin.H{
		"total_orders":     total,
		"today_orders":     today,
		"pending_orders":   pending,
		"processed_orders": fmt.Sprintf("%d", processed),
		"system":           "ЕРПИ ТЕСТ",
		"timestamp":        time.Now().Format(time.RFC3339),
	})
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
		}
		// Конвертируем Items если есть
		for _, pbItem := range pbOrder.Items {
			order.Items = append(order.Items, models.PizzaItem{
				PizzaName:   pbItem.PizzaName,
				Ingredients: pbItem.Ingredients,
				Extras:      pbItem.Extras,
				Quantity:    int(pbItem.Quantity),
				Price:       int(pbItem.Price),
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
		
		return order, nil
	}

	// Fallback на JSON для обратной совместимости
	var order models.PizzaOrder
	if err := json.Unmarshal(orderBytes, &order); err != nil {
		return nil, err
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
	conn, err := kafka.Dial("tcp", brokers[0])
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": fmt.Sprintf("Failed to connect to Kafka: %v", err),
		})
		return
	}
	defer conn.Close()

	// Подключаемся к партиции 0 для получения offset
	partitionConn, err := kafka.DialPartition(context.Background(), "tcp", brokers[0], kafka.Partition{
		Topic: "pizza-orders",
		ID:    0,
	})
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
			orders = append(orders, map[string]interface{}{
				"id":          pbOrder.Id,
				"display_id":  pbOrder.DisplayId,
				"customer_id": pbOrder.CustomerId,
				"status":      pbOrder.Status,
				"created_at":  time.Unix(0, pbOrder.CreatedAt).Format(time.RFC3339),
				"size_bytes":  len(msg.Value),
			})
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
	type SlotResponse struct {
		SlotID      string `json:"slot_id"`
		StartTime   string `json:"start_time"`   // ISO 8601 строка
		EndTime     string `json:"end_time"`     // ISO 8601 строка
		CurrentLoad int    `json:"current_load"`
		MaxCapacity int    `json:"max_capacity"`
	}

	slotResponses := make([]SlotResponse, len(slots))
	for i, slot := range slots {
		slotResponses[i] = SlotResponse{
			SlotID:      slot.SlotID,
			StartTime:   slot.StartTime.Format(time.RFC3339),
			EndTime:     slot.EndTime.Format(time.RFC3339),
			CurrentLoad: slot.CurrentLoad,
			MaxCapacity: slot.MaxCapacity,
		}
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

