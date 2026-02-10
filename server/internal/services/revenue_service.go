package services

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"time"

	"gorm.io/gorm"
	"zephyrvpn/server/internal/models"
	"zephyrvpn/server/internal/utils"
)

// RevenueService управляет расчетом выручки из заказов
type RevenueService struct {
	redisUtil    *utils.RedisClient
	db           *gorm.DB // Доступ к PostgreSQL для чтения заказов
	nixtlaClient *NixtlaClient
	weatherClient *WeatherClient // Клиент для получения данных о погоде
	useNixtla    bool // Использовать ли Nixtla для прогнозирования
}

// NewRevenueService создает новый сервис выручки
func NewRevenueService(redisUtil *utils.RedisClient, db *gorm.DB) *RevenueService {
	return &RevenueService{
		redisUtil:  redisUtil,
		db:         db,
		useNixtla: false,
	}
}

// SetNixtlaClient устанавливает клиент Nixtla для использования AI-прогнозирования
func (rs *RevenueService) SetNixtlaClient(apiKey string) {
	if apiKey != "" {
		rs.nixtlaClient = NewNixtlaClient(apiKey)
		rs.useNixtla = true
		log.Printf("✅ Nixtla клиент инициализирован для AI-прогнозирования выручки")
	} else {
		rs.useNixtla = false
		log.Printf("⚠️ Nixtla API ключ не установлен, прогнозирование будет недоступно (линейная экстраполяция отключена)")
	}
}

// SetWeatherClient устанавливает клиент для получения данных о погоде
func (rs *RevenueService) SetWeatherClient(latitude, longitude float64, timezone string) {
	rs.weatherClient = NewWeatherClient(latitude, longitude, timezone, rs.db)
	log.Printf("✅ Weather клиент инициализирован (lat=%.2f, lon=%.2f, tz=%s)", latitude, longitude, timezone)
}

// RevenueStats содержит статистику выручки
type RevenueStats struct {
	Total           float64 `json:"total"`            // Общая выручка
	Cash            float64 `json:"cash"`              // Наличные
	Cashless        float64 `json:"cashless"`         // Безнал (карта)
	Online          float64 `json:"online"`           // Онлайн оплата
	Discounts       float64 `json:"discounts"`        // Сумма скидок
	CompletedOrders int     `json:"completed_orders"` // Количество завершенных заказов
	Change          float64 `json:"change"`          // Изменение в процентах (по сравнению с предыдущим днем)
}

// RevenueForecast содержит прогноз выручки
type RevenueForecast struct {
	ForecastTotal    float64 `json:"forecast_total"`     // Прогнозируемая выручка на конец дня
	CurrentRevenue   float64 `json:"current_revenue"`    // Текущая выручка
	RemainingHours   float64 `json:"remaining_hours"`    // Оставшиеся часы до закрытия
	AverageHourly    float64 `json:"average_hourly"`     // Средняя выручка в час (на основе истории)
	CurrentHourly    float64 `json:"current_hourly"`     // Текущая выручка в час (сегодня)
	HistoricalAvg    float64 `json:"historical_avg"`    // Средняя выручка за аналогичные дни недели
	Confidence       float64 `json:"confidence"`        // Уверенность в прогнозе (0-100%)
	Method           string  `json:"method"`             // Метод прогнозирования
}

// CalculateConfidenceScore рассчитывает оценку уверенности прогноза на основе временного горизонта
// Использует экспоненциальное затухание: высокая уверенность для коротких периодов, низкая для длинных
// days - количество дней в периоде прогноза
// Возвращает значение от 0 до 100
func CalculateConfidenceScore(days int) float64 {
	if days <= 0 {
		return 0
	}
	
	// Экспоненциальная модель затухания уверенности
	// Формула: confidence = 100 * e^(-days/decay_factor)
	// decay_factor определяет скорость падения уверенности
	// Для 7 дней: ~95%, для 30 дней: ~80%, для 90 дней: ~60%, для 180 дней: ~40%
	decayFactor := 45.0 // Подобрано эмпирически для получения нужных значений
	
	confidence := 100.0 * math.Exp(-float64(days)/decayFactor)
	
	// Ограничиваем минимальное значение до 20% (даже для очень длинных периодов)
	if confidence < 20 {
		confidence = 20
	}
	
	// Округляем до 1 знака после запятой
	return math.Round(confidence*10) / 10
}

