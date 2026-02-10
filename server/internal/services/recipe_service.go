package services

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"zephyrvpn/server/internal/models"
	"zephyrvpn/server/internal/utils"

	"gorm.io/gorm"
)

// validateNomenclatureIngredient проверяет, что NomenclatureID существует, активен и не удален
// Также проверяет совместимость единиц измерения
func (s *RecipeService) validateNomenclatureIngredient(tx *gorm.DB, ingredient *models.RecipeIngredient, ingredientIndex int) error {
	if ingredient.NomenclatureID == nil {
		return nil // Это полуфабрикат, валидация не требуется
	}

	// Загружаем номенклатуру с проверкой существования, активности и отсутствия удаления
	var nomenclature models.NomenclatureItem
	if err := tx.Where("id = ? AND is_active = true AND deleted_at IS NULL", *ingredient.NomenclatureID).
		First(&nomenclature).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return fmt.Errorf("ингредиент #%d: номенклатура с ID %s не найдена, неактивна или удалена", 
				ingredientIndex+1, *ingredient.NomenclatureID)
		}
		return fmt.Errorf("ингредиент #%d: ошибка проверки номенклатуры: %w", ingredientIndex+1, err)
	}

	// Нормализуем единицу измерения в lowercase
	normalizedUnit := strings.ToLower(strings.TrimSpace(ingredient.Unit))
	if normalizedUnit == "" {
		normalizedUnit = "g" // Значение по умолчанию
	}
	ingredient.Unit = normalizedUnit

	// Проверяем совместимость единиц измерения
	if !s.isUnitCompatible(normalizedUnit, nomenclature) {
		return fmt.Errorf("ингредиент #%d: единица измерения '%s' несовместима с номенклатурой '%s' (поддерживаемые единицы: %s, %s, %s)", 
			ingredientIndex+1, normalizedUnit, nomenclature.Name,
			nomenclature.BaseUnit, nomenclature.InboundUnit, nomenclature.ProductionUnit)
	}

	return nil
}

// isUnitCompatible проверяет, совместима ли единица измерения с номенклатурой
func (s *RecipeService) isUnitCompatible(unit string, nomenclature models.NomenclatureItem) bool {
	unit = strings.ToLower(strings.TrimSpace(unit))
	baseUnit := strings.ToLower(strings.TrimSpace(nomenclature.BaseUnit))
	inboundUnit := strings.ToLower(strings.TrimSpace(nomenclature.InboundUnit))
	productionUnit := strings.ToLower(strings.TrimSpace(nomenclature.ProductionUnit))

	// Прямое совпадение
	if unit == baseUnit || unit == inboundUnit || unit == productionUnit {
		return true
	}

	// Известные конвертации (g↔kg, ml↔l)
	compatiblePairs := map[string][]string{
		"g":  {"kg", "g"},
		"kg": {"g", "kg"},
		"ml": {"l", "ml"},
		"l":  {"ml", "l"},
		"gram":  {"g", "kg"},
		"grams": {"g", "kg"},
		"kilogram": {"kg", "g"},
		"kilograms": {"kg", "g"},
		"liter": {"l", "ml"},
		"liters": {"l", "ml"},
		"litre": {"l", "ml"},
		"litres": {"l", "ml"},
		"milliliter": {"ml", "l"},
		"milliliters": {"ml", "l"},
		"millilitre": {"ml", "l"},
		"millilitres": {"ml", "l"},
	}

	if compatible, ok := compatiblePairs[unit]; ok {
		for _, u := range compatible {
			if u == baseUnit || u == inboundUnit || u == productionUnit {
				return true
			}
		}
	}

	return false
}

// normalizePhotoURLsForJSONB нормализует photo_urls для вставки в JSONB колонку
// Пустая строка -> пустая строка (будет установлена как NULL в GORM)
// Непустая строка -> валидируется как JSON, если не валидна - возвращает ошибку
func normalizePhotoURLsForJSONB(photoURLs string) (string, error) {
	// Если пустая строка, возвращаем пустую (GORM установит NULL для JSONB)
	if photoURLs == "" {
		return "", nil
	}
	
	// Проверяем, является ли строка валидным JSON
	var testArray []interface{}
	if err := json.Unmarshal([]byte(photoURLs), &testArray); err != nil {
		// Если это не массив, проверяем, может быть это уже валидный JSON объект
		var testObject map[string]interface{}
		if err2 := json.Unmarshal([]byte(photoURLs), &testObject); err2 != nil {
			return "", fmt.Errorf("photo_urls должен быть валидным JSON массивом или объектом: %w", err)
		}
		// Если это объект, конвертируем в массив с одним элементом
		photoURLs = fmt.Sprintf(`[%s]`, photoURLs)
	}
	
	// Проверяем, что это массив строк
	var urlArray []string
	if err := json.Unmarshal([]byte(photoURLs), &urlArray); err != nil {
		return "", fmt.Errorf("photo_urls должен быть массивом строк: %w", err)
	}
	
	// Возвращаем нормализованный JSON
	normalized, err := json.Marshal(urlArray)
	if err != nil {
		return "", fmt.Errorf("ошибка сериализации photo_urls: %w", err)
	}
	
	return string(normalized), nil
}

// RecipeService управляет рецептами и технологическими картами
type RecipeService struct {
	db            *gorm.DB
	stockService  *StockService
	redisUtil     *utils.RedisClient // Для инвалидации кэша меню
}

