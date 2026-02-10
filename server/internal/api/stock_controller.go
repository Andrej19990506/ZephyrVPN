package api

import (
	"net/http"
	"strconv"
	"time"

	"zephyrvpn/server/internal/models"
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

// GetStockMovements возвращает список движений склада с фильтрацией
// GET /api/v1/inventory/stock/movements?branch_id=xxx&movement_type=sale&date_from=2024-01-01&date_to=2024-12-31&search=мука&limit=1000
func (sc *StockController) GetStockMovements(c *gin.Context) {
	branchID := c.DefaultQuery("branch_id", "all")
	movementType := c.DefaultQuery("movement_type", "")
	dateFrom := c.DefaultQuery("date_from", "")
	dateTo := c.DefaultQuery("date_to", "")
	searchQuery := c.DefaultQuery("search", "")
	
	limitStr := c.DefaultQuery("limit", "1000")
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 {
		limit = 1000
	}

	movements, err := sc.stockService.GetStockMovements(branchID, movementType, dateFrom, dateTo, searchQuery, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Ошибка получения движений",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"movements": movements,
		"count":     len(movements),
	})
}

// CreateInvoice создает новую накладную (черновик)
// POST /api/v1/inventory/stock/invoices
func (sc *StockController) CreateInvoice(c *gin.Context) {
	var request struct {
		Number        string  `json:"number" binding:"required"`
		CounterpartyID *string `json:"counterparty_id"`
		BranchID      string  `json:"branch_id" binding:"required"`
		TotalAmount   float64 `json:"total_amount" binding:"required"`
		InvoiceDate   string  `json:"invoice_date"` // Формат: 2006-01-02
		IsPaidCash    bool    `json:"is_paid_cash"`
		PerformedBy   string  `json:"performed_by"`
		Notes         string  `json:"notes"`
		Source        string  `json:"source"` // 'official' or 'internal'
		Items         []map[string]interface{} `json:"items"` // Товары накладной (для информации)
	}
	
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Неверные параметры запроса",
			"details": err.Error(),
		})
		return
	}
	
	invoice, err := sc.stockService.CreateInvoice(
		request.Number,
		request.CounterpartyID,
		request.BranchID,
		request.TotalAmount,
		request.InvoiceDate,
		request.IsPaidCash,
		request.PerformedBy,
		request.Notes,
		request.Source,
		request.Items,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Ошибка создания накладной",
			"details": err.Error(),
		})
		return
	}
	
	c.JSON(http.StatusCreated, invoice)
}

// InvoiceItemResponse представляет товар накладной с правильно рассчитанными ценами
type InvoiceItemResponse struct {
	ProductID      string  `json:"product_id"`
	ProductName    string  `json:"product_name"`
	NomenclatureID string `json:"nomenclature_id"`
	Quantity       float64 `json:"quantity"`        // Количество в base_unit (граммы)
	Unit           string  `json:"unit"`           // base_unit (граммы)
	PricePerKg     float64 `json:"price_per_unit"` // Цена за inbound_unit (килограммы)
	PriceUnit      string  `json:"price_unit"`     // inbound_unit (килограммы)
	TotalSum       float64 `json:"total_sum"`      // Итоговая стоимость строки: (Quantity / ConversionFactor) * PricePerKg
	ExpiryDate     *string `json:"expiry_date,omitempty"`
	ConversionFactor float64 `json:"conversion_factor"`
}

// InvoiceResponse представляет накладную с правильно рассчитанными данными
type InvoiceResponse struct {
	ID             string                `json:"id"`
	Number         string                `json:"number"`
	CounterpartyID *string               `json:"counterparty_id,omitempty"`
	Counterparty   *models.Counterparty  `json:"counterparty,omitempty"`
	BranchID       string                `json:"branch_id"`
	Branch         *models.Branch        `json:"branch,omitempty"`
	TotalAmount    float64               `json:"total_amount"`    // Общая сумма накладной (пересчитанная)
	Status         string                `json:"status"`
	InvoiceDate    string                `json:"invoice_date"`
	IsPaidCash     bool                  `json:"is_paid_cash"`
	PerformedBy    string                `json:"performed_by"`
	Notes          string                `json:"notes"`
	CreatedAt      string                `json:"created_at"`
	UpdatedAt      string                `json:"updated_at"`
	Items          []InvoiceItemResponse `json:"items"` // Товары с правильно рассчитанными ценами
}

