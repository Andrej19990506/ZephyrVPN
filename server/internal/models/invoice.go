package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// InvoiceStatus представляет статус накладной
type InvoiceStatus string

const (
	InvoiceStatusDraft     InvoiceStatus = "draft"     // Черновик
	InvoiceStatusCompleted InvoiceStatus = "completed"  // Завершена
	InvoiceStatusCancelled InvoiceStatus = "cancelled" // Отменена
)

// Invoice представляет входящую накладную (Source of Truth)
type Invoice struct {
	ID            string        `json:"id" gorm:"type:uuid;primaryKey"`
	Number        string        `json:"number" gorm:"type:varchar(100);not null;index"` // Внешний номер накладной
	CounterpartyID *string      `json:"counterparty_id" gorm:"type:uuid;index"` // Контрагент (поставщик)
	Counterparty  *Counterparty `gorm:"foreignKey:CounterpartyID" json:"counterparty,omitempty"`
	TotalAmount   float64       `json:"total_amount" gorm:"type:decimal(15,2);not null"` // Общая сумма накладной
	Status        InvoiceStatus `json:"status" gorm:"type:varchar(20);default:'draft';index"` // Статус накладной
	BranchID      string        `json:"branch_id" gorm:"type:uuid;not null;index"` // Филиал
	Branch        *Branch       `gorm:"foreignKey:BranchID" json:"branch,omitempty"`
	InvoiceDate   time.Time     `json:"invoice_date" gorm:"not null;index"` // Дата накладной
	IsPaidCash    bool          `json:"is_paid_cash" gorm:"default:false"` // Оплачено наличными
	PerformedBy   string        `json:"performed_by" gorm:"type:varchar(255)"` // Кто обработал накладную
	Notes         string        `json:"notes" gorm:"type:text"` // Дополнительные заметки
	CreatedAt     time.Time     `json:"created_at" gorm:"autoCreateTime;index"`
	UpdatedAt     time.Time     `json:"updated_at" gorm:"autoUpdateTime"`
	DeletedAt     gorm.DeletedAt `json:"deleted_at,omitempty" gorm:"index"`

	// Relations (для удобства доступа)
	StockBatches   []StockBatch        `gorm:"foreignKey:InvoiceID" json:"stock_batches,omitempty"`
	StockMovements  []StockMovement     `gorm:"foreignKey:InvoiceID" json:"stock_movements,omitempty"`
	FinanceTransaction *FinanceTransaction `gorm:"foreignKey:InvoiceID" json:"finance_transaction,omitempty"`
}

// TableName указывает имя таблицы
func (Invoice) TableName() string {
	return "invoices"
}

// BeforeCreate генерирует UUID и устанавливает значения по умолчанию
func (i *Invoice) BeforeCreate(tx *gorm.DB) error {
	if i.ID == "" {
		i.ID = uuid.New().String()
	}
	if i.Status == "" {
		i.Status = InvoiceStatusDraft
	}
	if i.InvoiceDate.IsZero() {
		i.InvoiceDate = time.Now()
	}
	return nil
}

// IsDraft проверяет, является ли накладная черновиком
func (i *Invoice) IsDraft() bool {
	return i.Status == InvoiceStatusDraft
}

// IsCompleted проверяет, завершена ли накладная
func (i *Invoice) IsCompleted() bool {
	return i.Status == InvoiceStatusCompleted
}

// IsCancelled проверяет, отменена ли накладная
func (i *Invoice) IsCancelled() bool {
	return i.Status == InvoiceStatusCancelled
}

