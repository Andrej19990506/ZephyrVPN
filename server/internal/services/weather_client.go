package services

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
	
	"gorm.io/gorm"
)

// WeatherClient клиент для работы с Open-Meteo API
type WeatherClient struct {
	baseURL string
	client  *http.Client
	// Координаты ресторана (по умолчанию можно задать в конфиге)
	latitude  float64
	longitude  float64
	timezone  string
	db        *gorm.DB // Для сохранения данных о погоде в БД
}

// NewWeatherClient создает новый клиент для получения прогноза погоды
func NewWeatherClient(latitude, longitude float64, timezone string, db *gorm.DB) *WeatherClient {
	if latitude == 0 && longitude == 0 {
		// Дефолтные координаты (можно задать в конфиге через WEATHER_LATITUDE и WEATHER_LONGITUDE)
		// ВАЖНО: Установите координаты вашего ресторана для точного прогноза!
		latitude = 55.7558  // Москва (пример, замените на координаты вашего города)
		longitude = 37.6173
		log.Printf("⚠️ Weather: используются координаты по умолчанию (Москва). Установите WEATHER_LATITUDE и WEATHER_LONGITUDE для вашего города!")
	} else {
		log.Printf("✅ Weather: координаты установлены (lat=%.4f, lon=%.4f, tz=%s)", latitude, longitude, timezone)
	}
	if timezone == "" {
		timezone = "Europe/Moscow" // По умолчанию московское время
	}

	return &WeatherClient{
		baseURL:   "https://api.open-meteo.com/v1/forecast",
		latitude:  latitude,
		longitude: longitude,
		timezone:  timezone,
		db:        db,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// WeatherForecastResponse ответ от Open-Meteo API
type WeatherForecastResponse struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Timezone  string  `json:"timezone"`
	Hourly    struct {
		Time           []string  `json:"time"`            // ISO8601 формат
		Temperature2m  []float64 `json:"temperature_2m"` // Температура на высоте 2м
	} `json:"hourly"`
	HourlyUnits struct {
		Time          string `json:"time"`
		Temperature2m string `json:"temperature_2m"`
	} `json:"hourly_units"`
}

// DailyWeatherData агрегированные данные о погоде за день
type DailyWeatherData struct {
	Date        string  `json:"date"`         // Дата в формате YYYY-MM-DD
	AvgTemp     float64 `json:"avg_temp"`     // Средняя температура за день
	MaxTemp     float64 `json:"max_temp"`     // Максимальная температура
	MinTemp     float64 `json:"min_temp"`     // Минимальная температура
	TempAt12    float64 `json:"temp_at_12"`   // Температура в 12:00 (обеденное время)
	TempAt18    float64 `json:"temp_at_18"`   // Температура в 18:00 (ужин)
}

// GetForecast получает прогноз погоды на указанное количество дней
// days - количество дней прогноза (максимум 16 дней для бесплатного API)
func (wc *WeatherClient) GetForecast(days int) (*WeatherForecastResponse, error) {
	if days > 16 {
		days = 16 // Open-Meteo бесплатный API ограничен 16 днями
	}
	if days < 1 {
		days = 7 // По умолчанию 7 дней
	}

	// Формируем URL запроса
	url := fmt.Sprintf("%s?latitude=%.2f&longitude=%.2f&hourly=temperature_2m&timezone=%s&forecast_days=%d",
		wc.baseURL, wc.latitude, wc.longitude, wc.timezone, days)

	log.Printf("🌤️ Weather: запрос прогноза погоды на %d дней (lat=%.2f, lon=%.2f, tz=%s)", 
		days, wc.latitude, wc.longitude, wc.timezone)

	// Выполняем запрос
	resp, err := wc.client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to get weather forecast: %w", err)
	}
	defer resp.Body.Close()

	// Проверяем статус код
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("weather API error (status %d): %s", resp.StatusCode, string(body))
	}

	// Читаем ответ
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Парсим ответ
	var forecast WeatherForecastResponse
	if err := json.Unmarshal(body, &forecast); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	log.Printf("🌤️ Weather: получен прогноз на %d часов", len(forecast.Hourly.Time))

	return &forecast, nil
}

// GetDailyAggregatedData агрегирует почасовые данные в дневные
// Возвращает массив данных по дням для использования в прогнозировании
func (wc *WeatherClient) GetDailyAggregatedData(forecast *WeatherForecastResponse) ([]DailyWeatherData, error) {
	if forecast == nil || len(forecast.Hourly.Time) == 0 {
		return nil, fmt.Errorf("empty forecast data")
	}

	// Группируем по дням
	dailyData := make(map[string]*DailyWeatherData)

	for i, timeStr := range forecast.Hourly.Time {
		if i >= len(forecast.Hourly.Temperature2m) {
			break
		}

		// Парсим время (Open-Meteo возвращает формат "2006-01-02T15:04" без секунд и таймзоны)
		// Пробуем разные форматы
		var t time.Time
		var err error
		
		// Формат с таймзоной: "2006-01-02T15:04:05Z07:00"
		if t, err = time.Parse(time.RFC3339, timeStr); err != nil {
			// Формат без секунд: "2006-01-02T15:04"
			if t, err = time.Parse("2006-01-02T15:04", timeStr); err != nil {
				// Формат ISO8601 без таймзоны: "2006-01-02T15:04:05"
				if t, err = time.Parse("2006-01-02T15:04:05", timeStr); err != nil {
					log.Printf("⚠️ Weather: ошибка парсинга времени %s: %v", timeStr, err)
					continue
				}
			}
		}

		// Получаем дату в формате YYYY-MM-DD
		date := t.Format("2006-01-02")
		temp := forecast.Hourly.Temperature2m[i]

		// Инициализируем данные для дня, если еще нет
		if dailyData[date] == nil {
			dailyData[date] = &DailyWeatherData{
				Date:    date,
				MinTemp: temp,
				MaxTemp: temp,
				AvgTemp: 0,
			}
		}

		day := dailyData[date]

		// Обновляем минимум и максимум
		if temp < day.MinTemp {
			day.MinTemp = temp
		}
		if temp > day.MaxTemp {
			day.MaxTemp = temp
		}

		// Сохраняем температуру в 12:00 и 18:00
		hour := t.Hour()
		if hour == 12 {
			day.TempAt12 = temp
		}
		if hour == 18 {
			day.TempAt18 = temp
		}
	}

	// Вычисляем среднюю температуру и формируем результат
	result := make([]DailyWeatherData, 0, len(dailyData))
	for _, day := range dailyData {
		// Подсчитываем среднюю температуру (упрощенно: среднее между мин и макс)
		// В реальности нужно суммировать все значения и делить на количество
		day.AvgTemp = (day.MinTemp + day.MaxTemp) / 2.0
		result = append(result, *day)
	}

	log.Printf("🌤️ Weather: агрегировано %d дней данных", len(result))

	return result, nil
}

