package api

import (
	"fmt"
	"log"
	"net/http"
	"strings"

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

	// Конвертируем StationIDs из JSON строки в массив для каждого рецепта
	recipesWithStationIDs := make([]map[string]interface{}, len(recipes))
	for i, recipe := range recipes {
		recipeMap := make(map[string]interface{})
		// Используем рефлексию или просто создаем новый объект с station_ids как массив
		stationIDs, _ := recipe.GetStationIDs()
		recipeMap["id"] = recipe.ID
		recipeMap["name"] = recipe.Name
		recipeMap["description"] = recipe.Description
		recipeMap["menu_item_id"] = recipe.MenuItemID
		recipeMap["station_ids"] = stationIDs // Возвращаем как массив
		recipeMap["portion_size"] = recipe.PortionSize
		recipeMap["unit"] = recipe.Unit
		recipeMap["is_semi_finished"] = recipe.IsSemiFinished
		recipeMap["is_active"] = recipe.IsActive
		recipeMap["instruction_text"] = recipe.InstructionText
		recipeMap["video_url"] = recipe.VideoURL
		recipeMap["photo_urls"] = recipe.PhotoURLs
		recipeMap["created_at"] = recipe.CreatedAt
		recipeMap["updated_at"] = recipe.UpdatedAt
		recipeMap["ingredients"] = recipe.Ingredients
		recipesWithStationIDs[i] = recipeMap
	}

	c.JSON(http.StatusOK, gin.H{
		"recipes": recipesWithStationIDs,
		"count":   len(recipesWithStationIDs),
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

	// Конвертируем StationIDs из JSON строки в массив
	stationIDs, _ := recipe.GetStationIDs()
	recipeResponse := map[string]interface{}{
		"id":                recipe.ID,
		"name":              recipe.Name,
		"description":       recipe.Description,
		"menu_item_id":      recipe.MenuItemID,
		"station_ids":       stationIDs, // Возвращаем как массив
		"portion_size":      recipe.PortionSize,
		"unit":              recipe.Unit,
		"is_semi_finished":  recipe.IsSemiFinished,
		"is_active":         recipe.IsActive,
		"instruction_text": recipe.InstructionText,
		"video_url":         recipe.VideoURL,
		"photo_urls":        recipe.PhotoURLs,
		"created_at":       recipe.CreatedAt,
		"updated_at":        recipe.UpdatedAt,
		"ingredients":       recipe.Ingredients,
	}

	c.JSON(http.StatusOK, recipeResponse)
}

// CreateRecipeRequest представляет запрос на создание рецепта
// Поддерживает как старый формат (station_id), так и новый (station_ids)
type CreateRecipeRequest struct {
	models.Recipe
	StationIDs []string `json:"station_ids"` // Массив ID станций (новый формат)
}

// CreateRecipe создает новый рецепт
// POST /api/v1/recipes
func (rc *RecipeController) CreateRecipe(c *gin.Context) {
	log.Printf("📝 CreateRecipe: получен запрос на создание рецепта")
	
	var req CreateRecipeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("❌ CreateRecipe: ошибка парсинга JSON: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Неверные параметры запроса",
			"details": err.Error(),
		})
		return
	}
	
	// Конвертируем массив StationIDs в JSON строку
	if len(req.StationIDs) > 0 {
		if err := req.Recipe.SetStationIDs(req.StationIDs); err != nil {
			log.Printf("❌ CreateRecipe: ошибка конвертации StationIDs: %v", err)
			c.JSON(http.StatusBadRequest, gin.H{
				"error":   "Ошибка обработки station_ids",
				"details": err.Error(),
			})
			return
		}
	} else {
		// Если station_ids не указаны, возвращаем ошибку
		log.Printf("❌ CreateRecipe: station_ids не указаны")
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "station_ids обязательны (массив ID станций)",
			"details": "Укажите хотя бы одну станцию в массиве station_ids",
		})
		return
	}
	
	recipe := req.Recipe
	log.Printf("📝 CreateRecipe: данные рецепта получены - Name: %s, Ingredients: %d, StationIDs: %v", 
		recipe.Name, len(recipe.Ingredients), req.StationIDs)

	// Валидация ингредиентов (циклические зависимости)
	// Примечание: проверка на дубликаты nomenclature_id/ingredient_recipe_id выполняется в сервисе
	for i, ingredient := range recipe.Ingredients {
		if ingredient.IngredientRecipeID != nil {
			if err := rc.recipeService.ValidateRecipeIngredient(recipe.ID, ingredient.IngredientRecipeID); err != nil {
				log.Printf("❌ CreateRecipe: ошибка валидации ингредиента #%d: %v", i+1, err)
				c.JSON(http.StatusBadRequest, gin.H{
					"error":   fmt.Sprintf("Ошибка валидации ингредиента #%d", i+1),
					"details": err.Error(),
				})
				return
			}
		}
	}

	// Создаем рецепт (все валидации и проверки выполняются внутри сервиса)
	if err := rc.recipeService.CreateRecipe(&recipe); err != nil {
		log.Printf("❌ CreateRecipe: ошибка создания рецепта в сервисе: %v", err)
		
		// Определяем статус код в зависимости от типа ошибки
		statusCode := http.StatusInternalServerError
		errorMsg := "Ошибка создания рецепта"
		
		// Если это ошибка валидации или дубликата, возвращаем 400
		if strings.Contains(err.Error(), "дубликат") || 
		   strings.Contains(err.Error(), "валидации") ||
		   strings.Contains(err.Error(), "должен быть указан") {
			statusCode = http.StatusBadRequest
			errorMsg = "Ошибка валидации данных"
		}
		
		c.JSON(statusCode, gin.H{
			"error":   errorMsg,
			"details": err.Error(),
		})
		return
	}

	// Загружаем созданный рецепт с полными данными (включая ингредиенты с preload)
	// Это гарантирует, что возвращаем "чистый" объект из БД, а не тот, что был передан в запросе
	createdRecipe, err := rc.recipeService.GetRecipe(recipe.ID)
	if err != nil {
		log.Printf("⚠️ CreateRecipe: рецепт создан, но не удалось загрузить для ответа: %v", err)
		// Возвращаем то, что есть (хотя это не идеально)
		stationIDs, _ := recipe.GetStationIDs()
		recipeResponse := map[string]interface{}{
			"id":               recipe.ID,
			"name":             recipe.Name,
			"description":      recipe.Description,
			"menu_item_id":     recipe.MenuItemID,
			"station_ids":       stationIDs,
			"portion_size":     recipe.PortionSize,
			"unit":             recipe.Unit,
			"is_semi_finished": recipe.IsSemiFinished,
			"is_active":        recipe.IsActive,
			"instruction_text": recipe.InstructionText,
			"video_url":        recipe.VideoURL,
			"photo_urls":       recipe.PhotoURLs,
			"created_at":      recipe.CreatedAt,
			"updated_at":       recipe.UpdatedAt,
			"ingredients":      recipe.Ingredients,
		}
		c.JSON(http.StatusCreated, recipeResponse)
		return
	}

	// Конвертируем StationIDs из JSON строки в массив
	stationIDs, _ := createdRecipe.GetStationIDs()
	recipeResponse := map[string]interface{}{
		"id":               createdRecipe.ID,
		"name":             createdRecipe.Name,
		"description":      createdRecipe.Description,
		"menu_item_id":     createdRecipe.MenuItemID,
		"station_ids":       stationIDs, // Возвращаем как массив
		"portion_size":     createdRecipe.PortionSize,
		"unit":             createdRecipe.Unit,
		"is_semi_finished": createdRecipe.IsSemiFinished,
		"is_active":        createdRecipe.IsActive,
		"instruction_text": createdRecipe.InstructionText,
		"video_url":        createdRecipe.VideoURL,
		"photo_urls":       createdRecipe.PhotoURLs,
		"created_at":       createdRecipe.CreatedAt,
		"updated_at":        createdRecipe.UpdatedAt,
		"ingredients":       createdRecipe.Ingredients,
	}

	log.Printf("✅ CreateRecipe: рецепт успешно создан - ID: %s, Name: %s, Ingredients: %d, StationIDs: %v", 
		createdRecipe.ID, createdRecipe.Name, len(createdRecipe.Ingredients), stationIDs)
	c.JSON(http.StatusCreated, recipeResponse)
}

