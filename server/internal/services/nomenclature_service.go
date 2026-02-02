package services

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/xuri/excelize/v2"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/transform"
	"gorm.io/gorm"
	"zephyrvpn/server/internal/models"
)

type NomenclatureService struct {
	db         *gorm.DB
	pluService *PLUService // Для генерации SKU на основе PLU
}

func NewNomenclatureService(db *gorm.DB) *NomenclatureService {
	return &NomenclatureService{
		db: db,
	}
}

// SetPLUService устанавливает PLU сервис для генерации SKU
func (ns *NomenclatureService) SetPLUService(pluService *PLUService) {
	ns.pluService = pluService
}

// GetAllItems возвращает все товары номенклатуры
func (ns *NomenclatureService) GetAllItems() ([]models.NomenclatureItem, error) {
	var items []models.NomenclatureItem
	if err := ns.db.Where("deleted_at IS NULL").Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

// GetItemByID возвращает товар по ID
func (ns *NomenclatureService) GetItemByID(id string) (*models.NomenclatureItem, error) {
	var item models.NomenclatureItem
	if err := ns.db.Where("id = ? AND deleted_at IS NULL", id).First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

// GetItemBySKU возвращает товар по SKU
func (ns *NomenclatureService) GetItemBySKU(sku string) (*models.NomenclatureItem, error) {
	var item models.NomenclatureItem
	if err := ns.db.Where("sku = ? AND deleted_at IS NULL", sku).First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

// CreateItem создает новый товар
func (ns *NomenclatureService) CreateItem(item *models.NomenclatureItem) error {
	// Проверка на дубликат SKU (глобальная проверка уникальности)
	var existing models.NomenclatureItem
	if err := ns.db.Where("sku = ? AND deleted_at IS NULL", item.SKU).First(&existing).Error; err == nil {
		return fmt.Errorf("товар с SKU '%s' уже существует", item.SKU)
	}
	
	// Если SKU не указан, генерируем его автоматически
	if item.SKU == "" {
		// Используем базовый генератор (без PLU сервиса, чтобы избежать циклических зависимостей)
		item.SKU = ns.generateBasicSKU(item.Name)
	}
	
	// Если указана категория по имени, находим её ID
	if item.CategoryName != "" && item.CategoryID == nil {
		var category models.NomenclatureCategory
		if err := ns.db.Where("name = ? AND deleted_at IS NULL", item.CategoryName).First(&category).Error; err == nil {
			item.CategoryID = &category.ID
			item.CategoryColor = category.Color
		}
	}
	
	return ns.db.Create(item).Error
}

// UpdateItem обновляет товар
func (ns *NomenclatureService) UpdateItem(id string, item *models.NomenclatureItem) error {
	// Проверка существования
	var existing models.NomenclatureItem
	if err := ns.db.Where("id = ? AND deleted_at IS NULL", id).First(&existing).Error; err != nil {
		return fmt.Errorf("товар не найден")
	}
	
	// Проверка на дубликат SKU (если SKU изменился)
	if item.SKU != existing.SKU {
		var duplicate models.NomenclatureItem
		if err := ns.db.Where("sku = ? AND id != ? AND deleted_at IS NULL", item.SKU, id).First(&duplicate).Error; err == nil {
			return fmt.Errorf("товар с SKU '%s' уже существует", item.SKU)
		}
	}
	
	// Если указана категория по имени, находим её ID
	if item.CategoryName != "" && item.CategoryID == nil {
		var category models.NomenclatureCategory
		if err := ns.db.Where("name = ? AND deleted_at IS NULL", item.CategoryName).First(&category).Error; err == nil {
			item.CategoryID = &category.ID
			item.CategoryColor = category.Color
		}
	}
	
	item.ID = id
	return ns.db.Model(&existing).Updates(item).Error
}

// DeleteItem удаляет товар (soft delete)
func (ns *NomenclatureService) DeleteItem(id string) error {
	return ns.db.Where("id = ?", id).Delete(&models.NomenclatureItem{}).Error
}

// generateBasicSKU генерирует базовый SKU на основе названия (fallback если PLU не найден)
func (ns *NomenclatureService) generateBasicSKU(productName string) string {
	// Нормализуем название
	normalizedName := strings.TrimSpace(strings.ToUpper(productName))
	
	// Берем первые буквы слов
	words := strings.Fields(normalizedName)
	sku := ""
	for _, word := range words {
		if len(word) > 0 {
			sku += string(word[0])
			if len(sku) >= 6 {
				break
			}
		}
	}
	
	// Убеждаемся, что SKU уникален
	baseSKU := sku
	counter := 1
	for {
		var count int64
		ns.db.Model(&models.NomenclatureItem{}).
			Where("sku = ? AND deleted_at IS NULL", sku).
			Count(&count)
		
		if count == 0 {
			break
		}
		
		sku = fmt.Sprintf("%s-%d", baseSKU, counter)
		counter++
		if counter > 999 {
			// Если не удалось найти уникальный, используем UUID
			sku = fmt.Sprintf("AUTO-%s", strings.ToUpper(productName[:min(8, len(productName))]))
			break
		}
	}
	
	return sku
}

// min возвращает минимальное из двух чисел
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ValidateImport валидирует данные перед импортом
func (ns *NomenclatureService) ValidateImport(items []map[string]interface{}, fieldMapping map[string]string, autoCreateCategories bool) []models.ImportValidationResult {
	results := make([]models.ImportValidationResult, 0, len(items))
	
	// Получаем все существующие SKU для проверки дубликатов
	var existingItems []models.NomenclatureItem
	ns.db.Where("deleted_at IS NULL").Select("sku").Find(&existingItems)
	existingSKUs := make(map[string]bool)
	for _, item := range existingItems {
		existingSKUs[item.SKU] = true
	}
	
	// Получаем все существующие категории
	var existingCategories []models.NomenclatureCategory
	ns.db.Where("deleted_at IS NULL").Select("name").Find(&existingCategories)
	existingCategoryNames := make(map[string]bool)
	for _, cat := range existingCategories {
		existingCategoryNames[cat.Name] = true
	}
	
	// Валидируем каждую строку
	for i, row := range items {
		result := models.ImportValidationResult{
			Row:      i + 1,
			Item:     make(map[string]interface{}),
			Status:   "success",
			Errors:   []string{},
			Warnings: []string{},
		}
		
		// Логирование первых 3 строк для отладки
		if i < 3 {
			log.Printf("🔍 ValidateImport Row %d: keys in row: %v", i+1, getMapKeys(row))
			log.Printf("🔍 ValidateImport Row %d: raw row values - name=%v (type: %T), sku=%v (type: %T), unit=%v (type: %T)", 
				i+1, row["name"], row["name"], row["sku"], row["sku"], row["unit"], row["unit"])
		}
		
		// Извлекаем данные напрямую из row (данные уже распарсены с системными ключами)
		// row содержит ключи: "name", "sku", "category", "unit", "price" (не имена колонок из файла)
		name := getStringValue(row, "name")
		sku := getStringValue(row, "sku")
		category := getStringValue(row, "category")
		// Пытаемся получить unit, если нет - используем base_unit или inbound_unit как fallback
		unit := getStringValue(row, "unit")
		if unit == "" {
			unit = getStringValue(row, "base_unit")
			if unit == "" {
				unit = getStringValue(row, "inbound_unit")
			}
		}
		price := getFloatValue(row, "price")
		
		// Логирование первых 3 строк для отладки
		if i < 3 {
			log.Printf("🔍 ValidateImport Row %d: after getStringValue - name='%s', sku='%s', unit='%s'", i+1, name, sku, unit)
		}
		
		result.Item["name"] = name
		result.Item["sku"] = sku
		result.Item["category"] = category
		result.Item["unit"] = unit
		result.Item["price"] = price
		
		// Валидация обязательных полей
		if name == "" {
			result.Errors = append(result.Errors, "Отсутствует название")
		}
		if sku == "" {
			// Просто отмечаем как ошибку - предложение SKU будет по запросу пользователя
			result.Errors = append(result.Errors, "Отсутствует SKU")
		}
		if unit == "" {
			result.Errors = append(result.Errors, "Отсутствует единица измерения")
		}
		
		// Нормализуем единицу измерения
		normalizedUnit := normalizeUnit(unit)
		if normalizedUnit != unit && unit != "" {
			// Обновляем единицу в данных
			result.Item["unit"] = normalizedUnit
			unit = normalizedUnit
		}
		
		// Проверка валидности единицы измерения
		validUnits := map[string]bool{
			"kg": true, "g": true, "l": true, "ml": true, "pcs": true, "box": true,
		}
		if unit != "" && !validUnits[strings.ToLower(unit)] {
			result.Warnings = append(result.Warnings, fmt.Sprintf("Неизвестная единица измерения: %s", unit))
		}
		
		// Проверка на дубликат SKU
		if sku != "" && existingSKUs[sku] {
			result.Warnings = append(result.Warnings, fmt.Sprintf("Дубликат SKU: товар с таким SKU уже существует"))
		}
		
		// Проверка категории
		if category != "" && !existingCategoryNames[category] {
			if autoCreateCategories {
				result.Warnings = append(result.Warnings, fmt.Sprintf("Категория '%s' не найдена, будет создана автоматически", category))
			} else {
				result.Warnings = append(result.Warnings, fmt.Sprintf("Категория '%s' не найдена", category))
			}
		}
		
		// Определяем статус
		if len(result.Errors) > 0 {
			result.Status = "error"
		} else if len(result.Warnings) > 0 {
			result.Status = "warning"
		}
		
		results = append(results, result)
	}
	
	return results
}

// ProcessImport выполняет массовый импорт товаров
func (ns *NomenclatureService) ProcessImport(items []map[string]interface{}, fieldMapping map[string]string, autoCreateCategories bool) (*models.ImportResult, error) {
	result := &models.ImportResult{
		ImportedCount: 0,
		ErrorCount:    0,
		WarningCount:  0,
		Errors:        []string{},
	}
	
	// Сначала валидируем
	validation := ns.ValidateImport(items, fieldMapping, autoCreateCategories)
	result.Validation = validation
	
	// Создаем категории, если нужно
	categoryCache := make(map[string]string) // name -> id
	if autoCreateCategories {
		for _, row := range items {
			// Данные уже распарсены с системными ключами
			categoryName := getStringValue(row, "category")
			if categoryName != "" {
				if _, exists := categoryCache[categoryName]; !exists {
					// Проверяем, существует ли категория
					var existing models.NomenclatureCategory
					if err := ns.db.Where("name = ? AND deleted_at IS NULL", categoryName).First(&existing).Error; err != nil {
						// Создаем новую категорию
						newCategory := &models.NomenclatureCategory{
							Name:           categoryName,
							Color:          "#10b981",
							AccountingType: "hybrid",
						}
						if err := ns.db.Create(newCategory).Error; err != nil {
							log.Printf("Ошибка создания категории %s: %v", categoryName, err)
							continue
						}
						categoryCache[categoryName] = newCategory.ID
					} else {
						categoryCache[categoryName] = existing.ID
					}
				}
			}
		}
	}
	
	// Подготавливаем данные для batch insert
	type itemData struct {
		ID               string
		SKU              string
		Name             string
		CategoryID       *string
		CategoryName     string
		CategoryColor    string
		BaseUnit         string
		InboundUnit      string
		ProductionUnit   string
		ConversionFactor float64
		MinStockLevel    float64
		StorageZone      string
		LastPrice        float64
		IsActive         bool
		CreatedAt        time.Time
		UpdatedAt        time.Time
		RowNum           int
	}
	
	itemsToInsert := make([]itemData, 0)
	
	// Подготавливаем данные для batch insert
	for i, row := range items {
		validationResult := validation[i]
		
		// Пропускаем строки с ошибками
		if validationResult.Status == "error" {
			result.ErrorCount++
			result.Errors = append(result.Errors, fmt.Sprintf("Строка %d: %s", validationResult.Row, strings.Join(validationResult.Errors, ", ")))
			continue
		}
		
		// Извлекаем данные напрямую из row (данные уже распарсены с системными ключами)
		name := getStringValue(row, "name")
		sku := getStringValue(row, "sku")
		categoryName := getStringValue(row, "category")
		// Пытаемся получить unit, если нет - используем base_unit или inbound_unit как fallback
		unit := getStringValue(row, "unit")
		if unit == "" {
			unit = getStringValue(row, "base_unit")
			if unit == "" {
				unit = getStringValue(row, "inbound_unit")
			}
		}
		price := getFloatValue(row, "price")
		
		// Пропускаем если нет обязательных полей
		if name == "" || sku == "" {
			result.ErrorCount++
			result.Errors = append(result.Errors, fmt.Sprintf("Строка %d: отсутствуют обязательные поля", validationResult.Row))
			continue
		}
		
		// Определяем category_id
		var categoryID *string
		var categoryColor string
		if categoryName != "" {
			if catID, exists := categoryCache[categoryName]; exists {
				categoryID = &catID
				// Получаем цвет категории
				var category models.NomenclatureCategory
				if err := ns.db.Where("id = ?", catID).First(&category).Error; err == nil {
					categoryColor = category.Color
				}
			}
		}
		
		// Генерируем UUID
		itemID := uuid.New().String()
		now := time.Now()
		
		itemsToInsert = append(itemsToInsert, itemData{
			ID:               itemID,
			SKU:              sku,
			Name:             name,
			CategoryID:       categoryID,
			CategoryName:     categoryName,
			CategoryColor:    categoryColor,
			BaseUnit:         unit,
			InboundUnit:      unit,
			ProductionUnit:   unit,
			ConversionFactor: 1.0,
			MinStockLevel:    0,
			StorageZone:      "dry_storage",
			LastPrice:        price,
			IsActive:         true,
			CreatedAt:        now,
			UpdatedAt:        now,
			RowNum:           validationResult.Row,
		})
	}
	
	if len(itemsToInsert) == 0 {
		return result, nil
	}
	
	// Дедуплицируем данные: если в батче есть дубликаты SKU, оставляем только последний
	// Это предотвращает ошибку "ON CONFLICT DO UPDATE command cannot affect row a second time"
	skuMap := make(map[string]int) // SKU -> индекс в itemsToInsert
	deduplicatedItems := make([]itemData, 0)
	
	deletedRows := make([]string, 0) // Список удаленных строк для отчета
	
	for _, item := range itemsToInsert {
		if existingIdx, exists := skuMap[item.SKU]; exists {
			// SKU уже встречался, заменяем на новое значение (берем последнее)
			oldItem := deduplicatedItems[existingIdx]
			log.Printf("⚠️ Дубликат SKU '%s' в батче:", item.SKU)
			log.Printf("   ❌ УДАЛЕНА строка %d: '%s' (SKU: %s)", oldItem.RowNum, oldItem.Name, oldItem.SKU)
			log.Printf("   ✅ ОСТАВЛЕНА строка %d: '%s' (SKU: %s)", item.RowNum, item.Name, item.SKU)
			deletedRows = append(deletedRows, fmt.Sprintf("Строка %d: '%s' (SKU: %s)", oldItem.RowNum, oldItem.Name, oldItem.SKU))
			deduplicatedItems[existingIdx] = item
		} else {
			// Новый SKU, добавляем
			skuMap[item.SKU] = len(deduplicatedItems)
			deduplicatedItems = append(deduplicatedItems, item)
		}
	}
	
	if len(deletedRows) > 0 {
		log.Printf("📋 УДАЛЕННЫЕ СТРОКИ из-за дубликатов SKU (%d шт.):", len(deletedRows))
		for _, deleted := range deletedRows {
			log.Printf("   - %s", deleted)
		}
	}
	
	log.Printf("📊 Дедупликация: было %d строк, стало %d уникальных SKU", len(itemsToInsert), len(deduplicatedItems))
	itemsToInsert = deduplicatedItems
	
	// Выполняем batch insert в транзакции
	err := ns.db.Transaction(func(tx *gorm.DB) error {
		// Используем batch insert с ON CONFLICT для обновления существующих товаров
		batchSize := 100 // Размер батча для оптимизации
		
		for i := 0; i < len(itemsToInsert); i += batchSize {
			end := i + batchSize
			if end > len(itemsToInsert) {
				end = len(itemsToInsert)
			}
			
			batch := itemsToInsert[i:end]
			
			// Дополнительная проверка на дубликаты внутри батча (на всякий случай)
			batchSKUs := make(map[string]bool)
			uniqueBatch := make([]itemData, 0)
			for _, item := range batch {
				if !batchSKUs[item.SKU] {
					batchSKUs[item.SKU] = true
					uniqueBatch = append(uniqueBatch, item)
				} else {
					log.Printf("⚠️ Дубликат SKU '%s' внутри батча, пропускаем", item.SKU)
				}
			}
			batch = uniqueBatch
			
			// Строим SQL запрос для batch insert с ON CONFLICT
			query := `
				INSERT INTO nomenclature_items (
					id, sku, name, category_id,
					base_unit, inbound_unit, production_unit, conversion_factor,
					min_stock_level, storage_zone, last_price, is_active,
					created_at, updated_at
				) VALUES `
			
			args := make([]interface{}, 0)
			placeholders := make([]string, 0)
			argIndex := 1
			
			for _, item := range batch {
				ph := fmt.Sprintf("($%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d)",
					argIndex, argIndex+1, argIndex+2, argIndex+3,
					argIndex+4, argIndex+5, argIndex+6, argIndex+7, argIndex+8, argIndex+9,
					argIndex+10, argIndex+11, argIndex+12, argIndex+13)
				placeholders = append(placeholders, ph)
				
				args = append(args,
					item.ID, item.SKU, item.Name, item.CategoryID,
					item.BaseUnit, item.InboundUnit, item.ProductionUnit, item.ConversionFactor,
					item.MinStockLevel, item.StorageZone, item.LastPrice, item.IsActive,
					item.CreatedAt, item.UpdatedAt)
				argIndex += 14
			}
			
			query += strings.Join(placeholders, ", ")
			query += `
				ON CONFLICT (sku) 
				DO UPDATE SET
					name = EXCLUDED.name,
					category_id = EXCLUDED.category_id,
					last_price = EXCLUDED.last_price,
					updated_at = EXCLUDED.updated_at
				WHERE nomenclature_items.deleted_at IS NULL`
			
			// Выполняем batch insert
			if err := tx.Exec(query, args...).Error; err != nil {
				log.Printf("❌ Ошибка batch insert (строки %d-%d): %v", i+1, end, err)
				// Помечаем все строки батча как ошибки
				for _, item := range batch {
					result.ErrorCount++
					result.Errors = append(result.Errors, fmt.Sprintf("Строка %d: ошибка batch insert: %v", item.RowNum, err))
				}
				return err
			}
			
			// Подсчитываем успешно импортированные (все строки батча)
			// ON CONFLICT DO UPDATE означает, что товар либо создан, либо обновлен
			for _, item := range batch {
				// Проверяем, был ли это дубликат (из валидации)
				hasWarning := false
				for _, v := range validation {
					if v.Row == item.RowNum && len(v.Warnings) > 0 {
						for _, warning := range v.Warnings {
							if strings.Contains(warning, "Дубликат SKU") {
								hasWarning = true
								break
							}
						}
					}
				}
				
				if hasWarning {
					result.WarningCount++
				} else {
					result.ImportedCount++
				}
			}
		}
		
		return nil
	})
	
	if err != nil {
		log.Printf("❌ Ошибка транзакции импорта: %v", err)
		return result, fmt.Errorf("ошибка импорта: %w", err)
	}
	
	return result, nil
}

// GetAllCategories возвращает все категории
func (ns *NomenclatureService) GetAllCategories() ([]models.NomenclatureCategory, error) {
	var categories []models.NomenclatureCategory
	if err := ns.db.Where("deleted_at IS NULL").Find(&categories).Error; err != nil {
		return nil, err
	}
	return categories, nil
}

// CreateCategory создает новую категорию
func (ns *NomenclatureService) CreateCategory(category *models.NomenclatureCategory) error {
	// Проверка на дубликат имени
	var existing models.NomenclatureCategory
	if err := ns.db.Where("name = ? AND deleted_at IS NULL", category.Name).First(&existing).Error; err == nil {
		return fmt.Errorf("категория с именем '%s' уже существует", category.Name)
	}
	return ns.db.Create(category).Error
}

// UpdateCategory обновляет категорию
func (ns *NomenclatureService) UpdateCategory(id string, category *models.NomenclatureCategory) error {
	var existing models.NomenclatureCategory
	if err := ns.db.Where("id = ? AND deleted_at IS NULL", id).First(&existing).Error; err != nil {
		return fmt.Errorf("категория не найдена")
	}
	
	// Проверка на дубликат имени (если имя изменилось)
	if category.Name != existing.Name {
		var duplicate models.NomenclatureCategory
		if err := ns.db.Where("name = ? AND id != ? AND deleted_at IS NULL", category.Name, id).First(&duplicate).Error; err == nil {
			return fmt.Errorf("категория с именем '%s' уже существует", category.Name)
		}
	}
	
	category.ID = id
	return ns.db.Model(&existing).Updates(category).Error
}

// GetCategoryByID возвращает категорию по ID
func (ns *NomenclatureService) GetCategoryByID(id string) (*models.NomenclatureCategory, error) {
	var category models.NomenclatureCategory
	if err := ns.db.Where("id = ? AND deleted_at IS NULL", id).First(&category).Error; err != nil {
		return nil, err
	}
	return &category, nil
}

// DeleteCategory удаляет категорию (soft delete)
func (ns *NomenclatureService) DeleteCategory(id string) error {
	return ns.db.Where("id = ?", id).Delete(&models.NomenclatureCategory{}).Error
}

// Helper functions
func getStringValue(row map[string]interface{}, key string) string {
	if key == "" {
		return ""
	}
	if val, ok := row[key]; ok {
		if str, ok := val.(string); ok {
			return strings.TrimSpace(str)
		}
		return fmt.Sprintf("%v", val)
	}
	return ""
}

// getMapKeys возвращает список ключей из map для отладки
func getMapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func getFloatValue(row map[string]interface{}, key string) float64 {
	if key == "" {
		return 0
	}
	if val, ok := row[key]; ok {
		switch v := val.(type) {
		case float64:
			return v
		case int:
			return float64(v)
		case string:
			var f float64
			fmt.Sscanf(v, "%f", &f)
			return f
		}
	}
	return 0
}

// ParseUploadedFile парсит загруженный файл (CSV или XLSX) и возвращает массив строк
func (ns *NomenclatureService) ParseUploadedFile(file multipart.File, filename string) ([]map[string]interface{}, error) {
	if strings.HasSuffix(strings.ToLower(filename), ".csv") {
		return ns.parseCSVFile(file)
	} else if strings.HasSuffix(strings.ToLower(filename), ".xlsx") || strings.HasSuffix(strings.ToLower(filename), ".xls") {
		return ns.parseXLSXFile(file)
	}
	return nil, fmt.Errorf("неподдерживаемый формат файла: %s. Используйте .csv или .xlsx", filename)
}

// DetectFileHeaders определяет заголовки файла и возвращает информацию о структуре
// Возвращает: headerRowIndex, columnNames, sampleRows
func (ns *NomenclatureService) DetectFileHeaders(file multipart.File, filename string) (int, []string, [][]string, error) {
	if strings.HasSuffix(strings.ToLower(filename), ".csv") {
		return ns.detectCSVHeaders(file)
	} else if strings.HasSuffix(strings.ToLower(filename), ".xlsx") || strings.HasSuffix(strings.ToLower(filename), ".xls") {
		return ns.detectXLSXHeaders(file)
	}
	return 0, nil, nil, fmt.Errorf("неподдерживаемый формат файла: %s", filename)
}

// ParseFileWithMapping парсит файл используя маппинг колонок
// columnMapping: map[systemField]fileColumnName (например: {"name": "Наименование", "sku": "Артикул"})
// columns: список колонок из первого этапа (опционально, для точного соответствия)
// headerRowIndex: индекс строки заголовков (опционально)
func (ns *NomenclatureService) ParseFileWithMapping(file multipart.File, filename string, columnMapping map[string]string, columns []string, headerRowIndex int) ([]map[string]interface{}, error) {
	if strings.HasSuffix(strings.ToLower(filename), ".csv") {
		return ns.parseCSVWithMapping(file, columnMapping, columns)
	} else if strings.HasSuffix(strings.ToLower(filename), ".xlsx") || strings.HasSuffix(strings.ToLower(filename), ".xls") {
		return ns.parseXLSXWithMapping(file, columnMapping, columns, headerRowIndex)
	}
	return nil, fmt.Errorf("неподдерживаемый формат файла: %s", filename)
}

// parseCSVFile парсит CSV файл с автоматическим определением разделителя и кодировки
func (ns *NomenclatureService) parseCSVFile(file multipart.File) ([]map[string]interface{}, error) {
	// Читаем весь файл в память для определения кодировки и разделителя
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("ошибка чтения файла: %w", err)
	}
	
	// Определяем кодировку и конвертируем в UTF-8
	var utf8Data []byte
	if !utf8.Valid(data) {
		// Пробуем Windows-1251
		decoder := charmap.Windows1251.NewDecoder()
		utf8Data, _, err = transform.Bytes(decoder, data)
		if err != nil {
			// Если не получилось, используем исходные данные
			utf8Data = data
		}
	} else {
		utf8Data = data
	}
	
	// Определяем разделитель (запятая, точка с запятой, табуляция)
	delimiter := detectDelimiter(utf8Data)
	
	reader := csv.NewReader(bytes.NewReader(utf8Data))
	reader.Comma = delimiter
	reader.LazyQuotes = true
	reader.TrimLeadingSpace = true
	
	// Читаем заголовки
	headers, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("ошибка чтения заголовков CSV: %w", err)
	}
	
	// Очищаем заголовки от пробелов и кавычек
	for i, h := range headers {
		headers[i] = strings.TrimSpace(strings.Trim(h, "\"'\t"))
	}
	
	var rows []map[string]interface{}
	rowNum := 1
	
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			// Пропускаем строки с ошибками, но логируем
			log.Printf("⚠️ Ошибка чтения строки %d: %v, пропускаем", rowNum, err)
			rowNum++
			continue
		}
		
		// Создаем map для строки
		row := make(map[string]interface{})
		hasData := false
		
		for i, value := range record {
			cleanedValue := strings.TrimSpace(strings.Trim(value, "\"'\t"))
			if i < len(headers) && headers[i] != "" {
				row[headers[i]] = cleanedValue
				if cleanedValue != "" {
					hasData = true
				}
			}
		}
		
		// Пропускаем полностью пустые строки
		if hasData {
			rows = append(rows, row)
		}
		rowNum++
	}
	
	return rows, nil
}

