package api

import (
	"net/http"

	"zephyrvpn/server/internal/models"
	"zephyrvpn/server/internal/services"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// FinanceController управляет API endpoints для финансовых транзакций
type FinanceController struct {
	service *services.FinanceService
}

// NewFinanceController создает новый контроллер финансов
func NewFinanceController(service *services.FinanceService) *FinanceController {
	return &FinanceController{
		service: service,
	}
}

// GetTransactions получает список финансовых транзакций
// GET /api/v1/finance/transactions?branch_id=xxx&source=bank|cash&entity_ids=...
func (fc *FinanceController) GetTransactions(c *gin.Context) {
	branchID := c.Query("branch_id")
	source := c.Query("source")
	entityIDs := c.Query("entity_ids")

	transactions, err := fc.service.GetTransactions(branchID, source, entityIDs)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Ошибка получения транзакций",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"transactions": transactions,
		"count":        len(transactions),
	})
}

// GetTransaction получает транзакцию по ID
// GET /api/v1/finance/transactions/:id
func (fc *FinanceController) GetTransaction(c *gin.Context) {
	id := c.Param("id")
	transaction, err := fc.service.GetTransactionByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "Транзакция не найдена",
		})
		return
	}

	c.JSON(http.StatusOK, transaction)
}

// CreateTransaction создает новую финансовую транзакцию
// POST /api/v1/finance/transactions
func (fc *FinanceController) CreateTransaction(c *gin.Context) {
	var req models.FinanceTransaction
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Неверные данные",
			"details": err.Error(),
		})
		return
	}

	// Валидация обязательных полей
	if req.Amount == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Сумма транзакции обязательна",
		})
		return
	}

	// Генерируем ID если не указан
	if req.ID == "" {
		req.ID = uuid.New().String()
	}

	if err := fc.service.CreateTransaction(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Ошибка создания транзакции",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusCreated, req)
}

// GetCounterpartiesWithBalances получает список контрагентов с балансами из finance_transactions
// GET /api/v1/finance/counterparties/with-balances
func (fc *FinanceController) GetCounterpartiesWithBalances(c *gin.Context) {
	counterparties, err := fc.service.GetCounterpartiesWithBalances()
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

