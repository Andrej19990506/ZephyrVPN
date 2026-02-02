package api

import (
	"net/http"

	"zephyrvpn/server/internal/models"
	"zephyrvpn/server/internal/services"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// CounterpartyController управляет API endpoints для контрагентов
type CounterpartyController struct {
	service *services.CounterpartyService
}

// NewCounterpartyController создает новый контроллер контрагентов
func NewCounterpartyController(service *services.CounterpartyService) *CounterpartyController {
	return &CounterpartyController{
		service: service,
	}
}

// GetCounterparties получает список всех контрагентов
// GET /api/v1/finance/counterparties
func (cc *CounterpartyController) GetCounterparties(c *gin.Context) {
	counterparties, err := cc.service.GetAllCounterparties()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Ошибка получения контрагентов",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"counterparties": counterparties,
		"count":          len(counterparties),
	})
}

// GetCounterparty получает контрагента по ID
// GET /api/v1/finance/counterparties/:id
func (cc *CounterpartyController) GetCounterparty(c *gin.Context) {
	id := c.Param("id")
	counterparty, err := cc.service.GetCounterpartyByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "Контрагент не найден",
		})
		return
	}

	c.JSON(http.StatusOK, counterparty)
}

// CreateCounterparty создает нового контрагента
// POST /api/v1/finance/counterparties
func (cc *CounterpartyController) CreateCounterparty(c *gin.Context) {
	var req models.Counterparty
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Неверные данные",
			"details": err.Error(),
		})
		return
	}

	// Валидация обязательных полей
	if req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Название контрагента обязательно",
		})
		return
	}

	// Генерируем ID если не указан
	if req.ID == "" {
		req.ID = uuid.New().String()
	}

	if err := cc.service.CreateCounterparty(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Ошибка создания контрагента",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusCreated, req)
}

// UpdateCounterparty обновляет данные контрагента
// PUT /api/v1/finance/counterparties/:id
func (cc *CounterpartyController) UpdateCounterparty(c *gin.Context) {
	id := c.Param("id")

	var req models.Counterparty
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Неверные данные",
			"details": err.Error(),
		})
		return
	}

	if err := cc.service.UpdateCounterparty(id, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Ошибка обновления контрагента",
			"details": err.Error(),
		})
		return
	}

	// Получаем обновленного контрагента
	counterparty, err := cc.service.GetCounterpartyByID(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Ошибка получения обновленного контрагента",
		})
		return
	}

	c.JSON(http.StatusOK, counterparty)
}

// DeleteCounterparty удаляет контрагента
// DELETE /api/v1/finance/counterparties/:id
func (cc *CounterpartyController) DeleteCounterparty(c *gin.Context) {
	id := c.Param("id")

	if err := cc.service.DeleteCounterparty(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Ошибка удаления контрагента",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Контрагент удален",
	})
}

