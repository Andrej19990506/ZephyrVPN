package services

import (
	"fmt"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
	"zephyrvpn/server/internal/utils"
)

// SlotService управляет временными слотами для Capacity-Based Slot Scheduling
type SlotService struct {
	redisUtil *utils.RedisClient
	client    *redis.Client // Прямой доступ к Redis клиенту для Lua scripts
	slotDuration time.Duration // Длительность слота (по умолчанию 15 минут)
	maxCapacityPerSlot int     // Максимальная емкость слота в РУБЛЯХ (не количество заказов!)
	
	// Бизнес-часы пиццерии (в UTC, клиент сам конвертирует в свой часовой пояс)
	openHour  int // Час открытия в UTC
	closeHour int // Час закрытия в UTC
	closeMin  int // Минута закрытия в UTC
}

// SlotInfo информация о слоте
type SlotInfo struct {
	SlotID      string    `json:"slot_id"`
	StartTime   time.Time `json:"start_time"`   // RFC3339 формат
	EndTime     time.Time `json:"end_time"`     // RFC3339 формат
	CurrentLoad int       `json:"current_load"` // Текущая сумма в рублях
	MaxCapacity int       `json:"max_capacity"` // Максимальная сумма в рублях
}

// NewSlotService создает новый сервис слотов
// ВАЖНО: Все временные операции выполняются в UTC
// Конвертация в локальное время происходит на клиенте (фронтенде)
// Бизнес-часы задаются в UTC через переменные окружения
func NewSlotService(redisUtil *utils.RedisClient, openHour, closeHour, closeMin int) *SlotService {
	ss := &SlotService{
		redisUtil:         redisUtil,
		slotDuration:      15 * time.Minute, // 15 минут по умолчанию
		maxCapacityPerSlot: 10000,           // 10000 рублей на слот по умолчанию (устанавливается через ERP API UpdateSlotConfig)
		openHour:          openHour,         // Открытие в UTC
		closeHour:         closeHour,        // Закрытие в UTC
		closeMin:          closeMin,         // Минута закрытия в UTC
	}
	
	log.Printf("✅ SlotService инициализирован: рабочие часы %02d:00 - %02d:%02d UTC (клиент конвертирует в свой часовой пояс)", 
		openHour, closeHour, closeMin)
	
	// Получаем прямой доступ к redis.Client для Lua scripts
	if redisUtil != nil {
		ss.client = redisUtil.GetClient()
		// Загружаем сохраненное значение maxCapacity из Redis
		if ss.client != nil {
			ctx := redisUtil.Context()
			savedCapacity, err := ss.client.Get(ctx, "slot:config:max_capacity").Int()
			if err == nil && savedCapacity > 0 {
				ss.maxCapacityPerSlot = savedCapacity
				log.Printf("✅ Загружено сохраненное значение maxCapacity из Redis: %d₽", savedCapacity)
			}
		}
	}
	
	return ss
}

// SetSlotDuration устанавливает длительность слота
func (ss *SlotService) SetSlotDuration(duration time.Duration) {
	ss.slotDuration = duration
}

// SetMaxCapacity устанавливает максимальную емкость слота в РУБЛЯХ
func (ss *SlotService) SetMaxCapacity(capacity int) {
	oldCapacity := ss.maxCapacityPerSlot
	ss.maxCapacityPerSlot = capacity
	// Сохраняем в Redis для персистентности
	if ss.client != nil && ss.redisUtil != nil {
		ctx := ss.redisUtil.Context()
		if err := ss.client.Set(ctx, "slot:config:max_capacity", capacity, 0).Err(); err != nil {
			log.Printf("⚠️ Ошибка сохранения maxCapacity в Redis: %v", err)
		} else {
			log.Printf("✅ maxCapacity обновлен: %d₽ -> %d₽ (сохранено в Redis)", oldCapacity, capacity)
		}
	} else {
		log.Printf("✅ maxCapacity обновлен в памяти: %d₽ -> %d₽ (Redis недоступен)", oldCapacity, capacity)
	}
}

