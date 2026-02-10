package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// StockBatch представляет партию товара с отслеживанием срока годности
type StockBatch struct {
	ID                string         `json:"id" gorm:"type:uuid;primaryKey"`
	NomenclatureID    string         `json:"nomenclature_id" gorm:"type:uuid;not null;index"`
	Nomenclature      NomenclatureItem `gorm:"foreignKey:NomenclatureID" json:"nomenclature,omitempty"`
	BranchID          string         `json:"branch_id" gorm:"type:uuid;not null;index"`
	Quantity          float64        `json:"quantity" gorm:"type:decimal(10,2);not null"`
	Unit              string         `json:"unit" gorm:"type:varchar(20);not null"`
	CostPerUnit       float64        `json:"cost_per_unit" gorm:"type:decimal(10,2);default:0"`
	CreatedAt         time.Time      `json:"created_at" gorm:"autoCreateTime;index"`
	ExpiryAt          *time.Time     `json:"expiry_at" gorm:"index"` // NULL если срок годности не отслеживается
	Source            string         `json:"source" gorm:"type:varchar(50)"` // 'invoice', 'production', 'adjustment'
	SourceReferenceID *string        `json:"source_reference_id" gorm:"type:uuid"` // ID накладной, производства и т.д. (deprecated, используйте InvoiceID)
	InvoiceID         *string        `json:"invoice_id" gorm:"type:uuid;index"` // FK на invoices (для накладных)
	Invoice           *Invoice       `gorm:"foreignKey:InvoiceID" json:"invoice,omitempty"`
	RemainingQuantity float64        `json:"remaining_quantity" gorm:"type:decimal(10,2);not null"` // Остаток после списаний
	IsExpired         bool           `json:"is_expired" gorm:"default:false;index"`
	UpdatedAt         time.Time      `json:"updated_at" gorm:"autoUpdateTime"`
	DeletedAt         gorm.DeletedAt `json:"deleted_at,omitempty" gorm:"index"`

	// Virtual fields for UI
	DaysUntilExpiry   int     `json:"days_until_expiry" gorm:"-"`
	HoursUntilExpiry   float64 `json:"hours_until_expiry" gorm:"-"`
	IsAtRisk          bool    `json:"is_at_risk" gorm:"-"` // Риск испортиться до продажи
	SalesVelocity     float64 `json:"sales_velocity" gorm:"-"` // Скорость продаж (ед/день)
}

// TableName указывает имя таблицы
func (StockBatch) TableName() string {
	return "stock_batches"
}

// BeforeCreate генерирует UUID
func (s *StockBatch) BeforeCreate(tx *gorm.DB) error {
	if s.ID == "" {
		s.ID = uuid.New().String()
	}
	if s.RemainingQuantity == 0 {
		s.RemainingQuantity = s.Quantity
	}
	return nil
}

