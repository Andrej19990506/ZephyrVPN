package services

import (
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"zephyrvpn/server/internal/models"

	"gorm.io/gorm"
)

// ProcurementCatalogService управляет каталогом поставщиков
type ProcurementCatalogService struct {
	db *gorm.DB
}

// calculateConversionFactorFromUnits вычисляет коэффициент конвертации на основе единиц измерения
// Используется для проверки конфликтов при синхронизации из каталога поставщиков
func (s *ProcurementCatalogService) calculateConversionFactorFromUnits(inboundUnit, baseUnit string) float64 {
	inboundUnitNormalized := strings.ToLower(strings.TrimSpace(inboundUnit))
	baseUnitNormalized := strings.ToLower(strings.TrimSpace(baseUnit))
	
	// Стандартные конвертации
	if (inboundUnitNormalized == "кг" || inboundUnitNormalized == "kg") && 
	   (baseUnitNormalized == "г" || baseUnitNormalized == "g") {
		return 1000 // 1 кг = 1000 г
	}
	if (inboundUnitNormalized == "л" || inboundUnitNormalized == "l") && 
	   (baseUnitNormalized == "мл" || baseUnitNormalized == "ml") {
		return 1000 // 1 л = 1000 мл
	}
	if (inboundUnitNormalized == "г" || inboundUnitNormalized == "g") && 
	   (baseUnitNormalized == "кг" || baseUnitNormalized == "kg") {
		return 0.001 // 1 г = 0.001 кг (обратная конвертация)
	}
	if (inboundUnitNormalized == "мл" || inboundUnitNormalized == "ml") && 
	   (baseUnitNormalized == "л" || baseUnitNormalized == "l") {
		return 0.001 // 1 мл = 0.001 л (обратная конвертация)
	}
	
	// Если единицы совпадают, коэффициент = 1
	if inboundUnitNormalized == baseUnitNormalized {
		return 1
	}
	
	// Если единицы не совпадают, но нет стандартной конвертации, возвращаем 0
	return 0
}

// validateAndFixUnitSettingsFromCatalog валидирует и исправляет конфликты единиц измерения
// при синхронизации из каталога поставщиков
// КРИТИЧЕСКИ ВАЖНО: BaseUnit должен быть минимальной единицей (г/мл), а не крупной (кг/л)
func (s *ProcurementCatalogService) validateAndFixUnitSettingsFromCatalog(item *models.NomenclatureItem) {
	baseUnitNormalized := strings.ToLower(strings.TrimSpace(item.BaseUnit))
	inboundUnitNormalized := strings.ToLower(strings.TrimSpace(item.InboundUnit))
	
	// Исправляем BaseUnit: если установлен "кг" или "л", меняем на "г" или "мл"
	if baseUnitNormalized == "кг" || baseUnitNormalized == "kg" {
		if inboundUnitNormalized == "кг" || inboundUnitNormalized == "kg" {
			// Если и BaseUnit и InboundUnit = "кг", исправляем BaseUnit на "г"
			oldBaseUnit := item.BaseUnit
			item.BaseUnit = "g"
			log.Printf("⚠️ ВНИМАНИЕ: Исправлен BaseUnit из каталога для товара '%s': '%s' -> 'g' (для правильной работы формул расчета стоимости)",
				item.Name, oldBaseUnit)
		}
	} else if baseUnitNormalized == "л" || baseUnitNormalized == "l" {
		if inboundUnitNormalized == "л" || inboundUnitNormalized == "l" {
			// Если и BaseUnit и InboundUnit = "л", исправляем BaseUnit на "мл"
			oldBaseUnit := item.BaseUnit
			item.BaseUnit = "ml"
			log.Printf("⚠️ ВНИМАНИЕ: Исправлен BaseUnit из каталога для товара '%s': '%s' -> 'ml' (для правильной работы формул расчета стоимости)",
				item.Name, oldBaseUnit)
		}
	}
}

// NewProcurementCatalogService создает новый экземпляр ProcurementCatalogService
func NewProcurementCatalogService(db *gorm.DB) *ProcurementCatalogService {
	return &ProcurementCatalogService{
		db: db,
	}
}