// GetRevenueForDate получает выручку за указанную дату
// date - дата в формате "2006-01-02", если пустая - сегодня
func (rs *RevenueService) GetRevenueForDate(date string) (*RevenueStats, error) {
	if rs.redisUtil == nil {
		return nil, fmt.Errorf("Redis not available")
	}

	// Если дата не указана, используем сегодня
	if date == "" {
		date = time.Now().Format("2006-01-02")
	}

	stats := &RevenueStats{
		Total:           0,
		Cash:            0,
		Cashless:        0,
		Online:          0,
		Discounts:        0,
		CompletedOrders: 0,
		Change:          0,
	}

	// Парсим дату для фильтрации
	targetDate, err := time.Parse("2006-01-02", date)
	if err != nil {
		return nil, fmt.Errorf("invalid date format: %s", date)
	}
	
	// ВАЛИДАЦИЯ: Проверяем, что дата находится в допустимом диапазоне (последние 12 месяцев)
	now := time.Now()
	minDate := now.AddDate(0, -12, 0) // 12 месяцев назад
	maxDate := now.AddDate(0, 0, 1)   // Завтра (для учета сегодняшнего дня)
	
	if targetDate.Before(minDate) {
		log.Printf("⚠️ GetRevenueForDate: дата %s слишком старая (раньше %s), возвращаем пустую статистику", 
			date, minDate.Format("2006-01-02"))
		return stats, nil // Возвращаем пустую статистику вместо ошибки
	}
	
	if targetDate.After(maxDate) {
		log.Printf("⚠️ GetRevenueForDate: дата %s в будущем (позже %s), возвращаем пустую статистику", 
			date, maxDate.Format("2006-01-02"))
		return stats, nil // Возвращаем пустую статистику вместо ошибки
	}
	
	targetDateStart := time.Date(targetDate.Year(), targetDate.Month(), targetDate.Day(), 0, 0, 0, 0, time.UTC)
	targetDateEnd := targetDateStart.Add(24 * time.Hour)

	// ОПТИМИЗАЦИЯ: Сначала проверяем PostgreSQL (быстрее для исторических данных)
	// Только если данных нет в PostgreSQL, проверяем Redis
	if rs.db != nil {
		// Быстрая проверка наличия данных в PostgreSQL
		if rs.hasDataInPostgreSQL(targetDateStart, targetDateEnd) {
			pgStats := rs.getRevenueFromPostgreSQL(targetDateStart, targetDateEnd)
			if pgStats.CompletedOrders > 0 {
				log.Printf("📊 GetRevenueForDate: найдено %d заказов в PostgreSQL для даты %s", pgStats.CompletedOrders, date)
				stats = pgStats
				// Общая выручка уже рассчитана в getRevenueFromPostgreSQL
				stats.Total = stats.Cash + stats.Cashless + stats.Online
				
				// Рассчитываем изменение в процентах (по сравнению с предыдущим днем)
				prevDate := targetDateStart.AddDate(0, 0, -1)
				minValidDate := now.AddDate(0, -12, 0) // 12 месяцев назад
				
				if prevDate.After(minValidDate) || prevDate.Equal(minValidDate) {
					prevStats, _ := rs.GetRevenueForDate(prevDate.Format("2006-01-02"))
					if prevStats != nil && prevStats.Total > 0 {
						stats.Change = ((stats.Total - prevStats.Total) / prevStats.Total) * 100
					}
				}
				
				return stats, nil
			}
		}
	}

	// Если данных нет в PostgreSQL, проверяем Redis (только для сегодняшних/активных заказов)
	// ОПТИМИЗАЦИЯ: Для старых дат (больше 1 дня назад) не проверяем Redis
	if targetDate.Before(now.AddDate(0, 0, -1)) {
		// Для старых дат данных в Redis точно нет (они уже в PostgreSQL)
		log.Printf("📊 GetRevenueForDate: данных нет в PostgreSQL для даты %s (старая дата, Redis не проверяем)", date)
		return stats, nil
	}

	// Только для сегодня/вчера проверяем Redis
	maxArchiveOrders := 1000
	archiveKey := "erp:orders:archive"
	archiveLength, _ := rs.redisUtil.LLen(archiveKey)
	startIndex := int64(0)
	if archiveLength > int64(maxArchiveOrders) {
		startIndex = archiveLength - int64(maxArchiveOrders)
	}
	
	orderIDs, err := rs.redisUtil.LRange(archiveKey, startIndex, -1)
	if err != nil {
		log.Printf("⚠️ GetRevenueForDate: ошибка получения архива заказов: %v", err)
		return stats, nil
	}

	activeOrderIDs, _ := rs.redisUtil.SMembers("erp:orders:active")
	allOrderIDs := append(orderIDs, activeOrderIDs...)

	uniqueOrderIDs := make(map[string]bool)
	for _, id := range allOrderIDs {
		if id != "" {
			uniqueOrderIDs[id] = true
		}
	}

	log.Printf("📊 GetRevenueForDate: проверяем %d уникальных заказов из Redis для даты %s", len(uniqueOrderIDs), date)

	processedCount := 0
	maxProcessOrders := 2000
	for orderID := range uniqueOrderIDs {
		if processedCount >= maxProcessOrders {
			break
		}
		
		order, err := rs.getOrderFromRedis(orderID)
		if err != nil {
			continue
		}
		processedCount++

		if order.CreatedAt.Before(targetDateStart) || order.CreatedAt.After(targetDateEnd) || order.CreatedAt.Equal(targetDateEnd) {
			continue
		}

		status := order.Status
		if status != "delivered" && status != "ready" && status != "archived" {
			continue
		}

		orderPrice := float64(order.FinalPrice)
		if orderPrice == 0 {
			orderPrice = float64(order.TotalPrice)
		}

		paymentMethod := order.PaymentMethod
		switch paymentMethod {
		case "CASH", "cash":
			stats.Cash += orderPrice
		case "CARD", "CARD_ONLINE", "card", "card_online":
			stats.Cashless += orderPrice
		case "ONLINE", "online", "CRYPTO", "crypto":
			stats.Online += orderPrice
		default:
			stats.Cashless += orderPrice
		}

		if order.DiscountAmount > 0 {
			stats.Discounts += float64(order.DiscountAmount)
		}

		stats.CompletedOrders++
	}

	log.Printf("📊 GetRevenueForDate: обработано %d заказов из Redis, найдено %d завершенных заказов за %s", 
		processedCount, stats.CompletedOrders, date)

	// Общая выручка
	stats.Total = stats.Cash + stats.Cashless + stats.Online

	// Рассчитываем изменение в процентах (по сравнению с предыдущим днем)
	// ВАЛИДАЦИЯ: Проверяем, что предыдущий день не слишком старый (в пределах 12 месяцев)
	prevDate := targetDateStart.AddDate(0, 0, -1)
	minValidDate := now.AddDate(0, -12, 0) // 12 месяцев назад
	
	if prevDate.After(minValidDate) || prevDate.Equal(minValidDate) {
		prevStats, _ := rs.GetRevenueForDate(prevDate.Format("2006-01-02"))
		if prevStats != nil && prevStats.Total > 0 {
			stats.Change = ((stats.Total - prevStats.Total) / prevStats.Total) * 100
		}
	} else {
		log.Printf("⚠️ GetRevenueForDate: предыдущий день %s слишком старый (раньше %s), пропускаем расчет изменения", 
			prevDate.Format("2006-01-02"), minValidDate.Format("2006-01-02"))
	}

	return stats, nil
}

