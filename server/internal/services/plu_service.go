package services

import (
	"fmt"
	"log"
	"strings"

	"gorm.io/gorm"
	"zephyrvpn/server/internal/models"
)

type PLUService struct {
	db *gorm.DB
}

func NewPLUService(db *gorm.DB) *PLUService {
	return &PLUService{
		db: db,
	}
}

// FindPLUByProductName ищет PLU код по названию продукта (нечеткое совпадение)
func (ps *PLUService) FindPLUByProductName(productName string) (*models.PLUCode, error) {
	if productName == "" {
		return nil, nil
	}

	// Нормализуем название для поиска (убираем скобки, приводим к нижнему регистру)
	normalizedName := normalizeProductName(productName)

	var plu models.PLUCode
	
	// Ищем точное совпадение по названию (без учета регистра)
	if err := ps.db.Where("LOWER(TRIM(name)) = LOWER(TRIM(?))", normalizedName).
		Where("deleted_at IS NULL").
		First(&plu).Error; err == nil {
		return &plu, nil
	}

	// Ищем частичное совпадение (название содержит ключевые слова)
	keywords := extractKeywords(normalizedName)
	if len(keywords) > 0 {
		query := ps.db.Where("deleted_at IS NULL")
		for _, keyword := range keywords {
			if len(keyword) > 2 { // Игнорируем слишком короткие слова
				query = query.Where("LOWER(name) LIKE ? OR LOWER(name_en) LIKE ?", 
					"%"+strings.ToLower(keyword)+"%", 
					"%"+strings.ToLower(keyword)+"%")
			}
		}
		
		if err := query.First(&plu).Error; err == nil {
			return &plu, nil
		}
	}

	return nil, nil
}

// SuggestSKU предлагает SKU на основе PLU кода или генерирует уникальный
func (ps *PLUService) SuggestSKU(productName string, branchID string) (string, error) {
	// Сначала пытаемся найти PLU код
	plu, err := ps.FindPLUByProductName(productName)
	if err != nil {
		return "", err
	}

	if plu != nil {
		// Используем PLU код как основу для SKU
		// Формат: PLU + префикс филиала (если нужно)
		sku := plu.PLU
		if branchID != "" {
			// Можно добавить префикс филиала, но для глобальных PLU лучше оставить как есть
			// sku = branchID + "-" + plu.PLU
		}
		
		// Проверяем уникальность
		if ps.isSKUUnique(sku) {
			return sku, nil
		}
		
		// Если не уникален, добавляем суффикс
		counter := 1
		for {
			newSKU := fmt.Sprintf("%s-%d", sku, counter)
			if ps.isSKUUnique(newSKU) {
				return newSKU, nil
			}
			counter++
			if counter > 999 {
				break // Защита от бесконечного цикла
			}
		}
	}

	// Если PLU не найден, генерируем SKU на основе названия
	return ps.generateSKUFromName(productName, branchID), nil
}

// isSKUUnique проверяет уникальность SKU в базе данных
func (ps *PLUService) isSKUUnique(sku string) bool {
	if sku == "" {
		return false
	}

	var count int64
	ps.db.Model(&models.NomenclatureItem{}).
		Where("sku = ? AND deleted_at IS NULL", sku).
		Count(&count)
	
	return count == 0
}

// generateSKUFromName генерирует SKU на основе названия продукта
// Использует безопасный диапазон 8000-9999 для нестандартных товаров (не конфликтует с PLU)
func (ps *PLUService) generateSKUFromName(productName string, branchID string) string {
	// Генерируем числовой SKU в безопасном диапазоне 8000-9999
	// Это не конфликтует со стандартными PLU кодами (3000-6999)
	baseSKU := ps.generateNumericSKU(productName)
	
	// Проверяем уникальность и конфликты со стандартными PLU
	counter := 0
	sku := baseSKU
	maxAttempts := 2000 // 8000-9999 = 2000 возможных значений
	
	for !ps.isSKUUnique(sku) || ps.isStandardPLU(sku) {
		counter++
		if counter >= maxAttempts {
			// Если не удалось найти уникальный в диапазоне, используем буквенно-цифровой формат
			return ps.generateAlphanumericSKU(productName, branchID)
		}
		
		// Пробуем следующее число в диапазоне 8000-9999
		skuNum := 8000 + (counter % 2000)
		sku = fmt.Sprintf("%d", skuNum)
	}
	
	return sku
}

