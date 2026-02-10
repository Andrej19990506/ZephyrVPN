package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// RecipeVersion представляет версию рецепта для отслеживания изменений
type RecipeVersion struct {
	ID          string    `json:"id" gorm:"type:uuid;primaryKey"`
	RecipeID    string    `json:"recipe_id" gorm:"type:uuid;not null;index"`
	Version     int       `json:"version" gorm:"not null"` // Номер версии (1, 2, 3...)
	ChangedBy   string    `json:"changed_by" gorm:"type:varchar(255);not null"` // ID пользователя или имя
	ChangeReason string   `json:"change_reason" gorm:"type:text"` // Причина изменения
	IngredientsJSON string `json:"ingredients_json" gorm:"type:text"` // JSON снимок ингредиентов на момент изменения
	CreatedAt   time.Time `json:"created_at" gorm:"autoCreateTime"`
	
	// Relations
	Recipe      *Recipe   `json:"recipe,omitempty" gorm:"foreignKey:RecipeID"`
}

// TableName указывает имя таблицы
func (RecipeVersion) TableName() string {
	return "recipe_versions"
}

// BeforeCreate генерирует UUID
func (rv *RecipeVersion) BeforeCreate(tx *gorm.DB) error {
	if rv.ID == "" {
		rv.ID = uuid.New().String()
	}
	return nil
}

// TrainingMaterial представляет обучающий материал для рецепта
type TrainingMaterial struct {
	ID          string    `json:"id" gorm:"type:uuid;primaryKey"`
	RecipeID    string    `json:"recipe_id" gorm:"type:uuid;not null;index"`
	Type        string    `json:"type" gorm:"type:varchar(50);not null"` // "video", "photo", "document"
	Title       string    `json:"title" gorm:"type:varchar(255);not null"`
	Description string    `json:"description" gorm:"type:text"`
	S3URL       string    `json:"s3_url" gorm:"type:text;not null"` // URL в S3
	ThumbnailURL string   `json:"thumbnail_url" gorm:"type:text"` // Превью для видео/фото
	Order       int       `json:"order" gorm:"default:0"` // Порядок отображения
	IsActive    bool      `json:"is_active" gorm:"default:true"`
	CreatedBy   string    `json:"created_by" gorm:"type:varchar(255);not null"`
	CreatedAt   time.Time `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt   time.Time `json:"updated_at" gorm:"autoUpdateTime"`
	
	// Relations
	Recipe      *Recipe   `json:"recipe,omitempty" gorm:"foreignKey:RecipeID"`
}

// TableName указывает имя таблицы
func (TrainingMaterial) TableName() string {
	return "training_materials"
}

// BeforeCreate генерирует UUID
func (tm *TrainingMaterial) BeforeCreate(tx *gorm.DB) error {
	if tm.ID == "" {
		tm.ID = uuid.New().String()
	}
	return nil
}

// RecipeExam представляет экзамен по рецепту для сотрудников
type RecipeExam struct {
	ID          string    `json:"id" gorm:"type:uuid;primaryKey"`
	RecipeID    string    `json:"recipe_id" gorm:"type:uuid;not null;index"`
	StaffID     string    `json:"staff_id" gorm:"type:uuid;not null;index"`
	Status      string    `json:"status" gorm:"type:varchar(50);not null;default:'pending'"` // "pending", "passed", "failed"
	Score       int       `json:"score" gorm:"default:0"` // Баллы (0-100)
	PassedAt    *time.Time `json:"passed_at" gorm:"type:timestamp"` // Дата сдачи
	ExaminedBy  string    `json:"examined_by" gorm:"type:varchar(255)"` // Кто проверил
	Notes       string    `json:"notes" gorm:"type:text"` // Заметки технолога
	CreatedAt   time.Time `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt   time.Time `json:"updated_at" gorm:"autoUpdateTime"`
	
	// Relations
	Recipe      *Recipe   `json:"recipe,omitempty" gorm:"foreignKey:RecipeID"`
	Staff       *Staff    `json:"staff,omitempty" gorm:"foreignKey:StaffID"`
}

// TableName указывает имя таблицы
func (RecipeExam) TableName() string {
	return "recipe_exams"
}

// BeforeCreate генерирует UUID
func (re *RecipeExam) BeforeCreate(tx *gorm.DB) error {
	if re.ID == "" {
		re.ID = uuid.New().String()
	}
	return nil
}

// RecipeUsageTree представляет дерево использования рецепта (какие рецепты используют этот как ингредиент)
type RecipeUsageTree struct {
	RecipeID      string   `json:"recipe_id"`
	RecipeName    string   `json:"recipe_name"`
	IsSemiFinished bool    `json:"is_semi_finished"`
	UsedIn        []RecipeUsageTree `json:"used_in"` // Рекурсивная структура
}









