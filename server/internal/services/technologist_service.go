package services

import (
	"encoding/json"
	"fmt"
	"log"

	"zephyrvpn/server/internal/models"

	"gorm.io/gorm"
)

// TechnologistService управляет модулем Technologist Workspace
type TechnologistService struct {
	db *gorm.DB
}

// NewTechnologistService создает новый сервис технолога
func NewTechnologistService(db *gorm.DB) *TechnologistService {
	return &TechnologistService{
		db: db,
	}
}

// GetDB возвращает экземпляр БД для прямых запросов
func (ts *TechnologistService) GetDB() *gorm.DB {
	return ts.db
}

// ProductionDashboardData представляет данные для Production Dashboard (фокус на качестве и экономике)
type ProductionDashboardData struct {
	// Статистика рецептов
	ActiveRecipesCount   int                   `json:"active_recipes_count"`
	SemiFinishedCount    int                   `json:"semi_finished_count"`
	FinishedGoodsCount   int                   `json:"finished_goods_count"`
	
	// Food Cost Variance (колебания цен на ключевые ингредиенты)
	FoodCostVariance    []FoodCostVarianceItem `json:"food_cost_variance"`
	
	// Waste & Loss Report (топ-5 товаров с наибольшими потерями за 24ч)
	WasteLossReport     []WasteLossItem        `json:"waste_loss_report"`
	
	// LMS Readiness (% сотрудников, сдавших экзамены по рецептам текущего меню)
	LMSReadiness        LMSReadinessData       `json:"lms_readiness"`
	
	// Recipe Integrity (рецепты с проблемами)
	RecipeIntegrity     RecipeIntegrityData    `json:"recipe_integrity"`
	
	// Critical Stock Alert (только критические товары ниже минимума)
	CriticalStockAlerts []CriticalStockAlert   `json:"critical_stock_alerts"`
	
	// Action Required (товары, требующие внимания)
	ActionRequired     []ActionRequiredItem   `json:"action_required"`
}

type FoodCostVarianceItem struct {
	NomenclatureID   string  `json:"nomenclature_id"`
	Name             string  `json:"name"`
	CurrentPrice     float64 `json:"current_price"`
	PreviousPrice    float64 `json:"previous_price"`
	PriceChange      float64 `json:"price_change"`      // Абсолютное изменение
	PriceChangePercent float64 `json:"price_change_percent"` // Процентное изменение
	Unit             string  `json:"unit"`
	IsKeyIngredient  bool    `json:"is_key_ingredient"` // Используется в >3 рецептах
}

type WasteLossItem struct {
	NomenclatureID   string  `json:"nomenclature_id"`
	Name             string  `json:"name"`
	WasteQuantity    float64 `json:"waste_quantity"`
	Unit             string  `json:"unit"`
	WasteValue       float64 `json:"waste_value"` // Стоимость потерь в рублях
	MovementCount    int     `json:"movement_count"` // Количество записей о потерях
}

type LMSReadinessData struct {
	TotalStaff        int     `json:"total_staff"`
	StaffWithExams    int     `json:"staff_with_exams"`
	ReadinessPercent  float64 `json:"readiness_percent"`
	MenuRecipesCount  int     `json:"menu_recipes_count"`
	RecipesWithExams  int     `json:"recipes_with_exams"`
}

type RecipeIntegrityData struct {
	TotalDishes     int                  `json:"total_dishes"`      // Всего блюд в номенклатуре (IsSaleable=true)
	ExistingRecipes int                  `json:"existing_recipes"` // Существующих рецептов в БД
	ValidRecipes    int                  `json:"valid_recipes"`     // Валидных рецептов (с ингредиентами)
	InvalidRecipes  []RecipeIntegrityIssue `json:"invalid_recipes"`
	IsComplete      bool                 `json:"is_complete"`      // true если соотношение 1/1
}

type RecipeIntegrityIssue struct {
	RecipeID          string   `json:"recipe_id"`
	RecipeName        string   `json:"recipe_name"`
	Issues            []string `json:"issues"` // Список проблем
	AffectedIngredients []string `json:"affected_ingredients"` // Названия проблемных ингредиентов
}

