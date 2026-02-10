package models

import (
	"time"

	"gorm.io/gorm"
)

// RevenuePlan модель для хранения планов выручки в PostgreSQL
type RevenuePlan struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	PlanDate       time.Time `gorm:"type:date;uniqueIndex;not null" json:"plan_date"` // Дата, на которую создан план
	ForecastTotal  float64   `gorm:"type:decimal(15,2);not null" json:"forecast_total"` // Прогнозируемая выручка на конец дня
	CurrentRevenue float64   `gorm:"type:decimal(15,2);default:0" json:"current_revenue"` // Текущая выручка на момент создания плана
	RemainingHours float64   `gorm:"type:decimal(5,2);not null" json:"remaining_hours"` // Оставшиеся часы до закрытия
	AverageHourly  float64   `gorm:"type:decimal(15,2);default:0" json:"average_hourly"` // Средняя выручка в час (на основе истории)
	CurrentHourly  float64   `gorm:"type:decimal(15,2);default:0" json:"current_hourly"` // Текущая выручка в час (сегодня)
	HistoricalAvg  float64   `gorm:"type:decimal(15,2);default:0" json:"historical_avg"` // Средняя выручка за аналогичные дни недели
	Confidence     float64   `gorm:"type:decimal(5,2);default:0" json:"confidence"`     // Уверенность в прогнозе (0-100%)
	Method         string    `gorm:"type:varchar(50);not null" json:"method"`            // Метод прогнозирования
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// TableName возвращает имя таблицы
func (RevenuePlan) TableName() string {
	return "revenue_plans"
}

// BeforeCreate вызывается перед созданием записи
func (rp *RevenuePlan) BeforeCreate(tx *gorm.DB) error {
	now := time.Now()
	rp.CreatedAt = now
	rp.UpdatedAt = now
	return nil
}

// BeforeUpdate вызывается перед обновлением записи
func (rp *RevenuePlan) BeforeUpdate(tx *gorm.DB) error {
	rp.UpdatedAt = time.Now()
	return nil
}

