package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// LegalEntity представляет юридическое лицо (ИП, ООО и т.д.)
type LegalEntity struct {
	ID          string         `json:"id" gorm:"type:uuid;primaryKey"`
	Name        string         `json:"name" gorm:"type:varchar(255);not null;uniqueIndex"`
	INN         string         `json:"inn" gorm:"type:varchar(12);uniqueIndex"` // ИНН
	Type        string         `json:"type" gorm:"type:varchar(50);default:'IP'"` // IP, OOO, etc
	IsActive    bool           `json:"is_active" gorm:"default:true"`
	CreatedAt   time.Time      `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt   time.Time      `json:"updated_at" gorm:"autoUpdateTime"`
	DeletedAt   gorm.DeletedAt  `json:"deleted_at,omitempty" gorm:"index"`
}

// TableName указывает имя таблицы
func (LegalEntity) TableName() string {
	return "legal_entities"
}

// BeforeCreate генерирует UUID
func (le *LegalEntity) BeforeCreate(tx *gorm.DB) error {
	if le.ID == "" {
		le.ID = uuid.New().String()
	}
	return nil
}