// isWithinWorkingHours проверяет, находится ли время в рабочих часах пиццерии
// ВАЖНО: время должно быть в UTC, рабочие часы тоже заданы в UTC
// ВРЕМЕННО ОТКЛЮЧЕНО ДЛЯ ТЕСТА: всегда возвращает true (круглосуточная работа)
func (ss *SlotService) isWithinWorkingHours(t time.Time) bool {
	// ВРЕМЕННО: для теста всегда возвращаем true (круглосуточная работа)
	return true
	
	// Работаем напрямую с UTC, без конвертации
	// Клиент сам конвертирует время в свой часовой пояс
	// utcTime := t.UTC()
	// 
	// hour := utcTime.Hour()
	// min := utcTime.Minute()
	// 
	// // Если час меньше открытия или больше закрытия
	// if hour < ss.openHour || hour > ss.closeHour {
	// 	return false
	// }
	// 
	// // Если последний час (closeHour), проверяем минуты (до closeMin включительно)
	// if hour == ss.closeHour && min > ss.closeMin {
	// 	return false
	// }
	// 
	// return true
}

// GenerateSlotID генерирует ID слота на основе времени начала (публичный метод)
func (ss *SlotService) GenerateSlotID(startTime time.Time) string {
	return ss.generateSlotID(startTime)
}

// generateSlotID генерирует ID слота на основе времени начала
// ВАЖНО: ID не должен зависеть от формата времени или часового пояса
// Используем Unix timestamp для уникальности и независимости от формата
func (ss *SlotService) generateSlotID(startTime time.Time) string {
	// Используем Unix timestamp (секунды с 1970-01-01 UTC)
	// Это простое число, не зависящее от часового пояса или формата даты
	return fmt.Sprintf("slot:%d", startTime.UTC().Unix())
}

// GetSlotStartTime вычисляет время начала ближайшего доступного слота (публичный метод)
func (ss *SlotService) GetSlotStartTime(now time.Time) time.Time {
	return ss.getSlotStartTime(now)
}

// getSlotStartTime вычисляет время начала ближайшего доступного слота
// ВАЖНО: всегда возвращает БУДУЩИЙ слот (который еще не начался)
// Все времена в UTC - конвертация в локальное время на клиенте
func (ss *SlotService) getSlotStartTime(now time.Time) time.Time {
	// Используем UTC для всех операций
	nowUTC := now.UTC()
	
	// Округляем до ближайшего слота (15 минут)
	minutes := nowUTC.Minute()
	slotMinutes := (minutes / int(ss.slotDuration.Minutes())) * int(ss.slotDuration.Minutes())
	
	// Создаем время начала слота в UTC
	slotStart := time.Date(nowUTC.Year(), nowUTC.Month(), nowUTC.Day(), 
		nowUTC.Hour(), slotMinutes, 0, 0, time.UTC)
	
	// ВСЕГДА берем следующий слот (который еще не начался)
	// Если текущее время равно началу слота или уже прошло, берем следующий
	if !nowUTC.Before(slotStart) {
		slotStart = slotStart.Add(ss.slotDuration)
	}
	
	return slotStart
}

