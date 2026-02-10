package api

import (
	"fmt"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"zephyrvpn/server/internal/models"
	"zephyrvpn/server/internal/services"
	"zephyrvpn/server/internal/utils"
)

type StationsController struct {
	db        *gorm.DB
	redisUtil *utils.RedisClient
}

func NewStationsController(db *gorm.DB, redisUtil *utils.RedisClient) *StationsController {
	return &StationsController{
		db:        db,
		redisUtil: redisUtil,
	}
}

// GetStations получает список всех активных станций из PostgreSQL
// GET /api/v1/erp/stations
func (sc *StationsController) GetStations(c *gin.Context) {
	if sc.db == nil {
		c.JSON(http.StatusOK, gin.H{
			"stations": []map[string]interface{}{},
			"count":    0,
		})
		return
	}

	var dbStations []models.Station
	if err := sc.db.Where("deleted_at IS NULL").Find(&dbStations).Error; err != nil {
		log.Printf("❌ GetStations: ошибка получения станций из БД: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to fetch stations",
		})
		return
	}

	// Оптимизация: используем Redis Pipeline для батчевого получения данных
	// Вместо N+1 запросов делаем один батч-запрос
	stations := make([]map[string]interface{}, 0, len(dbStations))
	
	if sc.redisUtil != nil && len(dbStations) > 0 {
		// Подготавливаем ключи для батчевого запроса
		queueKeys := make([]string, 0, len(dbStations))
		sessionKeys := make([]string, 0, len(dbStations))
		
		for _, dbStation := range dbStations {
			queueKeys = append(queueKeys, fmt.Sprintf("erp:station:%s:queue", dbStation.ID))
			sessionKeys = append(sessionKeys, fmt.Sprintf("erp:station:%s:session", dbStation.ID))
		}
		
		// Используем Pipeline для батчевого выполнения
		pipe := sc.redisUtil.Pipeline()
		ctx := sc.redisUtil.Context()
		
		// Добавляем команды Get для queue counts
		queueCmds := make([]*redis.StringCmd, len(queueKeys))
		for i, key := range queueKeys {
			queueCmds[i] = pipe.Get(ctx, key)
		}
		
		// Добавляем команды Exists для session checks (Exists возвращает IntCmd)
		sessionCmds := make([]*redis.IntCmd, len(sessionKeys))
		for i, key := range sessionKeys {
			sessionCmds[i] = pipe.Exists(ctx, key)
		}
		
		// Выполняем все команды одним батчем
		_, err := pipe.Exec(ctx)
		if err != nil && err != redis.Nil {
			log.Printf("⚠️ GetStations: ошибка Redis Pipeline: %v", err)
		}
		
		// Обрабатываем результаты
		for i, dbStation := range dbStations {
			stationMap := dbStation.ToMap()
			
			// Получаем queue_count из результата Pipeline
			if queueVal, err := queueCmds[i].Result(); err == nil {
				var count int
				if _, err := fmt.Sscanf(queueVal, "%d", &count); err == nil {
					stationMap["queue_count"] = count
				}
			}
			
			// Получаем статус online из результата Pipeline
			if hasSession, err := sessionCmds[i].Result(); err == nil && hasSession > 0 {
				stationMap["status"] = "online"
			} else {
				stationMap["status"] = "offline"
			}
			
			stations = append(stations, stationMap)
		}
	} else {
		// Если Redis недоступен, используем данные только из БД
		for _, dbStation := range dbStations {
			stationMap := dbStation.ToMap()
			if dbStation.Status == "" {
				stationMap["status"] = "offline"
			}
			stations = append(stations, stationMap)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"stations": stations,
		"count":    len(stations),
	})
}

// GetCapabilities получает список доступных capabilities и категорий
// GET /api/v1/erp/stations/capabilities
func (sc *StationsController) GetCapabilities(c *gin.Context) {
	capabilities := []map[string]interface{}{
		{
			"key":         "view_composition",
			"label":       "Подготовка/Начинка",
			"description": "Работа с ингредиентами и начинкой",
			"icon":        "Utensils",
		},
		{
			"key":         "view_oven_queue",
			"label":       "Выпекание/Печь",
			"description": "Управление очередью печи",
			"icon":        "Flame",
		},
		{
			"key":         "order_assembly",
			"label":       "Сборка/Комплектация",
			"description": "Сборка и упаковка заказов",
			"icon":        "Package",
		},
	}

	categories := []map[string]interface{}{
		{"id": "pizza", "label": "Пицца"},
		{"id": "appetizers", "label": "Закуски"},
		{"id": "drinks", "label": "Напитки"},
		{"id": "consumables", "label": "Расходники"}, // Салфетки, ложки и т.д.
	}

	c.JSON(http.StatusOK, gin.H{
		"capabilities": capabilities,
		"categories":   categories,
	})
}

