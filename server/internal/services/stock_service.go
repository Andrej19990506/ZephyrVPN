package services

import (
	"fmt"
	"log"
	"time"

	"github.com/shopspring/decimal"
	"zephyrvpn/server/internal/models"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// StockService управляет остатками товаров, партиями и сроками годности
type StockService struct {
	db                *gorm.DB
	counterpartyService *CounterpartyService
	financeService     *FinanceService
}

// GetDB возвращает экземпляр БД для доступа из других сервисов
func (s *StockService) GetDB() *gorm.DB {
	return s.db
}

// NewStockService создает новый экземпляр StockService
func NewStockService(db *gorm.DB) *StockService {
	return &StockService{db: db}
}

// SetCounterpartyService устанавливает сервис контрагентов
func (s *StockService) SetCounterpartyService(cs *CounterpartyService) {
	s.counterpartyService = cs
}

// SetFinanceService устанавливает сервис финансов
func (s *StockService) SetFinanceService(fs *FinanceService) {
	s.financeService = fs
}

// calculateBatchValue рассчитывает стоимость батча по правильной формуле
// КРИТИЧЕСКИ ВАЖНО: Формула должна быть ТОЧНО такой:
// TotalValue = (RemainingQuantityInGrams * CostPerKg) / 1000
// 
// Пример: 10кг майонеза по 1,234₽/кг
// - RemainingQuantity = 10000г (в BaseUnit)
// - CostPerUnit = 1234₽/кг (цена за 1кг, НЕ за грамм!)
// - Calculation: (10000 * 1234) / 1000 = 12,340,000 / 1000 = 12,340₽
// 
// Параметры:
//   - remainingQty: Остаток в BaseUnit (граммы/миллилитры)
//   - costPerUnit: Цена за InboundUnit (цена за 1кг/1л, НЕ за грамм!)
//   - conversionFactor: Коэффициент конвертации из BaseUnit в InboundUnit (1000 для г->кг, мл->л)
//
// Возвращает: Стоимость остатка в рублях
func calculateBatchValue(remainingQty decimal.Decimal, costPerUnit decimal.Decimal, conversionFactor decimal.Decimal) decimal.Decimal {
	// ВАЖНО: Сначала умножаем, потом делим - это избегает потери точности
	// Формула: (RemainingQuantity * CostPerUnit) / ConversionFactor
	// Пример: (10000г * 1234₽/кг) / 1000 = 12,340₽
	total := remainingQty.Mul(costPerUnit)
	
	// Делим на коэффициент конвертации ТОЛЬКО если он не равен 1
	// Для товаров в граммах/миллилитрах: conversionFactor = 1000
	// Для товаров в штуках: conversionFactor = 1 (деление не нужно)
	if !conversionFactor.Equal(decimal.NewFromInt(1)) {
		result := total.Div(conversionFactor)
		// Логирование для отладки (можно убрать после проверки)
		log.Printf("💰 calculateBatchValue: %.2f %s * %.2f₽/%s / %.0f = %.2f₽",
			remainingQty.InexactFloat64(), "base_unit",
			costPerUnit.InexactFloat64(), "major_unit",
			conversionFactor.InexactFloat64(),
			result.InexactFloat64())
		return result
	}
	
	return total
}

// GetStockItems возвращает остатки товаров с учетом партий и сроков годности
func (s *StockService) GetStockItems(branchID string, includeExpired bool) ([]map[string]interface{}, error) {
	type BatchWithBranch struct {
		models.StockBatch
		BranchName string `gorm:"column:branch_name"`
	}
	
	var batches []models.StockBatch
	
	query := s.db.Model(&models.StockBatch{}).
		Preload("Nomenclature").
		Where("remaining_quantity > 0")
	
	if branchID != "" && branchID != "all" {
		query = query.Where("branch_id = ?", branchID)
	}
	
	if !includeExpired {
		query = query.Where("is_expired = false")
	}
	
	if err := query.Find(&batches).Error; err != nil {
		return nil, err
	}
	
	// Загружаем филиалы для получения имен
	branchMap := make(map[string]string)
	var branchIDs []string
	for _, batch := range batches {
		if _, exists := branchMap[batch.BranchID]; !exists {
			branchIDs = append(branchIDs, batch.BranchID)
		}
	}
	
	if len(branchIDs) > 0 {
		var branches []models.Branch
		if err := s.db.Where("id IN ?", branchIDs).Find(&branches).Error; err == nil {
			for _, branch := range branches {
				branchMap[branch.ID] = branch.Name
			}
		}
	}
	
	// Загружаем накладные для получения номеров
	invoiceMap := make(map[string]string) // invoiceID -> invoiceNumber
	var invoiceIDs []string
	for _, batch := range batches {
		if batch.InvoiceID != nil && *batch.InvoiceID != "" {
			if _, exists := invoiceMap[*batch.InvoiceID]; !exists {
				invoiceIDs = append(invoiceIDs, *batch.InvoiceID)
			}
		}
	}
	
	if len(invoiceIDs) > 0 {
		var invoices []models.Invoice
		if err := s.db.Where("id IN ?", invoiceIDs).Find(&invoices).Error; err == nil {
			for _, invoice := range invoices {
				invoiceMap[invoice.ID] = invoice.Number
			}
		}
	}
	
	// Группируем по товарам и филиалам
	stockMap := make(map[string]map[string]interface{})
	
		for _, batch := range batches {
		key := batch.NomenclatureID + "_" + batch.BranchID
		nomenclature := batch.Nomenclature
		
		// Вычисляем коэффициент конвертации для правильного расчета стоимости
		// cost_per_unit всегда за InboundUnit (кг/л/шт), а current_stock может быть в Base Unit (г)
		conversionFactor := decimal.NewFromInt(1)
		baseUnit := nomenclature.BaseUnit
		inboundUnit := nomenclature.InboundUnit
		
		// Если единицы разные, вычисляем коэффициент конвертации
		if baseUnit != inboundUnit && inboundUnit != "" {
			if (baseUnit == "g" && inboundUnit == "kg") || (baseUnit == "ml" && inboundUnit == "l") {
				conversionFactor = decimal.NewFromInt(1000) // граммы в килограммы, миллилитры в литры
			} else if (baseUnit == "kg" && inboundUnit == "g") || (baseUnit == "l" && inboundUnit == "ml") {
				conversionFactor = decimal.NewFromFloat(0.001)
			} else if nomenclature.ConversionFactor > 0 {
				// Используем коэффициент конвертации из модели
				conversionFactor = decimal.NewFromFloat(nomenclature.ConversionFactor)
			}
		}
		
			// ВАЖНО: CostPerUnit никогда не меняется - это константа закупки (цена за InboundUnit)
		// КРИТИЧЕСКИ ВАЖНО: CostPerUnit должен быть ценой за 1кг/1л, НЕ за грамм!
		// ПРАВИЛЬНАЯ формула расчета стоимости:
		// TotalValue = (RemainingQuantityInGrams * CostPerKg) / 1000
		// Пример: (10000г * 1234₽/кг) / 1000 = 12,340₽
		// 
		// КРИТИЧЕСКИ ВАЖНО: Проверяем, что CostPerUnit - это цена за 1кг/1л, НЕ за грамм!
		// Если CostPerUnit < 10, возможно он сохранен как цена за грамм (неправильно!)
		// Пример: если цена должна быть 1234₽/кг, но сохранена как 1.234₽, это ошибка!
		// В этом случае нужно умножить CostPerUnit на 1000 для правильного расчета
		var correctedCostPerUnit float64 = batch.CostPerUnit
		if batch.CostPerUnit > 0 && batch.CostPerUnit < 10 && (baseUnit == "g" && inboundUnit == "kg") {
			log.Printf("⚠️ ВНИМАНИЕ: CostPerUnit кажется слишком низким для товара %s (ID: %s)", nomenclature.Name, batch.NomenclatureID)
			log.Printf("   CostPerUnit в БД: %.4f₽/%s - возможно сохранена цена за грамм вместо цены за кг!", batch.CostPerUnit, inboundUnit)
			log.Printf("   Исправляем: умножаем на 1000 -> %.2f₽/кг", batch.CostPerUnit*1000)
			correctedCostPerUnit = batch.CostPerUnit * 1000 // Исправляем цену
		} else if batch.CostPerUnit > 0 && batch.CostPerUnit < 10 && (baseUnit == "ml" && inboundUnit == "l") {
			log.Printf("⚠️ ВНИМАНИЕ: CostPerUnit кажется слишком низким для товара %s (ID: %s)", nomenclature.Name, batch.NomenclatureID)
			log.Printf("   CostPerUnit в БД: %.4f₽/%s - возможно сохранена цена за мл вместо цены за л!", batch.CostPerUnit, inboundUnit)
			log.Printf("   Исправляем: умножаем на 1000 -> %.2f₽/л", batch.CostPerUnit*1000)
			correctedCostPerUnit = batch.CostPerUnit * 1000 // Исправляем цену
		}
		
		// Логирование для отладки
		log.Printf("🔍 GetStockItems: расчет стоимости для %s (ID: %s)", nomenclature.Name, batch.NomenclatureID)
		log.Printf("   RemainingQuantity: %.2f %s", batch.RemainingQuantity, baseUnit)
		log.Printf("   CostPerUnit (из БД): %.4f₽/%s", batch.CostPerUnit, inboundUnit)
		if correctedCostPerUnit != batch.CostPerUnit {
			log.Printf("   CostPerUnit (исправлено): %.2f₽/%s", correctedCostPerUnit, inboundUnit)
		}
		log.Printf("   ConversionFactor: %.0f", conversionFactor.InexactFloat64())
		// КРИТИЧЕСКИ ВАЖНО: batch.RemainingQuantity должен быть в BaseUnit
		// Если BaseUnit = "g", а RemainingQuantity < 1000, возможно он сохранен в кг - конвертируем
		var batchRemainingQtyForCalc float64 = batch.RemainingQuantity
		if baseUnit == "g" && batch.RemainingQuantity < 1000 && batch.RemainingQuantity > 0 {
			// Умножаем на 1000 для конвертации в граммы
			batchRemainingQtyForCalc = batch.RemainingQuantity * 1000
		} else if baseUnit == "ml" && batch.RemainingQuantity < 1000 && batch.RemainingQuantity > 0 {
			// Аналогично для миллилитров
			batchRemainingQtyForCalc = batch.RemainingQuantity * 1000
		}
		
		log.Printf("   Формула: (%.2f * %.2f) / %.0f", batchRemainingQtyForCalc, correctedCostPerUnit, conversionFactor.InexactFloat64())
		
		batchCostValueDecimal := calculateBatchValue(
			decimal.NewFromFloat(batchRemainingQtyForCalc), // Остаток в BaseUnit (граммы/мл) - исправленный если нужно
			decimal.NewFromFloat(correctedCostPerUnit),       // Цена за InboundUnit (цена за 1кг/1л) - исправленная если нужно
			conversionFactor,                                 // Коэффициент конвертации (1000 для г->кг)
		)
		
		log.Printf("   Результат: %.2f₽", batchCostValueDecimal.InexactFloat64())
		// Правильный расчет ожидаемого результата с учетом реального BaseUnit
		var expectedResult float64
		if conversionFactor.GreaterThan(decimal.NewFromInt(1)) {
			expectedResult = (batchRemainingQtyForCalc * correctedCostPerUnit) / conversionFactor.InexactFloat64()
		} else {
			expectedResult = batchRemainingQtyForCalc * correctedCostPerUnit
		}
		log.Printf("   Ожидаемый результат для %.2f %s по %.2f₽/%s: %.2f₽", 
			batchRemainingQtyForCalc, baseUnit, correctedCostPerUnit, inboundUnit, expectedResult)
		
		if stockItem, exists := stockMap[key]; exists {
			// Обновляем существующий товар
			// КРИТИЧЕСКИ ВАЖНО: batch.RemainingQuantity должен быть в BaseUnit
			// Если BaseUnit = "g", а RemainingQuantity < 1000, возможно он сохранен в кг - конвертируем
			var batchRemainingQty float64 = batch.RemainingQuantity
			if baseUnit == "g" && batch.RemainingQuantity < 1000 && batch.RemainingQuantity > 0 {
				// Проверяем, возможно RemainingQuantity сохранен в килограммах вместо граммов
				// Если значение меньше 1000 и больше 0, вероятно это килограммы
				// Умножаем на 1000 для конвертации в граммы
				batchRemainingQty = batch.RemainingQuantity * 1000
				log.Printf("⚠️ GetStockItems: исправление единиц для %s (ID: %s): %.2f кг -> %.2f г",
					nomenclature.Name, batch.NomenclatureID, batch.RemainingQuantity, batchRemainingQty)
			} else if baseUnit == "ml" && batch.RemainingQuantity < 1000 && batch.RemainingQuantity > 0 {
				// Аналогично для миллилитров
				batchRemainingQty = batch.RemainingQuantity * 1000
				log.Printf("⚠️ GetStockItems: исправление единиц для %s (ID: %s): %.2f л -> %.2f мл",
					nomenclature.Name, batch.NomenclatureID, batch.RemainingQuantity, batchRemainingQty)
			}
			currentStock := stockItem["current_stock"].(float64) + batchRemainingQty
			stockItem["current_stock"] = currentStock
			
			// Суммируем стоимость всех батчей (каждый батч может иметь свою цену)
			// ВАЖНО: Не пересчитываем общую стоимость по средневзвешенной цене,
			// а суммируем стоимость каждого батча отдельно по правильной формуле:
			// TotalCost = Sum((RemainingQuantity_i * CostPerUnit_i) / ConversionFactor)
			// Сначала умножаем, потом делим - это избегает потери точности
			existingCostValue := decimal.NewFromFloat(stockItem["cost_value"].(float64))
			totalCostValue := existingCostValue.Add(batchCostValueDecimal)
			stockItem["cost_value"] = totalCostValue.InexactFloat64()
			
			// ПРИМЕЧАНИЕ: cost_per_unit в итоговом объекте берется от последнего батча (для отображения)
			// Реальная стоимость рассчитывается через суммирование стоимости каждого батча отдельно
			// Обновляем cost_per_unit в stockItem на исправленное значение
			stockItem["cost_per_unit"] = correctedCostPerUnit
			
			// Обновляем branch_name, если его еще нет
			if _, hasBranchName := stockItem["branch_name"]; !hasBranchName {
				stockItem["branch_name"] = branchMap[batch.BranchID]
			}
			
			// Обновляем информацию о сроках годности
			batchesList := stockItem["batches"].([]map[string]interface{})
			
			// Исправляем cost_per_unit если он сохранен как цена за грамм
			var batchCorrectedCostPerUnit float64 = batch.CostPerUnit
			if batch.CostPerUnit > 0 && batch.CostPerUnit < 10 && (baseUnit == "g" && inboundUnit == "kg") {
				batchCorrectedCostPerUnit = batch.CostPerUnit * 1000
			} else if batch.CostPerUnit > 0 && batch.CostPerUnit < 10 && (baseUnit == "ml" && inboundUnit == "l") {
				batchCorrectedCostPerUnit = batch.CostPerUnit * 1000
			}
			
			batchData := map[string]interface{}{
				"id":                batch.ID,
				"quantity":          batch.RemainingQuantity,
				"expiry_at":         batch.ExpiryAt,
				"days_until_expiry": s.calculateDaysUntilExpiry(batch.ExpiryAt),
				"hours_until_expiry": s.calculateHoursUntilExpiry(batch.ExpiryAt),
				"is_expired":        batch.IsExpired,
				"is_at_risk":        s.isAtRisk(batch),
				"cost_per_unit":     batchCorrectedCostPerUnit, // Цена за InboundUnit (исправленная если нужно)
			}
			// Добавляем информацию о накладной, если есть
			if batch.InvoiceID != nil && *batch.InvoiceID != "" {
				batchData["invoice_id"] = *batch.InvoiceID
				if invoiceNumber, exists := invoiceMap[*batch.InvoiceID]; exists {
					batchData["invoice_number"] = invoiceNumber
				}
			}
			batchesList = append(batchesList, batchData)
			stockItem["batches"] = batchesList
		} else {
			// Создаем новый товар
			minStock := nomenclature.MinStockLevel
			// КРИТИЧЕСКИ ВАЖНО: batch.RemainingQuantity должен быть в BaseUnit
			// Если BaseUnit = "g", а RemainingQuantity < 1000, возможно он сохранен в кг - конвертируем
			var currentStock float64 = batch.RemainingQuantity
			if baseUnit == "g" && batch.RemainingQuantity < 1000 && batch.RemainingQuantity > 0 {
				// Проверяем, возможно RemainingQuantity сохранен в килограммах вместо граммов
				// Если значение меньше 1000 и больше 0, вероятно это килограммы
				// Умножаем на 1000 для конвертации в граммы
				currentStock = batch.RemainingQuantity * 1000
				log.Printf("⚠️ GetStockItems: исправление единиц для %s (ID: %s): %.2f кг -> %.2f г",
					nomenclature.Name, batch.NomenclatureID, batch.RemainingQuantity, currentStock)
			} else if baseUnit == "ml" && batch.RemainingQuantity < 1000 && batch.RemainingQuantity > 0 {
				// Аналогично для миллилитров
				currentStock = batch.RemainingQuantity * 1000
				log.Printf("⚠️ GetStockItems: исправление единиц для %s (ID: %s): %.2f л -> %.2f мл",
					nomenclature.Name, batch.NomenclatureID, batch.RemainingQuantity, currentStock)
			}
			
			status := "in_stock"
			if currentStock <= 0 {
				status = "out_of_stock"
			} else if currentStock < minStock {
				status = "low_stock"
			}
			
			// Вычисляем cost_value используя правильную формулу
			// Формула: (Остаток в BaseUnit * Цена за InboundUnit) / ConversionFactor
			costValue := batchCostValueDecimal.InexactFloat64()
			
			stockMap[key] = map[string]interface{}{
				"id":                nomenclature.ID,
				"product_id":        nomenclature.ID,
				"product_name":     nomenclature.Name,
				"category":         nomenclature.CategoryName,
				"category_color":    nomenclature.CategoryColor,
				"category_id":       nomenclature.CategoryID,
				"unit":             nomenclature.InboundUnit, // Единица измерения для отображения (кг/л/шт) - используется для цены
				"base_unit":        nomenclature.BaseUnit, // Базовая единица склада (г/мл/шт) - для точного учета
				"inbound_unit":     nomenclature.InboundUnit, // Единица поступления (кг/л/шт) - для цены закупки
				"branch_id":        batch.BranchID,
				"branch_name":      branchMap[batch.BranchID], // Добавляем имя филиала
				"current_stock":    currentStock, // В BaseUnit
				"min_stock":        minStock,
				"cost_per_unit":    correctedCostPerUnit, // Цена за InboundUnit (кг/л/шт) - исправленная если нужно
				"cost_value":       costValue, // Стоимость = (currentStockInBaseUnit * CorrectedCostPerUnit) / ConversionFactor
				"status":           status,
				"batches": []map[string]interface{}{
					func() map[string]interface{} {
						// Исправляем cost_per_unit если он сохранен как цена за грамм
						var batchCorrectedCostPerUnit float64 = batch.CostPerUnit
						if batch.CostPerUnit > 0 && batch.CostPerUnit < 10 && (baseUnit == "g" && inboundUnit == "kg") {
							batchCorrectedCostPerUnit = batch.CostPerUnit * 1000
						} else if batch.CostPerUnit > 0 && batch.CostPerUnit < 10 && (baseUnit == "ml" && inboundUnit == "l") {
							batchCorrectedCostPerUnit = batch.CostPerUnit * 1000
						}
						
						batchData := map[string]interface{}{
							"id":                batch.ID,
							"quantity":          batch.RemainingQuantity,
							"expiry_at":         batch.ExpiryAt,
							"days_until_expiry": s.calculateDaysUntilExpiry(batch.ExpiryAt),
							"hours_until_expiry": s.calculateHoursUntilExpiry(batch.ExpiryAt),
							"is_expired":        batch.IsExpired,
							"is_at_risk":        s.isAtRisk(batch),
							"cost_per_unit":     batchCorrectedCostPerUnit, // Цена за InboundUnit (исправленная если нужно)
						}
						// Добавляем информацию о накладной, если есть
						if batch.InvoiceID != nil && *batch.InvoiceID != "" {
							batchData["invoice_id"] = *batch.InvoiceID
							if invoiceNumber, exists := invoiceMap[*batch.InvoiceID]; exists {
								batchData["invoice_number"] = invoiceNumber
							}
						}
						return batchData
					}(),
				},
			}
		}
	}
	
	// Преобразуем map в slice
	result := make([]map[string]interface{}, 0, len(stockMap))
	for _, item := range stockMap {
		result = append(result, item)
	}
	
	return result, nil
}

// GetBatchesHistory возвращает историю всех батчей для конкретной номенклатуры
// Включает все батчи (даже с нулевым остатком) для полной истории приходов
func (s *StockService) GetBatchesHistory(nomenclatureID string, branchID string) ([]map[string]interface{}, error) {
	if s.db == nil {
		return nil, fmt.Errorf("PostgreSQL недоступен")
	}
	
	query := s.db.Model(&models.StockBatch{}).
		Preload("Nomenclature").
		Preload("Invoice").
		Where("nomenclature_id = ?", nomenclatureID).
		Order("created_at DESC") // Сначала новые
	
	if branchID != "" && branchID != "all" {
		query = query.Where("branch_id = ?", branchID)
	}
	
	var batches []models.StockBatch
	if err := query.Find(&batches).Error; err != nil {
		return nil, fmt.Errorf("ошибка загрузки истории батчей: %w", err)
	}
	
	// Загружаем филиалы для получения имен
	branchMap := make(map[string]string)
	var branchIDs []string
	for _, batch := range batches {
		if _, exists := branchMap[batch.BranchID]; !exists {
			branchIDs = append(branchIDs, batch.BranchID)
		}
	}
	
	if len(branchIDs) > 0 {
		var branches []models.Branch
		if err := s.db.Where("id IN ?", branchIDs).Find(&branches).Error; err == nil {
			for _, branch := range branches {
				branchMap[branch.ID] = branch.Name
			}
		}
	}
	
	result := make([]map[string]interface{}, 0, len(batches))
	for _, batch := range batches {
		nomenclature := batch.Nomenclature
		
		// Вычисляем коэффициент конвертации
		conversionFactor := 1.0
		baseUnit := nomenclature.BaseUnit
		inboundUnit := nomenclature.InboundUnit
		
		if baseUnit != inboundUnit && inboundUnit != "" {
			if (baseUnit == "g" && inboundUnit == "kg") || (baseUnit == "ml" && inboundUnit == "l") {
				conversionFactor = 1000
			} else if (baseUnit == "kg" && inboundUnit == "g") || (baseUnit == "l" && inboundUnit == "ml") {
				conversionFactor = 0.001
			} else if nomenclature.ConversionFactor > 0 {
				conversionFactor = nomenclature.ConversionFactor
			}
		}
		
		// Количество в основной единице (для отображения)
		quantityInMajorUnit := batch.Quantity
		if conversionFactor > 1 {
			quantityInMajorUnit = batch.Quantity / conversionFactor
		}
		
		// Стоимость батча (используем правильную формулу)
		batchCostValueDecimal := calculateBatchValue(
			decimal.NewFromFloat(batch.RemainingQuantity), // Остаток в BaseUnit
			decimal.NewFromFloat(batch.CostPerUnit),      // Цена за InboundUnit
			decimal.NewFromFloat(conversionFactor),       // Коэффициент конвертации
		)
		
		batchData := map[string]interface{}{
			"id":                batch.ID,
			"batch_id_short":    batch.ID[len(batch.ID)-3:], // Последние 3 символа для отображения
			"quantity":          batch.Quantity,              // В BaseUnit
			"quantity_major":    quantityInMajorUnit,         // В InboundUnit (для отображения)
			"remaining_quantity": batch.RemainingQuantity,    // Остаток в BaseUnit
			"remaining_quantity_major": func() float64 {
				if conversionFactor > 1 {
					return batch.RemainingQuantity / conversionFactor
				}
				return batch.RemainingQuantity
			}(),
			"unit":              baseUnit,
			"major_unit":         inboundUnit,
			"cost_per_unit":      batch.CostPerUnit,          // Цена за InboundUnit
			"cost_value":         batchCostValueDecimal.InexactFloat64(),
			"expiry_at":          batch.ExpiryAt,
			"days_until_expiry": s.calculateDaysUntilExpiry(batch.ExpiryAt),
			"is_expired":         batch.IsExpired,
			"is_at_risk":         s.isAtRisk(batch),
			"source":             batch.Source,
			"created_at":         batch.CreatedAt,
			"branch_id":          batch.BranchID,
			"branch_name":        branchMap[batch.BranchID],
		}
		
		// Добавляем информацию о накладной, если есть
		if batch.InvoiceID != nil && *batch.InvoiceID != "" {
			batchData["invoice_id"] = *batch.InvoiceID
			if batch.Invoice != nil {
				batchData["invoice_number"] = batch.Invoice.Number
				batchData["invoice_date"] = batch.Invoice.InvoiceDate.Format("2006-01-02")
				batchData["invoice_status"] = string(batch.Invoice.Status)
				if batch.Invoice.Counterparty != nil {
					batchData["counterparty_name"] = batch.Invoice.Counterparty.Name
				}
			}
		}
		
		result = append(result, batchData)
	}
	
	return result, nil
}

// GetAtRiskInventory возвращает товары с риском истечения срока годности
func (s *StockService) GetAtRiskInventory(branchID string) ([]map[string]interface{}, error) {
	var batches []models.StockBatch
	
	query := s.db.Model(&models.StockBatch{}).
		Preload("Nomenclature").
		Where("remaining_quantity > 0").
		Where("expiry_at IS NOT NULL").
		Where("is_expired = false")
	
	if branchID != "" && branchID != "all" {
		query = query.Where("branch_id = ?", branchID)
	}
	
	if err := query.Find(&batches).Error; err != nil {
		return nil, err
	}
	
	atRiskItems := []map[string]interface{}{}
	
	for _, batch := range batches {
		if !s.isAtRisk(batch) {
			continue
		}
		
		hoursUntilExpiry := s.calculateHoursUntilExpiry(batch.ExpiryAt)
		daysUntilExpiry := s.calculateDaysUntilExpiry(batch.ExpiryAt)
		
		// Получаем скорость продаж (за последние 7 дней)
		salesVelocity := s.calculateSalesVelocity(batch.NomenclatureID, batch.BranchID)
		
		// Рассчитываем, успеем ли продать до истечения срока
		canSellBeforeExpiry := salesVelocity > 0 && (float64(batch.RemainingQuantity)/salesVelocity) < float64(daysUntilExpiry)
		
		atRiskItems = append(atRiskItems, map[string]interface{}{
			"batch_id":          batch.ID,
			"product_id":        batch.NomenclatureID,
			"product_name":      batch.Nomenclature.Name,
			"category":          batch.Nomenclature.CategoryName,
			"category_color":    batch.Nomenclature.CategoryColor,
			"quantity":          batch.RemainingQuantity,
			"unit":             batch.Nomenclature.BaseUnit,
			"expiry_at":        batch.ExpiryAt,
			"hours_until_expiry": hoursUntilExpiry,
			"days_until_expiry": daysUntilExpiry,
			"sales_velocity":   salesVelocity,
			"can_sell_before_expiry": canSellBeforeExpiry,
			"risk_level":       s.getRiskLevel(hoursUntilExpiry),
			"branch_id":        batch.BranchID,
		})
	}
	
	return atRiskItems, nil
}

// GetExpiryAlerts возвращает активные уведомления о сроке годности
func (s *StockService) GetExpiryAlerts(branchID string, alertType string) ([]models.ExpiryAlert, error) {
	var alerts []models.ExpiryAlert
	
	query := s.db.Model(&models.ExpiryAlert{}).
		Preload("Batch").
		Preload("Batch.Nomenclature").
		Where("is_resolved = false")
	
	if branchID != "" && branchID != "all" {
		query = query.Joins("JOIN stock_batches ON expiry_alerts.stock_batch_id = stock_batches.id").
			Where("stock_batches.branch_id = ?", branchID)
	}
	
	if alertType != "" {
		query = query.Where("alert_type = ?", alertType)
	}
	
	if err := query.Order("expires_at ASC").Find(&alerts).Error; err != nil {
		return nil, err
	}
	
	return alerts, nil
}

// processIngredientDepletion рекурсивно обрабатывает списание ингредиента (сырье или полуфабрикат)
func (s *StockService) processIngredientDepletion(ingredient models.RecipeIngredient, requiredQuantity float64, branchID string, performedBy string, saleID string, visitedRecipes map[string]bool) error {
	// Защита от циклических зависимостей
	if ingredient.IngredientRecipeID != nil {
		if visitedRecipes[*ingredient.IngredientRecipeID] {
			return fmt.Errorf("обнаружена циклическая зависимость в рецептах: %s", *ingredient.IngredientRecipeID)
		}
		visitedRecipes[*ingredient.IngredientRecipeID] = true
	}

	// Если ингредиент - это полуфабрикат (есть связанный рецепт)
	if ingredient.IngredientRecipeID != nil {
		// Загружаем рецепт полуфабриката
		var subRecipe models.Recipe
		if err := s.db.Preload("Ingredients").Preload("Ingredients.Nomenclature").Preload("Ingredients.IngredientRecipe").
			First(&subRecipe, "id = ?", *ingredient.IngredientRecipeID).Error; err != nil {
			return fmt.Errorf("рецепт полуфабриката не найден: %w", err)
		}

		// Рекурсивно списываем ингредиенты полуфабриката
		// requiredQuantity уже в граммах, нужно пересчитать на количество порций полуфабриката
		// Если в рецепте указано 500g теста, а нужно 1000g, то нужно 2 порции теста
		subRecipeQuantity := requiredQuantity / subRecipe.PortionSize // Количество порций полуфабриката

		for _, subIngredient := range subRecipe.Ingredients {
			subRequiredQuantity := subIngredient.Quantity * subRecipeQuantity
			if err := s.processIngredientDepletion(subIngredient, subRequiredQuantity, branchID, performedBy, saleID, visitedRecipes); err != nil {
				return err
			}
		}
		return nil
	}

	// Если ингредиент - это сырье (nomenclature_id)
	if ingredient.NomenclatureID == nil {
		return fmt.Errorf("ингредиент должен иметь либо nomenclature_id, либо ingredient_recipe_id")
	}

	// Находим партии с достаточным остатком (FIFO по сроку годности)
	var batches []models.StockBatch
	if err := s.db.Where("nomenclature_id = ? AND branch_id = ? AND remaining_quantity > 0 AND is_expired = false",
		*ingredient.NomenclatureID, branchID).
		Order("COALESCE(expiry_at, '9999-12-31') ASC"). // Сначала с ближайшим сроком годности
		Find(&batches).Error; err != nil {
		return err
	}

	remainingToDeduct := requiredQuantity

	for _, batch := range batches {
		if remainingToDeduct <= 0 {
			break
		}

		deductQuantity := remainingToDeduct
		if batch.RemainingQuantity < deductQuantity {
			deductQuantity = batch.RemainingQuantity
		}

		// Создаем движение остатков
		movement := models.StockMovement{
			StockBatchID:      &batch.ID,
			NomenclatureID:    *ingredient.NomenclatureID,
			BranchID:          branchID,
			Quantity:          -deductQuantity, // Отрицательное = расход (в граммах)
			Unit:              "g",              // Всегда граммы
			MovementType:      "sale",
			SourceReferenceID: &saleID,
			PerformedBy:       performedBy,
			Notes:             "Автоматическое списание при продаже",
		}

		if err := s.db.Create(&movement).Error; err != nil {
			return err
		}

		// Обновляем остаток партии
		// ВАЖНО: Обновляем ТОЛЬКО RemainingQuantity, CostPerUnit никогда не меняется (это константа закупки)
		batch.RemainingQuantity -= deductQuantity
		if err := s.db.Model(&batch).Update("remaining_quantity", batch.RemainingQuantity).Error; err != nil {
			return err
		}

		remainingToDeduct -= deductQuantity
	}

	if remainingToDeduct > 0 {
		var ingredientName string
		if ingredient.Nomenclature != nil {
			ingredientName = ingredient.Nomenclature.Name
		} else {
			ingredientName = "неизвестный ингредиент"
		}
		log.Printf("⚠️ Недостаточно остатков для ингредиента %s (требуется: %.2f g, недостает: %.2f g)",
			ingredientName, requiredQuantity, remainingToDeduct)
	}

	return nil
}

// ProcessSaleDepletion обрабатывает списание ингредиентов при продаже (с поддержкой рекурсивных рецептов)
func (s *StockService) ProcessSaleDepletion(recipeID string, quantity float64, branchID string, performedBy string, saleID string) error {
	// Получаем рецепт
	var recipe models.Recipe
	if err := s.db.Preload("Ingredients").Preload("Ingredients.Nomenclature").Preload("Ingredients.IngredientRecipe").
		First(&recipe, "id = ?", recipeID).Error; err != nil {
		return err
	}

	// Для каждого ингредиента списываем остатки (рекурсивно)
	visitedRecipes := make(map[string]bool)
	visitedRecipes[recipeID] = true // Помечаем текущий рецепт как посещенный

	for _, ingredient := range recipe.Ingredients {
		// requiredQuantity в граммах (quantity - количество порций готового продукта)
		requiredQuantity := ingredient.Quantity * quantity

		if err := s.processIngredientDepletion(ingredient, requiredQuantity, branchID, performedBy, saleID, visitedRecipes); err != nil {
			return err
		}
	}

	return nil
}

// CalculatePrimeCost рекурсивно рассчитывает себестоимость рецепта (в рублях)
// visitedRecipes может быть nil - функция создаст новый map
func (s *StockService) CalculatePrimeCost(recipeID string, visitedRecipes map[string]bool) (float64, error) {
	if visitedRecipes == nil {
		visitedRecipes = make(map[string]bool)
	}
	
	// Защита от циклических зависимостей
	if visitedRecipes[recipeID] {
		return 0, fmt.Errorf("обнаружена циклическая зависимость в рецептах: %s", recipeID)
	}
	visitedRecipes[recipeID] = true

	// Получаем рецепт
	var recipe models.Recipe
	if err := s.db.Preload("Ingredients").Preload("Ingredients.Nomenclature").Preload("Ingredients.IngredientRecipe").
		First(&recipe, "id = ?", recipeID).Error; err != nil {
		return 0, err
	}

	var totalCost float64 = 0

	// Для каждого ингредиента рассчитываем стоимость
	for _, ingredient := range recipe.Ingredients {
		var ingredientCost float64

		// Если ингредиент - это полуфабрикат (есть связанный рецепт)
		if ingredient.IngredientRecipeID != nil {
			// Создаем копию visitedRecipes для рекурсивного вызова
			subVisited := make(map[string]bool)
			for k, v := range visitedRecipes {
				subVisited[k] = v
			}
			
			// Рекурсивно рассчитываем себестоимость полуфабриката
			subRecipeCost, err := s.CalculatePrimeCost(*ingredient.IngredientRecipeID, subVisited)
			if err != nil {
				return 0, err
			}

			// Загружаем рецепт полуфабриката для получения PortionSize
			var subRecipe models.Recipe
			if err := s.db.First(&subRecipe, "id = ?", *ingredient.IngredientRecipeID).Error; err != nil {
				return 0, err
			}

			// Стоимость ингредиента = (себестоимость полуфабриката / размер порции) * количество в граммах
			// Если себестоимость теста 100₽ за 1кг (1000g), а нужно 500g, то стоимость = 100₽ / 1000g * 500g = 50₽
			if subRecipe.PortionSize > 0 {
				ingredientCost = (subRecipeCost / subRecipe.PortionSize) * ingredient.Quantity
			} else {
				ingredientCost = 0
			}
		} else if ingredient.NomenclatureID != nil {
			// Если ингредиент - это сырье, берем цену из номенклатуры
			var nomenclature models.NomenclatureItem
			if err := s.db.First(&nomenclature, "id = ?", *ingredient.NomenclatureID).Error; err != nil {
				return 0, fmt.Errorf("номенклатура не найдена: %w", err)
			}

			// ВАЖНО: Используем правильную формулу расчета стоимости с shopspring/decimal для точности
			// LastPrice хранится за InboundUnit (кг/л/шт) - это нормализованная цена за единицу
			// ingredient.Quantity в BaseUnit (г/мл/шт)
			// Формула: TotalCost = (QuantityInGrams / 1000) * CostPerUnit(за кг)
			// Пример: (5500г / 1000) * 122.1₽/кг = 5.5 * 122.1 = 671.55₽
			conversionFactor := decimal.NewFromFloat(1.0)
			if nomenclature.BaseUnit == "g" && nomenclature.InboundUnit == "kg" {
				conversionFactor = decimal.NewFromInt(1000)
			} else if nomenclature.BaseUnit == "ml" && nomenclature.InboundUnit == "l" {
				conversionFactor = decimal.NewFromInt(1000)
			} else if nomenclature.ConversionFactor > 0 {
				conversionFactor = decimal.NewFromFloat(nomenclature.ConversionFactor)
			}
			
			// Используем calculateBatchValue для точного расчета стоимости
			quantityDecimal := decimal.NewFromFloat(ingredient.Quantity)
			priceDecimal := decimal.NewFromFloat(nomenclature.LastPrice)
			ingredientCostDecimal := calculateBatchValue(quantityDecimal, priceDecimal, conversionFactor)
			ingredientCost = ingredientCostDecimal.InexactFloat64()
		} else {
			return 0, fmt.Errorf("ингредиент должен иметь либо nomenclature_id, либо ingredient_recipe_id")
		}

		totalCost += ingredientCost
	}

	return totalCost, nil
}

// CommitProduction обрабатывает ручное производство полуфабриката
// quantity - количество производимого полуфабриката в граммах
func (s *StockService) CommitProduction(recipeID string, quantity float64, branchID string, performedBy string, productionOrderID string) error {
	// Начинаем транзакцию
	tx := s.db.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// Получаем рецепт
	var recipe models.Recipe
	if err := tx.Preload("Ingredients").Preload("Ingredients.Nomenclature").Preload("Ingredients.IngredientRecipe").
		First(&recipe, "id = ?", recipeID).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("рецепт не найден: %w", err)
	}

	// Рассчитываем себестоимость
	visitedRecipes := make(map[string]bool)
	primeCost, err := s.CalculatePrimeCost(recipeID, visitedRecipes)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("ошибка расчета себестоимости: %w", err)
	}

	// Рассчитываем стоимость за грамм
	costPerGram := primeCost / recipe.PortionSize // Если себестоимость за PortionSize грамм
	totalCost := costPerGram * quantity

	// Списываем ингредиенты (используем рекурсивную логику)
	visitedRecipes = make(map[string]bool)
	visitedRecipes[recipeID] = true

	// Количество порций для списания
	portionsToProduce := quantity / recipe.PortionSize

	for _, ingredient := range recipe.Ingredients {
		requiredQuantity := ingredient.Quantity * portionsToProduce

		if err := s.processIngredientDepletionInTx(tx, ingredient, requiredQuantity, branchID, performedBy, productionOrderID, visitedRecipes); err != nil {
			tx.Rollback()
			return err
		}
	}

	// ПРИМЕЧАНИЕ: Для создания партии готового полуфабриката нужен NomenclatureID
	// В текущей схеме Recipe связан с MenuItemID, но не с NomenclatureID напрямую
	// Для полноценной работы нужно либо:
	// 1. Добавить NomenclatureID в Recipe
	// 2. Или создать связь через MenuItem -> NomenclatureItem
	// 
	// Пока что логируем успешное производство, но не создаем StockBatch
	// Это можно доработать позже, когда будет ясна схема связи рецептов с номенклатурой
	// 
	// ВАЖНО: Ингредиенты уже списаны через processIngredientDepletionInTx

	// Коммитим транзакцию
	if err := tx.Commit().Error; err != nil {
		return err
	}

	log.Printf("✅ Производство завершено: %s, количество: %.2f г, себестоимость: %.2f ₽", recipe.Name, quantity, totalCost)
	return nil
}

