package api

import (
	"fmt"
	"log"
	"net/http"
	"net/url"

	"zephyrvpn/server/internal/models"
	"zephyrvpn/server/internal/services"

	"github.com/gin-gonic/gin"
)

// TechnologistController управляет API endpoints для Technologist Workspace
type TechnologistController struct {
	technologistService *services.TechnologistService
	recipeService       *services.RecipeService
}

// NewTechnologistController создает новый контроллер технолога
func NewTechnologistController(
	technologistService *services.TechnologistService,
	recipeService *services.RecipeService,
) *TechnologistController {
	return &TechnologistController{
		technologistService: technologistService,
		recipeService:       recipeService,
	}
}

// RequireTechnologistRole - middleware для проверки роли TECHNOLOGIST или SUPER_ADMIN
func RequireTechnologistRole() gin.HandlerFunc {
	return func(c *gin.Context) {
		// TODO: Реализовать проверку роли из сессии/токена
		// Пока что это заглушка - нужно интегрировать с системой аутентификации
		// ВРЕМЕННО ОТКЛЮЧЕНО: Для разработки и тестирования проверка роли отключена
		// После интеграции с системой аутентификации раскомментировать:
		/*
		userRole := c.GetString("user_role") // Предполагается, что роль устанавливается в middleware аутентификации
		
		if userRole != "Technologist" && userRole != "SuperAdmin" {
			c.JSON(http.StatusForbidden, gin.H{
				"error": "Доступ запрещен. Требуется роль Technologist или SuperAdmin",
			})
			c.Abort()
			return
		}
		*/
		
		// Временно пропускаем все запросы без проверки
		c.Next()
	}
}

// GetProductionDashboard возвращает данные для Production Dashboard
// GET /api/v1/technologist/dashboard?branch_id=xxx
func (tc *TechnologistController) GetProductionDashboard(c *gin.Context) {
	branchID := c.DefaultQuery("branch_id", "")
	if branchID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "branch_id обязателен",
		})
		return
	}

	dashboard, err := tc.technologistService.GetProductionDashboard(branchID)
	if err != nil {
		log.Printf("❌ GetProductionDashboard: ошибка: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Ошибка загрузки dashboard",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, dashboard)
}

// GetRecipeVersions возвращает версии рецепта
// GET /api/v1/technologist/recipes/:id/versions
func (tc *TechnologistController) GetRecipeVersions(c *gin.Context) {
	recipeID := c.Param("id")
	if recipeID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "ID рецепта не указан",
		})
		return
	}

	versions, err := tc.technologistService.GetRecipeVersions(recipeID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Ошибка загрузки версий",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"versions": versions,
		"count":    len(versions),
	})
}

// GetRecipeUsageTree возвращает дерево использования рецепта
// GET /api/v1/technologist/recipes/:id/usage-tree
func (tc *TechnologistController) GetRecipeUsageTree(c *gin.Context) {
	recipeID := c.Param("id")
	if recipeID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "ID рецепта не указан",
		})
		return
	}

	tree, err := tc.technologistService.GetRecipeUsageTree(recipeID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Ошибка построения дерева использования",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, tree)
}

// CreateTrainingMaterial создает обучающий материал
// POST /api/v1/technologist/training-materials
func (tc *TechnologistController) CreateTrainingMaterial(c *gin.Context) {
	var material models.TrainingMaterial
	if err := c.ShouldBindJSON(&material); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Неверные параметры запроса",
			"details": err.Error(),
		})
		return
	}

	// Устанавливаем created_by из сессии (TODO: из middleware аутентификации)
	material.CreatedBy = c.GetString("user_id") // Заглушка

	if err := tc.technologistService.CreateTrainingMaterial(&material); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Ошибка создания материала",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusCreated, material)
}

// GetTrainingMaterials возвращает обучающие материалы для рецепта
// GET /api/v1/technologist/recipes/:id/training-materials
func (tc *TechnologistController) GetTrainingMaterials(c *gin.Context) {
	recipeID := c.Param("id")
	if recipeID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "ID рецепта не указан",
		})
		return
	}

	materials, err := tc.technologistService.GetTrainingMaterials(recipeID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Ошибка загрузки материалов",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"materials": materials,
		"count":     len(materials),
	})
}