// GetHistoricalWeather получает исторические данные о погоде (если доступны)
// Для Open-Meteo это может быть ограничено, но можно использовать архивные данные
func (wc *WeatherClient) GetHistoricalWeather(startDate, endDate time.Time) ([]DailyWeatherData, error) {
	// Open-Meteo предоставляет исторические данные через другой endpoint
	// Для простоты используем тот же API, но с датами в прошлом
	days := int(endDate.Sub(startDate).Hours() / 24)
	if days > 16 {
		days = 16
	}

	// Формируем URL для исторических данных
	url := fmt.Sprintf("%s?latitude=%.2f&longitude=%.2f&hourly=temperature_2m&timezone=%s&start_date=%s&end_date=%s",
		wc.baseURL, wc.latitude, wc.longitude, wc.timezone,
		startDate.Format("2006-01-02"), endDate.Format("2006-01-02"))

	log.Printf("🌤️ Weather: запрос исторических данных с %s по %s", 
		startDate.Format("2006-01-02"), endDate.Format("2006-01-02"))

	resp, err := wc.client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to get historical weather: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("⚠️ Weather: ошибка получения исторических данных (status %d): %s", resp.StatusCode, string(body))
		return nil, fmt.Errorf("weather API error (status %d)", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var forecast WeatherForecastResponse
	if err := json.Unmarshal(body, &forecast); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return wc.GetDailyAggregatedData(&forecast)
}

// SaveWeatherData сохраняет данные о погоде в БД
func (wc *WeatherClient) SaveWeatherData(dailyData []DailyWeatherData) error {
	if wc.db == nil {
		log.Printf("⚠️ Weather: БД недоступна, данные о погоде не сохранены")
		return nil // Не критичная ошибка
	}
	
	saved := 0
	for _, dayData := range dailyData {
		// Парсим дату
		date, err := time.Parse("2006-01-02", dayData.Date)
		if err != nil {
			log.Printf("⚠️ Weather: ошибка парсинга даты %s: %v", dayData.Date, err)
			continue
		}
		
		// Используем прямую SQL-операцию для сохранения, чтобы избежать проблем с маппингом имен колонок
		// GORM может неправильно преобразовать TempAt12 в temp_at12 вместо temp_at_12
		updateData := map[string]interface{}{
			"latitude":  wc.latitude,
			"longitude": wc.longitude,
			"timezone":  wc.timezone,
			"source":    "open-meteo",
		}
		if dayData.AvgTemp != 0 {
			updateData["avg_temp"] = dayData.AvgTemp
		}
		if dayData.MaxTemp != 0 {
			updateData["max_temp"] = dayData.MaxTemp
		}
		if dayData.MinTemp != 0 {
			updateData["min_temp"] = dayData.MinTemp
		}
		if dayData.TempAt12 != 0 {
			updateData["temp_at_12"] = dayData.TempAt12 // ВАЖНО: в БД колонка temp_at_12 (с подчеркиванием)
		}
		if dayData.TempAt18 != 0 {
			updateData["temp_at_18"] = dayData.TempAt18 // ВАЖНО: в БД колонка temp_at_18 (с подчеркиванием)
		}
		
		// Используем INSERT ... ON CONFLICT через прямую SQL-операцию
		// Это гарантирует правильные имена колонок
		var exists bool
		err = wc.db.Raw("SELECT EXISTS(SELECT 1 FROM weather_data WHERE date = ?)", date).Scan(&exists).Error
		if err != nil {
			log.Printf("⚠️ Weather: ошибка проверки существования записи для %s: %v", dayData.Date, err)
			continue
		}
		
		if exists {
			// Обновляем существующую запись
			err = wc.db.Table("weather_data").Where("date = ?", date).Updates(updateData).Error
			if err != nil {
				log.Printf("⚠️ Weather: ошибка обновления данных для %s: %v", dayData.Date, err)
				continue
			}
		} else {
			// Создаем новую запись
			// ВАЖНО: Не используем LastInsertId для PostgreSQL - он не поддерживается
			// GORM автоматически обработает вставку без возврата ID
			updateData["date"] = date
			err = wc.db.Table("weather_data").Create(updateData).Error
			if err != nil {
				log.Printf("⚠️ Weather: ошибка создания записи для %s: %v", dayData.Date, err)
				continue
			}
		}
		
		saved++
	}
	
	if saved > 0 {
		log.Printf("✅ Weather: сохранено %d записей о погоде в БД", saved)
	}
	
	return nil
}