// detectDelimiter определяет разделитель CSV файла
func detectDelimiter(data []byte) rune {
	// Берем первые 1000 байт для анализа
	sample := string(data)
	if len(sample) > 1000 {
		sample = sample[:1000]
	}
	
	// Подсчитываем количество каждого возможного разделителя
	commaCount := strings.Count(sample, ",")
	semicolonCount := strings.Count(sample, ";")
	tabCount := strings.Count(sample, "\t")
	pipeCount := strings.Count(sample, "|")
	
	// Выбираем наиболее частый разделитель
	maxCount := commaCount
	delimiter := ','
	
	if semicolonCount > maxCount {
		maxCount = semicolonCount
		delimiter = ';'
	}
	if tabCount > maxCount {
		maxCount = tabCount
		delimiter = '\t'
	}
	if pipeCount > maxCount {
		delimiter = '|'
	}
	
	return delimiter
}

// detectCSVHeaders определяет заголовки CSV файла
func (ns *NomenclatureService) detectCSVHeaders(file multipart.File) (int, []string, [][]string, error) {
	// Читаем весь файл
	data, err := io.ReadAll(file)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("ошибка чтения файла: %w", err)
	}
	
	// Определяем кодировку
	var utf8Data []byte
	if !utf8.Valid(data) {
		decoder := charmap.Windows1251.NewDecoder()
		utf8Data, _, err = transform.Bytes(decoder, data)
		if err != nil {
			utf8Data = data
		}
	} else {
		utf8Data = data
	}
	
	delimiter := detectDelimiter(utf8Data)
	reader := csv.NewReader(bytes.NewReader(utf8Data))
	reader.Comma = delimiter
	reader.LazyQuotes = true
	reader.TrimLeadingSpace = true
	
	// Читаем первые 10 строк
	var allRows [][]string
	for i := 0; i < 10; i++ {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			continue
		}
		allRows = append(allRows, row)
	}
	
	if len(allRows) == 0 {
		return 0, nil, nil, fmt.Errorf("файл пуст")
	}
	
	// Ищем строку с заголовками по ключевым словам
	headerKeywords := []string{"наименование", "товар", "name", "sku", "артикул", "секция", "категория", "category", "единица", "unit", "цена", "price"}
	headerRowIndex := 0
	maxMatches := 0
	
	for i, row := range allRows {
		matches := 0
		for _, cell := range row {
			cellLower := strings.ToLower(strings.TrimSpace(cell))
			for _, keyword := range headerKeywords {
				if strings.Contains(cellLower, keyword) {
					matches++
					break
				}
			}
		}
		if matches > maxMatches {
			maxMatches = matches
			headerRowIndex = i
		}
	}
	
	// Очищаем заголовки
	headers := allRows[headerRowIndex]
	columnNames := make([]string, len(headers))
	for i, h := range headers {
		columnNames[i] = strings.TrimSpace(strings.Trim(h, "\"'\t"))
	}
	
	// Возвращаем несколько примеров строк для предпросмотра
	sampleRows := allRows[headerRowIndex+1:]
	if len(sampleRows) > 5 {
		sampleRows = sampleRows[:5]
	}
	
	return headerRowIndex, columnNames, sampleRows, nil
}