// CreateRecipeExam создает/обновляет экзамен по рецепту
// POST /api/v1/technologist/recipe-exams
func (tc *TechnologistController) CreateRecipeExam(c *gin.Context) {
	var exam models.RecipeExam
	if err := c.ShouldBindJSON(&exam); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Неверные параметры запроса",
			"details": err.Error(),
		})
		return
	}

	// Устанавливаем examined_by из сессии
	exam.ExaminedBy = c.GetString("user_id") // Заглушка

	if err := tc.technologistService.CreateRecipeExam(&exam); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Ошибка создания экзамена",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusCreated, exam)
}

// GetRecipeExams возвращает экзамены по рецепту
// GET /api/v1/technologist/recipes/:id/exams
func (tc *TechnologistController) GetRecipeExams(c *gin.Context) {
	recipeID := c.Param("id")
	if recipeID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "ID рецепта не указан",
		})
		return
	}

	exams, err := tc.technologistService.GetRecipeExams(recipeID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Ошибка загрузки экзаменов",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"exams": exams,
		"count": len(exams),
	})
}

// GetStaffRecipeExams возвращает экзамены сотрудника
// GET /api/v1/technologist/staff/:id/recipe-exams
func (tc *TechnologistController) GetStaffRecipeExams(c *gin.Context) {
	staffID := c.Param("id")
	if staffID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "ID сотрудника не указан",
		})
		return
	}

	exams, err := tc.technologistService.GetStaffRecipeExams(staffID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Ошибка загрузки экзаменов",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"exams": exams,
		"count": len(exams),
	})
}

// UnifiedCreateMenuItem - расширенная версия unified create с версионированием
// POST /api/v1/technologist/unified-create
func (tc *TechnologistController) UnifiedCreateMenuItem(c *gin.Context) {
	var request struct {
		Name                  string                    `json:"name" binding:"required"`
		Description           string                    `json:"description"`
		Price                 int                       `json:"price"` // Обязательно только для finished товаров
		Ingredients           []models.RecipeIngredient `json:"ingredients" binding:"required"`
		NomenclatureData      *models.NomenclatureItem  `json:"nomenclature_data"` // Опционально, если указан existing_nomenclature_id
		ExistingNomenclatureID *string                  `json:"existing_nomenclature_id"` // ID существующего товара (альтернатива nomenclature_data)
		IsSemiFinished        bool                      `json:"is_semi_finished"` // true для полуфабрикатов
		ChangeReason          string                    `json:"change_reason"`    // Причина создания (для версионирования)
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Неверные параметры запроса",
			"details": err.Error(),
		})
		return
	}

	// Валидация: должен быть либо nomenclature_data, либо existing_nomenclature_id
	if request.NomenclatureData == nil && (request.ExistingNomenclatureID == nil || *request.ExistingNomenclatureID == "") {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Неверные параметры запроса",
			"details": "должен быть указан либо nomenclature_data, либо existing_nomenclature_id",
		})
		return
	}

	// Если указан existing_nomenclature_id, загружаем существующий товар
	if request.ExistingNomenclatureID != nil && *request.ExistingNomenclatureID != "" {
		var existingNomenclature models.NomenclatureItem
		if err := tc.recipeService.GetDB().First(&existingNomenclature, "id = ?", *request.ExistingNomenclatureID).Error; err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":   "Товар не найден",
				"details": fmt.Sprintf("номенклатура с ID %s не найдена", *request.ExistingNomenclatureID),
			})
			return
		}
		// Используем существующий товар как nomenclature_data
		request.NomenclatureData = &existingNomenclature
	}

	// Для finished товаров цена обязательна
	if !request.IsSemiFinished && request.Price <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Неверные параметры запроса",
			"details": "для finished товаров цена должна быть больше 0",
		})
		return
	}

	// Используем UnifiedCreateMenuItem из RecipeService
	createdRecipe, err := tc.recipeService.UnifiedCreateMenuItem(
		request.Name,
		request.Description,
		request.Price,
		request.Ingredients,
		request.NomenclatureData,
		request.IsSemiFinished, // Передаем флаг полуфабриката
	)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Ошибка создания Menu Item",
			"details": err.Error(),
		})
		return
	}

	// Создаем начальную версию рецепта
	changedBy := c.GetString("user_id") // Заглушка
	if changedBy == "" {
		changedBy = "system"
	}

	if err := tc.technologistService.CreateRecipeVersion(
		createdRecipe.ID,
		changedBy,
		request.ChangeReason,
	); err != nil {
		log.Printf("⚠️ Ошибка создания версии рецепта: %v", err)
		// Не прерываем выполнение, версия не критична
	}

	c.JSON(http.StatusCreated, createdRecipe)
}