// CreateStation создает новую станцию в PostgreSQL
// POST /api/v1/erp/stations
func (sc *StationsController) CreateStation(c *gin.Context) {
	if sc.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "Database not available",
		})
		return
	}

	var req struct {
		Name     string                 `json:"name" binding:"required"`
		Config   map[string]interface{} `json:"config" binding:"required"`
		BranchID string                 `json:"branch_id"` // Опционально
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Invalid request body",
			"details": err.Error(),
		})
		return
	}

	// Валидация: имя не должно быть пустым
	if req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Station name is required",
		})
		return
	}

	// Валидация: категории должны быть выбраны
	categories, ok := req.Config["categories"].([]interface{})
	if !ok || len(categories) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "At least one category must be selected",
		})
		return
	}

	// Извлекаем данные из config
	icon := "ChefHat"
	if iconVal, ok := req.Config["icon"].(string); ok {
		icon = iconVal
	}

	capabilities := []string{}
	if caps, ok := req.Config["capabilities"].([]interface{}); ok {
		for _, cap := range caps {
			if capStr, ok := cap.(string); ok {
				capabilities = append(capabilities, capStr)
			}
		}
	}

	categoryStrings := []string{}
	for _, cat := range categories {
		if catStr, ok := cat.(string); ok {
			categoryStrings = append(categoryStrings, catStr)
		}
	}

	// Используем дефолтный branch_id если не указан
	branchID := req.BranchID
	if branchID == "" {
		var branch models.Branch
		if err := sc.db.Where("is_active = ?", true).First(&branch).Error; err == nil {
			branchID = branch.ID
		}
		// Если не найден активный филиал, оставляем пустым - будет ошибка валидации
	}

	// Создаем станцию в БД
	station := models.Station{
		ID:         uuid.New().String(), // Генерируем UUID в коде
		Name:       req.Name,
		Icon:       icon,
		Status:     "offline",
		QueueCount: 0,
		BranchID:   branchID,
		Config: models.StationConfig{
			Icon:          icon,
			Capabilities:  capabilities,
			Categories:    categoryStrings,
			TriggerStatus: getString(req.Config, "trigger_status"),
			TargetStatus:  getString(req.Config, "target_status"),
		},
	}

	if err := sc.db.Create(&station).Error; err != nil {
		log.Printf("❌ CreateStation: ошибка создания станции в БД: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to create station",
		})
		return
	}

	log.Printf("✅ Создана станция: %s (ID: %s)", req.Name, station.ID)

	c.JSON(http.StatusCreated, station.ToMap())
}

// getString безопасно извлекает строку из map
func getString(m map[string]interface{}, key string) string {
	if val, ok := m[key].(string); ok {
		return val
	}
	return ""
}