// NewRecipeService создает новый сервис рецептов
func NewRecipeService(db *gorm.DB) *RecipeService {
	return &RecipeService{
		db: db,
	}
}

// SetStockService устанавливает сервис остатков для расчета себестоимости
func (s *RecipeService) SetStockService(stockService *StockService) {
	s.stockService = stockService
}

// SetRedisUtil устанавливает Redis клиент для инвалидации кэша меню
func (s *RecipeService) SetRedisUtil(redisUtil *utils.RedisClient) {
	s.redisUtil = redisUtil
}

// GetDB возвращает экземпляр БД для прямых запросов
func (s *RecipeService) GetDB() *gorm.DB {
	return s.db
}

// UnifiedCreateMenuItem создает Menu Item (товар для продажи или полуфабрикат) в единой транзакции:
// 1. Создает NomenclatureItem с IsSaleable=true (для finished) или false (для semi-finished)
// 2. Создает Recipe (Sales Recipe, IsSemiFinished=false) или Production Recipe (IsSemiFinished=true)
// 3. Создает PizzaRecipe (только для finished товаров, для обратной совместимости)
// Все операции выполняются в одной транзакции - либо все успешно, либо откат
func (s *RecipeService) UnifiedCreateMenuItem(
	name string,
	description string,
	price int,
	ingredients []models.RecipeIngredient,
	nomenclatureData *models.NomenclatureItem, // Данные для создания NomenclatureItem
	isSemiFinished bool, // true для полуфабрикатов, false для готовых товаров
) (*models.Recipe, error) {
	// Начинаем транзакцию
	tx := s.db.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
			log.Printf("❌ UnifiedCreateMenuItem: panic recovered, transaction rolled back: %v", r)
		}
	}()

	// 1. Создаем или используем существующий NomenclatureItem
	if nomenclatureData == nil {
		tx.Rollback()
		return nil, fmt.Errorf("nomenclatureData не может быть nil")
	}
	
	// Проверяем, существует ли уже товар (если передан ID)
	if nomenclatureData.ID != "" {
		// Товар уже существует - проверяем, что он есть в БД
		var existingNomenclature models.NomenclatureItem
		if err := tx.First(&existingNomenclature, "id = ?", nomenclatureData.ID).Error; err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("номенклатура с ID %s не найдена: %w", nomenclatureData.ID, err)
		}
		// Используем существующий товар
		nomenclatureData = &existingNomenclature
		log.Printf("✅ UnifiedCreateMenuItem: используется существующий NomenclatureItem - ID: %s, Name: %s", nomenclatureData.ID, nomenclatureData.Name)
	} else {
		// Создаем новый товар
		// Устанавливаем обязательные поля
		// IsSaleable: true для finished товаров (готовы к продаже), false для полуфабрикатов
		// Если значение уже установлено в nomenclatureData, используем его, иначе устанавливаем по умолчанию
		if !isSemiFinished {
			// Finished товар - готов к продаже
			nomenclatureData.IsSaleable = true
		} else {
			// Полуфабрикат - не продается напрямую
			nomenclatureData.IsSaleable = false
		}
		nomenclatureData.IsActive = true
		if nomenclatureData.Name == "" {
			nomenclatureData.Name = name
		}
		if nomenclatureData.SKU == "" {
			// Генерируем SKU из имени (можно улучшить)
			nomenclatureData.SKU = strings.ToUpper(strings.ReplaceAll(name, " ", "_"))
		}
		
		if err := tx.Create(nomenclatureData).Error; err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("ошибка создания NomenclatureItem: %w", err)
		}
		log.Printf("✅ UnifiedCreateMenuItem: создан NomenclatureItem - ID: %s, Name: %s, IsSaleable: %v, IsSemiFinished: %v", 
			nomenclatureData.ID, nomenclatureData.Name, nomenclatureData.IsSaleable, isSemiFinished)
	}

	// 2. Создаем Recipe (Sales Recipe для finished или Production Recipe для semi-finished)
	recipe := &models.Recipe{
		Name:           name,
		Description:    description,
		MenuItemID:     &nomenclatureData.ID, // Связываем с NomenclatureItem
		PortionSize:    1.0,
		Unit:           "pcs",
		IsSemiFinished: isSemiFinished, // Используем значение из запроса
		IsActive:       true,
		Ingredients:    ingredients,
	}
	
	// ВАЖНО: Инициализируем StationIDs как валидный JSON массив (пустой массив)
	// Если не инициализировать, GORM попытается вставить пустую строку, что вызовет ошибку PostgreSQL
	if err := recipe.SetStationIDs([]string{}); err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("ошибка инициализации StationIDs: %w", err)
	}
	
	// Валидация ингредиентов
	for i := range recipe.Ingredients {
		ingredient := &recipe.Ingredients[i]
		if ingredient.NomenclatureID == nil && ingredient.IngredientRecipeID == nil {
			tx.Rollback()
			return nil, fmt.Errorf("ингредиент #%d: должен быть указан либо nomenclature_id, либо ingredient_recipe_id", i+1)
		}
		
		// Валидация номенклатуры (если это сырье)
		if ingredient.NomenclatureID != nil {
			if err := s.validateNomenclatureIngredient(tx, ingredient, i); err != nil {
				tx.Rollback()
				return nil, fmt.Errorf("ошибка валидации ингредиента #%d: %w", i+1, err)
			}
		}
	}
	
	// Нормализуем PhotoURLs для JSONB: пустая строка -> NULL, иначе валидируем JSON
	normalizedPhotoURLs, err := normalizePhotoURLsForJSONB(recipe.PhotoURLs)
	if err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("ошибка нормализации photo_urls: %w", err)
	}
	
	// ВАЖНО: Сохраняем ингредиенты во временную переменную и очищаем recipe.Ingredients
	// Это предотвращает двойное сохранение ингредиентов (GORM может попытаться сохранить их автоматически)
	ingredientsToCreate := recipe.Ingredients
	recipe.Ingredients = nil // Очищаем перед созданием Recipe
	
	// Создаем Recipe (без ингредиентов сначала)
	// Используем Omit для предотвращения автоматического сохранения ассоциаций
	createQuery := tx.Omit("Ingredients")
	
	// Если photo_urls пустое, не включаем его в запрос (останется NULL в БД)
	// Если не пустое, устанавливаем нормализованное значение
	if normalizedPhotoURLs != "" {
		recipe.PhotoURLs = normalizedPhotoURLs
		// Включаем все поля, включая photo_urls
		if err := createQuery.Create(recipe).Error; err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("ошибка создания Recipe: %w", err)
		}
	} else {
		// Исключаем photo_urls из запроса, чтобы GORM не пытался вставить пустую строку в JSONB
		if err := createQuery.Omit("photo_urls").Create(recipe).Error; err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("ошибка создания Recipe: %w", err)
		}
	}
	
	// Создаем ингредиенты вручную (один раз)
	for i := range ingredientsToCreate {
		ingredient := &ingredientsToCreate[i]
		ingredient.RecipeID = recipe.ID
		ingredient.ID = "" // Сбрасываем ID для создания нового
		
		if err := tx.Create(ingredient).Error; err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("ошибка создания ингредиента #%d: %w", i+1, err)
		}
	}
	log.Printf("✅ UnifiedCreateMenuItem: создан Recipe - ID: %s, Name: %s, IsSemiFinished: %v", recipe.ID, recipe.Name, recipe.IsSemiFinished)

	// 3. Создаем PizzaRecipe (только для finished товаров, для обратной совместимости)
	// Полуфабрикаты не должны иметь PizzaRecipe, так как они не продаются напрямую
	if !isSemiFinished {
		// Преобразуем ингредиенты в JSON
		ingredientNames := make([]string, 0, len(ingredients))
		ingredientAmounts := make(map[string]int)
		
		for _, ing := range ingredients {
			if ing.Nomenclature != nil && ing.Nomenclature.Name != "" {
				ingredientNames = append(ingredientNames, ing.Nomenclature.Name)
				// Используем количество в граммах (если unit = "g")
				if ing.Unit == "g" {
					ingredientAmounts[ing.Nomenclature.Name] = int(ing.Quantity)
				} else {
					ingredientAmounts[ing.Nomenclature.Name] = 100 // Значение по умолчанию
				}
			}
		}
		
		ingredientsJSON, _ := json.Marshal(ingredientNames)
		amountsJSON, _ := json.Marshal(ingredientAmounts)
		
		pizzaRecipe := models.PizzaRecipe{
			Name:              name,
			Price:             price,
			Ingredients:       string(ingredientsJSON),
			IngredientAmounts: string(amountsJSON),
			IsActive:          true,
		}
		
		if err := tx.Create(&pizzaRecipe).Error; err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("ошибка создания PizzaRecipe: %w", err)
		}
		log.Printf("✅ UnifiedCreateMenuItem: создан PizzaRecipe - Name: %s, Price: %d", pizzaRecipe.Name, pizzaRecipe.Price)
	} else {
		log.Printf("ℹ️ UnifiedCreateMenuItem: PizzaRecipe не создается для полуфабриката '%s'", name)
	}

	// Коммитим транзакцию
	if err := tx.Commit().Error; err != nil {
		return nil, fmt.Errorf("ошибка коммита транзакции: %w", err)
	}

	// Инвалидируем кэш меню
	s.invalidateMenuCache()

	// Загружаем созданный Recipe с полными данными
	createdRecipe, err := s.GetRecipe(recipe.ID)
	if err != nil {
		return nil, fmt.Errorf("ошибка загрузки созданного Recipe: %w", err)
	}

	log.Printf("✅ UnifiedCreateMenuItem: успешно создан Menu Item - Nomenclature ID: %s, Recipe ID: %s, Name: %s", 
		nomenclatureData.ID, createdRecipe.ID, name)
	
	return createdRecipe, nil
}

