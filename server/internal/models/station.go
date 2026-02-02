package models

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"time"

	"gorm.io/gorm"
)

// StationConfig представляет конфигурацию станции (JSON в БД)
type StationConfig struct {
	Icon          string   `json:"icon"`
	Capabilities  []string `json:"capabilities"`
	Categories    []string `json:"categories"`
	TriggerStatus string   `json:"trigger_status"`
	TargetStatus  string   `json:"target_status"`
}

// Value реализует driver.Valuer для сохранения в БД
func (sc StationConfig) Value() (driver.Value, error) {
	return json.Marshal(sc)
}

// Scan реализует sql.Scanner для чтения из БД
func (sc *StationConfig) Scan(value interface{}) error {
	if value == nil {
		return nil
	}

	bytes, ok := value.([]byte)
	if !ok {
		return errors.New("failed to unmarshal StationConfig value")
	}

	return json.Unmarshal(bytes, sc)
}

// Station представляет кухонную станцию в БД
type Station struct {
	ID         string         `gorm:"type:varchar(36);primaryKey" json:"id"` // UUID как строка (36 символов)
	Name       string         `gorm:"type:varchar(255);not null" json:"name"`
	Icon       string         `gorm:"type:varchar(50);not null;default:'ChefHat'" json:"icon"`
	Status     string         `gorm:"type:varchar(20);default:'offline'" json:"status"` // "online" or "offline"
	QueueCount int            `gorm:"default:0" json:"queue_count"`
	Config     StationConfig  `gorm:"type:jsonb" json:"config"`
	BranchID   string         `gorm:"type:varchar(255);index" json:"branch_id"` // UUID как строка для совместимости
	CreatedAt  time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt  time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt  gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName возвращает имя таблицы
func (Station) TableName() string {
	return "stations"
}

// ToMap преобразует Station в map для API ответа
func (s *Station) ToMap() map[string]interface{} {
	// Нормализация массивов для предотвращения null в JSON
	capabilities := s.Config.Capabilities
	if capabilities == nil {
		capabilities = []string{}
	}
	categories := s.Config.Categories
	if categories == nil {
		categories = []string{}
	}
	
	return map[string]interface{}{
		"id":          s.ID,
		"name":        s.Name,
		"icon":        s.Icon,
		"status":      s.Status,
		"queue_count": s.QueueCount,
		"config": map[string]interface{}{
			"icon":           s.Config.Icon,
			"capabilities":   capabilities,
			"categories":     categories,
			"trigger_status": s.Config.TriggerStatus,
			"target_status":  s.Config.TargetStatus,
		},
		"branch_id":  s.BranchID,
		"created_at": s.CreatedAt.Format(time.RFC3339),
		"updated_at": s.UpdatedAt.Format(time.RFC3339),
	}
}