// detectXLSXHeaders определяет заголовки XLSX файла
func (ns *NomenclatureService) detectXLSXHeaders(file multipart.File) (int, []string, [][]string, error) {
	data, err := io.ReadAll(file)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("ошибка чтения файла: %w", err)
	}
	
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return 0, nil, nil, fmt.Errorf("ошибка открытия XLSX файла: %w", err)
	}
	defer f.Close()
	
	sheetName := f.GetSheetName(0)
	if sheetName == "" {
		return 0, nil, nil, fmt.Errorf("файл не содержит листов")
	}
	
	// Читаем первые 10 строк
	rows, err := f.GetRows(sheetName)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("ошибка чтения листа: %w", err)
	}
	
	if len(rows) == 0 {
		return 0, nil, nil, fmt.Errorf("файл пуст")
	}
	
	// Ограничиваем до 10 строк для поиска заголовков
	maxRows := 10
	if len(rows) < maxRows {
		maxRows = len(rows)
	}
	
	// Ищем строку с заголовками
	headerKeywords := []string{"наименование", "товар", "name", "sku", "артикул", "секция", "категория", "category", "единица", "unit", "цена", "price"}
	headerRowIndex := 0
	maxMatches := 0
	
	for i := 0; i < maxRows; i++ {
		matches := 0
		for _, cell := range rows[i] {
			cellLower := strings.ToLower(strings.TrimSpace(cell))
			for _, keyword := range headerKeywords {
				if strings.Contains(cellLower, keyword) {
					matches++
					break
				}
			}
		}
		if matches > maxMatches {
			maxMatches = matches
			headerRowIndex = i
		}
	}
	
	// Очищаем заголовки
	headers := rows[headerRowIndex]
	columnNames := make([]string, 0)
	for _, h := range headers {
		cleaned := strings.TrimSpace(strings.Trim(h, "\"'\t"))
		columnNames = append(columnNames, cleaned)
	}
	
	// Примеры строк
	sampleRows := make([][]string, 0)
	for i := headerRowIndex + 1; i < len(rows) && i < headerRowIndex+6; i++ {
		sampleRows = append(sampleRows, rows[i])
	}
	
	return headerRowIndex, columnNames, sampleRows, nil
}