type CriticalStockAlert struct {
	NomenclatureID   string  `json:"nomenclature_id"`
	Name             string  `json:"name"`
	CurrentStock     float64 `json:"current_stock"`
	MinStockLevel    float64 `json:"min_stock_level"`
	ShortageQty      float64 `json:"shortage_qty"`
	Unit             string  `json:"unit"`
	AffectedRecipes  []string `json:"affected_recipes"` // Рецепты, которые используют этот товар
}

type ActionRequiredItem struct {
	NomenclatureID   string   `json:"nomenclature_id"`
	Name             string   `json:"name"`
	SKU              string   `json:"sku"`
	CategoryName     string   `json:"category_name"`
	Issue            string   `json:"issue"` // "missing_recipe", "missing_ingredients", "inactive_recipe"
	IssueDescription string   `json:"issue_description"`
	RecipeID         *string  `json:"recipe_id,omitempty"` // Если рецепт существует, но неполный
	RecipeName       *string  `json:"recipe_name,omitempty"`
}

type RawMaterialStock struct {
	NomenclatureID   string  `json:"nomenclature_id"`
	Name             string  `json:"name"`
	CurrentStock     float64 `json:"current_stock"`
	Unit             string  `json:"unit"`
	MinStockLevel    float64 `json:"min_stock_level"`
	Status           string  `json:"status"` // "sufficient", "low", "critical"
}

type PlannedProduction struct {
	RecipeID    string  `json:"recipe_id"`
	RecipeName  string  `json:"recipe_name"`
	Quantity    float64 `json:"quantity"`
	Unit        string  `json:"unit"`
	RequiredRawMaterials []RequiredMaterial `json:"required_raw_materials"`
}

type RequiredMaterial struct {
	NomenclatureID string  `json:"nomenclature_id"`
	Name           string  `json:"name"`
	RequiredQty    float64 `json:"required_qty"`
	AvailableQty   float64 `json:"available_qty"`
	Unit           string  `json:"unit"`
	Status         string  `json:"status"` // "sufficient", "insufficient"
}

type StockShortage struct {
	NomenclatureID string  `json:"nomenclature_id"`
	Name           string  `json:"name"`
	RequiredQty    float64 `json:"required_qty"`
	AvailableQty   float64 `json:"available_qty"`
	ShortageQty    float64 `json:"shortage_qty"`
	Unit           string  `json:"unit"`
	AffectedRecipes []string `json:"affected_recipes"`
}

