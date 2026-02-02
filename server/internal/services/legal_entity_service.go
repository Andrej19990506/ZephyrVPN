package services

import (
	"fmt"

	"zephyrvpn/server/internal/models"

	"gorm.io/gorm"
)

// LegalEntityService управляет логикой юридических лиц
type LegalEntityService struct {
	db *gorm.DB
}

// NewLegalEntityService создает новый экземпляр LegalEntityService
func NewLegalEntityService(db *gorm.DB) *LegalEntityService {
	return &LegalEntityService{db: db}
}

// GetAllLegalEntities возвращает список всех юридических лиц
func (s *LegalEntityService) GetAllLegalEntities() ([]models.LegalEntity, error) {
	var entities []models.LegalEntity
	if err := s.db.Where("is_active = ?", true).Find(&entities).Error; err != nil {
		return nil, fmt.Errorf("ошибка получения юридических лиц: %w", err)
	}
	return entities, nil
}

// GetLegalEntityByID возвращает юридическое лицо по ID
func (s *LegalEntityService) GetLegalEntityByID(id string) (*models.LegalEntity, error) {
	var entity models.LegalEntity
	if err := s.db.Where("id = ? AND is_active = ?", id, true).First(&entity).Error; err != nil {
		return nil, fmt.Errorf("юридическое лицо с ID %s не найдено: %w", id, err)
	}
	return &entity, nil
}

// CreateLegalEntity создает новое юридическое лицо
func (s *LegalEntityService) CreateLegalEntity(entity *models.LegalEntity) error {
	if err := s.db.Create(entity).Error; err != nil {
		return fmt.Errorf("ошибка создания юридического лица: %w", err)
	}
	return nil
}

// UpdateLegalEntity обновляет существующее юридическое лицо
func (s *LegalEntityService) UpdateLegalEntity(id string, updatedEntity *models.LegalEntity) error {
	var entity models.LegalEntity
	if err := s.db.First(&entity, "id = ?", id).Error; err != nil {
		return fmt.Errorf("юридическое лицо с ID %s не найдено: %w", id, err)
	}

	if err := s.db.Model(&entity).Updates(updatedEntity).Error; err != nil {
		return fmt.Errorf("ошибка обновления юридического лица: %w", err)
	}
	return nil
}

// DeleteLegalEntity удаляет юридическое лицо (мягкое удаление)
func (s *LegalEntityService) DeleteLegalEntity(id string) error {
	if err := s.db.Delete(&models.LegalEntity{}, "id = ?", id).Error; err != nil {
		return fmt.Errorf("ошибка удаления юридического лица: %w", err)
	}
	return nil
}

