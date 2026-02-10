package services

import (
	"fmt"
	"log"
	"time"
)

// KitchenLoadService управляет расчетом загрузки кухни на основе слотов
type KitchenLoadService struct {
	slotService *SlotService
}

// NewKitchenLoadService создает новый сервис загрузки кухни
func NewKitchenLoadService(slotService *SlotService) *KitchenLoadService {
	return &KitchenLoadService{
		slotService: slotService,
	}
}

// KitchenLoadStats содержит статистику загрузки кухни
type KitchenLoadStats struct {
	TotalLoad   float64 `json:"total_load"`   // Общая загрузка в процентах (0-100)
	Fluent      float64 `json:"fluent"`       // Свободно (0-50%)
	Congested   float64 `json:"congested"`    // Загружено (50-80%)
	Busy        float64 `json:"busy"`          // Занято (80-100%)
	CurrentLoad int     `json:"current_load"` // Текущая загрузка в рублях
	MaxCapacity  int     `json:"max_capacity"` // Максимальная емкость в рублях
	SlotsCount   int     `json:"slots_count"`  // Количество слотов, по которым считается загрузка
}

// GetKitchenLoad получает загрузку кухни
// timeWindow - временное окно для расчета:
//   - "current" - только текущий слот (15 минут)
//   - "next" - текущий + следующий слот (30 минут) - рекомендуется для оперативного управления
//   - "shift" - за всю смену (не рекомендуется для оперативного управления)
func (kls *KitchenLoadService) GetKitchenLoad(timeWindow string) (*KitchenLoadStats, error) {
	if kls.slotService == nil {
		return nil, fmt.Errorf("SlotService not available")
	}

	now := time.Now()
	stats := &KitchenLoadStats{
		TotalLoad:   0,
		Fluent:      0,
		Congested:   0,
		Busy:         0,
		CurrentLoad: 0,
		MaxCapacity:  0,
		SlotsCount:   0,
	}

	// Получаем все слоты
	allSlots, err := kls.slotService.GetAllSlots()
	if err != nil {
		return nil, fmt.Errorf("failed to get slots: %w", err)
	}

	if len(allSlots) == 0 {
		log.Printf("⚠️ GetKitchenLoad: нет доступных слотов")
		return stats, nil
	}

	// Находим текущий слот
	var currentSlot *SlotInfo
	var currentSlotIndex int = -1
	for i, slot := range allSlots {
		if !slot.StartTime.After(now) && !slot.EndTime.Before(now) {
			currentSlot = slot
			currentSlotIndex = i
			break
		}
	}

	// Если текущий слот не найден, берем ближайший будущий слот
	if currentSlot == nil {
		for i, slot := range allSlots {
			if slot.StartTime.After(now) {
				currentSlot = slot
				currentSlotIndex = i
				break
			}
		}
	}

	if currentSlot == nil {
		log.Printf("⚠️ GetKitchenLoad: текущий слот не найден")
		return stats, nil
	}

	// Определяем, какие слоты учитывать
	var slotsToCalculate []*SlotInfo

	switch timeWindow {
	case "current":
		// Только текущий слот (15 минут)
		slotsToCalculate = []*SlotInfo{currentSlot}
	case "next", "operational":
		// Текущий + следующий слот (30 минут) - рекомендуется для оперативного управления
		slotsToCalculate = []*SlotInfo{currentSlot}
		if currentSlotIndex+1 < len(allSlots) {
			slotsToCalculate = append(slotsToCalculate, allSlots[currentSlotIndex+1])
		}
	case "shift":
		// Все активные слоты за смену (для статистики, не для оперативного управления)
		// Берем только будущие и текущие слоты
		for i := currentSlotIndex; i < len(allSlots) && i < currentSlotIndex+96; i++ { // Максимум 24 часа (96 слотов по 15 минут)
			slot := allSlots[i]
			if !slot.Disabled {
				slotsToCalculate = append(slotsToCalculate, slot)
			}
		}
	default:
		// По умолчанию используем "next" (оперативное управление)
		slotsToCalculate = []*SlotInfo{currentSlot}
		if currentSlotIndex+1 < len(allSlots) {
			slotsToCalculate = append(slotsToCalculate, allSlots[currentSlotIndex+1])
		}
	}

	// Считаем общую загрузку
	totalCurrentLoad := 0
	totalMaxCapacity := 0

	for _, slot := range slotsToCalculate {
		if slot.Disabled {
			continue // Пропускаем отключенные слоты
		}
		totalCurrentLoad += slot.CurrentLoad
		totalMaxCapacity += slot.MaxCapacity
		stats.SlotsCount++
	}

	stats.CurrentLoad = totalCurrentLoad
	stats.MaxCapacity = totalMaxCapacity

	// Рассчитываем процент загрузки
	if totalMaxCapacity > 0 {
		stats.TotalLoad = (float64(totalCurrentLoad) / float64(totalMaxCapacity)) * 100
	} else {
		stats.TotalLoad = 0
	}

	// Разбиваем на категории для визуализации donut chart
	// Свободно (fluent): показывает сколько свободной емкости
	// Загружено (congested): 50-80% загрузки
	// Занято (busy): 80-100% загрузки
	
	// ИСПРАВЛЕНИЕ: Если загрузка 0%, то свободно должно быть 100%
	if stats.TotalLoad == 0 {
		// Полностью свободно
		stats.Fluent = 100
		stats.Congested = 0
		stats.Busy = 0
	} else if stats.TotalLoad <= 50 {
		// Загрузка 1-50%: свободно = 100% - загрузка, загружено = загрузка
		stats.Fluent = 100 - stats.TotalLoad // Сколько свободно от 100%
		stats.Congested = stats.TotalLoad     // Сколько загружено
		stats.Busy = 0
	} else if stats.TotalLoad <= 80 {
		// Загрузка 50-80%: свободно = 50% (максимум в категории), загружено = загрузка - 50%
		stats.Fluent = 50
		stats.Congested = stats.TotalLoad - 50
		stats.Busy = 0
	} else {
		// Загрузка 80-100%: свободно = 50%, загружено = 30%, занято = загрузка - 80%
		stats.Fluent = 50
		stats.Congested = 30 // 80 - 50
		stats.Busy = stats.TotalLoad - 80
	}

	// Ограничиваем значения до 100%
	if stats.TotalLoad > 100 {
		stats.TotalLoad = 100
		stats.Busy = 20 // 100 - 80
		stats.Congested = 30
		stats.Fluent = 50
	}

	log.Printf("📊 GetKitchenLoad (window=%s): загрузка=%.1f%%, текущая=%d₽, макс=%d₽, слотов=%d, свободно=%.1f%%, загружено=%.1f%%, занято=%.1f%%",
		timeWindow, stats.TotalLoad, stats.CurrentLoad, stats.MaxCapacity, stats.SlotsCount,
		stats.Fluent, stats.Congested, stats.Busy)

	return stats, nil
}

// GetKitchenLoadOperational получает оперативную загрузку кухни (текущий + следующий слот)
// Это рекомендуемый метод для оперативного управления в foodtech
func (kls *KitchenLoadService) GetKitchenLoadOperational() (*KitchenLoadStats, error) {
	return kls.GetKitchenLoad("next")
}