// ActivateForMenu активирует товар для меню
// POST /api/v1/technologist/activate-for-menu
func (tc *TechnologistController) ActivateForMenu(c *gin.Context) {
	var request struct {
		NomenclatureID string `json:"nomenclature_id" binding:"required"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "nomenclature_id обязателен",
		})
		return
	}

	if err := tc.technologistService.ActivateForMenu(request.NomenclatureID); err != nil {
		log.Printf("❌ ActivateForMenu: ошибка: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Ошибка активации товара",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Товар успешно активирован для меню",
		"nomenclature_id": request.NomenclatureID,
	})
}

// ============================================
// УПРАВЛЕНИЕ ДОПАМИ (EXTRAS)
// ============================================

// GetExtras возвращает список всех допов
// GET /api/v1/technologist/extras
func (tc *TechnologistController) GetExtras(c *gin.Context) {
	var extras []models.ExtraDB
	if err := tc.technologistService.GetDB().Find(&extras).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Ошибка загрузки допов",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"extras": extras,
		"count":  len(extras),
	})
}

// CreateExtra создает новый доп
// POST /api/v1/technologist/extras
func (tc *TechnologistController) CreateExtra(c *gin.Context) {
	var request struct {
		Name           string  `json:"name" binding:"required"`
		Price          int     `json:"price" binding:"required,min=1"`
		NomenclatureID *string `json:"nomenclature_id"` // Опционально: для простых допов (один ингредиент)
		RecipeID       *string `json:"recipe_id"`       // Опционально: для сложных допов (BOM)
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Неверные параметры запроса",
			"details": err.Error(),
		})
		return
	}

	// Валидация: должен быть указан либо nomenclature_id, либо recipe_id (или оба)
	if request.NomenclatureID == nil && request.RecipeID == nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Необходимо указать либо nomenclature_id (для простых допов), либо recipe_id (для сложных допов с BOM)",
		})
		return
	}

	// Проверяем, существует ли nomenclature_id (если указан)
	if request.NomenclatureID != nil {
		var count int64
		tc.technologistService.GetDB().Model(&models.NomenclatureItem{}).
			Where("id = ?", *request.NomenclatureID).
			Count(&count)
		if count == 0 {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "Номенклатура с указанным ID не найдена",
			})
			return
		}
	}

	// Проверяем, существует ли recipe_id (если указан)
	if request.RecipeID != nil {
		var count int64
		tc.technologistService.GetDB().Model(&models.Recipe{}).
			Where("id = ?", *request.RecipeID).
			Count(&count)
		if count == 0 {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "Рецепт с указанным ID не найден",
			})
			return
		}
	}

	extra := models.ExtraDB{
		Name:           request.Name,
		Price:          request.Price,
		NomenclatureID: request.NomenclatureID,
		RecipeID:       request.RecipeID,
		IsActive:       true,
	}

	if err := tc.technologistService.GetDB().Create(&extra).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Ошибка создания допа",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusCreated, extra)
}

// UpdateExtra обновляет доп
// PUT /api/v1/technologist/extras/:id
func (tc *TechnologistController) UpdateExtra(c *gin.Context) {
	extraID := c.Param("id")
	if extraID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "ID допа не указан",
		})
		return
	}

	var request struct {
		Name           string  `json:"name"`
		Price          int     `json:"price"`
		NomenclatureID *string `json:"nomenclature_id"`
		RecipeID       *string `json:"recipe_id"`
		IsActive       *bool   `json:"is_active"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Неверные параметры запроса",
			"details": err.Error(),
		})
		return
	}

	var extra models.ExtraDB
	if err := tc.technologistService.GetDB().First(&extra, extraID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "Доп не найден",
		})
		return
	}

	// Обновляем поля
	if request.Name != "" {
		extra.Name = request.Name
	}
	if request.Price > 0 {
		extra.Price = request.Price
	}
	if request.NomenclatureID != nil {
		extra.NomenclatureID = request.NomenclatureID
	}
	if request.RecipeID != nil {
		extra.RecipeID = request.RecipeID
	}
	if request.IsActive != nil {
		extra.IsActive = *request.IsActive
	}

	if err := tc.technologistService.GetDB().Save(&extra).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Ошибка обновления допа",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, extra)
}

