package services

import (
	"fmt"

	"zephyrvpn/server/internal/models"

	"gorm.io/gorm"
)

// UoMConversionService управляет правилами конвертации единиц измерения
type UoMConversionService struct {
	db *gorm.DB
}

// NewUoMConversionService создает новый экземпляр UoMConversionService
func NewUoMConversionService(db *gorm.DB) *UoMConversionService {
	return &UoMConversionService{
		db: db,
	}
}

// GetAllRules возвращает все активные правила конвертации
func (s *UoMConversionService) GetAllRules() ([]models.UoMConversionRule, error) {
	var rules []models.UoMConversionRule
	if err := s.db.Where("deleted_at IS NULL AND is_active = ?", true).
		Order("is_default DESC, name ASC").
		Find(&rules).Error; err != nil {
		return nil, fmt.Errorf("ошибка загрузки правил конвертации: %w", err)
	}
	return rules, nil
}

// GetRuleByID возвращает правило по ID
func (s *UoMConversionService) GetRuleByID(ruleID string) (*models.UoMConversionRule, error) {
	var rule models.UoMConversionRule
	if err := s.db.Where("id = ? AND deleted_at IS NULL", ruleID).First(&rule).Error; err != nil {
		return nil, fmt.Errorf("правило конвертации не найдено: %w", err)
	}
	return &rule, nil
}

// GetDefaultRule возвращает правило по умолчанию
func (s *UoMConversionService) GetDefaultRule() (*models.UoMConversionRule, error) {
	var rule models.UoMConversionRule
	if err := s.db.Where("is_default = ? AND is_active = ? AND deleted_at IS NULL", true, true).
		First(&rule).Error; err != nil {
		// Если правило по умолчанию не найдено, возвращаем первое активное
		if err := s.db.Where("is_active = ? AND deleted_at IS NULL", true).
			First(&rule).Error; err != nil {
			return nil, fmt.Errorf("правила конвертации не найдены: %w", err)
		}
	}
	return &rule, nil
}

// CreateRule создает новое правило конвертации
func (s *UoMConversionService) CreateRule(rule *models.UoMConversionRule) error {
	if err := s.db.Create(rule).Error; err != nil {
		return fmt.Errorf("ошибка создания правила конвертации: %w", err)
	}
	return nil
}

// UpdateRule обновляет правило конвертации
func (s *UoMConversionService) UpdateRule(ruleID string, updates map[string]interface{}) error {
	if err := s.db.Model(&models.UoMConversionRule{}).
		Where("id = ? AND deleted_at IS NULL", ruleID).
		Updates(updates).Error; err != nil {
		return fmt.Errorf("ошибка обновления правила конвертации: %w", err)
	}
	return nil
}

// DeleteRule мягко удаляет правило конвертации
func (s *UoMConversionService) DeleteRule(ruleID string) error {
	if err := s.db.Where("id = ?", ruleID).Delete(&models.UoMConversionRule{}).Error; err != nil {
		return fmt.Errorf("ошибка удаления правила конвертации: %w", err)
	}
	return nil
}

// UpdateAllRulesDefault снимает флаг is_default со всех правил
func (s *UoMConversionService) UpdateAllRulesDefault(isDefault bool) error {
	if err := s.db.Model(&models.UoMConversionRule{}).
		Where("deleted_at IS NULL").
		Update("is_default", isDefault).Error; err != nil {
		return fmt.Errorf("ошибка обновления правил по умолчанию: %w", err)
	}
	return nil
}