// processIngredientDepletionInTx - версия processIngredientDepletion для работы внутри транзакции
func (s *StockService) processIngredientDepletionInTx(tx *gorm.DB, ingredient models.RecipeIngredient, requiredQuantity float64, branchID string, performedBy string, sourceID string, visitedRecipes map[string]bool) error {
	// Защита от циклических зависимостей
	if ingredient.IngredientRecipeID != nil {
		if visitedRecipes[*ingredient.IngredientRecipeID] {
			return fmt.Errorf("обнаружена циклическая зависимость в рецептах: %s", *ingredient.IngredientRecipeID)
		}
		visitedRecipes[*ingredient.IngredientRecipeID] = true
	}

	// Если ингредиент - это полуфабрикат
	if ingredient.IngredientRecipeID != nil {
		var subRecipe models.Recipe
		if err := tx.Preload("Ingredients").Preload("Ingredients.Nomenclature").Preload("Ingredients.IngredientRecipe").
			First(&subRecipe, "id = ?", *ingredient.IngredientRecipeID).Error; err != nil {
			return fmt.Errorf("рецепт полуфабриката не найден: %w", err)
		}

		subRecipeQuantity := requiredQuantity / subRecipe.PortionSize

		for _, subIngredient := range subRecipe.Ingredients {
			subRequiredQuantity := subIngredient.Quantity * subRecipeQuantity
			if err := s.processIngredientDepletionInTx(tx, subIngredient, subRequiredQuantity, branchID, performedBy, sourceID, visitedRecipes); err != nil {
				return err
			}
		}
		return nil
	}

	// Если ингредиент - это сырье
	if ingredient.NomenclatureID == nil {
		return fmt.Errorf("ингредиент должен иметь либо nomenclature_id, либо ingredient_recipe_id")
	}

	var batches []models.StockBatch
	if err := tx.Where("nomenclature_id = ? AND branch_id = ? AND remaining_quantity > 0 AND is_expired = false",
		*ingredient.NomenclatureID, branchID).
		Order("COALESCE(expiry_at, '9999-12-31') ASC").
		Find(&batches).Error; err != nil {
		return err
	}

	remainingToDeduct := requiredQuantity

	for _, batch := range batches {
		if remainingToDeduct <= 0 {
			break
		}

		deductQuantity := remainingToDeduct
		if batch.RemainingQuantity < deductQuantity {
			deductQuantity = batch.RemainingQuantity
		}

		movement := models.StockMovement{
			StockBatchID:      &batch.ID,
			NomenclatureID:    *ingredient.NomenclatureID,
			BranchID:          branchID,
			Quantity:          -deductQuantity,
			Unit:              "g",
			MovementType:      "production",
			SourceReferenceID: &sourceID,
			PerformedBy:       performedBy,
			Notes:             "Списание при производстве",
		}

		if err := tx.Create(&movement).Error; err != nil {
			return err
		}

		// ВАЖНО: Обновляем ТОЛЬКО RemainingQuantity, CostPerUnit никогда не меняется (это константа закупки)
		batch.RemainingQuantity -= deductQuantity
		if err := tx.Model(&batch).Update("remaining_quantity", batch.RemainingQuantity).Error; err != nil {
			return err
		}

		remainingToDeduct -= deductQuantity
	}

	if remainingToDeduct > 0 {
		var ingredientName string
		if ingredient.Nomenclature != nil {
			ingredientName = ingredient.Nomenclature.Name
		} else {
			ingredientName = "неизвестный ингредиент"
		}
		return fmt.Errorf("недостаточно остатков для ингредиента %s (требуется: %.2f г, недостает: %.2f г)",
			ingredientName, requiredQuantity, remainingToDeduct)
	}

	return nil
}