// AssignSlot атомарно бронирует место в слоте через Redis
// orderPrice - сумма заказа в рублях (используется для расчета загрузки слота)
// itemsCount - количество элементов в заказе (для расчета времени подготовки)
// Возвращает ID слота, время начала слота, время показа заказа и ошибку
func (ss *SlotService) AssignSlot(orderID string, orderPrice int, itemsCount int) (string, time.Time, time.Time, error) {
	if ss.redisUtil == nil {
		return "", time.Time{}, time.Time{}, fmt.Errorf("Redis client not initialized")
	}

	ctx := ss.redisUtil.Context()
	
	// ВАЖНО: Загружаем актуальное значение maxCapacity из Redis перед каждым использованием
	// Это гарантирует, что мы используем последнее установленное значение
	if ss.client != nil {
		savedCapacity, err := ss.client.Get(ctx, "slot:config:max_capacity").Int()
		if err == nil && savedCapacity > 0 {
			if savedCapacity != ss.maxCapacityPerSlot {
				log.Printf("🔄 AssignSlot: обновлено maxCapacity из Redis: %d₽ -> %d₽", 
					ss.maxCapacityPerSlot, savedCapacity)
				ss.maxCapacityPerSlot = savedCapacity
			}
		}
	}
	
	// Используем UTC для всех временных операций
	now := time.Now().UTC()
	
	// Начинаем с ближайшего будущего слота
	slotStart := ss.getSlotStartTime(now)
	
	// ПРОВЕРКА БЛИЖНЯКА:
	// Если до конца текущего слота осталось меньше 8 минут,
	// повар физически не успеет. Перелетаем сразу на следующий.
	currentSlotEnd := slotStart.Add(ss.slotDuration)
	timeUntilSlotEnd := currentSlotEnd.Sub(now)
	
	if timeUntilSlotEnd < 8*time.Minute {
		log.Printf("⚠️ AssignSlot: до конца текущего слота осталось %v (< 8 минут), перелетаем на следующий слот", timeUntilSlotEnd)
		slotStart = slotStart.Add(ss.slotDuration)
	}
	
	// Пытаемся найти свободный слот, начиная с ближайшего
	maxAttempts := 100 // Страховка от бесконечного цикла
	failedAttempts := 0 // Счетчик неудачных попыток для логирования
	
	for attempt := 0; attempt < maxAttempts; attempt++ {
		// ВРЕМЕННО ОТКЛЮЧЕНО ДЛЯ ТЕСТА: убраны проверки рабочего времени (круглосуточная работа)
		// Проверяем, что слот все еще в текущем дне
		// if slotStart.Day() != now.Day() || slotStart.Month() != now.Month() || slotStart.Year() != now.Year() {
		// 	return "", time.Time{}, time.Time{}, fmt.Errorf("кухня закрыта, заказы на сегодня не принимаются (рабочее время: %02d:00 - %02d:%02d UTC)", 
		// 		ss.openHour, ss.closeHour, ss.closeMin)
		// }
		// 
		// // Проверяем, что слот не превышает конец рабочего дня
		// if !slotStart.Before(endOfDay) {
		// 	return "", time.Time{}, time.Time{}, fmt.Errorf("кухня закрыта, заказы на сегодня не принимаются (рабочее время: %02d:00 - %02d:%02d UTC)", 
		// 		ss.openHour, ss.closeHour, ss.closeMin)
		// }
		// 
		// // Проверяем, что слот находится в рабочих часах пиццерии
		// if !ss.isWithinWorkingHours(slotStart) {
		// 	// Если дошли до закрытия, прекращаем поиск
		// 	return "", time.Time{}, time.Time{}, fmt.Errorf("кухня закрыта, заказы на сегодня не принимаются (рабочее время: %02d:00 - %02d:%02d UTC)", 
		// 		ss.openHour, ss.closeHour, ss.closeMin)
		// }
		
		slotID := ss.generateSlotID(slotStart)
		
		// Используем Redis Lua script для атомарной операции
		// Это гарантирует, что только один заказ сможет занять последнее место
		// Считаем по СУММЕ заказов, а не по количеству!
		luaScript := `
			local slot_key = KEYS[1]
			local order_key = KEYS[2]
			local max_capacity = tonumber(ARGV[1])  -- Максимальная сумма в рублях
			local slot_id = ARGV[2]
			local order_id = ARGV[3]
			local order_price = tonumber(ARGV[4])  -- Сумма текущего заказа
			local slot_start = ARGV[5]
			local slot_end = ARGV[6]
			
			-- Получаем текущую загрузку слота (сумма в рублях)
			local current_load = redis.call('GET', slot_key)
			if current_load == false then
				current_load = 0
			else
				current_load = tonumber(current_load)
			end
			
			-- Проверяем, есть ли место (по сумме, а не по количеству!)
			if current_load + order_price > max_capacity then
				return {0, current_load} -- Слот переполнен (не хватает места по сумме)
			end
			
			-- Атомарно увеличиваем сумму слота на сумму заказа
			-- КРИТИЧНО: TTL увеличен до 2 часов (7200 сек) для сохранения истории прошедших слотов
			redis.call('INCRBY', slot_key, order_price)
			redis.call('EXPIRE', slot_key, 7200) -- TTL 2 часа для истории
			
			-- Сохраняем информацию о слоте (если еще не сохранена)
			local slot_info_key = slot_key .. ':info'
			if redis.call('EXISTS', slot_info_key) == 0 then
				redis.call('HSET', slot_info_key, 
					'start_time', slot_start,
					'end_time', slot_end,
					'max_capacity', max_capacity)
				redis.call('EXPIRE', slot_info_key, 7200) -- TTL 2 часа для истории
			end
			
			-- Сохраняем связь заказ -> слот и сумму заказа
			-- КРИТИЧНО: TTL увеличен до 2 часов для сохранения истории
			redis.call('HSET', order_key, 'slot_id', slot_id, 'price', order_price)
			redis.call('EXPIRE', order_key, 7200) -- TTL 2 часа для истории
			
			-- Добавляем заказ в список заказов слота
			redis.call('SADD', slot_key .. ':orders', order_id)
			redis.call('EXPIRE', slot_key .. ':orders', 7200) -- TTL 2 часа для истории
			
			return {1, current_load + order_price} -- Успех, возвращаем новую сумму
		`
		
		slotKey := fmt.Sprintf("slot:%s", slotID)
		orderSlotKey := fmt.Sprintf("order:slot:%s", orderID)
		slotEnd := slotStart.Add(ss.slotDuration)
		
		if ss.client == nil {
			return "", time.Time{}, time.Time{}, fmt.Errorf("Redis client not available for Lua scripts")
		}
		
		result, err := ss.client.Eval(ctx, luaScript, []string{
			slotKey,
			orderSlotKey,
		}, []interface{}{
			ss.maxCapacityPerSlot,        // Максимальная сумма в рублях
			slotID,
			orderID,
			orderPrice,                   // Сумма заказа в рублях
			slotStart.Format(time.RFC3339),
			slotEnd.Format(time.RFC3339),
		}).Result()
		
		if err != nil {
			// Логируем только каждую 10-ю ошибку, чтобы не засорять логи
			if failedAttempts%10 == 0 {
				log.Printf("⚠️ SlotService: ошибка при бронировании слота %s: %v (попытка #%d)", slotID, err, attempt+1)
			}
			failedAttempts++
			continue // Пробуем следующий слот
		}
		
		// Результат: [success (1 или 0), current_load]
		resultArray, ok := result.([]interface{})
		if !ok || len(resultArray) < 2 {
			// Логируем только каждую 10-ю ошибку
			if failedAttempts%10 == 0 {
				log.Printf("⚠️ SlotService: неожиданный результат от Lua script: %v (попытка #%d)", result, attempt+1)
			}
			failedAttempts++
			continue
		}
		
		success, _ := resultArray[0].(int64)
		currentLoad, _ := resultArray[1].(int64)
		if success == 1 {
			// РАСЧЕТ VISIBLE_AT:
			// Заказ должен появиться на планшете за 30 минут до начала слота.
			// НО: если это "ближняк" (до начала слота < 30 минут), показываем с начала слота.
			prepTimeBeforeSlot := 30 * time.Minute
			timeUntilSlotStart := slotStart.Sub(now)
			
			var visibleAt time.Time
			if timeUntilSlotStart >= prepTimeBeforeSlot {
				// Достаточно времени - показываем за 30 минут до начала слота
				visibleAt = slotStart.Add(-prepTimeBeforeSlot)
			} else {
				// Ближняк - показываем с начала слота
				visibleAt = slotStart
			}
			
			// Успешно забронировали место! Логируем только успешные назначения
			if attempt > 0 {
				log.Printf("✅ AssignSlot: заказ %s (сумма: %d₽) назначен на слот %s после %d попыток (загрузка: %d₽/%d₽)", 
					orderID, orderPrice, slotID, attempt+1, currentLoad, ss.maxCapacityPerSlot)
			}
			return slotID, slotStart, visibleAt, nil
		}
		
		// Слот переполнен, пробуем следующий (НЕ логируем каждую попытку - слишком много логов!)
		failedAttempts++
		
		// Переходим к следующему слоту (просто добавляем 15 минут)
		slotStart = slotStart.Add(ss.slotDuration)
	}

	// Все слоты переполнены (маловероятно, но возможно)
	return "", time.Time{}, time.Time{}, fmt.Errorf("все слоты переполнены, попробуйте позже")
}

