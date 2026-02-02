package models

import (
	"time"

	"gorm.io/gorm"
)

// StaffStatus представляет статус сотрудника
type StaffStatus string

const (
	StatusActive     StaffStatus = "Active"
	StatusReserve    StaffStatus = "Reserve"
	StatusBlacklisted StaffStatus = "Blacklisted"
)

// Staff представляет сотрудника в БД
type Staff struct {
	ID              string         `gorm:"type:varchar(36);primaryKey" json:"id"` // UUID как строка
	Name            string         `gorm:"type:varchar(255);not null" json:"name"`
	Phone           string         `gorm:"type:varchar(20);uniqueIndex;not null" json:"phone"`
	RoleName        string         `gorm:"type:varchar(100);not null;default:'employee';index" json:"role_name"` // Название роли (динамическое)
	Status          StaffStatus    `gorm:"type:varchar(20);default:'Active'" json:"status"`
	BranchID        string         `gorm:"type:varchar(255);not null;index" json:"branch_id"` // Обязательное поле
	PerformanceScore float64       `gorm:"type:decimal(5,2);default:0.0" json:"performance_score"`
	CreatedAt       time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt       time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt       gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName возвращает имя таблицы
func (Staff) TableName() string {
	return "staff"
}

// ToMap преобразует Staff в map для API ответа
func (s *Staff) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"id":               s.ID,
		"name":             s.Name,
		"phone":            s.Phone,
		"role":             s.RoleName, // Используем RoleName вместо Role
		"role_name":        s.RoleName,
		"status":           string(s.Status),
		"branch_id":        s.BranchID,
		"performance_score": s.PerformanceScore,
		"created_at":       s.CreatedAt.Format(time.RFC3339),
		"updated_at":       s.UpdatedAt.Format(time.RFC3339),
	}
}

// CanTransitionTo проверяет, разрешен ли переход статуса (State Machine)
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