// DebitIngredients списывает ингредиенты по рецепту с использованием FEFO (First Expired, First Out)
// и пессимистической блокировки для обеспечения атомарности и предотвращения race conditions
// 
// УПРОЩЕННАЯ ЛОГИКА: НЕ взрывает полуфабрикаты автоматически.
// Полуфабрикаты проверяются как обычные товары на складе.
// Если полуфабриката нет на складе - возвращается ошибка "Shortage Error".
// Производство полуфабрикатов должно выполняться отдельно через Production service.
//
// performedBy - имя пользователя или ID для аудита (опционально, по умолчанию "system")
func (s *StockService) DebitIngredients(recipeID string, branchID string, quantity float64, performedBy ...string) error {
	performedByUser := "system"
	if len(performedBy) > 0 && performedBy[0] != "" {
		performedByUser = performedBy[0]
	}
	
	// Начинаем транзакцию
	tx := s.db.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// Загружаем рецепт с ингредиентами
	var recipe models.Recipe
	if err := tx.Preload("Ingredients").Preload("Ingredients.Nomenclature").
		Preload("Ingredients.IngredientRecipe").
		First(&recipe, "id = ?", recipeID).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("рецепт не найден: %w", err)
	}

	// Вычисляем количество порций для списания
	if recipe.PortionSize <= 0 {
		tx.Rollback()
		return fmt.Errorf("неверный размер порции рецепта: %.2f", recipe.PortionSize)
	}
	portionsToProduce := quantity / recipe.PortionSize

	// Собираем список недостающих товаров для детального сообщения об ошибке
	var missingItems []string

	// Обрабатываем каждый ингредиент (БЕЗ рекурсии)
	for _, ingredient := range recipe.Ingredients {
		// Вычисляем требуемое количество ингредиента
		requiredQuantity := ingredient.Quantity * portionsToProduce

		// Если ингредиент - это полуфабрикат (вложенный рецепт)
		if ingredient.IngredientRecipeID != nil {
			// Загружаем рецепт полуфабриката
			var subRecipe models.Recipe
			if err := tx.First(&subRecipe, "id = ?", *ingredient.IngredientRecipeID).Error; err != nil {
				tx.Rollback()
				return fmt.Errorf("рецепт полуфабриката не найден: %w", err)
			}

			// Ищем NomenclatureItem для полуфабриката по имени рецепта
			// Полуфабрикат должен быть создан как NomenclatureItem для отслеживания на складе
			var semiFinishedNomenclature models.NomenclatureItem
			if err := tx.Where("name = ? AND is_active = true AND deleted_at IS NULL", subRecipe.Name).
				First(&semiFinishedNomenclature).Error; err != nil {
				tx.Rollback()
				return fmt.Errorf("полуфабрикат '%s' не найден в номенклатуре. Полуфабрикат должен быть создан как товар в номенклатуре для отслеживания на складе", subRecipe.Name)
			}

			// Проверяем наличие полуфабриката на складе как обычного товара
			// Конвертируем требуемое количество в единицы хранения
			requiredInBaseUnit, err := s.convertToBaseUnit(requiredQuantity, ingredient.Unit, semiFinishedNomenclature)
			if err != nil {
				tx.Rollback()
				return fmt.Errorf("ошибка конвертации единиц для полуфабриката '%s': %w", subRecipe.Name, err)
			}

			// Проверяем наличие на складе
			var totalStock float64
			if err := tx.Model(&models.StockBatch{}).
				Where("nomenclature_id = ? AND branch_id = ? AND remaining_quantity > 0 AND is_expired = false AND deleted_at IS NULL",
					semiFinishedNomenclature.ID, branchID).
				Select("COALESCE(SUM(remaining_quantity), 0)").
				Scan(&totalStock).Error; err != nil {
				tx.Rollback()
				return fmt.Errorf("ошибка проверки остатков полуфабриката '%s': %w", subRecipe.Name, err)
			}

			if totalStock < requiredInBaseUnit {
				missingItems = append(missingItems, 
					fmt.Sprintf("Полуфабрикат '%s': требуется %.4f %s, доступно %.4f %s",
						subRecipe.Name, requiredInBaseUnit, semiFinishedNomenclature.BaseUnit, totalStock, semiFinishedNomenclature.BaseUnit))
				continue // Продолжаем проверку остальных ингредиентов для полного списка недостающих
			}

			// Списываем полуфабрикат со склада (FEFO)
			if err := s.debitNomenclatureFromStock(tx, semiFinishedNomenclature.ID, requiredInBaseUnit, branchID, recipeID, performedByUser, semiFinishedNomenclature); err != nil {
				tx.Rollback()
				return fmt.Errorf("ошибка списания полуфабриката '%s': %w", subRecipe.Name, err)
			}

			log.Printf("📦 Списан полуфабрикат '%s': %.4f %s", subRecipe.Name, requiredInBaseUnit, semiFinishedNomenclature.BaseUnit)
			continue
		}

		// Если ингредиент - это сырье (nomenclature_id)
		if ingredient.NomenclatureID == nil {
			tx.Rollback()
			return fmt.Errorf("ингредиент должен иметь либо nomenclature_id, либо ingredient_recipe_id")
		}

		// Загружаем номенклатуру для получения единиц измерения
		var nomenclature models.NomenclatureItem
		if err := tx.First(&nomenclature, "id = ?", *ingredient.NomenclatureID).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("номенклатура не найдена: %w", err)
		}

		// Конвертируем требуемое количество в единицы хранения (base_unit)
		requiredInBaseUnit, err := s.convertToBaseUnit(requiredQuantity, ingredient.Unit, nomenclature)
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("ошибка конвертации единиц для %s: %w", nomenclature.Name, err)
		}

		// Проверяем наличие на складе
		var totalStock float64
		if err := tx.Model(&models.StockBatch{}).
			Where("nomenclature_id = ? AND branch_id = ? AND remaining_quantity > 0 AND is_expired = false AND deleted_at IS NULL",
				*ingredient.NomenclatureID, branchID).
			Select("COALESCE(SUM(remaining_quantity), 0)").
			Scan(&totalStock).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("ошибка проверки остатков: %w", err)
		}

		if totalStock < requiredInBaseUnit {
			missingItems = append(missingItems,
				fmt.Sprintf("'%s': требуется %.4f %s, доступно %.4f %s",
					nomenclature.Name, requiredInBaseUnit, nomenclature.BaseUnit, totalStock, nomenclature.BaseUnit))
			continue // Продолжаем проверку остальных ингредиентов
		}

		// Списываем сырье со склада (FEFO)
		if err := s.debitNomenclatureFromStock(tx, *ingredient.NomenclatureID, requiredInBaseUnit, branchID, recipeID, performedByUser, nomenclature); err != nil {
			tx.Rollback()
			return fmt.Errorf("ошибка списания '%s': %w", nomenclature.Name, err)
		}

		log.Printf("📦 Списано сырье '%s': %.4f %s", nomenclature.Name, requiredInBaseUnit, nomenclature.BaseUnit)
	}

	// Если есть недостающие товары, возвращаем ошибку с полным списком
	if len(missingItems) > 0 {
		tx.Rollback()
		errorMsg := "❌ Недостаточно остатков на складе:\n"
		for i, item := range missingItems {
			errorMsg += fmt.Sprintf("  %d. %s\n", i+1, item)
		}
		errorMsg += "\nПроизводство полуфабрикатов должно быть выполнено отдельно через Production service."
		return fmt.Errorf(errorMsg)
	}

	// Коммитим транзакцию
	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("ошибка коммита транзакции: %w", err)
	}

	log.Printf("✅ Списаны ингредиенты для рецепта %s (количество: %.2f %s)", recipe.Name, quantity, recipe.Unit)
	return nil
}

