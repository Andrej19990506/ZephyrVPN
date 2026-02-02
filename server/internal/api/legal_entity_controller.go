package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"zephyrvpn/server/internal/services"
)

// LegalEntityController управляет API endpoints для юридических лиц
type LegalEntityController struct {
	service *services.LegalEntityService
}

// NewLegalEntityController создает новый контроллер юридических лиц
func NewLegalEntityController(service *services.LegalEntityService) *LegalEntityController {
	return &LegalEntityController{service: service}
}

// GetLegalEntities возвращает список всех юридических лиц
// GET /api/v1/legal-entities
func (lec *LegalEntityController) GetLegalEntities(c *gin.Context) {
	entities, err := lec.service.GetAllLegalEntities()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"legal_entities": entities, "count": len(entities)})
}

// GetLegalEntity возвращает юридическое лицо по ID
// GET /api/v1/legal-entities/:id
func (lec *LegalEntityController) GetLegalEntity(c *gin.Context) {
	id := c.Param("id")
	entity, err := lec.service.GetLegalEntityByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, entity)
}