// GetProductionDashboard возвращает данные для Production Dashboard (фокус на качестве и экономике)
func (ts *TechnologistService) GetProductionDashboard(branchID string) (*ProductionDashboardData, error) {
	dashboard := &ProductionDashboardData{}

	// 1. Подсчитываем рецепты
	var activeRecipesCount, semiFinishedCount, finishedGoodsCount int64
	ts.db.Model(&models.Recipe{}).
		Where("is_active = true AND deleted_at IS NULL").
		Count(&activeRecipesCount)
	
	ts.db.Model(&models.Recipe{}).
		Where("is_active = true AND is_semi_finished = true AND deleted_at IS NULL").
		Count(&semiFinishedCount)
	
	ts.db.Model(&models.Recipe{}).
		Where("is_active = true AND is_semi_finished = false AND deleted_at IS NULL").
		Count(&finishedGoodsCount)

	dashboard.ActiveRecipesCount = int(activeRecipesCount)
	dashboard.SemiFinishedCount = int(semiFinishedCount)
	dashboard.FinishedGoodsCount = int(finishedGoodsCount)

	// 2. Food Cost Variance (колебания цен на ключевые ингредиенты)
	foodCostVariance, err := ts.getFoodCostVariance(branchID)
	if err != nil {
		log.Printf("⚠️ Ошибка получения Food Cost Variance: %v", err)
		dashboard.FoodCostVariance = []FoodCostVarianceItem{}
	} else {
		dashboard.FoodCostVariance = foodCostVariance
	}

	// 3. Waste & Loss Report (топ-5 товаров с наибольшими потерями за 24ч)
	wasteLoss, err := ts.getWasteLossReport(branchID)
	if err != nil {
		log.Printf("⚠️ Ошибка получения Waste & Loss Report: %v", err)
		dashboard.WasteLossReport = []WasteLossItem{}
	} else {
		dashboard.WasteLossReport = wasteLoss
	}

	// 4. LMS Readiness (% сотрудников, сдавших экзамены по рецептам текущего меню)
	lmsReadiness, err := ts.getLMSReadiness(branchID)
	if err != nil {
		log.Printf("⚠️ Ошибка получения LMS Readiness: %v", err)
		dashboard.LMSReadiness = LMSReadinessData{}
	} else {
		dashboard.LMSReadiness = lmsReadiness
	}

	// 5. Recipe Integrity (проверка рецептов на неактивные/отсутствующие товары)
	recipeIntegrity, err := ts.getRecipeIntegrity(branchID)
	if err != nil {
		log.Printf("⚠️ Ошибка получения Recipe Integrity: %v", err)
		dashboard.RecipeIntegrity = RecipeIntegrityData{}
	} else {
		dashboard.RecipeIntegrity = recipeIntegrity
	}

	// 6. Critical Stock Alert (только критические товары ниже минимума)
	criticalStock, err := ts.getCriticalStockAlerts(branchID)
	if err != nil {
		log.Printf("⚠️ Ошибка получения Critical Stock Alerts: %v", err)
		dashboard.CriticalStockAlerts = []CriticalStockAlert{}
	} else {
		dashboard.CriticalStockAlerts = criticalStock
	}

	// 7. Action Required (товары, требующие внимания)
	actionRequired, err := ts.getActionRequired()
	if err != nil {
		log.Printf("⚠️ Ошибка получения Action Required: %v", err)
		dashboard.ActionRequired = []ActionRequiredItem{}
	} else {
		dashboard.ActionRequired = actionRequired
	}

	return dashboard, nil
}

// CreateRecipeVersion создает новую версию рецепта при изменении
func (ts *TechnologistService) CreateRecipeVersion(recipeID string, changedBy string, changeReason string) error {
	// Загружаем текущий рецепт с ингредиентами
	var recipe models.Recipe
	if err := ts.db.Preload("Ingredients").First(&recipe, "id = ?", recipeID).Error; err != nil {
		return fmt.Errorf("рецепт не найден: %w", err)
	}

	// Определяем номер следующей версии
	var maxVersion int
	if err := ts.db.Model(&models.RecipeVersion{}).
		Where("recipe_id = ?", recipeID).
		Select("COALESCE(MAX(version), 0)").
		Scan(&maxVersion).Error; err != nil {
		return fmt.Errorf("ошибка определения версии: %w", err)
	}

	// Сериализуем ингредиенты в JSON
	ingredientsJSON, err := json.Marshal(recipe.Ingredients)
	if err != nil {
		return fmt.Errorf("ошибка сериализации ингредиентов: %w", err)
	}

	// Создаем версию
	version := &models.RecipeVersion{
		RecipeID:        recipeID,
		Version:         maxVersion + 1,
		ChangedBy:       changedBy,
		ChangeReason:    changeReason,
		IngredientsJSON: string(ingredientsJSON),
	}

	if err := ts.db.Create(version).Error; err != nil {
		return fmt.Errorf("ошибка создания версии: %w", err)
	}

	log.Printf("✅ Создана версия %d для рецепта %s (изменено: %s)", version.Version, recipe.Name, changedBy)
	return nil
}

// GetRecipeVersions возвращает все версии рецепта
func (ts *TechnologistService) GetRecipeVersions(recipeID string) ([]models.RecipeVersion, error) {
	var versions []models.RecipeVersion
	if err := ts.db.Where("recipe_id = ?", recipeID).
		Order("version DESC").
		Find(&versions).Error; err != nil {
		return nil, fmt.Errorf("ошибка загрузки версий: %w", err)
	}
	return versions, nil
}

