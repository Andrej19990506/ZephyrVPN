package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// PurchaseOrderStatus представляет статус заказа на закупку
type PurchaseOrderStatus string

const (
	PurchaseOrderStatusDraft            PurchaseOrderStatus = "draft"             // Черновик
	PurchaseOrderStatusOrdered          PurchaseOrderStatus = "ordered"           // Отправлен поставщику
	PurchaseOrderStatusPartiallyReceived PurchaseOrderStatus = "partially_received" // Частично получен
	PurchaseOrderStatusReceived         PurchaseOrderStatus = "received"          // Получен полностью
	PurchaseOrderStatusCancelled        PurchaseOrderStatus = "cancelled"         // Отменен
)

// PurchaseOrder представляет заказ на закупку товаров у поставщика
type PurchaseOrder struct {
	ID                  string              `json:"id" gorm:"type:uuid;primaryKey"`
	OrderNumber         string              `json:"order_number" gorm:"type:varchar(100);uniqueIndex;not null"` // PO-2026-001
	SupplierID          string              `json:"supplier_id" gorm:"type:uuid;not null;index"`
	Supplier            *Counterparty        `gorm:"foreignKey:SupplierID" json:"supplier,omitempty"`
	BranchID            string              `json:"branch_id" gorm:"type:uuid;not null;index"`
	Branch              *Branch             `gorm:"foreignKey:BranchID" json:"branch,omitempty"`
	
	// Статус заказа
	Status              PurchaseOrderStatus `json:"status" gorm:"type:varchar(50);default:'draft';index"`
	
	// Даты
	OrderDate           time.Time          `json:"order_date" gorm:"type:date;not null;index"` // Дата создания заказа
	ExpectedDeliveryDate time.Time          `json:"expected_delivery_date" gorm:"type:date;not null;index"` // Ожидаемая дата доставки
	ActualDeliveryDate   *time.Time         `json:"actual_delivery_date" gorm:"type:date"` // Фактическая дата доставки
	
	// Финансовые данные
	TotalAmount          float64            `json:"total_amount" gorm:"type:decimal(15,2);not null;default:0"`
	Currency             string             `json:"currency" gorm:"type:varchar(3);default:'RUB'"`
	
	// Условия оплаты
	PaymentTerms         string             `json:"payment_terms" gorm:"type:varchar(255)"`
	PaymentMethod        string             `json:"payment_method" gorm:"type:varchar(50);default:'bank'"` // 'bank', 'cash', 'hybrid'
	
	// Ответственные лица
	CreatedBy            string             `json:"created_by" gorm:"type:varchar(255);not null"` // Username менеджера
	ApprovedBy           *string            `json:"approved_by" gorm:"type:varchar(255)"` // Username утвердившего
	ReceivedBy           *string            `json:"received_by" gorm:"type:varchar(255)"` // Username складского работника
	
	// Связь с накладной (создается автоматически при получении)
	InvoiceID            *string            `json:"invoice_id" gorm:"type:uuid;index"`
	Invoice               *Invoice           `gorm:"foreignKey:InvoiceID" json:"invoice,omitempty"`
	
	// Дополнительная информация
	Notes                 string             `json:"notes" gorm:"type:text"` // Заметки менеджера
	InternalNotes         string             `json:"internal_notes" gorm:"type:text"` // Внутренние заметки
	
	// Метаданные
	CreatedAt             time.Time          `json:"created_at" gorm:"autoCreateTime;index"`
	UpdatedAt             time.Time          `json:"updated_at" gorm:"autoUpdateTime"`
	DeletedAt              gorm.DeletedAt    `json:"deleted_at,omitempty" gorm:"index"`
	
	// Relations
	Items                 []PurchaseOrderItem `gorm:"foreignKey:PurchaseOrderID" json:"items,omitempty"`
}

// TableName указывает имя таблицы
func (PurchaseOrder) TableName() string {
	return "purchase_orders"
}

// BeforeCreate генерирует UUID и устанавливает значения по умолчанию
func (po *PurchaseOrder) BeforeCreate(tx *gorm.DB) error {
	if po.ID == "" {
		po.ID = uuid.New().String()
	}
	if po.Status == "" {
		po.Status = PurchaseOrderStatusDraft
	}
	if po.OrderDate.IsZero() {
		po.OrderDate = time.Now()
	}
	if po.Currency == "" {
		po.Currency = "RUB"
	}
	if po.PaymentMethod == "" {
		po.PaymentMethod = "bank"
	}
	return nil
}

// IsDraft проверяет, является ли заказ черновиком
func (po *PurchaseOrder) IsDraft() bool {
	return po.Status == PurchaseOrderStatusDraft
}

// IsOrdered проверяет, отправлен ли заказ поставщику
func (po *PurchaseOrder) IsOrdered() bool {
	return po.Status == PurchaseOrderStatusOrdered
}

