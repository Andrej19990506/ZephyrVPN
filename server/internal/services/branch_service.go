package services

import (
	"fmt"

	"zephyrvpn/server/internal/models"

	"gorm.io/gorm"
)

// BranchService управляет логикой филиалов
type BranchService struct {
	db *gorm.DB
}

// NewBranchService создает новый экземпляр BranchService
func NewBranchService(db *gorm.DB) *BranchService {
	return &BranchService{db: db}
}

// GetAllBranches возвращает список всех филиалов
// Если передан legalEntityID, возвращает только филиалы этого ИП
// Если передан superAdminID, возвращает только филиалы этого аккаунта
func (s *BranchService) GetAllBranches(legalEntityID *string, superAdminID *string) ([]models.Branch, error) {
	var branches []models.Branch
	query := s.db.Where("is_active = ?", true)

	if legalEntityID != nil && *legalEntityID != "" {
		query = query.Where("legal_entity_id = ?", *legalEntityID)
	}

	if superAdminID != nil && *superAdminID != "" {
		query = query.Where("super_admin_id = ?", *superAdminID)
	}

	if err := query.Preload("LegalEntity").Preload("SuperAdmin").Find(&branches).Error; err != nil {
		return nil, fmt.Errorf("ошибка получения филиалов: %w", err)
	}

	return branches, nil
}

// GetBranchByID возвращает филиал по ID
func (s *BranchService) GetBranchByID(id string) (*models.Branch, error) {
	var branch models.Branch
	if err := s.db.Preload("LegalEntity").Preload("SuperAdmin").First(&branch, "id = ?", id).Error; err != nil {
		return nil, fmt.Errorf("филиал с ID %s не найден: %w", id, err)
	}
	return &branch, nil
}

// CreateBranch создает новый филиал
func (s *BranchService) CreateBranch(branch *models.Branch) error {
	// Валидация: legal_entity_id обязателен
	if branch.LegalEntityID == nil || *branch.LegalEntityID == "" {
		return fmt.Errorf("legal_entity_id обязателен для создания филиала")
	}

	// Проверяем, что LegalEntity существует
	var legalEntity models.LegalEntity
	if err := s.db.First(&legalEntity, "id = ?", *branch.LegalEntityID).Error; err != nil {
		return fmt.Errorf("юридическое лицо с ID %s не найдено: %w", *branch.LegalEntityID, err)
	}

	// Если указан SuperAdminID, проверяем его существование
	if branch.SuperAdminID != nil && *branch.SuperAdminID != "" {
		var superAdmin models.SuperAdmin
		if err := s.db.First(&superAdmin, "id = ?", *branch.SuperAdminID).Error; err != nil {
			return fmt.Errorf("супер-администратор с ID %s не найден: %w", *branch.SuperAdminID, err)
		}
	}

	if err := s.db.Create(branch).Error; err != nil {
		return fmt.Errorf("ошибка создания филиала: %w", err)
	}

	return nil
}

// UpdateBranch обновляет существующий филиал
func (s *BranchService) UpdateBranch(id string, updatedBranch *models.Branch) error {
	var branch models.Branch
	if err := s.db.First(&branch, "id = ?", id).Error; err != nil {
		return fmt.Errorf("филиал с ID %s не найден: %w", id, err)
	}

	// Если обновляется legal_entity_id, проверяем его существование
	if updatedBranch.LegalEntityID != nil && *updatedBranch.LegalEntityID != "" {
		var legalEntity models.LegalEntity
		if err := s.db.First(&legalEntity, "id = ?", *updatedBranch.LegalEntityID).Error; err != nil {
			return fmt.Errorf("юридическое лицо с ID %s не найдено: %w", *updatedBranch.LegalEntityID, err)
		}
	}

	// Если обновляется super_admin_id, проверяем его существование
	if updatedBranch.SuperAdminID != nil && *updatedBranch.SuperAdminID != "" {
		var superAdmin models.SuperAdmin
		if err := s.db.First(&superAdmin, "id = ?", *updatedBranch.SuperAdminID).Error; err != nil {
			return fmt.Errorf("супер-администратор с ID %s не найден: %w", *updatedBranch.SuperAdminID, err)
		}
	}

	if err := s.db.Model(&branch).Updates(updatedBranch).Error; err != nil {
		return fmt.Errorf("ошибка обновления филиала: %w", err)
	}

	return nil
}

// DeleteBranch удаляет филиал (мягкое удаление)
func (s *BranchService) DeleteBranch(id string) error {
	if err := s.db.Delete(&models.Branch{}, "id = ?", id).Error; err != nil {
		return fmt.Errorf("ошибка удаления филиала: %w", err)
	}
	return nil
}

// GetBranchesByLegalEntity возвращает все филиалы для указанного ИП
func (s *BranchService) GetBranchesByLegalEntity(legalEntityID string) ([]models.Branch, error) {
	return s.GetAllBranches(&legalEntityID, nil)
}

// GetBranchesBySuperAdmin возвращает все филиалы для указанного аккаунта
func (s *BranchService) GetBranchesBySuperAdmin(superAdminID string) ([]models.Branch, error) {
	return s.GetAllBranches(nil, &superAdminID)
}