// GetRecipes возвращает список всех рецептов
func (s *RecipeService) GetRecipes(includeInactive bool) ([]models.Recipe, error) {
	var recipes []models.Recipe
	query := s.db.Preload("Ingredients").Preload("Ingredients.Nomenclature").Preload("Ingredients.IngredientRecipe").
		Where("deleted_at IS NULL") // Исключаем удаленные рецепты

	if !includeInactive {
		query = query.Where("is_active = ?", true)
	}

	if err := query.Order("created_at DESC").Find(&recipes).Error; err != nil {
		return nil, err
	}

	log.Printf("📋 GetRecipes: возвращено %d рецептов (includeInactive: %v)", len(recipes), includeInactive)
	return recipes, nil
}

// GetRecipe возвращает рецепт по ID
func (s *RecipeService) GetRecipe(recipeID string) (*models.Recipe, error) {
	var recipe models.Recipe
	if err := s.db.Preload("Ingredients").Preload("Ingredients.Nomenclature").Preload("Ingredients.IngredientRecipe").
		First(&recipe, "id = ?", recipeID).Error; err != nil {
		return nil, err
	}

	return &recipe, nil
}

// CreateRecipe создает новый рецепт
// ВАЖНО: Использует Omit("Ingredients") чтобы предотвратить двойное сохранение ингредиентов
// GORM автоматически сохраняет ассоциации при Create, поэтому мы явно исключаем Ingredients
// и создаем их вручную для полного контроля над процессом
func (s *RecipeService) CreateRecipe(recipe *models.Recipe) error {
	// Начинаем транзакцию
	tx := s.db.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
			log.Printf("❌ CreateRecipe: panic recovered, transaction rolled back: %v", r)
		}
	}()

	// Валидация: проверяем на дубликаты nomenclature_id в рамках одного рецепта
	// (для сырья) или ingredient_recipe_id (для полуфабрикатов)
	nomenclatureMap := make(map[string]bool)
	recipeMap := make(map[string]bool)
	
	for i, ingredient := range recipe.Ingredients {
		// Проверяем дубликаты сырья (nomenclature_id)
		if ingredient.NomenclatureID != nil {
			nomenclatureKey := *ingredient.NomenclatureID
			if nomenclatureMap[nomenclatureKey] {
				tx.Rollback()
				return fmt.Errorf("дубликат ингредиента: nomenclature_id %s уже используется в этом рецепте", nomenclatureKey)
			}
			nomenclatureMap[nomenclatureKey] = true
		}
		
		// Проверяем дубликаты полуфабрикатов (ingredient_recipe_id)
		if ingredient.IngredientRecipeID != nil {
			recipeKey := *ingredient.IngredientRecipeID
			if recipeMap[recipeKey] {
				tx.Rollback()
				return fmt.Errorf("дубликат ингредиента: ingredient_recipe_id %s уже используется в этом рецепте", recipeKey)
			}
			recipeMap[recipeKey] = true
		}
		
		// Валидация: должен быть указан либо nomenclature_id, либо ingredient_recipe_id
		if ingredient.NomenclatureID == nil && ingredient.IngredientRecipeID == nil {
			tx.Rollback()
			return fmt.Errorf("ингредиент #%d: должен быть указан либо nomenclature_id, либо ingredient_recipe_id", i+1)
		}
		
		// Валидация: не могут быть указаны оба одновременно
		if ingredient.NomenclatureID != nil && ingredient.IngredientRecipeID != nil {
			tx.Rollback()
			return fmt.Errorf("ингредиент #%d: не могут быть указаны одновременно nomenclature_id и ingredient_recipe_id", i+1)
		}

		// Валидация номенклатуры: существование, активность, совместимость единиц
		// Нормализуем единицу измерения перед валидацией
		if ingredient.NomenclatureID != nil {
			recipe.Ingredients[i].Unit = strings.ToLower(strings.TrimSpace(recipe.Ingredients[i].Unit))
			if recipe.Ingredients[i].Unit == "" {
				recipe.Ingredients[i].Unit = "g" // Значение по умолчанию
			}
			
			if err := s.validateNomenclatureIngredient(tx, &recipe.Ingredients[i], i); err != nil {
				tx.Rollback()
				return err
			}
		}
	}

	// Нормализуем PhotoURLs для JSONB: пустая строка -> NULL, иначе валидируем JSON
	normalizedPhotoURLs, err := normalizePhotoURLsForJSONB(recipe.PhotoURLs)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("ошибка нормализации photo_urls: %w", err)
	}
	
	// Сохраняем рецепт БЕЗ ингредиентов (Omit предотвращает автоматическое сохранение ассоциаций)
	// Это критически важно, чтобы избежать двойного сохранения ингредиентов
	createQuery := tx.Omit("Ingredients")
	
	// Если photo_urls пустое, не включаем его в запрос (останется NULL в БД)
	// Если не пустое, устанавливаем нормализованное значение
	if normalizedPhotoURLs != "" {
		recipe.PhotoURLs = normalizedPhotoURLs
		// Включаем все поля, включая photo_urls
		if err := createQuery.Create(recipe).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("ошибка создания рецепта: %w", err)
		}
	} else {
		// Сохраняем оригинальное значение (пустая строка) для сохранения структуры
		// Но исключаем photo_urls из запроса, чтобы GORM не пытался вставить пустую строку в JSONB
		originalPhotoURLs := recipe.PhotoURLs
		recipe.PhotoURLs = "" // Временно очищаем
		if err := createQuery.Omit("photo_urls").Create(recipe).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("ошибка создания рецепта: %w", err)
		}
		recipe.PhotoURLs = originalPhotoURLs // Восстанавливаем для дальнейшего использования
	}

	log.Printf("📝 CreateRecipe: рецепт создан - ID: %s, Name: %s, Ingredients count: %d", 
		recipe.ID, recipe.Name, len(recipe.Ingredients))

	// Сохраняем ингредиенты вручную (это единственное место, где они создаются)
	for i := range recipe.Ingredients {
		// Очищаем ID ингредиента, чтобы GORM сгенерировал новый UUID
		// Это предотвращает конфликты, если клиент отправил существующий ID
		recipe.Ingredients[i].ID = ""
		recipe.Ingredients[i].RecipeID = recipe.ID
		
		// Единица измерения уже нормализована в валидации выше
		
		// Валидация циклических зависимостей для полуфабрикатов
		if recipe.Ingredients[i].IngredientRecipeID != nil {
			if err := s.ValidateRecipeIngredient(recipe.ID, recipe.Ingredients[i].IngredientRecipeID); err != nil {
				tx.Rollback()
				return fmt.Errorf("ошибка валидации ингредиента #%d: %w", i+1, err)
			}
		}
		
		if err := tx.Create(&recipe.Ingredients[i]).Error; err != nil {
			tx.Rollback()
			// Проверяем, не является ли ошибка нарушением уникального ограничения
			if isUniqueConstraintError(err) {
				return fmt.Errorf("дубликат ингредиента: %w (возможно, уже существует в базе данных)", err)
			}
			return fmt.Errorf("ошибка создания ингредиента #%d: %w", i+1, err)
		}
		
		log.Printf("📝 CreateRecipe: создан ингредиент #%d - ID: %s, RecipeID: %s, Unit: %s", 
			i+1, recipe.Ingredients[i].ID, recipe.Ingredients[i].RecipeID, recipe.Ingredients[i].Unit)
	}

	// Коммитим транзакцию
	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("ошибка коммита транзакции: %w", err)
	}

	log.Printf("✅ CreateRecipe: рецепт успешно создан - ID: %s, Name: %s, Ingredients: %d", 
		recipe.ID, recipe.Name, len(recipe.Ingredients))
	
	// Инвалидируем кэш меню через Redis Pub/Sub
	s.invalidateMenuCache()
	
	return nil
}

