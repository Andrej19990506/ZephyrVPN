package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// CounterpartyType представляет тип контрагента
type CounterpartyType string

const (
	CounterpartyTypeSupplier CounterpartyType = "Supplier" // Поставщик
	CounterpartyTypeService  CounterpartyType = "Service"  // Сервисная компания
	CounterpartyTypeOther    CounterpartyType = "Other"    // Прочее
)

// CounterpartyStatus представляет статус контрагента
type CounterpartyStatus string

const (
	CounterpartyStatusActive   CounterpartyStatus = "Active"   // Активный
	CounterpartyStatusArchived CounterpartyStatus = "Archived" // Архивирован
)

// Counterparty представляет контрагента (поставщика, сервисную компанию и т.д.)
type Counterparty struct {
	ID              string             `json:"id" gorm:"type:uuid;primaryKey"`
	Name            string             `json:"name" gorm:"type:varchar(255);not null"`
	FullLegalName   string             `json:"full_legal_name" gorm:"type:varchar(500)"` // Полное юридическое название
	INN             string             `json:"inn" gorm:"type:varchar(20);uniqueIndex"`   // ИНН (уникальный)
	Type            CounterpartyType    `json:"type" gorm:"type:varchar(50);default:'Supplier'"`
	Status          CounterpartyStatus  `json:"status" gorm:"type:varchar(20);default:'Active';index"`
	
	// Hybrid Logic: балансы для официальных и внутренних операций
	BalanceOfficial float64            `json:"balance_official" gorm:"type:decimal(15,2);default:0"` // Долг по банковским операциям
	BalanceInternal float64            `json:"balance_internal" gorm:"type:decimal(15,2);default:0"` // Долг по наличным операциям
	
	// Дополнительные поля
	HybridMode      bool               `json:"hybrid_mode" gorm:"default:false"` // Режим гибридного учета
	CreditLimit     float64            `json:"credit_limit" gorm:"type:decimal(15,2);default:0"` // Кредитный лимит
	PaymentTerms    string             `json:"payment_terms" gorm:"type:varchar(255)"` // Условия оплаты
	
	// Контактная информация
	ContactPerson   string             `json:"contact_person" gorm:"type:varchar(255)"`
	Phone           string             `json:"phone" gorm:"type:varchar(50)"`
	Email           string             `json:"email" gorm:"type:varchar(255)"`
	Telegram        string             `json:"telegram" gorm:"type:varchar(100)"`
	PaymentMethod   string             `json:"payment_method" gorm:"type:varchar(50)"` // 'bank', 'cash', 'hybrid'
	
	CreatedAt       time.Time          `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt       time.Time          `json:"updated_at" gorm:"autoUpdateTime"`
	DeletedAt       gorm.DeletedAt     `json:"deleted_at,omitempty" gorm:"index"`
}

// TableName указывает имя таблицы
func (Counterparty) TableName() string {
	return "counterparties"
}

// BeforeCreate генерирует UUID
func (c *Counterparty) BeforeCreate(tx *gorm.DB) error {
	if c.ID == "" {
		c.ID = uuid.New().String()
	}
	if c.Type == "" {
		c.Type = CounterpartyTypeSupplier
	}
	if c.Status == "" {
		c.Status = CounterpartyStatusActive
	}
	return nil
}

// GetTotalDebt возвращает общий долг (официальный + внутренний)
func (c *Counterparty) GetTotalDebt() float64 {
	return c.BalanceOfficial + c.BalanceInternal
}

// IsHighDebt проверяет, превышен ли кредитный лимит
func (c *Counterparty) IsHighDebt() bool {
	if c.CreditLimit <= 0 {
		return false // Если лимит не установлен, не считаем высоким долгом
	}
	return c.GetTotalDebt() > c.CreditLimit
}

