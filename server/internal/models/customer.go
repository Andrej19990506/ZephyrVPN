package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// AddressType представляет тип адреса доставки
type AddressType string

const (
	AddressTypeHome  AddressType = "home"  // Домашний адрес
	AddressTypeWork  AddressType = "work"  // Рабочий адрес
	AddressTypeOther AddressType = "other" // Другой адрес
)

// Customer представляет профиль КЛИЕНТА (покупателя пиццы)
// Содержит только потребительскую информацию: баллы лояльности, количество заказов, предпочтения
//
// Бизнес-логика:
// - Связан с User через UserID (один User = один Customer профиль)
// - User может иметь Role = "customer", но также "technologist", "admin", "courier" (для скидок сотрудника)
// - Customer может иметь несколько адресов доставки (CustomerAddress)
// - Customer содержит баллы лояльности и историю заказов
//
// ВАЖНО: Customer НЕ содержит базовую информацию (имя, телефон) - она в User
// Customer содержит только потребительскую информацию: баллы, заказы, адреса
//
// Пример использования:
// - Обычный клиент: User (role="customer") + Customer
// - Сотрудник-клиент: User (role="technologist") + Staff + Customer (для скидок)
type Customer struct {
	// UserID - связь с User (обязательное поле)
	// Если User удален, Customer также удаляется (CASCADE)
	UserID       string    `json:"user_id" gorm:"type:uuid;primaryKey;not null;index"`

	// Потребительская информация
	FirstName    *string   `json:"first_name" gorm:"type:varchar(100)"`        // Имя (опционально, может быть в User)
	LastName     *string   `json:"last_name" gorm:"type:varchar(100)"`         // Фамилия (опционально)
	LoyaltyPoints int      `json:"loyalty_points" gorm:"type:int;not null;default:0"` // Баллы лояльности
	TotalOrders  int       `json:"total_orders" gorm:"type:int;not null;default:0"`  // Общее количество заказов

	// Метаданные
	CreatedAt    time.Time `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt    time.Time `json:"updated_at" gorm:"autoUpdateTime"`

	// Связи
	User         *User              `json:"user,omitempty" gorm:"foreignKey:UserID;references:ID"` // Связь с User (базовая информация)
	Addresses    []CustomerAddress  `json:"addresses,omitempty" gorm:"foreignKey:CustomerID;references:UserID"` // Адреса доставки
}

// TableName указывает имя таблицы
func (Customer) TableName() string {
	return "customers"
}

// CustomerAddress представляет адрес доставки для КЛИЕНТА
// Клиент может иметь несколько адресов (дом, работа, другой)
// Один из адресов может быть помечен как "по умолчанию" (is_default = true)
// Триггер в БД обеспечивает, что только один адрес может быть default
type CustomerAddress struct {
	ID          string      `json:"id" gorm:"type:uuid;primaryKey"`
	CustomerID  string      `json:"customer_id" gorm:"type:uuid;not null;index:idx_customer_addresses_customer"`
	Type        AddressType `json:"type" gorm:"type:varchar(20);not null;default:'home';index:idx_customer_addresses_type"`
	Address     string      `json:"address" gorm:"type:text;not null"`
	Coordinates *string     `json:"coordinates" gorm:"type:point"` // GPS координаты (POINT type) для расчета маршрута
	Floor       *string     `json:"floor" gorm:"type:varchar(10)"`
	Entrance    *string     `json:"entrance" gorm:"type:varchar(10)"`
	Comment     *string     `json:"comment" gorm:"type:text"`
	IsDefault   bool        `json:"is_default" gorm:"type:boolean;not null;default:false;index:idx_customer_addresses_default"`
	CreatedAt   time.Time   `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt   time.Time   `json:"updated_at" gorm:"autoUpdateTime"`

	// Связи
	Customer    *Customer `json:"customer,omitempty" gorm:"foreignKey:CustomerID;references:UserID"`
}

// TableName указывает имя таблицы
func (CustomerAddress) TableName() string {
	return "customer_addresses"
}

// BeforeCreate генерирует UUID если не указан
func (ca *CustomerAddress) BeforeCreate(tx *gorm.DB) error {
	if ca.ID == "" {
		ca.ID = uuid.New().String()
	}
	return nil
}

// GetDefaultAddress возвращает адрес по умолчанию для клиента
// Если адрес по умолчанию не найден, возвращает первый адрес
func (c *Customer) GetDefaultAddress() *CustomerAddress {
	if len(c.Addresses) == 0 {
		return nil
	}

	// Ищем адрес по умолчанию
	for i := range c.Addresses {
		if c.Addresses[i].IsDefault {
			return &c.Addresses[i]
		}
	}

	// Если адреса по умолчанию нет, возвращаем первый
	return &c.Addresses[0]
}