// isUniqueConstraintError проверяет, является ли ошибка нарушением уникального ограничения
func isUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	// PostgreSQL unique constraint violation patterns
	return strings.Contains(errStr, "duplicate key") || 
		   strings.Contains(errStr, "unique constraint") || 
		   strings.Contains(errStr, "violates unique constraint") ||
		   strings.Contains(errStr, "23505") // PostgreSQL unique violation error code
}

// UpdateRecipe обновляет существующий рецепт
func (s *RecipeService) UpdateRecipe(recipeID string, recipe *models.Recipe) error {
	// Начинаем транзакцию
	tx := s.db.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// Проверяем существование рецепта
	var existingRecipe models.Recipe
	if err := tx.First(&existingRecipe, "id = ?", recipeID).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("рецепт не найден: %w", err)
	}

	// Обновляем основные поля рецепта (включая поля Recipe Book)
	recipe.ID = recipeID
	updates := map[string]interface{}{
		"name":            recipe.Name,
		"description":    recipe.Description,
		"menu_item_id":    recipe.MenuItemID,
		"portion_size":    recipe.PortionSize,
		"unit":            recipe.Unit,
		"is_semi_finished": recipe.IsSemiFinished,
		"is_active":       recipe.IsActive,
	}
	
	// Обновляем поля Recipe Book (если указаны)
	if recipe.InstructionText != "" {
		updates["instruction_text"] = recipe.InstructionText
	}
	if recipe.VideoURL != "" {
		updates["video_url"] = recipe.VideoURL
	}
	// Нормализуем PhotoURLs для JSONB: пустая строка -> NULL, иначе валидируем JSON
	normalizedPhotoURLs, err := normalizePhotoURLsForJSONB(recipe.PhotoURLs)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("ошибка нормализации photo_urls: %w", err)
	}
	if normalizedPhotoURLs != "" {
		updates["photo_urls"] = normalizedPhotoURLs
	} else {
		// Если пустая строка, устанавливаем NULL для JSONB
		updates["photo_urls"] = nil
	}
	
	if err := tx.Model(&existingRecipe).Updates(updates).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("ошибка обновления рецепта: %w", err)
	}

	// Удаляем старые ингредиенты
	if err := tx.Where("recipe_id = ?", recipeID).Delete(&models.RecipeIngredient{}).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("ошибка удаления старых ингредиентов: %w", err)
	}

	// Валидация ингредиентов перед созданием
	for i := range recipe.Ingredients {
		// Нормализуем единицу измерения перед валидацией
		if recipe.Ingredients[i].NomenclatureID != nil {
			recipe.Ingredients[i].Unit = strings.ToLower(strings.TrimSpace(recipe.Ingredients[i].Unit))
			if recipe.Ingredients[i].Unit == "" {
				recipe.Ingredients[i].Unit = "g" // Значение по умолчанию
			}
			
			// Валидация номенклатуры: существование, активность, совместимость единиц
			if err := s.validateNomenclatureIngredient(tx, &recipe.Ingredients[i], i); err != nil {
				tx.Rollback()
				return fmt.Errorf("ошибка валидации ингредиента #%d: %w", i+1, err)
			}
		}
	}

	// Создаем новые ингредиенты
	for i := range recipe.Ingredients {
		recipe.Ingredients[i].RecipeID = recipeID
		recipe.Ingredients[i].ID = "" // Сбрасываем ID для создания нового
		
		// Единица измерения уже нормализована в валидации выше
		
		if err := tx.Create(&recipe.Ingredients[i]).Error; err != nil {
			tx.Rollback()
			// Проверяем, не является ли ошибка нарушением уникального ограничения
			if isUniqueConstraintError(err) {
				return fmt.Errorf("дубликат ингредиента: %w (возможно, уже существует в базе данных)", err)
			}
			return fmt.Errorf("ошибка создания ингредиента #%d: %w", i+1, err)
		}
	}

	if err := tx.Commit().Error; err != nil {
		return err
	}

	log.Printf("✅ Обновлен рецепт: %s (ID: %s)", recipe.Name, recipeID)
	
	// TODO: Создать версию рецепта через TechnologistService
	// Это требует инъекции TechnologistService или вызова через интерфейс
	// Пока оставляем как TODO для будущей интеграции
	
	// Инвалидируем кэш меню через Redis Pub/Sub
	s.invalidateMenuCache()
	
	return nil
}

