package services

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// UoMParser парсит текстовые описания единиц измерения и вычисляет множитель конвертации
type UoMParser struct{}

// NewUoMParser создает новый парсер единиц измерения
func NewUoMParser() *UoMParser {
	return &UoMParser{}
}

// ParseQuantityResult содержит результат парсинга
type ParseQuantityResult struct {
	Multiplier float64 `json:"multiplier"`
	Extracted  string  `json:"extracted"` // Извлеченное значение (например, "2.4 л")
	Message    string  `json:"message"`   // Сообщение о результате парсинга
}

// ParseQuantity парсит текстовое описание единицы измерения и вычисляет множитель конвертации
// Пример: "бутылка 2,4 л" + "мл" -> 2400
func (p *UoMParser) ParseQuantity(inputUOMText, baseUOM string) (*ParseQuantityResult, error) {
	// Нормализуем входные данные
	inputUOMText = strings.TrimSpace(inputUOMText)
	baseUOM = strings.TrimSpace(strings.ToLower(baseUOM))

	if inputUOMText == "" {
		return nil, fmt.Errorf("текст единицы измерения не может быть пустым")
	}

	if baseUOM == "" {
		return nil, fmt.Errorf("базовая единица не может быть пустой")
	}

	// Извлекаем число и единицу из текста
	quantity, unit, err := p.extractQuantityAndUnit(inputUOMText)
	if err != nil {
		return nil, fmt.Errorf("не удалось извлечь количество и единицу: %w", err)
	}

	// Нормализуем единицу
	normalizedUnit := p.normalizeUnit(unit)
	normalizedBase := p.normalizeUnit(baseUOM)

	// Вычисляем множитель
	multiplier, message, err := p.calculateMultiplier(quantity, normalizedUnit, normalizedBase)
	if err != nil {
		return nil, err
	}

	return &ParseQuantityResult{
		Multiplier: multiplier,
		Extracted:  fmt.Sprintf("%.4f %s", quantity, normalizedUnit),
		Message:    message,
	}, nil
}

// extractQuantityAndUnit извлекает количество и единицу измерения из текста
// Примеры:
//   - "бутылка 2,4 л" -> (2.4, "л")
//   - "упак (6 шт х 2 л)" -> (12, "л") или (6, "шт") в зависимости от приоритета
//   - "10 кг" -> (10, "кг")
func (p *UoMParser) extractQuantityAndUnit(text string) (float64, string, error) {
	// Убираем лишние пробелы и приводим к нижнему регистру для анализа
	text = strings.ToLower(strings.TrimSpace(text))

	// Паттерны для поиска чисел и единиц
	// Поддерживаем как запятую, так и точку в качестве разделителя
	numberPattern := `(\d+[,.]?\d*)`
	
	// Список известных единиц измерения (в порядке приоритета)
	units := []string{
		"кг", "kg", "килограмм", "килограммов",
		"г", "g", "грамм", "граммов", "грамма",
		"л", "l", "литр", "литров", "литра",
		"мл", "ml", "миллилитр", "миллилитров", "миллилитра",
		"шт", "pcs", "штук", "штука",
		"упак", "упаковка", "упаковок", "box",
	}

	// Ищем все числа в тексте
	numberRegex := regexp.MustCompile(numberPattern)
	numbers := numberRegex.FindAllString(text, -1)

	if len(numbers) == 0 {
		return 0, "", fmt.Errorf("не найдено число в тексте: %s", text)
	}

	// Ищем единицы измерения
	var foundUnit string
	var foundQuantity float64

	// Специальная обработка для сложных случаев типа "упак (6 шт х 2 л)"
	if strings.Contains(text, "х") || strings.Contains(text, "x") || strings.Contains(text, "*") {
		return p.parseComplexUnit(text, units)
	}

	// Простой случай: ищем первое число и первую единицу
	for _, numStr := range numbers {
		// Заменяем запятую на точку
		numStr = strings.Replace(numStr, ",", ".", 1)
		quantity, err := strconv.ParseFloat(numStr, 64)
		if err != nil {
			continue
		}

		// Ищем единицу рядом с числом
		for _, unit := range units {
			// Ищем единицу в тексте (до или после числа)
			unitPattern := regexp.MustCompile(regexp.QuoteMeta(unit))
			if unitPattern.MatchString(text) {
				// Проверяем, что единица находится рядом с числом
				numIndex := strings.Index(text, numStr)
				unitIndex := strings.Index(text, unit)
				
				// Если единица находится в пределах 10 символов от числа
				if abs(unitIndex-numIndex) <= 10 {
					foundUnit = unit
					foundQuantity = quantity
					break
				}
			}
		}

		if foundUnit != "" {
			break
		}
	}

	// Если не нашли единицу, но есть число, используем первое число
	if foundUnit == "" && len(numbers) > 0 {
		numStr := strings.Replace(numbers[0], ",", ".", 1)
		quantity, err := strconv.ParseFloat(numStr, 64)
		if err == nil {
			// Пытаемся угадать единицу по контексту
			// Если в тексте есть ключевые слова, используем их
			if strings.Contains(text, "литр") || strings.Contains(text, "л") {
				foundUnit = "л"
			} else if strings.Contains(text, "килограмм") || strings.Contains(text, "кг") {
				foundUnit = "кг"
			} else if strings.Contains(text, "грамм") || strings.Contains(text, "г") {
				foundUnit = "г"
			} else if strings.Contains(text, "штук") || strings.Contains(text, "шт") {
				foundUnit = "шт"
			} else {
				return 0, "", fmt.Errorf("не удалось определить единицу измерения в тексте: %s", text)
			}
			foundQuantity = quantity
		}
	}

	if foundUnit == "" {
		return 0, "", fmt.Errorf("не найдена единица измерения в тексте: %s", text)
	}

	return foundQuantity, foundUnit, nil
}