// GetRevenueForToday получает выручку за сегодня
func (rs *RevenueService) GetRevenueForToday() (*RevenueStats, error) {
	return rs.GetRevenueForDate("")
}

// getOrderFromRedis получает заказ из Redis
func (rs *RevenueService) getOrderFromRedis(orderID string) (*models.PizzaOrder, error) {
	// Пробуем получить из erp:order:{id}
	orderKey := fmt.Sprintf("erp:order:%s", orderID)
	orderJSON, err := rs.redisUtil.GetBytes(orderKey)
	if err == nil && len(orderJSON) > 0 {
		var order models.PizzaOrder
		if err := json.Unmarshal(orderJSON, &order); err == nil {
			return &order, nil
		}
	}

	// Пробуем получить из order:{id}
	orderKey2 := fmt.Sprintf("order:%s", orderID)
	orderJSON2, err := rs.redisUtil.GetBytes(orderKey2)
	if err == nil && len(orderJSON2) > 0 {
		var order models.PizzaOrder
		if err := json.Unmarshal(orderJSON2, &order); err == nil {
			return &order, nil
		}
	}

	return nil, fmt.Errorf("order not found: %s", orderID)
}

// hasDataInPostgreSQL быстро проверяет наличие данных в PostgreSQL за указанный период
func (rs *RevenueService) hasDataInPostgreSQL(startDate, endDate time.Time) bool {
	if rs.db == nil {
		return false
	}

	var count int64
	// Быстрая проверка наличия данных (использует индекс)
	query := `
		SELECT COUNT(*) 
		FROM orders
		WHERE created_at >= $1 
		  AND created_at < $2
		  AND status IN ('delivered', 'ready', 'archived')
		LIMIT 1
	`
	
	err := rs.db.Raw(query, startDate, endDate).Scan(&count).Error
	if err != nil {
		return false
	}
	
	return count > 0
}

// getDatesWithRevenueData получает список дат, где есть завершенные заказы
// Возвращает только те даты, где действительно есть данные (оптимизация)
func (rs *RevenueService) getDatesWithRevenueData(startDate, endDate time.Time) []time.Time {
	if rs.db == nil {
		return nil
	}

	// Один запрос для получения всех дат с данными
	query := `
		SELECT DISTINCT DATE(created_at) as order_date
		FROM orders
		WHERE created_at >= $1 
		  AND created_at < $2
		  AND status IN ('delivered', 'ready', 'archived')
		ORDER BY order_date DESC
	`

	rows, err := rs.db.Raw(query, startDate, endDate).Rows()
	if err != nil {
		log.Printf("⚠️ getDatesWithRevenueData: ошибка запроса: %v", err)
		return nil
	}
	defer func() {
		if err := rows.Close(); err != nil {
			log.Printf("⚠️ getDatesWithRevenueData: ошибка закрытия rows: %v", err)
		}
	}()

	dates := make([]time.Time, 0)
	for rows.Next() {
		var orderDate time.Time
		if err := rows.Scan(&orderDate); err != nil {
			log.Printf("⚠️ getDatesWithRevenueData: ошибка сканирования: %v", err)
			continue
		}
		dates = append(dates, orderDate)
	}

	return dates
}

