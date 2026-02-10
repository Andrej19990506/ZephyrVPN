package api

import (
	"log"
	"net/http"
	"time"

	"zephyrvpn/server/internal/services"

	"github.com/gin-gonic/gin"
)

// AnalyticsController управляет API endpoints для аналитики и прогнозирования
type AnalyticsController struct {
	revenueService      *services.RevenueService
	revenuePlanService   *services.RevenuePlanService
}

// NewAnalyticsController создает новый контроллер аналитики
func NewAnalyticsController(
	revenueService *services.RevenueService,
	revenuePlanService *services.RevenuePlanService,
) *AnalyticsController {
	return &AnalyticsController{
		revenueService:    revenueService,
		revenuePlanService: revenuePlanService,
	}
}

// RunForecast запускает прогнозирование выручки и сохраняет результат в БД
// POST /api/v1/analytics/run-forecast
// Body: {"start_date": "2006-01-02", "end_date": "2006-01-09"} - период прогноза
// Если не указаны, используется сегодня (1 день)
func (ac *AnalyticsController) RunForecast(c *gin.Context) {
	if ac.revenueService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "Revenue service not available",
		})
		return
	}

	if ac.revenuePlanService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "Revenue plan service not available",
		})
		return
	}

	var req struct {
		StartDate string `json:"start_date"` // Начало периода, формат "2006-01-02"
		EndDate   string `json:"end_date"`   // Конец периода, формат "2006-01-02"
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		// Если тело запроса пустое, используем сегодня
		req.StartDate = ""
		req.EndDate = ""
	}

	// Определяем период для прогноза
	now := time.Now()
	startDate := now
	endDate := now

	if req.StartDate != "" {
		parsedDate, err := time.Parse("2006-01-02", req.StartDate)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":   "invalid start_date format",
				"details": "start_date must be in format YYYY-MM-DD",
			})
			return
		}
		startDate = parsedDate
	}

	if req.EndDate != "" {
		parsedDate, err := time.Parse("2006-01-02", req.EndDate)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":   "invalid end_date format",
				"details": "end_date must be in format YYYY-MM-DD",
			})
			return
		}
		endDate = parsedDate
	}

	// Если endDate раньше startDate, меняем местами
	if endDate.Before(startDate) {
		startDate, endDate = endDate, startDate
	}

	// Вычисляем количество дней в периоде
	daysDiff := int(endDate.Sub(startDate).Hours() / 24)
	if daysDiff < 0 {
		daysDiff = 0
	}
	// Добавляем 1, так как включаем оба дня (начало и конец)
	daysInPeriod := daysDiff + 1

	log.Printf("📊 RunForecast: запуск прогнозирования для периода %s - %s (%d дней)",
		startDate.Format("2006-01-02"), endDate.Format("2006-01-02"), daysInPeriod)

	// Запускаем прогнозирование с указанным периодом
	forecast, err := ac.revenueService.GetRevenueForecastForPeriod(
		startDate.Format("2006-01-02"),
		endDate.Format("2006-01-02"),
	)
	if err == nil && forecast != nil {
		log.Printf("✅ RunForecast: прогноз получен методом '%s', результат: %.2f₽ (уверенность: %.1f%%)",
			forecast.Method, forecast.ForecastTotal, forecast.Confidence)
	}
	if err != nil {
		log.Printf("❌ RunForecast: ошибка прогнозирования: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Ошибка прогнозирования выручки",
			"details": err.Error(),
		})
		return
	}

	// Рассчитываем confidence на основе периода
	forecast.Confidence = services.CalculateConfidenceScore(daysInPeriod)
	log.Printf("📊 RunForecast: рассчитана уверенность %.1f%% для периода %d дней", forecast.Confidence, daysInPeriod)

	// Сохраняем план в БД (используем startDate как основную дату)
	if err := ac.revenuePlanService.SavePlan(forecast, startDate); err != nil {
		log.Printf("❌ RunForecast: ошибка сохранения плана: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Ошибка сохранения плана",
			"details": err.Error(),
		})
		return
	}

	log.Printf("✅ RunForecast: прогноз успешно создан и сохранен для периода %s - %s (уверенность: %.1f%%)",
		startDate.Format("2006-01-02"), endDate.Format("2006-01-02"), forecast.Confidence)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Прогноз успешно создан и сохранен",
		"forecast": forecast,
		"start_date": startDate.Format("2006-01-02"),
		"end_date":   endDate.Format("2006-01-02"),
		"days":       daysInPeriod,
	})
}

// GetLatestPlan получает последний сохраненный план выручки
// GET /api/v1/analytics/latest-plan?date=2006-01-02 (опционально)
func (ac *AnalyticsController) GetLatestPlan(c *gin.Context) {
	if ac.revenuePlanService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "Revenue plan service not available",
		})
		return
	}

	dateStr := c.DefaultQuery("date", "")
	var planDate *time.Time

	if dateStr != "" {
		parsedDate, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":   "invalid date format",
				"details": "date must be in format YYYY-MM-DD",
			})
			return
		}
		planDate = &parsedDate
	}

	plan, err := ac.revenuePlanService.GetLatestPlan(planDate)
	if err != nil {
		log.Printf("❌ GetLatestPlan: ошибка получения плана: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Ошибка получения плана",
			"details": err.Error(),
		})
		return
	}

	if plan == nil {
		// План не найден - это нормально, возвращаем статус "not found"
		c.JSON(http.StatusOK, gin.H{
			"plan": nil,
			"message": "План не найден. Запустите прогнозирование в разделе Reports & Analytics.",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"plan": plan,
	})
}

