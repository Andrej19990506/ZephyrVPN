package api

import (
	"net/http"
	"strconv"

	"zephyrvpn/server/internal/services"

	"github.com/gin-gonic/gin"
)

// StockController управляет API endpoints для остатков
type StockController struct {
	stockService *services.StockService
}

// NewStockController создает новый контроллер остатков
func NewStockController(stockService *services.StockService) *StockController {
	return &StockController{
		stockService: stockService,
	}
}

// GetStockItems возвращает остатки товаров
// GET /api/v1/inventory/stock?branch_id=xxx&include_expired=true
func (sc *StockController) GetStockItems(c *gin.Context) {
	branchID := c.DefaultQuery("branch_id", "all")
	includeExpiredStr := c.DefaultQuery("include_expired", "false")
	includeExpired, _ := strconv.ParseBool(includeExpiredStr)
	
	items, err := sc.stockService.GetStockItems(branchID, includeExpired)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Ошибка получения остатков",
			"details": err.Error(),
		})
		return
	}
	
	c.JSON(http.StatusOK, gin.H{
		"items": items,
		"count": len(items),
	})
}

// GetAtRiskInventory возвращает товары с риском истечения срока годности
// GET /api/v1/inventory/stock/at-risk?branch_id=xxx
func (sc *StockController) GetAtRiskInventory(c *gin.Context) {
	branchID := c.DefaultQuery("branch_id", "all")
	
	items, err := sc.stockService.GetAtRiskInventory(branchID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Ошибка получения рискованных товаров",
			"details": err.Error(),
		})
		return
	}
	
	c.JSON(http.StatusOK, gin.H{
		"items": items,
		"count": len(items),
	})
}

// GetExpiryAlerts возвращает активные уведомления о сроке годности
// GET /api/v1/inventory/stock/expiry-alerts?branch_id=xxx&alert_type=warning|critical
func (sc *StockController) GetExpiryAlerts(c *gin.Context) {
	branchID := c.DefaultQuery("branch_id", "all")
	alertType := c.DefaultQuery("alert_type", "")
	
	alerts, err := sc.stockService.GetExpiryAlerts(branchID, alertType)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Ошибка получения уведомлений",
			"details": err.Error(),
		})
		return
	}
	
	c.JSON(http.StatusOK, gin.H{
		"alerts": alerts,
		"count":  len(alerts),
	})
}

// ProcessSaleDepletion обрабатывает списание ингредиентов при продаже
// POST /api/v1/inventory/stock/process-sale
func (sc *StockController) ProcessSaleDepletion(c *gin.Context) {
	var request struct {
		RecipeID    string  `json:"recipe_id" binding:"required"`
		Quantity    float64 `json:"quantity" binding:"required"`
		BranchID    string  `json:"branch_id" binding:"required"`
		PerformedBy string  `json:"performed_by" binding:"required"`
		SaleID      string  `json:"sale_id" binding:"required"`
	}
	
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Неверные параметры запроса",
			"details": err.Error(),
		})
		return
	}
	
	if err := sc.stockService.ProcessSaleDepletion(
		request.RecipeID,
		request.Quantity,
		request.BranchID,
		request.PerformedBy,
		request.SaleID,
	); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Ошибка обработки списания",
			"details": err.Error(),
		})
		return
	}
	
	c.JSON(http.StatusOK, gin.H{
		"message": "Списание успешно обработано",
	})
}

// CommitProduction обрабатывает ручное производство полуфабриката
// POST /api/v1/inventory/stock/commit-production
func (sc *StockController) CommitProduction(c *gin.Context) {
	var request struct {
		RecipeID          string  `json:"recipe_id" binding:"required"`
		Quantity          float64 `json:"quantity" binding:"required"` // Количество в граммах
		BranchID          string  `json:"branch_id" binding:"required"`
		PerformedBy      string  `json:"performed_by" binding:"required"`
		ProductionOrderID string  `json:"production_order_id" binding:"required"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Неверные параметры запроса",
			"details": err.Error(),
		})
		return
	}

	if err := sc.stockService.CommitProduction(
		request.RecipeID,
		request.Quantity,
		request.BranchID,
		request.PerformedBy,
		request.ProductionOrderID,
	); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Ошибка обработки производства",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Производство успешно зафиксировано",
	})
}

// GetRecipePrimeCost возвращает себестоимость рецепта
// GET /api/v1/inventory/recipes/:id/prime-cost
func (sc *StockController) GetRecipePrimeCost(c *gin.Context) {
	recipeID := c.Param("id")
	if recipeID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "ID рецепта не указан",
		})
		return
	}

	cost, err := sc.stockService.CalculatePrimeCost(recipeID, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Ошибка расчета себестоимости",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"recipe_id": recipeID,
		"prime_cost": cost,
		"currency":   "RUB",
	})
}

// CheckExpiryAlerts запускает проверку сроков годности и создает уведомления
// POST /api/v1/inventory/stock/check-expiry-alerts
func (sc *StockController) CheckExpiryAlerts(c *gin.Context) {
	if err := sc.stockService.CheckAndCreateExpiryAlerts(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Ошибка проверки сроков годности",
			"details": err.Error(),
		})
		return
	}
	
	c.JSON(http.StatusOK, gin.H{
		"message": "Проверка сроков годности завершена",
	})
}

// ProcessInboundInvoice обрабатывает входящую накладную
// POST /api/v1/inventory/stock/process-inbound-invoice
func (sc *StockController) ProcessInboundInvoice(c *gin.Context) {
	var request struct {
		InvoiceID      string                   `json:"invoice_id" binding:"required"`
		Items          []map[string]interface{} `json:"items" binding:"required"`
		PerformedBy    string                   `json:"performed_by" binding:"required"`
		CounterpartyID string                   `json:"counterparty_id"` // Опционально
		TotalAmount    float64                  `json:"total_amount"`     // Общая сумма накладной
		IsPaidCash     bool                     `json:"is_paid_cash"`     // Оплачено наличными
		InvoiceDate    string                   `json:"invoice_date"`     // Дата накладной (опционально, формат: 2006-01-02)
	}
	
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Неверные параметры запроса",
			"details": err.Error(),
		})
		return
	}
	
	if err := sc.stockService.ProcessInboundInvoice(
		request.InvoiceID,
		request.Items,
		request.PerformedBy,
		request.CounterpartyID,
		request.TotalAmount,
		request.IsPaidCash,
		request.InvoiceDate,
	); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Ошибка обработки накладной",
			"details": err.Error(),
		})
		return
	}
	
	c.JSON(http.StatusOK, gin.H{
		"message": "Накладная успешно обработана",
		"invoice_id": request.InvoiceID,
		"items_count": len(request.Items),
	})
}


