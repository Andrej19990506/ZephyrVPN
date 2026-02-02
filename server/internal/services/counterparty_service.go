package services

import (
	"fmt"
	"log"

	"zephyrvpn/server/internal/models"

	"gorm.io/gorm"
)

// CounterpartyService управляет контрагентами
type CounterpartyService struct {
	db *gorm.DB
}

// NewCounterpartyService создает новый экземпляр CounterpartyService
func NewCounterpartyService(db *gorm.DB) *CounterpartyService {
	return &CounterpartyService{db: db}
}

// GetAllCounterparties получает список всех контрагентов
func (s *CounterpartyService) GetAllCounterparties() ([]models.Counterparty, error) {
	var counterparties []models.Counterparty
	if err := s.db.Where("status = ?", models.CounterpartyStatusActive).Find(&counterparties).Error; err != nil {
		return nil, err
	}
	return counterparties, nil
}

// GetCounterpartyByID получает контрагента по ID
func (s *CounterpartyService) GetCounterpartyByID(id string) (*models.Counterparty, error) {
	var counterparty models.Counterparty
	if err := s.db.First(&counterparty, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &counterparty, nil
}

// CreateCounterparty создает нового контрагента
func (s *CounterpartyService) CreateCounterparty(counterparty *models.Counterparty) error {
	// Проверяем уникальность ИНН
	if counterparty.INN != "" {
		var existing models.Counterparty
		if err := s.db.Where("inn = ?", counterparty.INN).First(&existing).Error; err == nil {
			return fmt.Errorf("контрагент с ИНН %s уже существует", counterparty.INN)
		}
	}

	if err := s.db.Create(counterparty).Error; err != nil {
		return err
	}
	return nil
}

// UpdateCounterparty обновляет данные контрагента
func (s *CounterpartyService) UpdateCounterparty(id string, counterparty *models.Counterparty) error {
	// Проверяем уникальность ИНН (если изменился)
	if counterparty.INN != "" {
		var existing models.Counterparty
		if err := s.db.Where("inn = ? AND id != ?", counterparty.INN, id).First(&existing).Error; err == nil {
			return fmt.Errorf("контрагент с ИНН %s уже существует", counterparty.INN)
		}
	}

	if err := s.db.Model(&models.Counterparty{}).Where("id = ?", id).Updates(counterparty).Error; err != nil {
		return err
	}
	return nil
}

// DeleteCounterparty удаляет контрагента (soft delete)
func (s *CounterpartyService) DeleteCounterparty(id string) error {
	if err := s.db.Delete(&models.Counterparty{}, "id = ?", id).Error; err != nil {
		return err
	}
	return nil
}

// UpdateCounterpartyBalance обновляет баланс контрагента
// amount: сумма для добавления (положительная = увеличение долга)
// isOfficial: true для официальных операций (банк), false для внутренних (наличные)
func (s *CounterpartyService) UpdateCounterpartyBalance(counterpartyID string, amount float64, isOfficial bool) error {
	var counterparty models.Counterparty
	if err := s.db.First(&counterparty, "id = ?", counterpartyID).Error; err != nil {
		return fmt.Errorf("контрагент не найден: %v", err)
	}

	if isOfficial {
		counterparty.BalanceOfficial += amount
	} else {
		counterparty.BalanceInternal += amount
	}

	if err := s.db.Save(&counterparty).Error; err != nil {
		return fmt.Errorf("ошибка обновления баланса: %v", err)
	}

	log.Printf("✅ Обновлен баланс контрагента %s: официальный=%.2f, внутренний=%.2f", 
		counterpartyID, counterparty.BalanceOfficial, counterparty.BalanceInternal)
	return nil
}

