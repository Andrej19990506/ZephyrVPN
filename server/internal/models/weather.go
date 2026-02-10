package models

import (
	"time"
)

// WeatherData модель для хранения данных о погоде
// ВАЖНО: Не используем gorm.DeletedAt, так как в таблице нет колонки deleted_at
type WeatherData struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	
	Date      time.Time `gorm:"type:date;uniqueIndex;not null" json:"date"` // Дата (UNIQUE)
	Latitude  float64   `gorm:"type:decimal(10,8);not null" json:"latitude"`
	Longitude float64   `gorm:"type:decimal(11,8);not null" json:"longitude"`
	Timezone  string    `gorm:"type:varchar(50);not null;default:'Europe/Berlin'" json:"timezone"`
	
	// Температурные данные
	AvgTemp  *float64 `gorm:"type:decimal(5,2);column:avg_temp" json:"avg_temp"`   // Средняя температура
	MaxTemp  *float64 `gorm:"type:decimal(5,2);column:max_temp" json:"max_temp"`   // Максимальная температура
	MinTemp  *float64 `gorm:"type:decimal(5,2);column:min_temp" json:"min_temp"`   // Минимальная температура
	TempAt12 *float64 `gorm:"type:decimal(5,2);column:temp_at_12" json:"temp_at_12"` // Температура в 12:00 (ВАЖНО: в БД колонка temp_at_12 с подчеркиванием)
	TempAt18 *float64 `gorm:"type:decimal(5,2);column:temp_at_18" json:"temp_at_18"` // Температура в 18:00 (ВАЖНО: в БД колонка temp_at_18 с подчеркиванием)
	
	// Метаданные
	Source string `gorm:"type:varchar(50);not null;default:'open-meteo'" json:"source"` // Источник данных
}

// TableName возвращает имя таблицы
func (WeatherData) TableName() string {
	return "weather_data"
}