// GetRecipeUsageTree возвращает дерево использования рецепта (какие рецепты используют этот)
func (ts *TechnologistService) GetRecipeUsageTree(recipeID string) (*models.RecipeUsageTree, error) {
	var recipe models.Recipe
	if err := ts.db.First(&recipe, "id = ?", recipeID).Error; err != nil {
		return nil, fmt.Errorf("рецепт не найден: %w", err)
	}

	tree := &models.RecipeUsageTree{
		RecipeID:       recipe.ID,
		RecipeName:     recipe.Name,
		IsSemiFinished: recipe.IsSemiFinished,
		UsedIn:         []models.RecipeUsageTree{},
	}

	// Находим все рецепты, которые используют этот как ингредиент
	var usingRecipes []models.Recipe
	if err := ts.db.Joins("JOIN recipe_ingredients ON recipes.id = recipe_ingredients.recipe_id").
		Where("recipe_ingredients.ingredient_recipe_id = ?", recipeID).
		Find(&usingRecipes).Error; err != nil {
		return nil, fmt.Errorf("ошибка загрузки использующих рецептов: %w", err)
	}

	// Рекурсивно строим дерево
	for _, usingRecipe := range usingRecipes {
		subTree, err := ts.GetRecipeUsageTree(usingRecipe.ID)
		if err != nil {
			log.Printf("⚠️ Ошибка загрузки поддерева для %s: %v", usingRecipe.Name, err)
			continue
		}
		tree.UsedIn = append(tree.UsedIn, *subTree)
	}

	return tree, nil
}

// CreateTrainingMaterial создает обучающий материал
func (ts *TechnologistService) CreateTrainingMaterial(material *models.TrainingMaterial) error {
	if err := ts.db.Create(material).Error; err != nil {
		return fmt.Errorf("ошибка создания обучающего материала: %w", err)
	}
	return nil
}

// GetTrainingMaterials возвращает обучающие материалы для рецепта
func (ts *TechnologistService) GetTrainingMaterials(recipeID string) ([]models.TrainingMaterial, error) {
	var materials []models.TrainingMaterial
	if err := ts.db.Where("recipe_id = ? AND is_active = true", recipeID).
		Order("\"order\" ASC").
		Find(&materials).Error; err != nil {
		return nil, fmt.Errorf("ошибка загрузки материалов: %w", err)
	}
	return materials, nil
}

// CreateRecipeExam создает запись об экзамене
func (ts *TechnologistService) CreateRecipeExam(exam *models.RecipeExam) error {
	// Проверяем, не существует ли уже экзамен
	var existing models.RecipeExam
	if err := ts.db.Where("recipe_id = ? AND staff_id = ?", exam.RecipeID, exam.StaffID).
		First(&existing).Error; err == nil {
		// Обновляем существующий
		exam.ID = existing.ID
		return ts.db.Save(exam).Error
	}

	if err := ts.db.Create(exam).Error; err != nil {
		return fmt.Errorf("ошибка создания экзамена: %w", err)
	}
	return nil
}

// GetRecipeExams возвращает экзамены по рецепту
func (ts *TechnologistService) GetRecipeExams(recipeID string) ([]models.RecipeExam, error) {
	var exams []models.RecipeExam
	if err := ts.db.Preload("Staff").
		Where("recipe_id = ?", recipeID).
		Order("created_at DESC").
		Find(&exams).Error; err != nil {
		return nil, fmt.Errorf("ошибка загрузки экзаменов: %w", err)
	}
	return exams, nil
}

// GetStaffRecipeExams возвращает экзамены сотрудника
func (ts *TechnologistService) GetStaffRecipeExams(staffID string) ([]models.RecipeExam, error) {
	var exams []models.RecipeExam
	if err := ts.db.Preload("Recipe").
		Where("staff_id = ?", staffID).
		Order("created_at DESC").
		Find(&exams).Error; err != nil {
		return nil, fmt.Errorf("ошибка загрузки экзаменов: %w", err)
	}
	return exams, nil
}