// parseComplexUnit парсит сложные единицы типа "упак (6 шт х 2 л)"
func (p *UoMParser) parseComplexUnit(text string, units []string) (float64, string, error) {
	// Ищем паттерн типа "число единица х число единица"
	complexPattern := regexp.MustCompile(`(\d+[,.]?\d*)\s*([а-яa-z]+)\s*[хx*]\s*(\d+[,.]?\d*)\s*([а-яa-z]+)`)
	matches := complexPattern.FindStringSubmatch(text)

	if len(matches) >= 5 {
		// Извлекаем оба числа и единицы
		qty1Str := strings.Replace(matches[1], ",", ".", 1)
		unit1 := matches[2]
		qty2Str := strings.Replace(matches[3], ",", ".", 1)
		unit2 := matches[4]

		qty1, err1 := strconv.ParseFloat(qty1Str, 64)
		qty2, err2 := strconv.ParseFloat(qty2Str, 64)

		if err1 == nil && err2 == nil {
			// Нормализуем единицы
			normUnit1 := p.normalizeUnit(unit1)
			normUnit2 := p.normalizeUnit(unit2)

			// Если единицы одинаковые, умножаем количества
			if normUnit1 == normUnit2 {
				return qty1 * qty2, normUnit1, nil
			}

			// Если разные единицы, возвращаем первую единицу с общим количеством
			// Например, "6 шт х 2 л" -> возвращаем 6 шт (количество упаковок)
			// Множитель будет вычислен позже на основе базовой единицы
			return qty1, normUnit1, nil
		}
	}

	// Если не удалось распарсить сложную единицу, пробуем простой парсинг
	return p.extractQuantityAndUnit(text)
}

// normalizeUnit нормализует единицу измерения к стандартному виду
func (p *UoMParser) normalizeUnit(unit string) string {
	unit = strings.ToLower(strings.TrimSpace(unit))

	// Масса
	if unit == "кг" || unit == "kg" || unit == "килограмм" || unit == "килограммов" || unit == "килограмма" {
		return "kg"
	}
	if unit == "г" || unit == "g" || unit == "грамм" || unit == "граммов" || unit == "грамма" {
		return "g"
	}

	// Объем
	if unit == "л" || unit == "l" || unit == "литр" || unit == "литров" || unit == "литра" {
		return "l"
	}
	if unit == "мл" || unit == "ml" || unit == "миллилитр" || unit == "миллилитров" || unit == "миллилитра" {
		return "ml"
	}

	// Штуки
	if unit == "шт" || unit == "pcs" || unit == "штук" || unit == "штука" {
		return "pcs"
	}
	if unit == "упак" || unit == "упаковка" || unit == "упаковок" || unit == "box" {
		return "pcs" // Упаковка считается как штука
	}

	return unit
}

