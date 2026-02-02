package api

import (
	"net/http"

	"zephyrvpn/server/internal/models"
	"zephyrvpn/server/internal/services"

	"github.com/gin-gonic/gin"
)

// RecipeController управляет API endpoints для рецептов
type RecipeController struct {
	recipeService *services.RecipeService
}

// NewRecipeController создает новый контроллер рецептов
func NewRecipeController(recipeService *services.RecipeService) *RecipeController {
	return &RecipeController{
		recipeService: recipeService,
	}
}

// GetRecipes возвращает список всех рецептов
// GET /api/v1/recipes?include_inactive=false
func (rc *RecipeController) GetRecipes(c *gin.Context) {
	includeInactive := c.DefaultQuery("include_inactive", "false") == "true"

	recipes, err := rc.recipeService.GetRecipes(includeInactive)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Ошибка получения рецептов",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"recipes": recipes,
		"count":   len(recipes),
	})
}

// GetRecipe возвращает рецепт по ID
// GET /api/v1/recipes/:id
func (rc *RecipeController) GetRecipe(c *gin.Context) {
	recipeID := c.Param("id")
	if recipeID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "ID рецепта не указан",
		})
		return
	}

	recipe, err := rc.recipeService.GetRecipe(recipeID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error":   "Рецепт не найден",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, recipe)
}

// CreateRecipe создает новый рецепт
// POST /api/v1/recipes
func (rc *RecipeController) CreateRecipe(c *gin.Context) {
	var recipe models.Recipe
	if err := c.ShouldBindJSON(&recipe); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Неверные параметры запроса",
			"details": err.Error(),
		})
		return
	}

	// Валидация ингредиентов
	for _, ingredient := range recipe.Ingredients {
		if err := rc.recipeService.ValidateRecipeIngredient(recipe.ID, ingredient.IngredientRecipeID); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":   "Ошибка валидации ингредиента",
				"details": err.Error(),
			})
			return
		}
	}

	if err := rc.recipeService.CreateRecipe(&recipe); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Ошибка создания рецепта",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusCreated, recipe)
}

// UpdateRecipe обновляет существующий рецепт
// PUT /api/v1/recipes/:id
func (rc *RecipeController) UpdateRecipe(c *gin.Context) {
	recipeID := c.Param("id")
	if recipeID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "ID рецепта не указан",
		})
		return
	}

	var recipe models.Recipe
	if err := c.ShouldBindJSON(&recipe); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Неверные параметры запроса",
			"details": err.Error(),
		})
		return
	}

	// Валидация ингредиентов
	for _, ingredient := range recipe.Ingredients {
		if err := rc.recipeService.ValidateRecipeIngredient(recipeID, ingredient.IngredientRecipeID); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":   "Ошибка валидации ингредиента",
				"details": err.Error(),
			})
			return
		}
	}

	if err := rc.recipeService.UpdateRecipe(recipeID, &recipe); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Ошибка обновления рецепта",
			"details": err.Error(),
		})
		return
	}

	// Загружаем обновленный рецепт
	updatedRecipe, err := rc.recipeService.GetRecipe(recipeID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Ошибка загрузки обновленного рецепта",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, updatedRecipe)
}

// DeleteRecipe удаляет рецепт
// DELETE /api/v1/recipes/:id
func (rc *RecipeController) DeleteRecipe(c *gin.Context) {
	recipeID := c.Param("id")
	if recipeID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "ID рецепта не указан",
		})
		return
	}

	if err := rc.recipeService.DeleteRecipe(recipeID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Ошибка удаления рецепта",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Рецепт успешно удален",
	})
}

