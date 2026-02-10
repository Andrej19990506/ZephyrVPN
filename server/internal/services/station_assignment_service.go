package services

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"gorm.io/gorm"
	"zephyrvpn/server/internal/models"
	"zephyrvpn/server/internal/utils"
)

// StationAssignmentService управляет распределением заказов по станциям
type StationAssignmentService struct {
	db        *gorm.DB
	redisUtil *utils.RedisClient
}

// NewStationAssignmentService создает новый сервис распределения по станциям
func NewStationAssignmentService(db *gorm.DB, redisUtil *utils.RedisClient) *StationAssignmentService {
	return &StationAssignmentService{
		db:        db,
		redisUtil: redisUtil,
	}
}

// AssignOrderToStations распределяет заказ по станциям при создании
// Логика: берет StationID напрямую из Recipe (обязательно должен быть указан)
// Если Recipe не найден или StationID не указан - возвращает ошибку
func (sas *StationAssignmentService) AssignOrderToStations(order *models.PizzaOrder) error {
	if sas.redisUtil == nil {
		return fmt.Errorf("Redis недоступен")
	}

	if sas.db == nil {
		return fmt.Errorf("PostgreSQL недоступен")
	}

	// Создаем маппинг заказа к станциям
	mapping := models.OrderStationMapping{
		OrderID:           order.ID,
		ItemStatuses:      make([]models.OrderItemStatus, 0, len(order.Items)),
		StationAssignments: make(map[string][]int),
		CurrentStage:      "preparation",
		CreatedAt:         time.Now(),
		UpdatedAt:         time.Now(),
	}

	// Обрабатываем каждую позицию заказа
	for itemIndex, item := range order.Items {
		// Получаем Recipe через PizzaName (ищем по имени)
		var recipe models.Recipe
		if err := sas.db.Where("name = ? AND is_active = true AND deleted_at IS NULL", item.PizzaName).First(&recipe).Error; err != nil {
			log.Printf("❌ AssignOrderToStations: Recipe не найден для '%s'", item.PizzaName)
			return fmt.Errorf("Recipe не найден для позиции '%s' (обязательно должен быть создан рецепт с указанием StationIDs)", item.PizzaName)
		}

		// Получаем массив StationIDs из рецепта
		stationIDs, err := recipe.GetStationIDs()
		if err != nil {
			log.Printf("❌ AssignOrderToStations: ошибка парсинга StationIDs для '%s': %v", item.PizzaName, err)
			return fmt.Errorf("Ошибка парсинга StationIDs в рецепте '%s': %w", item.PizzaName, err)
		}

		// Проверяем, что StationIDs указаны в рецепте (обязательно)
		if len(stationIDs) == 0 {
			log.Printf("❌ AssignOrderToStations: StationIDs не указаны в рецепте '%s'", item.PizzaName)
			return fmt.Errorf("StationIDs не указаны в рецепте '%s' (обязательно нужно указать хотя бы одну станцию при создании рецепта)", item.PizzaName)
		}

		// Берем первую станцию из списка (начальная станция)
		firstStationID := stationIDs[0]

		// Проверяем, что станция существует и активна
		var station models.Station
		if err := sas.db.Where("id = ? AND deleted_at IS NULL", firstStationID).First(&station).Error; err != nil {
			log.Printf("❌ AssignOrderToStations: станция %s не найдена для рецепта '%s'", firstStationID, item.PizzaName)
			return fmt.Errorf("Станция %s не найдена для рецепта '%s' (проверьте, что станция существует и не удалена)", firstStationID, item.PizzaName)
		}

		// Создаем статус позиции и привязываем к первой станции
		itemStatus := models.OrderItemStatus{
			OrderID:             order.ID,
			ItemIndex:           itemIndex,
			StationID:           station.ID,
			StationIDs:          recipe.StationIDs, // Сохраняем весь список StationIDs
			CurrentStationIndex: 0,                 // Начинаем с первой станции (индекс 0)
			Status:              "pending",
			UpdatedAt:            time.Now(),
		}

		// Добавляем в список позиций этой станции
		mapping.StationAssignments[station.ID] = append(mapping.StationAssignments[station.ID], itemIndex)
		// Увеличиваем счетчик очереди станции
		sas.incrementStationQueue(station.ID)

		mapping.ItemStatuses = append(mapping.ItemStatuses, itemStatus)
	}

	// Сохраняем маппинг в Redis
	if err := sas.saveOrderStationMapping(&mapping); err != nil {
		return fmt.Errorf("ошибка сохранения маппинга станций: %w", err)
	}

	log.Printf("✅ AssignOrderToStations: заказ %s распределен по станциям (%d позиций)", 
		order.ID, len(mapping.ItemStatuses))

	return nil
}