// DeleteRecipe удаляет рецепт (soft delete)
func (s *RecipeService) DeleteRecipe(recipeID string) error {
	if err := s.db.Delete(&models.Recipe{}, "id = ?", recipeID).Error; err != nil {
		return fmt.Errorf("ошибка удаления рецепта: %w", err)
	}

	log.Printf("✅ Удален рецепт (ID: %s)", recipeID)
	
	// Инвалидируем кэш меню через Redis Pub/Sub
	s.invalidateMenuCache()
	
	return nil
}

// invalidateMenuCache публикует событие обновления меню в Redis
func (s *RecipeService) invalidateMenuCache() {
	if s.redisUtil != nil {
		// Используем тот же канал, что и MenuService
		if err := s.redisUtil.Publish("menu:update", "recipe_updated"); err != nil {
			log.Printf("⚠️ Ошибка публикации события обновления меню: %v", err)
		} else {
			log.Println("📢 Событие обновления меню опубликовано в Redis")
		}
	}
}

// ValidateRecipeIngredient проверяет, что ингредиент валиден (нет циклических зависимостей)
func (s *RecipeService) ValidateRecipeIngredient(recipeID string, ingredientRecipeID *string) error {
	if ingredientRecipeID == nil {
		return nil // Сырье - всегда валидно
	}

	// Проверяем, что не пытаемся добавить сам рецепт как ингредиент
	if *ingredientRecipeID == recipeID {
		return fmt.Errorf("нельзя использовать рецепт как собственный ингредиент (циклическая зависимость)")
	}

	// Рекурсивно проверяем вложенные рецепты
	visited := make(map[string]bool)
	return s.checkCyclicDependency(recipeID, *ingredientRecipeID, visited)
}