// GetInvoices возвращает список накладных с фильтрацией
// GET /api/v1/inventory/stock/invoices?branch_id=xxx&status=draft&limit=100
func (sc *StockController) GetInvoices(c *gin.Context) {
	branchID := c.DefaultQuery("branch_id", "")
	status := c.DefaultQuery("status", "") // 'draft', 'completed', 'cancelled'
	limitStr := c.DefaultQuery("limit", "100")
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 {
		limit = 100
	}
	
	invoices, err := sc.stockService.GetInvoices(branchID, status, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Ошибка получения накладных",
			"details": err.Error(),
		})
		return
	}
	
	// Преобразуем модели в DTO с правильными расчетами
	invoiceResponses := make([]InvoiceResponse, 0, len(invoices))
	for _, invoice := range invoices {
		response := sc.mapInvoiceToResponse(invoice)
		invoiceResponses = append(invoiceResponses, response)
	}
	
	c.JSON(http.StatusOK, gin.H{
		"invoices": invoiceResponses,
		"count":    len(invoiceResponses),
	})
}

// mapInvoiceToResponse преобразует модель Invoice в DTO с правильно рассчитанными ценами
func (sc *StockController) mapInvoiceToResponse(invoice models.Invoice) InvoiceResponse {
	items := make([]InvoiceItemResponse, 0, len(invoice.StockBatches))
	var recalculatedTotalAmount float64
	
	for _, batch := range invoice.StockBatches {
		// Проверяем, что номенклатура загружена (проверяем по ID, так как Nomenclature - это структура, а не указатель)
		if batch.NomenclatureID == "" {
			continue
		}
		
		nomenclature := batch.Nomenclature
		// Если номенклатура не загружена (пустое имя), пропускаем
		if nomenclature.Name == "" {
			continue
		}
		inboundUnit := nomenclature.InboundUnit
		if inboundUnit == "" {
			inboundUnit = nomenclature.BaseUnit
		}
		baseUnit := nomenclature.BaseUnit
		if baseUnit == "" {
			baseUnit = "g"
		}
		conversionFactor := nomenclature.ConversionFactor
		if conversionFactor <= 0 {
			conversionFactor = 1
		}
		
		// Количество остается в base_unit (граммы)
		quantity := batch.Quantity
		unit := baseUnit
		
		// Цена за inbound_unit (килограммы) = цена за base_unit (граммы) * conversion_factor
		pricePerKg := batch.CostPerUnit * conversionFactor
		priceUnit := inboundUnit
		
		// Итоговая стоимость строки: (Quantity в граммах / ConversionFactor) * PricePerKg
		// Например: (12000 г / 1000) * 230₽/кг = 12 кг * 230₽/кг = 2760₽
		totalSum := (quantity / conversionFactor) * pricePerKg
		recalculatedTotalAmount += totalSum
		
		// Форматируем expiry_date
		var expiryDateStr *string
		if batch.ExpiryAt != nil {
			formatted := batch.ExpiryAt.Format("2006-01-02")
			expiryDateStr = &formatted
		}
		
		item := InvoiceItemResponse{
			ProductID:        batch.NomenclatureID,
			ProductName:      nomenclature.Name,
			NomenclatureID:   batch.NomenclatureID,
			Quantity:         quantity,
			Unit:             unit,
			PricePerKg:       pricePerKg,
			PriceUnit:        priceUnit,
			TotalSum:         totalSum,
			ExpiryDate:       expiryDateStr,
			ConversionFactor: conversionFactor,
		}
		items = append(items, item)
	}
	
	// Используем пересчитанную сумму, если есть товары, иначе используем TotalAmount из БД
	finalTotalAmount := recalculatedTotalAmount
	if finalTotalAmount == 0 && invoice.TotalAmount > 0 {
		finalTotalAmount = invoice.TotalAmount
	}
	
	return InvoiceResponse{
		ID:             invoice.ID,
		Number:         invoice.Number,
		CounterpartyID: invoice.CounterpartyID,
		Counterparty:   invoice.Counterparty,
		BranchID:       invoice.BranchID,
		Branch:         invoice.Branch,
		TotalAmount:    finalTotalAmount,
		Status:         string(invoice.Status),
		InvoiceDate:    invoice.InvoiceDate.Format("2006-01-02"),
		IsPaidCash:     invoice.IsPaidCash,
		PerformedBy:    invoice.PerformedBy,
		Notes:          invoice.Notes,
		CreatedAt:      invoice.CreatedAt.Format(time.RFC3339),
		UpdatedAt:      invoice.UpdatedAt.Format(time.RFC3339),
		Items:          items,
	}
}