// DeleteExtra удаляет доп
// DELETE /api/v1/technologist/extras/:id
func (tc *TechnologistController) DeleteExtra(c *gin.Context) {
	extraID := c.Param("id")
	if extraID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "ID допа не указан",
		})
		return
	}

	// Сначала проверяем, используется ли доп в связях
	var count int64
	tc.technologistService.GetDB().Model(&models.PizzaExtra{}).Where("extra_id = ?", extraID).Count(&count)
	if count > 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Невозможно удалить доп: он привязан к пиццам. Сначала удалите связи.",
		})
		return
	}

	if err := tc.technologistService.GetDB().Delete(&models.ExtraDB{}, extraID).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Ошибка удаления допа",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Доп успешно удален",
	})
}

// ============================================
// УПРАВЛЕНИЕ СВЯЗЯМИ ПИЦЦА-ДОП
// ============================================

// GetPizzaExtras возвращает допы для конкретной пиццы
// GET /api/v1/technologist/pizzas/:pizza_name/extras
func (tc *TechnologistController) GetPizzaExtras(c *gin.Context) {
	// Декодируем URL параметр (может содержать кириллицу)
	pizzaNameRaw := c.Param("pizza_name")
	pizzaName, err := url.QueryUnescape(pizzaNameRaw)
	if err != nil {
		// Если декодирование не удалось, используем исходное значение
		pizzaName = pizzaNameRaw
		log.Printf("⚠️ GetPizzaExtras: не удалось декодировать pizza_name '%s', используем как есть", pizzaNameRaw)
	}
	
	log.Printf("🔍 GetPizzaExtras: pizza_name='%s' (raw='%s')", pizzaName, pizzaNameRaw)
	
	if pizzaName == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Название пиццы не указано",
		})
		return
	}

	var pizzaExtras []models.PizzaExtra
	if err := tc.technologistService.GetDB().
		Preload("Extra").
		Where("pizza_name = ?", pizzaName).
		Order("display_order ASC, id ASC").
		Find(&pizzaExtras).Error; err != nil {
		log.Printf("❌ GetPizzaExtras: ошибка БД для пиццы '%s': %v", pizzaName, err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Ошибка загрузки допов для пиццы",
			"details": err.Error(),
		})
		return
	}

	log.Printf("✅ GetPizzaExtras: найдено %d допов для пиццы '%s'", len(pizzaExtras), pizzaName)

	// Преобразуем в удобный формат
	extras := make([]map[string]interface{}, len(pizzaExtras))
	for i, pe := range pizzaExtras {
		extras[i] = map[string]interface{}{
			"id":           pe.ID,
			"extra_id":     pe.ExtraID,
			"name":         pe.Extra.Name,
			"price":        pe.Extra.Price,
			"is_default":   pe.IsDefault,
			"display_order": pe.DisplayOrder,
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"pizza_name": pizzaName,
		"extras":    extras,
		"count":     len(extras),
	})
}