// debitNomenclatureFromStock списывает номенклатуру со склада по FEFO принципу
// Это вспомогательный метод для упрощения кода DebitIngredients
func (s *StockService) debitNomenclatureFromStock(tx *gorm.DB, nomenclatureID string, requiredQuantity float64, branchID string, sourceRecipeID string, performedBy string, nomenclature models.NomenclatureItem) error {
	// Получаем доступные партии с FEFO сортировкой и пессимистической блокировкой
	var batches []models.StockBatch
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("nomenclature_id = ? AND branch_id = ? AND remaining_quantity > 0 AND is_expired = false AND deleted_at IS NULL",
			nomenclatureID, branchID).
		Order("COALESCE(expiry_at, '9999-12-31'::timestamp) ASC, created_at ASC").
		Find(&batches).Error; err != nil {
		return fmt.Errorf("ошибка получения партий: %w", err)
	}

	// Списываем по FEFO принципу (частичное списание по батчам)
	remainingToDeduct := requiredQuantity

	for i := range batches {
		if remainingToDeduct <= 0 {
			break
		}

		batch := &batches[i]
		deductQuantity := remainingToDeduct
		if batch.RemainingQuantity < deductQuantity {
			deductQuantity = batch.RemainingQuantity
		}

		// Создаем запись движения для аудита
		movement := models.StockMovement{
			StockBatchID:      &batch.ID,
			NomenclatureID:    nomenclatureID,
			BranchID:          branchID,
			Quantity:          -deductQuantity, // Отрицательное значение = расход
			Unit:              nomenclature.BaseUnit,
			MovementType:      "production",
			SourceReferenceID: &sourceRecipeID,
			PerformedBy:       performedBy,
			Notes:             fmt.Sprintf("Списание при производстве (рецепт: %s)", sourceRecipeID),
		}

		if err := tx.Create(&movement).Error; err != nil {
			return fmt.Errorf("ошибка создания записи движения: %w", err)
		}

		// Обновляем остаток партии
		// ВАЖНО: Обновляем ТОЛЬКО RemainingQuantity, CostPerUnit никогда не меняется (это константа закупки)
		batch.RemainingQuantity -= deductQuantity
		if err := tx.Model(batch).Update("remaining_quantity", batch.RemainingQuantity).Error; err != nil {
			return fmt.Errorf("ошибка обновления партии: %w", err)
		}

		remainingToDeduct -= deductQuantity

		log.Printf("📦 Списано %.4f %s из партии %s (остаток: %.4f %s)",
			deductQuantity, nomenclature.BaseUnit, batch.ID, batch.RemainingQuantity, nomenclature.BaseUnit)
	}

	return nil
}