// generateNumericSKU генерирует числовой SKU на основе названия (хеш)
func (ps *PLUService) generateNumericSKU(productName string) string {
	// Простой хеш названия для получения числа в диапазоне 8000-9999
	hash := 0
	for _, char := range productName {
		hash = (hash*31 + int(char)) % 2000
	}
	skuNum := 8000 + hash
	return fmt.Sprintf("%d", skuNum)
}

// generateAlphanumericSKU генерирует буквенно-цифровой SKU (fallback)
func (ps *PLUService) generateAlphanumericSKU(productName string, branchID string) string {
	// Нормализуем название
	normalizedName := normalizeProductName(productName)
	
	// Берем первые буквы слов (до 6 символов)
	words := strings.Fields(normalizedName)
	sku := ""
	for _, word := range words {
		if len(word) > 0 {
			sku += strings.ToUpper(string(word[0]))
			if len(sku) >= 6 {
				break
			}
		}
	}
	
	// Добавляем префикс филиала если нужно
	if branchID != "" && len(branchID) > 0 {
		sku = strings.ToUpper(branchID[:min(3, len(branchID))]) + "-" + sku
	}
	
	// Убеждаемся, что SKU уникален и не конфликтует с PLU
	baseSKU := sku
	counter := 1
	for !ps.isSKUUnique(sku) || ps.isStandardPLU(sku) {
		sku = fmt.Sprintf("%s-%d", baseSKU, counter)
		counter++
		if counter > 999 {
			// Если не удалось найти уникальный, используем UUID
			return fmt.Sprintf("AUTO-%s", strings.ToUpper(productName[:min(8, len(productName))]))
		}
	}
	
	return sku
}

// isStandardPLU проверяет, не является ли SKU стандартным PLU кодом
func (ps *PLUService) isStandardPLU(sku string) bool {
	// Проверяем, есть ли такой PLU код в базе
	var count int64
	ps.db.Model(&models.PLUCode{}).
		Where("plu = ? AND deleted_at IS NULL", sku).
		Count(&count)
	
	return count > 0
}

// normalizeProductName нормализует название продукта для поиска
func normalizeProductName(name string) string {
	// Убираем скобки и их содержимое (например, "(гр.)", "(шт.)")
	name = strings.TrimSpace(name)
	
	// Убираем скобки
	name = strings.ReplaceAll(name, "(", " ")
	name = strings.ReplaceAll(name, ")", " ")
	
	// Убираем лишние пробелы
	words := strings.Fields(name)
	return strings.Join(words, " ")
}

// extractKeywords извлекает ключевые слова из названия
func extractKeywords(name string) []string {
	// Убираем стоп-слова
	stopWords := map[string]bool{
		"и": true, "или": true, "для": true, "на": true, "в": true,
		"с": true, "без": true, "по": true, "от": true, "до": true,
		"the": true, "and": true, "or": true, "for": true, "with": true,
	}
	
	words := strings.Fields(strings.ToLower(name))
	keywords := make([]string, 0)
	
	for _, word := range words {
		word = strings.Trim(word, ".,!?;:")
		if len(word) > 2 && !stopWords[word] {
			keywords = append(keywords, word)
		}
	}
	
	return keywords
}