// GetOrderForStation возвращает заказ с учетом видимости для конкретной станции
// Логика видимости на основе capabilities:
// - Станция с "view_composition" (Приготовка) - видит ТОЛЬКО свои позиции, назначенные на эту станцию
// - Станция с "view_oven_queue" (Выпечка) - видит позиции от других станций, которые готовы к выпечке (ready)
//   Может работать только если позиции ready (готовы к выпечке)
// - Станция с "order_assembly" (Упаковка) - видит ВЕСЬ заказ полностью
//   Может работать только если ВСЕ позиции ready/completed
// - Станция без специальных capabilities - видит только свои позиции, назначенные на эту станцию
func (sas *StationAssignmentService) GetOrderForStation(order *models.PizzaOrder, stationID string) (*models.PizzaOrder, bool, error) {
	if sas.redisUtil == nil {
		return nil, false, fmt.Errorf("Redis недоступен")
	}

	// Получаем маппинг заказа
	mapping, err := sas.getOrderStationMapping(order.ID)
	if err != nil {
		// Если маппинга нет, возвращаем заказ как есть (для обратной совместимости)
		return order, true, nil
	}

	// Получаем информацию о станции
	var station models.Station
	if sas.db != nil {
		if err := sas.db.Where("id = ? AND deleted_at IS NULL", stationID).First(&station).Error; err != nil {
			return nil, false, fmt.Errorf("станция не найдена: %w", err)
		}
	}

	// Определяем, какие позиции видит эта станция
	visibleItems := make([]models.PizzaItem, 0)
	canWork := false

	// Проверяем capabilities станции
	hasViewComposition := contains(station.Config.Capabilities, "view_composition")
	hasViewOvenQueue := contains(station.Config.Capabilities, "view_oven_queue")
	hasOrderAssembly := contains(station.Config.Capabilities, "order_assembly")

	for _, itemStatus := range mapping.ItemStatuses {
		item := order.Items[itemStatus.ItemIndex]
		shouldShow := false
		canWorkOnThis := false

		// Проверяем, что станция входит в список StationIDs для этой позиции
		stationIDs, err := sas.parseStationIDs(itemStatus.StationIDs)
		if err != nil {
			log.Printf("⚠️ GetOrderForStation: ошибка парсинга StationIDs для позиции %d: %v", itemStatus.ItemIndex, err)
			continue
		}
		isStationInList := sas.containsStation(stationIDs, stationID)
		if !isStationInList {
			// Станция не входит в список StationIDs для этой позиции - пропускаем
			continue
		}

		// Логика видимости в зависимости от capabilities станции
		if hasViewComposition {
			// Станция "Приготовка" (view_composition) - видит ТОЛЬКО свои позиции
			// Позиции должны быть назначены на эту станцию (текущая станция) и в статусе pending/preparing
			if itemStatus.StationID == stationID && 
			   (itemStatus.Status == "pending" || itemStatus.Status == "preparing") {
				shouldShow = true
				canWorkOnThis = true
			}
		} else if hasViewOvenQueue {
			// Станция "Выпечка" (view_oven_queue) - видит позиции от предыдущих станций
			// Видит позиции, которые назначены на ДРУГУЮ станцию (не эту) и готовы к выпечке
			if itemStatus.StationID != "" && itemStatus.StationID != stationID {
				// Позиция назначена на другую станцию
				if itemStatus.Status == "ready" {
					// Позиция готова к выпечке
					shouldShow = true
					canWorkOnThis = true
				} else if itemStatus.Status == "pending" || itemStatus.Status == "preparing" {
					// Позиция еще готовится на другой станции - видим, но не можем работать
					shouldShow = true
					canWorkOnThis = false
				}
			}
		} else if hasOrderAssembly {
			// Станция "Упаковка" (order_assembly) - видит ВЕСЬ заказ полностью
			// Видит все позиции независимо от статуса
			shouldShow = true
			// Может работать только если ВСЕ позиции готовы (ready или completed)
			allReady := true
			for _, status := range mapping.ItemStatuses {
				if status.Status != "ready" && status.Status != "completed" {
					allReady = false
					break
				}
			}
			if allReady {
				canWorkOnThis = true
			}
		} else {
			// Станция без специальных capabilities - видит только свои позиции
			// Позиции должны быть назначены на эту станцию (текущая станция)
			if itemStatus.StationID == stationID {
				shouldShow = true
				if itemStatus.Status == "pending" || itemStatus.Status == "preparing" {
					canWorkOnThis = true
				}
			}
		}

		if shouldShow {
			visibleItems = append(visibleItems, item)
			if canWorkOnThis {
				canWork = true
			}
		}
	}

	// Если нет видимых позиций, заказ не показываем
	if len(visibleItems) == 0 {
		return nil, false, nil
	}

	// Создаем копию заказа с видимыми позициями
	orderCopy := *order
	orderCopy.Items = visibleItems

	return &orderCopy, canWork, nil
}

