package services

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"zephyrvpn/server/internal/models"

	"gorm.io/gorm"
)

// InvoiceItem представляет валидированный товар из накладной
type InvoiceItem struct {
	NomenclatureID string
	BranchID       string
	Quantity       decimal.Decimal // Количество в BaseUnit (г/мл/шт)
	Unit           string          // Единица измерения из накладной
	PricePerUnit   decimal.Decimal // Цена за упаковку (если указан pack_size) или за InboundUnit
	PricePerKg     decimal.Decimal // Цена за InboundUnit (кг/л/шт) - вычисленная цена за единицу после деления на pack_size
	PricePerGram   decimal.Decimal // Цена за BaseUnit (г/мл/шт) - вычисляется через ConversionFactor из номенклатуры
	TotalCost      decimal.Decimal // Общая стоимость: Quantity * PricePerGram
	ExpiryAt       *time.Time
	ConversionFactor decimal.Decimal // Коэффициент конвертации из номенклатуры (InboundUnit -> BaseUnit)
	PackSize       decimal.Decimal   // Размер упаковки (например, 10 для "Ведро 10кг") - опционально
}

// ValidateInvoiceItem выполняет предварительную валидацию товара
// db используется для загрузки данных номенклатуры (InboundUnit, ConversionFactor)
func ValidateInvoiceItem(db *gorm.DB, itemData map[string]interface{}) (*InvoiceItem, error) {
	// Проверяем nomenclature_id (должен быть валидным UUID)
	nomenclatureID, ok := itemData["nomenclature_id"].(string)
	if !ok || nomenclatureID == "" {
		return nil, fmt.Errorf("отсутствует nomenclature_id")
	}
	
	// Проверяем формат UUID
	if _, err := uuid.Parse(nomenclatureID); err != nil {
		return nil, fmt.Errorf("невалидный UUID для nomenclature_id: %s", nomenclatureID)
	}
	
	// Проверяем branch_id (должен быть валидным UUID)
	branchID, ok := itemData["branch_id"].(string)
	if !ok || branchID == "" {
		return nil, fmt.Errorf("отсутствует branch_id")
	}
	
	if _, err := uuid.Parse(branchID); err != nil {
		return nil, fmt.Errorf("невалидный UUID для branch_id: %s", branchID)
	}
	
	// Получаем количество (вес в граммах)
	var quantity decimal.Decimal
	if qtyVal, ok := itemData["quantity"]; ok {
		switch v := qtyVal.(type) {
		case float64:
			quantity = decimal.NewFromFloat(v)
		case int:
			quantity = decimal.NewFromInt(int64(v))
		case int64:
			quantity = decimal.NewFromInt(v)
		case string:
			var err error
			quantity, err = decimal.NewFromString(v)
			if err != nil {
				return nil, fmt.Errorf("неверный формат quantity: %v", v)
			}
		default:
			return nil, fmt.Errorf("неверный тип quantity: %T", v)
		}
	} else {
		return nil, fmt.Errorf("отсутствует quantity")
	}
	
	// Валидация: вес должен быть > 0
	if quantity.LessThanOrEqual(decimal.Zero) {
		return nil, fmt.Errorf("quantity должен быть > 0, получено: %s", quantity.String())
	}
	
	// Получаем единицу измерения из накладной
	unit, ok := itemData["unit"].(string)
	if !ok || unit == "" {
		unit = "g" // Значение по умолчанию
	}
	
	// Загружаем данные номенклатуры ДО обработки цены, чтобы знать BaseUnit и InboundUnit
	var nomenclature models.NomenclatureItem
	if err := db.First(&nomenclature, "id = ?", nomenclatureID).Error; err != nil {
		return nil, fmt.Errorf("номенклатура с ID %s не найдена: %w", nomenclatureID, err)
	}
	
	// Используем InboundUnit из номенклатуры (единица измерения для закупки)
	inboundUnit := nomenclature.InboundUnit
	if inboundUnit == "" {
		inboundUnit = nomenclature.BaseUnit // Fallback на BaseUnit
	}
	
	baseUnit := nomenclature.BaseUnit
	if baseUnit == "" {
		baseUnit = "g" // Значение по умолчанию
	}
	
	// КРИТИЧЕСКИ ВАЖНО: Исправляем BaseUnit, если он установлен неправильно
	// BaseUnit должен быть минимальной единицей (г/мл), а не крупной (кг/л) для правильной работы формул
	baseUnitNormalized := strings.ToLower(strings.TrimSpace(baseUnit))
	inboundUnitNormalized := strings.ToLower(strings.TrimSpace(nomenclature.InboundUnit))
	if (baseUnitNormalized == "кг" || baseUnitNormalized == "kg") && 
	   (inboundUnitNormalized == "кг" || inboundUnitNormalized == "kg") {
		// Если BaseUnit = "кг" и InboundUnit = "кг", исправляем BaseUnit на "г"
		log.Printf("⚠️ ВНИМАНИЕ: BaseUnit товара '%s' установлен как 'kg', исправляем на 'g' для правильной работы формул",
			nomenclature.Name)
		baseUnit = "g"
		baseUnitNormalized = "g"
	} else if (baseUnitNormalized == "л" || baseUnitNormalized == "l") && 
	          (inboundUnitNormalized == "л" || inboundUnitNormalized == "l") {
		// Если BaseUnit = "л" и InboundUnit = "л", исправляем BaseUnit на "мл"
		log.Printf("⚠️ ВНИМАНИЕ: BaseUnit товара '%s' установлен как 'l', исправляем на 'ml' для правильной работы формул",
			nomenclature.Name)
		baseUnit = "ml"
		baseUnitNormalized = "ml"
	}
	
	// ВАЖНО: Конвертируем quantity в BaseUnit (граммы/мл/шт)
	// КРИТИЧЕСКИ ВАЖНО: Если BaseUnit = "g" или "ml", ВСЕГДА конвертируем в граммы/миллилитры
	// независимо от того, что ввел пользователь (кг/л или граммы/мл)
	// Если пользователь ввел 10 кг, а BaseUnit = "g", то quantity = 10000 г
	quantityInBaseUnit := quantity
	
	// Нормализуем единицы для сравнения (кг/КГ -> kg, г/Г -> g, л/Л -> l, мл/МЛ -> ml)
	unitNormalized := strings.ToLower(strings.TrimSpace(unit))
	if unitNormalized == "кг" || unitNormalized == "килограмм" {
		unitNormalized = "kg"
	} else if unitNormalized == "г" || unitNormalized == "грамм" {
		unitNormalized = "g"
	} else if unitNormalized == "л" || unitNormalized == "литр" {
		unitNormalized = "l"
	} else if unitNormalized == "мл" || unitNormalized == "миллилитр" {
		unitNormalized = "ml"
	}
	
	// baseUnitNormalized уже вычислен и исправлен выше
	
	// КРИТИЧЕСКИ ВАЖНО: Если BaseUnit = "g", ВСЕГДА конвертируем в граммы
	// Если BaseUnit = "ml", ВСЕГДА конвертируем в миллилитры
	if baseUnitNormalized == "g" {
		// BaseUnit = граммы - конвертируем все в граммы
		if unitNormalized == "kg" || unit == "кг" || unit == "КГ" {
			quantityInBaseUnit = quantity.Mul(decimal.NewFromInt(1000)) // кг -> г
		} else if unitNormalized == "g" || unit == "г" || unit == "Г" {
			// Уже в граммах, конвертация не нужна
			quantityInBaseUnit = quantity
		} else {
			// Используем conversion_factor если он указан
			conversionFactor := decimal.NewFromFloat(nomenclature.ConversionFactor)
			if conversionFactor.GreaterThan(decimal.Zero) && conversionFactor.GreaterThan(decimal.NewFromInt(1)) {
				quantityInBaseUnit = quantity.Mul(conversionFactor)
			} else {
				// Если conversion_factor не указан или = 1, оставляем как есть
				quantityInBaseUnit = quantity
			}
		}
	} else if baseUnitNormalized == "ml" {
		// BaseUnit = миллилитры - конвертируем все в миллилитры
		if unitNormalized == "l" || unit == "л" || unit == "Л" {
			quantityInBaseUnit = quantity.Mul(decimal.NewFromInt(1000)) // л -> мл
		} else if unitNormalized == "ml" || unit == "мл" || unit == "МЛ" {
			// Уже в миллилитрах, конвертация не нужна
			quantityInBaseUnit = quantity
		} else {
			// Используем conversion_factor если он указан
			conversionFactor := decimal.NewFromFloat(nomenclature.ConversionFactor)
			if conversionFactor.GreaterThan(decimal.Zero) && conversionFactor.GreaterThan(decimal.NewFromInt(1)) {
				quantityInBaseUnit = quantity.Mul(conversionFactor)
			} else {
				// Если conversion_factor не указан или = 1, оставляем как есть
				quantityInBaseUnit = quantity
			}
		}
	} else if baseUnitNormalized == "kg" {
		// BaseUnit = килограммы - конвертируем в килограммы
		if unitNormalized == "g" || unit == "г" || unit == "Г" {
			quantityInBaseUnit = quantity.Div(decimal.NewFromInt(1000)) // г -> кг
		} else if unitNormalized == "kg" || unit == "кг" || unit == "КГ" {
			// Уже в килограммах, конвертация не нужна
			quantityInBaseUnit = quantity
		} else {
			// Используем conversion_factor если он указан
			conversionFactor := decimal.NewFromFloat(nomenclature.ConversionFactor)
			if conversionFactor.GreaterThan(decimal.Zero) && conversionFactor.GreaterThan(decimal.NewFromInt(1)) {
				quantityInBaseUnit = quantity.Div(conversionFactor)
			} else {
				quantityInBaseUnit = quantity
			}
		}
	} else if baseUnitNormalized == "l" {
		// BaseUnit = литры - конвертируем в литры
		if unitNormalized == "ml" || unit == "мл" || unit == "МЛ" {
			quantityInBaseUnit = quantity.Div(decimal.NewFromInt(1000)) // мл -> л
		} else if unitNormalized == "l" || unit == "л" || unit == "Л" {
			// Уже в литрах, конвертация не нужна
			quantityInBaseUnit = quantity
		} else {
			// Используем conversion_factor если он указан
			conversionFactor := decimal.NewFromFloat(nomenclature.ConversionFactor)
			if conversionFactor.GreaterThan(decimal.Zero) && conversionFactor.GreaterThan(decimal.NewFromInt(1)) {
				quantityInBaseUnit = quantity.Div(conversionFactor)
			} else {
				quantityInBaseUnit = quantity
			}
		}
	} else {
		// Для других единиц (шт, box и т.д.) используем conversion_factor или оставляем как есть
		if unitNormalized != baseUnitNormalized {
			conversionFactor := decimal.NewFromFloat(nomenclature.ConversionFactor)
			if conversionFactor.GreaterThan(decimal.Zero) && conversionFactor.GreaterThan(decimal.NewFromInt(1)) {
				// Определяем направление конвертации
				if baseUnitNormalized == "g" && (unitNormalized == "kg" || unit == "кг" || unit == "КГ") {
					quantityInBaseUnit = quantity.Mul(conversionFactor)
				} else if baseUnitNormalized == "ml" && (unitNormalized == "l" || unit == "л" || unit == "Л") {
					quantityInBaseUnit = quantity.Mul(conversionFactor)
				} else {
					quantityInBaseUnit = quantity.Div(conversionFactor)
				}
			} else {
				quantityInBaseUnit = quantity
			}
		} else {
			quantityInBaseUnit = quantity
		}
	}
	
	log.Printf("🔄 Конвертация количества: %.2f %s -> %.2f %s (BaseUnit=%s)", 
		quantity.InexactFloat64(), unit, quantityInBaseUnit.InexactFloat64(), baseUnit, baseUnit)
	
	// Получаем цену за упаковку (или за единицу, если pack_size не указан)
	var pricePerUnit decimal.Decimal
	if priceVal, ok := itemData["price_per_unit"]; ok {
		switch v := priceVal.(type) {
		case float64:
			pricePerUnit = decimal.NewFromFloat(v)
		case int:
			pricePerUnit = decimal.NewFromInt(int64(v))
		case int64:
			pricePerUnit = decimal.NewFromInt(v)
		case string:
			var err error
			pricePerUnit, err = decimal.NewFromString(v)
			if err != nil {
				return nil, fmt.Errorf("неверный формат price_per_unit: %v", v)
			}
		default:
			return nil, fmt.Errorf("неверный тип price_per_unit: %T", v)
		}
	} else {
		return nil, fmt.Errorf("отсутствует price_per_unit")
	}
	
	// Валидация: цена должна быть > 0
	if pricePerUnit.LessThanOrEqual(decimal.Zero) {
		return nil, fmt.Errorf("price_per_unit должен быть > 0, получено: %s", pricePerUnit.String())
	}
	
	// Получаем коэффициент конвертации из номенклатуры
	conversionFactor := decimal.NewFromFloat(nomenclature.ConversionFactor)
	if conversionFactor.LessThanOrEqual(decimal.Zero) {
		conversionFactor = decimal.NewFromInt(1) // По умолчанию 1, если не указан
	}
	
	// Получаем размер упаковки (pack_size) - опционально
	// ВАЖНО: pack_size должен быть в единицах InboundUnit (кг/л/шт)
	// Пример: "Ведро 10кг" -> pack_size = 10 (кг), не 10000 (г)
	// Если указан, то price_per_unit - это цена за упаковку, и нужно разделить на pack_size
	var packSize decimal.Decimal
	if packSizeVal, ok := itemData["pack_size"]; ok && packSizeVal != nil {
		switch v := packSizeVal.(type) {
		case float64:
			packSize = decimal.NewFromFloat(v)
		case int:
			packSize = decimal.NewFromInt(int64(v))
		case int64:
			packSize = decimal.NewFromInt(v)
		case string:
			var err error
			packSize, err = decimal.NewFromString(v)
			if err != nil {
				return nil, fmt.Errorf("неверный формат pack_size: %v", v)
			}
		default:
			// Игнорируем неверный тип, pack_size опционален
		}
		
		// Валидация: если pack_size указан, он должен быть > 0
		if packSize.GreaterThan(decimal.Zero) {
			// pack_size валиден, будет использован для нормализации цены
		} else if packSize.LessThan(decimal.Zero) {
			// Отрицательный pack_size недопустим
			return nil, fmt.Errorf("pack_size не может быть отрицательным, получено: %s", packSize.String())
		}
		// Если packSize = 0, это нормально - pack_size опционален
	}
	
	// ВАЖНО: Нормализация цены - вычисляем цену за 1 базовую единицу измерения (кг/л/шт)
	// Формула: CostPerUnit (за кг/л) = Сумма_за_упаковку / Вес_упаковки_в_кг
	// Пример: "Ведро 10кг" за 1221₽ -> pricePerUnit = 1221, packSize = 10 -> pricePerInboundUnit = 1221 / 10 = 122.1₽/кг
	// В StockBatch.cost_per_unit сохраняется цена за единицу (122.1), а не за упаковку (1221)
	pricePerInboundUnit := pricePerUnit
	if packSize.GreaterThan(decimal.Zero) {
		pricePerInboundUnit = pricePerUnit.Div(packSize)
		log.Printf("📦 Нормализация цены: цена за упаковку %.2f₽ / размер упаковки %.2f %s = цена за единицу %.2f₽/%s",
			pricePerUnit.InexactFloat64(), packSize.InexactFloat64(), inboundUnit, pricePerInboundUnit.InexactFloat64(), inboundUnit)
	}
	
	// ВАЖНО: Расчет общей стоимости используя shopspring/decimal для точности
	// Формула: TotalCost = (QuantityInBaseUnit / ConversionFactor) * PricePerInboundUnit
	// Пример: 10000г / 1000 * 122.1₽/кг = 10кг * 122.1₽/кг = 1221₽
	// Сначала конвертируем quantity в единицы цены (InboundUnit), затем умножаем на цену
	var quantityInInboundUnit decimal.Decimal
	if conversionFactor.GreaterThan(decimal.NewFromInt(1)) {
		quantityInInboundUnit = quantityInBaseUnit.Div(conversionFactor)
	} else {
		quantityInInboundUnit = quantityInBaseUnit
	}
	totalCost := quantityInInboundUnit.Mul(pricePerInboundUnit)
	
	// PricePerGram вычисляется только для справки (не сохраняется в батче)
	pricePerBaseUnit := pricePerInboundUnit
	if nomenclature.BaseUnit != inboundUnit && conversionFactor.GreaterThan(decimal.NewFromInt(1)) {
		pricePerBaseUnit = pricePerInboundUnit.Div(conversionFactor)
	}
	
	// Обрабатываем expiry_date
	var expiryAt *time.Time
	if expiryDate, exists := itemData["expiry_date"]; exists && expiryDate != nil {
		if expiryStr, ok := expiryDate.(string); ok && expiryStr != "" {
			if parsedTime, err := time.Parse("2006-01-02", expiryStr); err == nil {
				expiryAt = &parsedTime
			}
		}
	}
	
		return &InvoiceItem{
			NomenclatureID:  nomenclatureID,
			BranchID:        branchID,
			Quantity:        quantityInBaseUnit, // Количество в BaseUnit (г/мл/шт) - конвертировано из unit
			Unit:            baseUnit,           // Единица измерения в BaseUnit
			PricePerUnit:    pricePerUnit,       // Цена за упаковку (если указан pack_size) или за InboundUnit
			PricePerKg:      pricePerInboundUnit, // Цена за InboundUnit (кг/л/шт) - нормализованная цена за единицу
			PricePerGram:    pricePerBaseUnit,   // Цена за BaseUnit (г/мл/шт) - вычисляется через ConversionFactor
			TotalCost:       totalCost,          // Общая стоимость: (QuantityInBaseUnit / ConversionFactor) * PricePerInboundUnit
			ExpiryAt:        expiryAt,
			ConversionFactor: conversionFactor,
			PackSize:        packSize,           // Размер упаковки в InboundUnit (опционально)
		}, nil
}