// debitIngredientRecursive УДАЛЕН - больше не используется
// Логика упрощена: полуфабрикаты проверяются как обычные товары на складе
// Рекурсивное "взрывание" полуфабрикатов больше не выполняется

// convertToBaseUnit конвертирует количество из единицы ингредиента в базовую единицу номенклатуры
// 
// ВАЖНО: Использует float64, что может привести к погрешностям округления при больших объемах.
func (s *StockService) convertToBaseUnit(quantity float64, fromUnit string, nomenclature models.NomenclatureItem) (float64, error) {
	// Если единицы совпадают, возвращаем как есть
	if fromUnit == nomenclature.BaseUnit {
		return quantity, nil
	}

	// Конвертация граммы <-> килограммы (точная конвертация: 1 кг = 1000 г)
	if fromUnit == "g" && nomenclature.BaseUnit == "kg" {
		return quantity / 1000.0, nil
	}
	if fromUnit == "kg" && nomenclature.BaseUnit == "g" {
		return quantity * 1000.0, nil
	}

	// Конвертация миллилитры <-> литры (точная конвертация: 1 л = 1000 мл)
	if fromUnit == "ml" && nomenclature.BaseUnit == "l" {
		return quantity / 1000.0, nil
	}
	if fromUnit == "l" && nomenclature.BaseUnit == "ml" {
		return quantity * 1000.0, nil
	}

	// Конвертация штуки -> граммы/килограммы (требуется unit_weight)
	if fromUnit == "pcs" {
		if nomenclature.UnitWeight <= 0 {
			return 0, fmt.Errorf("для конвертации штук (pcs) в %s требуется указать unit_weight в номенклатуре товара '%s'", nomenclature.BaseUnit, nomenclature.Name)
		}
		
		// Конвертируем штуки в граммы
		grams := quantity * nomenclature.UnitWeight
		
		// Если базовая единица - килограммы, конвертируем граммы в кг
		if nomenclature.BaseUnit == "kg" {
			return grams / 1000.0, nil
		}
		if nomenclature.BaseUnit == "g" {
			return grams, nil
		}
		
		// Если базовая единица не граммы/килограммы, возвращаем ошибку
		return 0, fmt.Errorf("конвертация штук (pcs) в %s не поддерживается (требуется g или kg)", nomenclature.BaseUnit)
	}

	// Используем коэффициент конвертации из номенклатуры
	if nomenclature.ConversionFactor > 0 {
		// Если production_unit отличается от base_unit, используем conversion_factor
		if fromUnit == nomenclature.ProductionUnit && nomenclature.BaseUnit != nomenclature.ProductionUnit {
			return quantity / nomenclature.ConversionFactor, nil
		}
		// Если inbound_unit отличается от base_unit
		if fromUnit == nomenclature.InboundUnit && nomenclature.BaseUnit != nomenclature.InboundUnit {
			return quantity / nomenclature.ConversionFactor, nil
		}
	}

	// Если единицы не совпадают и нет способа конвертации
	return 0, fmt.Errorf("не удалось конвертировать из %s в %s для товара '%s' (нет коэффициента конвертации или unit_weight)", 
		fromUnit, nomenclature.BaseUnit, nomenclature.Name)
}