// GetSlotInfo получает информацию о слоте (базовая версия, использует только Redis counter)
func (ss *SlotService) GetSlotInfo(slotID string) (*SlotInfo, error) {
	if ss.redisUtil == nil {
		return nil, fmt.Errorf("Redis client not initialized")
	}

	ctx := ss.redisUtil.Context()
	slotKey := fmt.Sprintf("slot:%s", slotID)
	
	if ss.client == nil {
		return nil, fmt.Errorf("Redis client not available")
	}
	
	// Получаем текущую загрузку из Slot Counter (сумма в рублях)
	currentLoad, err := ss.client.Get(ctx, slotKey).Int64()
	if err == redis.Nil {
		return &SlotInfo{
			SlotID:      slotID,
			CurrentLoad: 0,
			MaxCapacity: ss.maxCapacityPerSlot,
		}, nil
	}
	if err != nil {
		return nil, err
	}
	
	// Получаем информацию о слоте
	slotInfoKey := slotKey + ":info"
	info, err := ss.client.HGetAll(ctx, slotInfoKey).Result()
	if err != nil {
		return nil, err
	}
	
	var startTime, endTime time.Time
	if startStr, ok := info["start_time"]; ok {
		startTime, _ = time.Parse(time.RFC3339, startStr)
	}
	if endStr, ok := info["end_time"]; ok {
		endTime, _ = time.Parse(time.RFC3339, endStr)
	}
	
	return &SlotInfo{
		SlotID:      slotID,
		StartTime:   startTime,
		EndTime:     endTime,
		CurrentLoad:  int(currentLoad),
		MaxCapacity: ss.maxCapacityPerSlot,
	}, nil
}

