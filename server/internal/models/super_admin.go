package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// SuperAdmin представляет супер-администратора системы
type SuperAdmin struct {
	ID           string         `json:"id" gorm:"type:uuid;primaryKey"`
	Username     string         `json:"username" gorm:"type:varchar(100);uniqueIndex;not null"`
	PasswordHash string         `json:"-" gorm:"type:varchar(255);not null"` // Хеш пароля (не возвращается в JSON)
	LegalEntityID *string       `json:"legal_entity_id" gorm:"type:uuid;index"` // ИП под которым работает админ
	IsActive     bool           `json:"is_active" gorm:"default:true"`
	LastLoginAt  *time.Time     `json:"last_login_at" gorm:"type:timestamp"`
	CreatedAt    time.Time      `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt    time.Time      `json:"updated_at" gorm:"autoUpdateTime"`
	DeletedAt    gorm.DeletedAt `json:"deleted_at,omitempty" gorm:"index"`
}

// TableName указывает имя таблицы
func (SuperAdmin) TableName() string {
	return "super_admins"
}

// BeforeCreate генерирует UUID
func (sa *SuperAdmin) BeforeCreate(tx *gorm.DB) error {
	if sa.ID == "" {
		sa.ID = uuid.New().String()
	}
	return nil
}

