package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"zephyrvpn/server/internal/models"
	"gorm.io/gorm"
)

// AuthController управляет API endpoints для авторизации
type AuthController struct {
	db *gorm.DB
}

// NewAuthController создает новый контроллер авторизации
func NewAuthController(db *gorm.DB) *AuthController {
	return &AuthController{db: db}
}

// SuperAdminLoginRequest представляет запрос на вход супер-админа
type SuperAdminLoginRequest struct {
	Username      string `json:"username" binding:"required"`
	Password      string `json:"password" binding:"required"`
	LegalEntityID string `json:"legal_entity_id" binding:"required"`
}

// SuperAdminLoginResponse представляет ответ на вход супер-админа
type SuperAdminLoginResponse struct {
	Token     string    `json:"token"`
	UserID    string    `json:"user_id"`
	Username  string    `json:"username"`
	Email     string    `json:"email"`
	ExpiresAt int64     `json:"expires_at"`
	LegalEntityID string `json:"legal_entity_id"`
}

// SuperAdminLogin обрабатывает вход супер-админа
// POST /api/v1/auth/super-admin/login
func (ac *AuthController) SuperAdminLogin(c *gin.Context) {
	var req SuperAdminLoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Неверные параметры запроса",
			"details": err.Error(),
		})
		return
	}

	// Ищем супер-админа по username
	var admin models.SuperAdmin
	if err := ac.db.Where("username = ? AND is_active = ?", req.Username, true).First(&admin).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "Неверный логин или пароль",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Ошибка проверки учетных данных",
		})
		return
	}

	// Проверяем пароль
	if err := bcrypt.CompareHashAndPassword([]byte(admin.PasswordHash), []byte(req.Password)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "Неверный логин или пароль",
		})
		return
	}

	// Проверяем, что ИП существует и активно
	var legalEntity models.LegalEntity
	if err := ac.db.Where("id = ? AND is_active = ?", req.LegalEntityID, true).First(&legalEntity).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "Указанное ИП не найдено или неактивно",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Ошибка проверки ИП",
		})
		return
	}

	// Обновляем время последнего входа
	now := time.Now()
	admin.LastLoginAt = &now
	ac.db.Save(&admin)

	// Генерируем токен (упрощенная версия, в продакшене использовать JWT)
	token := generateSimpleToken(admin.ID)

	// Устанавливаем ИП для админа, если еще не установлен
	if admin.LegalEntityID == nil {
		admin.LegalEntityID = &req.LegalEntityID
		ac.db.Save(&admin)
	}

	// Возвращаем ответ
	response := SuperAdminLoginResponse{
		Token:        token,
		UserID:       admin.ID,
		Username:     admin.Username,
		Email:        "", // Можно добавить email в модель SuperAdmin
		ExpiresAt:    time.Now().Add(24 * time.Hour).Unix(),
		LegalEntityID: req.LegalEntityID,
	}

	c.JSON(http.StatusOK, response)
}

// generateSimpleToken генерирует простой токен (в продакшене использовать JWT)
func generateSimpleToken(adminID string) string {
	// Упрощенная версия - в продакшене использовать JWT с подписью
	return "super_admin_token_" + adminID + "_" + time.Now().Format("20060102150405")
}