// calculateMultiplier вычисляет множитель конвертации между единицами
func (p *UoMParser) calculateMultiplier(quantity float64, fromUnit, toUnit string) (float64, string, error) {
	// Если единицы одинаковые, множитель = количество
	if fromUnit == toUnit {
		return quantity, fmt.Sprintf("Единицы одинаковые: %.4f %s", quantity, fromUnit), nil
	}

	// Конвертация между единицами одного типа (масса или объем)
	multiplier, err := p.convertSameType(fromUnit, toUnit)
	if err == nil {
		return quantity * multiplier, fmt.Sprintf("Конвертация: %.4f %s -> %.4f %s", quantity, fromUnit, quantity*multiplier, toUnit), nil
	}

	// Конвертация между массой и объемом (используем плотность воды: 1 кг/л)
	if p.isMass(fromUnit) && p.isVolume(toUnit) {
		// Масса -> Объем: используем плотность воды
		// 1 кг = 1 л (для воды)
		massInKg := p.toKilograms(quantity, fromUnit)
		volumeInL := massInKg // 1 кг = 1 л для воды
		volumeInTarget := p.fromLiters(volumeInL, toUnit)
		return volumeInTarget, fmt.Sprintf("Конвертация массы в объем (плотность воды): %.4f %s -> %.4f %s", quantity, fromUnit, volumeInTarget, toUnit), nil
	}

	if p.isVolume(fromUnit) && p.isMass(toUnit) {
		// Объем -> Масса: используем плотность воды
		volumeInL := p.toLiters(quantity, fromUnit)
		massInKg := volumeInL // 1 л = 1 кг для воды
		massInTarget := p.fromKilograms(massInKg, toUnit)
		return massInTarget, fmt.Sprintf("Конвертация объема в массу (плотность воды): %.4f %s -> %.4f %s", quantity, fromUnit, massInTarget, toUnit), nil
	}

	return 0, "", fmt.Errorf("неподдерживаемая конвертация: %s -> %s", fromUnit, toUnit)
}

// convertSameType конвертирует между единицами одного типа
func (p *UoMParser) convertSameType(fromUnit, toUnit string) (float64, error) {
	// Масса
	if p.isMass(fromUnit) && p.isMass(toUnit) {
		fromKg := p.toKilograms(1, fromUnit)
		toKg := p.toKilograms(1, toUnit)
		return fromKg / toKg, nil
	}

	// Объем
	if p.isVolume(fromUnit) && p.isVolume(toUnit) {
		fromL := p.toLiters(1, fromUnit)
		toL := p.toLiters(1, toUnit)
		return fromL / toL, nil
	}

	// Штуки
	if p.isCount(fromUnit) && p.isCount(toUnit) {
		return 1, nil // Штуки не конвертируются
	}

	return 0, fmt.Errorf("разные типы единиц")
}

// Вспомогательные функции для работы с единицами

func (p *UoMParser) isMass(unit string) bool {
	return unit == "kg" || unit == "g"
}

func (p *UoMParser) isVolume(unit string) bool {
	return unit == "l" || unit == "ml"
}

func (p *UoMParser) isCount(unit string) bool {
	return unit == "pcs"
}

func (p *UoMParser) toKilograms(quantity float64, unit string) float64 {
	switch unit {
	case "kg":
		return quantity
	case "g":
		return quantity / 1000
	default:
		return quantity
	}
}

func (p *UoMParser) fromKilograms(quantityKg float64, unit string) float64 {
	switch unit {
	case "kg":
		return quantityKg
	case "g":
		return quantityKg * 1000
	default:
		return quantityKg
	}
}

func (p *UoMParser) toLiters(quantity float64, unit string) float64 {
	switch unit {
	case "l":
		return quantity
	case "ml":
		return quantity / 1000
	default:
		return quantity
	}
}

func (p *UoMParser) fromLiters(quantityL float64, unit string) float64 {
	switch unit {
	case "l":
		return quantityL
	case "ml":
		return quantityL * 1000
	default:
		return quantityL
	}
}

// abs возвращает абсолютное значение числа
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