// checkCyclicDependency рекурсивно проверяет циклические зависимости
func (s *RecipeService) checkCyclicDependency(originalRecipeID string, currentRecipeID string, visited map[string]bool) error {
	if currentRecipeID == originalRecipeID {
		return fmt.Errorf("обнаружена циклическая зависимость: рецепт %s ссылается на себя", originalRecipeID)
	}

	if visited[currentRecipeID] {
		return nil // Уже проверяли этот рецепт
	}
	visited[currentRecipeID] = true

	// Загружаем рецепт и проверяем его ингредиенты
	var recipe models.Recipe
	if err := s.db.Preload("Ingredients").First(&recipe, "id = ?", currentRecipeID).Error; err != nil {
		return nil // Рецепт не найден - не критично
	}

	for _, ingredient := range recipe.Ingredients {
		if ingredient.IngredientRecipeID != nil {
			if err := s.checkCyclicDependency(originalRecipeID, *ingredient.IngredientRecipeID, visited); err != nil {
				return err
			}
		}
	}

	return nil
}

// GetFolderContent возвращает содержимое папки (дочерние узлы)
func (s *RecipeService) GetFolderContent(parentID *string) ([]models.RecipeNode, error) {
	var nodes []models.RecipeNode
	query := s.db.Preload("Recipe").Preload("Recipe.Ingredients")
	
	if parentID == nil || *parentID == "" {
		// Корневой уровень - узлы без родителя
		query = query.Where("parent_id IS NULL")
	} else {
		query = query.Where("parent_id = ?", *parentID)
	}
	
	// Подсчитываем количество дочерних элементов для каждого узла
	if err := query.Order("is_folder DESC, name ASC").Find(&nodes).Error; err != nil {
		return nil, fmt.Errorf("ошибка получения содержимого папки: %w", err)
	}
	
	// Подсчитываем количество дочерних элементов
	for i := range nodes {
		var count int64
		s.db.Model(&models.RecipeNode{}).Where("parent_id = ?", nodes[i].ID).Count(&count)
		nodes[i].ChildrenCount = int(count)
	}
	
	return nodes, nil
}