// GetCatalogItemPrice возвращает цену товара из каталога поставщиков
// по nomenclature_id и counterparty_id (supplier_id)
func (s *ProcurementCatalogService) GetCatalogItemPrice(nomenclatureID string, counterpartyID string, branchID string) (float64, bool, error) {
	var catalogItem models.SupplierCatalogItem
	query := s.db.Model(&models.SupplierCatalogItem{}).
		Where("nomenclature_id = ? AND supplier_id = ? AND deleted_at IS NULL", nomenclatureID, counterpartyID)
	
	// Если указан branch_id, ищем товар для конкретного филиала или общий
	if branchID != "" {
		query = query.Where("branch_id = ? OR branch_id IS NULL", branchID)
	} else {
		query = query.Where("branch_id IS NULL")
	}
	
	if err := query.First(&catalogItem).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return 0, false, nil // Товар не найден в каталоге
		}
		return 0, false, fmt.Errorf("ошибка поиска товара в каталоге: %w", err)
	}
	
	return catalogItem.Price, true, nil
}

// CatalogTemplateResponse представляет структуру каталога для UI
type CatalogTemplateResponse struct {
	Categories []CatalogCategory `json:"categories"` // Группировка по категориям
}

// CatalogCategory представляет категорию с товарами
type CatalogCategory struct {
	CategoryID    string           `json:"category_id"`
	CategoryName string           `json:"category_name"`
	CategoryColor string          `json:"category_color"`
	Items        []CatalogItem    `json:"items"` // Товары в этой категории
}

// CatalogItem представляет товар в каталоге (для UI)
type CatalogItem struct {
	// Идентификаторы
	ID             string  `json:"id"`              // ID из supplier_catalog_items (если существует)
	NomenclatureID *string `json:"nomenclature_id"` // ID товара из номенклатуры (если существует)
	
	// Основные данные
	Status              string  `json:"status"`              // "active" | "inactive" | "new" (новый товар, еще не в системе)
	Name                string  `json:"name"`                 // Наименование
	InputUnit           string  `json:"input_unit"`          // Ед.изм (упак, кг и т.д.) - DEPRECATED
	InputUOM            string  `json:"input_uom"`          // Единица измерения поставщика - DEPRECATED, используйте UoMRuleID
	ConversionMultiplier float64 `json:"conversion_multiplier"` // Множитель конвертации - DEPRECATED, используйте UoMRuleID
	UoMRuleID           *string `json:"uom_rule_id"`        // ID правила конвертации единиц измерения
	Price               float64 `json:"price"`               // Цена
	SupplierID          string  `json:"supplier_id"`         // ID поставщика
	SupplierName        string  `json:"supplier_name"`       // Название поставщика
	Brand               string  `json:"brand"`               // Бренд
	MinOrderBatch       float64 `json:"min_order_batch"`     // Мин партия
	CurrentOrder        float64 `json:"current_order"`       // Ваш заказ (для текущего планирования)
	
	// Единицы измерения для склада
	BaseUnit            string  `json:"base_unit,omitempty"`        // Базовая единица (г, кг, л, мл, шт)
	InboundUnit         string  `json:"inbound_unit,omitempty"`    // Единица закупки
	ProductionUnit      string  `json:"production_unit,omitempty"` // Единица использования
	ConversionFactor    float64 `json:"conversion_factor,omitempty"` // Коэффициент пересчета
	
	// Дополнительные данные
	SKU            string  `json:"sku,omitempty"`   // SKU товара (если существует)
	CategoryID     *string `json:"category_id,omitempty"`
	CategoryName   string  `json:"category_name,omitempty"`
}

