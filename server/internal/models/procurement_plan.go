package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ProcurementPlanStatus представляет статус плана закупок
type ProcurementPlanStatus string

const (
	ProcurementPlanStatusDraft     ProcurementPlanStatus = "draft"     // Черновик
	ProcurementPlanStatusSubmitted ProcurementPlanStatus = "submitted" // Отправлен (созданы PurchaseOrders)
	ProcurementPlanStatusApproved  ProcurementPlanStatus = "approved" // Утвержден
	ProcurementPlanStatusExecuted  ProcurementPlanStatus = "executed" // Выполнен
	ProcurementPlanStatusCancelled ProcurementPlanStatus = "cancelled" // Отменен
)

// ProcurementPlan представляет план закупок на месяц
type ProcurementPlan struct {
	ID          string                `json:"id" gorm:"type:uuid;primaryKey"`
	PlanNumber  string                `json:"plan_number" gorm:"type:varchar(100);uniqueIndex;not null"` // PLAN-2026-02
	BranchID    string                `json:"branch_id" gorm:"type:uuid;not null;index"`
	Branch      *Branch               `gorm:"foreignKey:BranchID" json:"branch,omitempty"`
	Month       time.Time             `json:"month" gorm:"type:date;not null;index"` // Первый день месяца
	Year        int                   `json:"year" gorm:"not null;index"`
	MonthNumber int                   `json:"month_number" gorm:"not null;index"` // 1-12
	
	// Статус плана
	Status      ProcurementPlanStatus `json:"status" gorm:"type:varchar(50);default:'draft';index"`
	
	// Ответственные
	CreatedBy   string                `json:"created_by" gorm:"type:varchar(255);not null"`
	ApprovedBy  *string               `json:"approved_by" gorm:"type:varchar(255)"`
	SubmittedAt *time.Time            `json:"submitted_at" gorm:"type:timestamp"`
	
	// Метаданные
	Notes       string                `json:"notes" gorm:"type:text"`
	CreatedAt   time.Time             `json:"created_at" gorm:"autoCreateTime;index"`
	UpdatedAt   time.Time             `json:"updated_at" gorm:"autoUpdateTime"`
	DeletedAt   gorm.DeletedAt        `json:"deleted_at,omitempty" gorm:"index"`
	
	// Relations
	Items       []ProcurementPlanItem `gorm:"foreignKey:PlanID" json:"items,omitempty"`
}

// TableName указывает имя таблицы
func (ProcurementPlan) TableName() string {
	return "procurement_plans"
}

// BeforeCreate генерирует UUID и устанавливает значения по умолчанию
func (pp *ProcurementPlan) BeforeCreate(tx *gorm.DB) error {
	if pp.ID == "" {
		pp.ID = uuid.New().String()
	}
	if pp.Status == "" {
		pp.Status = ProcurementPlanStatusDraft
	}
	if pp.Month.IsZero() {
		pp.Month = time.Now()
	}
	// Извлекаем год и месяц из даты
	if pp.Year == 0 {
		pp.Year = pp.Month.Year()
	}
	if pp.MonthNumber == 0 {
		pp.MonthNumber = int(pp.Month.Month())
	}
	return nil
}

