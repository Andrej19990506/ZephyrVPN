package api

import (
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"zephyrvpn/server/internal/models"
	"zephyrvpn/server/internal/utils"
)

type StaffController struct {
	db        *gorm.DB
	redisUtil *utils.RedisClient
}

func NewStaffController(db *gorm.DB, redisUtil *utils.RedisClient) *StaffController {
	return &StaffController{
		db:        db,
		redisUtil: redisUtil,
	}
}

// GetStaff получает список сотрудников с фильтрацией по статусу
// GET /api/v1/erp/staff?status=Active
func (sc *StaffController) GetStaff(c *gin.Context) {
	if sc.db == nil {
		c.JSON(http.StatusOK, gin.H{
			"staff": []map[string]interface{}{},
			"count": 0,
		})
		return
	}

	status := c.Query("status")     // Active, Reserve, Blacklisted
	role := c.Query("role")         // Опциональный фильтр по роли
	search := c.Query("search")     // Поиск по имени/телефону
	branchID := c.Query("branch_id") // Фильтр по филиалу

	query := sc.db.Where("deleted_at IS NULL")

	// Фильтр по статусу
	if status != "" {
		query = query.Where("status = ?", status)
	} else {
		// По умолчанию показываем только активных
		query = query.Where("status = ?", "Active")
	}

	// Фильтр по роли (используем role_name вместо role)
	if role != "" {
		query = query.Where("role_name = ?", role)
	}

	// Фильтр по филиалу
	if branchID != "" {
		query = query.Where("branch_id = ?", branchID)
	}

	// Поиск по имени или телефону
	if search != "" {
		searchPattern := "%" + strings.ToLower(search) + "%"
		query = query.Where("LOWER(name) LIKE ? OR phone LIKE ?", searchPattern, searchPattern)
	}

	var dbStaff []models.Staff
	if err := query.Find(&dbStaff).Error; err != nil {
		log.Printf("❌ GetStaff: ошибка получения сотрудников из БД: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to fetch staff",
		})
		return
	}

	// Оптимизация: используем Redis Pipeline для батчевого получения статусов онлайн
	staff := make([]map[string]interface{}, 0, len(dbStaff))

	if sc.redisUtil != nil && len(dbStaff) > 0 {
		// Подготавливаем ключи для проверки активных сессий
		sessionKeys := make([]string, 0, len(dbStaff))
		staffIDMap := make(map[string]int) // Индекс сотрудника по ID

		for i, s := range dbStaff {
			// Ключ сессии: erp:staff:{id}:session или erp:staff:{phone}:session
			sessionKey := fmt.Sprintf("erp:staff:%s:session", s.ID)
			sessionKeys = append(sessionKeys, sessionKey)
			staffIDMap[s.ID] = i
		}

		// Используем Pipeline для батчевого выполнения
		pipe := sc.redisUtil.Pipeline()
		ctx := sc.redisUtil.Context()

		// Добавляем команды Exists для проверки сессий
		sessionCmds := make([]*redis.IntCmd, len(sessionKeys))
		for i, key := range sessionKeys {
			sessionCmds[i] = pipe.Exists(ctx, key)
		}

		// Выполняем все команды одним батчем
		_, err := pipe.Exec(ctx)
		if err != nil && err != redis.Nil {
			log.Printf("⚠️ GetStaff: ошибка Redis Pipeline: %v", err)
		}

		// Обрабатываем результаты
		for i, s := range dbStaff {
			staffMap := s.ToMap()

			// Проверяем статус онлайн через Redis
			if hasSession, err := sessionCmds[i].Result(); err == nil && hasSession > 0 {
				staffMap["is_online"] = true
				// Получаем информацию о станции, если сотрудник на станции
				stationKey := fmt.Sprintf("erp:staff:%s:station", s.ID)
				if stationID, err := sc.redisUtil.Get(stationKey); err == nil {
					staffMap["current_station_id"] = stationID
				}
			} else {
				staffMap["is_online"] = false
			}

			staff = append(staff, staffMap)
		}
	} else {
		// Если Redis недоступен, используем данные только из БД
		for _, s := range dbStaff {
			staffMap := s.ToMap()
			staffMap["is_online"] = false
			staff = append(staff, staffMap)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"staff": staff,
		"count": len(staff),
	})
}

