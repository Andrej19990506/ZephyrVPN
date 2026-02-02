package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Branch представляет филиал/точку продаж
type Branch struct {
	ID            string         `json:"id" gorm:"type:uuid;primaryKey"`
	Name          string         `json:"name" gorm:"type:varchar(255);not null"`
	Address       string         `json:"address" gorm:"type:text"`
	Phone         string         `json:"phone" gorm:"type:varchar(50)"`
	Email         string         `json:"email" gorm:"type:varchar(255)"`
	LegalEntityID *string        `json:"legal_entity_id" gorm:"type:uuid;index;not null"` // Связь с ИП (один ко многим)
	SuperAdminID  *string        `json:"super_admin_id" gorm:"type:uuid;index"`              // Связь с аккаунтом (опционально)
	IsActive      bool           `json:"is_active" gorm:"default:true"`
	CreatedAt     time.Time      `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt     time.Time      `json:"updated_at" gorm:"autoUpdateTime"`
	DeletedAt     gorm.DeletedAt `json:"deleted_at,omitempty" gorm:"index"`

	// Связи
	LegalEntity *LegalEntity `json:"legal_entity,omitempty" gorm:"foreignKey:LegalEntityID;references:ID"`
	SuperAdmin  *SuperAdmin   `json:"super_admin,omitempty" gorm:"foreignKey:SuperAdminID;references:ID"`
}

// TableName указывает имя таблицы
func (Branch) TableName() string {
	return "branches"
}

// BeforeCreate генерирует UUID
func (b *Branch) BeforeCreate(tx *gorm.DB) error {
	if b.ID == "" {
		b.ID = uuid.New().String()
	}
	return nil
}
