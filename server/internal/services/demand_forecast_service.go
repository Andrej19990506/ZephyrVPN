package services

import (
	"fmt"
	"log"
	"time"

	"zephyrvpn/server/internal/models"

	"gorm.io/gorm"
)

// DemandForecastService управляет прогнозированием спроса
type DemandForecastService struct {
	db *gorm.DB
}

// NewDemandForecastService создает новый экземпляр DemandForecastService
func NewDemandForecastService(db *gorm.DB) *DemandForecastService {
	return &DemandForecastService{db: db}
}

// ForecastResult представляет результат прогноза
type ForecastResult struct {
	ForecastedQuantity  float64 `json:"forecasted_quantity"`
	ConfidenceScore     float64 `json:"confidence_score"` // 0-100%
	PredictedKitchenLoad string `json:"predicted_kitchen_load"` // 'high', 'medium', 'low'
	Method              string  `json:"method"` // 'moving_average', 'seasonal', 'ml_model'
}

// HistoricalData представляет исторические данные
type HistoricalData struct {
	LastMonthQuantity float64 `json:"last_month_quantity"` // В прошлом месяце в этот день недели
	AvgLast3Months    float64 `json:"avg_last_3_months"`  // Среднее за последние 3 месяца
	LastOrderDate     string  `json:"last_order_date"`    // Дата последнего заказа
}

// GetForecast возвращает прогноз спроса для товара на конкретную дату
func (s *DemandForecastService) GetForecast(branchID string, nomenclatureID string, date time.Time) (*ForecastResult, error) {
	// 1. Проверяем, есть ли сохраненный прогноз
	var savedForecast models.DemandForecast
	err := s.db.Where("branch_id = ? AND nomenclature_id = ? AND forecast_date = ?", branchID, nomenclatureID, date).
		First(&savedForecast).Error
	
	if err == nil {
		// Используем сохраненный прогноз, если он актуален
		if savedForecast.ValidUntil == nil || savedForecast.ValidUntil.After(time.Now()) {
			return &ForecastResult{
				ForecastedQuantity:  savedForecast.ForecastedQuantity,
				ConfidenceScore:     savedForecast.ConfidenceScore,
				PredictedKitchenLoad: savedForecast.PredictedKitchenLoad,
				Method:              savedForecast.ForecastMethod,
			}, nil
		}
	}
	
	// 2. Рассчитываем новый прогноз
	forecast, err := s.calculateForecast(branchID, nomenclatureID, date)
	if err != nil {
		return nil, fmt.Errorf("ошибка расчета прогноза: %w", err)
	}
	
	// 3. Сохраняем прогноз
	demandForecast := models.DemandForecast{
		BranchID:            branchID,
		NomenclatureID:      nomenclatureID,
		ForecastDate:        date,
		ForecastedQuantity:  forecast.ForecastedQuantity,
		ConfidenceScore:     forecast.ConfidenceScore,
		ForecastMethod:      forecast.Method,
		PredictedKitchenLoad: forecast.PredictedKitchenLoad,
		Unit:                "kg", // TODO: Получить из номенклатуры
		ValidUntil:          &time.Time{},
	}
	*demandForecast.ValidUntil = date.AddDate(0, 0, 7) // Прогноз актуален 7 дней
	
	// Обновляем или создаем прогноз
	if err == nil {
		// Обновляем существующий
		s.db.Model(&savedForecast).Updates(demandForecast)
	} else {
		// Создаем новый
		s.db.Create(&demandForecast)
	}
	
	return forecast, nil
}