// LoadStandardPLUCodes загружает стандартные PLU коды в базу данных
func (ps *PLUService) LoadStandardPLUCodes() error {
	// Стандартные PLU коды (IFPS)
	standardPLUs := []models.PLUCode{
		// Фрукты
		{PLU: "4011", Name: "Бананы", NameEN: "Bananas", Category: "Fruit"},
		{PLU: "4012", Name: "Бананы органические", NameEN: "Bananas Organic", Category: "Fruit", IsOrganic: true},
		{PLU: "4016", Name: "Бананы большие", NameEN: "Bananas Large", Category: "Fruit"},
		{PLU: "4020", Name: "Яблоки", NameEN: "Apples", Category: "Fruit"},
		{PLU: "4021", Name: "Яблоки органические", NameEN: "Apples Organic", Category: "Fruit", IsOrganic: true},
		{PLU: "4022", Name: "Яблоки Гренни Смит", NameEN: "Apples Granny Smith", Category: "Fruit", Variety: "Granny Smith"},
		{PLU: "4023", Name: "Яблоки Гала", NameEN: "Apples Gala", Category: "Fruit", Variety: "Gala"},
		{PLU: "4024", Name: "Яблоки Фуджи", NameEN: "Apples Fuji", Category: "Fruit", Variety: "Fuji"},
		{PLU: "4025", Name: "Яблоки Ред Делишес", NameEN: "Apples Red Delicious", Category: "Fruit", Variety: "Red Delicious"},
		{PLU: "4030", Name: "Апельсины", NameEN: "Oranges", Category: "Fruit"},
		{PLU: "4031", Name: "Апельсины органические", NameEN: "Oranges Organic", Category: "Fruit", IsOrganic: true},
		{PLU: "4032", Name: "Апельсины Навел", NameEN: "Oranges Navel", Category: "Fruit", Variety: "Navel"},
		{PLU: "4040", Name: "Лимон", NameEN: "Lemons", Category: "Fruit"},
		{PLU: "4041", Name: "Лимон органический", NameEN: "Lemons Organic", Category: "Fruit", IsOrganic: true},
		{PLU: "4050", Name: "Лайм", NameEN: "Limes", Category: "Fruit"},
		{PLU: "4060", Name: "Грейпфрут", NameEN: "Grapefruit", Category: "Fruit"},
		{PLU: "4070", Name: "Виноград", NameEN: "Grapes", Category: "Fruit"},
		{PLU: "4071", Name: "Виноград органический", NameEN: "Grapes Organic", Category: "Fruit", IsOrganic: true},
		{PLU: "4072", Name: "Виноград красный", NameEN: "Grapes Red", Category: "Fruit", Variety: "Red"},
		{PLU: "4073", Name: "Виноград зеленый", NameEN: "Grapes Green", Category: "Fruit", Variety: "Green"},
		{PLU: "4080", Name: "Клубника", NameEN: "Strawberries", Category: "Fruit"},
		{PLU: "4081", Name: "Клубника органическая", NameEN: "Strawberries Organic", Category: "Fruit", IsOrganic: true},
		{PLU: "4090", Name: "Черника", NameEN: "Blueberries", Category: "Fruit"},
		{PLU: "4091", Name: "Черника органическая", NameEN: "Blueberries Organic", Category: "Fruit", IsOrganic: true},
		{PLU: "4100", Name: "Малина", NameEN: "Raspberries", Category: "Fruit"},
		{PLU: "4110", Name: "Ежевика", NameEN: "Blackberries", Category: "Fruit"},
		{PLU: "4120", Name: "Вишня", NameEN: "Cherries", Category: "Fruit"},
		{PLU: "4130", Name: "Персики", NameEN: "Peaches", Category: "Fruit"},
		{PLU: "4140", Name: "Нектарины", NameEN: "Nectarines", Category: "Fruit"},
		{PLU: "4150", Name: "Сливы", NameEN: "Plums", Category: "Fruit"},
		{PLU: "4160", Name: "Абрикосы", NameEN: "Apricots", Category: "Fruit"},
		{PLU: "4170", Name: "Груши", NameEN: "Pears", Category: "Fruit"},
		{PLU: "4171", Name: "Груши органические", NameEN: "Pears Organic", Category: "Fruit", IsOrganic: true},
		{PLU: "4180", Name: "Ананасы", NameEN: "Pineapples", Category: "Fruit"},
		{PLU: "4190", Name: "Манго", NameEN: "Mangoes", Category: "Fruit"},
		{PLU: "4200", Name: "Папайя", NameEN: "Papayas", Category: "Fruit"},
		{PLU: "4210", Name: "Киви", NameEN: "Kiwis", Category: "Fruit"},
		{PLU: "4220", Name: "Авокадо", NameEN: "Avocados", Category: "Fruit"},
		{PLU: "4221", Name: "Авокадо органический", NameEN: "Avocados Organic", Category: "Fruit", IsOrganic: true},
		{PLU: "4230", Name: "Дыня", NameEN: "Cantaloupes", Category: "Fruit"},
		{PLU: "4240", Name: "Арбуз", NameEN: "Watermelons", Category: "Fruit"},
		{PLU: "4250", Name: "Медовая дыня", NameEN: "Honeydew Melons", Category: "Fruit"},
		
		// Овощи
		{PLU: "4510", Name: "Помидоры", NameEN: "Tomatoes", Category: "Vegetable"},
		{PLU: "4511", Name: "Помидоры органические", NameEN: "Tomatoes Organic", Category: "Vegetable", IsOrganic: true},
		{PLU: "4512", Name: "Помидоры черри", NameEN: "Tomatoes Cherry", Category: "Vegetable", Variety: "Cherry"},
		{PLU: "4513", Name: "Помидоры на ветке", NameEN: "Tomatoes on the Vine", Category: "Vegetable"},
		{PLU: "4520", Name: "Огурцы", NameEN: "Cucumbers", Category: "Vegetable"},
		{PLU: "4521", Name: "Огурцы органические", NameEN: "Cucumbers Organic", Category: "Vegetable", IsOrganic: true},
		{PLU: "4530", Name: "Перец болгарский", NameEN: "Bell Peppers", Category: "Vegetable"},
		{PLU: "4531", Name: "Перец болгарский красный", NameEN: "Bell Peppers Red", Category: "Vegetable", Variety: "Red"},
		{PLU: "4532", Name: "Перец болгарский зеленый", NameEN: "Bell Peppers Green", Category: "Vegetable", Variety: "Green"},
		{PLU: "4533", Name: "Перец болгарский желтый", NameEN: "Bell Peppers Yellow", Category: "Vegetable", Variety: "Yellow"},
		{PLU: "4540", Name: "Перец чили", NameEN: "Chili Peppers", Category: "Vegetable"},
		{PLU: "4550", Name: "Лук репчатый", NameEN: "Onions", Category: "Vegetable"},
		{PLU: "4551", Name: "Лук репчатый органический", NameEN: "Onions Organic", Category: "Vegetable", IsOrganic: true},
		{PLU: "4552", Name: "Лук красный", NameEN: "Onions Red", Category: "Vegetable", Variety: "Red"},
		{PLU: "4553", Name: "Лук белый", NameEN: "Onions White", Category: "Vegetable", Variety: "White"},
		{PLU: "4554", Name: "Лук желтый", NameEN: "Onions Yellow", Category: "Vegetable", Variety: "Yellow"},
		{PLU: "4560", Name: "Чеснок", NameEN: "Garlic", Category: "Vegetable"},
		{PLU: "4570", Name: "Морковь", NameEN: "Carrots", Category: "Vegetable"},
		{PLU: "4571", Name: "Морковь органическая", NameEN: "Carrots Organic", Category: "Vegetable", IsOrganic: true},
		{PLU: "4580", Name: "Картофель", NameEN: "Potatoes", Category: "Vegetable"},
		{PLU: "4581", Name: "Картофель органический", NameEN: "Potatoes Organic", Category: "Vegetable", IsOrganic: true},
		{PLU: "4582", Name: "Картофель красный", NameEN: "Potatoes Red", Category: "Vegetable", Variety: "Red"},
		{PLU: "4583", Name: "Картофель белый", NameEN: "Potatoes White", Category: "Vegetable", Variety: "White"},
		{PLU: "4590", Name: "Свекла", NameEN: "Beets", Category: "Vegetable"},
		{PLU: "4600", Name: "Капуста", NameEN: "Cabbage", Category: "Vegetable"},
		{PLU: "4610", Name: "Капуста белокочанная", NameEN: "Cabbage White", Category: "Vegetable", Variety: "White"},
		{PLU: "4620", Name: "Капуста краснокочанная", NameEN: "Cabbage Red", Category: "Vegetable", Variety: "Red"},
		{PLU: "4630", Name: "Брокколи", NameEN: "Broccoli", Category: "Vegetable"},
		{PLU: "4640", Name: "Цветная капуста", NameEN: "Cauliflower", Category: "Vegetable"},
		{PLU: "4650", Name: "Салат", NameEN: "Lettuce", Category: "Vegetable"},
		{PLU: "4651", Name: "Салат органический", NameEN: "Lettuce Organic", Category: "Vegetable", IsOrganic: true},
		{PLU: "4652", Name: "Салат Айсберг", NameEN: "Lettuce Iceberg", Category: "Vegetable", Variety: "Iceberg"},
		{PLU: "4653", Name: "Салат Романо", NameEN: "Lettuce Romaine", Category: "Vegetable", Variety: "Romaine"},
		{PLU: "4660", Name: "Шпинат", NameEN: "Spinach", Category: "Vegetable"},
		{PLU: "4670", Name: "Сельдерей", NameEN: "Celery", Category: "Vegetable"},
		{PLU: "4680", Name: "Спаржа", NameEN: "Asparagus", Category: "Vegetable"},
		{PLU: "4690", Name: "Кабачки", NameEN: "Zucchini", Category: "Vegetable"},
		{PLU: "4700", Name: "Баклажаны", NameEN: "Eggplants", Category: "Vegetable"},
		{PLU: "4710", Name: "Грибы", NameEN: "Mushrooms", Category: "Vegetable"},
		{PLU: "4711", Name: "Грибы органические", NameEN: "Mushrooms Organic", Category: "Vegetable", IsOrganic: true},
		{PLU: "4712", Name: "Шампиньоны", NameEN: "Mushrooms Button", Category: "Vegetable", Variety: "Button"},
		{PLU: "4720", Name: "Кукуруза", NameEN: "Corn", Category: "Vegetable"},
		{PLU: "4730", Name: "Горох", NameEN: "Peas", Category: "Vegetable"},
		{PLU: "4740", Name: "Фасоль", NameEN: "Beans", Category: "Vegetable"},
		{PLU: "4750", Name: "Бобы", NameEN: "Lima Beans", Category: "Vegetable"},
		
		// Мясо и рыба (нестандартные PLU, используем диапазон 5000-5999)
		{PLU: "5001", Name: "Говядина", NameEN: "Beef", Category: "Meat"},
		{PLU: "5002", Name: "Свинина", NameEN: "Pork", Category: "Meat"},
		{PLU: "5003", Name: "Курица", NameEN: "Chicken", Category: "Meat"},
		{PLU: "5004", Name: "Индейка", NameEN: "Turkey", Category: "Meat"},
		{PLU: "5005", Name: "Баранина", NameEN: "Lamb", Category: "Meat"},
		{PLU: "5010", Name: "Лосось", NameEN: "Salmon", Category: "Fish"},
		{PLU: "5011", Name: "Тунец", NameEN: "Tuna", Category: "Fish"},
		{PLU: "5012", Name: "Креветки", NameEN: "Shrimp", Category: "Fish"},
		{PLU: "5013", Name: "Креветки тигровые", NameEN: "Shrimp Tiger", Category: "Fish", Variety: "Tiger"},
		
		// Молочные продукты (диапазон 6000-6999)
		{PLU: "6001", Name: "Молоко", NameEN: "Milk", Category: "Dairy"},
		{PLU: "6002", Name: "Молоко органическое", NameEN: "Milk Organic", Category: "Dairy", IsOrganic: true},
		{PLU: "6003", Name: "Сыр", NameEN: "Cheese", Category: "Dairy"},
		{PLU: "6004", Name: "Сыр Моцарелла", NameEN: "Cheese Mozzarella", Category: "Dairy", Variety: "Mozzarella"},
		{PLU: "6005", Name: "Сыр Чеддер", NameEN: "Cheese Cheddar", Category: "Dairy", Variety: "Cheddar"},
		{PLU: "6006", Name: "Сыр Пармезан", NameEN: "Cheese Parmesan", Category: "Dairy", Variety: "Parmesan"},
		{PLU: "6007", Name: "Сметана", NameEN: "Sour Cream", Category: "Dairy"},
		{PLU: "6008", Name: "Сливки", NameEN: "Cream", Category: "Dairy"},
		{PLU: "6009", Name: "Йогурт", NameEN: "Yogurt", Category: "Dairy"},
		{PLU: "6010", Name: "Масло", NameEN: "Butter", Category: "Dairy"},
		{PLU: "6011", Name: "Яйца", NameEN: "Eggs", Category: "Dairy"},
		{PLU: "6012", Name: "Яйца органические", NameEN: "Eggs Organic", Category: "Dairy", IsOrganic: true},
	}

	// Загружаем PLU коды в базу данных (только если их еще нет)
	for _, plu := range standardPLUs {
		var existing models.PLUCode
		if err := ps.db.Where("plu = ? AND deleted_at IS NULL", plu.PLU).First(&existing).Error; err != nil {
			// PLU код не существует, создаем его
			if err := ps.db.Create(&plu).Error; err != nil {
				log.Printf("⚠️ Ошибка создания PLU кода %s: %v", plu.PLU, err)
			} else {
				log.Printf("✅ Создан PLU код: %s - %s", plu.PLU, plu.Name)
			}
		}
	}

	log.Printf("✅ Загружено %d стандартных PLU кодов", len(standardPLUs))
	return nil
}

