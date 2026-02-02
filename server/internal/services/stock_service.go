package services

import (
	"fmt"
	"log"
	"time"

	"zephyrvpn/server/internal/models"

	"gorm.io/gorm"
)

// StockService управляет остатками товаров, партиями и сроками годности
type StockService struct {
	db                *gorm.DB
	counterpartyService *CounterpartyService
	financeService     *FinanceService
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
	
	// Группируем по товарам и филиалам
	stockMap := make(map[string]map[string]interface{})
	
		for _, batch := range batches {
		key := batch.NomenclatureID + "_" + batch.BranchID
		nomenclature := batch.Nomenclature
		
		// Вычисляем коэффициент конвертации для правильного расчета стоимости
		// cost_per_unit всегда за InboundUnit (кг/л/шт), а current_stock может быть в Base Unit (г)
		conversionFactor := 1.0
		baseUnit := nomenclature.BaseUnit
		inboundUnit := nomenclature.InboundUnit
		
		// Если единицы разные, вычисляем коэффициент конвертации
		if baseUnit != inboundUnit && inboundUnit != "" {
			if (baseUnit == "g" && inboundUnit == "kg") || (baseUnit == "ml" && inboundUnit == "l") {
				conversionFactor = 1000.0 // граммы в килограммы, миллилитры в литры
			} else if (baseUnit == "kg" && inboundUnit == "g") || (baseUnit == "l" && inboundUnit == "ml") {
				conversionFactor = 0.001
			} else if nomenclature.ConversionFactor > 0 {
				// Используем коэффициент конвертации из модели
				conversionFactor = nomenclature.ConversionFactor
			}
		}
		
		// Конвертируем current_stock в InboundUnit для расчета стоимости
		currentStockInMajorUnit := batch.RemainingQuantity
		if conversionFactor != 1.0 {
			currentStockInMajorUnit = batch.RemainingQuantity / conversionFactor
		}
		
		if stockItem, exists := stockMap[key]; exists {
			// Обновляем существующий товар
			currentStock := stockItem["current_stock"].(float64) + batch.RemainingQuantity
			stockItem["current_stock"] = currentStock
			
			// Пересчитываем cost_value с учетом конвертации
			currentStockInMajorUnitTotal := currentStock
			if conversionFactor != 1.0 {
				currentStockInMajorUnitTotal = currentStock / conversionFactor
			}
			stockItem["cost_value"] = currentStockInMajorUnitTotal * batch.CostPerUnit
			
			// Обновляем branch_name, если его еще нет
			if _, hasBranchName := stockItem["branch_name"]; !hasBranchName {
				stockItem["branch_name"] = branchMap[batch.BranchID]
			}
			
			// Обновляем информацию о сроках годности
			batchesList := stockItem["batches"].([]map[string]interface{})
			batchesList = append(batchesList, map[string]interface{}{
				"id":                batch.ID,
				"quantity":          batch.RemainingQuantity,
				"expiry_at":         batch.ExpiryAt,
				"days_until_expiry": s.calculateDaysUntilExpiry(batch.ExpiryAt),
				"hours_until_expiry": s.calculateHoursUntilExpiry(batch.ExpiryAt),
				"is_expired":        batch.IsExpired,
				"is_at_risk":        s.isAtRisk(batch),
			})
			stockItem["batches"] = batchesList
		} else {
			// Создаем новый товар
			minStock := nomenclature.MinStockLevel
			currentStock := batch.RemainingQuantity
			
			status := "in_stock"
			if currentStock <= 0 {
				status = "out_of_stock"
			} else if currentStock < minStock {
				status = "low_stock"
			}
			
			// Вычисляем cost_value с учетом конвертации единиц
			costValue := currentStockInMajorUnit * batch.CostPerUnit
			
			stockMap[key] = map[string]interface{}{
				"id":                nomenclature.ID,
				"product_id":        nomenclature.ID,
				"product_name":     nomenclature.Name,
				"category":         nomenclature.CategoryName,
				"category_color":    nomenclature.CategoryColor,
				"category_id":       nomenclature.CategoryID,
				"unit":             nomenclature.BaseUnit,
				"branch_id":        batch.BranchID,
				"branch_name":      branchMap[batch.BranchID], // Добавляем имя филиала
				"current_stock":    currentStock,
				"min_stock":        minStock,
				"cost_per_unit":    batch.CostPerUnit,
				"cost_value":       costValue,
				"status":           status,
				"batches": []map[string]interface{}{
					{
						"id":                batch.ID,
						"quantity":          batch.RemainingQuantity,
						"expiry_at":         batch.ExpiryAt,
						"days_until_expiry": s.calculateDaysUntilExpiry(batch.ExpiryAt),
						"hours_until_expiry": s.calculateHoursUntilExpiry(batch.ExpiryAt),
						"is_expired":        batch.IsExpired,
						"is_at_risk":        s.isAtRisk(batch),
					},
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
		batch.RemainingQuantity -= deductQuantity
		if err := s.db.Save(&batch).Error; err != nil {
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

			// Цена за грамм = LastPrice / 1000 (если цена указана за кг)
			// Но так как все в граммах, а цена обычно за кг, нужно конвертировать
			pricePerGram := nomenclature.LastPrice / 1000.0 // Предполагаем, что LastPrice за кг
			ingredientCost = pricePerGram * ingredient.Quantity
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

		batch.RemainingQuantity -= deductQuantity
		if err := tx.Save(&batch).Error; err != nil {
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
	// Используем оптимизированную батч-версию для обработки
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