// UnifiedCreateMenuItem создает Menu Item (товар для продажи) в единой транзакции
// POST /api/v1/recipes/unified-create
func (rc *RecipeController) UnifiedCreateMenuItem(c *gin.Context) {
	log.Printf("📝 UnifiedCreateMenuItem: получен запрос на создание Menu Item")
	
	var request struct {
		Name             string                      `json:"name" binding:"required"`
		Description      string                      `json:"description"`
		Price            int                         `json:"price" binding:"required"`
		Ingredients      []models.RecipeIngredient  `json:"ingredients" binding:"required"`
		NomenclatureData *models.NomenclatureItem   `json:"nomenclature_data" binding:"required"`
	}
	
	if err := c.ShouldBindJSON(&request); err != nil {
		log.Printf("❌ UnifiedCreateMenuItem: ошибка парсинга JSON: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Неверные параметры запроса",
			"details": err.Error(),
		})
		return
	}
	
	log.Printf("📝 UnifiedCreateMenuItem: данные получены - Name: %s, Price: %d, Ingredients: %d", 
		request.Name, request.Price, len(request.Ingredients))
	
	// Создаем Menu Item в единой транзакции
	// Для старого API всегда создаем finished товар (isSemiFinished = false)
	createdRecipe, err := rc.recipeService.UnifiedCreateMenuItem(
		request.Name,
		request.Description,
		request.Price,
		request.Ingredients,
		request.NomenclatureData,
		false, // Старый API всегда создает finished товары
	)
	
	if err != nil {
		log.Printf("❌ UnifiedCreateMenuItem: ошибка создания: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Ошибка создания Menu Item",
			"details": err.Error(),
		})
		return
	}
	
	log.Printf("✅ UnifiedCreateMenuItem: Menu Item успешно создан - Recipe ID: %s, Name: %s", 
		createdRecipe.ID, createdRecipe.Name)
	c.JSON(http.StatusCreated, createdRecipe)
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

	var req CreateRecipeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Неверные параметры запроса",
			"details": err.Error(),
		})
		return
	}

	// Конвертируем массив StationIDs в JSON строку (если указаны)
	if len(req.StationIDs) > 0 {
		if err := req.Recipe.SetStationIDs(req.StationIDs); err != nil {
			log.Printf("❌ UpdateRecipe: ошибка конвертации StationIDs: %v", err)
			c.JSON(http.StatusBadRequest, gin.H{
				"error":   "Ошибка обработки station_ids",
				"details": err.Error(),
			})
			return
		}
	} else if req.Recipe.StationIDs == "" {
		// Если station_ids не указаны и в рецепте их нет, возвращаем ошибку
		log.Printf("❌ UpdateRecipe: station_ids не указаны")
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "station_ids обязательны (массив ID станций)",
			"details": "Укажите хотя бы одну станцию в массиве station_ids",
		})
		return
	}

	recipe := req.Recipe

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

	// Конвертируем StationIDs из JSON строки в массив
	stationIDs, _ := updatedRecipe.GetStationIDs()
	recipeResponse := map[string]interface{}{
		"id":               updatedRecipe.ID,
		"name":             updatedRecipe.Name,
		"description":      updatedRecipe.Description,
		"menu_item_id":     updatedRecipe.MenuItemID,
		"station_ids":       stationIDs, // Возвращаем как массив
		"portion_size":     updatedRecipe.PortionSize,
		"unit":             updatedRecipe.Unit,
		"is_semi_finished": updatedRecipe.IsSemiFinished,
		"is_active":        updatedRecipe.IsActive,
		"instruction_text": updatedRecipe.InstructionText,
		"video_url":        updatedRecipe.VideoURL,
		"photo_urls":       updatedRecipe.PhotoURLs,
		"created_at":       updatedRecipe.CreatedAt,
		"updated_at":        updatedRecipe.UpdatedAt,
		"ingredients":       updatedRecipe.Ingredients,
	}

	c.JSON(http.StatusOK, recipeResponse)
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

