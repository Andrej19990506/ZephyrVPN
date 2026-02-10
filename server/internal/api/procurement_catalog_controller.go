package api

import (
	"net/http"

	"zephyrvpn/server/internal/models"
	"zephyrvpn/server/internal/services"

	"github.com/gin-gonic/gin"
)

// ProcurementCatalogController управляет каталогом поставщиков
type ProcurementCatalogController struct {
	catalogService *services.ProcurementCatalogService
	uomService     *services.UoMConversionService
}

// NewProcurementCatalogController создает новый контроллер
func NewProcurementCatalogController(catalogService *services.ProcurementCatalogService, uomService *services.UoMConversionService) *ProcurementCatalogController {
	return &ProcurementCatalogController{
		catalogService: catalogService,
		uomService:     uomService,
	}
}

// GetSetupTemplate возвращает структуру каталога для UI
// GET /api/v1/procurement/setup-template?branch_id=...
func (c *ProcurementCatalogController) GetSetupTemplate(ctx *gin.Context) {
	branchID := ctx.Query("branch_id")

	template, err := c.catalogService.GetSetupTemplate(branchID)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Ошибка загрузки каталога",
			"details": err.Error(),
		})
		return
	}

	ctx.JSON(http.StatusOK, template)
}

// SaveCatalog сохраняет каталог поставщиков
// POST /api/v1/procurement/save-catalog
func (c *ProcurementCatalogController) SaveCatalog(ctx *gin.Context) {
	var req services.SaveCatalogRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{
			"error":   "Неверные данные",
			"details": err.Error(),
		})
		return
	}

	if req.BranchID == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{
			"error": "branch_id обязателен",
		})
		return
	}

	if err := c.catalogService.SaveCatalog(&req); err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Ошибка сохранения каталога",
			"details": err.Error(),
		})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"message": "Каталог успешно сохранен",
	})
}

// GetUoMConversionRules возвращает все активные правила конвертации
// GET /api/v1/procurement/uom-rules
func (c *ProcurementCatalogController) GetUoMConversionRules(ctx *gin.Context) {
	if c.uomService == nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{
			"error": "Сервис правил конвертации не инициализирован",
		})
		return
	}

	rules, err := c.uomService.GetAllRules()
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Ошибка загрузки правил конвертации",
			"details": err.Error(),
		})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"rules": rules,
	})
}

// CreateUoMConversionRule создает новое правило конвертации
// POST /api/v1/procurement/uom-rules
func (c *ProcurementCatalogController) CreateUoMConversionRule(ctx *gin.Context) {
	if c.uomService == nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{
			"error": "Сервис правил конвертации не инициализирован",
		})
		return
	}

	var rule models.UoMConversionRule
	if err := ctx.ShouldBindJSON(&rule); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{
			"error":   "Неверные данные",
			"details": err.Error(),
		})
		return
	}

	// Валидация
	if rule.Name == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{
			"error": "Название правила обязательно",
		})
		return
	}

	if rule.InputUOM == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{
			"error": "Единица поставщика обязательна",
		})
		return
	}

	if rule.BaseUnit == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{
			"error": "Базовая единица обязательна",
		})
		return
	}

	if rule.Multiplier <= 0 {
		ctx.JSON(http.StatusBadRequest, gin.H{
			"error": "Множитель должен быть больше 0",
		})
		return
	}

	// Если это правило по умолчанию, снимаем флаг с других правил
	if rule.IsDefault {
		if err := c.uomService.UpdateAllRulesDefault(false); err != nil {
			ctx.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Ошибка обновления правил по умолчанию",
				"details": err.Error(),
			})
			return
		}
	}

	if err := c.uomService.CreateRule(&rule); err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Ошибка создания правила",
			"details": err.Error(),
		})
		return
	}

	ctx.JSON(http.StatusCreated, rule)
}