// GetStockMovements возвращает список движений склада с фильтрацией
// branchID - фильтр по филиалу (пустая строка или "all" = все филиалы)
// movementType - фильтр по типу движения (sale, production, adjustment, waste, invoice, пустая строка = все)
// dateFrom - начальная дата (опционально)
// dateTo - конечная дата (опционально)
// searchQuery - поиск по названию товара (опционально)
// limit - максимальное количество записей (по умолчанию 1000)
func (s *StockService) GetStockMovements(branchID, movementType, dateFrom, dateTo, searchQuery string, limit int) ([]models.StockMovement, error) {
	if limit <= 0 || limit > 10000 {
		limit = 1000 // Защита от слишком больших запросов
	}

	query := s.db.Model(&models.StockMovement{}).
		Preload("Nomenclature").
		Preload("Batch").
		Order("created_at DESC")

	// Фильтр по филиалу
	if branchID != "" && branchID != "all" {
		query = query.Where("branch_id = ?", branchID)
	}

	// Фильтр по типу движения
	if movementType != "" {
		query = query.Where("movement_type = ?", movementType)
	}

	// Фильтр по дате (от)
	if dateFrom != "" {
		if dateFromTime, err := time.Parse("2006-01-02", dateFrom); err == nil {
			query = query.Where("created_at >= ?", dateFromTime)
		}
	}

	// Фильтр по дате (до)
	if dateTo != "" {
		if dateToTime, err := time.Parse("2006-01-02", dateTo); err == nil {
			// Добавляем 23:59:59 к конечной дате, чтобы включить весь день
			dateToTime = dateToTime.Add(23*time.Hour + 59*time.Minute + 59*time.Second)
			query = query.Where("created_at <= ?", dateToTime)
		}
	}

	// Поиск по названию товара (через JOIN с номенклатурой)
	if searchQuery != "" {
		query = query.Joins("JOIN nomenclature_items ON stock_movements.nomenclature_id = nomenclature_items.id").
			Where("nomenclature_items.name ILIKE ?", "%"+searchQuery+"%")
	}

	// Ограничение количества записей
	query = query.Limit(limit)

	var movements []models.StockMovement
	if err := query.Find(&movements).Error; err != nil {
		return nil, fmt.Errorf("ошибка получения движений: %w", err)
	}

	return movements, nil
}

// CheckAndCreateExpiryAlerts проверяет сроки годности и создает уведомления
func (s *StockService) CheckAndCreateExpiryAlerts() error {
	now := time.Now()
	warningThreshold := now.Add(3 * time.Hour) // За 3 часа до истечения
	
	// Находим партии, которые истекают в ближайшие 3 часа
	var warningBatches []models.StockBatch
	if err := s.db.Where("expiry_at IS NOT NULL").
		Where("expiry_at <= ?", warningThreshold).
		Where("expiry_at > ?", now).
		Where("remaining_quantity > 0").
		Where("is_expired = false").
		Find(&warningBatches).Error; err != nil {
		return err
	}
	
	// Создаем предупреждения
	for _, batch := range warningBatches {
		// Проверяем, нет ли уже активного предупреждения
		var existingAlert models.ExpiryAlert
		if err := s.db.Where("stock_batch_id = ? AND alert_type = 'warning' AND is_resolved = false", batch.ID).
			First(&existingAlert).Error; err == nil {
			continue // Предупреждение уже существует
		}
		
		alert := models.ExpiryAlert{
			StockBatchID: batch.ID,
			AlertType:    "warning",
			ExpiresAt:    *batch.ExpiryAt,
		}
		
		if err := s.db.Create(&alert).Error; err != nil {
			log.Printf("❌ Ошибка создания предупреждения для партии %s: %v", batch.ID, err)
		}
	}
	
	// Находим просроченные партии
	var expiredBatches []models.StockBatch
	if err := s.db.Where("expiry_at IS NOT NULL").
		Where("expiry_at <= ?", now).
		Where("remaining_quantity > 0").
		Where("is_expired = false").
		Find(&expiredBatches).Error; err != nil {
		return err
	}
	
	// Помечаем как просроченные и создаем критические уведомления
	for _, batch := range expiredBatches {
		batch.IsExpired = true
		if err := s.db.Save(&batch).Error; err != nil {
			log.Printf("❌ Ошибка обновления просроченной партии %s: %v", batch.ID, err)
			continue
		}
		
		// Создаем критическое уведомление
		var existingAlert models.ExpiryAlert
		if err := s.db.Where("stock_batch_id = ? AND alert_type = 'critical' AND is_resolved = false", batch.ID).
			First(&existingAlert).Error; err == nil {
			continue // Уведомление уже существует
		}
		
		alert := models.ExpiryAlert{
			StockBatchID: batch.ID,
			AlertType:    "critical",
			ExpiresAt:    *batch.ExpiryAt,
		}
		
		if err := s.db.Create(&alert).Error; err != nil {
			log.Printf("❌ Ошибка создания критического уведомления для партии %s: %v", batch.ID, err)
		}
	}
	
	return nil
}

// Helper functions

func (s *StockService) calculateDaysUntilExpiry(expiryAt *time.Time) int {
	if expiryAt == nil {
		return 9999 // Нет срока годности
	}
	
	now := time.Now()
	diff := expiryAt.Sub(now)
	return int(diff.Hours() / 24)
}

func (s *StockService) calculateHoursUntilExpiry(expiryAt *time.Time) float64 {
	if expiryAt == nil {
		return 999999 // Нет срока годности
	}
	
	now := time.Now()
	diff := expiryAt.Sub(now)
	return diff.Hours()
}

func (s *StockService) isAtRisk(batch models.StockBatch) bool {
	if batch.ExpiryAt == nil {
		return false
	}
	
	hoursUntilExpiry := s.calculateHoursUntilExpiry(batch.ExpiryAt)
	
	// Риск, если до истечения менее 24 часов
	return hoursUntilExpiry > 0 && hoursUntilExpiry < 24
}

