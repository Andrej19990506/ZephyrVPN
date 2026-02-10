package services

import (
	"fmt"
	"log"
	"time"

	"zephyrvpn/server/internal/models"

	"gorm.io/gorm"
)

// ProcurementPlanningService управляет планами закупок
type ProcurementPlanningService struct {
	db                  *gorm.DB
	purchaseOrderService *PurchaseOrderService
	forecastService     *DemandForecastService
}

// NewProcurementPlanningService создает новый экземпляр ProcurementPlanningService
func NewProcurementPlanningService(db *gorm.DB, purchaseOrderService *PurchaseOrderService, forecastService *DemandForecastService) *ProcurementPlanningService {
	return &ProcurementPlanningService{
		db:                  db,
		purchaseOrderService: purchaseOrderService,
		forecastService:     forecastService,
	}
}

// MonthlyPlanResponse представляет ответ для GET /api/v1/procurement/monthly-plan
type MonthlyPlanResponse struct {
	Plan        *models.ProcurementPlan   `json:"plan"`
	Matrix      []MonthlyPlanMatrixRow     `json:"matrix"`      // Матрица: строки (товары) × колонки (даты)
	Dates       []string                   `json:"dates"`       // Список дат месяца (для колонок)
	Analytics   MonthlyPlanAnalytics       `json:"analytics"`   // Общая аналитика по плану
}

// MonthlyPlanMatrixRow представляет строку матрицы (товар)
type MonthlyPlanMatrixRow struct {
	NomenclatureID   string                `json:"nomenclature_id"`
	NomenclatureName string                `json:"nomenclature_name"`
	SKU              string                `json:"sku"`
	Unit             string                `json:"unit"`
	CategoryName     string                `json:"category_name"`
	Cells            []MonthlyPlanCell     `json:"cells"`      // Ячейки для каждой даты
	TotalPlanned     float64               `json:"total_planned"` // Сумма по всем дням
	SuggestedSupplier *SupplierSuggestion  `json:"suggested_supplier,omitempty"` // Рекомендуемый поставщик
}

// MonthlyPlanCell представляет ячейку матрицы (день × товар)
type MonthlyPlanCell struct {
	Date                string  `json:"date"`                 // Дата в формате YYYY-MM-DD
	PlannedQuantity     float64 `json:"planned_quantity"`     // Планируемое количество (вводится менеджером)
	ForecastedQuantity  float64 `json:"forecasted_quantity"`   // Прогнозируемое количество
	ForecastConfidence  float64 `json:"forecast_confidence"`   // Уверенность прогноза (0-100%)
	PredictedKitchenLoad string `json:"predicted_kitchen_load"` // 'high', 'medium', 'low'
	LastMonthQuantity   float64 `json:"last_month_quantity"`   // Количество в прошлом месяце в этот день недели
	AvgLast3Months      float64 `json:"avg_last_3_months"`    // Среднее за последние 3 месяца
	HasData             bool    `json:"has_data"`              // Есть ли данные для этой ячейки
}

// SupplierSuggestion представляет рекомендацию по поставщику
type SupplierSuggestion struct {
	SupplierID      string  `json:"supplier_id"`
	SupplierName    string  `json:"supplier_name"`
	PricePerUnit    float64 `json:"price_per_unit"`
	LastOrderDate   string  `json:"last_order_date"`
	ReliabilityScore float64 `json:"reliability_score"` // 0-100%
}

// MonthlyPlanAnalytics представляет общую аналитику по плану
type MonthlyPlanAnalytics struct {
	TotalItems        int     `json:"total_items"`         // Всего товаров в плане
	TotalDays         int     `json:"total_days"`          // Количество дней в месяце
	TotalPlannedValue float64 `json:"total_planned_value"` // Общая стоимость плана
	HighLoadDays      int     `json:"high_load_days"`      // Дней с высокой загрузкой
	MediumLoadDays    int     `json:"medium_load_days"`    // Дней со средней загрузкой
	LowLoadDays       int     `json:"low_load_days"`       // Дней с низкой загрузкой
}

