package api

import (
	"net/http"
	"strconv"
	"time"

	"zephyrvpn/server/internal/services"

	"github.com/gin-gonic/gin"
)

// ProcurementPlanningController управляет API для планирования закупок
type ProcurementPlanningController struct {
	planningService *services.ProcurementPlanningService
}

// NewProcurementPlanningController создает новый контроллер
func NewProcurementPlanningController(planningService *services.ProcurementPlanningService) *ProcurementPlanningController {
	return &ProcurementPlanningController{
		planningService: planningService,
	}
}

// GetMonthlyPlan возвращает план на месяц с матрицей данных
// GET /api/v1/procurement/monthly-plan?branch_id=xxx&year=2026&month=2
func (c *ProcurementPlanningController) GetMonthlyPlan(ctx *gin.Context) {
	branchID := ctx.Query("branch_id")
	if branchID == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "branch_id обязателен"})
		return
	}
	
	yearStr := ctx.DefaultQuery("year", "")
	monthStr := ctx.DefaultQuery("month", "")
	
	var year, month int
	var err error
	
	if yearStr == "" || monthStr == "" {
		// Если не указаны, используем текущий месяц
		now := time.Now()
		year = now.Year()
		month = int(now.Month())
	} else {
		year, err = strconv.Atoi(yearStr)
		if err != nil || year < 2020 || year > 2100 {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": "неверный год"})
			return
		}
		
		month, err = strconv.Atoi(monthStr)
		if err != nil || month < 1 || month > 12 {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": "неверный месяц (1-12)"})
			return
		}
	}
	
	plan, err := c.planningService.GetMonthlyPlan(branchID, year, month)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Ошибка получения плана",
			"details": err.Error(),
		})
		return
	}
	
	ctx.JSON(http.StatusOK, plan)
}

// UpdatePlanCell обновляет ячейку в плане
// PUT /api/v1/procurement/plan-cell
func (c *ProcurementPlanningController) UpdatePlanCell(ctx *gin.Context) {
	var request struct {
		PlanID         string  `json:"plan_id" binding:"required"`
		NomenclatureID string  `json:"nomenclature_id" binding:"required"`
		Date           string  `json:"date" binding:"required"` // YYYY-MM-DD
		Quantity       float64 `json:"quantity" binding:"required"`
	}
	
	if err := ctx.ShouldBindJSON(&request); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{
			"error":   "Неверные параметры",
			"details": err.Error(),
		})
		return
	}
	
	if err := c.planningService.UpdatePlanCell(request.PlanID, request.NomenclatureID, request.Date, request.Quantity); err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Ошибка обновления ячейки",
			"details": err.Error(),
		})
		return
	}
	
	ctx.JSON(http.StatusOK, gin.H{"success": true})
}

// SubmitPlan обрабатывает отправку плана и создает PurchaseOrders
// POST /api/v1/procurement/submit-plan
func (c *ProcurementPlanningController) SubmitPlan(ctx *gin.Context) {
	var request struct {
		PlanID    string `json:"plan_id" binding:"required"`
		CreatedBy string `json:"created_by" binding:"required"`
	}
	
	if err := ctx.ShouldBindJSON(&request); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{
			"error":   "Неверные параметры",
			"details": err.Error(),
		})
		return
	}
	
	if err := c.planningService.SubmitPlan(request.PlanID, request.CreatedBy); err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Ошибка отправки плана",
			"details": err.Error(),
		})
		return
	}
	
	ctx.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "План отправлен. Заказы на закупку созданы.",
	})
}



