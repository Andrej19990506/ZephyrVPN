package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// UoMConversionRule представляет правило конвертации единиц измерения
// Например: "Упак (6 шт х 2 л)" -> 6 штук, "Килограмм" -> 1000 грамм
type UoMConversionRule struct {
	ID          string    `json:"id" gorm:"type:uuid;primaryKey"`
	Name        string    `json:"name" gorm:"type:varchar(255);not null"`        // Название правила (например: "Упак (6 шт х 2 л)")
	Description string    `json:"description" gorm:"type:text"`                  // Описание правила
	InputUOM    string    `json:"input_uom" gorm:"type:varchar(255);not null"`  // Единица измерения поставщика (текст)
	BaseUnit    string    `json:"base_unit" gorm:"type:varchar(50);not null"`   // Базовая единица склада (g, kg, l, ml, pcs)
	Multiplier  float64   `json:"multiplier" gorm:"type:decimal(10,4);not null"` // Множитель конвертации
	IsActive    bool      `json:"is_active" gorm:"default:true"`                 // Активно ли правило
	IsDefault   bool      `json:"is_default" gorm:"default:false"`               // Правило по умолчанию
	
	CreatedAt   time.Time      `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt   time.Time      `json:"updated_at" gorm:"autoUpdateTime"`
	DeletedAt   gorm.DeletedAt `json:"deleted_at,omitempty" gorm:"index"`
}

// TableName указывает имя таблицы
func (UoMConversionRule) TableName() string {
	return "uom_conversion_rules"
}

// BeforeCreate генерирует UUID
func (u *UoMConversionRule) BeforeCreate(tx *gorm.DB) error {
	if u.ID == "" {
		u.ID = uuid.New().String()
	}
	return nil
}