// GetMonthlyPlan возвращает план на месяц с матрицей данных
func (s *ProcurementPlanningService) GetMonthlyPlan(branchID string, year int, month int) (*MonthlyPlanResponse, error) {
	// 1. Создаем или загружаем план
	planMonth := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	
	var plan models.ProcurementPlan
	err := s.db.Where("branch_id = ? AND year = ? AND month_number = ? AND deleted_at IS NULL", branchID, year, month).
		Preload("Items").
		Preload("Items.Nomenclature").
		Preload("Items.SuggestedSupplier").
		Preload("Branch").
		First(&plan).Error
	
	if err == gorm.ErrRecordNotFound {
		// Создаем новый план
		plan = models.ProcurementPlan{
			BranchID:    branchID,
			Month:       planMonth,
			Year:        year,
			MonthNumber: month,
			Status:      models.ProcurementPlanStatusDraft,
			CreatedBy:   "system", // Будет обновлено при сохранении
		}
		if err := s.db.Create(&plan).Error; err != nil {
			return nil, fmt.Errorf("ошибка создания плана: %w", err)
		}
	} else if err != nil {
		return nil, fmt.Errorf("ошибка загрузки плана: %w", err)
	}
	
	// 2. Получаем список всех активных товаров из номенклатуры
	var nomenclatureItems []models.NomenclatureItem
	if err := s.db.Where("is_active = true AND deleted_at IS NULL").
		Order("category_name, name").
		Find(&nomenclatureItems).Error; err != nil {
		return nil, fmt.Errorf("ошибка загрузки номенклатуры: %w", err)
	}
	
	// 3. Генерируем список дат месяца
	dates := generateMonthDates(year, month)
	
	// 4. Строим матрицу: для каждого товара создаем строку с ячейками для каждой даты
	matrix := make([]MonthlyPlanMatrixRow, 0, len(nomenclatureItems))
	
	for _, item := range nomenclatureItems {
		row := MonthlyPlanMatrixRow{
			NomenclatureID:   item.ID,
			NomenclatureName: item.Name,
			SKU:              item.SKU,
			Unit:             item.InboundUnit,
			CategoryName:     item.CategoryName,
			Cells:            make([]MonthlyPlanCell, 0, len(dates)),
			TotalPlanned:     0,
		}
		
		// Для каждой даты создаем ячейку
		for _, dateStr := range dates {
			date, _ := time.Parse("2006-01-02", dateStr)
			
			// Ищем существующую позицию плана для этой даты и товара
			var planItem *models.ProcurementPlanItem
			for i := range plan.Items {
				if plan.Items[i].NomenclatureID == item.ID && 
				   plan.Items[i].PlanDate.Format("2006-01-02") == dateStr {
					planItem = &plan.Items[i]
					break
				}
			}
			
			// Получаем прогноз и исторические данные
			forecast, _ := s.forecastService.GetForecast(branchID, item.ID, date)
			history, _ := s.forecastService.GetHistoricalData(branchID, item.ID, date)
			
			cell := MonthlyPlanCell{
				Date:                dateStr,
				PlannedQuantity:     0,
				ForecastedQuantity:  forecast.ForecastedQuantity,
				ForecastConfidence:  forecast.ConfidenceScore,
				PredictedKitchenLoad: forecast.PredictedKitchenLoad,
				LastMonthQuantity:   history.LastMonthQuantity,
				AvgLast3Months:      history.AvgLast3Months,
				HasData:             planItem != nil,
			}
			
			if planItem != nil {
				cell.PlannedQuantity = planItem.PlannedQuantity
				row.TotalPlanned += planItem.PlannedQuantity
			}
			
			row.Cells = append(row.Cells, cell)
		}
		
		// Получаем рекомендацию по поставщику
		supplierSuggestion, _ := s.getSupplierSuggestion(branchID, item.ID)
		if supplierSuggestion != nil {
			row.SuggestedSupplier = supplierSuggestion
		}
		
		matrix = append(matrix, row)
	}
	
	// 5. Рассчитываем аналитику
	analytics := s.calculateAnalytics(matrix, dates)
	
	return &MonthlyPlanResponse{
		Plan:      &plan,
		Matrix:    matrix,
		Dates:     dates,
		Analytics: analytics,
	}, nil
}

