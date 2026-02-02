package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"zephyrvpn/server/internal/models"
	"zephyrvpn/server/internal/services"
)

type BranchController struct {
	service *services.BranchService
}

func NewBranchController(service *services.BranchService) *BranchController {
	return &BranchController{service: service}
}

// GetBranches получает список всех филиалов
// GET /api/v1/branches?legal_entity_id=xxx&super_admin_id=xxx
func (bc *BranchController) GetBranches(c *gin.Context) {
	// Получаем опциональные фильтры из query параметров
	legalEntityID := c.Query("legal_entity_id")
	superAdminID := c.Query("super_admin_id")

	var legalEntityIDPtr *string
	var superAdminIDPtr *string

	if legalEntityID != "" {
		legalEntityIDPtr = &legalEntityID
	}
	if superAdminID != "" {
		superAdminIDPtr = &superAdminID
	}

	branches, err := bc.service.GetAllBranches(legalEntityIDPtr, superAdminIDPtr)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"branches": branches,
		"count":    len(branches),
	})
}

// GetBranch получает филиал по ID
// GET /api/v1/branches/:id
func (bc *BranchController) GetBranch(c *gin.Context) {
	id := c.Param("id")

	branch, err := bc.service.GetBranchByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, branch)
}

// CreateBranch создает новый филиал
// POST /api/v1/branches
func (bc *BranchController) CreateBranch(c *gin.Context) {
	var branch models.Branch

	if err := c.ShouldBindJSON(&branch); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Неверные данные",
			"details": err.Error(),
		})
		return
	}

	if err := bc.service.CreateBranch(&branch); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, branch)
}

// UpdateBranch обновляет филиал
// PUT /api/v1/branches/:id
func (bc *BranchController) UpdateBranch(c *gin.Context) {
	id := c.Param("id")

	var updatedBranch models.Branch
	if err := c.ShouldBindJSON(&updatedBranch); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Неверные данные",
			"details": err.Error(),
		})
		return
	}

	if err := bc.service.UpdateBranch(id, &updatedBranch); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Получаем обновленный филиал
	branch, err := bc.service.GetBranchByID(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, branch)
}

// DeleteBranch удаляет филиал
// DELETE /api/v1/branches/:id
func (bc *BranchController) DeleteBranch(c *gin.Context) {
	id := c.Param("id")

	if err := bc.service.DeleteBranch(id); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Филиал удален"})
}




