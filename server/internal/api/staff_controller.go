package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
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
			// Ключ сессии: erp:staff:{user_id}:session
			sessionKey := fmt.Sprintf("erp:staff:%s:session", s.UserID)
			sessionKeys = append(sessionKeys, sessionKey)
			staffIDMap[s.UserID] = i
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
				stationKey := fmt.Sprintf("erp:staff:%s:station", s.UserID)
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
		Name     string  `json:"name"` // Опционально, для будущего использования (Customer профиль)
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
	// Name больше не обязателен, так как базовая информация теперь в User (только Phone)

	if req.Phone == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Phone is required",
		})
		return
	}

	// Проверка уникальности телефона в таблице users
	var existingUser models.User
	if err := sc.db.Where("phone = ?", req.Phone).First(&existingUser).Error; err == nil {
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

	// Определяем роль User на основе RoleName
	userRole := models.RoleKitchenStaff // По умолчанию
	switch strings.ToLower(req.Role) {
	case "courier":
		userRole = models.RoleCourier
	case "technologist":
		userRole = models.RoleTechnologist
	case "admin":
		userRole = models.RoleAdmin
	default:
		userRole = models.RoleKitchenStaff
	}

	// Сначала создаем User (базовая информация)
	user := models.User{
		Phone:  req.Phone,
		Role:   userRole,
		Status: models.UserStatusActive,
	}

	if err := sc.db.Create(&user).Error; err != nil {
		log.Printf("❌ CreateStaff: ошибка создания пользователя в БД: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to create user",
		})
		return
	}

	// Затем создаем Staff профиль (рабочая информация)
	staff := models.Staff{
		UserID:          user.ID, // Связь с созданным User
		RoleName:        req.Role, // Должность: "Cook", "Courier", "Manager"
		Status:          models.StatusActive,
		BranchID:        finalBranchID,
		PerformanceScore: 0.0,
	}

	if err := sc.db.Create(&staff).Error; err != nil {
		log.Printf("❌ CreateStaff: ошибка создания профиля сотрудника в БД: %v", err)
		// Откатываем создание User
		sc.db.Delete(&user)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to create staff profile",
		})
		return
	}

	// Загружаем User для ToMap()
	staff.User = &user

	log.Printf("✅ Создан сотрудник: %s (UserID: %s)", req.Phone, staff.UserID)

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

	// Получаем сотрудника по user_id
	var staff models.Staff
	if err := sc.db.Where("user_id = ? AND deleted_at IS NULL", id).First(&staff).Error; err != nil {
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

	// Загружаем User для логирования
	sc.db.Preload("User").First(&staff, "user_id = ?", id)
	staffName := "Unknown"
	if staff.User != nil {
		staffName = staff.User.Phone
	}
	log.Printf("✅ Обновлен статус сотрудника %s: %s -> %s", staffName, staff.Status, req.Status)

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

	// Получаем сотрудника с User
	var staff models.Staff
	if err := sc.db.Preload("User").Where("user_id = ? AND deleted_at IS NULL", id).First(&staff).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "Staff not found",
		})
		return
	}

	// Обновляем User (имя и телефон теперь в User)
	userUpdates := map[string]interface{}{}
	if req.Phone != "" && staff.User != nil && req.Phone != staff.User.Phone {
		// Проверка уникальности телефона
		var existingUser models.User
		if err := sc.db.Where("phone = ? AND id != ?", req.Phone, staff.UserID).First(&existingUser).Error; err == nil {
			c.JSON(http.StatusConflict, gin.H{
				"error": "Phone number already exists",
			})
			return
		}
		userUpdates["phone"] = req.Phone
	}

	// Обновляем Staff (должность)
	staffUpdates := map[string]interface{}{}
	if req.Role != "" {
		staffUpdates["role_name"] = req.Role
		staff.RoleName = req.Role
	}

	// Выполняем обновления
	if len(userUpdates) > 0 && staff.User != nil {
		if err := sc.db.Model(staff.User).Updates(userUpdates).Error; err != nil {
			log.Printf("❌ UpdateStaff: ошибка обновления пользователя: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "Failed to update user",
			})
			return
		}
	}

	if len(staffUpdates) > 0 {
		if err := sc.db.Model(&staff).Updates(staffUpdates).Error; err != nil {
			log.Printf("❌ UpdateStaff: ошибка обновления сотрудника: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "Failed to update staff",
			})
			return
		}
	}

	// Перезагружаем User для ToMap()
	sc.db.Preload("User").First(&staff, "user_id = ?", id)

	staffName := "Unknown"
	if staff.User != nil {
		staffName = staff.User.Phone
	}
	log.Printf("✅ Обновлен сотрудник: %s (UserID: %s)", staffName, staff.UserID)

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
	if err := sc.db.Where("user_id = ? AND deleted_at IS NULL", id).First(&staff).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "Staff not found",
		})
		return
	}

	// Soft delete Staff
	if err := sc.db.Delete(&staff).Error; err != nil {
		log.Printf("❌ DeleteStaff: ошибка удаления сотрудника: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to delete staff",
		})
		return
	}

	// Также удаляем User (CASCADE удалит Staff автоматически, но лучше явно)
	var user models.User
	if err := sc.db.Where("id = ?", id).First(&user).Error; err == nil {
		sc.db.Delete(&user)
	}

	log.Printf("✅ Удален сотрудник (UserID: %s)", id)

	c.JSON(http.StatusOK, gin.H{
		"message": "Staff deleted successfully",
		"id":      id,
	})
}