// Recipe представляет рецепт/технологическую карту блюда
type Recipe struct {
	ID             string         `json:"id" gorm:"type:uuid;primaryKey"`
	Name           string         `json:"name" gorm:"type:varchar(255);not null"`
	Description    string         `json:"description" gorm:"type:text"`
	MenuItemID     *string        `json:"menu_item_id" gorm:"type:uuid;index"` // Связь с позицией меню
	StationIDs     string         `json:"station_ids" gorm:"type:text"` // JSON массив ID станций, через которые проходит блюдо (обязательно, например: ["station-uuid-1", "station-uuid-2"])
	PortionSize    float64        `json:"portion_size" gorm:"type:decimal(10,2);default:1"` // Количество порций
	Unit           string         `json:"unit" gorm:"type:varchar(20);default:'pcs'"` // Единица измерения порции
	IsSemiFinished bool           `json:"is_semi_finished" gorm:"default:false"` // Флаг полуфабриката
	IsActive       bool           `json:"is_active" gorm:"default:true"`
	// Recipe Book fields (Frontend Knowledge Base)
	InstructionText string        `json:"instruction_text" gorm:"type:text"` // Пошаговая инструкция в Markdown
	VideoURL        string        `json:"video_url" gorm:"type:text"` // Ссылка на видео в S3
	PhotoURLs       string        `json:"photo_urls" gorm:"type:jsonb"` // JSONB массив ссылок на фото в S3 (оптимизировано для индексации)
	CreatedAt      time.Time      `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt      time.Time      `json:"updated_at" gorm:"autoUpdateTime"`
	DeletedAt      gorm.DeletedAt `json:"deleted_at,omitempty" gorm:"index"`

	// Relations
	Ingredients []RecipeIngredient `json:"ingredients" gorm:"foreignKey:RecipeID"`
}

// TableName указывает имя таблицы
func (Recipe) TableName() string {
	return "recipes"
}

// BeforeCreate генерирует UUID
func (r *Recipe) BeforeCreate(tx *gorm.DB) error {
	if r.ID == "" {
		r.ID = uuid.New().String()
	}
	return nil
}

// GetStationIDs возвращает массив ID станций из JSON строки
func (r *Recipe) GetStationIDs() ([]string, error) {
	if r.StationIDs == "" {
		return []string{}, nil
	}
	var stationIDs []string
	if err := json.Unmarshal([]byte(r.StationIDs), &stationIDs); err != nil {
		return nil, err
	}
	return stationIDs, nil
}

// SetStationIDs устанавливает массив ID станций в JSON строку
func (r *Recipe) SetStationIDs(stationIDs []string) error {
	if len(stationIDs) == 0 {
		r.StationIDs = "[]"
		return nil
	}
	data, err := json.Marshal(stationIDs)
	if err != nil {
		return err
	}
	r.StationIDs = string(data)
	return nil
}

// RecipeIngredient представляет ингредиент в рецепте (BOM)
type RecipeIngredient struct {
	ID                string    `json:"id" gorm:"type:uuid;primaryKey"`
	RecipeID          string    `json:"recipe_id" gorm:"type:uuid;not null;index"`
	NomenclatureID    *string   `json:"nomenclature_id" gorm:"type:uuid;index"` // NULL если это вложенный рецепт
	Nomenclature      *NomenclatureItem `gorm:"foreignKey:NomenclatureID" json:"nomenclature,omitempty"`
	IngredientRecipeID *string  `json:"ingredient_recipe_id" gorm:"type:uuid;index"` // NULL если это сырье, UUID рецепта если это полуфабрикат
	IngredientRecipe  *Recipe    `gorm:"foreignKey:IngredientRecipeID" json:"ingredient_recipe,omitempty"` // Связь с рецептом-полуфабрикатом
	Quantity          float64    `json:"quantity" gorm:"type:decimal(10,4);not null"` // Количество на 1 порцию в единицах измерения товара
	Unit              string     `json:"unit" gorm:"type:varchar(20);not null;default:'g'"` // Единица измерения (берется из номенклатуры: g, kg, pcs, l, ml и т.д.)
	IsOptional        bool       `json:"is_optional" gorm:"default:false"` // Опциональный ингредиент
	CreatedAt         time.Time  `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt         time.Time  `json:"updated_at" gorm:"autoUpdateTime"`
}

// TableName указывает имя таблицы
func (RecipeIngredient) TableName() string {
	return "recipe_ingredients"
}

// BeforeCreate генерирует UUID
func (ri *RecipeIngredient) BeforeCreate(tx *gorm.DB) error {
	if ri.ID == "" {
		ri.ID = uuid.New().String()
	}
	return nil
}