// getRevenueFromPostgreSQL получает выручку из PostgreSQL за указанный период
func (rs *RevenueService) getRevenueFromPostgreSQL(startDate, endDate time.Time) *RevenueStats {
	if rs.db == nil {
		return &RevenueStats{}
	}

	stats := &RevenueStats{
		Total:           0,
		Cash:            0,
		Cashless:        0,
		Online:          0,
		Discounts:       0,
		CompletedOrders: 0,
		Change:          0,
	}

	// Быстрая проверка наличия данных перед полным запросом
	if !rs.hasDataInPostgreSQL(startDate, endDate) {
		return stats // Нет данных, возвращаем пустую статистику
	}

	// Запрос к PostgreSQL для получения завершенных заказов за указанный период
	query := `
		SELECT 
			payment_method,
			COALESCE(final_price, total_price - COALESCE(discount_amount, 0)) as final_price,
			COALESCE(discount_amount, 0) as discount_amount,
			status
		FROM orders
		WHERE created_at >= $1 
		  AND created_at < $2
		  AND status IN ('delivered', 'ready', 'archived')
	`

	rows, err := rs.db.Raw(query, startDate, endDate).Rows()
	if err != nil {
		log.Printf("⚠️ getRevenueFromPostgreSQL: ошибка запроса: %v", err)
		return stats
	}
	defer func() {
		if err := rows.Close(); err != nil {
			log.Printf("⚠️ getRevenueFromPostgreSQL: ошибка закрытия rows: %v", err)
		}
	}()

	for rows.Next() {
		var paymentMethod sql.NullString
		var finalPrice int
		var discountAmount int
		var status string

		err := rows.Scan(&paymentMethod, &finalPrice, &discountAmount, &status)
		if err != nil {
			log.Printf("⚠️ getRevenueFromPostgreSQL: ошибка сканирования: %v", err)
			continue
		}

		orderPrice := float64(finalPrice)

		// Разбиваем по типам оплаты
		pm := paymentMethod.String
		switch pm {
		case "CASH", "cash":
			stats.Cash += orderPrice
		case "CARD", "CARD_ONLINE", "card", "card_online":
			stats.Cashless += orderPrice
		case "ONLINE", "online", "CRYPTO", "crypto":
			stats.Online += orderPrice
		default:
			// По умолчанию считаем как безнал
			stats.Cashless += orderPrice
		}

		// Учитываем скидки
		if discountAmount > 0 {
			stats.Discounts += float64(discountAmount)
		}

		stats.CompletedOrders++
	}

	// Общая выручка
	stats.Total = stats.Cash + stats.Cashless + stats.Online

	return stats
}

// getWeatherFeaturesForDate получает данные о погоде для указанной даты
// Возвращает словарь признаков для использования в модели (температура, день недели и т.д.)
// ВАЖНО: Nixtla API требует формат словаря (map), а не массива
func (rs *RevenueService) getWeatherFeaturesForDate(date time.Time) map[string]float64 {
	weekday := float64(date.Weekday())
	
	// Значения по умолчанию (если данных о погоде нет)
	avgTemp := 0.0
	tempAt12 := 0.0
	tempAt18 := 0.0
	
	// Пытаемся получить данные о погоде из БД
	if rs.db != nil {
		var weatherData models.WeatherData
		dateStr := date.Format("2006-01-02")
		
		err := rs.db.Where("date = ?", dateStr).First(&weatherData).Error
		
		if err == nil {
			if weatherData.AvgTemp != nil {
				avgTemp = *weatherData.AvgTemp
			}
			if weatherData.TempAt12 != nil {
				tempAt12 = *weatherData.TempAt12
			} else if weatherData.AvgTemp != nil {
				tempAt12 = *weatherData.AvgTemp // Используем среднюю, если нет данных в 12:00
			}
			if weatherData.TempAt18 != nil {
				tempAt18 = *weatherData.TempAt18
			} else if weatherData.AvgTemp != nil {
				tempAt18 = *weatherData.AvgTemp // Используем среднюю, если нет данных в 18:00
			}
		} else {
			// Логируем только если это не просто отсутствие данных (gorm.ErrRecordNotFound)
			if err.Error() != "record not found" {
				log.Printf("🌤️ Weather: ошибка получения данных о погоде для %s: %v", dateStr, err)
			}
		}
	}
	
	// Возвращаем словарь с названиями признаков (Nixtla требует именно такой формат)
	return map[string]float64{
		"day_of_week": weekday,
		"temp_avg":    avgTemp,
		"temp_12":     tempAt12,
		"temp_18":     tempAt18,
	}
}