// GetSlotInfoWithOrders получает информацию о слоте с учетом pending + active заказов
// КРИТИЧНО: Считает загрузку как сумму всех заказов (pending + active), назначенных на этот слот
// 
// Логика расчета:
// 1. Базовая загрузка берется из Slot Counter (Redis key: slot:{id})
//    - Slot Counter обновляется через AssignSlot() при создании заказа
//    - Slot Counter обновляется через ReleaseSlot() при отмене заказа
//    - Slot Counter обновляется через Kafka события (Created -> Cooking -> Done/Cancelled)
// 2. Дополнительно проверяются pending и active заказы из Redis sets:
//    - erp:orders:pending_slots - заказы, ожидающие активации
//    - erp:orders:active - активные заказы на KDS
// 3. Итоговая загрузка = базовая загрузка (если > 0) или сумма pending + active заказов
//
// ВАЖНО: Slot Counter сохраняется в Redis с TTL 2 часа для истории прошедших слотов
func (ss *SlotService) GetSlotInfoWithOrders(slotID string, slotStart, slotEnd time.Time) (*SlotInfo, error) {
	if ss.redisUtil == nil || ss.client == nil {
		return nil, fmt.Errorf("Redis client not initialized")
	}

	ctx := ss.redisUtil.Context()
	slotKey := fmt.Sprintf("slot:%s", slotID)
	
	// 1. Получаем базовую загрузку из Slot Counter (это сумма всех заказов, когда-либо назначенных на слот)
	baseLoad, err := ss.client.Get(ctx, slotKey).Int64()
	if err == redis.Nil {
		baseLoad = 0
	} else if err != nil {
		return nil, err
	}
	
	// 2. Дополнительно проверяем pending и active заказы для этого слота
	// Это важно, если заказы перешли в active, но Slot Counter еще не обновлен
	pendingLoad := int64(0)
	activeLoad := int64(0)
	
	// Получаем список заказов слота из Redis
	slotOrdersKey := slotKey + ":orders"
	orderIDs, err := ss.client.SMembers(ctx, slotOrdersKey).Result()
	if err == nil && len(orderIDs) > 0 {
		// Проверяем каждый заказ: pending или active?
		for _, orderID := range orderIDs {
			// Проверяем, находится ли заказ в pending_slots
			isPending, _ := ss.redisUtil.SIsMember("erp:orders:pending_slots", orderID)
			// Проверяем, находится ли заказ в active
			isActive, _ := ss.redisUtil.SIsMember("erp:orders:active", orderID)
			
			// Получаем сумму заказа
			orderSlotKey := fmt.Sprintf("order:slot:%s", orderID)
			orderInfo, err := ss.client.HGetAll(ctx, orderSlotKey).Result()
			if err == nil {
				if priceStr, ok := orderInfo["price"]; ok {
					var orderPrice int64
					fmt.Sscanf(priceStr, "%d", &orderPrice)
					
					if isPending {
						pendingLoad += orderPrice
					} else if isActive {
						activeLoad += orderPrice
					}
				}
			}
		}
	}
	
	// 3. Итоговая загрузка = базовая загрузка из Slot Counter
	// Если базовая загрузка = 0, но есть pending/active заказы - используем их сумму
	totalLoad := baseLoad
	if totalLoad == 0 && (pendingLoad > 0 || activeLoad > 0) {
		totalLoad = pendingLoad + activeLoad
		log.Printf("🔍 GetSlotInfoWithOrders: слот %s - базовая загрузка 0, но найдены заказы: pending=%d₽, active=%d₽", 
			slotID, pendingLoad, activeLoad)
	}
	
	// Получаем информацию о слоте
	slotInfoKey := slotKey + ":info"
	info, err := ss.client.HGetAll(ctx, slotInfoKey).Result()
	if err != nil {
		// Если информации нет, используем переданные времена
		return &SlotInfo{
			SlotID:      slotID,
			StartTime:   slotStart,
			EndTime:     slotEnd,
			CurrentLoad: int(totalLoad),
			MaxCapacity: ss.maxCapacityPerSlot,
		}, nil
	}
	
	var startTime, endTime time.Time
	if startStr, ok := info["start_time"]; ok {
		startTime, _ = time.Parse(time.RFC3339, startStr)
	} else {
		startTime = slotStart
	}
	if endStr, ok := info["end_time"]; ok {
		endTime, _ = time.Parse(time.RFC3339, endStr)
	} else {
		endTime = slotEnd
	}
	
	return &SlotInfo{
		SlotID:      slotID,
		StartTime:   startTime,
		EndTime:     endTime,
		CurrentLoad: int(totalLoad),
		MaxCapacity: ss.maxCapacityPerSlot,
	}, nil
}

