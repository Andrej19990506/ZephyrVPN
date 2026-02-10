package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// SupplierCatalogItem представляет товар в каталоге поставщика
// Это связующая таблица между NomenclatureItem и Counterparty (поставщиком)
// Один товар может быть у нескольких поставщиков с разными ценами
type SupplierCatalogItem struct {
	ID               string         `json:"id" gorm:"type:uuid;primaryKey"`
	NomenclatureID   string         `json:"nomenclature_id" gorm:"type:uuid;not null;index"`
	Nomenclature     *NomenclatureItem `gorm:"foreignKey:NomenclatureID" json:"nomenclature,omitempty"`
	SupplierID       string         `json:"supplier_id" gorm:"type:uuid;not null;index"`
	Supplier         *Counterparty  `gorm:"foreignKey:SupplierID" json:"supplier,omitempty"`
	BranchID         string         `json:"branch_id" gorm:"type:uuid;index"` // Для какого филиала этот каталог
	
	// Данные из каталога
	Brand                string         `json:"brand" gorm:"type:varchar(255)"`              // Бренд
	InputUnit            string         `json:"input_unit" gorm:"type:varchar(50)"`          // Единица измерения для заказа (упак, кг и т.д.) - DEPRECATED
	InputUOM             string         `json:"input_uom" gorm:"type:varchar(255)"`         // Единица измерения поставщика (текст) - DEPRECATED, используйте UoMRuleID
	ConversionMultiplier float64        `json:"conversion_multiplier" gorm:"type:decimal(10,4);default:1.0"` // Множитель конвертации - DEPRECATED, используйте UoMRuleID
	UoMRuleID            *string        `json:"uom_rule_id" gorm:"type:uuid;index"`          // ID правила конвертации единиц измерения
	UoMRule              *UoMConversionRule `gorm:"foreignKey:UoMRuleID" json:"uom_rule,omitempty"` // Правило конвертации
	Price                float64        `json:"price" gorm:"type:decimal(10,2);default:0"`    // Цена за единицу
	MinOrderBatch        float64        `json:"min_order_batch" gorm:"type:decimal(10,2);default:0"` // Минимальная партия
	IsActive             bool           `json:"is_active" gorm:"default:true"`                 // Активен ли этот товар у поставщика
	
	// Метаданные
	LastOrderDate    *time.Time     `json:"last_order_date" gorm:"type:timestamp"`        // Дата последнего заказа
	LastOrderPrice   float64        `json:"last_order_price" gorm:"type:decimal(10,2);default:0"` // Цена последнего заказа
	Notes            string         `json:"notes" gorm:"type:text"`                       // Заметки
	
	CreatedAt        time.Time      `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt        time.Time      `json:"updated_at" gorm:"autoUpdateTime"`
	DeletedAt        gorm.DeletedAt `json:"deleted_at,omitempty" gorm:"index"`
}

// TableName указывает имя таблицы
func (SupplierCatalogItem) TableName() string {
	return "supplier_catalog_items"
}

// BeforeCreate генерирует UUID
func (s *SupplierCatalogItem) BeforeCreate(tx *gorm.DB) error {
	if s.ID == "" {
		s.ID = uuid.New().String()
	}
	return nil
}