// CreateNode создает новый узел (папку или рецепт)
func (s *RecipeService) CreateNode(name string, parentID *string, isFolder bool, recipeID *string) (*models.RecipeNode, error) {
	// Валидация
	if name == "" {
		return nil, fmt.Errorf("название узла не может быть пустым")
	}
	
	// Если это не папка, должен быть указан recipeID
	if !isFolder && recipeID == nil {
		return nil, fmt.Errorf("для узла-рецепта должен быть указан recipe_id")
	}
	
	// Если это папка, recipeID должен быть NULL
	if isFolder && recipeID != nil {
		return nil, fmt.Errorf("для папки не должен быть указан recipe_id")
	}
	
	// Проверяем, что родитель существует (если указан)
	if parentID != nil && *parentID != "" {
		var parent models.RecipeNode
		if err := s.db.First(&parent, "id = ?", *parentID).Error; err != nil {
			return nil, fmt.Errorf("родительская папка не найдена: %w", err)
		}
		
		// Проверяем, что родитель - это папка
		if !parent.IsFolder {
			return nil, fmt.Errorf("родитель должен быть папкой")
		}
	}
	
	// Проверяем уникальность имени в той же папке
	var existingNode models.RecipeNode
	query := s.db.Where("name = ?", name)
	if parentID == nil || *parentID == "" {
		query = query.Where("parent_id IS NULL")
	} else {
		query = query.Where("parent_id = ?", *parentID)
	}
	
	if err := query.First(&existingNode).Error; err == nil {
		return nil, fmt.Errorf("узел с таким именем уже существует в этой папке")
	}
	
	// Создаем узел
	node := &models.RecipeNode{
		Name:     name,
		ParentID: parentID,
		IsFolder: isFolder,
		RecipeID: recipeID,
	}
	
	if err := s.db.Create(node).Error; err != nil {
		return nil, fmt.Errorf("ошибка создания узла: %w", err)
	}
	
	// Загружаем связанные данные
	if err := s.db.Preload("Recipe").Preload("Recipe.Ingredients").First(node, "id = ?", node.ID).Error; err != nil {
		return nil, fmt.Errorf("ошибка загрузки созданного узла: %w", err)
	}
	
	log.Printf("✅ Создан узел: %s (ID: %s, IsFolder: %v)", node.Name, node.ID, node.IsFolder)
	return node, nil
}

// GetNodePath возвращает путь к узлу (массив родительских узлов)
func (s *RecipeService) GetNodePath(nodeID string) ([]models.RecipeNode, error) {
	var path []models.RecipeNode
	var node models.RecipeNode
	
	if err := s.db.First(&node, "id = ?", nodeID).Error; err != nil {
		return nil, fmt.Errorf("узел не найден: %w", err)
	}
	
	// Собираем путь от текущего узла до корня
	currentID := node.ParentID
	for currentID != nil {
		var parent models.RecipeNode
		if err := s.db.First(&parent, "id = ?", *currentID).Error; err != nil {
			break
		}
		path = append([]models.RecipeNode{parent}, path...)
		currentID = parent.ParentID
	}
	
	return path, nil
}

// UpdateNode обновляет узел (например, parent_id для перемещения)
func (s *RecipeService) UpdateNode(nodeID string, updates map[string]interface{}) (*models.RecipeNode, error) {
	var node models.RecipeNode
	if err := s.db.First(&node, "id = ?", nodeID).Error; err != nil {
		return nil, fmt.Errorf("узел не найден: %w", err)
	}
	
	// Если обновляется parent_id, проверяем, что новый родитель существует и это папка
	if newParentID, ok := updates["parent_id"].(*string); ok && newParentID != nil {
		var parent models.RecipeNode
		if err := s.db.First(&parent, "id = ?", *newParentID).Error; err != nil {
			return nil, fmt.Errorf("родительская папка не найдена: %w", err)
		}
		if !parent.IsFolder {
			return nil, fmt.Errorf("родитель должен быть папкой")
		}
		// Проверяем, что не перемещаем папку в саму себя или в свою дочернюю папку
		if nodeID == *newParentID {
			return nil, fmt.Errorf("нельзя переместить папку в саму себя")
		}
		// Проверяем циклические зависимости
		path, _ := s.GetNodePath(*newParentID)
		for _, p := range path {
			if p.ID == nodeID {
				return nil, fmt.Errorf("нельзя переместить папку в свою дочернюю папку")
			}
		}
	}
	
	// Обновляем поля
	if err := s.db.Model(&node).Updates(updates).Error; err != nil {
		return nil, fmt.Errorf("ошибка обновления узла: %w", err)
	}
	
	// Загружаем обновленный узел
	if err := s.db.First(&node, "id = ?", nodeID).Error; err != nil {
		return nil, fmt.Errorf("ошибка загрузки обновленного узла: %w", err)
	}
	
	log.Printf("✅ Обновлен узел: %s (ID: %s)", node.Name, nodeID)
	return &node, nil
}

// DeleteNode удаляет узел (soft delete)
func (s *RecipeService) DeleteNode(nodeID string) error {
	// Проверяем, что узел существует
	var node models.RecipeNode
	if err := s.db.First(&node, "id = ?", nodeID).Error; err != nil {
		return fmt.Errorf("узел не найден: %w", err)
	}
	
	// Если это папка, проверяем, что она пуста
	if node.IsFolder {
		var count int64
		s.db.Model(&models.RecipeNode{}).Where("parent_id = ?", nodeID).Count(&count)
		if count > 0 {
			return fmt.Errorf("нельзя удалить папку, содержащую элементы")
		}
	}
	
	if err := s.db.Delete(&node).Error; err != nil {
		return fmt.Errorf("ошибка удаления узла: %w", err)
	}
	
	log.Printf("✅ Удален узел: %s (ID: %s)", node.Name, nodeID)
	return nil
}

