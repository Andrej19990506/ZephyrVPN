package services

import (
	"fmt"
	"log"

	"zephyrvpn/server/internal/models"

	"gorm.io/gorm"
)

// RecipeService управляет рецептами и технологическими картами
type RecipeService struct {
	db            *gorm.DB
	stockService  *StockService
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

// GetRecipes возвращает список всех рецептов
func (s *RecipeService) GetRecipes(includeInactive bool) ([]models.Recipe, error) {
	var recipes []models.Recipe
	query := s.db.Preload("Ingredients").Preload("Ingredients.Nomenclature").Preload("Ingredients.IngredientRecipe")

	if !includeInactive {
		query = query.Where("is_active = ?", true)
	}

	if err := query.Find(&recipes).Error; err != nil {
		return nil, err
	}

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
func (s *RecipeService) CreateRecipe(recipe *models.Recipe) error {
	// Начинаем транзакцию
	tx := s.db.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// Сохраняем рецепт
	if err := tx.Create(recipe).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("ошибка создания рецепта: %w", err)
	}

	// Сохраняем ингредиенты
	for i := range recipe.Ingredients {
		recipe.Ingredients[i].RecipeID = recipe.ID
		if err := tx.Create(&recipe.Ingredients[i]).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("ошибка создания ингредиента: %w", err)
		}
	}

	if err := tx.Commit().Error; err != nil {
		return err
	}

	log.Printf("✅ Создан рецепт: %s (ID: %s)", recipe.Name, recipe.ID)
	return nil
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

	// Обновляем основные поля рецепта
	recipe.ID = recipeID
	if err := tx.Model(&existingRecipe).Updates(map[string]interface{}{
		"name":            recipe.Name,
		"description":    recipe.Description,
		"menu_item_id":    recipe.MenuItemID,
		"portion_size":    recipe.PortionSize,
		"unit":            recipe.Unit,
		"is_semi_finished": recipe.IsSemiFinished,
		"is_active":       recipe.IsActive,
	}).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("ошибка обновления рецепта: %w", err)
	}

	// Удаляем старые ингредиенты
	if err := tx.Where("recipe_id = ?", recipeID).Delete(&models.RecipeIngredient{}).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("ошибка удаления старых ингредиентов: %w", err)
	}

	// Создаем новые ингредиенты
	for i := range recipe.Ingredients {
		recipe.Ingredients[i].RecipeID = recipeID
		recipe.Ingredients[i].ID = "" // Сбрасываем ID для создания нового
		if err := tx.Create(&recipe.Ingredients[i]).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("ошибка создания ингредиента: %w", err)
		}
	}

	if err := tx.Commit().Error; err != nil {
		return err
	}

	log.Printf("✅ Обновлен рецепт: %s (ID: %s)", recipe.Name, recipeID)
	return nil
}

// DeleteRecipe удаляет рецепт (soft delete)
func (s *RecipeService) DeleteRecipe(recipeID string) error {
	if err := s.db.Delete(&models.Recipe{}, "id = ?", recipeID).Error; err != nil {
		return fmt.Errorf("ошибка удаления рецепта: %w", err)
	}

	log.Printf("✅ Удален рецепт (ID: %s)", recipeID)
	return nil
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

