package services

import (
	"fmt"
	"log"
	"time"

	"zephyrvpn/server/internal/models"

	"gorm.io/gorm"
)

// RevenuePlanService управляет планами выручки в PostgreSQL
type RevenuePlanService struct {
	db *gorm.DB
}

// NewRevenuePlanService создает новый сервис планов выручки
func NewRevenuePlanService(db *gorm.DB) *RevenuePlanService {
	return &RevenuePlanService{
		db: db,
	}
}

// SavePlan сохраняет план выручки в БД
// Если план на эту дату уже существует, обновляет его (UPSERT)
func (rps *RevenuePlanService) SavePlan(forecast *RevenueForecast, planDate time.Time) error {
	if rps.db == nil {
		return fmt.Errorf("database connection not available")
	}

	// Нормализуем дату (убираем время, оставляем только дату)
	normalizedDate := time.Date(planDate.Year(), planDate.Month(), planDate.Day(), 0, 0, 0, 0, time.UTC)

	plan := &models.RevenuePlan{
		PlanDate:       normalizedDate,
		ForecastTotal:  forecast.ForecastTotal,
		CurrentRevenue: forecast.CurrentRevenue,
		RemainingHours:  forecast.RemainingHours,
		AverageHourly:   forecast.AverageHourly,
		CurrentHourly:   forecast.CurrentHourly,
		HistoricalAvg:   forecast.HistoricalAvg,
		Confidence:      forecast.Confidence,
		Method:          forecast.Method,
	}

	// UPSERT: обновляем если существует, создаем если нет
	result := rps.db.Where("plan_date = ?", normalizedDate).
		Assign(*plan).
		FirstOrCreate(plan)

	if result.Error != nil {
		return fmt.Errorf("failed to save revenue plan: %w", result.Error)
	}

	log.Printf("✅ RevenuePlan сохранен: дата=%s, прогноз=%.2f₽, уверенность=%.0f%%",
		normalizedDate.Format("2006-01-02"), forecast.ForecastTotal, forecast.Confidence)

	return nil
}

// GetLatestPlan получает последний сохраненный план выручки
// Если date указана, получает план на эту дату, иначе - последний созданный план
func (rps *RevenuePlanService) GetLatestPlan(date *time.Time) (*models.RevenuePlan, error) {
	if rps.db == nil {
		return nil, fmt.Errorf("database connection not available")
	}

	var plan models.RevenuePlan
	query := rps.db.Order("created_at DESC")

	if date != nil {
		// Нормализуем дату
		normalizedDate := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC)
		query = query.Where("plan_date = ?", normalizedDate)
	}

	if err := query.First(&plan).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil // План не найден - это нормально
		}
		return nil, fmt.Errorf("failed to get revenue plan: %w", err)
	}

	return &plan, nil
}

// GetPlanForDate получает план на конкретную дату
func (rps *RevenuePlanService) GetPlanForDate(date time.Time) (*models.RevenuePlan, error) {
	return rps.GetLatestPlan(&date)
}

// GetPlanForToday получает план на сегодня
func (rps *RevenuePlanService) GetPlanForToday() (*models.RevenuePlan, error) {
	today := time.Now()
	return rps.GetPlanForDate(today)
}