// OrphanedIngredient представляет "осиротевший" ингредиент
type OrphanedIngredient struct {
	IngredientID      string  `json:"ingredient_id"`
	RecipeID          string  `json:"recipe_id"`
	RecipeName        string  `json:"recipe_name"`
	NomenclatureID    *string `json:"nomenclature_id"`
	IngredientName    string  `json:"ingredient_name"`
	Quantity          float64 `json:"quantity"`
	Unit              string  `json:"unit"`
	IssueType         string  `json:"issue_type"` // "not_found", "deleted", "inactive"
	IssueDescription  string  `json:"issue_description"`
}

// FindOrphanedIngredients находит все "осиротевшие" ингредиенты, которые ссылаются на несуществующие,
// удаленные или неактивные товары в номенклатуре
func (s *RecipeService) FindOrphanedIngredients() ([]OrphanedIngredient, error) {
	var orphaned []OrphanedIngredient

	// Загружаем все ингредиенты с номенклатурой
	var ingredients []models.RecipeIngredient
	if err := s.db.Preload("Nomenclature").
		Where("nomenclature_id IS NOT NULL").
		Find(&ingredients).Error; err != nil {
		return nil, fmt.Errorf("ошибка загрузки ингредиентов: %w", err)
	}

	// Кэшируем рецепты для избежания множественных запросов
	recipeCache := make(map[string]*models.Recipe)

	for _, ingredient := range ingredients {
		if ingredient.NomenclatureID == nil {
			continue // Пропускаем полуфабрикаты
		}

		// Загружаем рецепт из кэша или БД
		var recipe *models.Recipe
		if cachedRecipe, ok := recipeCache[ingredient.RecipeID]; ok {
			recipe = cachedRecipe
		} else {
			var r models.Recipe
			if err := s.db.First(&r, "id = ?", ingredient.RecipeID).Error; err == nil {
				recipe = &r
				recipeCache[ingredient.RecipeID] = &r
			}
		}

		// Проверяем различные проблемы
		if ingredient.Nomenclature == nil {
			// Номенклатура не загружена - значит не существует
			orphaned = append(orphaned, OrphanedIngredient{
				IngredientID:     ingredient.ID,
				RecipeID:         ingredient.RecipeID,
				RecipeName:       getRecipeName(recipe),
				NomenclatureID:   ingredient.NomenclatureID,
				IngredientName:   "Неизвестный товар",
				Quantity:         ingredient.Quantity,
				Unit:             ingredient.Unit,
				IssueType:        "not_found",
				IssueDescription: fmt.Sprintf("Номенклатура с ID %s не найдена в базе данных", *ingredient.NomenclatureID),
			})
			continue
		}

		nomenclature := *ingredient.Nomenclature

		// Проверяем, удалена ли номенклатура (soft delete)
		if nomenclature.DeletedAt.Valid {
			orphaned = append(orphaned, OrphanedIngredient{
				IngredientID:     ingredient.ID,
				RecipeID:         ingredient.RecipeID,
				RecipeName:       getRecipeName(recipe),
				NomenclatureID:   ingredient.NomenclatureID,
				IngredientName:   nomenclature.Name,
				Quantity:         ingredient.Quantity,
				Unit:             ingredient.Unit,
				IssueType:        "deleted",
				IssueDescription: fmt.Sprintf("Номенклатура '%s' была удалена (deleted_at: %s)", nomenclature.Name, nomenclature.DeletedAt.Time.Format("2006-01-02")),
			})
			continue
		}

		// Проверяем, активна ли номенклатура
		if !nomenclature.IsActive {
			orphaned = append(orphaned, OrphanedIngredient{
				IngredientID:     ingredient.ID,
				RecipeID:         ingredient.RecipeID,
				RecipeName:       getRecipeName(recipe),
				NomenclatureID:   ingredient.NomenclatureID,
				IngredientName:   nomenclature.Name,
				Quantity:         ingredient.Quantity,
				Unit:             ingredient.Unit,
				IssueType:        "inactive",
				IssueDescription: fmt.Sprintf("Номенклатура '%s' неактивна (is_active = false)", nomenclature.Name),
			})
			continue
		}

		// Проверяем совместимость единиц измерения
		if !s.isUnitCompatible(ingredient.Unit, nomenclature) {
			orphaned = append(orphaned, OrphanedIngredient{
				IngredientID:     ingredient.ID,
				RecipeID:         ingredient.RecipeID,
				RecipeName:       getRecipeName(recipe),
				NomenclatureID:   ingredient.NomenclatureID,
				IngredientName:   nomenclature.Name,
				Quantity:         ingredient.Quantity,
				Unit:             ingredient.Unit,
				IssueType:        "unit_mismatch",
				IssueDescription: fmt.Sprintf("Единица измерения '%s' несовместима с номенклатурой '%s' (поддерживаемые: %s, %s, %s)",
					ingredient.Unit, nomenclature.Name, nomenclature.BaseUnit, nomenclature.InboundUnit, nomenclature.ProductionUnit),
			})
		}
	}

	return orphaned, nil
}

// getRecipeName безопасно получает имя рецепта
func getRecipeName(recipe *models.Recipe) string {
	if recipe == nil {
		return "Неизвестный рецепт"
	}
	if recipe.Name == "" {
		return "Без названия"
	}
	return recipe.Name
}