// parseCSVWithMapping парсит CSV с использованием маппинга колонок
func (ns *NomenclatureService) parseCSVWithMapping(file multipart.File, columnMapping map[string]string, knownColumns []string) ([]map[string]interface{}, error) {
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("ошибка чтения файла: %w", err)
	}
	
	var utf8Data []byte
	if !utf8.Valid(data) {
		decoder := charmap.Windows1251.NewDecoder()
		utf8Data, _, err = transform.Bytes(decoder, data)
		if err != nil {
			utf8Data = data
		}
	} else {
		utf8Data = data
	}
	
	delimiter := detectDelimiter(utf8Data)
	reader := csv.NewReader(bytes.NewReader(utf8Data))
	reader.Comma = delimiter
	reader.LazyQuotes = true
	reader.TrimLeadingSpace = true
	
	// Читаем заголовки
	headers, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("ошибка чтения заголовков: %w", err)
	}
	
	// Создаем индекс колонок (используем известные колонки если они переданы)
	columnIndex := make(map[string]int)
	if len(knownColumns) > 0 {
		// ВСЕГДА используем известные колонки из первого этапа для точного соответствия
		log.Printf("📋 CSV: Используем известные колонки из первого этапа (%d колонок): %v", len(knownColumns), knownColumns)
		for i, colName := range knownColumns {
			columnIndex[colName] = i
		}
	} else {
		// Используем заголовки из файла (fallback)
		log.Printf("📋 CSV: Используем заголовки из файла: %v", headers)
		for i, h := range headers {
			cleaned := strings.TrimSpace(strings.Trim(h, "\"'\t"))
			if cleaned != "" {
				columnIndex[cleaned] = i
			}
		}
	}
	
	log.Printf("📋 CSV: Создан индекс колонок: %v", columnIndex)
	
	// Создаем маппинг индексов
	fieldToIndex := make(map[string]int)
	for field, columnName := range columnMapping {
		if columnName != "" {
			if idx, ok := columnIndex[columnName]; ok {
				fieldToIndex[field] = idx
				log.Printf("✅ CSV Маппинг: поле '%s' -> колонка '%s' -> индекс %d", field, columnName, idx)
			} else {
				log.Printf("⚠️ CSV: Колонка '%s' не найдена. Доступные колонки: %v", columnName, columnIndex)
			}
		}
	}
	
	if len(fieldToIndex) == 0 {
		return nil, fmt.Errorf("не удалось создать маппинг: ни одна колонка не найдена. Маппинг: %v, Доступные колонки: %v", columnMapping, columnIndex)
	}
	
	var rows []map[string]interface{}
	rowNum := 1
	
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Printf("⚠️ Ошибка чтения строки %d: %v, пропускаем", rowNum, err)
			rowNum++
			continue
		}
		
		row := make(map[string]interface{})
		hasData := false
		
		// Заполняем данные по маппингу
		for field, idx := range fieldToIndex {
			if idx < len(record) {
				value := strings.TrimSpace(strings.Trim(record[idx], "\"'\t"))
				// Нормализуем единицу измерения если это поле "unit"
				if field == "unit" {
					value = normalizeUnit(value)
				}
				row[field] = value
				if value != "" {
					hasData = true
				}
			}
		}
		
		// Всегда создаем поле "unit" на основе base_unit или inbound_unit если оно не было заполнено
		if unitVal, hasUnit := row["unit"]; !hasUnit || unitVal == "" || unitVal == nil {
			if baseUnit, ok := row["base_unit"].(string); ok && baseUnit != "" {
				row["unit"] = normalizeUnit(baseUnit)
			} else if inboundUnit, ok := row["inbound_unit"].(string); ok && inboundUnit != "" {
				row["unit"] = normalizeUnit(inboundUnit)
			} else {
				// Если ничего не найдено, устанавливаем пустую строку
				row["unit"] = ""
			}
		}
		
		// Пропускаем пустые строки или строки без обязательных полей
		if hasData {
			// Проверяем наличие обязательного поля "name"
			if nameVal, ok := row["name"]; ok {
				if nameStr, ok := nameVal.(string); ok && strings.TrimSpace(nameStr) != "" {
					rows = append(rows, row)
				}
			}
		}
		rowNum++
	}
	
	return rows, nil
}