// GetHistoricalData возвращает исторические данные для аналитики
func (s *DemandForecastService) GetHistoricalData(branchID string, nomenclatureID string, date time.Time) (*HistoricalData, error) {
	dayOfWeek := int(date.Weekday())
	if dayOfWeek == 0 {
		dayOfWeek = 7 // Воскресенье
	}
	
	// 1. Количество в прошлом месяце в этот же день недели
	lastMonth := date.AddDate(0, -1, 0)
	lastMonthSameDay := findSameDayOfWeek(lastMonth, dayOfWeek)
	
	var lastMonthOrder models.ProcurementHistory
	err := s.db.Where("branch_id = ? AND nomenclature_id = ? AND delivery_date = ?", branchID, nomenclatureID, lastMonthSameDay).
		First(&lastMonthOrder).Error
	
	lastMonthQuantity := 0.0
	if err == nil {
		lastMonthQuantity = lastMonthOrder.ReceivedQuantity
	}
	
	// 2. Среднее за последние 3 месяца для этого дня недели
	threeMonthsAgo := date.AddDate(0, -3, 0)
	
	var avgResult struct {
		AvgQuantity float64
	}
	err = s.db.Model(&models.ProcurementHistory{}).
		Select("COALESCE(AVG(received_quantity), 0) as avg_quantity").
		Where("branch_id = ? AND nomenclature_id = ? AND day_of_week = ? AND delivery_date >= ? AND delivery_date < ?",
			branchID, nomenclatureID, dayOfWeek, threeMonthsAgo, date).
		Scan(&avgResult).Error
	
	avgLast3Months := 0.0
	if err == nil {
		avgLast3Months = avgResult.AvgQuantity
	}
	
	// 3. Дата последнего заказа
	var lastOrder models.ProcurementHistory
	err = s.db.Where("branch_id = ? AND nomenclature_id = ?", branchID, nomenclatureID).
		Order("delivery_date DESC").
		First(&lastOrder).Error
	
	lastOrderDate := ""
	if err == nil {
		lastOrderDate = lastOrder.DeliveryDate.Format("2006-01-02")
	}
	
	return &HistoricalData{
		LastMonthQuantity: lastMonthQuantity,
		AvgLast3Months:    avgLast3Months,
		LastOrderDate:     lastOrderDate,
	}, nil
}

// calculateForecast рассчитывает прогноз спроса
// Использует метод скользящего среднего с учетом дня недели и сезонности
func (s *DemandForecastService) calculateForecast(branchID string, nomenclatureID string, date time.Time) (*ForecastResult, error) {
	dayOfWeek := int(date.Weekday())
	if dayOfWeek == 0 {
		dayOfWeek = 7 // Воскресенье
	}
	
	// 1. Получаем исторические данные за последние 3 месяца для этого дня недели
	threeMonthsAgo := date.AddDate(0, -3, 0)
	
	var history []models.ProcurementHistory
	err := s.db.Where("branch_id = ? AND nomenclature_id = ? AND day_of_week = ? AND delivery_date >= ? AND delivery_date < ?",
		branchID, nomenclatureID, dayOfWeek, threeMonthsAgo, date).
		Order("delivery_date DESC").
		Find(&history).Error
	
	if err != nil {
		return nil, fmt.Errorf("ошибка загрузки истории: %w", err)
	}
	
	// 2. Рассчитываем скользящее среднее
	var forecastedQuantity float64
	var confidenceScore float64
	method := "moving_average"
	
	if len(history) == 0 {
		// Нет исторических данных - используем минимальный прогноз
		forecastedQuantity = 0
		confidenceScore = 0
		method = "no_data"
	} else if len(history) < 3 {
		// Мало данных - простое среднее
		sum := 0.0
		for _, h := range history {
			sum += h.ReceivedQuantity
		}
		forecastedQuantity = sum / float64(len(history))
		confidenceScore = 30.0 // Низкая уверенность
	} else {
		// Скользящее среднее за последние 3 месяца
		sum := 0.0
		count := 0
		for i := 0; i < len(history) && i < 12; i++ { // Максимум 12 записей (3 месяца × 4 недели)
			sum += history[i].ReceivedQuantity
			count++
		}
		forecastedQuantity = sum / float64(count)
		confidenceScore = 70.0 // Средняя уверенность
		
		// Применяем сезонный коэффициент (если есть)
		seasonalFactor := s.getSeasonalFactor(date)
		forecastedQuantity *= seasonalFactor
	}
	
	// 3. Определяем прогнозируемую загрузку кухни
	predictedKitchenLoad := "low"
	if forecastedQuantity > 100 {
		predictedKitchenLoad = "high"
	} else if forecastedQuantity > 50 {
		predictedKitchenLoad = "medium"
	}
	
	return &ForecastResult{
		ForecastedQuantity:  forecastedQuantity,
		ConfidenceScore:     confidenceScore,
		PredictedKitchenLoad: predictedKitchenLoad,
		Method:              method,
	}, nil
}