// ReleaseSlot освобождает место в слоте (если заказ отменен)
func (ss *SlotService) ReleaseSlot(orderID string) error {
	if ss.redisUtil == nil {
		return fmt.Errorf("Redis client not initialized")
	}

	ctx := ss.redisUtil.Context()
	orderSlotKey := fmt.Sprintf("order:slot:%s", orderID)
	
	if ss.client == nil {
		return fmt.Errorf("Redis client not available")
	}
	
	// Получаем ID слота и сумму заказа для этого заказа
	info, err := ss.client.HGetAll(ctx, orderSlotKey).Result()
	if err == redis.Nil || len(info) == 0 {
		return nil // Заказ не был назначен на слот
	}
	if err != nil {
		return err
	}
	
	slotID, ok := info["slot_id"]
	if !ok {
		return nil // Нет информации о слоте
	}
	
	orderPriceStr, ok := info["price"]
	orderPrice := 0
	if ok {
		fmt.Sscanf(orderPriceStr, "%d", &orderPrice)
	}
	
	slotKey := fmt.Sprintf("slot:%s", slotID)
	
	// Атомарно уменьшаем сумму слота на сумму заказа и удаляем заказ из списка
	luaScript := `
		local slot_key = KEYS[1]
		local order_key = KEYS[2]
		local order_id = ARGV[1]
		local order_price = tonumber(ARGV[2])
		
		-- Уменьшаем сумму слота на сумму заказа (но не ниже 0)
		local current_load = redis.call('GET', slot_key)
		if current_load ~= false then
			local load = tonumber(current_load)
			if load >= order_price then
				redis.call('INCRBY', slot_key, -order_price)
			else
				redis.call('SET', slot_key, 0)
			end
		end
		
		-- Удаляем заказ из списка заказов слота
		redis.call('SREM', slot_key .. ':orders', order_id)
		
		-- Удаляем связь заказ -> слот
		redis.call('DEL', order_key)
		
		return 1
	`
	
	_, err = ss.client.Eval(ctx, luaScript, []string{
		slotKey,
		orderSlotKey,
	}, []interface{}{
		orderID,
		orderPrice,
	}).Result()
	
	if err != nil {
		return fmt.Errorf("ошибка при освобождении слота: %w", err)
	}
	
	log.Printf("✅ SlotService: место в слоте %s освобождено для заказа %s", slotID, orderID)
	return nil
}