// ProcessInboundInvoiceBatch обрабатывает входящую накладную с использованием батч-вставки
// Создает Invoice как Source of Truth, затем батч-вставляет товары
func (s *StockService) ProcessInboundInvoiceBatch(invoiceID string, items []map[string]interface{}, performedBy string, counterpartyID string, totalAmount float64, isPaidCash bool, invoiceDate string) error {
	// Шаг 1: Pre-flight валидация всех товаров (до транзакции)
	validatedItems := make([]*InvoiceItem, 0, len(items))
	validationErrors := make([]string, 0)
	
	for i, itemData := range items {
		validatedItem, err := ValidateInvoiceItem(s.db, itemData)
		if err != nil {
			validationErrors = append(validationErrors, fmt.Sprintf("Строка %d: %v", i+1, err))
			log.Printf("⚠️ Пропущен товар (строка %d): %v", i+1, err)
			continue
		}
		validatedItems = append(validatedItems, validatedItem)
	}
	
	if len(validationErrors) > 0 {
		log.Printf("⚠️ Найдено %d ошибок валидации из %d товаров", len(validationErrors), len(items))
	}
	
	if len(validatedItems) == 0 {
		return fmt.Errorf("нет валидных товаров для обработки")
	}
	
	// Шаг 2: Начинаем транзакцию
	tx := s.db.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
			log.Printf("❌ Транзакция откачена из-за panic: %v", r)
		}
	}()
	
	// Шаг 3: Создаем или обновляем Invoice (Source of Truth)
	// Генерируем invoiceID если не передан или невалидный
	var invoiceUUID string
	if invoiceID != "" {
		if _, err := uuid.Parse(invoiceID); err == nil {
			invoiceUUID = invoiceID
		} else {
			invoiceUUID = uuid.New().String()
			log.Printf("⚠️ invoiceID '%s' не является UUID, создан новый: %s", invoiceID, invoiceUUID)
		}
	} else {
		invoiceUUID = uuid.New().String()
	}
	
	// Получаем branch_id из первого товара
	branchID := validatedItems[0].BranchID
	
	// Парсим дату накладной
	parsedInvoiceDate := time.Now()
	if invoiceDate != "" {
		if parsed, err := time.Parse("2006-01-02", invoiceDate); err == nil {
			parsedInvoiceDate = parsed
		}
	}
	
	// Проверяем, существует ли накладная (черновик)
	var existingInvoice models.Invoice
	invoiceExists := tx.Where("id = ?", invoiceUUID).First(&existingInvoice).Error == nil
	
	// Определяем номер накладной (будет использован везде)
	var invoiceNumber string
	var invoice *models.Invoice
	
	if invoiceExists {
		// Обновляем существующую накладную (черновик) - меняем статус на Completed
		invoiceNumber = existingInvoice.Number // Используем существующий номер
		existingInvoice.Status = models.InvoiceStatusCompleted
		existingInvoice.TotalAmount = totalAmount
		existingInvoice.IsPaidCash = isPaidCash
		existingInvoice.PerformedBy = performedBy
		if counterpartyID != "" {
			existingInvoice.CounterpartyID = &counterpartyID
		}
		existingInvoice.Notes = fmt.Sprintf("Оприходование %d товаров", len(validatedItems))
		if err := tx.Save(&existingInvoice).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("ошибка обновления накладной: %w", err)
		}
		invoice = &existingInvoice
		log.Printf("✅ Обновлена накладная (черновик → завершена): ID=%s, номер=%s", invoiceUUID, invoiceNumber)
	} else {
		// Создаем новую накладную
		invoiceNumber = invoiceID
		if invoiceNumber == "" || invoiceNumber == invoiceUUID {
			invoiceNumber = fmt.Sprintf("INV-%s", time.Now().Format("20060102-150405"))
		}
		
		invoice = &models.Invoice{
			ID:            invoiceUUID,
			Number:        invoiceNumber,
			CounterpartyID: &counterpartyID,
			TotalAmount:   totalAmount,
			Status:        models.InvoiceStatusCompleted,
			BranchID:      branchID,
			InvoiceDate:   parsedInvoiceDate,
			IsPaidCash:    isPaidCash,
			PerformedBy:   performedBy,
			Notes:         fmt.Sprintf("Оприходование %d товаров", len(validatedItems)),
		}
		
		if err := tx.Create(invoice).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("ошибка создания накладной: %w", err)
		}
		log.Printf("✅ Создана новая накладная: ID=%s, номер=%s, сумма=%.2f", invoiceUUID, invoiceNumber, totalAmount)
	}
	
	// Шаг 4: Подготавливаем данные для батч-вставки
	// Разбиваем на чанки по 1500 строк (безопасно для PostgreSQL параметров)
	const chunkSize = 1500
	batches := make([]models.StockBatch, 0, len(validatedItems))
	movements := make([]models.StockMovement, 0, len(validatedItems))
	
	now := time.Now()
	
	for _, item := range validatedItems {
		// Генерируем UUID для партии
		batchID := uuid.New().String()
		
		// Загружаем номенклатуру для логирования
		var nomenclature models.NomenclatureItem
		if err := s.db.First(&nomenclature, "id = ?", item.NomenclatureID).Error; err == nil {
			// Логирование для отладки
			costPerUnitValue := item.PricePerKg.InexactFloat64()
			quantityValue := item.Quantity.InexactFloat64()
			conversionFactorValue := item.ConversionFactor.InexactFloat64()
			
			// Правильный расчет ожидаемой стоимости с учетом conversionFactor
			// Формула: (QuantityInBaseUnit * CostPerInboundUnit) / ConversionFactor
			var expectedCost float64
			if conversionFactorValue > 1 {
				expectedCost = (quantityValue * costPerUnitValue) / conversionFactorValue
			} else {
				expectedCost = quantityValue * costPerUnitValue
			}
			
			log.Printf("💾 Сохранение StockBatch для товара '%s' (ID: %s):", nomenclature.Name, item.NomenclatureID)
			log.Printf("   Quantity (BaseUnit): %.2f %s", quantityValue, item.Unit)
			log.Printf("   CostPerUnit (InboundUnit): %.2f₽/%s (цена за 1кг/1л, НЕ за грамм!)", 
				costPerUnitValue, nomenclature.InboundUnit)
			if conversionFactorValue > 1 {
				log.Printf("   Ожидаемая стоимость при чтении: (%.2f * %.2f) / %.0f = %.2f₽", 
					quantityValue, costPerUnitValue, conversionFactorValue, expectedCost)
			} else {
				log.Printf("   Ожидаемая стоимость при чтении: %.2f * %.2f = %.2f₽", 
					quantityValue, costPerUnitValue, expectedCost)
			}
		}
		
		// Создаем StockBatch с FK на Invoice
		// КРИТИЧЕСКИ ВАЖНО: CostPerUnit должен быть ценой за 1кг/1л, НЕ за грамм!
		// Если указан pack_size, цена нормализуется: pricePerInboundUnit = pricePerUnit / packSize
		// Пример: "Ведро 10кг" за 1,221₽ -> pack_size=10 -> CostPerUnit = 1221/10 = 122.1₽/кг
		// Если pack_size не указан, то CostPerUnit = price_per_unit (цена уже за единицу)
		// 
		// Количество сохраняется в BaseUnit (граммы): 10кг = 10000г
		// 
		// Формула расчета стоимости при чтении:
		// TotalValue = (RemainingQuantityInGrams * CostPerKg) / 1000
		// Пример: (10000г * 122.1₽/кг) / 1000 = 1,221₽
		batch := models.StockBatch{
			ID:                batchID,
			NomenclatureID:    item.NomenclatureID,
			BranchID:          item.BranchID,
			Quantity:          item.Quantity.InexactFloat64(), // Количество в BaseUnit (г/мл/шт)
			Unit:              item.Unit,
			CostPerUnit:       item.PricePerKg.InexactFloat64(), // Цена за InboundUnit (кг/л/шт) - цена за 1кг/1л!
			ExpiryAt:          item.ExpiryAt,
			Source:            "invoice",
			InvoiceID:         &invoiceUUID, // FK на Invoice (Source of Truth)
			RemainingQuantity: item.Quantity.InexactFloat64(), // Остаток в BaseUnit (г/мл/шт)
			CreatedAt:         now,
			UpdatedAt:         now,
		}
		batches = append(batches, batch)
		
		// Создаем StockMovement с FK на Invoice
		movement := models.StockMovement{
			ID:                uuid.New().String(),
			StockBatchID:      &batchID,
			NomenclatureID:    item.NomenclatureID,
			BranchID:          item.BranchID,
			Quantity:          item.Quantity.InexactFloat64(), // Положительное = приход
			Unit:              item.Unit,
			MovementType:      "invoice",
			InvoiceID:         &invoiceUUID, // FK на Invoice (Source of Truth)
			PerformedBy:       performedBy,
			Notes:             fmt.Sprintf("Оприходование по накладной %s", invoiceNumber),
			CreatedAt:         now,
		}
		movements = append(movements, movement)
	}
	
	// Шаг 5: Батч-вставка через GORM CreateInBatches (оптимизированная вставка)
	// Вставляем партии батчами по 1500 строк
	for i := 0; i < len(batches); i += chunkSize {
		end := i + chunkSize
		if end > len(batches) {
			end = len(batches)
		}
		
		chunk := batches[i:end]
		if err := tx.CreateInBatches(chunk, chunkSize).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("ошибка батч-вставки партий (чанк %d-%d): %w", i, end, err)
		}
	}
	
	// Вставляем движения батчами
	for i := 0; i < len(movements); i += chunkSize {
		end := i + chunkSize
		if end > len(movements) {
			end = len(movements)
		}
		
		chunk := movements[i:end]
		if err := tx.CreateInBatches(chunk, chunkSize).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("ошибка батч-вставки движений (чанк %d-%d): %w", i, end, err)
		}
	}
	
	// Шаг 6: Обновляем last_price для каждого уникального товара
	nomenclaturePriceMap := make(map[string]decimal.Decimal)
	for _, item := range validatedItems {
		// Сохраняем максимальную цену за Major Unit для каждого товара
		if currentPrice, exists := nomenclaturePriceMap[item.NomenclatureID]; !exists || item.PricePerKg.GreaterThan(currentPrice) {
			nomenclaturePriceMap[item.NomenclatureID] = item.PricePerKg
		}
	}
	
	for nomID, pricePerKg := range nomenclaturePriceMap {
		if err := tx.Model(&models.NomenclatureItem{}).
			Where("id = ?", nomID).
			Update("last_price", pricePerKg.InexactFloat64()).Error; err != nil {
			log.Printf("⚠️ Ошибка обновления last_price для товара %s: %v", nomID, err)
			// Не прерываем транзакцию
		}
	}
	
	// Шаг 7: Создаем финансовую транзакцию (в той же транзакции)
	if s.financeService != nil && counterpartyID != "" && totalAmount > 0 {
		// Определяем источник транзакции
		var source models.TransactionSource
		if isPaidCash {
			source = models.TransactionSourceCash
		} else {
			source = models.TransactionSourceBank
		}
		
		// Определяем статус транзакции
		var status models.TransactionStatus
		if isPaidCash {
			status = models.TransactionStatusCompleted
		} else {
			status = models.TransactionStatusPending // Банковские операции ожидают подтверждения
		}
		
		financeTransaction := &models.FinanceTransaction{
			Date:          parsedInvoiceDate,
			Type:          models.TransactionTypeExpense,
			Category:      "Операционные расходы",
			Amount:        totalAmount,
			Description:   fmt.Sprintf("Оприходование накладной %s", invoiceNumber),
			BranchID:      branchID,
			Source:        source,
			Status:        status,
			CounterpartyID: &counterpartyID,
			InvoiceID:     &invoiceUUID, // FK на Invoice
			PerformedBy:   performedBy,
		}
		
		if err := tx.Create(financeTransaction).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("ошибка создания финансовой транзакции: %w", err)
		}
		
		log.Printf("✅ Создана финансовая транзакция для накладной %s (ID: %s)", invoiceNumber, financeTransaction.ID)
	}
	
	// Шаг 8: Обновляем баланс контрагента (в той же транзакции)
	if s.counterpartyService != nil && counterpartyID != "" && totalAmount > 0 {
		// Обновляем баланс напрямую в транзакции для атомарности
		if !isPaidCash {
			// Официальный баланс (долг)
			if err := tx.Model(&models.Counterparty{}).
				Where("id = ?", counterpartyID).
				Update("balance_official", gorm.Expr("COALESCE(balance_official, 0) + ?", totalAmount)).Error; err != nil {
				tx.Rollback()
				return fmt.Errorf("ошибка обновления баланса контрагента: %w", err)
			}
		} else {
			// Внутренний баланс
			if err := tx.Model(&models.Counterparty{}).
				Where("id = ?", counterpartyID).
				Update("balance_internal", gorm.Expr("COALESCE(balance_internal, 0) + ?", totalAmount)).Error; err != nil {
				tx.Rollback()
				return fmt.Errorf("ошибка обновления баланса контрагента: %w", err)
			}
		}
		log.Printf("✅ Обновлен баланс контрагента %s: +%.2f", counterpartyID, totalAmount)
	}
	
	// Шаг 9: Коммитим транзакцию (все или ничего)
	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("ошибка коммита транзакции: %w", err)
	}
	
	log.Printf("✅ Обработана накладная %s (ID: %s): создано %d партий (валидировано %d из %d)", 
		invoiceNumber, invoiceUUID, len(batches), len(validatedItems), len(items))
	
	return nil
}