// AddPizzaExtra привязывает доп к пицце
// POST /api/v1/technologist/pizzas/:pizza_name/extras
func (tc *TechnologistController) AddPizzaExtra(c *gin.Context) {
	pizzaName := c.Param("pizza_name")
	if pizzaName == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Название пиццы не указано",
		})
		return
	}

	var request struct {
		ExtraID     uint `json:"extra_id" binding:"required"`
		IsDefault   bool `json:"is_default"`
		DisplayOrder int `json:"display_order"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Неверные параметры запроса",
			"details": err.Error(),
		})
		return
	}

	// Проверяем, существует ли пицца
	var pizza models.PizzaRecipe
	if err := tc.technologistService.GetDB().Where("name = ?", pizzaName).First(&pizza).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "Пицца не найдена",
		})
		return
	}

	// Проверяем, существует ли доп
	var extra models.ExtraDB
	if err := tc.technologistService.GetDB().First(&extra, request.ExtraID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "Доп не найден",
		})
		return
	}

	// Проверяем, не привязан ли уже доп к этой пицце
	var existing models.PizzaExtra
	if err := tc.technologistService.GetDB().
		Where("pizza_name = ? AND extra_id = ?", pizzaName, request.ExtraID).
		First(&existing).Error; err == nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Доп уже привязан к этой пицце",
		})
		return
	}

	pizzaExtra := models.PizzaExtra{
		PizzaName:    pizzaName,
		ExtraID:      request.ExtraID,
		IsDefault:    request.IsDefault,
		DisplayOrder: request.DisplayOrder,
	}

	if err := tc.technologistService.GetDB().Create(&pizzaExtra).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Ошибка привязки допа к пицце",
			"details": err.Error(),
		})
		return
	}

	// Загружаем с допом для ответа
	tc.technologistService.GetDB().Preload("Extra").First(&pizzaExtra, pizzaExtra.ID)

	c.JSON(http.StatusCreated, pizzaExtra)
}

// RemovePizzaExtra отвязывает доп от пиццы
// DELETE /api/v1/technologist/pizzas/:pizza_name/extras/:extra_id
func (tc *TechnologistController) RemovePizzaExtra(c *gin.Context) {
	pizzaName := c.Param("pizza_name")
	extraID := c.Param("extra_id")
	if pizzaName == "" || extraID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Название пиццы или ID допа не указаны",
		})
		return
	}

	if err := tc.technologistService.GetDB().
		Where("pizza_name = ? AND extra_id = ?", pizzaName, extraID).
		Delete(&models.PizzaExtra{}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Ошибка отвязки допа от пиццы",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Доп успешно отвязан от пиццы",
	})
}

// UpdatePizzaExtra обновляет связь пицца-доп (is_default, display_order)
// PUT /api/v1/technologist/pizzas/:pizza_name/extras/:extra_id
func (tc *TechnologistController) UpdatePizzaExtra(c *gin.Context) {
	pizzaName := c.Param("pizza_name")
	extraID := c.Param("extra_id")
	if pizzaName == "" || extraID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Название пиццы или ID допа не указаны",
		})
		return
	}

	var request struct {
		IsDefault   *bool `json:"is_default"`
		DisplayOrder *int `json:"display_order"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Неверные параметры запроса",
			"details": err.Error(),
		})
		return
	}

	var pizzaExtra models.PizzaExtra
	if err := tc.technologistService.GetDB().
		Where("pizza_name = ? AND extra_id = ?", pizzaName, extraID).
		First(&pizzaExtra).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "Связь пицца-доп не найдена",
		})
		return
	}

	if request.IsDefault != nil {
		pizzaExtra.IsDefault = *request.IsDefault
	}
	if request.DisplayOrder != nil {
		pizzaExtra.DisplayOrder = *request.DisplayOrder
	}

	if err := tc.technologistService.GetDB().Save(&pizzaExtra).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Ошибка обновления связи",
			"details": err.Error(),
		})
		return
	}

	tc.technologistService.GetDB().Preload("Extra").First(&pizzaExtra, pizzaExtra.ID)

	c.JSON(http.StatusOK, pizzaExtra)
}

