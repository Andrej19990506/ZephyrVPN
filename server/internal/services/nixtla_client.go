package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"time"
)

// NixtlaClient клиент для работы с Nixtla API
type NixtlaClient struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

// NewNixtlaClient создает новый клиент Nixtla
func NewNixtlaClient(apiKey string) *NixtlaClient {
	// Проверяем, что ключ не пустой и не обрезан
	if apiKey == "" {
		log.Printf("⚠️ Nixtla: API ключ пустой")
	} else if len(apiKey) < 20 {
		log.Printf("⚠️ Nixtla: API ключ слишком короткий (%d символов), возможно обрезан", len(apiKey))
	} else {
		// Логируем только первые и последние 4 символа для безопасности
		maskedKey := apiKey[:4] + "..." + apiKey[len(apiKey)-4:]
		log.Printf("✅ Nixtla: API ключ установлен (длина: %d, маска: %s)", len(apiKey), maskedKey)
	}
	
	// Используем TimeGPT-1 API endpoint (api.nixtla.io)
	// Для TimeGPT-2 используйте: "https://api-preview.nixtla.io" (требует специального доступа)
	baseURL := "https://api.nixtla.io"
	log.Printf("✅ Nixtla: используется TimeGPT-1 API endpoint: %s", baseURL)
	
	return &NixtlaClient{
		apiKey:  apiKey,
		baseURL: baseURL,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// TimeSeriesData представляет временной ряд для прогнозирования
// Используется для внутренней обработки, перед отправкой преобразуется в формат Nixtla
type TimeSeriesData struct {
	DS string             // Дата в формате YYYY-MM-DD
	Y  float64            // Значение выручки
	X  map[string]float64 // Внешние регрессоры в формате словаря
}

// TimeSeriesPoint представляет одну точку данных для Nixtla API
// Формат согласно документации: {"ds": "YYYY-MM-DD", "y": 123456.78}
// unique_id опционален и используется только для мультисерийных данных
type TimeSeriesPoint struct {
	DS string  `json:"ds"`        // Дата в формате YYYY-MM-DD (обязательно)
	Y  float64 `json:"y"`         // Значение выручки (обязательно, float64)
	// unique_id не добавляем, так как у нас одна серия данных
}

// ForecastRequest запрос на прогнозирование в формате TimeGPT REST API
// Правильный формат согласно документации:
// {
//   "model": "timegpt-1",
//   "freq": "D",
//   "h": 20,
//   "df": [
//     {"ds": "2025-12-10", "y": 299107},
//     {"ds": "2025-12-11", "y": 264371},
//     ...
//   ]
// }
// Для TimeGPT-2 (требует специального доступа): используйте "timegpt-2.1", "timegpt-2-pro", "timegpt-2-lab", "timegpt-2-mini"
type ForecastRequest struct {
	Model string            `json:"model"`                  // Модель (обязательно: "timegpt-1" для стандартной подписки)
	Freq  string            `json:"freq"`                  // Частота данных (обязательно: "D" для дней)
	H     int               `json:"h"`                      // Горизонт прогноза (обязательно)
	DF    []TimeSeriesPoint `json:"df"`                    // Массив точек данных (обязательно)
	Level []float64         `json:"level,omitempty"`        // Уровни доверительных интервалов (опционально)
}

// ForecastResponse ответ от Nixtla API
// Формат ответа: {"timestamp": [...], "value": [...], "level": {...}, ...}
// ВАЖНО: API возвращает timestamp и value на верхнем уровне, а не внутри forecast
// ВАЖНО: API может возвращать даты в формате "YYYY-MM-DD HH:MM:SS" или "YYYY-MM-DD"
// ВАЖНО: API может применять логарифмическую трансформацию по умолчанию для больших значений
type ForecastResponse struct {
	Timestamp     []string  `json:"timestamp"`      // Массив дат прогноза (может быть в формате "YYYY-MM-DD HH:MM:SS")
	Value         []float64 `json:"value"`          // Массив значений прогноза (может быть в логарифмическом масштабе)
	Level         map[string]struct {
		Lo []float64 `json:"lo"`
		Hi []float64 `json:"hi"`
	} `json:"level,omitempty"`                      // Доверительные интервалы по уровням
	Model         string    `json:"model,omitempty"`
	InputTokens   int       `json:"input_tokens,omitempty"`
	OutputTokens  int       `json:"output_tokens,omitempty"`
	FinetuneTokens int      `json:"finetune_tokens,omitempty"`
	RequestID     string    `json:"request_id,omitempty"`
}

// Forecast выполняет прогнозирование временного ряда
func (nc *NixtlaClient) Forecast(req *ForecastRequest) (*ForecastResponse, error) {
	if nc.apiKey == "" {
		return nil, fmt.Errorf("Nixtla API key is not set")
	}

	// Подготавливаем данные для запроса
	requestBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Создаем HTTP запрос - используем правильный endpoint /timegpt
	// Для TimeGPT-2 используется тот же endpoint, но другой baseURL (api-preview.nixtla.io)
	url := fmt.Sprintf("%s/timegpt", nc.baseURL)
	httpReq, err := http.NewRequest("POST", url, bytes.NewBuffer(requestBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Устанавливаем заголовки - используем x-api-key вместо Bearer
	httpReq.Header.Set("Content-Type", "application/json")
	// Используем правильный формат авторизации: Authorization: Bearer <api_key>
	httpReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", nc.apiKey))
	
	// Логируем запрос для отладки (первые 500 символов)
	if len(requestBody) > 0 {
		requestPreview := string(requestBody)
		if len(requestPreview) > 500 {
			requestPreview = requestPreview[:500] + "..."
		}
		log.Printf("🤖 Nixtla: отправка запроса на %s (данных: %d точек, горизонт: %d)", 
			url, len(req.DF), req.H)
		log.Printf("📤 Nixtla: тело запроса (первые 500 символов): %s", requestPreview)
	}

	// Выполняем запрос
	resp, err := nc.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	// Читаем ответ
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Проверяем статус код
	if resp.StatusCode != http.StatusOK {
		log.Printf("❌ Nixtla API error (status %d): %s", resp.StatusCode, string(body))
		// Логируем тело запроса для отладки (первые 500 символов)
		if len(requestBody) > 0 {
			requestPreview := string(requestBody)
			if len(requestPreview) > 500 {
				requestPreview = requestPreview[:500] + "..."
			}
			log.Printf("📤 Nixtla: отправленный запрос (первые 500 символов): %s", requestPreview)
		}
		return nil, fmt.Errorf("Nixtla API error (status %d): %s", resp.StatusCode, string(body))
	}

	// Логируем сырой ответ API для отладки (первые 1000 символов)
	rawResponsePreview := string(body)
	if len(rawResponsePreview) > 1000 {
		rawResponsePreview = rawResponsePreview[:1000] + "..."
	}
	log.Printf("📥 Nixtla: сырой ответ API (первые 1000 символов): %s", rawResponsePreview)

	// Парсим ответ
	var forecastResp ForecastResponse
	if err := json.Unmarshal(body, &forecastResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	// Логируем полученный прогноз для отладки
	if len(forecastResp.Value) > 0 {
		sum := 0.0
		for _, v := range forecastResp.Value {
			sum += v
		}
		log.Printf("✅ Nixtla: получен прогноз - %d значений, первое: %.2f, последнее: %.2f, сумма: %.2f", 
			len(forecastResp.Value), 
			forecastResp.Value[0], 
			forecastResp.Value[len(forecastResp.Value)-1],
			sum)
		
		// ПРОБЛЕМА 1: Проверяем масштаб значений
		// Если значения очень маленькие (меньше 100), возможно API применил логарифмическую трансформацию
		// или модель не поняла масштаб. Проверяем среднее значение исторических данных для сравнения.
		avgForecastValue := sum / float64(len(forecastResp.Value))
		if avgForecastValue < 100 {
			log.Printf("⚠️ Nixtla: ВНИМАНИЕ! Среднее значение прогноза (%.2f) очень маленькое. Возможно, API применил логарифмическую трансформацию или модель не поняла масштаб данных.", avgForecastValue)
		}
		
		// ПРОБЛЕМА 2: Парсим и проверяем даты
		if len(forecastResp.Timestamp) > 0 {
			firstTimestamp := forecastResp.Timestamp[0]
			lastTimestamp := forecastResp.Timestamp[len(forecastResp.Timestamp)-1]
			log.Printf("📅 Nixtla: даты прогноза (сырые) - с %s по %s", firstTimestamp, lastTimestamp)
			
			// Пробуем распарсить дату в разных форматах
			parsedFirstDate, err := parseNixtlaTimestamp(firstTimestamp)
			if err != nil {
				log.Printf("⚠️ Nixtla: ошибка парсинга первой даты '%s': %v", firstTimestamp, err)
			} else {
				log.Printf("📅 Nixtla: первая дата (распарсена): %s", parsedFirstDate.Format("2006-01-02"))
				// Проверяем, что дата не в прошлом (2016 год - это явно ошибка)
				if parsedFirstDate.Year() < 2020 {
					log.Printf("❌ Nixtla: КРИТИЧЕСКАЯ ОШИБКА! Дата прогноза (%s) в прошлом (год %d). API вернул неправильные даты!", 
						parsedFirstDate.Format("2006-01-02"), parsedFirstDate.Year())
				}
			}
			
			parsedLastDate, err := parseNixtlaTimestamp(lastTimestamp)
			if err != nil {
				log.Printf("⚠️ Nixtla: ошибка парсинга последней даты '%s': %v", lastTimestamp, err)
			} else {
				log.Printf("📅 Nixtla: последняя дата (распарсена): %s", parsedLastDate.Format("2006-01-02"))
			}
		}
	} else {
		log.Printf("⚠️ Nixtla: получен пустой прогноз (0 значений)")
	}

	return &forecastResp, nil
}

// parseNixtlaTimestamp парсит timestamp от Nixtla API в разных форматах
// API может возвращать даты в форматах:
// - "YYYY-MM-DD HH:MM:SS" (например, "2016-01-14 00:00:00")
// - "YYYY-MM-DD" (например, "2016-01-14")
// - "YYYY-MM-DDTHH:MM:SS" (ISO 8601)
func parseNixtlaTimestamp(timestamp string) (time.Time, error) {
	// Пробуем разные форматы
	formats := []string{
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02",
		time.RFC3339,
		time.RFC3339Nano,
	}
	
	for _, format := range formats {
		if parsed, err := time.Parse(format, timestamp); err == nil {
			return parsed, nil
		}
	}
	
	return time.Time{}, fmt.Errorf("не удалось распарсить timestamp '%s' ни в одном из форматов", timestamp)
}

// ForecastRevenue прогнозирует выручку на основе исторических данных
// historicalData - массив исторических значений выручки по дням
// horizon - количество дней для прогноза
// futureExogenous - внешние регрессоры для будущих периодов (не используется в текущей версии)
// Для длинных горизонтов (более 30 дней) автоматически используется модель "timegpt-1-long-horizon"
func (nc *NixtlaClient) ForecastRevenue(historicalData []TimeSeriesData, horizon int, futureExogenous []map[string]float64) (*ForecastResponse, error) {
	const uniqueID = "revenue_krasnoyarsk" // Константа для идентификации временного ряда
	// Nixtla требует минимум 2 точки данных, но рекомендуется 7+
	if len(historicalData) < 2 {
		return nil, fmt.Errorf("insufficient historical data: need at least 2 days, got %d", len(historicalData))
	}

	// Валидация данных: проверяем формат дат и наличие значений
	validData := make([]TimeSeriesData, 0)
	for _, data := range historicalData {
		// Проверяем, что дата в правильном формате YYYY-MM-DD
		if data.DS == "" {
			log.Printf("⚠️ Nixtla: пропущена запись с пустой датой")
			continue
		}
		// Проверяем формат даты
		if _, err := time.Parse("2006-01-02", data.DS); err != nil {
			log.Printf("⚠️ Nixtla: пропущена запись с неверным форматом даты: %s", data.DS)
			continue
		}
		// Проверяем, что значение не отрицательное
		if data.Y < 0 {
			log.Printf("⚠️ Nixtla: пропущена запись с отрицательным значением: %.2f", data.Y)
			continue
		}
		validData = append(validData, data)
	}

	if len(validData) < 2 {
		return nil, fmt.Errorf("insufficient valid historical data: need at least 2 valid days, got %d", len(validData))
	}

	// КРИТИЧЕСКИ ВАЖНО: Nixtla требует хронологический порядок от старых дат к новым!
	// Используем эффективную сортировку через sort.Slice
	sortedData := make([]TimeSeriesData, len(validData))
	copy(sortedData, validData)
	
	// Сортируем по дате (DS) от старых к новым используя встроенную сортировку
	sort.Slice(sortedData, func(i, j int) bool {
		return sortedData[i].DS < sortedData[j].DS
	})
	
	// Убираем дубликаты дат (оставляем только первую запись для каждой даты)
	uniqueData := make([]TimeSeriesData, 0)
	seenDates := make(map[string]bool)
	for _, data := range sortedData {
		if !seenDates[data.DS] {
			uniqueData = append(uniqueData, data)
			seenDates[data.DS] = true
		} else {
			log.Printf("⚠️ Nixtla: пропущен дубликат даты %s", data.DS)
		}
	}
	
	if len(uniqueData) < 2 {
		return nil, fmt.Errorf("insufficient unique historical data: need at least 2 unique days, got %d", len(uniqueData))
	}
	
	log.Printf("📊 Nixtla: отсортировано и дедуплицировано: %d уникальных дней (было %d)", len(uniqueData), len(validData))
	
	// GAP FILLING: Заполняем пропущенные дни значением 0
	// Определяем диапазон дат от первой до последней
	if len(uniqueData) == 0 {
		return nil, fmt.Errorf("нет данных для прогнозирования")
	}
	
	firstDate, err := time.Parse("2006-01-02", uniqueData[0].DS)
	if err != nil {
		return nil, fmt.Errorf("ошибка парсинга первой даты: %w", err)
	}
	lastDate, err := time.Parse("2006-01-02", uniqueData[len(uniqueData)-1].DS)
	if err != nil {
		return nil, fmt.Errorf("ошибка парсинга последней даты: %w", err)
	}
	
	// Создаем map для быстрого доступа к данным по дате
	dataMap := make(map[string]TimeSeriesData)
	for _, data := range uniqueData {
		dataMap[data.DS] = data
	}
	
	// Собираем все уникальные ключи из внешних регрессоров
	exogenousKeys := make(map[string]bool)
	for _, data := range uniqueData {
		if data.X != nil {
			for key := range data.X {
				exogenousKeys[key] = true
			}
		}
	}
	// Добавляем ключи из будущих данных
	for _, futureData := range futureExogenous {
		if futureData != nil {
			for key := range futureData {
				exogenousKeys[key] = true
			}
		}
	}
	
	// Формируем непрерывный временной ряд с заполнением пропусков
	filledData := make([]TimeSeriesPoint, 0)
	currentDate := firstDate
	
	for !currentDate.After(lastDate) {
		dateStr := currentDate.Format("2006-01-02")
		
		// Создаем точку данных с базовыми полями
		yValue := 0.0 // По умолчанию 0 для пропущенных дней
		
		// Если есть данные для этой даты, используем их
		if data, exists := dataMap[dateStr]; exists {
			yValue = data.Y
		}
		
		// Создаем точку данных в правильном формате: {"ds": "YYYY-MM-DD", "y": float64}
		point := TimeSeriesPoint{
			DS: dateStr,
			Y:  yValue, // Убеждаемся, что это float64
		}
		
		// ВАЖНО: Внешние регрессоры (X) временно отключены, так как они требуют специального формата
		// Если нужно добавить внешние регрессоры, их нужно передавать отдельным массивом в корне запроса
		// Для простоты сейчас используем только ds и y
		
		filledData = append(filledData, point)
		currentDate = currentDate.AddDate(0, 0, 1) // Следующий день
	}
	
	log.Printf("📊 Nixtla: Gap Filling выполнен - заполнено %d дней (было %d уникальных дней, добавлено %d пропущенных)", 
		len(filledData), len(uniqueData), len(filledData)-len(uniqueData))
	
	// Валидация: проверяем, что даты идут в хронологическом порядке
	if len(filledData) > 1 {
		for i := 1; i < len(filledData); i++ {
			prevDS := filledData[i-1].DS
			currDS := filledData[i].DS
			if currDS <= prevDS {
				log.Printf("❌ Nixtla: КРИТИЧЕСКАЯ ОШИБКА - даты не в хронологическом порядке! %s >= %s", prevDS, currDS)
				return nil, fmt.Errorf("даты не отсортированы в хронологическом порядке")
			}
		}
		firstDS := filledData[0].DS
		lastDS := filledData[len(filledData)-1].DS
		log.Printf("✅ Nixtla: валидация дат пройдена - первая дата: %s, последняя: %s", firstDS, lastDS)
	}

	// Формируем запрос в правильном формате TimeGPT REST API
	// Формат: {"model": "timegpt-1", "freq": "D", "h": 20, "df": [{"ds": "...", "y": ...}, ...]}
	// Для длинных горизонтов (более 30 дней) автоматически используем "timegpt-1-long-horizon" для лучшей точности
	// Для TimeGPT-2 (требует специального доступа): используйте "timegpt-2.1", "timegpt-2-pro", "timegpt-2-lab", "timegpt-2-mini"
	modelName := "timegpt-1"
	if horizon > 30 {
		modelName = "timegpt-1-long-horizon"
		log.Printf("📊 Nixtla: используется модель long-horizon для горизонта %d дней", horizon)
	}
	
	req := &ForecastRequest{
		Model: modelName,             // Используем TimeGPT-1 или timegpt-1-long-horizon для длинных горизонтов
		Freq:  "D",                   // Дневная частота
		H:     horizon,               // Горизонт прогноза
		DF:    filledData,            // Массив точек данных в формате [{"ds": "...", "y": ...}]
		Level: []float64{80, 95},    // Доверительные интервалы (опционально)
	}
	
	hasExogenous := len(exogenousKeys) > 0
	if hasExogenous {
		log.Printf("🤖 Nixtla: отправка запроса в формате REST API (история: %d дней, горизонт: %d дней, внешние регрессоры: %d признаков)", 
			len(filledData), horizon, len(exogenousKeys))
	} else {
		log.Printf("🤖 Nixtla: отправка запроса в формате REST API (история: %d дней, горизонт: %d дней, без внешних регрессоров)", 
			len(filledData), horizon)
	}

	return nc.Forecast(req)
}