// RecipeNode представляет узел в иерархической структуре папок для рецептов
type RecipeNode struct {
	ID          string    `json:"id" gorm:"type:uuid;primaryKey"`
	Name        string    `json:"name" gorm:"type:varchar(255);not null"`
	ParentID    *string   `json:"parent_id" gorm:"type:uuid;index"` // NULL для корневого уровня
	IsFolder    bool      `json:"is_folder" gorm:"default:false;index"` // true для папки, false для рецепта
	RecipeID    *string   `json:"recipe_id" gorm:"type:uuid;index"` // NULL для папок, UUID рецепта для узлов-рецептов
	Recipe      *Recipe   `gorm:"foreignKey:RecipeID" json:"recipe,omitempty"` // Связь с рецептом
	GridCol     *int      `json:"grid_col" gorm:"type:integer;default:0"` // Позиция в сетке (колонка)
	GridRow     *int      `json:"grid_row" gorm:"type:integer;default:0"` // Позиция в сетке (строка)
	CreatedAt   time.Time `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt   time.Time `json:"updated_at" gorm:"autoUpdateTime"`
	DeletedAt   gorm.DeletedAt `json:"deleted_at,omitempty" gorm:"index"`
	
	// Virtual fields
	Parent      *RecipeNode   `gorm:"foreignKey:ParentID" json:"parent,omitempty"`
	Children    []RecipeNode  `gorm:"foreignKey:ParentID" json:"children,omitempty"`
	ChildrenCount int         `json:"children_count" gorm:"-"` // Количество дочерних элементов
}

// TableName указывает имя таблицы
func (RecipeNode) TableName() string {
	return "recipe_nodes"
}

// BeforeCreate генерирует UUID
func (rn *RecipeNode) BeforeCreate(tx *gorm.DB) error {
	if rn.ID == "" {
		rn.ID = uuid.New().String()
	}
	return nil
}

// StockMovement представляет движение остатков (списание, оприходование)
type StockMovement struct {
	ID                string         `json:"id" gorm:"type:uuid;primaryKey"`
	StockBatchID      *string        `json:"stock_batch_id" gorm:"type:uuid;index"` // NULL для движений без привязки к партии
	Batch             *StockBatch    `gorm:"foreignKey:StockBatchID" json:"batch,omitempty"`
	NomenclatureID    string         `json:"nomenclature_id" gorm:"type:uuid;not null;index"`
	Nomenclature      NomenclatureItem `gorm:"foreignKey:NomenclatureID" json:"nomenclature,omitempty"`
	BranchID          string         `json:"branch_id" gorm:"type:uuid;not null;index"`
	Quantity          float64        `json:"quantity" gorm:"type:decimal(10,2);not null"` // Положительное = приход, отрицательное = расход
	Unit              string         `json:"unit" gorm:"type:varchar(20);not null"`
	MovementType      string         `json:"movement_type" gorm:"type:varchar(50);not null;index"` // 'sale', 'production', 'waste', 'adjustment', 'invoice'
	SourceReferenceID *string        `json:"source_reference_id" gorm:"type:uuid"` // ID продажи, производства, накладной (deprecated, используйте InvoiceID)
	InvoiceID         *string        `json:"invoice_id" gorm:"type:uuid;index"` // FK на invoices (для накладных)
	Invoice           *Invoice      `gorm:"foreignKey:InvoiceID" json:"invoice,omitempty"`
	PerformedBy       string         `json:"performed_by" gorm:"type:varchar(255)"` // Username или ID пользователя
	Notes             string         `json:"notes" gorm:"type:text"`
	CreatedAt         time.Time      `json:"created_at" gorm:"autoCreateTime;index"`
	DeletedAt         gorm.DeletedAt `json:"deleted_at,omitempty" gorm:"index"`
}

// TableName указывает имя таблицы
func (StockMovement) TableName() string {
	return "stock_movements"
}

// BeforeCreate генерирует UUID
func (sm *StockMovement) BeforeCreate(tx *gorm.DB) error {
	if sm.ID == "" {
		sm.ID = uuid.New().String()
	}
	return nil
}

// ExpiryAlert представляет уведомление о сроке годности
type ExpiryAlert struct {
	ID            string    `json:"id" gorm:"type:uuid;primaryKey"`
	StockBatchID  string    `json:"stock_batch_id" gorm:"type:uuid;not null;index"`
	Batch         StockBatch `gorm:"foreignKey:StockBatchID" json:"batch,omitempty"`
	AlertType     string    `json:"alert_type" gorm:"type:varchar(20);not null;index"` // 'warning' (3 часа до), 'critical' (просрочено)
	ExpiresAt     time.Time `json:"expires_at" gorm:"index"`
	IsRead        bool      `json:"is_read" gorm:"default:false;index"`
	IsResolved    bool      `json:"is_resolved" gorm:"default:false;index"` // Решено (товар списан или продан)
	CreatedAt     time.Time `json:"created_at" gorm:"autoCreateTime;index"`
	ResolvedAt    *time.Time `json:"resolved_at"`
}

// TableName указывает имя таблицы
func (ExpiryAlert) TableName() string {
	return "expiry_alerts"
}

// BeforeCreate генерирует UUID
func (ea *ExpiryAlert) BeforeCreate(tx *gorm.DB) error {
	if ea.ID == "" {
		ea.ID = uuid.New().String()
	}
	return nil
}