// ProcurementPlanItem представляет позицию в плане (ячейка матрицы: день × товар)
type ProcurementPlanItem struct {
	ID                   string         `json:"id" gorm:"type:uuid;primaryKey"`
	PlanID               string         `json:"plan_id" gorm:"type:uuid;not null;index"`
	Plan                 *ProcurementPlan `gorm:"foreignKey:PlanID" json:"plan,omitempty"`
	NomenclatureID       string         `json:"nomenclature_id" gorm:"type:uuid;not null;index"`
	Nomenclature         *NomenclatureItem `gorm:"foreignKey:NomenclatureID" json:"nomenclature,omitempty"`
	PlanDate             time.Time      `json:"plan_date" gorm:"type:date;not null;index"` // Конкретная дата в месяце
	
	// Планируемое количество (вводится менеджером)
	PlannedQuantity      float64        `json:"planned_quantity" gorm:"type:decimal(10,2);not null;default:0"`
	Unit                 string         `json:"unit" gorm:"type:varchar(20);not null;default:'kg'"`
	
	// Прогноз спроса (автоматически рассчитанный)
	ForecastedQuantity   float64        `json:"forecasted_quantity" gorm:"type:decimal(10,2);default:0"`
	ForecastConfidence   float64        `json:"forecast_confidence" gorm:"type:decimal(5,2);default:0"` // 0-100%
	PredictedKitchenLoad string         `json:"predicted_kitchen_load" gorm:"type:varchar(20)"` // 'high', 'medium', 'low'
	
	// Исторические данные (для аналитики)
	LastMonthQuantity    float64        `json:"last_month_quantity" gorm:"type:decimal(10,2);default:0"`
	AvgLast3Months       float64        `json:"avg_last_3_months" gorm:"type:decimal(10,2);default:0"`
	
	// Рекомендации по поставщику
	SuggestedSupplierID  *string        `json:"suggested_supplier_id" gorm:"type:uuid;index"`
	SuggestedSupplier    *Counterparty  `gorm:"foreignKey:SuggestedSupplierID" json:"suggested_supplier,omitempty"`
	SuggestedPricePerUnit float64       `json:"suggested_price_per_unit" gorm:"type:decimal(10,2);default:0"`
	
	// Метаданные
	Notes                string         `json:"notes" gorm:"type:text"`
	CreatedAt            time.Time      `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt            time.Time      `json:"updated_at" gorm:"autoUpdateTime"`
}

// TableName указывает имя таблицы
func (ProcurementPlanItem) TableName() string {
	return "procurement_plan_items"
}

// BeforeCreate генерирует UUID
func (ppi *ProcurementPlanItem) BeforeCreate(tx *gorm.DB) error {
	if ppi.ID == "" {
		ppi.ID = uuid.New().String()
	}
	return nil
}

// ProcurementHistory представляет исторические данные закупок
type ProcurementHistory struct {
	ID                string         `json:"id" gorm:"type:uuid;primaryKey"`
	BranchID           string         `json:"branch_id" gorm:"type:uuid;not null;index"`
	Branch             *Branch        `gorm:"foreignKey:BranchID" json:"branch,omitempty"`
	NomenclatureID    string         `json:"nomenclature_id" gorm:"type:uuid;not null;index"`
	Nomenclature      *NomenclatureItem `gorm:"foreignKey:NomenclatureID" json:"nomenclature,omitempty"`
	OrderDate         time.Time      `json:"order_date" gorm:"type:date;not null"`
	DeliveryDate      time.Time      `json:"delivery_date" gorm:"type:date;not null;index"`
	DayOfWeek         int            `json:"day_of_week" gorm:"not null;index"` // 1-7 (понедельник-воскресенье)
	WeekOfMonth       int            `json:"week_of_month" gorm:"not null"` // 1-5
	
	// Данные заказа
	OrderedQuantity   float64        `json:"ordered_quantity" gorm:"type:decimal(10,2);not null"`
	ReceivedQuantity  float64        `json:"received_quantity" gorm:"type:decimal(10,2);not null"`
	Unit              string         `json:"unit" gorm:"type:varchar(20);not null"`
	PurchasePricePerUnit float64      `json:"purchase_price_per_unit" gorm:"type:decimal(10,2);not null"`
	SupplierID        *string        `json:"supplier_id" gorm:"type:uuid;index"`
	Supplier          *Counterparty  `gorm:"foreignKey:SupplierID" json:"supplier,omitempty"`
	
	// Связь с заказом
	PurchaseOrderID   *string        `json:"purchase_order_id" gorm:"type:uuid;index"`
	PurchaseOrder     *PurchaseOrder `gorm:"foreignKey:PurchaseOrderID" json:"purchase_order,omitempty"`
	
	// Метаданные
	CreatedAt         time.Time      `json:"created_at" gorm:"autoCreateTime"`
}

// TableName указывает имя таблицы
func (ProcurementHistory) TableName() string {
	return "procurement_history"
}

// BeforeCreate генерирует UUID и вычисляет day_of_week и week_of_month
func (ph *ProcurementHistory) BeforeCreate(tx *gorm.DB) error {
	if ph.ID == "" {
		ph.ID = uuid.New().String()
	}
	// Вычисляем день недели (1=понедельник, 7=воскресенье)
	if ph.DayOfWeek == 0 {
		weekday := int(ph.DeliveryDate.Weekday())
		if weekday == 0 {
			ph.DayOfWeek = 7 // Воскресенье
		} else {
			ph.DayOfWeek = weekday
		}
	}
	// Вычисляем неделю месяца (1-5)
	if ph.WeekOfMonth == 0 {
		day := ph.DeliveryDate.Day()
		ph.WeekOfMonth = (day-1)/7 + 1
		if ph.WeekOfMonth > 5 {
			ph.WeekOfMonth = 5
		}
	}
	return nil
}

// DemandForecast представляет прогноз спроса
type DemandForecast struct {
	ID                  string         `json:"id" gorm:"type:uuid;primaryKey"`
	BranchID            string         `json:"branch_id" gorm:"type:uuid;not null;index"`
	Branch              *Branch        `gorm:"foreignKey:BranchID" json:"branch,omitempty"`
	NomenclatureID      string         `json:"nomenclature_id" gorm:"type:uuid;not null;index"`
	Nomenclature        *NomenclatureItem `gorm:"foreignKey:NomenclatureID" json:"nomenclature,omitempty"`
	ForecastDate        time.Time      `json:"forecast_date" gorm:"type:date;not null;index"`
	
	// Прогнозируемое количество
	ForecastedQuantity  float64        `json:"forecasted_quantity" gorm:"type:decimal(10,2);not null"`
	Unit                string         `json:"unit" gorm:"type:varchar(20);not null"`
	
	// Метрики прогноза
	ConfidenceScore     float64        `json:"confidence_score" gorm:"type:decimal(5,2);default:0"` // 0-100%
	ForecastMethod      string         `json:"forecast_method" gorm:"type:varchar(50)"` // 'moving_average', 'seasonal', 'ml_model', 'manual'
	PredictedKitchenLoad string        `json:"predicted_kitchen_load" gorm:"type:varchar(20)"` // 'high', 'medium', 'low'
	
	// Факторы влияния
	SeasonalFactor      float64        `json:"seasonal_factor" gorm:"type:decimal(5,2);default:1.0"`
	TrendFactor         float64        `json:"trend_factor" gorm:"type:decimal(5,2);default:1.0"`
	DayOfWeekFactor     float64        `json:"day_of_week_factor" gorm:"type:decimal(5,2);default:1.0"`
	
	// Метаданные
	CalculatedAt        time.Time      `json:"calculated_at" gorm:"autoCreateTime"`
	ValidUntil         *time.Time      `json:"valid_until" gorm:"type:timestamp;index"`
}

// TableName указывает имя таблицы
func (DemandForecast) TableName() string {
	return "demand_forecasts"
}

// BeforeCreate генерирует UUID
func (df *DemandForecast) BeforeCreate(tx *gorm.DB) error {
	if df.ID == "" {
		df.ID = uuid.New().String()
	}
	return nil
}



