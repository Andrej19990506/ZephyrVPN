package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"zephyrvpn/server/internal/models"
	"zephyrvpn/server/internal/services"
)

// getMapKeysFromItems возвращает список ключей из map для отладки
func getMapKeysFromItems(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

type NomenclatureController struct {
	service   *services.NomenclatureService
	pluService *services.PLUService
}

func NewNomenclatureController(service *services.NomenclatureService, pluService *services.PLUService) *NomenclatureController {
	return &NomenclatureController{
		service:   service,
		pluService: pluService,
	}
}

// SuggestSKU предлагает SKU на основе названия продукта
// GET /api/v1/inventory/nomenclature/suggest-sku?name=Томат&branch_id=xxx
func (nc *NomenclatureController) SuggestSKU(c *gin.Context) {
	if nc.pluService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "PLU сервис недоступен",
		})
		return
	}

	// Получаем параметры из URL разными способами для надежности
	productName := c.Query("name")
	branchID := c.Query("branch_id")
	
	// Если не получили через Query, пробуем через GetQuery (более надежный метод)
	if productName == "" {
		if name, exists := c.GetQuery("name"); exists {
			productName = name
		}
	}
	if branchID == "" {
		if bid, exists := c.GetQuery("branch_id"); exists {
			branchID = bid
		}
	}
	
	// Логирование для отладки
	log.Printf("🔍 SuggestSKU: получен запрос")
	log.Printf("  - Method: %s", c.Request.Method)
	log.Printf("  - Raw URL: %s", c.Request.URL.String())
	log.Printf("  - Raw Query: %s", c.Request.URL.RawQuery)
	log.Printf("  - Query params: %v", c.Request.URL.Query())
	log.Printf("  - name='%s' (len=%d, empty=%v)", productName, len(productName), productName == "")
	log.Printf("  - branch_id='%s'", branchID)

	if productName == "" {
		log.Printf("⚠️ SuggestSKU: название продукта пустое после всех попыток получения")
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Не указано название продукта",
			"debug": map[string]interface{}{
				"url": c.Request.URL.String(),
				"query_params": c.Request.URL.Query(),
			},
		})
		return
	}

	// Предлагаем SKU
	suggestedSKU, err := nc.pluService.SuggestSKU(productName, branchID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Ошибка генерации SKU",
			"details": err.Error(),
		})
		return
	}

	// Ищем PLU код для дополнительной информации
	plu, _ := nc.pluService.FindPLUByProductName(productName)

	c.JSON(http.StatusOK, gin.H{
		"sku": suggestedSKU,
		"plu": plu,
		"is_plu_based": plu != nil,
	})
}

// GetNomenclatureItems получает список всех товаров номенклатуры
// GET /api/v1/inventory/nomenclature
func (nc *NomenclatureController) GetNomenclatureItems(c *gin.Context) {
	if nc.service == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "Сервис номенклатуры недоступен",
			"items": []interface{}{},
		})
		return
	}

	items, err := nc.service.GetAllItems()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Ошибка получения товаров",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"items": items,
		"count": len(items),
	})
}

// GetNomenclatureItem получает товар по ID
// GET /api/v1/inventory/nomenclature/:id
func (nc *NomenclatureController) GetNomenclatureItem(c *gin.Context) {
	if nc.service == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "Сервис номенклатуры недоступен",
		})
		return
	}

	id := c.Param("id")
	item, err := nc.service.GetItemByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "Товар не найден",
		})
		return
	}

	c.JSON(http.StatusOK, item)
}

// CreateNomenclatureItem создает новый товар
// POST /api/v1/inventory/nomenclature
func (nc *NomenclatureController) CreateNomenclatureItem(c *gin.Context) {
	if nc.service == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "Сервис номенклатуры недоступен",
		})
		return
	}

	var req models.NomenclatureItem
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Неверные данные",
			"details": err.Error(),
		})
		return
	}

	// Валидация обязательных полей
	if req.SKU == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "SKU обязателен",
		})
		return
	}
	if req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Название обязательно",
		})
		return
	}

	if err := nc.service.CreateItem(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusCreated, req)
}

// UpdateNomenclatureItem обновляет товар
// PUT /api/v1/inventory/nomenclature/:id
func (nc *NomenclatureController) UpdateNomenclatureItem(c *gin.Context) {
	if nc.service == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "Сервис номенклатуры недоступен",
		})
		return
	}

	id := c.Param("id")
	
	var req models.NomenclatureItem
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Неверные данные",
			"details": err.Error(),
		})
		return
	}

	if err := nc.service.UpdateItem(id, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})
		return
	}

	// Получаем обновленный товар
	item, err := nc.service.GetItemByID(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Ошибка получения обновленного товара",
		})
		return
	}

	c.JSON(http.StatusOK, item)
}