// GetFolderContent возвращает содержимое папки
// GET /api/v1/recipes/folder?parent_id=xxx
func (rc *RecipeController) GetFolderContent(c *gin.Context) {
	parentID := c.Query("parent_id")
	var parentIDPtr *string
	if parentID != "" {
		parentIDPtr = &parentID
	}

	nodes, err := rc.recipeService.GetFolderContent(parentIDPtr)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Ошибка получения содержимого папки",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"nodes": nodes,
		"count": len(nodes),
	})
}

// CreateNode создает новый узел (папку или рецепт)
// POST /api/v1/recipes/nodes
func (rc *RecipeController) CreateNode(c *gin.Context) {
	var request struct {
		Name     string  `json:"name" binding:"required"`
		ParentID *string `json:"parent_id"`
		IsFolder bool    `json:"is_folder"`
		RecipeID *string `json:"recipe_id"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Неверные параметры запроса",
			"details": err.Error(),
		})
		return
	}

	node, err := rc.recipeService.CreateNode(request.Name, request.ParentID, request.IsFolder, request.RecipeID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Ошибка создания узла",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusCreated, node)
}

// GetNodePath возвращает путь к узлу
// GET /api/v1/recipes/nodes/:id/path
func (rc *RecipeController) GetNodePath(c *gin.Context) {
	nodeID := c.Param("id")
	if nodeID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "ID узла не указан",
		})
		return
	}

	path, err := rc.recipeService.GetNodePath(nodeID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error":   "Узел не найден",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"path": path,
	})
}

// UpdateNode обновляет узел
// PUT /api/v1/recipes/nodes/:id
func (rc *RecipeController) UpdateNode(c *gin.Context) {
	nodeID := c.Param("id")
	if nodeID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "ID узла не указан",
		})
		return
	}

	var request struct {
		Name     *string `json:"name"`
		ParentID *string `json:"parent_id"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Неверные параметры запроса",
			"details": err.Error(),
		})
		return
	}

	updates := make(map[string]interface{})
	if request.Name != nil {
		updates["name"] = *request.Name
	}
	if request.ParentID != nil {
		updates["parent_id"] = request.ParentID
	}

	node, err := rc.recipeService.UpdateNode(nodeID, updates)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Ошибка обновления узла",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, node)
}