// IsReceived проверяет, получен ли заказ полностью
func (po *PurchaseOrder) IsReceived() bool {
	return po.Status == PurchaseOrderStatusReceived
}

// IsPartiallyReceived проверяет, получен ли заказ частично
func (po *PurchaseOrder) IsPartiallyReceived() bool {
	return po.Status == PurchaseOrderStatusPartiallyReceived
}

// IsCancelled проверяет, отменен ли заказ
func (po *PurchaseOrder) IsCancelled() bool {
	return po.Status == PurchaseOrderStatusCancelled
}

// IsOverdue проверяет, просрочен ли заказ (ожидаемая дата доставки прошла, но заказ не получен)
func (po *PurchaseOrder) IsOverdue() bool {
	if po.IsReceived() || po.IsCancelled() {
		return false
	}
	return time.Now().After(po.ExpectedDeliveryDate)
}

// CalculateTotalAmount пересчитывает общую сумму заказа из позиций
func (po *PurchaseOrder) CalculateTotalAmount() float64 {
	total := 0.0
	for _, item := range po.Items {
		total += item.TotalPrice
	}
	return total
}

// PurchaseOrderItem представляет позицию в заказе на закупку
type PurchaseOrderItem struct {
	ID                string         `json:"id" gorm:"type:uuid;primaryKey"`
	PurchaseOrderID   string         `json:"purchase_order_id" gorm:"type:uuid;not null;index"`
	PurchaseOrder     *PurchaseOrder `gorm:"foreignKey:PurchaseOrderID" json:"purchase_order,omitempty"`
	
	// Связь с номенклатурой
	NomenclatureID    string         `json:"nomenclature_id" gorm:"type:uuid;not null;index"`
	Nomenclature      *NomenclatureItem `gorm:"foreignKey:NomenclatureID" json:"nomenclature,omitempty"`
	
	// Количество и единицы измерения
	OrderedQuantity    float64        `json:"ordered_quantity" gorm:"type:decimal(10,2);not null"` // Заказанное количество
	Unit               string         `json:"unit" gorm:"type:varchar(20);not null;default:'kg'"` // Единица измерения
	
	// Цена закупки (фиксируется на момент создания заказа)
	PurchasePricePerUnit float64      `json:"purchase_price_per_unit" gorm:"type:decimal(10,2);not null"` // Цена за единицу на момент заказа
	TotalPrice          float64        `json:"total_price" gorm:"type:decimal(15,2);not null"` // Общая стоимость позиции
	
	// Полученное количество (заполняется при получении товара)
	ReceivedQuantity    float64        `json:"received_quantity" gorm:"type:decimal(10,2);default:0"` // Фактически полученное количество
	ReceivedTotalPrice  float64        `json:"received_total_price" gorm:"type:decimal(15,2);default:0"` // Фактическая стоимость полученного товара
	
	// Дополнительная информация
	Notes               string         `json:"notes" gorm:"type:text"`
	ExpiryDate          *time.Time      `json:"expiry_date" gorm:"type:date"` // Ожидаемый срок годности
	
	// Метаданные
	CreatedAt           time.Time      `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt           time.Time      `json:"updated_at" gorm:"autoUpdateTime"`
}

// TableName указывает имя таблицы
func (PurchaseOrderItem) TableName() string {
	return "purchase_order_items"
}

// BeforeCreate генерирует UUID и рассчитывает TotalPrice
func (poi *PurchaseOrderItem) BeforeCreate(tx *gorm.DB) error {
	if poi.ID == "" {
		poi.ID = uuid.New().String()
	}
	// Автоматически рассчитываем общую стоимость позиции
	if poi.TotalPrice == 0 {
		poi.TotalPrice = poi.OrderedQuantity * poi.PurchasePricePerUnit
	}
	return nil
}

// BeforeUpdate пересчитывает TotalPrice при изменении
func (poi *PurchaseOrderItem) BeforeUpdate(tx *gorm.DB) error {
	// Пересчитываем общую стоимость при изменении количества или цены
	poi.TotalPrice = poi.OrderedQuantity * poi.PurchasePricePerUnit
	return nil
}

// IsFullyReceived проверяет, получена ли позиция полностью
func (poi *PurchaseOrderItem) IsFullyReceived() bool {
	return poi.ReceivedQuantity >= poi.OrderedQuantity
}

// IsPartiallyReceived проверяет, получена ли позиция частично
func (poi *PurchaseOrderItem) IsPartiallyReceived() bool {
	return poi.ReceivedQuantity > 0 && poi.ReceivedQuantity < poi.OrderedQuantity
}

// GetRemainingQuantity возвращает количество, которое еще не получено
func (poi *PurchaseOrderItem) GetRemainingQuantity() float64 {
	remaining := poi.OrderedQuantity - poi.ReceivedQuantity
	if remaining < 0 {
		return 0
	}
	return remaining
}



