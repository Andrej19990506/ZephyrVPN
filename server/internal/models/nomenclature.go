package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// NomenclatureItem представляет товар в номенклатуре
type NomenclatureItem struct {
	ID               string         `json:"id" gorm:"type:uuid;primaryKey"`
	SKU              string         `json:"sku" gorm:"type:varchar(100);uniqueIndex;not null"`
	Name             string         `json:"name" gorm:"type:varchar(255);not null"`
	CategoryID       *string        `json:"category_id" gorm:"type:uuid;index"`
	CategoryName     string         `json:"category_name" gorm:"type:varchar(100)"`
	CategoryColor    string         `json:"category_color" gorm:"type:varchar(7);default:'#10b981'"`
	BaseUnit         string         `json:"base_unit" gorm:"type:varchar(20);not null;default:'kg'"` // kg, l, pcs, box
	InboundUnit      string         `json:"inbound_unit" gorm:"type:varchar(20);not null;default:'kg'"`
	ProductionUnit   string         `json:"production_unit" gorm:"type:varchar(20);not null;default:'g'"`
	ConversionFactor float64        `json:"conversion_factor" gorm:"type:decimal(10,2);default:1.0"`
	MinStockLevel    float64        `json:"min_stock_level" gorm:"type:decimal(10,2);default:0"`
	StorageZone      string         `json:"storage_zone" gorm:"type:varchar(50);default:'dry_storage'"` // fridge, dry_storage, bar, freezer
	LastPrice        float64        `json:"last_price" gorm:"type:decimal(10,2);default:0"`
	IsActive         bool           `json:"is_active" gorm:"default:true"`
	CreatedAt        time.Time      `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt        time.Time      `json:"updated_at" gorm:"autoUpdateTime"`
	DeletedAt        gorm.DeletedAt `json:"deleted_at,omitempty" gorm:"index"`
}

// TableName указывает имя таблицы в БД
func (NomenclatureItem) TableName() string {
	return "nomenclature_items"
}

// BeforeCreate hook для генерации UUID если не указан
func (n *NomenclatureItem) BeforeCreate(tx *gorm.DB) error {
	if n.ID == "" {
		n.ID = uuid.New().String()
	}
	return nil
}

// NomenclatureCategory представляет категорию товаров
type NomenclatureCategory struct {
	ID                string         `json:"id" gorm:"type:uuid;primaryKey"`
	Name              string         `json:"name" gorm:"type:varchar(100);uniqueIndex;not null"`
	Color             string         `json:"color" gorm:"type:varchar(7);default:'#10b981'"`
	DefaultLossRate   float64        `json:"default_loss_rate" gorm:"type:decimal(5,2);default:0"`
	AccountingType    string         `json:"accounting_type" gorm:"type:varchar(20);default:'hybrid'"` // official, internal, hybrid
	ExpenseCategoryID *string        `json:"expense_category_id" gorm:"type:uuid;index"`
	TrackExpiration   bool           `json:"track_expiration" gorm:"default:false"`
	NotifyLowStock    bool           `json:"notify_low_stock" gorm:"default:false"`
	LowStockThreshold float64        `json:"low_stock_threshold" gorm:"type:decimal(10,2);default:0"`
	ParentID          *string        `json:"parent_id" gorm:"type:uuid;index"`
	CreatedAt         time.Time      `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt         time.Time      `json:"updated_at" gorm:"autoUpdateTime"`
	DeletedAt         gorm.DeletedAt `json:"deleted_at,omitempty" gorm:"index"`
}

// TableName указывает имя таблицы в БД
func (NomenclatureCategory) TableName() string {
	return "nomenclature_categories"
}

// BeforeCreate hook для генерации UUID если не указан
func (n *NomenclatureCategory) BeforeCreate(tx *gorm.DB) error {
	if n.ID == "" {
		n.ID = uuid.New().String()
	}
	return nil
}

// ImportValidationResult представляет результат валидации одной строки импорта
type ImportValidationResult struct {
	Row      int                    `json:"row"`
	Item     map[string]interface{} `json:"item"`
	Status   string                 `json:"status"` // success, warning, error
	Errors   []string               `json:"errors"`
	Warnings []string               `json:"warnings"`
}

// ImportResult представляет результат массового импорта
type ImportResult struct {
	ImportedCount int                    `json:"imported_count"`
	ErrorCount    int                    `json:"error_count"`
	WarningCount  int                    `json:"warning_count"`
	Errors        []string               `json:"errors,omitempty"`
	Validation    []ImportValidationResult `json:"validation,omitempty"`
}