// GetSetupTemplate возвращает структуру каталога, сгруппированную по категориям
// ВАЖНО: Каталог поставщиков является источником истины - возвращаем только товары из SupplierCatalogItem
// Если каталог пустой, возвращаем пустой список (empty state) для ручного ввода менеджером
func (s *ProcurementCatalogService) GetSetupTemplate(branchID string) (*CatalogTemplateResponse, error) {
	// 1. Загружаем все категории номенклатуры (для группировки товаров)
	var categories []models.NomenclatureCategory
	if err := s.db.Where("deleted_at IS NULL").Order("name ASC").Find(&categories).Error; err != nil {
		return nil, fmt.Errorf("ошибка загрузки категорий: %w", err)
	}

	// 2. Загружаем ТОЛЬКО товары из каталога поставщиков для данного филиала
	// Это источник истины - менеджер вводит данные здесь, а не загружает из номенклатуры
	var catalogItems []models.SupplierCatalogItem
	query := s.db.Model(&models.SupplierCatalogItem{}).
		Preload("Nomenclature").
		Preload("Supplier").
		Preload("UoMRule").
		Where("deleted_at IS NULL")
	
	if branchID != "" {
		// Загружаем товары для конкретного филиала или общие (где branch_id IS NULL)
		query = query.Where("branch_id = ? OR branch_id IS NULL", branchID)
	} else {
		// Если branchID не указан, загружаем только общие товары
		query = query.Where("branch_id IS NULL")
	}
	
	if err := query.Find(&catalogItems).Error; err != nil {
		return nil, fmt.Errorf("ошибка загрузки каталога: %w", err)
	}

	// 3. Строим структуру: категории → товары
	categoryMap := make(map[string]*CatalogCategory)
	
	// Сначала создаем все категории из номенклатуры (для группировки)
	for _, cat := range categories {
		categoryMap[cat.ID] = &CatalogCategory{
			CategoryID:    cat.ID,
			CategoryName:  cat.Name,
			CategoryColor: cat.Color,
			Items:         []CatalogItem{},
		}
	}

	// Добавляем товары из каталога поставщиков
	for _, catalogItem := range catalogItems {
		// Пропускаем товары без номенклатуры или поставщика
		if catalogItem.Nomenclature == nil || catalogItem.Supplier == nil {
			continue
		}

		// Определяем категорию товара
		categoryID := "uncategorized"
		categoryName := "Без категории"
		if catalogItem.Nomenclature.CategoryID != nil {
			categoryID = *catalogItem.Nomenclature.CategoryID
			categoryName = catalogItem.Nomenclature.CategoryName
			// Если CategoryName пустой, загружаем категорию из БД
			if categoryName == "" {
				var category models.NomenclatureCategory
				if err := s.db.First(&category, "id = ?", categoryID).Error; err == nil {
					categoryName = category.Name
				}
			}
		}

		// Создаем категорию "Без категории", если её нет
		if categoryID == "uncategorized" {
			if _, exists := categoryMap[categoryID]; !exists {
				categoryMap[categoryID] = &CatalogCategory{
					CategoryID:    categoryID,
					CategoryName:  categoryName,
					CategoryColor: "#9ca3af",
					Items:         []CatalogItem{},
				}
			}
		} else {
			// Создаем категорию, если её нет в списке (может быть удалена из номенклатуры)
			if _, exists := categoryMap[categoryID]; !exists {
				var cat models.NomenclatureCategory
				if err := s.db.First(&cat, "id = ?", categoryID).Error; err == nil {
					categoryMap[categoryID] = &CatalogCategory{
						CategoryID:    cat.ID,
						CategoryName:  cat.Name,
						CategoryColor: cat.Color,
						Items:         []CatalogItem{},
					}
				} else {
					// Если категория не найдена, используем "Без категории"
					categoryID = "uncategorized"
					categoryName = "Без категории"
					if _, exists := categoryMap[categoryID]; !exists {
						categoryMap[categoryID] = &CatalogCategory{
							CategoryID:    categoryID,
							CategoryName:  categoryName,
							CategoryColor: "#9ca3af",
							Items:         []CatalogItem{},
						}
					}
				}
			}
		}

		item := CatalogItem{
			ID:                 catalogItem.ID,
			NomenclatureID:     &catalogItem.NomenclatureID,
			Status:             "active",
			Name:               catalogItem.Nomenclature.Name,
			InputUnit:          catalogItem.InputUnit, // DEPRECATED, для обратной совместимости
			InputUOM:           catalogItem.InputUOM, // DEPRECATED
			ConversionMultiplier: catalogItem.ConversionMultiplier, // DEPRECATED
			UoMRuleID:          catalogItem.UoMRuleID, // ID правила конвертации
			// Единицы измерения для склада
			BaseUnit:           catalogItem.Nomenclature.BaseUnit,
			InboundUnit:        catalogItem.Nomenclature.InboundUnit,
			ProductionUnit:     catalogItem.Nomenclature.ProductionUnit,
			ConversionFactor:   catalogItem.Nomenclature.ConversionFactor,
			Price:              catalogItem.Price,
			SupplierID:         catalogItem.SupplierID,
			SupplierName:       catalogItem.Supplier.Name,
			Brand:              catalogItem.Brand,
			MinOrderBatch:      catalogItem.MinOrderBatch,
			CurrentOrder:       0,
			SKU:                catalogItem.Nomenclature.SKU,
			CategoryID:         &categoryID,
			CategoryName:        categoryName,
		}
		
		// Если InputUOM пустой, используем InputUnit для обратной совместимости
		if item.InputUOM == "" && catalogItem.InputUnit != "" {
			item.InputUOM = catalogItem.InputUnit
		}
		
		// Если ConversionMultiplier равен 0, устанавливаем 1.0 по умолчанию
		if item.ConversionMultiplier == 0 {
			item.ConversionMultiplier = 1.0
		}

		// Определяем статус
		if catalogItem.IsActive {
			item.Status = "active"
		} else {
			item.Status = "inactive"
		}

		categoryMap[categoryID].Items = append(categoryMap[categoryID].Items, item)
	}

	// 4. Преобразуем map в slice
	result := &CatalogTemplateResponse{
		Categories: []CatalogCategory{},
	}

	// ВАЖНО: Всегда добавляем ВСЕ категории, включая пустые, чтобы пользователь мог добавлять товары в любую категорию
	// Сначала добавляем все категории из номенклатуры (даже если в них нет товаров)
	for _, cat := range categories {
		// Проверяем, есть ли уже эта категория в categoryMap (если в ней есть товары)
		if existingCat, exists := categoryMap[cat.ID]; exists {
			// Категория уже есть в map (с товарами), добавляем её
			result.Categories = append(result.Categories, *existingCat)
		} else {
			// Категории нет в map (пустая), создаем пустую категорию
			result.Categories = append(result.Categories, CatalogCategory{
				CategoryID:    cat.ID,
				CategoryName:  cat.Name,
				CategoryColor: cat.Color,
				Items:         []CatalogItem{},
			})
		}
	}
	
	// Добавляем категории из categoryMap, которых нет в списке categories
	// НО пропускаем пустую категорию "Без категории" (uncategorized), если в ней нет товаров
	for categoryID, cat := range categoryMap {
		// Пропускаем пустую категорию "Без категории"
		if categoryID == "uncategorized" && len(cat.Items) == 0 {
			continue
		}
		
		// Проверяем, не добавлена ли уже эта категория
		found := false
		for _, existingCat := range result.Categories {
			if existingCat.CategoryID == categoryID {
				found = true
				break
			}
		}
		if !found {
			result.Categories = append(result.Categories, *cat)
		}
	}

	return result, nil
}