// DeleteNomenclatureItem удаляет товар
// DELETE /api/v1/inventory/nomenclature/:id
func (nc *NomenclatureController) DeleteNomenclatureItem(c *gin.Context) {
	if nc.service == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "Сервис номенклатуры недоступен",
		})
		return
	}

	id := c.Param("id")
	if err := nc.service.DeleteItem(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Ошибка удаления товара",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Товар удален",
	})
}

// ValidateNomenclatureImport валидирует данные перед импортом
// POST /api/v1/inventory/nomenclature/validate-import
func (nc *NomenclatureController) ValidateNomenclatureImport(c *gin.Context) {
	if nc.service == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "Сервис номенклатуры недоступен",
		})
		return
	}

	var req struct {
		Items              []map[string]interface{} `json:"items" binding:"required"`
		FieldMapping       map[string]string         `json:"field_mapping" binding:"required"`
		AutoCreateCategories bool                    `json:"auto_create_categories"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Неверные данные",
			"details": err.Error(),
		})
		return
	}

	// Логирование для отладки
	if len(req.Items) > 0 {
		log.Printf("🔍 ValidateImport: получено %d строк, первая строка keys: %v", len(req.Items), getMapKeysFromItems(req.Items[0]))
		if len(req.Items) > 0 {
			firstRow := req.Items[0]
			log.Printf("🔍 ValidateImport: первая строка - name='%v', sku='%v', unit='%v'", firstRow["name"], firstRow["sku"], firstRow["unit"])
		}
	}

	validation := nc.service.ValidateImport(req.Items, req.FieldMapping, req.AutoCreateCategories)

	c.JSON(http.StatusOK, gin.H{
		"validation": validation,
		"count": len(validation),
	})
}

// ImportNomenclature выполняет массовый импорт товаров
// POST /api/v1/inventory/nomenclature/import
func (nc *NomenclatureController) ImportNomenclature(c *gin.Context) {
	if nc.service == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "Сервис номенклатуры недоступен",
		})
		return
	}

	var req struct {
		Items              []map[string]interface{} `json:"items" binding:"required"`
		FieldMapping       map[string]string         `json:"field_mapping" binding:"required"`
		AutoCreateCategories bool                    `json:"auto_create_categories"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Неверные данные",
			"details": err.Error(),
		})
		return
	}

	result, err := nc.service.ProcessImport(req.Items, req.FieldMapping, req.AutoCreateCategories)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Ошибка импорта",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, result)
}

// GetNomenclatureCategories получает список всех категорий
// GET /api/v1/inventory/nomenclature/categories
func (nc *NomenclatureController) GetNomenclatureCategories(c *gin.Context) {
	if nc.service == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "Сервис номенклатуры недоступен",
			"categories": []interface{}{},
		})
		return
	}

	categories, err := nc.service.GetAllCategories()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Ошибка получения категорий",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"categories": categories,
		"count": len(categories),
	})
}

// CreateNomenclatureCategory создает новую категорию
// POST /api/v1/inventory/nomenclature/categories
func (nc *NomenclatureController) CreateNomenclatureCategory(c *gin.Context) {
	if nc.service == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "Сервис номенклатуры недоступен",
		})
		return
	}

	var req models.NomenclatureCategory
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Неверные данные",
			"details": err.Error(),
		})
		return
	}

	if req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Название категории обязательно",
		})
		return
	}

	if req.ID == "" {
		req.ID = uuid.New().String()
	}

	if err := nc.service.CreateCategory(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusCreated, req)
}

// UpdateNomenclatureCategory обновляет категорию
// PUT /api/v1/inventory/nomenclature/categories/:id
func (nc *NomenclatureController) UpdateNomenclatureCategory(c *gin.Context) {
	if nc.service == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "Сервис номенклатуры недоступен",
		})
		return
	}

	id := c.Param("id")
	
	var req models.NomenclatureCategory
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Неверные данные",
			"details": err.Error(),
		})
		return
	}

	if err := nc.service.UpdateCategory(id, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})
		return
	}

	// Получаем обновленную категорию
	category, err := nc.service.GetCategoryByID(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Ошибка получения обновленной категории",
		})
		return
	}

	c.JSON(http.StatusOK, category)
}

// DeleteNomenclatureCategory удаляет категорию
// DELETE /api/v1/inventory/nomenclature/categories/:id
func (nc *NomenclatureController) DeleteNomenclatureCategory(c *gin.Context) {
	if nc.service == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "Сервис номенклатуры недоступен",
		})
		return
	}

	id := c.Param("id")
	if err := nc.service.DeleteCategory(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Ошибка удаления категории",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Категория удалена",
	})
}