// parseXLSXWithMapping парсит XLSX с использованием маппинга колонок
func (ns *NomenclatureService) parseXLSXWithMapping(file multipart.File, columnMapping map[string]string, knownColumns []string, headerRowIndex int) ([]map[string]interface{}, error) {
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("ошибка чтения файла: %w", err)
	}
	
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("ошибка открытия XLSX файла: %w", err)
	}
	defer f.Close()
	
	sheetName := f.GetSheetName(0)
	rows, err := f.GetRows(sheetName)
	if err != nil {
		return nil, fmt.Errorf("ошибка чтения листа: %w", err)
	}
	
	if len(rows) == 0 {
		return nil, fmt.Errorf("файл пуст")
	}
	
	// Определяем индекс строки заголовков
	detectedHeaderRowIndex := headerRowIndex
	if headerRowIndex < 0 || headerRowIndex >= len(rows) {
		// Если индекс не передан или неверный, ищем заголовки
		headerKeywords := []string{"наименование", "товар", "name", "sku", "артикул", "секция", "категория", "category", "единица", "unit", "цена", "price"}
		maxMatches := 0
		for i := 0; i < len(rows) && i < 10; i++ {
			matches := 0
			for _, cell := range rows[i] {
				cellLower := strings.ToLower(strings.TrimSpace(cell))
				for _, keyword := range headerKeywords {
					if strings.Contains(cellLower, keyword) {
						matches++
						break
					}
				}
			}
			if matches > maxMatches {
				maxMatches = matches
				detectedHeaderRowIndex = i
			}
		}
	}
	
	// Создаем индекс колонок (используем известные колонки если они переданы)
	headers := rows[detectedHeaderRowIndex]
	columnIndex := make(map[string]int)
	
	if len(knownColumns) > 0 {
		// ВСЕГДА используем известные колонки из первого этапа для точного соответствия
		log.Printf("📋 XLSX: Используем известные колонки из первого этапа (%d колонок): %v", len(knownColumns), knownColumns)
		for i, colName := range knownColumns {
			columnIndex[colName] = i
		}
	} else {
		// Используем заголовки из файла (fallback)
		log.Printf("📋 XLSX: Используем заголовки из файла (строка %d): %v", detectedHeaderRowIndex, headers)
		for i, h := range headers {
			cleaned := strings.TrimSpace(strings.Trim(h, "\"'\t"))
			if cleaned != "" {
				columnIndex[cleaned] = i
			}
		}
	}
	
	log.Printf("📋 XLSX: Создан индекс колонок: %v", columnIndex)
	
	// Создаем маппинг индексов
	fieldToIndex := make(map[string]int)
	for field, columnName := range columnMapping {
		if columnName != "" {
			if idx, ok := columnIndex[columnName]; ok {
				fieldToIndex[field] = idx
				log.Printf("✅ XLSX Маппинг: поле '%s' -> колонка '%s' -> индекс %d", field, columnName, idx)
			} else {
				log.Printf("⚠️ XLSX: Колонка '%s' не найдена. Доступные колонки: %v", columnName, columnIndex)
			}
		}
	}
	
	if len(fieldToIndex) == 0 {
		return nil, fmt.Errorf("не удалось создать маппинг: ни одна колонка не найдена. Маппинг: %v, Доступные колонки: %v", columnMapping, columnIndex)
	}
	
	// Находим максимальный индекс для проверки границ
	maxIndex := -1
	for _, idx := range fieldToIndex {
		if idx > maxIndex {
			maxIndex = idx
		}
	}
	
	// Получаем индекс для обязательного поля "name"
	nameIndex, hasNameField := fieldToIndex["name"]
	if !hasNameField {
		return nil, fmt.Errorf("обязательное поле 'name' отсутствует в маппинге")
	}
	
	// Логирование начала парсинга данных
	log.Printf("Starting data extraction from row %d", detectedHeaderRowIndex+1)
	
	// Создаем контекст с таймаутом для предотвращения блокировки
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	
	result := make([]map[string]interface{}, 0)
	rowsProcessed := 0
	
	// Динамический offset: начинаем строго с detectedHeaderRowIndex + 1
	// Используем range для итерации по всем строкам, но пропускаем заголовки
	for i, currentRow := range rows {
		// Динамический пропуск шапки: пропускаем все строки до и включая detectedHeaderRowIndex
		if i <= detectedHeaderRowIndex {
			continue
		}
		
		// Проверяем контекст на таймаут
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("таймаут парсинга файла: %w", ctx.Err())
		default:
		}
		
		// Boundary Protection: проверяем границы перед доступом к ячейкам
		if len(currentRow) <= maxIndex {
			// Пропускаем строки с недостаточным количеством колонок
			continue
		}
		
		// Boundary Protection: проверяем наличие колонки "name" перед доступом
		if nameIndex >= len(currentRow) {
			// Пропускаем строки без колонки "name"
			continue
		}
		
		// Empty Name Guard: проверяем, не пустое ли поле "name"
		nameValue := strings.TrimSpace(strings.Trim(currentRow[nameIndex], "\"'\t"))
		if nameValue == "" {
			// Пропускаем пустые строки без генерации ошибки
			continue
		}
		
		// Заполняем данные по маппингу с защитой от выхода за границы
		row := make(map[string]interface{})
		for field, idx := range fieldToIndex {
			// Boundary Protection: всегда проверяем границы перед доступом
			if idx < len(currentRow) {
				// Санитизация: trim пробелов для всех значений
				value := strings.TrimSpace(strings.Trim(currentRow[idx], "\"'\t"))
				// Нормализуем единицу измерения если это поле "unit"
				if field == "unit" {
					value = normalizeUnit(value)
				}
				row[field] = value
			} else {
				// Если колонка отсутствует, устанавливаем пустое значение
				row[field] = ""
			}
		}
		
		// Всегда создаем поле "unit" на основе base_unit или inbound_unit если оно не было заполнено
		if unitVal, hasUnit := row["unit"]; !hasUnit || unitVal == "" || unitVal == nil {
			if baseUnit, ok := row["base_unit"].(string); ok && baseUnit != "" {
				row["unit"] = normalizeUnit(baseUnit)
			} else if inboundUnit, ok := row["inbound_unit"].(string); ok && inboundUnit != "" {
				row["unit"] = normalizeUnit(inboundUnit)
			} else {
				// Если ничего не найдено, устанавливаем пустую строку
				row["unit"] = ""
			}
		} else {
			// Если unit уже есть, нормализуем его
			if unitStr, ok := unitVal.(string); ok {
				row["unit"] = normalizeUnit(unitStr)
			}
		}
		
		// Логирование первых 3 строк для отладки (до добавления в результат)
		if rowsProcessed < 3 {
			log.Printf("🔍 Parser Row %d (before append): row map = %v", i+1, row)
			log.Printf("🔍 Parser Row %d: name='%v', sku='%v', unit='%v'", i+1, row["name"], row["sku"], row["unit"])
		}
		
		// Добавляем строку в результат
		result = append(result, row)
		rowsProcessed++
		
		// Логирование первых 3 строк для отладки
		if rowsProcessed <= 3 {
			skuValue := ""
			if skuIdx, ok := fieldToIndex["sku"]; ok && skuIdx < len(currentRow) {
				skuValue = strings.TrimSpace(strings.Trim(currentRow[skuIdx], "\"'\t"))
			}
			log.Printf("Reading Row %d: Name=%s, SKU=%s", i+1, nameValue, skuValue)
		}
	}
	
	log.Printf("✅ XLSX: Распарсено %d строк из %d (начиная со строки %d)", len(result), len(rows)-detectedHeaderRowIndex-1, detectedHeaderRowIndex+2)
	
	return result, nil
}