// CreateStaff создает нового сотрудника
// POST /api/v1/erp/staff
func (sc *StaffController) CreateStaff(c *gin.Context) {
	if sc.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "Database not available",
		})
		return
	}

	var req struct {
		Name     string  `json:"name" binding:"required"`
		Phone    string  `json:"phone" binding:"required"`
		Role     string  `json:"role" binding:"required"`
		BranchID string  `json:"branch_id"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Invalid request body",
			"details": err.Error(),
		})
		return
	}

	// Валидация
	if req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Name is required",
		})
		return
	}

	if req.Phone == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Phone is required",
		})
		return
	}

	// Проверка уникальности телефона
	var existingStaff models.Staff
	if err := sc.db.Where("phone = ? AND deleted_at IS NULL", req.Phone).First(&existingStaff).Error; err == nil {
		c.JSON(http.StatusConflict, gin.H{
			"error": "Phone number already exists",
		})
		return
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

	// Валидация: branch_id обязателен
	if req.BranchID == "" && branchID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Branch ID is required",
		})
		return
	}

	// Используем переданный branch_id или дефолтный
	finalBranchID := req.BranchID
	if finalBranchID == "" {
		finalBranchID = branchID
	}

	// Создаем сотрудника
	staff := models.Staff{
		ID:              uuid.New().String(),
		Name:            req.Name,
		Phone:           req.Phone,
		RoleName:        req.Role, // Используем RoleName вместо Role enum
		Status:          models.StatusActive,
		BranchID:        finalBranchID,
		PerformanceScore: 0.0,
	}

	if err := sc.db.Create(&staff).Error; err != nil {
		log.Printf("❌ CreateStaff: ошибка создания сотрудника в БД: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to create staff",
		})
		return
	}

	log.Printf("✅ Создан сотрудник: %s (ID: %s)", req.Name, staff.ID)

	c.JSON(http.StatusCreated, staff.ToMap())
}

// UpdateStaffStatus обновляет статус сотрудника с валидацией State Machine
// PUT /api/v1/erp/staff/:id/status
func (sc *StaffController) UpdateStaffStatus(c *gin.Context) {
	if sc.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "Database not available",
		})
		return
	}

	id := c.Param("id")

	var req struct {
		Status string `json:"status" binding:"required"`
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
		"Active":     true,
		"Reserve":    true,
		"Blacklisted": true,
	}
	if !validStatuses[req.Status] {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid status. Must be: Active, Reserve, or Blacklisted",
		})
		return
	}

	// Получаем сотрудника
	var staff models.Staff
	if err := sc.db.Where("id = ? AND deleted_at IS NULL", id).First(&staff).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "Staff not found",
		})
		return
	}

	// Строгая валидация State Machine
	newStatus := models.StaffStatus(req.Status)
	if !staff.CanTransitionTo(newStatus) {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("Status transition from %s to %s is not allowed. Blacklisted employees cannot be reactivated.", staff.Status, req.Status),
		})
		return
	}

	// Обновляем статус
	staff.Status = newStatus
	if err := sc.db.Save(&staff).Error; err != nil {
		log.Printf("❌ UpdateStaffStatus: ошибка обновления статуса: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to update status",
		})
		return
	}

	log.Printf("✅ Обновлен статус сотрудника %s: %s -> %s", staff.Name, staff.Status, req.Status)

	c.JSON(http.StatusOK, staff.ToMap())
}

// UpdateStaff обновляет данные сотрудника
// PUT /api/v1/erp/staff/:id
func (sc *StaffController) UpdateStaff(c *gin.Context) {
	if sc.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "Database not available",
		})
		return
	}

	id := c.Param("id")

	var req struct {
		Name  string `json:"name"`
		Phone string `json:"phone"`
		Role  string `json:"role"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Invalid request body",
			"details": err.Error(),
		})
		return
	}

	// Получаем сотрудника
	var staff models.Staff
	if err := sc.db.Where("id = ? AND deleted_at IS NULL", id).First(&staff).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "Staff not found",
		})
		return
	}

	// Проверка уникальности телефона (если изменяется)
	if req.Phone != "" && req.Phone != staff.Phone {
		var existingStaff models.Staff
		if err := sc.db.Where("phone = ? AND id != ? AND deleted_at IS NULL", req.Phone, id).First(&existingStaff).Error; err == nil {
			c.JSON(http.StatusConflict, gin.H{
				"error": "Phone number already exists",
			})
			return
		}
		staff.Phone = req.Phone
	}

	// Обновляем поля
	updates := map[string]interface{}{}
	if req.Name != "" {
		updates["name"] = req.Name
		staff.Name = req.Name
	}
	if req.Phone != "" {
		updates["phone"] = req.Phone
	}
	if req.Role != "" {
		updates["role_name"] = req.Role // Используем role_name вместо role
		staff.RoleName = req.Role
	}

	if len(updates) > 0 {
		if err := sc.db.Model(&staff).Updates(updates).Error; err != nil {
			log.Printf("❌ UpdateStaff: ошибка обновления сотрудника: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "Failed to update staff",
			})
			return
		}
	}

	log.Printf("✅ Обновлен сотрудник: %s (ID: %s)", staff.Name, id)

	c.JSON(http.StatusOK, staff.ToMap())
}

