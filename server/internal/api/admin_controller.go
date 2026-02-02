package api

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"zephyrvpn/server/internal/services"
)

type AdminController struct {
	menuService *services.MenuService
}

func NewAdminController(menuService *services.MenuService) *AdminController {
	return &AdminController{
		menuService: menuService,
	}
}

// UpdateMenu принудительно обновляет меню из БД (hot-reload без рестарта)
// Использует Redis Pub/Sub для мгновенного обновления на ВСЕХ серверах
// POST /api/v1/admin/update-menu
func (ac *AdminController) UpdateMenu(c *gin.Context) {
	// 1. Обновляем локально
	if err := ac.menuService.ForceReload(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to update menu",
			"details": err.Error(),
		})
		return
	}

	// 2. Публикуем событие в Redis для обновления на всех серверах
	if err := ac.menuService.PublishUpdate(); err != nil {
		log.Printf("⚠️ Не удалось опубликовать событие в Redis: %v", err)
		// Не критично, локальное обновление уже выполнено
	}

	lastUpdate := ac.menuService.GetLastUpdate()
	c.JSON(http.StatusOK, gin.H{
		"message":    "Menu updated successfully (broadcasted to all servers via Redis)",
		"last_update": lastUpdate.Format("2006-01-02 15:04:05"),
		"method":     "redis_pubsub",
	})
}

// GetMenuStatus возвращает статус меню (когда последний раз обновлялось)
// GET /api/v1/admin/menu-status
func (ac *AdminController) GetMenuStatus(c *gin.Context) {
	lastUpdate := ac.menuService.GetLastUpdate()
	c.JSON(http.StatusOK, gin.H{
		"last_update": lastUpdate.Format("2006-01-02 15:04:05"),
		"pizzas_count": len(GetAvailablePizzas()),
		"sets_count":   len(GetAvailableSets()),
		"extras_count": len(GetAvailableExtras()),
	})
}