// getFoodCostVariance возвращает колебания цен на ключевые ингредиенты
func (ts *TechnologistService) getFoodCostVariance(branchID string) ([]FoodCostVarianceItem, error) {
	// Находим ингредиенты, которые используются в >3 рецептах (ключевые)
	var keyIngredients []struct {
		NomenclatureID string
		RecipeCount    int64
	}
	
	if err := ts.db.Model(&models.RecipeIngredient{}).
		Select("nomenclature_id, COUNT(DISTINCT recipe_id) as recipe_count").
		Where("nomenclature_id IS NOT NULL").
		Group("nomenclature_id").
		Having("COUNT(DISTINCT recipe_id) > 3").
		Scan(&keyIngredients).Error; err != nil {
		return nil, fmt.Errorf("ошибка поиска ключевых ингредиентов: %w", err)
	}

	var variance []FoodCostVarianceItem
	for _, keyIng := range keyIngredients {
		var item models.NomenclatureItem
		if err := ts.db.First(&item, "id = ?", keyIng.NomenclatureID).Error; err != nil {
			continue
		}

		// Получаем текущую цену (LastPrice из номенклатуры или последняя CostPerUnit из StockBatch)
		currentPrice := item.LastPrice
		
		// Получаем предыдущую цену из последней партии (StockBatch) за последние 30 дней
		var previousPrice float64
		var lastBatch models.StockBatch
		if err := ts.db.Where("nomenclature_id = ? AND branch_id = ? AND created_at < NOW() - INTERVAL '30 days'", item.ID, branchID).
			Order("created_at DESC").
			First(&lastBatch).Error; err == nil {
			previousPrice = lastBatch.CostPerUnit
		} else {
			previousPrice = currentPrice // Если нет истории, используем текущую цену
		}

		priceChange := currentPrice - previousPrice
		var priceChangePercent float64
		if previousPrice > 0 {
			priceChangePercent = (priceChange / previousPrice) * 100
		}

		variance = append(variance, FoodCostVarianceItem{
			NomenclatureID:    item.ID,
			Name:              item.Name,
			CurrentPrice:      currentPrice,
			PreviousPrice:     previousPrice,
			PriceChange:       priceChange,
			PriceChangePercent: priceChangePercent,
			Unit:              item.BaseUnit,
			IsKeyIngredient:   true,
		})
	}

	return variance, nil
}

// getWasteLossReport возвращает топ-5 товаров с наибольшими потерями за последние 24 часа
func (ts *TechnologistService) getWasteLossReport(branchID string) ([]WasteLossItem, error) {
	var wasteItems []WasteLossItem

	// Агрегируем движения типа 'waste' и 'adjustment' (с отрицательным количеством) за последние 24ч
	var wasteMovements []struct {
		NomenclatureID string
		TotalWaste     float64
		MovementCount  int64
	}

	if err := ts.db.Model(&models.StockMovement{}).
		Select("nomenclature_id, ABS(SUM(quantity)) as total_waste, COUNT(*) as movement_count").
		Where("branch_id = ? AND movement_type IN ('waste', 'adjustment') AND quantity < 0 AND created_at >= NOW() - INTERVAL '24 hours'", branchID).
		Group("nomenclature_id").
		Order("total_waste DESC").
		Limit(5).
		Scan(&wasteMovements).Error; err != nil {
		return nil, fmt.Errorf("ошибка получения данных о потерях: %w", err)
	}

	for _, wm := range wasteMovements {
		var item models.NomenclatureItem
		if err := ts.db.First(&item, "id = ?", wm.NomenclatureID).Error; err != nil {
			continue
		}

		// Рассчитываем стоимость потерь
		wasteValue := wm.TotalWaste * item.LastPrice

		wasteItems = append(wasteItems, WasteLossItem{
			NomenclatureID: item.ID,
			Name:           item.Name,
			WasteQuantity:  wm.TotalWaste,
			Unit:           item.BaseUnit,
			WasteValue:     wasteValue,
			MovementCount:  int(wm.MovementCount),
		})
	}

	return wasteItems, nil
}