// getSeasonalFactor возвращает сезонный коэффициент для даты
func (s *DemandForecastService) getSeasonalFactor(date time.Time) float64 {
	month := int(date.Month())
	
	// Простая сезонность: зимние месяцы (12, 1, 2) - выше спрос
	// Летние месяцы (6, 7, 8) - ниже спрос
	seasonalFactors := map[int]float64{
		1:  1.1, // Январь
		2:  1.1, // Февраль
		3:  1.0, // Март
		4:  1.0, // Апрель
		5:  0.95, // Май
		6:  0.9, // Июнь
		7:  0.9, // Июль
		8:  0.95, // Август
		9:  1.0, // Сентябрь
		10: 1.0, // Октябрь
		11: 1.05, // Ноябрь
		12: 1.1, // Декабрь
	}
	
	if factor, ok := seasonalFactors[month]; ok {
		return factor
	}
	return 1.0
}

// findSameDayOfWeek находит дату в прошлом месяце с тем же днем недели
func findSameDayOfWeek(baseDate time.Time, targetDayOfWeek int) time.Time {
	// Находим первый день месяца
	firstDay := time.Date(baseDate.Year(), baseDate.Month(), 1, 0, 0, 0, 0, baseDate.Location())
	
	// Находим первый день недели с нужным днем недели
	firstDayWeekday := int(firstDay.Weekday())
	if firstDayWeekday == 0 {
		firstDayWeekday = 7
	}
	
	daysToAdd := targetDayOfWeek - firstDayWeekday
	if daysToAdd < 0 {
		daysToAdd += 7
	}
	
	return firstDay.AddDate(0, 0, daysToAdd)
}

// BuildProcurementHistory строит историю закупок из PurchaseOrders
// Вызывается после получения заказа для обновления истории
func (s *DemandForecastService) BuildProcurementHistory(purchaseOrder *models.PurchaseOrder) error {
	if purchaseOrder.Status != models.PurchaseOrderStatusReceived {
		return nil // Только для полученных заказов
	}
	
	for _, item := range purchaseOrder.Items {
		deliveryDate := purchaseOrder.ExpectedDeliveryDate
		if purchaseOrder.ActualDeliveryDate != nil {
			deliveryDate = *purchaseOrder.ActualDeliveryDate
		}
		
		dayOfWeek := int(deliveryDate.Weekday())
		if dayOfWeek == 0 {
			dayOfWeek = 7
		}
		
		weekOfMonth := (deliveryDate.Day()-1)/7 + 1
		if weekOfMonth > 5 {
			weekOfMonth = 5
		}
		
		history := models.ProcurementHistory{
			BranchID:            purchaseOrder.BranchID,
			NomenclatureID:      item.NomenclatureID,
			OrderDate:           purchaseOrder.OrderDate,
			DeliveryDate:        deliveryDate,
			DayOfWeek:           dayOfWeek,
			WeekOfMonth:         weekOfMonth,
			OrderedQuantity:     item.OrderedQuantity,
			ReceivedQuantity:    item.ReceivedQuantity,
			Unit:                item.Unit,
			PurchasePricePerUnit: item.PurchasePricePerUnit,
			SupplierID:          &purchaseOrder.SupplierID,
			PurchaseOrderID:      &purchaseOrder.ID,
		}
		
		// Проверяем, не существует ли уже запись
		var existing models.ProcurementHistory
		err := s.db.Where("purchase_order_id = ? AND nomenclature_id = ?", purchaseOrder.ID, item.NomenclatureID).
			First(&existing).Error
		
		if err == gorm.ErrRecordNotFound {
			// Создаем новую запись
			if err := s.db.Create(&history).Error; err != nil {
				log.Printf("❌ Ошибка создания истории закупок: %v", err)
			}
		} else if err == nil {
			// Обновляем существующую
			if err := s.db.Model(&existing).Updates(history).Error; err != nil {
				log.Printf("❌ Ошибка обновления истории закупок: %v", err)
			}
		}
	}
	
	return nil
}

