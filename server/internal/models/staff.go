package models

import (
	"time"

	"gorm.io/gorm"
)

// StaffStatus представляет статус сотрудника (State Machine)
// Управляет рабочим статусом сотрудника в компании
type StaffStatus string

const (
	StatusActive     StaffStatus = "Active"     // Активный сотрудник (работает)
	StatusReserve    StaffStatus = "Reserve"   // Резерв (временно не работает, но может вернуться)
	StatusBlacklisted StaffStatus = "Blacklisted" // В черном списке (уволен, не может вернуться)
)

// Staff представляет профиль СОТРУДНИКА компании
// Содержит только рабочую информацию: должность, филиал, производительность, зарплата
//
// Бизнес-логика:
// - Связан с User через UserID (один User = один Staff профиль)
// - User может иметь Role = "kitchen_staff", "courier", "technologist", "admin"
// - Staff содержит RoleName (должность: "Cook", "Courier", "Manager") - это НЕ то же самое, что User.Role
// - Staff может иметь Customer профиль одновременно (для скидок сотрудника)
//
// ВАЖНО: Staff НЕ содержит базовую информацию (имя, телефон) - она в User
// Staff содержит только рабочую информацию: филиал, должность, производительность
type Staff struct {
	// UserID - связь с User (обязательное поле)
	// Если User удален, Staff также удаляется (CASCADE)
	UserID         string         `json:"user_id" gorm:"type:uuid;primaryKey;not null;index"`
	
	// Рабочая информация
	RoleName       string         `json:"role_name" gorm:"type:varchar(100);not null;default:'employee';index"` // Должность: "Cook", "Courier", "Manager"
	Status         StaffStatus    `json:"status" gorm:"type:varchar(20);default:'Active'"`                      // Рабочий статус
	BranchID       string         `json:"branch_id" gorm:"type:varchar(255);not null;index"`                   // Филиал, где работает
	PerformanceScore float64      `json:"performance_score" gorm:"type:decimal(5,2);default:0.0"`              // Оценка производительности
	
	// Метаданные
	CreatedAt      time.Time      `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt      time.Time      `json:"updated_at" gorm:"autoUpdateTime"`
	DeletedAt      gorm.DeletedAt `json:"-" gorm:"index"` // Soft delete

	// Связи
	User           *User          `json:"user,omitempty" gorm:"foreignKey:UserID;references:ID"` // Связь с User (базовая информация)
}

// TableName возвращает имя таблицы
func (Staff) TableName() string {
	return "staff"
}

// ToMap преобразует Staff в map для API ответа
// Включает информацию из связанного User (если загружен)
func (s *Staff) ToMap() map[string]interface{} {
	result := map[string]interface{}{
		"user_id":          s.UserID,
		"role_name":        s.RoleName,
		"status":           string(s.Status),
		"branch_id":        s.BranchID,
		"performance_score": s.PerformanceScore,
		"created_at":       s.CreatedAt.Format(time.RFC3339),
		"updated_at":       s.UpdatedAt.Format(time.RFC3339),
	}

	// Если User загружен, добавляем его данные
	if s.User != nil {
		result["id"] = s.User.ID
		result["phone"] = s.User.Phone
		result["email"] = s.User.Email
		result["role"] = string(s.User.Role)
		result["user_status"] = string(s.User.Status)
	}

	return result
}

// CanTransitionTo проверяет, разрешен ли переход статуса (State Machine)
// Blacklisted -> ANY: СТРОГО ЗАПРЕЩЕНО (нельзя вернуться из черного списка)
func (s *Staff) CanTransitionTo(newStatus StaffStatus) bool {
	currentStatus := s.Status

	// Blacklisted -> ANY: STRICTLY PROHIBITED
	if currentStatus == StatusBlacklisted {
		return false
	}

	// Разрешенные переходы
	allowedTransitions := map[StaffStatus][]StaffStatus{
		StatusActive:  {StatusReserve, StatusBlacklisted},
		StatusReserve: {StatusActive, StatusBlacklisted},
	}

	if allowed, ok := allowedTransitions[currentStatus]; ok {
		for _, allowedStatus := range allowed {
			if allowedStatus == newStatus {
				return true
			}
		}
	}

	return false
}

// IsActive проверяет, активен ли сотрудник
func (s *Staff) IsActive() bool {
	return s.Status == StatusActive
}