// UpdateStation обновляет станцию в PostgreSQL
// PUT /api/v1/erp/stations/:id
func (sc *StationsController) UpdateStation(c *gin.Context) {
	if sc.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "Database not available",
		})
		return
	}

	id := c.Param("id")

	// Получаем существующую станцию из БД
	var station models.Station
	if err := sc.db.Where("id = ? AND deleted_at IS NULL", id).First(&station).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "Station not found",
		})
		return
	}

	// Обновляем данные из запроса
	var req struct {
		Name   string                 `json:"name"`
		Config map[string]interface{} `json:"config"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Invalid request body",
			"details": err.Error(),
		})
		return
	}

	// Валидация: имя не должно быть пустым (если передано)
	if req.Name != "" {
		station.Name = req.Name
	} else if req.Name == "" && req.Config == nil {
		// Если имя пустое и нет config, это ошибка
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Station name cannot be empty",
		})
		return
	}

	// Валидация: категории должны быть выбраны
	if req.Config != nil {
		if categories, ok := req.Config["categories"].([]interface{}); ok {
			if len(categories) == 0 {
				c.JSON(http.StatusBadRequest, gin.H{
					"error": "At least one category must be selected",
				})
				return
			}
		}

		// Обновляем config
		if icon, ok := req.Config["icon"].(string); ok {
			station.Icon = icon
			station.Config.Icon = icon
		}

		if capabilities, ok := req.Config["capabilities"].([]interface{}); ok {
			caps := []string{}
			for _, cap := range capabilities {
				if capStr, ok := cap.(string); ok {
					caps = append(caps, capStr)
				}
			}
			station.Config.Capabilities = caps
		}

		if categories, ok := req.Config["categories"].([]interface{}); ok {
			cats := []string{}
			for _, cat := range categories {
				if catStr, ok := cat.(string); ok {
					cats = append(cats, catStr)
				}
			}
			station.Config.Categories = cats
		}

		if triggerStatus, ok := req.Config["trigger_status"].(string); ok {
			station.Config.TriggerStatus = triggerStatus
		}

		if targetStatus, ok := req.Config["target_status"].(string); ok {
			station.Config.TargetStatus = targetStatus
		}
	}

	// Сохраняем обновленную станцию в БД
	// Используем Updates() для частичного обновления только измененных полей
	updates := map[string]interface{}{
		"name":   station.Name,
		"icon":   station.Icon,
		"config": station.Config,
	}
	
	if err := sc.db.Model(&station).Updates(updates).Error; err != nil {
		log.Printf("❌ UpdateStation: ошибка обновления станции в БД: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to update station",
		})
		return
	}

	log.Printf("✅ Обновлена станция: %s (ID: %s)", station.Name, id)

	c.JSON(http.StatusOK, station.ToMap())
}

// DeleteStation удаляет станцию из PostgreSQL (soft delete)
// DELETE /api/v1/erp/stations/:id
func (sc *StationsController) DeleteStation(c *gin.Context) {
	if sc.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "Database not available",
		})
		return
	}

	id := c.Param("id")

	// Проверяем существование станции
	var station models.Station
	if err := sc.db.Where("id = ? AND deleted_at IS NULL", id).First(&station).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "Station not found",
		})
		return
	}

	// Soft delete (GORM автоматически установит deleted_at)
	if err := sc.db.Delete(&station).Error; err != nil {
		log.Printf("❌ DeleteStation: ошибка удаления станции из БД: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to delete station",
		})
		return
	}

	log.Printf("✅ Удалена станция (ID: %s)", id)

	c.JSON(http.StatusOK, gin.H{
		"message": "Station deleted successfully",
		"id":      id,
	})
}

// UpdateOrderItemStatus обновляет статус позиции заказа на станции
// PUT /api/v1/erp/stations/:id/orders/:order_id/items/:item_index
func (sc *StationsController) UpdateOrderItemStatus(c *gin.Context) {
	if sc.db == nil || sc.redisUtil == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "Database or Redis not available",
		})
		return
	}

	stationID := c.Param("id") // Используем :id вместо :station_id для совместимости с другими роутами
	orderID := c.Param("order_id")
	itemIndexStr := c.Param("item_index")

	// Парсим item_index
	var itemIndex int
	if _, err := fmt.Sscanf(itemIndexStr, "%d", &itemIndex); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid item_index",
		})
		return
	}

	// Получаем новый статус из тела запроса
	var req struct {
		Status string `json:"status" binding:"required"` // "preparing", "ready", "completed"
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Invalid request body",
			"details": err.Error(),
		})
		return
	}

	// Валидация статуса
	validStatuses := map[string]bool{
		"preparing": true,
		"ready":     true,
		"completed": true,
	}
	if !validStatuses[req.Status] {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("Invalid status. Allowed: preparing, ready, completed"),
		})
		return
	}

	// Создаем сервис для обновления статуса
	stationAssignService := services.NewStationAssignmentService(sc.db, sc.redisUtil)

	// Обновляем статус позиции
	if err := stationAssignService.UpdateItemStatus(orderID, itemIndex, req.Status, stationID); err != nil {
		log.Printf("❌ UpdateOrderItemStatus: ошибка обновления статуса: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to update item status",
			"details": err.Error(),
		})
		return
	}

	log.Printf("✅ UpdateOrderItemStatus: заказ %s, позиция %d, статус %s (станция: %s)", 
		orderID, itemIndex, req.Status, stationID)

	c.JSON(http.StatusOK, gin.H{
		"message":    "Item status updated successfully",
		"order_id":   orderID,
		"item_index": itemIndex,
		"status":     req.Status,
		"station_id": stationID,
	})
}


