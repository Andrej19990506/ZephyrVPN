package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// UserRole представляет роль пользователя в системе (тип аккаунта)
// Определяет, к какой категории относится пользователь: клиент, сотрудник, администратор
// ВАЖНО: Это НЕ должность (Cook, Courier), а тип пользователя в системе
type UserRole string

const (
	RoleCustomer      UserRole = "customer"      // Клиент (покупатель пиццы)
	RoleCourier       UserRole = "courier"         // Курьер (доставляет заказы)
	RoleKitchenStaff  UserRole = "kitchen_staff"   // Персонал кухни (повара)
	RoleTechnologist  UserRole = "technologist"    // Технолог (управляет рецептами)
	RoleAdmin         UserRole = "admin"           // Администратор
)

// UserStatus представляет статус аккаунта пользователя
// Управляет доступом к системе независимо от роли
type UserStatus string

const (
	UserStatusActive    UserStatus = "active"    // Активный (может использовать систему)
	UserStatusInactive  UserStatus = "inactive"  // Неактивный (временно отключен)
	UserStatusSuspended UserStatus = "suspended"  // Заблокирован (не может использовать систему)
)

// User представляет центральную таблицу аутентификации для ВСЕХ пользователей системы
// Это единая точка входа для всех типов пользователей: клиентов, курьеров, поваров, технологи, админов
//
// Бизнес-логика:
// - Один User может иметь несколько профилей одновременно:
//   * Staff профиль (если работает в компании) - для работы, зарплаты, расписания
//   * Customer профиль (если заказывает пиццу) - для баллов лояльности, адресов доставки
// - Например: Технолог может иметь Customer профиль для получения скидок сотрудника
// - Например: Курьер может иметь Customer профиль для заказа пиццы после смены
//
// Содержит только базовую информацию для аутентификации и авторизации:
// - Идентификаторы (ID, Email, Phone)
// - Безопасность (PasswordHash)
// - Роль и статус для контроля доступа
type User struct {
	ID           string     `json:"id" gorm:"type:uuid;primaryKey"`
	Name         *string    `json:"name" gorm:"type:varchar(255)"` // Имя пользователя (для сотрудников)
	Email        *string    `json:"email" gorm:"type:varchar(255);uniqueIndex:idx_users_email,where:email IS NOT NULL"`
	Phone        string     `json:"phone" gorm:"type:varchar(20);uniqueIndex;not null"`
	PasswordHash *string    `json:"-" gorm:"type:varchar(255)"` // Не возвращаем в JSON для безопасности
	Role         UserRole   `json:"role" gorm:"type:varchar(50);not null;default:'customer';index:idx_users_role_status"`
	Status       UserStatus `json:"status" gorm:"type:varchar(20);not null;default:'active';index:idx_users_role_status"`
	CreatedAt    time.Time  `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt    time.Time  `json:"updated_at" gorm:"autoUpdateTime"`

	// Опциональные связи с профилями
	// User может иметь Staff профиль (если работает в компании)
	Staff *Staff `json:"staff,omitempty" gorm:"foreignKey:UserID;references:ID"`

	// User может иметь Customer профиль (если заказывает пиццу)
	// ВАЖНО: Technologist, Admin, Courier могут иметь Customer профиль для скидок сотрудника
	Customer *Customer `json:"customer,omitempty" gorm:"foreignKey:UserID;references:ID"`
}

// TableName указывает имя таблицы
func (User) TableName() string {
	return "users"
}

// BeforeCreate генерирует UUID если не указан
func (u *User) BeforeCreate(tx *gorm.DB) error {
	if u.ID == "" {
		u.ID = uuid.New().String()
	}
	return nil
}

// HasStaffProfile проверяет, имеет ли пользователь Staff профиль
func (u *User) HasStaffProfile() bool {
	return u.Staff != nil
}

// HasCustomerProfile проверяет, имеет ли пользователь Customer профиль
func (u *User) HasCustomerProfile() bool {
	return u.Customer != nil
}

// IsActive проверяет, активен ли пользователь
func (u *User) IsActive() bool {
	return u.Status == UserStatusActive
}

