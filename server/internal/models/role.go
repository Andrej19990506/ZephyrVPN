package models

import (
	"log"
	"time"

	"gorm.io/gorm"
)

// Role представляет роль сотрудника в БД
type Role struct {
	ID          string    `gorm:"type:varchar(36);primaryKey" json:"id"` // UUID как строка
	Name        string    `gorm:"type:varchar(100);uniqueIndex;not null" json:"name"` // Название роли (например, "Cook", "Courier")
	Label       string    `gorm:"type:varchar(100);not null" json:"label"` // Отображаемое название (например, "Повар", "Курьер")
	Description string    `gorm:"type:text" json:"description"` // Описание роли
	IsActive    bool      `gorm:"default:true" json:"is_active"` // Активна ли роль
	CreatedAt   time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt   time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

// TableName возвращает имя таблицы
func (Role) TableName() string {
	return "roles"
}

// InitDefaultRoles инициализирует роли по умолчанию
func InitDefaultRoles(db *gorm.DB) error {
	if db == nil {
		return nil
	}

	defaultRoles := []Role{
		{ID: "1", Name: "Cook", Label: "Повар", Description: "Приготовление блюд", IsActive: true},
		{ID: "2", Name: "Courier", Label: "Курьер", Description: "Доставка заказов", IsActive: true},
		{ID: "3", Name: "Admin", Label: "Администратор", Description: "Управление системой", IsActive: true},
		{ID: "4", Name: "Manager", Label: "Менеджер", Description: "Управление персоналом и операциями", IsActive: true},
		{ID: "5", Name: "Technologist", Label: "Технолог", Description: "Управление рецептами и производством", IsActive: true},
		{ID: "6", Name: "SuperAdmin", Label: "Супер-администратор", Description: "Полный доступ ко всем функциям", IsActive: true},
	}

	for _, role := range defaultRoles {
		var existing Role
		if err := db.Where("name = ?", role.Name).First(&existing).Error; err != nil {
			// Роль не существует, создаем
			if err := db.Create(&role).Error; err != nil {
				log.Printf("⚠️ Ошибка создания роли %s: %v", role.Name, err)
			}
		}
	}

	return nil
}

