package api

import (
	"net/http"
	"strconv"

	"zephyrvpn/server/internal/models"
	"zephyrvpn/server/internal/services"

	"github.com/gin-gonic/gin"
)

// PurchaseOrderController управляет заказами на закупку
type PurchaseOrderController struct {
	orderService *services.PurchaseOrderService
}

// NewPurchaseOrderController создает новый контроллер
func NewPurchaseOrderController(orderService *services.PurchaseOrderService) *PurchaseOrderController {
	return &PurchaseOrderController{
		orderService: orderService,
	}
}

// GetPurchaseOrders возвращает список заказов
// GET /api/v1/purchase-orders?branch_id=...&status=...&include_overdue=true&limit=...
func (c *PurchaseOrderController) GetPurchaseOrders(ctx *gin.Context) {
	branchID := ctx.Query("branch_id")
	status := ctx.Query("status")
	includeOverdue := ctx.Query("include_overdue") == "true"
	
	limitStr := ctx.DefaultQuery("limit", "100")
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 {
		limit = 100
	}

	orders, err := c.orderService.GetPurchaseOrders(branchID, status, includeOverdue, limit)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Ошибка получения заказов",
			"details": err.Error(),
		})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"orders": orders,
	})
}

// GetPurchaseOrder возвращает заказ по ID
// GET /api/v1/purchase-orders/:id
func (c *PurchaseOrderController) GetPurchaseOrder(ctx *gin.Context) {
	orderID := ctx.Param("id")

	order, err := c.orderService.GetPurchaseOrder(orderID)
	if err != nil {
		ctx.JSON(http.StatusNotFound, gin.H{
			"error":   "Заказ не найден",
			"details": err.Error(),
		})
		return
	}

	ctx.JSON(http.StatusOK, order)
}

// CreatePurchaseOrder создает новый заказ
// POST /api/v1/purchase-orders
func (c *PurchaseOrderController) CreatePurchaseOrder(ctx *gin.Context) {
	var order models.PurchaseOrder
	if err := ctx.ShouldBindJSON(&order); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{
			"error":   "Неверные данные",
			"details": err.Error(),
		})
		return
	}

	if err := c.orderService.CreatePurchaseOrder(&order); err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Ошибка создания заказа",
			"details": err.Error(),
		})
		return
	}

	ctx.JSON(http.StatusOK, order)
}

// UpdatePurchaseOrder обновляет заказ (только черновики)
// PUT /api/v1/purchase-orders/:id
func (c *PurchaseOrderController) UpdatePurchaseOrder(ctx *gin.Context) {
	orderID := ctx.Param("id")

	var updates map[string]interface{}
	if err := ctx.ShouldBindJSON(&updates); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{
			"error":   "Неверные данные",
			"details": err.Error(),
		})
		return
	}

	order, err := c.orderService.UpdatePurchaseOrder(orderID, updates)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Ошибка обновления заказа",
			"details": err.Error(),
		})
		return
	}

	ctx.JSON(http.StatusOK, order)
}

// DeletePurchaseOrder удаляет заказ (soft delete)
// DELETE /api/v1/purchase-orders/:id
func (c *PurchaseOrderController) DeletePurchaseOrder(ctx *gin.Context) {
	orderID := ctx.Param("id")
	reason := ctx.Query("reason")

	if err := c.orderService.CancelPurchaseOrder(orderID, reason); err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Ошибка отмены заказа",
			"details": err.Error(),
		})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"message": "Заказ успешно отменен",
	})
}

// SendPurchaseOrder отправляет заказ поставщику
// POST /api/v1/purchase-orders/:id/send
func (c *PurchaseOrderController) SendPurchaseOrder(ctx *gin.Context) {
	orderID := ctx.Param("id")

	var req struct {
		ApprovedBy string `json:"approved_by"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{
			"error":   "Неверные данные",
			"details": err.Error(),
		})
		return
	}

	if err := c.orderService.SendPurchaseOrder(orderID, req.ApprovedBy); err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Ошибка отправки заказа",
			"details": err.Error(),
		})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"message": "Заказ успешно отправлен",
	})
}

// ReceivePurchaseOrder получает заказ (создает накладную)
// POST /api/v1/purchase-orders/:id/receive
func (c *PurchaseOrderController) ReceivePurchaseOrder(ctx *gin.Context) {
	orderID := ctx.Param("id")

	var req struct {
		ReceivedItems []services.ReceivedItem `json:"received_items"`
		PerformedBy   string                   `json:"performed_by"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{
			"error":   "Неверные данные",
			"details": err.Error(),
		})
		return
	}

	if err := c.orderService.ReceivePurchaseOrder(orderID, req.ReceivedItems, req.PerformedBy); err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Ошибка получения заказа",
			"details": err.Error(),
		})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"message": "Заказ успешно получен, создана накладная",
	})
}

// CancelPurchaseOrder отменяет заказ
// POST /api/v1/purchase-orders/:id/cancel
func (c *PurchaseOrderController) CancelPurchaseOrder(ctx *gin.Context) {
	orderID := ctx.Param("id")

	var req struct {
		Reason string `json:"reason"`
	}
	ctx.ShouldBindJSON(&req) // Необязательное поле

	if err := c.orderService.CancelPurchaseOrder(orderID, req.Reason); err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Ошибка отмены заказа",
			"details": err.Error(),
		})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"message": "Заказ успешно отменен",
	})
}
