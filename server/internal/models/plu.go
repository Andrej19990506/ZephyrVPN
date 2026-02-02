package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// PLUCode представляет стандартный PLU код (IFPS - International Federation for Produce Standards)
type PLUCode struct {
	ID          string         `gorm:"type:uuid;primaryKey" json:"id"`
	PLU         string         `gorm:"type:varchar(10);not null;uniqueIndex" json:"plu"` // 4-5 цифр
	Name        string         `gorm:"type:varchar(255);not null;index" json:"name"`    // Название продукта
	NameEN      string         `gorm:"type:varchar(255)" json:"name_en"`                // Английское название
	Category    string         `gorm:"type:varchar(100)" json:"category"`              // Категория (Fruit, Vegetable, etc.)
	Variety     string         `gorm:"type:varchar(100)" json:"variety"`                // Сорт/разновидность
	IsOrganic   bool           `gorm:"default:false" json:"is_organic"`                 // Органический продукт
	IsGMO       bool           `gorm:"default:false" json:"is_gmo"`                      // ГМО продукт
	Description string         `gorm:"type:text" json:"description"`                    // Описание
	CreatedAt   time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt   time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName указывает имя таблицы в БД
func (PLUCode) TableName() string {
	return "plu_codes"
}

// BeforeCreate hook для генерации UUID если не указан
func (p *PLUCode) BeforeCreate(tx *gorm.DB) error {
	if p.ID == "" {
		p.ID = uuid.New().String()
	}
	return nil
}