// PinCodeAuthRequest представляет запрос на авторизацию по PIN-коду
type PinCodeAuthRequest struct {
	PinCode string `json:"pin_code" binding:"required"`
}

// PinCodeAuthResponse представляет ответ на авторизацию по PIN-коду
type PinCodeAuthResponse struct {
	Success     bool   `json:"success"`
	SessionToken string `json:"session_token,omitempty"`
	StaffID     string `json:"staff_id,omitempty"`
	UserID      string `json:"user_id,omitempty"`
	Role        string `json:"role,omitempty"`
	RoleName    string `json:"role_name,omitempty"`
	BranchID    string `json:"branch_id,omitempty"`
	UserName    string `json:"user_name,omitempty"` // Имя сотрудника
	Message     string `json:"message"`
}

// PinCodeAuth обрабатывает авторизацию по PIN-коду для KDS
// POST /api/v1/erp/staff/pin-auth
func (sc *StaffController) PinCodeAuth(c *gin.Context) {
	if sc.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success": false,
			"message": "Database not available",
		})
		return
	}

	if sc.redisUtil == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success": false,
			"message": "Redis not available",
		})
		return
	}

	var req PinCodeAuthRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "Invalid request body",
			"details": err.Error(),
		})
		return
	}

	// Ищем сотрудника по PIN-коду (используем phone как PIN для простоты)
	// В будущем можно добавить отдельное поле pin_code в Staff
	var staff models.Staff
	if err := sc.db.Preload("User").Joins("JOIN users ON users.id = staff.user_id").Where("staff.deleted_at IS NULL AND staff.status = ?", "Active").Where("users.phone = ? AND users.status = ?", req.PinCode, models.UserStatusActive).First(&staff).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"message": "Invalid PIN code",
			})
			return
		}
		log.Printf("❌ PinCodeAuth: ошибка поиска сотрудника: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "Error checking credentials",
		})
		return
	}

	// Проверяем, что User активен
	if staff.User == nil || staff.User.Status != models.UserStatusActive {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "User account is not active",
		})
		return
	}

	// Генерируем session token
	sessionToken := fmt.Sprintf("kds_session_%s_%d", staff.UserID, time.Now().Unix())

	// Сохраняем сессию в Redis (24 часа)
	sessionKey := fmt.Sprintf("erp:staff:%s:session", staff.UserID)
	sessionData := map[string]interface{}{
		"staff_id":  staff.UserID,
		"user_id":   staff.UserID,
		"role":      string(staff.User.Role),
		"role_name": staff.RoleName,
		"branch_id": staff.BranchID,
		"token":     sessionToken,
	}
	
	// Сохраняем как JSON в Redis
	sessionJSON, _ := json.Marshal(sessionData)
	if err := sc.redisUtil.Set(sessionKey, string(sessionJSON), 24*time.Hour); err != nil {
		log.Printf("⚠️ PinCodeAuth: ошибка сохранения сессии в Redis: %v", err)
	}

	// Также сохраняем маппинг token -> staff_id для быстрого поиска
	tokenKey := fmt.Sprintf("erp:kds:token:%s", sessionToken)
	sc.redisUtil.Set(tokenKey, staff.UserID, 24*time.Hour)

	log.Printf("✅ PinCodeAuth: успешная авторизация для сотрудника %s (UserID: %s, Role: %s)", 
		staff.RoleName, staff.UserID, staff.User.Role)

	// Получаем имя пользователя (если есть)
	userName := ""
	if staff.User != nil && staff.User.Name != nil {
		userName = *staff.User.Name
	} else if staff.User != nil {
		// Если имени нет, используем phone как fallback
		userName = staff.User.Phone
	}

	c.JSON(http.StatusOK, PinCodeAuthResponse{
		Success:     true,
		SessionToken: sessionToken,
		StaffID:     staff.UserID,
		UserID:      staff.UserID,
		Role:        string(staff.User.Role),
		RoleName:    staff.RoleName,
		BranchID:    staff.BranchID,
		UserName:    userName,
		Message:     "Authentication successful",
	})
}