// UpdateInvoice обновляет накладную (черновик)
// PUT /api/v1/inventory/stock/invoices/:id
func (sc *StockController) UpdateInvoice(c *gin.Context) {
	invoiceID := c.Param("id")
	if invoiceID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID накладной не указан"})
		return
	}
	
	var request struct {
		Number        *string `json:"number"`
		CounterpartyID *string `json:"counterparty_id"`
		BranchID      *string `json:"branch_id"`
		TotalAmount   *float64 `json:"total_amount"`
		InvoiceDate   *string `json:"invoice_date"`
		IsPaidCash    *bool   `json:"is_paid_cash"`
		PerformedBy   *string `json:"performed_by"`
		Notes         *string `json:"notes"`
		Source        *string `json:"source"`
		Items         []map[string]interface{} `json:"items"`
	}
	
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Неверные параметры запроса",
			"details": err.Error(),
		})
		return
	}
	
	// Преобразуем request в map для UpdateInvoice
	updates := make(map[string]interface{})
	if request.Number != nil {
		updates["number"] = *request.Number
	}
	if request.CounterpartyID != nil {
		updates["counterparty_id"] = *request.CounterpartyID
	}
	if request.BranchID != nil {
		updates["branch_id"] = *request.BranchID
	}
	if request.TotalAmount != nil {
		updates["total_amount"] = *request.TotalAmount
	}
	if request.InvoiceDate != nil {
		updates["invoice_date"] = *request.InvoiceDate
	}
	if request.IsPaidCash != nil {
		updates["is_paid_cash"] = *request.IsPaidCash
	}
	if request.PerformedBy != nil {
		updates["performed_by"] = *request.PerformedBy
	}
	if request.Notes != nil {
		updates["notes"] = *request.Notes
	}
	
	invoice, err := sc.stockService.UpdateInvoice(invoiceID, updates)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Ошибка обновления накладной",
			"details": err.Error(),
		})
		return
	}
	
	c.JSON(http.StatusOK, invoice)
}

// DeleteInvoice удаляет накладную (только черновики)
// DELETE /api/v1/inventory/stock/invoices/:id
func (sc *StockController) DeleteInvoice(c *gin.Context) {
	invoiceID := c.Param("id")
	if invoiceID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID накладной не указан"})
		return
	}
	
	if err := sc.stockService.DeleteInvoice(invoiceID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Ошибка удаления накладной",
			"details": err.Error(),
		})
		return
	}
	
	c.JSON(http.StatusOK, gin.H{"message": "Накладная успешно удалена"})
}

// GetBatchesHistory возвращает историю всех батчей для конкретной номенклатуры
// GET /api/v1/inventory/stock/batches-history?nomenclature_id=xxx&branch_id=xxx
func (sc *StockController) GetBatchesHistory(c *gin.Context) {
	nomenclatureID := c.Query("nomenclature_id")
	if nomenclatureID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "nomenclature_id обязателен",
		})
		return
	}
	
	branchID := c.DefaultQuery("branch_id", "all")
	
	batches, err := sc.stockService.GetBatchesHistory(nomenclatureID, branchID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Ошибка получения истории батчей",
			"details": err.Error(),
		})
		return
	}
	
	c.JSON(http.StatusOK, gin.H{
		"batches": batches,
		"count":   len(batches),
	})
}


