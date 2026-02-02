package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

type KitchenController struct {
	workerPool *KitchenWorkerPool
}

func NewKitchenController(workerPool *KitchenWorkerPool) *KitchenController {
	return &KitchenController{
		workerPool: workerPool,
	}
}

// GetWorkersStats возвращает статистику воркеров
func (kc *KitchenController) GetWorkersStats(c *gin.Context) {
	stats := kc.workerPool.GetStats()
	c.JSON(http.StatusOK, stats)
}

// SetWorkersCount устанавливает количество воркеров
func (kc *KitchenController) SetWorkersCount(c *gin.Context) {
	var req struct {
		Count int `json:"count" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid data", "details": err.Error()})
		return
	}

	if req.Count < 0 || req.Count > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "count must be between 0 and 100"})
		return
	}

	kc.workerPool.SetWorkerCount(req.Count)
	c.JSON(http.StatusOK, gin.H{
		"message": "workers count updated",
		"count":   req.Count,
	})
}

// AddWorker добавляет одного воркера
func (kc *KitchenController) AddWorker(c *gin.Context) {
	id := kc.workerPool.StartWorker()
	c.JSON(http.StatusOK, gin.H{
		"message": "worker added",
		"worker_id": id,
	})
}

// RemoveWorker удаляет воркера по ID
func (kc *KitchenController) RemoveWorker(c *gin.Context) {
	workerIDStr := c.Param("id")
	workerID, err := strconv.Atoi(workerIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid worker id"})
		return
	}

	success := kc.workerPool.StopWorker(workerID)
	if !success {
		c.JSON(http.StatusNotFound, gin.H{"error": "worker not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "worker removed",
		"worker_id": workerID,
	})
}

// StopAllWorkers останавливает всех воркеров
func (kc *KitchenController) StopAllWorkers(c *gin.Context) {
	kc.workerPool.StopAll()
	c.JSON(http.StatusOK, gin.H{
		"message": "all workers stopped",
	})
}

// StartWorkers запускает указанное количество воркеров
func (kc *KitchenController) StartWorkers(c *gin.Context) {
	var req struct {
		Count int `json:"count" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid data", "details": err.Error()})
		return
	}

	if req.Count < 0 || req.Count > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "count must be between 0 and 100"})
		return
	}

	// Останавливаем всех перед запуском новых
	kc.workerPool.StopAll()
	
	// Запускаем новое количество
	kc.workerPool.SetWorkerCount(req.Count)
	
	c.JSON(http.StatusOK, gin.H{
		"message": "workers started",
		"count":   req.Count,
	})
}