// parseXLSXFile парсит XLSX файл
func (ns *NomenclatureService) parseXLSXFile(file multipart.File) ([]map[string]interface{}, error) {
	// Читаем весь файл в память
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("ошибка чтения файла: %w", err)
	}
	
	// Excelize работает с bytes.Reader
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("ошибка открытия XLSX файла: %w. Убедитесь, что файл не поврежден", err)
	}
	defer f.Close()
	
	// Получаем имя первого листа
	sheetName := f.GetSheetName(0)
	if sheetName == "" {
		return nil, fmt.Errorf("файл не содержит листов")
	}
	
	// Читаем все строки с первого листа
	rows, err := f.GetRows(sheetName)
	if err != nil {
		return nil, fmt.Errorf("ошибка чтения листа: %w", err)
	}
	
	if len(rows) == 0 {
		return nil, fmt.Errorf("файл пуст")
	}
	
	// Находим строку с заголовками (может быть не первая, если есть пустые строки)
	headerRowIndex := 0
	for i, row := range rows {
		if len(row) > 0 {
			hasNonEmpty := false
			for _, cell := range row {
				if strings.TrimSpace(cell) != "" {
					hasNonEmpty = true
					break
				}
			}
			if hasNonEmpty {
				headerRowIndex = i
				break
			}
		}
	}
	
	// Первая непустая строка - заголовки
	headers := rows[headerRowIndex]
	for i, h := range headers {
		headers[i] = strings.TrimSpace(strings.Trim(h, "\"'\t"))
	}
	
	// Парсим остальные строки
	result := make([]map[string]interface{}, 0)
	
	for i := headerRowIndex + 1; i < len(rows); i++ {
		row := make(map[string]interface{})
		hasData := false
		
		for j, value := range rows[i] {
			cleanedValue := strings.TrimSpace(strings.Trim(value, "\"'\t"))
			if j < len(headers) && headers[j] != "" {
				row[headers[j]] = cleanedValue
				if cleanedValue != "" {
					hasData = true
				}
			}
		}
		
		// Пропускаем полностью пустые строки
		if hasData {
			// Нормализуем единицу измерения если она есть
			if unit, ok := row["unit"].(string); ok && unit != "" {
				row["unit"] = normalizeUnit(unit)
			}
			result = append(result, row)
		}
	}
	
	return result, nil
}