// UpdateItemStatus обновляет статус позиции заказа
// При изменении статуса на "ready" проверяет, нужно ли передать заказ следующей станции
func (sas *StationAssignmentService) UpdateItemStatus(orderID string, itemIndex int, newStatus string, stationID string) error {
	if sas.redisUtil == nil {
		return fmt.Errorf("Redis недоступен")
	}

	// Получаем маппинг
	mapping, err := sas.getOrderStationMapping(orderID)
	if err != nil {
		return fmt.Errorf("маппинг не найден: %w", err)
	}

	// Находим статус позиции
	var itemStatus *models.OrderItemStatus
	for i := range mapping.ItemStatuses {
		if mapping.ItemStatuses[i].ItemIndex == itemIndex {
			itemStatus = &mapping.ItemStatuses[i]
			break
		}
	}

	if itemStatus == nil {
		return fmt.Errorf("позиция %d не найдена в заказе", itemIndex)
	}

	// Проверяем, что станция имеет право обновлять этот статус
	if itemStatus.StationID != stationID && itemStatus.Status != "ready" {
		return fmt.Errorf("станция %s не может обновлять позицию, назначенную на станцию %s", 
			stationID, itemStatus.StationID)
	}

	oldStatus := itemStatus.Status
	itemStatus.Status = newStatus
	itemStatus.UpdatedAt = time.Now()

	// Обновляем временные метки
	if newStatus == "preparing" && oldStatus == "pending" {
		itemStatus.StartedAt = time.Now()
	} else if newStatus == "ready" && oldStatus != "ready" {
		itemStatus.CompletedAt = time.Now()
		// Уменьшаем счетчик очереди текущей станции
		if itemStatus.StationID != "" {
			sas.decrementStationQueue(itemStatus.StationID)
		}
		
		// Переходим на следующую станцию из списка StationIDs
		stationIDs, err := sas.parseStationIDs(itemStatus.StationIDs)
		if err == nil && len(stationIDs) > 0 {
			nextIndex := itemStatus.CurrentStationIndex + 1
			if nextIndex < len(stationIDs) {
				// Есть следующая станция - переходим на неё
				nextStationID := stationIDs[nextIndex]
				// Проверяем, что следующая станция существует
				var nextStation models.Station
				if sas.db != nil {
					if err := sas.db.Where("id = ? AND deleted_at IS NULL", nextStationID).First(&nextStation).Error; err == nil {
						// Удаляем из старой станции
						if itemStatus.StationID != "" {
							// Удаляем из списка позиций старой станции
							if assignments, ok := mapping.StationAssignments[itemStatus.StationID]; ok {
								newAssignments := []int{}
								for _, idx := range assignments {
									if idx != itemIndex {
										newAssignments = append(newAssignments, idx)
									}
								}
								mapping.StationAssignments[itemStatus.StationID] = newAssignments
							}
						}
						
						// Добавляем в новую станцию
						itemStatus.StationID = nextStation.ID
						itemStatus.CurrentStationIndex = nextIndex
						itemStatus.Status = "pending" // Сбрасываем статус для новой станции
						mapping.StationAssignments[nextStation.ID] = append(mapping.StationAssignments[nextStation.ID], itemIndex)
						sas.incrementStationQueue(nextStation.ID)
						
						log.Printf("✅ UpdateItemStatus: позиция %d перешла на следующую станцию %s (индекс %d)", 
							itemIndex, nextStation.ID, nextIndex)
					} else {
						log.Printf("⚠️ UpdateItemStatus: следующая станция %s не найдена для позиции %d", nextStationID, itemIndex)
					}
				}
			} else {
				// Это была последняя станция - позиция готова для упаковки
				log.Printf("✅ UpdateItemStatus: позиция %d прошла все станции, готова для упаковки", itemIndex)
			}
		}
	}

	// Сохраняем обновленный маппинг
	if err := sas.saveOrderStationMapping(mapping); err != nil {
		return fmt.Errorf("ошибка сохранения маппинга: %w", err)
	}

	log.Printf("✅ UpdateItemStatus: заказ %s, позиция %d, статус %s → %s (станция: %s)", 
		orderID, itemIndex, oldStatus, newStatus, stationID)

	return nil
}