// GetAllSlots получает информацию о ВСЕХ слотах (включая прошедшие, текущие и будущие)
// КРИТИЧНО: Возвращает слоты с начала дня (9:00) до конца дня (24:00)
// Включает прошедшие слоты для истории (минимум 1-2 часа назад)
// ВАЖНО: Все времена в UTC, клиент сам конвертирует в свой часовой пояс
func (ss *SlotService) GetAllSlots() ([]*SlotInfo, error) {
	if ss.redisUtil == nil || ss.client == nil {
		return nil, fmt.Errorf("Redis client not initialized")
	}

	// Используем UTC для всех временных операций
	now := time.Now().UTC()
	slots := make([]*SlotInfo, 0)

	// Начинаем с начала дня (9:00) для показа истории
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 9, 0, 0, 0, time.UTC)
	// Определяем конец дня (24:00)
	endOfDay := time.Date(now.Year(), now.Month(), now.Day(), 24, 0, 0, 0, time.UTC)
	
	// Также включаем слоты за последние 2 часа для истории
	historyStart := now.Add(-2 * time.Hour)
	if historyStart.Before(startOfDay) {
		historyStart = startOfDay
	}
	
	// Начинаем с самого раннего времени (начало дня или 2 часа назад)
	slotStart := startOfDay
	
	// Округляем до ближайшего слота (15 минут)
	minutes := slotStart.Minute()
	slotMinutes := (minutes / int(ss.slotDuration.Minutes())) * int(ss.slotDuration.Minutes())
	slotStart = time.Date(slotStart.Year(), slotStart.Month(), slotStart.Day(), 
		slotStart.Hour(), slotMinutes, 0, 0, time.UTC)

	// Генерируем слоты от начала дня до конца дня
	// ВАЖНО: Включаем ВСЕ слоты (прошедшие, текущие и будущие)
	stopReason := ""
	for slotStart.Before(endOfDay) {
		// КРИТИЧНО: Проверяем, что слот все еще в ТЕКУЩЕМ дне (в UTC)
		if slotStart.Day() != now.Day() || slotStart.Month() != now.Month() || slotStart.Year() != now.Year() {
			stopReason = fmt.Sprintf("переход на следующий день (слот: %s UTC, текущий день: %s)", 
				slotStart.Format("2006-01-02 15:04:05"), now.Format("2006-01-02"))
			break
		}
		
		slotEnd := slotStart.Add(ss.slotDuration)
		
		// КРИТИЧНО: Проверяем, что слот полностью помещается в текущий день (в UTC)
		if slotEnd.Day() != now.Day() || slotEnd.Month() != now.Month() || slotEnd.Year() != now.Year() {
			stopReason = fmt.Sprintf("слот переходит на следующий день (слот: %s - %s UTC)", 
				slotStart.Format("15:04:05"), slotEnd.Format("15:04:05"))
			break
		}

		slotID := ss.generateSlotID(slotStart)

		// Получаем информацию о слоте с учетом pending + active заказов
		slotInfo, err := ss.GetSlotInfoWithOrders(slotID, slotStart, slotEnd)
		if err != nil {
			// Если слот не существует, создаем пустой
			slotInfo = &SlotInfo{
				SlotID:      slotID,
				StartTime:   slotStart,
				EndTime:     slotEnd,
				CurrentLoad: 0,
				MaxCapacity: ss.maxCapacityPerSlot,
			}
		} else {
			// ВСЕГДА используем вычисленные времена, а не из Redis
			slotInfo.StartTime = slotStart
			slotInfo.EndTime = slotEnd
			slotInfo.SlotID = slotID
		}

		slots = append(slots, slotInfo)
		
		// Переходим к следующему слоту
		slotStart = slotStart.Add(ss.slotDuration)
		
		// Страховка, чтобы не уйти в бесконечный цикл (максимум 200 слотов для полного дня)
		if len(slots) > 200 {
			stopReason = "достигнут лимит в 200 слотов (страховка от бесконечного цикла)"
			break
		}
	}

	// Итоговое логирование
	if len(slots) > 0 {
		if stopReason != "" {
			log.Printf("📊 GetAllSlots: возвращено %d слотов (от %s до %s UTC). Остановка: %s", 
				len(slots), slots[0].StartTime.Format("15:04"), slots[len(slots)-1].StartTime.Format("15:04"), stopReason)
		} else {
			log.Printf("📊 GetAllSlots: возвращено %d слотов (от %s до %s UTC)", 
				len(slots), slots[0].StartTime.Format("15:04"), slots[len(slots)-1].StartTime.Format("15:04"))
		}
	} else if stopReason != "" {
		log.Printf("⚠️ GetAllSlots: слоты не сгенерированы. Причина: %s", stopReason)
	}

	return slots, nil
}
