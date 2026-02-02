package services

import (
	"fmt"
	"log"
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
	PricePerKg     decimal.Decimal // Цена за InboundUnit (кг/л/шт) - единица закупки из номенклатуры
	PricePerGram   decimal.Decimal // Цена за BaseUnit (г/мл/шт) - вычисляется через ConversionFactor из номенклатуры
	TotalCost      decimal.Decimal // Общая стоимость: Quantity * PricePerGram
	ExpiryAt       *time.Time
	ConversionFactor decimal.Decimal // Коэффициент конвертации из номенклатуры (InboundUnit -> BaseUnit)
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
	
	// Получаем единицу измерения
	unit, ok := itemData["unit"].(string)
	if !ok || unit == "" {
		unit = "g" // Значение по умолчанию
	}
	
	// Получаем цену за единицу (Major Unit - кг/л/шт)
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
	
	// Загружаем данные номенклатуры для получения InboundUnit и ConversionFactor
	var nomenclature models.NomenclatureItem
	if err := db.First(&nomenclature, "id = ?", nomenclatureID).Error; err != nil {
		return nil, fmt.Errorf("номенклатура с ID %s не найдена: %w", nomenclatureID, err)
	}
	
	// Используем InboundUnit из номенклатуры (единица измерения для закупки)
	inboundUnit := nomenclature.InboundUnit
	if inboundUnit == "" {
		inboundUnit = nomenclature.BaseUnit // Fallback на BaseUnit
	}
	
	// Получаем коэффициент конвертации из номенклатуры
	conversionFactor := decimal.NewFromFloat(nomenclature.ConversionFactor)
	if conversionFactor.LessThanOrEqual(decimal.Zero) {
		conversionFactor = decimal.NewFromInt(1) // По умолчанию 1, если не указан
	}
	
	// Вычисляем цену за Base Unit (грамм/миллилитр)
	// Если InboundUnit != BaseUnit, нужно конвертировать цену
	// Например: цена 100₽ за кг (InboundUnit), BaseUnit = g, ConversionFactor = 1000
	// Тогда цена за грамм = 100 / 1000 = 0.1₽
	pricePerBaseUnit := pricePerUnit
	if nomenclature.BaseUnit != inboundUnit && conversionFactor.GreaterThan(decimal.NewFromInt(1)) {
		// Цена указана за InboundUnit (кг/л), конвертируем в BaseUnit (г/мл)
		pricePerBaseUnit = pricePerUnit.Div(conversionFactor)
	}
	
	// Вычисляем общую стоимость: Total_Cost = Quantity_In_BaseUnit * Price_Per_BaseUnit
	totalCost := quantity.Mul(pricePerBaseUnit)
	
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
		Quantity:        quantity,
		Unit:            unit,
		PricePerKg:      pricePerUnit,      // Цена за InboundUnit (кг/л/шт)
		PricePerGram:    pricePerBaseUnit,  // Цена за BaseUnit (г/мл/шт)
		TotalCost:       totalCost,
		ExpiryAt:        expiryAt,
		ConversionFactor: conversionFactor,
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
	
	// Шаг 3: Создаем Invoice (Source of Truth)
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
	
	// Генерируем номер накладной (если не передан)
	invoiceNumber := invoiceID
	if invoiceNumber == "" || invoiceNumber == invoiceUUID {
		invoiceNumber = fmt.Sprintf("INV-%s", time.Now().Format("20060102-150405"))
	}
	
	// Создаем Invoice запись
	invoice := &models.Invoice{
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
	
	log.Printf("✅ Создана накладная %s (ID: %s, сумма: %.2f)", invoiceNumber, invoiceUUID, totalAmount)
	
	// Шаг 4: Подготавливаем данные для батч-вставки
	// Разбиваем на чанки по 1500 строк (безопасно для PostgreSQL параметров)
	const chunkSize = 1500
	batches := make([]models.StockBatch, 0, len(validatedItems))
	movements := make([]models.StockMovement, 0, len(validatedItems))
	
	now := time.Now()
	
	for _, item := range validatedItems {
		// Генерируем UUID для партии
		batchID := uuid.New().String()
		
		// Создаем StockBatch с FK на Invoice
		// CostPerUnit сохраняется как цена за BaseUnit (г/мл/шт) для корректного расчета стоимости остатков
		batch := models.StockBatch{
			ID:                batchID,
			NomenclatureID:    item.NomenclatureID,
			BranchID:          item.BranchID,
			Quantity:          item.Quantity.InexactFloat64(), // Количество в BaseUnit (г/мл/шт)
			Unit:              item.Unit,
			CostPerUnit:       item.PricePerGram.InexactFloat64(), // Цена за BaseUnit (из номенклатуры: InboundUnit -> BaseUnit через ConversionFactor)
			ExpiryAt:          item.ExpiryAt,
			Source:            "invoice",
			InvoiceID:         &invoiceUUID, // FK на Invoice (Source of Truth)
			RemainingQuantity: item.Quantity.InexactFloat64(),
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