// UploadNomenclatureFile определяет заголовки файла и возвращает список колонок
// POST /api/v1/inventory/nomenclature/upload-file
func (nc *NomenclatureController) UploadNomenclatureFile(c *gin.Context) {
	if nc.service == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "Сервис номенклатуры недоступен",
		})
		return
	}

	// Получаем файл из формы
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Файл не найден в запросе",
			"details": err.Error(),
		})
		return
	}
	defer file.Close()

	// Определяем заголовки
	headerRowIndex, columnNames, sampleRows, err := nc.service.DetectFileHeaders(file, header.Filename)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Ошибка определения заголовков",
			"details": err.Error(),
		})
		return
	}

	// Автоматически определяем маппинг полей
	autoMapping := make(map[string]string)
	for _, columnName := range columnNames {
		columnLower := strings.ToLower(columnName)
		
		// Название товара
		if autoMapping["name"] == "" {
			if strings.Contains(columnLower, "наименование") || 
			   strings.Contains(columnLower, "название") || 
			   strings.Contains(columnLower, "товар") ||
			   strings.Contains(columnLower, "name") ||
			   strings.Contains(columnLower, "product") {
				autoMapping["name"] = columnName
			}
		}
		
		// SKU/Артикул
		if autoMapping["sku"] == "" {
			if strings.Contains(columnLower, "sku") || 
			   strings.Contains(columnLower, "артикул") || 
			   strings.Contains(columnLower, "art") ||
			   strings.Contains(columnLower, "код") {
				autoMapping["sku"] = columnName
			}
		}
		
		// Категория
		if autoMapping["category"] == "" {
			if strings.Contains(columnLower, "категория") || 
			   strings.Contains(columnLower, "секция") || 
			   strings.Contains(columnLower, "category") ||
			   strings.Contains(columnLower, "section") {
				autoMapping["category"] = columnName
			}
		}
		
		// Единица измерения
		if autoMapping["unit"] == "" {
			if strings.Contains(columnLower, "единица") || 
			   strings.Contains(columnLower, "unit") ||
			   strings.Contains(columnLower, "ед") ||
			   strings.Contains(columnLower, "измерения") {
				autoMapping["unit"] = columnName
			}
		}
		
		// Цена
		if autoMapping["price"] == "" {
			if strings.Contains(columnLower, "цена") || 
			   strings.Contains(columnLower, "price") ||
			   strings.Contains(columnLower, "стоимость") ||
			   strings.Contains(columnLower, "cost") {
				autoMapping["price"] = columnName
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"header_row_index": headerRowIndex,
		"columns":          columnNames,
		"sample_rows":      sampleRows,
		"auto_mapping":     autoMapping,
		"count":            len(columnNames),
	})
}

// ParseNomenclatureFile парсит файл с использованием маппинга колонок
// POST /api/v1/inventory/nomenclature/parse-file
func (nc *NomenclatureController) ParseNomenclatureFile(c *gin.Context) {
	if nc.service == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "Сервис номенклатуры недоступен",
		})
		return
	}

	// Получаем файл из формы
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Файл не найден в запросе",
			"details": err.Error(),
		})
		return
	}
	defer file.Close()

	// Получаем маппинг колонок и список колонок из JSON
	var requestData struct {
		ColumnMapping map[string]string `json:"column_mapping" binding:"required"`
		Columns       []string           `json:"columns"` // Список колонок из первого этапа
		HeaderRowIndex int               `json:"header_row_index"` // Индекс строки заголовков
	}

	if err := c.ShouldBindJSON(&requestData); err != nil {
		// Пробуем получить из form-data
		mappingStr := c.PostForm("column_mapping")
		columnsStr := c.PostForm("columns")
		headerRowStr := c.PostForm("header_row_index")
		if mappingStr == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "Не указан маппинг колонок",
				"details": err.Error(),
			})
			return
		}
		// Парсим JSON из строки
		if err := json.Unmarshal([]byte(mappingStr), &requestData.ColumnMapping); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "Неверный формат маппинга колонок",
				"details": err.Error(),
			})
			return
		}
		if columnsStr != "" {
			json.Unmarshal([]byte(columnsStr), &requestData.Columns)
		}
		if headerRowStr != "" {
			fmt.Sscanf(headerRowStr, "%d", &requestData.HeaderRowIndex)
		}
	}

	log.Printf("📥 ParseFile: Получен маппинг: %v, Колонок: %d, HeaderRowIndex: %d", requestData.ColumnMapping, len(requestData.Columns), requestData.HeaderRowIndex)
	if len(requestData.Columns) > 0 {
		log.Printf("📥 ParseFile: Список колонок: %v", requestData.Columns)
	}

	// Парсим файл с маппингом и известными колонками
	rows, err := nc.service.ParseFileWithMapping(file, header.Filename, requestData.ColumnMapping, requestData.Columns, requestData.HeaderRowIndex)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Ошибка парсинга файла",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data":  rows,
		"count": len(rows),
	})
}