// getFutureWeatherData получает прогноз погоды для будущих периодов
// Возвращает массив словарей признаков для каждого дня прогноза (формат для Nixtla API)
func (rs *RevenueService) getFutureWeatherData(horizon int) []map[string]float64 {
	if rs.weatherClient == nil || horizon <= 0 {
		return nil
	}
	
	// Получаем прогноз погоды
	forecast, err := rs.weatherClient.GetForecast(horizon)
	if err != nil {
		log.Printf("⚠️ RevenueService: ошибка получения прогноза погоды: %v", err)
		return nil
	}
	
	// Агрегируем по дням
	dailyData, err := rs.weatherClient.GetDailyAggregatedData(forecast)
	if err != nil {
		log.Printf("⚠️ RevenueService: ошибка агрегации данных о погоде: %v", err)
		return nil
	}
	
	// Сохраняем данные о погоде в БД
	if err := rs.weatherClient.SaveWeatherData(dailyData); err != nil {
		log.Printf("⚠️ RevenueService: ошибка сохранения данных о погоде: %v", err)
		// Не критично, продолжаем работу
	}
	
	// Формируем массив словарей признаков для каждого дня (формат для Nixtla API)
	futureFeatures := make([]map[string]float64, 0, len(dailyData))
	now := time.Now()
	
	for i, dayData := range dailyData {
		if i >= horizon {
			break
		}
		
		// Парсим дату
		date, err := time.Parse("2006-01-02", dayData.Date)
		if err != nil {
			continue
		}
		
		// Пропускаем прошедшие дни
		if date.Before(now) {
			continue
		}
		
		// Формируем признаки в формате словаря (Nixtla требует именно такой формат)
		weekday := float64(date.Weekday())
		
		// Используем значения по умолчанию, если данные отсутствуют
		avgTemp := dayData.AvgTemp
		tempAt12 := dayData.TempAt12
		tempAt18 := dayData.TempAt18
		
		// Если температура в 12:00 или 18:00 не указана, используем среднюю
		if tempAt12 == 0 && avgTemp != 0 {
			tempAt12 = avgTemp
		}
		if tempAt18 == 0 && avgTemp != 0 {
			tempAt18 = avgTemp
		}
		
		// Словарь с названиями признаков (должны совпадать с историческими данными)
		features := map[string]float64{
			"day_of_week": weekday,
			"temp_avg":    avgTemp,
			"temp_12":     tempAt12,
			"temp_18":     tempAt18,
		}
		
		futureFeatures = append(futureFeatures, features)
	}
	
	log.Printf("🌤️ RevenueService: получен прогноз погоды для %d дней", len(futureFeatures))
	
	return futureFeatures
}

// GetRevenueForecast получает прогноз выручки на конец дня
// Использует комбинацию методов:
// 1. Nixtla AI (если доступен и есть достаточно исторических данных)
// 2. Линейная экстраполяция на основе текущего темпа
// 3. Средняя выручка за аналогичные дни недели (если есть история)
// 4. Взвешенное среднее для более точного прогноза
func (rs *RevenueService) GetRevenueForecast() (*RevenueForecast, error) {
	return rs.GetRevenueForecastForPeriod("", "")
}