func (s *StockService) getRiskLevel(hoursUntilExpiry float64) string {
	if hoursUntilExpiry <= 0 {
		return "critical" // Просрочено
	}
	if hoursUntilExpiry <= 3 {
		return "critical" // Менее 3 часов
	}
	if hoursUntilExpiry <= 24 {
		return "warning" // Менее 24 часов
	}
	return "safe"
}

func (s *StockService) calculateSalesVelocity(nomenclatureID string, branchID string) float64 {
	// Подсчитываем количество проданных единиц за последние 7 дней
	var totalQuantity float64
	
	sevenDaysAgo := time.Now().AddDate(0, 0, -7)
	
	if err := s.db.Model(&models.StockMovement{}).
		Where("nomenclature_id = ?", nomenclatureID).
		Where("branch_id = ?", branchID).
		Where("movement_type = 'sale'").
		Where("quantity < 0"). // Отрицательное = расход
		Where("created_at >= ?", sevenDaysAgo).
		Select("COALESCE(ABS(SUM(quantity)), 0)").
		Scan(&totalQuantity).Error; err != nil {
		log.Printf("⚠️ Ошибка расчета скорости продаж: %v", err)
		return 0
	}
	
	// Возвращаем среднее количество в день
	return totalQuantity / 7.0
}

// ProcessInboundInvoice обрабатывает входящую накладную и создает партии товаров
// Использует оптимизированную батч-вставку для больших объемов данных
// invoiceID: идентификатор накладной (опционально, для связи с финансовым модулем)
// items: массив товаров с полями: nomenclature_id, quantity, unit, price_per_unit, expiry_date, branch_id, conversion_factor
// performedBy: пользователь, который обработал накладную
// counterpartyID: идентификатор контрагента (опционально)
// totalAmount: общая сумма накладной
// isPaidCash: true если оплачено наличными (внутренний баланс), false если банком (официальный баланс)
// invoiceDate: дата накладной (опционально, формат: 2006-01-02)
func (s *StockService) ProcessInboundInvoice(invoiceID string, items []map[string]interface{}, performedBy string, counterpartyID string, totalAmount float64, isPaidCash bool, invoiceDate string) error {
	// ВАЖНО: Используем оптимизированную батч-версию для обработки
	// ProcessInboundInvoiceBatch правильно нормализует цены (делит на pack_size если указан)
	// и сохраняет CostPerUnit как цену за 1кг/1л, НЕ за грамм
	return s.ProcessInboundInvoiceBatch(invoiceID, items, performedBy, counterpartyID, totalAmount, isPaidCash, invoiceDate)
	// Начинаем транзакцию
	tx := s.db.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()
	
	for _, itemData := range items {
		// Извлекаем данные товара
		nomenclatureID, ok := itemData["nomenclature_id"].(string)
		if !ok {
			log.Printf("⚠️ Пропущен товар: отсутствует nomenclature_id")
			continue
		}
		
		quantity, ok := itemData["quantity"].(float64)
		if !ok {
			log.Printf("⚠️ Пропущен товар %s: неверное количество", nomenclatureID)
			continue
		}
		
		unit, ok := itemData["unit"].(string)
		if !ok {
			unit = "kg" // Значение по умолчанию
		}
		
		pricePerUnit, ok := itemData["price_per_unit"].(float64)
		if !ok {
			pricePerUnit = 0
		}
		
		branchID, ok := itemData["branch_id"].(string)
		if !ok {
			log.Printf("⚠️ Пропущен товар %s: отсутствует branch_id", nomenclatureID)
			continue
		}
		
		// Обрабатываем expiry_date (может быть строкой или null)
		var expiryAt *time.Time
		if expiryDate, exists := itemData["expiry_date"]; exists && expiryDate != nil {
			if expiryStr, ok := expiryDate.(string); ok && expiryStr != "" {
				if parsedTime, err := time.Parse("2006-01-02", expiryStr); err == nil {
					expiryAt = &parsedTime
				}
			}
		}
		
		// Создаем StockBatch
		// SourceReferenceID используем только если invoiceID является валидным UUID
		var sourceRefID *string
		if invoiceID != "" {
			// Проверяем, является ли invoiceID валидным UUID
			if len(invoiceID) == 36 && (invoiceID[8] == '-' && invoiceID[13] == '-' && invoiceID[18] == '-' && invoiceID[23] == '-') {
				sourceRefID = &invoiceID
			} else {
				// Если это не UUID (например, timestamp), не используем его как SourceReferenceID
				log.Printf("⚠️ invoiceID '%s' не является UUID, SourceReferenceID не будет установлен", invoiceID)
				sourceRefID = nil
			}
		}
		
		batch := models.StockBatch{
			NomenclatureID:    nomenclatureID,
			BranchID:          branchID,
			Quantity:          quantity,
			Unit:              unit,
			CostPerUnit:       pricePerUnit,
			ExpiryAt:          expiryAt,
			Source:            "invoice",
			SourceReferenceID: sourceRefID,
			RemainingQuantity: quantity,
		}
		
		if err := tx.Create(&batch).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("ошибка создания партии для товара %s: %v", nomenclatureID, err)
		}
		
		// Создаем StockMovement
		// SourceReferenceID используем только если invoiceID является валидным UUID
		var movementSourceRefID *string
		if invoiceID != "" {
			// Проверяем, является ли invoiceID валидным UUID
			if len(invoiceID) == 36 && (invoiceID[8] == '-' && invoiceID[13] == '-' && invoiceID[18] == '-' && invoiceID[23] == '-') {
				movementSourceRefID = &invoiceID
			} else {
				movementSourceRefID = nil
			}
		}
		
		movement := models.StockMovement{
			StockBatchID:      &batch.ID,
			NomenclatureID:    nomenclatureID,
			BranchID:          branchID,
			Quantity:          quantity, // Положительное = приход
			Unit:              unit,
			MovementType:      "invoice",
			SourceReferenceID: movementSourceRefID,
			PerformedBy:       performedBy,
			Notes:             fmt.Sprintf("Оприходование по накладной %s", invoiceID),
		}
		
		if err := tx.Create(&movement).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("ошибка создания движения для товара %s: %v", nomenclatureID, err)
		}
		
		// Обновляем last_price в NomenclatureItem
		// ВАЖНО: pricePerUnit всегда указывается за Major Unit (кг/л/шт), а не за грамм/миллилитр
		// Например: если товар хранится в граммах (base_unit = 'g'), но цена указывается за килограмм (inbound_unit = 'kg'),
		// то pricePerUnit = 100 означает 100 руб за 1 кг, а не за 1 грамм
		if pricePerUnit > 0 {
			if err := tx.Model(&models.NomenclatureItem{}).
				Where("id = ?", nomenclatureID).
				Update("last_price", pricePerUnit).Error; err != nil {
				log.Printf("⚠️ Ошибка обновления last_price для товара %s: %v", nomenclatureID, err)
				// Не прерываем транзакцию, это не критично
			} else {
				log.Printf("✅ Обновлена цена для товара %s: %.2f (за Major Unit)", nomenclatureID, pricePerUnit)
			}
		}
	}
	
	// Коммитим транзакцию
	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("ошибка коммита транзакции: %v", err)
	}
	
	// Обновляем баланс контрагента и создаем финансовые записи (после коммита основной транзакции)
	if counterpartyID != "" && totalAmount > 0 {
		// Обновляем баланс контрагента
		if s.counterpartyService != nil {
			// Если не оплачено наличными, увеличиваем долг (положительная сумма = долг)
			if !isPaidCash {
				if err := s.counterpartyService.UpdateCounterpartyBalance(counterpartyID, totalAmount, true); err != nil {
					log.Printf("⚠️ Ошибка обновления баланса контрагента: %v", err)
					// Не возвращаем ошибку, это не критично для создания партий
				} else {
					log.Printf("✅ Обновлен баланс контрагента %s: +%.2f (официальный)", counterpartyID, totalAmount)
				}
			} else {
				// Оплачено наличными - обновляем внутренний баланс
				if err := s.counterpartyService.UpdateCounterpartyBalance(counterpartyID, totalAmount, false); err != nil {
					log.Printf("⚠️ Ошибка обновления баланса контрагента: %v", err)
				} else {
					log.Printf("✅ Обновлен баланс контрагента %s: +%.2f (внутренний)", counterpartyID, totalAmount)
				}
			}
		}

		// Создаем финансовую транзакцию (Expense) и Bank Operation (если банковская)
		if s.financeService != nil && len(items) > 0 {
			// Парсим дату накладной или используем текущую дату
			parsedDate := time.Now()
			if invoiceDate != "" {
				if parsed, err := time.Parse("2006-01-02", invoiceDate); err == nil {
					parsedDate = parsed
				}
			}
			
			// Получаем branch_id из первого товара
			branchID := ""
			if branchIDVal, ok := items[0]["branch_id"].(string); ok {
				branchID = branchIDVal
			}
			
			if branchID != "" {
				// Создаем Expense транзакцию
				_, err := s.financeService.CreateExpenseFromInvoice(
					invoiceID,
					counterpartyID,
					totalAmount,
					branchID,
					parsedDate,
					isPaidCash,
					performedBy,
				)
				if err != nil {
					log.Printf("⚠️ Ошибка создания финансовой транзакции: %v", err)
					// Не возвращаем ошибку, это не критично для создания партий
				} else {
					log.Printf("✅ Создана финансовая транзакция (Expense) для накладной %s", invoiceID)
					
					// Если это банковская операция (не наличные), создаем запись Bank Operation со статусом Pending
					if !isPaidCash {
						log.Printf("📋 Создана банковская операция со статусом Pending для накладной %s", invoiceID)
						// Bank Operation уже создана как часть FinanceTransaction со статусом Pending
					}
				}
			}
		}
	}
	
	log.Printf("✅ Обработана накладная %s: создано %d партий", invoiceID, len(items))
	return nil
}

// CreateInvoice создает новую накладную (черновик) в БД
func (s *StockService) CreateInvoice(number string, counterpartyID *string, branchID string, totalAmount float64, invoiceDate string, isPaidCash bool, performedBy string, notes string, source string, items []map[string]interface{}) (*models.Invoice, error) {
	// Парсим дату накладной
	parsedDate := time.Now()
	if invoiceDate != "" {
		if parsed, err := time.Parse("2006-01-02", invoiceDate); err == nil {
			parsedDate = parsed
		}
	}
	
	// Определяем статус на основе source
	status := models.InvoiceStatusDraft
	if source == "finalized" {
		status = models.InvoiceStatusCompleted
	}
	
	invoice := &models.Invoice{
		Number:        number,
		CounterpartyID: counterpartyID,
		BranchID:      branchID,
		TotalAmount:   totalAmount,
		Status:        status,
		InvoiceDate:   parsedDate,
		IsPaidCash:    isPaidCash,
		PerformedBy:   performedBy,
		Notes:         notes,
	}
	
	if err := s.db.Create(invoice).Error; err != nil {
		return nil, fmt.Errorf("ошибка создания накладной: %w", err)
	}
	
	// Загружаем связи для ответа
	s.db.Preload("Counterparty").Preload("Branch").First(invoice, "id = ?", invoice.ID)
	
	log.Printf("✅ Создана накладная: ID=%s, номер=%s, статус=%s", invoice.ID, invoice.Number, invoice.Status)
	return invoice, nil
}