// DeleteStaff удаляет сотрудника (soft delete)
// DELETE /api/v1/erp/staff/:id
func (sc *StaffController) DeleteStaff(c *gin.Context) {
	if sc.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "Database not available",
		})
		return
	}

	id := c.Param("id")

	var staff models.Staff
	if err := sc.db.Where("id = ? AND deleted_at IS NULL", id).First(&staff).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "Staff not found",
		})
		return
	}

	// Soft delete
	if err := sc.db.Delete(&staff).Error; err != nil {
		log.Printf("❌ DeleteStaff: ошибка удаления сотрудника: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to delete staff",
		})
		return
	}

	log.Printf("✅ Удален сотрудник (ID: %s)", id)

	c.JSON(http.StatusOK, gin.H{
		"message": "Staff deleted successfully",
		"id":      id,
	})
}

// GetAvailableRoles получает список доступных ролей из БД
// GET /api/v1/erp/staff/roles
func (sc *StaffController) GetAvailableRoles(c *gin.Context) {
	if sc.db == nil {
		// Возвращаем дефолтные роли если БД недоступна
		defaultRoles := []map[string]interface{}{
			{"id": "1", "name": "Cook", "label": "Повар", "description": "Приготовление блюд"},
			{"id": "2", "name": "Courier", "label": "Курьер", "description": "Доставка заказов"},
			{"id": "3", "name": "Admin", "label": "Администратор", "description": "Управление системой"},
			{"id": "4", "name": "Manager", "label": "Менеджер", "description": "Управление персоналом и операциями"},
		}
		c.JSON(http.StatusOK, gin.H{
			"roles": defaultRoles,
		})
		return
	}

	var roles []models.Role
	if err := sc.db.Where("is_active = ?", true).Find(&roles).Error; err != nil {
		log.Printf("❌ GetAvailableRoles: ошибка получения ролей: %v", err)
		// Возвращаем дефолтные роли при ошибке
		defaultRoles := []map[string]interface{}{
			{"id": "1", "name": "Cook", "label": "Повар", "description": "Приготовление блюд"},
			{"id": "2", "name": "Courier", "label": "Курьер", "description": "Доставка заказов"},
			{"id": "3", "name": "Admin", "label": "Администратор", "description": "Управление системой"},
			{"id": "4", "name": "Manager", "label": "Менеджер", "description": "Управление персоналом и операциями"},
		}
		c.JSON(http.StatusOK, gin.H{
			"roles": defaultRoles,
		})
		return
	}

	rolesList := make([]map[string]interface{}, 0, len(roles))
	for _, role := range roles {
		rolesList = append(rolesList, map[string]interface{}{
			"id":          role.ID,
			"name":        role.Name,
			"label":       role.Label,
			"description": role.Description,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"roles": rolesList,
	})
}