// saveOrderStationMapping сохраняет маппинг заказа к станциям в Redis
func (sas *StationAssignmentService) saveOrderStationMapping(mapping *models.OrderStationMapping) error {
	mapping.UpdatedAt = time.Now()
	
	// Сериализуем в JSON
	mappingJSON, err := json.Marshal(mapping)
	if err != nil {
		return fmt.Errorf("ошибка сериализации маппинга: %w", err)
	}

	// Сохраняем в Redis
	key := fmt.Sprintf("erp:order:%s:stations", mapping.OrderID)
	return sas.redisUtil.Set(key, string(mappingJSON), 24*time.Hour)
}

// getOrderStationMapping получает маппинг заказа к станциям из Redis
func (sas *StationAssignmentService) getOrderStationMapping(orderID string) (*models.OrderStationMapping, error) {
	key := fmt.Sprintf("erp:order:%s:stations", orderID)
	mappingJSON, err := sas.redisUtil.Get(key)
	if err != nil {
		return nil, fmt.Errorf("маппинг не найден: %w", err)
	}

	var mapping models.OrderStationMapping
	if err := json.Unmarshal([]byte(mappingJSON), &mapping); err != nil {
		return nil, fmt.Errorf("ошибка десериализации маппинга: %w", err)
	}

	return &mapping, nil
}

// parseStationIDs парсит JSON массив StationIDs в []string
func (sas *StationAssignmentService) parseStationIDs(stationIDsJSON string) ([]string, error) {
	if stationIDsJSON == "" {
		return []string{}, nil
	}
	var stationIDs []string
	if err := json.Unmarshal([]byte(stationIDsJSON), &stationIDs); err != nil {
		return nil, err
	}
	return stationIDs, nil
}

// containsStation проверяет, содержится ли станция в списке
func (sas *StationAssignmentService) containsStation(stationIDs []string, stationID string) bool {
	for _, id := range stationIDs {
		if id == stationID {
			return true
		}
	}
	return false
}

// incrementStationQueue увеличивает счетчик очереди станции
func (sas *StationAssignmentService) incrementStationQueue(stationID string) {
	key := fmt.Sprintf("erp:station:%s:queue", stationID)
	_, err := sas.redisUtil.Increment(key)
	if err != nil {
		log.Printf("⚠️ incrementStationQueue: ошибка увеличения очереди станции %s: %v", stationID, err)
	}
}

// decrementStationQueue уменьшает счетчик очереди станции
func (sas *StationAssignmentService) decrementStationQueue(stationID string) {
	key := fmt.Sprintf("erp:station:%s:queue", stationID)
	_, err := sas.redisUtil.Decrement(key)
	if err != nil {
		log.Printf("⚠️ decrementStationQueue: ошибка уменьшения очереди станции %s: %v", stationID, err)
	}
}

// contains проверяет, содержит ли слайс элемент
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}