// UpdateNodePosition обновляет позицию узла в сетке
// PUT /api/v1/recipes/nodes/:id/position
func (rc *RecipeController) UpdateNodePosition(c *gin.Context) {
	nodeID := c.Param("id")
	if nodeID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "ID узла не указан",
		})
		return
	}

	var request struct {
		GridCol *int `json:"grid_col"`
		GridRow *int `json:"grid_row"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Неверные параметры запроса",
			"details": err.Error(),
		})
		return
	}

	if request.GridCol == nil || request.GridRow == nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "grid_col и grid_row обязательны",
		})
		return
	}

	updates := make(map[string]interface{})
	updates["grid_col"] = *request.GridCol
	updates["grid_row"] = *request.GridRow

	node, err := rc.recipeService.UpdateNode(nodeID, updates)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Ошибка обновления позиции узла",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, node)
}

// DeleteNode удаляет узел
// DELETE /api/v1/recipes/nodes/:id
func (rc *RecipeController) DeleteNode(c *gin.Context) {
	nodeID := c.Param("id")
	if nodeID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "ID узла не указан",
		})
		return
	}

	if err := rc.recipeService.DeleteNode(nodeID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Ошибка удаления узла",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Узел успешно удален",
	})
}

// FindOrphanedIngredients возвращает список "осиротевших" ингредиентов
// GET /api/v1/recipes/orphaned-ingredients
func (rc *RecipeController) FindOrphanedIngredients(c *gin.Context) {
	orphaned, err := rc.recipeService.FindOrphanedIngredients()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Ошибка поиска осиротевших ингредиентов",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"orphaned_ingredients": orphaned,
		"count":                len(orphaned),
		"message":              fmt.Sprintf("Найдено %d осиротевших ингредиентов", len(orphaned)),
	})
}