// getLMSReadiness возвращает процент сотрудников, сдавших экзамены по рецептам текущего меню
func (ts *TechnologistService) getLMSReadiness(branchID string) (LMSReadinessData, error) {
	var data LMSReadinessData

	// Подсчитываем общее количество сотрудников
	var totalStaff int64
	if err := ts.db.Model(&models.Staff{}).
		Where("branch_id = ? AND status = ?", branchID, models.StatusActive).
		Count(&totalStaff).Error; err != nil {
		return data, fmt.Errorf("ошибка подсчета сотрудников: %w", err)
	}
	data.TotalStaff = int(totalStaff)

	// Находим рецепты, которые есть в меню (IsSemiFinished = false, IsActive = true)
	var menuRecipes []models.Recipe
	if err := ts.db.Where("is_semi_finished = false AND is_active = true AND deleted_at IS NULL").
		Find(&menuRecipes).Error; err != nil {
		return data, fmt.Errorf("ошибка загрузки рецептов меню: %w", err)
	}
	data.MenuRecipesCount = len(menuRecipes)

	// Подсчитываем, сколько рецептов имеют хотя бы один экзамен
	var recipesWithExams int64
	if err := ts.db.Model(&models.RecipeExam{}).
		Distinct("recipe_id").
		Where("recipe_id IN (SELECT id FROM recipes WHERE is_semi_finished = false AND is_active = true AND deleted_at IS NULL)").
		Count(&recipesWithExams).Error; err != nil {
		log.Printf("⚠️ Ошибка подсчета рецептов с экзаменами: %v", err)
	}
	data.RecipesWithExams = int(recipesWithExams)

	// Подсчитываем сотрудников, которые сдали хотя бы один экзамен по рецептам меню
	var staffWithExams int64
	if err := ts.db.Model(&models.RecipeExam{}).
		Distinct("staff_id").
		Where("recipe_id IN (SELECT id FROM recipes WHERE is_semi_finished = false AND is_active = true AND deleted_at IS NULL) AND status = 'passed'").
		Count(&staffWithExams).Error; err != nil {
		log.Printf("⚠️ Ошибка подсчета сотрудников с экзаменами: %v", err)
	}
	data.StaffWithExams = int(staffWithExams)

	// Рассчитываем процент готовности
	if data.TotalStaff > 0 {
		data.ReadinessPercent = (float64(data.StaffWithExams) / float64(data.TotalStaff)) * 100
	}

	return data, nil
}

// getRecipeIntegrity проверяет рецепты на проблемы с ингредиентами
func (ts *TechnologistService) getRecipeIntegrity(branchID string) (RecipeIntegrityData, error) {
	var data RecipeIntegrityData

	// 1. Считаем общее количество блюд в номенклатуре (IsSaleable=true)
	var totalDishesCount int64
	if err := ts.db.Model(&models.NomenclatureItem{}).
		Where("is_saleable = true AND is_active = true AND deleted_at IS NULL").
		Count(&totalDishesCount).Error; err != nil {
		return data, fmt.Errorf("ошибка подсчета блюд в номенклатуре: %w", err)
	}
	data.TotalDishes = int(totalDishesCount)

	// 2. Считаем существующие рецепты (готовые блюда, не полуфабрикаты)
	var existingRecipesCount int64
	if err := ts.db.Model(&models.Recipe{}).
		Where("is_active = true AND is_semi_finished = false AND deleted_at IS NULL").
		Count(&existingRecipesCount).Error; err != nil {
		return data, fmt.Errorf("ошибка подсчета рецептов: %w", err)
	}
	data.ExistingRecipes = int(existingRecipesCount)

	// 3. Загружаем все активные рецепты с ингредиентами для проверки валидности
	var recipes []models.Recipe
	if err := ts.db.Where("is_active = true AND deleted_at IS NULL").
		Preload("Ingredients").
		Preload("Ingredients.Nomenclature").
		Find(&recipes).Error; err != nil {
		return data, fmt.Errorf("ошибка загрузки рецептов: %w", err)
	}

	data.ValidRecipes = 0

	for _, recipe := range recipes {
		var issues []string
		var affectedIngredients []string
		isValid := true

		for _, ing := range recipe.Ingredients {
			// Проверка 1: Если ингредиент - номенклатура, проверяем её существование и активность
			if ing.NomenclatureID != nil {
				var nom models.NomenclatureItem
				if err := ts.db.First(&nom, "id = ?", *ing.NomenclatureID).Error; err != nil {
					issues = append(issues, fmt.Sprintf("Ингредиент '%s' не найден в номенклатуре", ing.Nomenclature.Name))
					affectedIngredients = append(affectedIngredients, ing.Nomenclature.Name)
					isValid = false
				} else if !nom.IsActive {
					issues = append(issues, fmt.Sprintf("Ингредиент '%s' неактивен", nom.Name))
					affectedIngredients = append(affectedIngredients, nom.Name)
					isValid = false
				} else {
					// Проверка 2: Проверяем остатки на складе (если критично)
					var totalStock float64
					ts.db.Model(&models.StockBatch{}).
						Where("nomenclature_id = ? AND branch_id = ? AND remaining_quantity > 0", nom.ID, branchID).
						Select("COALESCE(SUM(remaining_quantity), 0)").
						Scan(&totalStock)
					
					if totalStock == 0 {
						issues = append(issues, fmt.Sprintf("Ингредиент '%s' отсутствует на складе", nom.Name))
						affectedIngredients = append(affectedIngredients, nom.Name)
						// Не помечаем как невалидный, т.к. это не критично для целостности рецепта
					}
				}
			}

			// Проверка 3: Если ингредиент - полуфабрикат (IngredientRecipeID), проверяем его существование
			if ing.IngredientRecipeID != nil {
				var subRecipe models.Recipe
				if err := ts.db.First(&subRecipe, "id = ?", *ing.IngredientRecipeID).Error; err != nil {
					issues = append(issues, fmt.Sprintf("Полуфабрикат '%s' не найден", ing.IngredientRecipe.Name))
					if ing.IngredientRecipe != nil {
						affectedIngredients = append(affectedIngredients, ing.IngredientRecipe.Name)
					}
					isValid = false
				} else if !subRecipe.IsActive {
					issues = append(issues, fmt.Sprintf("Полуфабрикат '%s' неактивен", subRecipe.Name))
					affectedIngredients = append(affectedIngredients, subRecipe.Name)
					isValid = false
				}
			}
		}

		if isValid && len(recipe.Ingredients) > 0 {
			data.ValidRecipes++
		} else if len(issues) > 0 {
			data.InvalidRecipes = append(data.InvalidRecipes, RecipeIntegrityIssue{
				RecipeID:            recipe.ID,
				RecipeName:          recipe.Name,
				Issues:              issues,
				AffectedIngredients: affectedIngredients,
			})
		}
	}

	// 4. Проверяем, полное ли соотношение (1/1)
	data.IsComplete = (data.TotalDishes > 0 && data.ExistingRecipes == data.TotalDishes)

	return data, nil
}