// UpdateUoMConversionRule обновляет существующее правило конвертации
// PUT /api/v1/procurement/uom-rules/:id
func (c *ProcurementCatalogController) UpdateUoMConversionRule(ctx *gin.Context) {
	if c.uomService == nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{
			"error": "Сервис правил конвертации не инициализирован",
		})
		return
	}

	ruleID := ctx.Param("id")
	if ruleID == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{
			"error": "ID правила обязателен",
		})
		return
	}

	var updates map[string]interface{}
	if err := ctx.ShouldBindJSON(&updates); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{
			"error":   "Неверные данные",
			"details": err.Error(),
		})
		return
	}

	// Валидация множителя, если он указан
	if multiplier, ok := updates["multiplier"].(float64); ok {
		if multiplier <= 0 {
			ctx.JSON(http.StatusBadRequest, gin.H{
				"error": "Множитель должен быть больше 0",
			})
			return
		}
	}

	// Если устанавливается правило по умолчанию, снимаем флаг с других правил
	if isDefault, ok := updates["is_default"].(bool); ok && isDefault {
		if err := c.uomService.UpdateAllRulesDefault(false); err != nil {
			ctx.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Ошибка обновления правил по умолчанию",
				"details": err.Error(),
			})
			return
		}
	}

	if err := c.uomService.UpdateRule(ruleID, updates); err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Ошибка обновления правила",
			"details": err.Error(),
		})
		return
	}

	// Возвращаем обновленное правило
	rule, err := c.uomService.GetRuleByID(ruleID)
	if err != nil {
		ctx.JSON(http.StatusOK, gin.H{
			"message": "Правило обновлено",
		})
		return
	}

	ctx.JSON(http.StatusOK, rule)
}

// DeleteUoMConversionRule удаляет правило конвертации
// DELETE /api/v1/procurement/uom-rules/:id
func (c *ProcurementCatalogController) DeleteUoMConversionRule(ctx *gin.Context) {
	if c.uomService == nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{
			"error": "Сервис правил конвертации не инициализирован",
		})
		return
	}

	ruleID := ctx.Param("id")
	if ruleID == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{
			"error": "ID правила обязателен",
		})
		return
	}

	if err := c.uomService.DeleteRule(ruleID); err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Ошибка удаления правила",
			"details": err.Error(),
		})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"message": "Правило удалено",
	})
}

// CalculateMultiplier вычисляет множитель конвертации на основе текстового описания
// POST /api/v1/procurement/calculate-multiplier
func (c *ProcurementCatalogController) CalculateMultiplier(ctx *gin.Context) {
	var req struct {
		InputUOM string `json:"input_uom" binding:"required"`
		BaseUOM  string `json:"base_uom" binding:"required"`
	}

	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{
			"error":   "Неверные данные",
			"details": err.Error(),
		})
		return
	}

	parser := services.NewUoMParser()
	result, err := parser.ParseQuantity(req.InputUOM, req.BaseUOM)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{
			"error":   "Ошибка парсинга единицы измерения",
			"details": err.Error(),
		})
		return
	}

	ctx.JSON(http.StatusOK, result)
}

// GetCatalogItemPrice возвращает цену товара из каталога поставщиков
// GET /api/v1/procurement/catalog-item-price?nomenclature_id=...&counterparty_id=...&branch_id=...
func (c *ProcurementCatalogController) GetCatalogItemPrice(ctx *gin.Context) {
	nomenclatureID := ctx.Query("nomenclature_id")
	counterpartyID := ctx.Query("counterparty_id")
	branchID := ctx.Query("branch_id")
	
	if nomenclatureID == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{
			"error": "nomenclature_id обязателен",
		})
		return
	}
	
	if counterpartyID == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{
			"error": "counterparty_id обязателен",
		})
		return
	}
	
	price, found, err := c.catalogService.GetCatalogItemPrice(nomenclatureID, counterpartyID, branchID)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Ошибка получения цены из каталога",
			"details": err.Error(),
		})
		return
	}
	
	if !found {
		ctx.JSON(http.StatusOK, gin.H{
			"found": false,
			"price": 0,
		})
		return
	}
	
	ctx.JSON(http.StatusOK, gin.H{
		"found": true,
		"price": price,
	})
}