// GetInvoices возвращает список накладных с фильтрацией
func (s *StockService) GetInvoices(branchID string, status string, limit int) ([]models.Invoice, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	
	query := s.db.Model(&models.Invoice{}).
		Preload("Counterparty").
		Preload("Branch").
		Preload("StockBatches").
		Preload("StockBatches.Nomenclature").
		Order("created_at DESC").
		Limit(limit)
	
	if branchID != "" {
		query = query.Where("branch_id = ?", branchID)
	}
	
	if status != "" {
		query = query.Where("status = ?", status)
	}
	
	var invoices []models.Invoice
	if err := query.Find(&invoices).Error; err != nil {
		return nil, fmt.Errorf("ошибка получения накладных: %w", err)
	}
	
	return invoices, nil
}

// UpdateInvoice обновляет накладную (только черновики)
func (s *StockService) UpdateInvoice(invoiceID string, updates map[string]interface{}) (*models.Invoice, error) {
	// Проверяем, что накладная существует и является черновиком
	var invoice models.Invoice
	if err := s.db.First(&invoice, "id = ?", invoiceID).Error; err != nil {
		return nil, fmt.Errorf("накладная не найдена: %w", err)
	}
	
	if invoice.Status != models.InvoiceStatusDraft {
		return nil, fmt.Errorf("можно обновлять только черновики (текущий статус: %s)", invoice.Status)
	}
	
	// Обновляем поля
	updatesMap := make(map[string]interface{})
	
	if updates["number"] != nil {
		updatesMap["number"] = updates["number"]
	}
	if updates["counterparty_id"] != nil {
		updatesMap["counterparty_id"] = updates["counterparty_id"]
	}
	if updates["branch_id"] != nil {
		updatesMap["branch_id"] = updates["branch_id"]
	}
	if updates["total_amount"] != nil {
		updatesMap["total_amount"] = updates["total_amount"]
	}
	if updates["invoice_date"] != nil {
		if dateStr, ok := updates["invoice_date"].(string); ok && dateStr != "" {
			if parsed, err := time.Parse("2006-01-02", dateStr); err == nil {
				updatesMap["invoice_date"] = parsed
			}
		}
	}
	if updates["is_paid_cash"] != nil {
		updatesMap["is_paid_cash"] = updates["is_paid_cash"]
	}
	if updates["performed_by"] != nil {
		updatesMap["performed_by"] = updates["performed_by"]
	}
	if updates["notes"] != nil {
		updatesMap["notes"] = updates["notes"]
	}
	
	if len(updatesMap) > 0 {
		if err := s.db.Model(&invoice).Updates(updatesMap).Error; err != nil {
			return nil, fmt.Errorf("ошибка обновления накладной: %w", err)
		}
	}
	
	// Загружаем обновленную накладную с связями
	s.db.Preload("Counterparty").Preload("Branch").First(&invoice, "id = ?", invoiceID)
	
	log.Printf("✅ Обновлена накладная: ID=%s", invoiceID)
	return &invoice, nil
}

// DeleteInvoice удаляет накладную (только черновики)
func (s *StockService) DeleteInvoice(invoiceID string) error {
	// Проверяем, что накладная существует и является черновиком
	var invoice models.Invoice
	if err := s.db.First(&invoice, "id = ?", invoiceID).Error; err != nil {
		return fmt.Errorf("накладная не найдена: %w", err)
	}
	
	if invoice.Status != models.InvoiceStatusDraft {
		return fmt.Errorf("можно удалять только черновики (текущий статус: %s)", invoice.Status)
	}
	
	// Мягкое удаление
	if err := s.db.Delete(&invoice).Error; err != nil {
		return fmt.Errorf("ошибка удаления накладной: %w", err)
	}
	
	log.Printf("✅ Удалена накладная: ID=%s", invoiceID)
	return nil
}

// CheckRecipeAvailability проверяет доступность ингредиентов для рецепта без фактического списания
// Возвращает ошибку, если ингредиентов недостаточно
func (s *StockService) CheckRecipeAvailability(recipeID string, quantity float64, branchID string) error {
	// Получаем рецепт
	var recipe models.Recipe
	if err := s.db.Preload("Ingredients").Preload("Ingredients.Nomenclature").Preload("Ingredients.IngredientRecipe").
		First(&recipe, "id = ?", recipeID).Error; err != nil {
		return fmt.Errorf("рецепт не найден: %w", err)
	}

	// Для каждого ингредиента проверяем остатки (рекурсивно)
	visitedRecipes := make(map[string]bool)
	visitedRecipes[recipeID] = true

	for _, ingredient := range recipe.Ingredients {
		// requiredQuantity в граммах (quantity - количество порций готового продукта)
		requiredQuantity := ingredient.Quantity * quantity

		if err := s.checkIngredientAvailability(ingredient, requiredQuantity, branchID, visitedRecipes); err != nil {
			return err
		}
	}

	return nil
}

// checkIngredientAvailability рекурсивно проверяет доступность ингредиента
func (s *StockService) checkIngredientAvailability(ingredient models.RecipeIngredient, requiredQuantity float64, branchID string, visitedRecipes map[string]bool) error {
	// Защита от циклических зависимостей
	if ingredient.IngredientRecipeID != nil {
		if visitedRecipes[*ingredient.IngredientRecipeID] {
			return fmt.Errorf("обнаружена циклическая зависимость в рецептах: %s", *ingredient.IngredientRecipeID)
		}
		visitedRecipes[*ingredient.IngredientRecipeID] = true
	}

	// Если ингредиент - это полуфабрикат (есть связанный рецепт)
	if ingredient.IngredientRecipeID != nil {
		// Загружаем рецепт полуфабриката
		var subRecipe models.Recipe
		if err := s.db.Preload("Ingredients").Preload("Ingredients.Nomenclature").Preload("Ingredients.IngredientRecipe").
			First(&subRecipe, "id = ?", *ingredient.IngredientRecipeID).Error; err != nil {
			return fmt.Errorf("рецепт полуфабриката не найден: %w", err)
		}

		// Рекурсивно проверяем ингредиенты полуфабриката
		subRecipeQuantity := requiredQuantity / subRecipe.PortionSize // Количество порций полуфабриката

		for _, subIngredient := range subRecipe.Ingredients {
			subRequiredQuantity := subIngredient.Quantity * subRecipeQuantity
			if err := s.checkIngredientAvailability(subIngredient, subRequiredQuantity, branchID, visitedRecipes); err != nil {
				return err
			}
		}
		return nil
	}

	// Если ингредиент - это сырье (nomenclature_id)
	if ingredient.NomenclatureID == nil {
		return fmt.Errorf("ингредиент должен иметь либо nomenclature_id, либо ingredient_recipe_id")
	}

	// Находим партии с достаточным остатком (FEFO по сроку годности)
	var batches []models.StockBatch
	if err := s.db.Where("nomenclature_id = ? AND branch_id = ? AND remaining_quantity > 0 AND is_expired = false",
		*ingredient.NomenclatureID, branchID).
		Order("COALESCE(expiry_at, '9999-12-31') ASC").
		Find(&batches).Error; err != nil {
		return fmt.Errorf("ошибка получения партий: %w", err)
	}

	// Проверяем, достаточно ли остатков
	availableQuantity := 0.0
	for _, batch := range batches {
		availableQuantity += batch.RemainingQuantity
	}

	if availableQuantity < requiredQuantity {
		var ingredientName string
		if ingredient.Nomenclature != nil {
			ingredientName = ingredient.Nomenclature.Name
		} else {
			ingredientName = "неизвестный ингредиент"
		}
		return fmt.Errorf("недостаточно остатков для ингредиента '%s': требуется %.2f г, доступно %.2f г",
			ingredientName, requiredQuantity, availableQuantity)
	}

	return nil
}

// CheckExtraAvailability проверяет доступность ингредиентов для допа
// extraID - ID допа из таблицы extras
// quantity - количество единиц допа
func (s *StockService) CheckExtraAvailability(extraID uint, quantity int, branchID string) error {
	// Получаем доп
	var extra models.ExtraDB
	if err := s.db.Preload("Nomenclature").Preload("Recipe").Preload("Recipe.Ingredients").
		First(&extra, "id = ?", extraID).Error; err != nil {
		return fmt.Errorf("доп с ID %d не найден: %w", extraID, err)
	}

	if !extra.IsActive {
		return fmt.Errorf("доп '%s' неактивен", extra.Name)
	}

	// Если доп связан с номенклатурой (простой доп)
	if extra.NomenclatureID != nil {
		// Проверяем остатки номенклатуры
		// Используем portion_weight_grams из допа (best practice: точное значение из БД)
		portionWeightGrams := float64(extra.PortionWeightGrams)
		if portionWeightGrams <= 0 {
			// Если вес не указан, используем значение по умолчанию (50г) и логируем предупреждение
			portionWeightGrams = 50.0
			log.Printf("⚠️ Доп '%s' (ID: %d) не имеет указанного веса порции, используется значение по умолчанию: 50г", extra.Name, extraID)
		}
		
		requiredQuantity := portionWeightGrams * float64(quantity)

		var batches []models.StockBatch
		if err := s.db.Where("nomenclature_id = ? AND branch_id = ? AND remaining_quantity > 0 AND is_expired = false",
			*extra.NomenclatureID, branchID).
			Find(&batches).Error; err != nil {
			return fmt.Errorf("ошибка получения партий для допа: %w", err)
		}

		availableQuantity := 0.0
		for _, batch := range batches {
			availableQuantity += batch.RemainingQuantity
		}

		if availableQuantity < requiredQuantity {
			extraName := extra.Name
			if extra.Nomenclature != nil {
				extraName = extra.Nomenclature.Name
			}
			return fmt.Errorf("недостаточно остатков для допа '%s': требуется %.2f г, доступно %.2f г",
				extraName, requiredQuantity, availableQuantity)
		}

		return nil
	}

	// Если доп связан с рецептом (сложный доп)
	if extra.RecipeID != nil {
		return s.CheckRecipeAvailability(*extra.RecipeID, float64(quantity), branchID)
	}

	// Если доп не связан ни с номенклатурой, ни с рецептом - считаем доступным
	return nil
}