// SaveCatalogRequest представляет запрос на сохранение каталога
type SaveCatalogRequest struct {
	BranchID  string       `json:"branch_id"`
	Items     []CatalogItem `json:"items"` // Все товары из таблицы
}

// SaveCatalog сохраняет каталог поставщиков
// Создает новые товары в номенклатуре, если их нет
// Создает/обновляет записи в supplier_catalog_items
func (s *ProcurementCatalogService) SaveCatalog(req *SaveCatalogRequest) error {
	tx := s.db.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
			log.Printf("❌ Транзакция откачена из-за panic: %v", r)
		}
	}()

	for _, item := range req.Items {
		// 1. Если товар новый (нет nomenclature_id), создаем его в номенклатуре
		var nomenclatureID string
		if item.NomenclatureID == nil || *item.NomenclatureID == "" {
			// Создаем новый товар
			categoryID := item.CategoryID
			if categoryID == nil || *categoryID == "" || *categoryID == "uncategorized" {
				// Ищем или создаем категорию "Без категории"
				var uncategorizedCategory models.NomenclatureCategory
				if err := tx.Where("name = ?", "Без категории").First(&uncategorizedCategory).Error; err != nil {
					// Создаем категорию
					uncategorizedCategory = models.NomenclatureCategory{
						Name:  "Без категории",
						Color: "#9ca3af",
					}
					if err := tx.Create(&uncategorizedCategory).Error; err != nil {
						tx.Rollback()
						return fmt.Errorf("ошибка создания категории: %w", err)
					}
				}
				categoryID = &uncategorizedCategory.ID
			} else {
				// Проверяем, что категория существует
				var existingCategory models.NomenclatureCategory
				if err := tx.Where("id = ?", *categoryID).First(&existingCategory).Error; err != nil {
					tx.Rollback()
					return fmt.Errorf("категория с ID %s не найдена", *categoryID)
				}
			}

			// Генерируем SKU на основе названия
			sku := generateSKUFromName(item.Name)

			// Определяем единицы измерения для номенклатуры
			// Используем поля из запроса, если указаны, иначе вычисляем из InputUOM
			baseUnit := item.BaseUnit
			inboundUnit := item.InboundUnit
			productionUnit := item.ProductionUnit
			conversionFactor := item.ConversionFactor
			
			// Если единицы измерения не указаны, используем InputUOM/InputUnit
			if baseUnit == "" || inboundUnit == "" || productionUnit == "" {
				// Используем InputUOM, если указан, иначе InputUnit
				tempInboundUnit := item.InputUOM
				if tempInboundUnit == "" {
					tempInboundUnit = item.InputUnit
				}
				if tempInboundUnit == "" {
					tempInboundUnit = "кг" // Значение по умолчанию
				}
				
				// Если не указаны явно, вычисляем из InputUOM
				if baseUnit == "" {
					baseUnit = normalizeUnit(tempInboundUnit)
				}
				if inboundUnit == "" {
					inboundUnit = tempInboundUnit
				}
				if productionUnit == "" {
					productionUnit = normalizeProductionUnitForCatalog(tempInboundUnit)
				}
			}
			
			// ВАЖНО: Берем conversion_factor из правила конвертации, которое указано в каталоге поставщиков
			// Если в каталоге указан UoMRuleID, загружаем правило и правильно интерпретируем multiplier
			// ВАЖНО: Проверяем правило ПОСЛЕ того, как inboundUnit уже установлен
			var packSize float64 = 0 // Размер упаковки в единицах InboundUnit (для нормализации цены)
			log.Printf("🔍 Перед проверкой правила: inboundUnit='%s', baseUnit='%s', productionUnit='%s'",
				inboundUnit, baseUnit, productionUnit)
			if item.UoMRuleID != nil && *item.UoMRuleID != "" {
				var uomRule models.UoMConversionRule
				if err := tx.Where("id = ? AND deleted_at IS NULL", *item.UoMRuleID).First(&uomRule).Error; err == nil {
					// Проверяем, совпадает ли InputUOM правила с InboundUnit номенклатуры
					// Если правило "1 ведро 10кг" (InputUOM), а InboundUnit = "kg", нужно правильно пересчитать
					ruleInputUOM := strings.ToLower(strings.TrimSpace(uomRule.InputUOM))
					inboundUnitLower := strings.ToLower(strings.TrimSpace(inboundUnit))
					
					// Нормализуем inboundUnit для проверки (кг -> kg)
					isKilogram := inboundUnitLower == "kg" || inboundUnitLower == "кг"
					log.Printf("🔍 Проверка правила '%s': InputUOM='%s', InboundUnit='%s' (lower: '%s'), isKilogram=%v",
						uomRule.Name, uomRule.InputUOM, inboundUnit, inboundUnitLower, isKilogram)
					log.Printf("🔍 Проверка правила '%s': InputUOM='%s', InboundUnit='%s' (lower: '%s'), isKilogram=%v",
						uomRule.Name, uomRule.InputUOM, inboundUnit, inboundUnitLower, isKilogram)
					
					// Если InputUOM правила содержит число и единицу (например, "1 ведро 10кг" или "10кг")
					// и InboundUnit номенклатуры = "kg" или "кг", нужно извлечь количество и разделить multiplier
					if strings.Contains(ruleInputUOM, "кг") || strings.Contains(ruleInputUOM, "kg") {
						// Пытаемся извлечь число из InputUOM (например, "10" из "1 ведро 10кг" или "10кг")
						// Ищем числа перед "кг" или "kg"
						re := regexp.MustCompile(`(\d+(?:[.,]\d+)?)\s*(?:кг|kg)`)
						matches := re.FindStringSubmatch(ruleInputUOM)
						if len(matches) > 1 {
							// Нашли число в InputUOM - извлекаем packSize для нормализации цены и conversion_factor
							fmt.Sscanf(strings.Replace(matches[1], ",", ".", 1), "%f", &packSize)
							if packSize > 0 && isKilogram {
								// Правило: 1 упаковка (packSize кг) = multiplier грамм
								// Нужно: 1 кг = multiplier / packSize грамм
								conversionFactor = uomRule.Multiplier / packSize
								log.Printf("✅ Правило '%s': 1 упаковка (%g кг) = %.2f г, значит 1 кг = %.2f г",
									uomRule.Name, packSize, uomRule.Multiplier, conversionFactor)
							} else if packSize > 0 {
								// packSize извлечен, но единицы не совпадают - используем multiplier как есть
								conversionFactor = uomRule.Multiplier
								log.Printf("✅ Использовано правило конвертации из каталога: '%s' (multiplier: %.2f, packSize: %g для нормализации цены)",
									uomRule.Name, uomRule.Multiplier, packSize)
							} else {
								// Если не удалось извлечь, используем multiplier как есть
								conversionFactor = uomRule.Multiplier
								log.Printf("✅ Использовано правило конвертации из каталога: '%s' (multiplier: %.2f)",
									uomRule.Name, uomRule.Multiplier)
							}
						} else if ruleInputUOM == inboundUnitLower || 
							(ruleInputUOM == "кг" && inboundUnitLower == "kg") ||
							(ruleInputUOM == "kg" && inboundUnitLower == "кг") {
							// InputUOM правила совпадает с InboundUnit номенклатуры - используем multiplier как есть
							conversionFactor = uomRule.Multiplier
							log.Printf("✅ Использовано правило конвертации из каталога: '%s' (multiplier: %.2f)",
								uomRule.Name, uomRule.Multiplier)
						} else {
							// InputUOM не совпадает - используем стандартную конвертацию
							log.Printf("⚠️ InputUOM правила '%s' не совпадает с InboundUnit '%s', используем стандартную конвертацию",
								uomRule.InputUOM, inboundUnit)
						}
					} else {
						// Если правило не содержит кг/kg, используем multiplier как есть
						conversionFactor = uomRule.Multiplier
						log.Printf("✅ Использовано правило конвертации из каталога: '%s' (multiplier: %.2f)",
							uomRule.Name, uomRule.Multiplier)
					}
				} else {
					log.Printf("⚠️ Правило конвертации с ID %s не найдено в БД", *item.UoMRuleID)
				}
			}
			
			// ВАЖНО: Нормализуем цену - если цена указана за упаковку, делим на размер упаковки
			// Пример: цена 1121₽ за ведро 10кг -> last_price = 1121 / 10 = 112.1₽/кг
			lastPrice := item.Price
			if packSize > 0 && lastPrice > 0 {
				lastPrice = lastPrice / packSize
				log.Printf("💰 Нормализация цены: цена за упаковку %.2f₽ / размер упаковки %.2f = цена за единицу %.2f₽/%s",
					item.Price, packSize, lastPrice, inboundUnit)
			}
			
			// Если правило не указано или не найдено, используем стандартные конвертации
			if conversionFactor <= 0 {
				if baseUnit == "g" && (inboundUnit == "kg" || inboundUnit == "кг") {
					conversionFactor = 1000 // килограммы -> граммы
				} else if baseUnit == "ml" && (inboundUnit == "l" || inboundUnit == "л") {
					conversionFactor = 1000 // литры -> миллилитры
				} else if baseUnit == inboundUnit && baseUnit == productionUnit {
					conversionFactor = 1
				} else {
					conversionFactor = 1000 // По умолчанию для г -> кг, мл -> л
				}
			}

			newItem := models.NomenclatureItem{
				Name:           item.Name,
				SKU:            sku,
				CategoryID:     categoryID,
				InboundUnit:    inboundUnit,
				BaseUnit:       baseUnit,
				ProductionUnit: productionUnit,
				ConversionFactor: conversionFactor,
				LastPrice:      lastPrice, // Нормализованная цена за единицу (кг/л/шт)
				IsActive:       item.Status == "active",
				IsSaleable:     false, // ВАЖНО: Все товары в каталоге поставщиков - это ингредиенты, не готовые продукты
			}

			// Загружаем категорию для получения имени
			var category models.NomenclatureCategory
			if err := tx.First(&category, "id = ?", categoryID).Error; err == nil {
				newItem.CategoryName = category.Name
				newItem.CategoryColor = category.Color
			}

			if err := tx.Create(&newItem).Error; err != nil {
				tx.Rollback()
				return fmt.Errorf("ошибка создания товара %s: %w", item.Name, err)
			}

			nomenclatureID = newItem.ID
			log.Printf("✅ Создан новый товар: %s (ID: %s)", item.Name, nomenclatureID)
		} else {
			nomenclatureID = *item.NomenclatureID
			
			// Обновляем существующий товар
			var existingItem models.NomenclatureItem
			if err := tx.First(&existingItem, "id = ?", nomenclatureID).Error; err != nil {
				tx.Rollback()
				return fmt.Errorf("товар не найден: %s", nomenclatureID)
			}

			// ВАЖНО: Проверяем, что товар не является готовым продуктом (is_saleable = false)
			if existingItem.IsSaleable {
				tx.Rollback()
				return fmt.Errorf("нельзя добавить готовый продукт '%s' в каталог поставщиков. Только ингредиенты могут быть в каталоге", existingItem.Name)
			}

			existingItem.Name = item.Name
			
			// Обновляем единицы измерения
			// Используем поля из запроса, если указаны, иначе из InputUOM/InputUnit
			if item.BaseUnit != "" {
				existingItem.BaseUnit = item.BaseUnit
			}
			if item.InboundUnit != "" {
				existingItem.InboundUnit = item.InboundUnit
			} else if item.InputUOM != "" {
				existingItem.InboundUnit = item.InputUOM
			} else if item.InputUnit != "" {
				existingItem.InboundUnit = item.InputUnit
			}
			if item.ProductionUnit != "" {
				existingItem.ProductionUnit = item.ProductionUnit
			}
			
			// КРИТИЧЕСКИ ВАЖНО: Валидация и исправление конфликтов единиц измерения
			// BaseUnit должен быть минимальной единицей (г/мл), а не крупной (кг/л)
			// для правильной работы формул расчета стоимости
			s.validateAndFixUnitSettingsFromCatalog(&existingItem)
			// ВАЖНО: Берем conversion_factor из правила конвертации, которое указано в каталоге поставщиков
			// Если в каталоге указан UoMRuleID, загружаем правило и берем multiplier
			if item.UoMRuleID != nil && *item.UoMRuleID != "" {
				var uomRule models.UoMConversionRule
				if err := tx.Where("id = ? AND deleted_at IS NULL", *item.UoMRuleID).First(&uomRule).Error; err == nil {
					// КРИТИЧЕСКИ ВАЖНО: Проверяем, что правило конвертации соответствует единицам измерения
					expectedFactor := s.calculateConversionFactorFromUnits(existingItem.InboundUnit, existingItem.BaseUnit)
					if expectedFactor > 0 && uomRule.Multiplier != expectedFactor {
						log.Printf("⚠️ ВНИМАНИЕ: Правило конвертации '%s' (multiplier: %.2f) из каталога не соответствует единицам для товара '%s' (InboundUnit: %s, BaseUnit: %s). Ожидается: %.2f",
							uomRule.Name, uomRule.Multiplier, existingItem.Name, existingItem.InboundUnit, existingItem.BaseUnit, expectedFactor)
						// Используем вычисленный коэффициент вместо multiplier из правила
						existingItem.ConversionFactor = expectedFactor
						log.Printf("✅ Использован вычисленный conversion_factor = %.2f вместо multiplier из правила",
							expectedFactor)
					} else {
						existingItem.ConversionFactor = uomRule.Multiplier
						log.Printf("✅ Обновлено правило конвертации из каталога для товара '%s': '%s' (multiplier: %.2f)",
							existingItem.Name, uomRule.Name, uomRule.Multiplier)
					}
				} else {
					log.Printf("⚠️ Правило конвертации с ID %s не найдено в БД для товара '%s'", *item.UoMRuleID, existingItem.Name)
				}
			} else if item.ConversionFactor > 0 {
				// Если правило не указано, но conversion_factor указан явно, проверяем его корректность
				expectedFactor := s.calculateConversionFactorFromUnits(existingItem.InboundUnit, existingItem.BaseUnit)
				if expectedFactor > 0 && item.ConversionFactor != expectedFactor {
					log.Printf("⚠️ ВНИМАНИЕ: ConversionFactor = %.2f из каталога не соответствует единицам для товара '%s' (InboundUnit: %s, BaseUnit: %s). Ожидается: %.2f",
						item.ConversionFactor, existingItem.Name, existingItem.InboundUnit, existingItem.BaseUnit, expectedFactor)
					// Используем вычисленный коэффициент
					existingItem.ConversionFactor = expectedFactor
				} else {
					existingItem.ConversionFactor = item.ConversionFactor
				}
			}
			
			// ВАЖНО: Нормализуем цену - если цена указана за упаковку, делим на размер упаковки
			// Сначала определяем packSize из правила, если оно указано
			var packSizeForPrice float64 = 0
			if item.UoMRuleID != nil && *item.UoMRuleID != "" {
				var uomRule models.UoMConversionRule
				if err := tx.Where("id = ? AND deleted_at IS NULL", *item.UoMRuleID).First(&uomRule).Error; err == nil {
					ruleInputUOM := strings.ToLower(strings.TrimSpace(uomRule.InputUOM))
					inboundUnitLower := strings.ToLower(strings.TrimSpace(existingItem.InboundUnit))
					if strings.Contains(ruleInputUOM, "кг") || strings.Contains(ruleInputUOM, "kg") {
						re := regexp.MustCompile(`(\d+(?:[.,]\d+)?)\s*(?:кг|kg)`)
						matches := re.FindStringSubmatch(ruleInputUOM)
						if len(matches) > 1 && inboundUnitLower == "kg" {
							fmt.Sscanf(strings.Replace(matches[1], ",", ".", 1), "%f", &packSizeForPrice)
						}
					}
				}
			}
			
			// Нормализуем цену
			if packSizeForPrice > 0 && item.Price > 0 {
				existingItem.LastPrice = item.Price / packSizeForPrice
				log.Printf("💰 Нормализация цены для товара '%s': цена за упаковку %.2f₽ / размер упаковки %.2f = цена за единицу %.2f₽/%s",
					existingItem.Name, item.Price, packSizeForPrice, existingItem.LastPrice, existingItem.InboundUnit)
			} else {
				existingItem.LastPrice = item.Price
			}
			existingItem.IsActive = item.Status == "active"
			// Убеждаемся, что IsSaleable остается false
			existingItem.IsSaleable = false

			if item.CategoryID != nil && *item.CategoryID != "" {
				existingItem.CategoryID = item.CategoryID
				var category models.NomenclatureCategory
				if err := tx.First(&category, "id = ?", item.CategoryID).Error; err == nil {
					existingItem.CategoryName = category.Name
					existingItem.CategoryColor = category.Color
				}
			}

			if err := tx.Save(&existingItem).Error; err != nil {
				tx.Rollback()
				return fmt.Errorf("ошибка обновления товара: %w", err)
			}
		}

		// 2. Создаем или обновляем запись в каталоге поставщиков
		if item.SupplierID == "" {
			continue // Пропускаем товары без поставщика
		}

		var catalogItem models.SupplierCatalogItem
		if item.ID != "" {
			// Обновляем существующую запись
			if err := tx.First(&catalogItem, "id = ?", item.ID).Error; err != nil {
				// Если не найдена, создаем новую
				catalogItem = models.SupplierCatalogItem{
					ID: item.ID,
				}
			}
		} else {
			// Проверяем, существует ли уже запись для этого товара и поставщика
			if err := tx.Where("nomenclature_id = ? AND supplier_id = ? AND branch_id = ?",
				nomenclatureID, item.SupplierID, req.BranchID).First(&catalogItem).Error; err != nil {
				// Не найдена, создаем новую
				catalogItem = models.SupplierCatalogItem{}
			}
		}

		catalogItem.NomenclatureID = nomenclatureID
		catalogItem.SupplierID = item.SupplierID
		catalogItem.BranchID = req.BranchID
		catalogItem.Brand = item.Brand
		catalogItem.InputUnit = item.InputUnit // DEPRECATED, для обратной совместимости
		
		// Сохраняем правило конвертации единиц измерения
		if item.UoMRuleID != nil && *item.UoMRuleID != "" {
			catalogItem.UoMRuleID = item.UoMRuleID
		} else {
			// Обратная совместимость: если правило не указано, используем старые поля
			catalogItem.InputUOM = item.InputUOM
			if item.InputUOM == "" && item.InputUnit != "" {
				catalogItem.InputUOM = item.InputUnit
			}
			if item.ConversionMultiplier > 0 {
				catalogItem.ConversionMultiplier = item.ConversionMultiplier
			} else {
				catalogItem.ConversionMultiplier = 1.0
			}
		}
		
		catalogItem.Price = item.Price
		catalogItem.MinOrderBatch = item.MinOrderBatch
		catalogItem.IsActive = item.Status == "active"

		if err := tx.Save(&catalogItem).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("ошибка сохранения каталога для товара %s: %w", item.Name, err)
		}
	}

	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("ошибка коммита транзакции: %w", err)
	}

	log.Printf("✅ Каталог поставщиков успешно сохранен (%d товаров)", len(req.Items))
	return nil
}

// generateSKUFromName генерирует SKU на основе названия товара
func generateSKUFromName(name string) string {
	// Простая генерация: первые буквы слов + timestamp
	// В реальности можно использовать более сложную логику
	sku := ""
	words := []rune(name)
	if len(words) > 0 {
		sku += string(words[0])
	}
	for i := 1; i < len(words); i++ {
		if words[i-1] == ' ' {
			sku += string(words[i])
		}
	}
	if len(sku) > 10 {
		sku = sku[:10]
	}
	sku += fmt.Sprintf("%d", time.Now().Unix()%10000)
	return sku
}

// normalizeProductionUnitForCatalog нормализует единицу измерения для производства
// Используется только в каталоге поставщиков
func normalizeProductionUnitForCatalog(unit string) string {
	unitMap := map[string]string{
		"упак": "g",
		"кг":   "g",
		"л":    "ml",
		"шт":   "g",
		"г":    "g",
		"мл":   "ml",
	}
	if normalized, ok := unitMap[unit]; ok {
		return normalized
	}
	return "g"
}

