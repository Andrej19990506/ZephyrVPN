package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// TransactionType представляет тип финансовой транзакции
type TransactionType string

const (
	TransactionTypeExpense    TransactionType = "expense"    // Расход
	TransactionTypeIncome     TransactionType = "income"     // Доход
	TransactionTypeTransfer   TransactionType = "transfer"   // Перевод
	TransactionTypeInvoice    TransactionType = "invoice"   // Накладная
	TransactionTypePayment    TransactionType = "payment"    // Платеж
)

// TransactionSource представляет источник транзакции
type TransactionSource string

const (
	TransactionSourceBank   TransactionSource = "bank"   // Банк
	TransactionSourceCash  TransactionSource = "cash"   // Наличные
	TransactionSourceHybrid TransactionSource = "hybrid" // Гибридный
)

// TransactionStatus представляет статус транзакции
type TransactionStatus string

const (
	TransactionStatusPending   TransactionStatus = "Pending"   // Ожидает обработки
	TransactionStatusCompleted TransactionStatus = "Completed" // Завершена
	TransactionStatusCancelled TransactionStatus = "Cancelled" // Отменена
)

// FinanceTransaction представляет финансовую транзакцию
type FinanceTransaction struct {
	ID              string            `json:"id" gorm:"type:uuid;primaryKey"`
	Date            time.Time         `json:"date" gorm:"not null;index"`
	Type            TransactionType   `json:"type" gorm:"type:varchar(50);not null;index"`
	Category        string            `json:"category" gorm:"type:varchar(100)"` // Категория расхода/дохода
	Amount          float64           `json:"amount" gorm:"type:decimal(15,2);not null"`
	Description     string            `json:"description" gorm:"type:text"`
	BranchID        string            `json:"branch_id" gorm:"type:uuid;index"`
	Source          TransactionSource  `json:"source" gorm:"type:varchar(20);not null;index"` // 'bank', 'cash', 'hybrid'
	Status          TransactionStatus `json:"status" gorm:"type:varchar(20);default:'Completed';index"`
	
	// Связи
	CounterpartyID  *string           `json:"counterparty_id" gorm:"type:uuid;index"` // Контрагент (для накладных)
	Counterparty    *Counterparty     `gorm:"foreignKey:CounterpartyID" json:"counterparty,omitempty"`
	BankAccountID   *string           `json:"bank_account_id" gorm:"type:uuid;index"` // Банковский счет
	LegalEntityID   *string           `json:"legal_entity_id" gorm:"type:uuid;index"` // Юридическое лицо
	
	// Референсы
	InvoiceID       *string           `json:"invoice_id" gorm:"type:uuid;index"` // FK на invoices (для связи с inbound invoice)
	Invoice         *Invoice          `gorm:"foreignKey:InvoiceID" json:"invoice,omitempty"`
	ReferenceID     *string           `json:"reference_id" gorm:"type:uuid;index"` // Референс на другую транзакцию
	
	// Дополнительные поля
	PerformedBy     string            `json:"performed_by" gorm:"type:varchar(255)"` // Кто выполнил операцию
	Responsible     string            `json:"responsible" gorm:"type:varchar(255)"` // Ответственный
	StaffID         *string           `json:"staff_id" gorm:"type:uuid;index"` // ID сотрудника (для расходов на персонал)
	ReceiptPhoto    string            `json:"receipt_photo" gorm:"type:text"` // URL или base64 фото чека
	
	CreatedAt       time.Time         `json:"created_at" gorm:"autoCreateTime;index"`
	UpdatedAt       time.Time         `json:"updated_at" gorm:"autoUpdateTime"`
	DeletedAt       gorm.DeletedAt    `json:"deleted_at,omitempty" gorm:"index"`
}

// TableName указывает имя таблицы
func (FinanceTransaction) TableName() string {
	return "finance_transactions"
}

// BeforeCreate генерирует UUID
func (ft *FinanceTransaction) BeforeCreate(tx *gorm.DB) error {
	if ft.ID == "" {
		ft.ID = uuid.New().String()
	}
	if ft.Status == "" {
		ft.Status = TransactionStatusCompleted
	}
	return nil
}

// IsPending проверяет, является ли транзакция ожидающей
func (ft *FinanceTransaction) IsPending() bool {
	return ft.Status == TransactionStatusPending
}

// IsBankOperation проверяет, является ли транзакция банковской операцией
func (ft *FinanceTransaction) IsBankOperation() bool {
	return ft.Source == TransactionSourceBank || ft.Source == TransactionSourceHybrid
}