// UpdatePlanCell обновляет ячейку в плане (количество для конкретной даты и товара)
func (s *ProcurementPlanningService) UpdatePlanCell(planID string, nomenclatureID string, dateStr string, quantity float64) error {
	date, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return fmt.Errorf("неверный формат даты: %w", err)
	}
	
	// Ищем существующую позицию
	var planItem models.ProcurementPlanItem
	err = s.db.Where("plan_id = ? AND nomenclature_id = ? AND plan_date = ?", planID, nomenclatureID, date).
		First(&planItem).Error
	
	if err == gorm.ErrRecordNotFound {
		// Создаем новую позицию
		planItem = models.ProcurementPlanItem{
			PlanID:          planID,
			NomenclatureID: nomenclatureID,
			PlanDate:       date,
			PlannedQuantity: quantity,
		}
		
		// Получаем единицу измерения из номенклатуры
		var nomenclature models.NomenclatureItem
		if err := s.db.First(&nomenclature, "id = ?", nomenclatureID).Error; err == nil {
			planItem.Unit = nomenclature.InboundUnit
		}
		
		// Получаем прогноз и исторические данные
		forecast, _ := s.forecastService.GetForecast("", nomenclatureID, date)
		history, _ := s.forecastService.GetHistoricalData("", nomenclatureID, date)
		
		planItem.ForecastedQuantity = forecast.ForecastedQuantity
		planItem.ForecastConfidence = forecast.ConfidenceScore
		planItem.PredictedKitchenLoad = forecast.PredictedKitchenLoad
		planItem.LastMonthQuantity = history.LastMonthQuantity
		planItem.AvgLast3Months = history.AvgLast3Months
		
		// Получаем рекомендацию по поставщику
		supplierSuggestion, _ := s.getSupplierSuggestion("", nomenclatureID)
		if supplierSuggestion != nil {
			planItem.SuggestedSupplierID = &supplierSuggestion.SupplierID
			planItem.SuggestedPricePerUnit = supplierSuggestion.PricePerUnit
		}
		
		if err := s.db.Create(&planItem).Error; err != nil {
			return fmt.Errorf("ошибка создания позиции: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("ошибка поиска позиции: %w", err)
	} else {
		// Обновляем существующую позицию
		planItem.PlannedQuantity = quantity
		if err := s.db.Save(&planItem).Error; err != nil {
			return fmt.Errorf("ошибка обновления позиции: %w", err)
		}
	}
	
	return nil
}

// SubmitPlan обрабатывает отправку плана и создает PurchaseOrders
func (s *ProcurementPlanningService) SubmitPlan(planID string, createdBy string) error {
	// 1. Загружаем план
	var plan models.ProcurementPlan
	if err := s.db.Preload("Items").
		Preload("Items.Nomenclature").
		Preload("Items.SuggestedSupplier").
		Preload("Branch").
		First(&plan, "id = ? AND deleted_at IS NULL", planID).Error; err != nil {
		return fmt.Errorf("план не найден: %w", err)
	}
	
	if plan.Status != models.ProcurementPlanStatusDraft {
		return fmt.Errorf("можно отправить только черновик (текущий статус: %s)", plan.Status)
	}
	
	// 2. Группируем позиции по дате и поставщику
	// Структура: date -> supplier_id -> []items
	ordersByDateAndSupplier := make(map[string]map[string][]models.ProcurementPlanItem)
	
	for _, item := range plan.Items {
		if item.PlannedQuantity <= 0 {
			continue // Пропускаем пустые позиции
		}
		
		dateStr := item.PlanDate.Format("2006-01-02")
		supplierID := ""
		if item.SuggestedSupplierID != nil {
			supplierID = *item.SuggestedSupplierID
		} else {
			// Если поставщик не указан, используем последнего поставщика для этого товара
			suggestion, _ := s.getSupplierSuggestion(plan.BranchID, item.NomenclatureID)
			if suggestion != nil {
				supplierID = suggestion.SupplierID
			} else {
				log.Printf("⚠️ Поставщик не найден для товара %s, пропускаем", item.Nomenclature.Name)
				continue
			}
		}
		
		if ordersByDateAndSupplier[dateStr] == nil {
			ordersByDateAndSupplier[dateStr] = make(map[string][]models.ProcurementPlanItem)
		}
		ordersByDateAndSupplier[dateStr][supplierID] = append(ordersByDateAndSupplier[dateStr][supplierID], item)
	}
	
	// 3. Создаем PurchaseOrders для каждой комбинации дата × поставщик
	createdOrders := 0
	for dateStr, suppliersMap := range ordersByDateAndSupplier {
		date, _ := time.Parse("2006-01-02", dateStr)
		
		for supplierID, items := range suppliersMap {
			// Создаем заказ на закупку
			purchaseOrder := &models.PurchaseOrder{
				SupplierID:          supplierID,
				BranchID:            plan.BranchID,
				Status:              models.PurchaseOrderStatusDraft,
				OrderDate:           time.Now(),
				ExpectedDeliveryDate: date,
				Currency:            "RUB",
				PaymentMethod:       "bank",
				CreatedBy:           createdBy,
				Notes:               fmt.Sprintf("Автоматически создан из плана %s", plan.PlanNumber),
			}
			
			// Создаем позиции заказа
			purchaseOrder.Items = make([]models.PurchaseOrderItem, 0, len(items))
			totalAmount := 0.0
			
			for _, planItem := range items {
				pricePerUnit := planItem.SuggestedPricePerUnit
				if pricePerUnit == 0 {
					// Если цена не указана, берем из номенклатуры
					pricePerUnit = planItem.Nomenclature.LastPrice
				}
				
				orderItem := models.PurchaseOrderItem{
					NomenclatureID:      planItem.NomenclatureID,
					OrderedQuantity:      planItem.PlannedQuantity,
					Unit:                planItem.Unit,
					PurchasePricePerUnit: pricePerUnit,
					TotalPrice:          planItem.PlannedQuantity * pricePerUnit,
				}
				
				purchaseOrder.Items = append(purchaseOrder.Items, orderItem)
				totalAmount += orderItem.TotalPrice
			}
			
			purchaseOrder.TotalAmount = totalAmount
			
			// Сохраняем заказ через PurchaseOrderService
			if err := s.purchaseOrderService.CreatePurchaseOrder(purchaseOrder); err != nil {
				log.Printf("❌ Ошибка создания заказа для даты %s и поставщика %s: %v", dateStr, supplierID, err)
				continue
			}
			
			createdOrders++
			log.Printf("✅ Создан заказ %s для даты %s, поставщик %s, сумма %.2f₽", purchaseOrder.OrderNumber, dateStr, supplierID, totalAmount)
		}
	}
	
	// 4. Обновляем статус плана
	plan.Status = models.ProcurementPlanStatusSubmitted
	now := time.Now()
	plan.SubmittedAt = &now
	if err := s.db.Save(&plan).Error; err != nil {
		return fmt.Errorf("ошибка обновления статуса плана: %w", err)
	}
	
	log.Printf("✅ План %s отправлен. Создано заказов: %d", plan.PlanNumber, createdOrders)
	return nil
}

// getSupplierSuggestion возвращает рекомендацию по поставщику для товара
func (s *ProcurementPlanningService) getSupplierSuggestion(branchID string, nomenclatureID string) (*SupplierSuggestion, error) {
	// Ищем последний заказ на этот товар
	var lastOrder models.PurchaseOrderItem
	err := s.db.Where("nomenclature_id = ?", nomenclatureID).
		Joins("JOIN purchase_orders ON purchase_order_items.purchase_order_id = purchase_orders.id").
		Where("purchase_orders.branch_id = ? OR ? = ''", branchID, branchID).
		Order("purchase_orders.order_date DESC").
		Preload("PurchaseOrder").
		Preload("PurchaseOrder.Supplier").
		First(&lastOrder).Error
	
	if err == gorm.ErrRecordNotFound {
		return nil, nil // Нет исторических данных
	} else if err != nil {
		return nil, fmt.Errorf("ошибка поиска поставщика: %w", err)
	}
	
	if lastOrder.PurchaseOrder == nil || lastOrder.PurchaseOrder.Supplier == nil {
		return nil, nil
	}
	
	supplier := lastOrder.PurchaseOrder.Supplier
	lastOrderDate := ""
	if lastOrder.PurchaseOrder.OrderDate != (time.Time{}) {
		lastOrderDate = lastOrder.PurchaseOrder.OrderDate.Format("2006-01-02")
	}
	
	return &SupplierSuggestion{
		SupplierID:      supplier.ID,
		SupplierName:    supplier.Name,
		PricePerUnit:    lastOrder.PurchasePricePerUnit,
		LastOrderDate:   lastOrderDate,
		ReliabilityScore: 85.0, // TODO: Рассчитывать на основе истории
	}, nil
}

// calculateAnalytics рассчитывает общую аналитику по плану
func (s *ProcurementPlanningService) calculateAnalytics(matrix []MonthlyPlanMatrixRow, dates []string) MonthlyPlanAnalytics {
	analytics := MonthlyPlanAnalytics{
		TotalItems: len(matrix),
		TotalDays:  len(dates),
	}
	
	highLoadDays := make(map[string]bool)
	mediumLoadDays := make(map[string]bool)
	lowLoadDays := make(map[string]bool)
	
	for _, row := range matrix {
		for _, cell := range row.Cells {
			if cell.PlannedQuantity > 0 {
				switch cell.PredictedKitchenLoad {
				case "high":
					highLoadDays[cell.Date] = true
				case "medium":
					mediumLoadDays[cell.Date] = true
				case "low":
					lowLoadDays[cell.Date] = true
				}
			}
		}
	}
	
	analytics.HighLoadDays = len(highLoadDays)
	analytics.MediumLoadDays = len(mediumLoadDays)
	analytics.LowLoadDays = len(lowLoadDays)
	
	// TODO: Рассчитать TotalPlannedValue на основе цен поставщиков
	
	return analytics
}

// generateMonthDates генерирует список дат месяца
func generateMonthDates(year int, month int) []string {
	firstDay := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	lastDay := firstDay.AddDate(0, 1, -1) // Последний день месяца
	
	dates := make([]string, 0, lastDay.Day())
	current := firstDay
	
	for current.Before(lastDay) || current.Equal(lastDay) {
		dates = append(dates, current.Format("2006-01-02"))
		current = current.AddDate(0, 0, 1)
	}
	
	return dates
}



