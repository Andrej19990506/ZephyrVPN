package services

import (
	"fmt"
	"log"
	"time"

	"zephyrvpn/server/internal/utils"
)

// DailyPlanService управляет планом на день
type DailyPlanService struct {
	redisUtil *utils.RedisClient
}

// NewDailyPlanService создает новый сервис плана на день
func NewDailyPlanService(redisUtil *utils.RedisClient) *DailyPlanService {
	return &DailyPlanService{
		redisUtil: redisUtil,
	}
}

// GetDailyPlan получает план на день
// date - дата в формате "2006-01-02", если пустая - сегодня
func (dps *DailyPlanService) GetDailyPlan(date string) (float64, error) {
	if dps.redisUtil == nil {
		return 0, fmt.Errorf("Redis not available")
	}

	// Если дата не указана, используем сегодня
	if date == "" {
		date = time.Now().Format("2006-01-02")
	}

	planKey := fmt.Sprintf("erp:daily_plan:%s", date)
	planStr, err := dps.redisUtil.Get(planKey)
	if err != nil || planStr == "" {
		// Если плана нет, возвращаем значение по умолчанию
		return 500000.0, nil
	}

	// Парсим значение плана
	var plan float64
	_, err = fmt.Sscanf(planStr, "%f", &plan)
	if err != nil {
		log.Printf("⚠️ GetDailyPlan: ошибка парсинга плана для %s: %v", date, err)
		return 500000.0, nil
	}

	return plan, nil
}

// SetDailyPlan устанавливает план на день
// date - дата в формате "2006-01-02", если пустая - сегодня
// plan - план в рублях
func (dps *DailyPlanService) SetDailyPlan(date string, plan float64) error {
	if dps.redisUtil == nil {
		return fmt.Errorf("Redis not available")
	}

	// Если дата не указана, используем сегодня
	if date == "" {
		date = time.Now().Format("2006-01-02")
	}

	planKey := fmt.Sprintf("erp:daily_plan:%s", date)
	planStr := fmt.Sprintf("%.2f", plan)
	
	// Сохраняем план на 30 дней (TTL)
	err := dps.redisUtil.Set(planKey, planStr, 30*24*time.Hour)
	if err != nil {
		return fmt.Errorf("failed to save daily plan: %v", err)
	}

	log.Printf("✅ SetDailyPlan: план на %s установлен: %.2f руб.", date, plan)
	return nil
}

// GetDailyPlanForToday получает план на сегодня
func (dps *DailyPlanService) GetDailyPlanForToday() (float64, error) {
	return dps.GetDailyPlan("")
}