// normalizeUnit нормализует единицу измерения (приводит к стандартному формату)
func normalizeUnit(unit string) string {
	if unit == "" {
		return unit
	}
	
	unitLower := strings.ToLower(strings.TrimSpace(unit))
	
	// Маппинг русских единиц на стандартные
	unitMap := map[string]string{
		"гр.":        "g",
		"г":          "g",
		"грамм":      "g",
		"граммы":     "g",
		"кг.":        "kg",
		"кг":         "kg",
		"килограмм":  "kg",
		"килограммы": "kg",
		"л.":         "l",
		"л":          "l",
		"литр":       "l",
		"литры":      "l",
		"литров":     "l",
		"мл.":        "ml",
		"мл":         "ml",
		"миллилитр":  "ml",
		"миллилитры": "ml",
		"шт.":        "pcs",
		"шт":         "pcs",
		"штука":      "pcs",
		"штуки":      "pcs",
		"штук":       "pcs",
		"упак.":      "box",
		"упак":       "box",
		"упаковка":   "box",
		"упаковки":   "box",
	}
	
	// Убираем скобки и точки
	unitLower = strings.Trim(unitLower, "()[]")
	unitLower = strings.TrimSuffix(unitLower, ".")
	
	// Проверяем маппинг
	if normalized, exists := unitMap[unitLower]; exists {
		return normalized
	}
	
	// Если уже стандартная единица, возвращаем как есть
	validUnits := map[string]bool{
		"kg": true, "g": true, "l": true, "ml": true, "pcs": true, "box": true,
	}
	if validUnits[unitLower] {
		return unitLower
	}
	
	// Если не удалось нормализовать, возвращаем исходное значение
	return unit
}