// BindStation привязывает станцию к сессии сотрудника
// POST /api/v1/erp/staff/bind-station
func (sc *StaffController) BindStation(c *gin.Context) {
	if sc.db == nil || sc.redisUtil == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success": false,
			"message": "Database or Redis not available",
		})
		return
	}

	var req struct {
		SessionToken string `json:"session_token" binding:"required"`
		StationID    string `json:"station_id" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "Invalid request body",
			"details": err.Error(),
		})
		return
	}

	// Проверяем токен и получаем staff_id
	tokenKey := fmt.Sprintf("erp:kds:token:%s", req.SessionToken)
	staffID, err := sc.redisUtil.Get(tokenKey)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "Invalid session token",
		})
		return
	}

	// Проверяем, что станция существует
	var station models.Station
	if err := sc.db.Where("id = ? AND deleted_at IS NULL", req.StationID).First(&station).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"message": "Station not found",
		})
		return
	}

	// Сохраняем привязку станции к сотруднику
	stationKey := fmt.Sprintf("erp:staff:%s:station", staffID)
	sc.redisUtil.Set(stationKey, req.StationID, 24*time.Hour)

	// Также сохраняем обратную привязку: станция -> список сотрудников
	stationSessionKey := fmt.Sprintf("erp:station:%s:session", req.StationID)
	sc.redisUtil.Set(stationSessionKey, staffID, 24*time.Hour)

	log.Printf("✅ BindStation: сотрудник %s привязан к станции %s (%s)", 
		staffID, req.StationID, station.Name)

	c.JSON(http.StatusOK, gin.H{
		"success":     true,
		"message":     "Station bound successfully",
		"station_id":  req.StationID,
		"station_name": station.Name,
	})
}

// SendPulse отправляет "пульс" для отслеживания онлайн статуса станции
// POST /api/v1/erp/staff/pulse
func (sc *StaffController) SendPulse(c *gin.Context) {
	if sc.redisUtil == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success": false,
			"message": "Redis not available",
		})
		return
	}

	var req struct {
		SessionToken string `json:"session_token" binding:"required"`
		StationID    string `json:"station_id" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "Invalid request body",
		})
		return
	}

	// Проверяем токен
	tokenKey := fmt.Sprintf("erp:kds:token:%s", req.SessionToken)
	staffID, err := sc.redisUtil.Get(tokenKey)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "Invalid session token",
		})
		return
	}

	// Обновляем время последнего пульса для станции
	pulseKey := fmt.Sprintf("erp:station:%s:pulse", req.StationID)
	sc.redisUtil.Set(pulseKey, time.Now().Unix(), 30*time.Second) // TTL 30 секунд

	// Обновляем сессию сотрудника
	sessionKey := fmt.Sprintf("erp:staff:%s:session", staffID)
	sc.redisUtil.Expire(sessionKey, 24*time.Hour)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Pulse sent",
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