// getCriticalStockAlerts возвращает только критические товары (ниже 50% от минимума)
func (ts *TechnologistService) getCriticalStockAlerts(branchID string) ([]CriticalStockAlert, error) {
	var alerts []CriticalStockAlert

	// Загружаем сырье (не IsSaleable) с минимальными уровнями
	var rawMaterials []models.NomenclatureItem
	if err := ts.db.Where("is_saleable = false AND is_active = true AND deleted_at IS NULL AND min_stock_level > 0").
		Find(&rawMaterials).Error; err != nil {
		return nil, fmt.Errorf("ошибка загрузки сырья: %w", err)
	}

	for _, material := range rawMaterials {
		var totalStock float64
		if err := ts.db.Model(&models.StockBatch{}).
			Where("nomenclature_id = ? AND branch_id = ? AND remaining_quantity > 0", material.ID, branchID).
			Select("COALESCE(SUM(remaining_quantity), 0)").
			Scan(&totalStock).Error; err != nil {
			log.Printf("⚠️ Ошибка расчета остатков для %s: %v", material.Name, err)
			totalStock = 0
		}

		// Критический уровень: ниже 50% от минимума
		criticalThreshold := material.MinStockLevel * 0.5
		if totalStock < criticalThreshold {
			shortageQty := material.MinStockLevel - totalStock

			// Находим рецепты, которые используют этот товар
			var affectedRecipes []string
			var recipeNames []struct {
				Name string
			}
			ts.db.Model(&models.Recipe{}).
				Joins("JOIN recipe_ingredients ON recipes.id = recipe_ingredients.recipe_id").
				Where("recipe_ingredients.nomenclature_id = ? AND recipes.is_active = true", material.ID).
				Select("DISTINCT recipes.name").
				Scan(&recipeNames)
			
			for _, rn := range recipeNames {
				affectedRecipes = append(affectedRecipes, rn.Name)
			}

			alerts = append(alerts, CriticalStockAlert{
				NomenclatureID: material.ID,
				Name:           material.Name,
				CurrentStock:   totalStock,
				MinStockLevel:  material.MinStockLevel,
				ShortageQty:    shortageQty,
				Unit:           material.BaseUnit,
				AffectedRecipes: affectedRecipes,
			})
		}
	}

	return alerts, nil
}