// GetRevenueForecastForPeriod получает прогноз выручки для указанного периода
// startDate и endDate в формате "2006-01-02", если пустые - используется сегодня
func (rs *RevenueService) GetRevenueForecastForPeriod(startDate, endDate string) (*RevenueForecast, error) {
	if rs.redisUtil == nil {
		return nil, fmt.Errorf("Redis not available")
	}

	log.Printf("📊 GetRevenueForecastForPeriod: запуск прогнозирования (startDate=%s, endDate=%s, useNixtla=%v)", 
		startDate, endDate, rs.useNixtla)

	now := time.Now()
	today := now.Format("2006-01-02")
	
	// Определяем период прогноза
	var targetDate time.Time
	var horizon int = 1 // По умолчанию прогноз на 1 день
	
	if startDate != "" {
		parsedDate, err := time.Parse("2006-01-02", startDate)
		if err != nil {
			return nil, fmt.Errorf("invalid start_date format: %w", err)
		}
		targetDate = parsedDate
	} else {
		targetDate = now
	}
	
	if endDate != "" {
		parsedEndDate, err := time.Parse("2006-01-02", endDate)
		if err != nil {
			return nil, fmt.Errorf("invalid end_date format: %w", err)
		}
		horizon = int(parsedEndDate.Sub(targetDate).Hours() / 24) + 1
		if horizon < 1 {
			horizon = 1
		}
	}
	
	currentHour := now.Hour()
	// currentMinute не используется (линейная экстраполяция отключена)
	
	// Получаем текущую выручку за сегодня
	currentStats, err := rs.GetRevenueForDate(today)
	if err != nil {
		return nil, fmt.Errorf("failed to get current revenue: %w", err)
	}
	
	// Пробуем использовать Nixtla AI, если доступен и есть достаточно данных
	if rs.useNixtla && rs.nixtlaClient != nil {
		log.Printf("🤖 Nixtla: проверка возможности использования AI-прогнозирования")
		// Собираем исторические данные за последние 30-90 дней
		historicalData := make([]TimeSeriesData, 0)
		minValidDate := now.AddDate(0, -3, 0) // 3 месяца назад
		
		// ОПТИМИЗАЦИЯ: Получаем список дат с данными из PostgreSQL одним запросом
		// Это намного быстрее, чем проверять каждую дату отдельно
		earliestDate := now.AddDate(0, 0, -90)
		if earliestDate.Before(minValidDate) {
			earliestDate = minValidDate
		}
		
		// Получаем список дат, где есть завершенные заказы
		datesWithData := rs.getDatesWithRevenueData(earliestDate, now)
		
		if len(datesWithData) == 0 {
			log.Printf("📊 GetRevenueForecastForPeriod: нет исторических данных в PostgreSQL за период %s - %s", 
				earliestDate.Format("2006-01-02"), now.Format("2006-01-02"))
		} else {
			log.Printf("📊 GetRevenueForecastForPeriod: найдено %d дней с данными из %d возможных", 
				len(datesWithData), int(now.Sub(earliestDate).Hours()/24))
			
			// Обрабатываем только даты, где есть данные
			for _, dateWithData := range datesWithData {
				historicalDateStr := dateWithData.Format("2006-01-02")
				historicalStats, err := rs.GetRevenueForDate(historicalDateStr)
				if err == nil && historicalStats != nil && historicalStats.Total >= 0 {
					// ВРЕМЕННО ОТКЛЮЧЕНО: Получаем данные о погоде для этой даты (если доступны)
					// weatherFeatures := rs.getWeatherFeaturesForDate(dateWithData)
					
					// Nixtla API требует формат: {"ds": "YYYY-MM-DD", "y": value}
					// ВРЕМЕННО: не передаем внешние регрессоры (погоду)
					historicalData = append(historicalData, TimeSeriesData{
						DS: historicalDateStr, // Дата в формате YYYY-MM-DD
						Y:  historicalStats.Total, // Значение выручки
						X:  nil, // ВРЕМЕННО ОТКЛЮЧЕНО: погода не используется
					})
				}
			}
		}
		
		// Если есть достаточно исторических данных (минимум 14 дней), используем Nixtla
		if len(historicalData) >= 14 {
			// ВРЕМЕННО ОТКЛЮЧЕНО: Получаем прогноз погоды для будущих периодов
			// futureWeatherData := rs.getFutureWeatherData(horizon)
			var futureWeatherData []map[string]float64 = nil
			
			log.Printf("🤖 Nixtla: используем AI-прогнозирование (история: %d дней, горизонт: %d дней, погода: ОТКЛЮЧЕНА)", 
				len(historicalData), horizon)
			
			nixtlaForecast, err := rs.nixtlaClient.ForecastRevenue(historicalData, horizon, futureWeatherData)
			if err == nil && nixtlaForecast != nil && len(nixtlaForecast.Value) > 0 {
				// Используем прогноз от Nixtla
				// API возвращает timestamp и value на верхнем уровне
				forecastTotal := 0.0
				if len(nixtlaForecast.Value) > 0 {
					// Суммируем все прогнозы за горизонт
					for i, val := range nixtlaForecast.Value {
						forecastTotal += val
						if i < 3 || i >= len(nixtlaForecast.Value)-3 {
							// Логируем первые и последние 3 значения для отладки
							log.Printf("📊 Nixtla: прогноз день %d/%d: %.2f₽", i+1, len(nixtlaForecast.Value), val)
						}
					}
				}
				
				// ПРОБЛЕМА 2: Проверяем масштаб значений
				// Вычисляем среднее историческое значение для сравнения
				avgHistoricalValue := 0.0
				if len(historicalData) > 0 {
					sumHistorical := 0.0
					for _, data := range historicalData {
						sumHistorical += data.Y
					}
					avgHistoricalValue = sumHistorical / float64(len(historicalData))
				}
				
				avgForecastValue := forecastTotal / float64(len(nixtlaForecast.Value))
				
				// Если прогноз в 100+ раз меньше исторических данных, возможно API применил логарифмическую трансформацию
				if avgHistoricalValue > 0 && avgForecastValue > 0 && (avgHistoricalValue / avgForecastValue) > 100 {
					log.Printf("⚠️ Nixtla: КРИТИЧЕСКАЯ ПРОБЛЕМА! Среднее значение прогноза (%.2f) в %.0f раз меньше среднего исторического (%.2f). Возможно, API применил логарифмическую трансформацию.", 
						avgForecastValue, avgHistoricalValue/avgForecastValue, avgHistoricalValue)
					
					// Пробуем обратную логарифмическую трансформацию: exp(value)
					// Но сначала проверяем, не является ли значение уже в логарифмическом масштабе
					// Если среднее значение прогноза близко к log(среднее историческое), то применяем exp
					expectedLogValue := math.Log(avgHistoricalValue)
					if math.Abs(avgForecastValue - expectedLogValue) < 2.0 {
						log.Printf("🔧 Nixtla: применяем обратную логарифмическую трансформацию (exp)")
						forecastTotal = 0.0
						for i, val := range nixtlaForecast.Value {
							transformedVal := math.Exp(val)
							forecastTotal += transformedVal
							if i < 3 || i >= len(nixtlaForecast.Value)-3 {
								log.Printf("📊 Nixtla: прогноз день %d/%d (после exp): %.2f₽ (было: %.2f)", 
									i+1, len(nixtlaForecast.Value), transformedVal, val)
							}
						}
						avgForecastValue = forecastTotal / float64(len(nixtlaForecast.Value))
						log.Printf("💰 Nixtla: итоговый прогноз после трансформации: %.2f₽ (среднее в день: %.2f₽)", 
							forecastTotal, avgForecastValue)
					} else {
						log.Printf("❌ Nixtla: не удалось определить тип трансформации. Прогноз может быть некорректным.")
					}
				} else {
					log.Printf("💰 Nixtla: итоговый прогноз за %d дней: %.2f₽ (среднее в день: %.2f₽, среднее историческое: %.2f₽)", 
						len(nixtlaForecast.Value), forecastTotal, avgForecastValue, avgHistoricalValue)
				}
				
				// Рассчитываем confidence на основе горизонта
				confidence := CalculateConfidenceScore(horizon)
				
				forecast := &RevenueForecast{
					ForecastTotal:  forecastTotal,
					CurrentRevenue: currentStats.Total,
					RemainingHours: float64(horizon * 24), // Приблизительно
					AverageHourly:  forecastTotal / float64(horizon * 24),
					CurrentHourly:  currentStats.Total / float64(currentHour+1),
					HistoricalAvg:  historicalData[len(historicalData)-1].Y, // Последнее историческое значение
					Confidence:     confidence,
					Method:         "nixtla_ai",
				}
				
				log.Printf("🤖 Nixtla: прогноз успешно получен: %.2f₽ (уверенность: %.1f%%)", 
					forecastTotal, confidence)
				
				return forecast, nil
			} else {
				log.Printf("❌ Nixtla: ошибка прогнозирования (%v), линейная экстраполяция ОТКЛЮЧЕНА", err)
				return nil, fmt.Errorf("Nixtla AI прогнозирование недоступно: %v (линейная экстраполяция временно отключена)", err)
			}
		} else {
			log.Printf("❌ Nixtla: недостаточно исторических данных (%d дней, нужно минимум 14), линейная экстраполяция ОТКЛЮЧЕНА", 
				len(historicalData))
			return nil, fmt.Errorf("недостаточно исторических данных для Nixtla AI (%d дней, нужно минимум 14). Линейная экстраполяция временно отключена", len(historicalData))
		}
	}

	// ВРЕМЕННО ОТКЛЮЧЕНО: Если Nixtla не используется, возвращаем ошибку
	if !rs.useNixtla {
		log.Printf("❌ RevenueForecast: Nixtla AI недоступен, линейная экстраполяция ОТКЛЮЧЕНА")
		return nil, fmt.Errorf("Nixtla AI недоступен (NIXTLA_API_KEY не установлен). Линейная экстраполяция временно отключена")
	} else if rs.nixtlaClient == nil {
		log.Printf("❌ RevenueForecast: Nixtla клиент не инициализирован, линейная экстраполяция ОТКЛЮЧЕНА")
		return nil, fmt.Errorf("Nixtla клиент не инициализирован. Линейная экстраполяция временно отключена")
	}

	// ВРЕМЕННО ОТКЛЮЧЕНО: Весь блок линейной экстраполяции закомментирован
	/* ЛИНЕЙНАЯ ЭКСТРАПОЛЯЦИЯ ОТКЛЮЧЕНА
	forecast := &RevenueForecast{
		CurrentRevenue: currentStats.Total,
		RemainingHours: 0,
		AverageHourly:  0,
		CurrentHourly:  0,
		HistoricalAvg:  0,
		Confidence:     50, // Базовая уверенность
		Method:         "linear_extrapolation",
	}

	// Определяем оставшиеся часы до закрытия (предполагаем закрытие в 23:00)
	closeHour := 23
	closeMinute := 0
	
	// Если уже после закрытия, прогноз = текущая выручка
	if currentHour >= closeHour {
		forecast.ForecastTotal = currentStats.Total
		forecast.RemainingHours = 0
		forecast.Confidence = 100
		return forecast, nil
	}

	// Рассчитываем оставшиеся часы
	remainingMinutes := (closeHour-currentHour)*60 - currentMinute + closeMinute
	forecast.RemainingHours = float64(remainingMinutes) / 60.0

	// Метод 1: Линейная экстраполяция на основе текущего темпа
	// Сколько часов прошло с открытия (предполагаем открытие в 9:00)
	openHour := 9
	openMinute := 0
	
	elapsedMinutes := (currentHour-openHour)*60 + currentMinute - openMinute
	elapsedHours := float64(elapsedMinutes) / 60.0
	
	var linearForecast float64
	if elapsedHours > 0 {
		forecast.CurrentHourly = currentStats.Total / elapsedHours
		linearForecast = currentStats.Total + (forecast.CurrentHourly * forecast.RemainingHours)
		forecast.ForecastTotal = linearForecast
		forecast.Method = "linear_extrapolation"
		forecast.Confidence = 60 // Средняя уверенность для линейной экстраполяции
	} else {
		// Если день только начался, используем исторические данные
		forecast.CurrentHourly = 0
		linearForecast = currentStats.Total
		forecast.ForecastTotal = currentStats.Total
		forecast.Confidence = 30
	}

	// Метод 2: Исторические данные за аналогичные дни недели
	// Получаем среднюю выручку за последние 4 недели в тот же день недели
	// ОГРАНИЧЕНИЕ: Берем только данные за последние 12 месяцев
	weekday := now.Weekday()
	historicalRevenues := make([]float64, 0)
	minValidDate := now.AddDate(0, -12, 0) // 12 месяцев назад
	
	for weeksAgo := 1; weeksAgo <= 52; weeksAgo++ { // Максимум 52 недели (1 год)
		historicalDate := now.AddDate(0, 0, -7*weeksAgo)
		
		// ВАЛИДАЦИЯ: Проверяем, что дата не слишком старая
		if historicalDate.Before(minValidDate) {
			break // Прекращаем, если дата слишком старая
		}
		
		// Проверяем, что это тот же день недели
		if historicalDate.Weekday() == weekday {
			historicalDateStr := historicalDate.Format("2006-01-02")
			
			historicalStats, err := rs.GetRevenueForDate(historicalDateStr)
			if err == nil && historicalStats.Total > 0 {
				historicalRevenues = append(historicalRevenues, historicalStats.Total)
				
				// Ограничиваем количество исторических данных (максимум 8 недель)
				if len(historicalRevenues) >= 8 {
					break
				}
			}
		}
	}

	if len(historicalRevenues) > 0 {
		// Считаем среднюю выручку за аналогичные дни
		sum := 0.0
		for _, rev := range historicalRevenues {
			sum += rev
		}
		forecast.HistoricalAvg = sum / float64(len(historicalRevenues))
		
		// Рассчитываем среднюю выручку в час на основе истории
		// Предполагаем рабочий день 14 часов (9:00 - 23:00)
		forecast.AverageHourly = forecast.HistoricalAvg / 14.0
		
		// Комбинированный прогноз: взвешенное среднее
		// 40% - текущий темп, 60% - историческая средняя
		historicalForecast := currentStats.Total + (forecast.AverageHourly * forecast.RemainingHours)
		
		// Взвешенное среднее
		forecast.ForecastTotal = (linearForecast * 0.4) + (historicalForecast * 0.6)
		forecast.Method = "weighted_average"
		forecast.Confidence = 75 // Выше уверенность при наличии истории
		
		log.Printf("📊 RevenueForecast [%s]: текущая=%.2f₽, линейный=%.2f₽, исторический=%.2f₽, итоговый=%.2f₽, уверенность=%.0f%%",
			forecast.Method, currentStats.Total, linearForecast, historicalForecast, forecast.ForecastTotal, forecast.Confidence)
	} else {
		log.Printf("📊 RevenueForecast [%s]: нет исторических данных, используем линейную экстраполяцию: %.2f₽",
			forecast.Method, forecast.ForecastTotal)
	}

	// Учитываем время суток для корректировки прогноза
	// Обеденное время (12:00-14:00) и ужин (18:00-21:00) обычно более активные
	timeMultiplier := 1.0
	if currentHour >= 12 && currentHour < 14 {
		timeMultiplier = 1.2 // Обеденное время - на 20% выше
	} else if currentHour >= 18 && currentHour < 21 {
		timeMultiplier = 1.3 // Ужин - на 30% выше
	} else if currentHour >= 21 {
		timeMultiplier = 0.7 // Поздний вечер - на 30% ниже
	}

	// Применяем корректировку только если есть оставшееся время
	if forecast.RemainingHours > 0 {
		// Корректируем прогноз с учетом времени суток
		baseForecast := forecast.ForecastTotal
		timeAdjustedForecast := currentStats.Total + ((forecast.ForecastTotal - currentStats.Total) * timeMultiplier)
		forecast.ForecastTotal = timeAdjustedForecast
		
		if timeMultiplier != 1.0 {
			log.Printf("📊 RevenueForecast: корректировка по времени суток (множитель=%.2f): %.2f₽ -> %.2f₽",
				timeMultiplier, baseForecast, forecast.ForecastTotal)
		}
	}

	// Ограничиваем прогноз разумными пределами
	// Минимум: текущая выручка (не может быть меньше)
	// Максимум: текущая выручка * 3 (не может быть больше чем в 3 раза от текущей)
	if forecast.ForecastTotal < currentStats.Total {
		forecast.ForecastTotal = currentStats.Total
	}
	maxForecast := currentStats.Total * 3.0
	if forecast.ForecastTotal > maxForecast {
		forecast.ForecastTotal = maxForecast
		forecast.Confidence = 40 // Снижаем уверенность при экстремальных значениях
	}

	return forecast, nil
	*/
	
	// Этот код не должен выполняться, но на всякий случай
	return nil, fmt.Errorf("неизвестная ошибка: прогнозирование не выполнено")
}