// getActionRequired возвращает список товаров, требующих внимания (нет рецепта или рецепт неполный)
func (ts *TechnologistService) getActionRequired() ([]ActionRequiredItem, error) {
	var items []ActionRequiredItem

	// Находим все товары типа "Dish" (IsSaleable=true), которые не готовы к продаже
	var saleableItems []models.NomenclatureItem
	if err := ts.db.Where("is_saleable = true AND is_ready_for_sale = false AND is_active = true AND deleted_at IS NULL").
		Find(&saleableItems).Error; err != nil {
		return nil, fmt.Errorf("ошибка загрузки товаров: %w", err)
	}

	for _, item := range saleableItems {
		// Проверяем, есть ли связанный Recipe
		var recipe models.Recipe
		if err := ts.db.Where("menu_item_id = ? AND is_active = true AND deleted_at IS NULL", item.ID).
			Preload("Ingredients").
			First(&recipe).Error; err != nil {
			// Рецепт не найден
			items = append(items, ActionRequiredItem{
				NomenclatureID:   item.ID,
				Name:             item.Name,
				SKU:              item.SKU,
				CategoryName:     item.CategoryName,
				Issue:            "missing_recipe",
				IssueDescription: "Отсутствует технологическая карта (Recipe)",
			})
		} else {
			// Рецепт найден, проверяем наличие ингредиентов
			if len(recipe.Ingredients) == 0 {
				items = append(items, ActionRequiredItem{
					NomenclatureID:   item.ID,
					Name:             item.Name,
					SKU:              item.SKU,
					CategoryName:     item.CategoryName,
					Issue:            "missing_ingredients",
					IssueDescription: "Технологическая карта пуста (нет ингредиентов)",
					RecipeID:         &recipe.ID,
					RecipeName:       &recipe.Name,
				})
			} else if !recipe.IsActive {
				items = append(items, ActionRequiredItem{
					NomenclatureID:   item.ID,
					Name:             item.Name,
					SKU:              item.SKU,
					CategoryName:     item.CategoryName,
					Issue:            "inactive_recipe",
					IssueDescription: "Технологическая карта неактивна",
					RecipeID:         &recipe.ID,
					RecipeName:       &recipe.Name,
				})
			}
		}
	}

	return items, nil
}

// ActivateForMenu активирует товар для меню (устанавливает is_ready_for_sale = true)
// Вызывается технологом после завершения работы над рецептом
func (ts *TechnologistService) ActivateForMenu(nomenclatureID string) error {
	// Проверяем, что товар существует и помечен как saleable
	var item models.NomenclatureItem
	if err := ts.db.Where("id = ? AND is_saleable = true AND is_active = true AND deleted_at IS NULL", nomenclatureID).
		First(&item).Error; err != nil {
		return fmt.Errorf("товар не найден или не помечен как saleable: %w", err)
	}

	// Проверяем, что есть связанный активный Recipe с ингредиентами
	var recipe models.Recipe
	if err := ts.db.Where("menu_item_id = ? AND is_active = true AND deleted_at IS NULL", nomenclatureID).
		Preload("Ingredients").
		First(&recipe).Error; err != nil {
		return fmt.Errorf("не найден активный Recipe для товара: %w", err)
	}

	if len(recipe.Ingredients) == 0 {
		return fmt.Errorf("Recipe не содержит ингредиентов. Добавьте ингредиенты перед активацией")
	}

	// Устанавливаем флаг готовности
	if err := ts.db.Model(&item).Update("is_ready_for_sale", true).Error; err != nil {
		return fmt.Errorf("ошибка обновления флага готовности: %w", err)
	}

	log.Printf("✅ Товар '%s' (ID: %s) активирован для меню", item.Name, item.ID)
	return nil
}

