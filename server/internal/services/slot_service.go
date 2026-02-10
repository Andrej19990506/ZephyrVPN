package services

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"zephyrvpn/server/internal/models"
	"zephyrvpn/server/internal/pb"
	"google.golang.org/protobuf/proto"
	"zephyrvpn/server/internal/utils"
)

// SlotService управляет временными слотами для Capacity-Based Slot Scheduling
type SlotService struct {
	redisUtil *utils.RedisClient
	client    *redis.Client // Прямой доступ к Redis клиенту для Lua scripts
	db        *gorm.DB      // Доступ к PostgreSQL для персистентного хранения планов
	slotDuration time.Duration // Длительность слота (по умолчанию 15 минут)
	maxCapacityPerSlot int     // Максимальная емкость слота в РУБЛЯХ (не количество заказов!)
	
	// Бизнес-часы пиццерии (в UTC, клиент сам конвертирует в свой часовой пояс)
	openHour  int // Час открытия в UTC
	openMin   int // Минута открытия в UTC
	closeHour int // Час закрытия в UTC
	closeMin  int // Минута закрытия в UTC
}

// OrderInfo информация о заказе в слоте
type OrderInfo struct {
	ID      string `json:"id"`       // ID заказа
	Total   int    `json:"total"`    // Сумма заказа в рублях
	IsPickup bool  `json:"is_pickup"` // Самовывоз или доставка
}

// SlotInfo информация о слоте
type SlotInfo struct {
	SlotID        string      `json:"slot_id"`
	StartTime     time.Time   `json:"start_time"`     // RFC3339 формат
	EndTime       time.Time   `json:"end_time"`       // RFC3339 формат
	CurrentLoad   int         `json:"current_load"`   // Текущая сумма в рублях
	MaxCapacity   int         `json:"max_capacity"`   // Максимальная сумма в рублях
	Disabled      bool        `json:"disabled"`       // Отключен ли слот
	OrdersCount   int         `json:"orders_count"`   // Общее количество заказов
	DeliveryCount int         `json:"delivery_count"` // Количество заказов на доставку
	PickupCount   int         `json:"pickup_count"`   // Количество заказов на самовывоз
	DeliveryPlan  int         `json:"delivery_plan"`  // План для доставки
	PickupPlan    int         `json:"pickup_plan"`     // План для самовывоза
	Orders        []OrderInfo `json:"orders"`         // Список заказов в слоте
}

// NewSlotService создает новый сервис слотов
// ВАЖНО: Все временные операции выполняются в UTC
// Конвертация в локальное время происходит на клиенте (фронтенде)
// Бизнес-часы задаются в UTC через переменные окружения
func NewSlotService(redisUtil *utils.RedisClient, db *gorm.DB, openHour, openMin, closeHour, closeMin int) *SlotService {
	ss := &SlotService{
		redisUtil:         redisUtil,
		db:                db,              // PostgreSQL для персистентного хранения планов
		slotDuration:      15 * time.Minute, // 15 минут по умолчанию
		maxCapacityPerSlot: 10000,           // 10000 рублей на слот по умолчанию (устанавливается через ERP API UpdateSlotConfig)
		openHour:          openHour,         // Открытие в UTC
		openMin:           openMin,          // Минута открытия в UTC
		closeHour:         closeHour,        // Закрытие в UTC
		closeMin:          closeMin,         // Минута закрытия в UTC
	}
	
	log.Printf("✅ SlotService инициализирован: рабочие часы %02d:%02d - %02d:%02d UTC (клиент конвертирует в свой часовой пояс)", 
		openHour, openMin, closeHour, closeMin)
	
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
func (ss *SlotService) isWithinWorkingHours(t time.Time) bool {
	// Работаем напрямую с UTC, без конвертации
	// Клиент сам конвертирует время в свой часовой пояс
	utcTime := t.UTC()
	
	hour := utcTime.Hour()
	min := utcTime.Minute()
	
	// Если час меньше открытия
	if hour < ss.openHour {
		return false
	}
	
	// Если час равен открытию, проверяем минуты (от openMin включительно)
	if hour == ss.openHour && min < ss.openMin {
		return false
	}
	
	// Если час больше закрытия
	if hour > ss.closeHour {
		return false
	}
	
	// Если последний час (closeHour), проверяем минуты (до closeMin включительно)
	if hour == ss.closeHour && min > ss.closeMin {
		return false
	}
	
	return true
}

// IsSlotDisabled проверяет, отключен ли слот
func (ss *SlotService) IsSlotDisabled(slotID string) bool {
	if ss.redisUtil == nil {
		return false
	}
	
	key := fmt.Sprintf("slot:%s:disabled", slotID)
	
	disabled, err := ss.redisUtil.Get(key)
	if err != nil {
		return false
	}
	
	return disabled == "1"
}

// SetSlotDisabled устанавливает статус отключения слота
func (ss *SlotService) SetSlotDisabled(slotID string, disabled bool) error {
	if ss.redisUtil == nil {
		return fmt.Errorf("Redis client not initialized")
	}
	
	key := fmt.Sprintf("slot:%s:disabled", slotID)
	
	if disabled {
		// Устанавливаем TTL 24 часа для автоматической очистки
		return ss.redisUtil.Set(key, "1", 24*time.Hour)
	} else {
		// Удаляем ключ, если слот включается
		return ss.redisUtil.Delete(key)
	}
}

// SetSlotPlan устанавливает план для слота (delivery_plan и pickup_plan)
// КРИТИЧНО: Сохраняет в PostgreSQL (персистентно) и Redis (кэш)
func (ss *SlotService) SetSlotPlan(slotID string, deliveryPlan, pickupPlan int) error {
	// 1. Сохраняем в PostgreSQL для персистентности
	if ss.db != nil {
		// Используем UPSERT (INSERT ... ON CONFLICT UPDATE) для PostgreSQL
		query := `
			INSERT INTO slot_plans (slot_id, delivery_plan, pickup_plan, updated_at)
			VALUES ($1, $2, $3, CURRENT_TIMESTAMP)
			ON CONFLICT (slot_id) 
			DO UPDATE SET 
				delivery_plan = EXCLUDED.delivery_plan,
				pickup_plan = EXCLUDED.pickup_plan,
				updated_at = CURRENT_TIMESTAMP
		`
		if err := ss.db.Exec(query, slotID, deliveryPlan, pickupPlan).Error; err != nil {
			log.Printf("⚠️ Ошибка сохранения плана слота в БД: %v", err)
			// Не возвращаем ошибку - продолжаем с Redis
		}
	}
	
	// 2. Сохраняем в Redis для быстрого доступа (кэш)
	if ss.redisUtil == nil {
		return fmt.Errorf("Redis client not initialized")
	}
	
	ctx := ss.redisUtil.Context()
	slotKey := fmt.Sprintf("slot:%s:info", slotID)
	
	// Сохраняем планы в Redis hash
	info := make(map[string]interface{})
	info["delivery_plan"] = strconv.Itoa(deliveryPlan)
	info["pickup_plan"] = strconv.Itoa(pickupPlan)
	
	// Используем HSet для обновления только этих полей
	if ss.client != nil {
		for k, v := range info {
			if err := ss.client.HSet(ctx, slotKey, k, v).Err(); err != nil {
				return fmt.Errorf("failed to set slot plan in Redis: %w", err)
			}
		}
		// Устанавливаем TTL 24 часа (кэш)
		ss.client.Expire(ctx, slotKey, 24*time.Hour)
		return nil
	}
	
	return fmt.Errorf("Redis client not available")
}

// GetSlotPlan получает планы для слота
// КРИТИЧНО: Сначала проверяет Redis (кэш), затем PostgreSQL (персистентное хранилище)
func (ss *SlotService) GetSlotPlan(slotID string) (deliveryPlan, pickupPlan int, err error) {
	// 1. Сначала проверяем Redis (быстрый кэш)
	if ss.redisUtil != nil && ss.client != nil {
		ctx := ss.redisUtil.Context()
		slotKey := fmt.Sprintf("slot:%s:info", slotID)
		
		info, err := ss.client.HGetAll(ctx, slotKey).Result()
		if err == nil && len(info) > 0 {
			if deliveryStr, ok := info["delivery_plan"]; ok && deliveryStr != "" {
				deliveryPlan, _ = strconv.Atoi(deliveryStr)
			}
			if pickupStr, ok := info["pickup_plan"]; ok && pickupStr != "" {
				pickupPlan, _ = strconv.Atoi(pickupStr)
			}
			// Если оба плана найдены в Redis - возвращаем их
			if deliveryPlan > 0 || pickupPlan > 0 {
				return deliveryPlan, pickupPlan, nil
			}
		}
	}
	
	// 2. Если в Redis нет или планы = 0, загружаем из PostgreSQL
	if ss.db != nil {
		var result struct {
			DeliveryPlan int `gorm:"column:delivery_plan"`
			PickupPlan   int `gorm:"column:pickup_plan"`
		}
		
		if err := ss.db.Raw("SELECT delivery_plan, pickup_plan FROM slot_plans WHERE slot_id = ?", slotID).Scan(&result).Error; err == nil {
			// Если нашли в БД - обновляем Redis кэш и возвращаем
			if result.DeliveryPlan > 0 || result.PickupPlan > 0 {
				deliveryPlan = result.DeliveryPlan
				pickupPlan = result.PickupPlan
				
				// Обновляем Redis кэш для следующего раза
				if ss.redisUtil != nil && ss.client != nil {
					ctx := ss.redisUtil.Context()
					slotKey := fmt.Sprintf("slot:%s:info", slotID)
					ss.client.HSet(ctx, slotKey, "delivery_plan", strconv.Itoa(deliveryPlan))
					ss.client.HSet(ctx, slotKey, "pickup_plan", strconv.Itoa(pickupPlan))
					ss.client.Expire(ctx, slotKey, 24*time.Hour)
				}
				
				return deliveryPlan, pickupPlan, nil
			}
		}
	}
	
	// 3. Если ни в Redis, ни в БД нет - возвращаем 0, 0 (это нормально для новых слотов)
	return 0, 0, nil
}

// GetSlotMaxCapacity получает максимальную емкость слота (индивидуальную или общую)
func (ss *SlotService) GetSlotMaxCapacity(slotID string) int {
	if ss.redisUtil == nil {
		return ss.maxCapacityPerSlot
	}
	
	key := fmt.Sprintf("slot:%s:max_capacity", slotID)
	
	capacityStr, err := ss.redisUtil.Get(key)
	if err != nil {
		// Если индивидуального лимита нет, возвращаем общий
		return ss.maxCapacityPerSlot
	}
	
	capacity, err := strconv.Atoi(capacityStr)
	if err != nil {
		return ss.maxCapacityPerSlot
	}
	
	return capacity
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
	slotsChecked := 0   // Счетчик проверенных слотов в рабочих часах
	
	// Проверяем, открыта ли кухня сейчас
	isKitchenOpen := ss.isWithinWorkingHours(now)
	
	for attempt := 0; attempt < maxAttempts; attempt++ {
		// Проверяем, что слот все еще в текущем дне
		if slotStart.Day() != now.Day() || slotStart.Month() != now.Month() || slotStart.Year() != now.Year() {
			// Перешли на следующий день - проверяем, была ли кухня открыта
			if isKitchenOpen && slotsChecked > 0 {
				// Кухня была открыта, но все слоты заполнены
				log.Printf("⚠️ AssignSlot: все слоты заполнены на сегодня (проверено %d слотов в рабочих часах)", slotsChecked)
				return "", time.Time{}, time.Time{}, status.Error(codes.ResourceExhausted, "All slots are full for today")
			}
			// Кухня закрыта
			return "", time.Time{}, time.Time{}, fmt.Errorf("кухня закрыта, заказы на сегодня не принимаются (рабочее время: %02d:%02d - %02d:%02d UTC)", 
				ss.openHour, ss.openMin, ss.closeHour, ss.closeMin)
		}
		
		// Вычисляем конец рабочего дня
		endOfDay := time.Date(now.Year(), now.Month(), now.Day(), ss.closeHour, ss.closeMin, 0, 0, time.UTC)
		
		// Проверяем, что слот не превышает конец рабочего дня
		if !slotStart.Before(endOfDay) {
			// Дошли до конца рабочего дня - проверяем, была ли кухня открыта
			if isKitchenOpen && slotsChecked > 0 {
				// Кухня была открыта, но все слоты заполнены
				log.Printf("⚠️ AssignSlot: все слоты заполнены на сегодня (проверено %d слотов в рабочих часах)", slotsChecked)
				return "", time.Time{}, time.Time{}, status.Error(codes.ResourceExhausted, "All slots are full for today")
			}
			// Кухня закрыта
			return "", time.Time{}, time.Time{}, fmt.Errorf("кухня закрыта, заказы на сегодня не принимаются (рабочее время: %02d:%02d - %02d:%02d UTC)", 
				ss.openHour, ss.openMin, ss.closeHour, ss.closeMin)
		}
		
		// Проверяем, что слот находится в рабочих часах пиццерии
		if !ss.isWithinWorkingHours(slotStart) {
			// Дошли до закрытия - проверяем, была ли кухня открыта
			if isKitchenOpen && slotsChecked > 0 {
				// Кухня была открыта, но все слоты заполнены
				log.Printf("⚠️ AssignSlot: все слоты заполнены на сегодня (проверено %d слотов в рабочих часах)", slotsChecked)
				return "", time.Time{}, time.Time{}, status.Error(codes.ResourceExhausted, "All slots are full for today")
			}
			// Кухня закрыта
			return "", time.Time{}, time.Time{}, fmt.Errorf("кухня закрыта, заказы на сегодня не принимаются (рабочее время: %02d:%02d - %02d:%02d UTC)", 
				ss.openHour, ss.openMin, ss.closeHour, ss.closeMin)
		}
		
		// Слот в рабочих часах - увеличиваем счетчик
		slotsChecked++
		
		slotID := ss.generateSlotID(slotStart)
		
		// ПРОВЕРКА: отключен ли слот
		if ss.IsSlotDisabled(slotID) {
			log.Printf("⚠️ AssignSlot: слот %s отключен, пропускаем", slotID)
			slotStart = slotStart.Add(ss.slotDuration)
			continue
		}
		
		// Получаем индивидуальный лимит слота или общий
		maxCapacity := ss.GetSlotMaxCapacity(slotID)
		
		// Используем Redis Lua script для атомарной операции
		// Это гарантирует, что только один заказ сможет занять последнее место
		// Считаем по СУММЕ заказов, а не по количеству!
		luaScript := `
			local slot_key = KEYS[1]
			local order_key = KEYS[2]
			local max_capacity = tonumber(ARGV[1])  -- Максимальная сумма в рублях (индивидуальная или общая)
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
			maxCapacity,                  // Максимальная сумма в рублях (индивидуальная или общая)
			slotID,
			orderID,
			orderPrice,                   // Сумма заказа в рублях
			slotStart.Format(time.RFC3339),
			slotEnd.Format(time.RFC3339),
		}).Result()
		
		if err != nil {
			// System Error: ошибка Redis/сети
			if failedAttempts%10 == 0 {
				log.Printf("❌ [System Error] SlotService: ошибка при бронировании слота %s: %v (попытка #%d)", slotID, err, attempt+1)
			}
			failedAttempts++
			// Jitter backoff для уменьшения contention при высокой нагрузке
			jitter := time.Duration(rand.Intn(10)) * time.Millisecond
			time.Sleep(jitter)
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
		
		// Business Error: слот переполнен (защита от перегрузки работает корректно)
		// Логируем только каждую 50-ю попытку, чтобы не засорять логи
		if failedAttempts%50 == 0 {
			log.Printf("✅ [Successful Overload Prevention] Слот %s переполнен: %d₽/%d₽ (попытка #%d)", 
				slotID, currentLoad, ss.maxCapacityPerSlot, attempt+1)
		}
		failedAttempts++
		
		// Jitter backoff для уменьшения contention при высокой нагрузке
		jitter := time.Duration(rand.Intn(10)) * time.Millisecond
		time.Sleep(jitter)
		
		// Переходим к следующему слоту (просто добавляем 15 минут)
		slotStart = slotStart.Add(ss.slotDuration)
	}

	// Все слоты переполнены - проверяем, была ли кухня открыта
	if isKitchenOpen && slotsChecked > 0 {
		// Кухня была открыта, но все слоты заполнены
		log.Printf("⚠️ AssignSlot: все слоты заполнены на сегодня (проверено %d слотов в рабочих часах)", slotsChecked)
		return "", time.Time{}, time.Time{}, status.Error(codes.ResourceExhausted, "All slots are full for today")
	}
	
	// Кухня закрыта или нет доступных слотов
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
		// Получаем индивидуальный лимит или общий
		maxCapacity := ss.GetSlotMaxCapacity(slotID)
		return &SlotInfo{
			SlotID:      slotID,
			CurrentLoad: 0,
			MaxCapacity: maxCapacity,
			Disabled:    ss.IsSlotDisabled(slotID),
			Orders:      make([]OrderInfo, 0),
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
	
	// Получаем индивидуальный лимит или общий
	maxCapacity := ss.GetSlotMaxCapacity(slotID)
	
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
		CurrentLoad: int(currentLoad),
		MaxCapacity: maxCapacity,
		Orders:      make([]OrderInfo, 0),
		Disabled:    ss.IsSlotDisabled(slotID),
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
	ordersCount := 0
	deliveryCount := 0
	pickupCount := 0
	orders := make([]OrderInfo, 0) // Список заказов для ответа
	
	// Получаем список заказов слота из Redis
	slotOrdersKey := slotKey + ":orders"
	orderIDs, err := ss.client.SMembers(ctx, slotOrdersKey).Result()
	if err == nil && len(orderIDs) > 0 {
		log.Printf("🔍 GetSlotInfoWithOrders: слот %s - найдено %d заказов в Redis", slotID, len(orderIDs))
		// Проверяем каждый заказ: pending или active?
		for _, orderID := range orderIDs {
			// Проверяем, находится ли заказ в pending_slots
			isPending, _ := ss.redisUtil.SIsMember("erp:orders:pending_slots", orderID)
			// Проверяем, находится ли заказ в active
			isActive, _ := ss.redisUtil.SIsMember("erp:orders:active", orderID)
			
			// Считаем заказ только если он pending или active
			if isPending || isActive {
				ordersCount++
				
				// Получаем полную информацию о заказе для определения типа доставки
				orderKey := "erp:order:" + orderID
				orderBytes, err := ss.redisUtil.GetBytes(orderKey)
				isPickup := false
				if err == nil {
					// Пробуем сначала Protobuf (быстрее!)
					pbOrder := &pb.PizzaOrder{}
					if err := proto.Unmarshal(orderBytes, pbOrder); err == nil {
						// Успешно распарсили Protobuf
						isPickup = pbOrder.IsPickup
						if isPickup {
							pickupCount++
						} else {
							deliveryCount++
						}
					} else {
						// Пробуем распарсить как JSON (fallback)
						var order models.PizzaOrder
						if err := json.Unmarshal(orderBytes, &order); err == nil {
							isPickup = order.IsPickup
							if isPickup {
								pickupCount++
							} else {
								deliveryCount++
							}
						} else {
							// Если не можем распарсить, используем дефолт (доставка)
							deliveryCount++
						}
					}
				} else {
					// Если не можем получить полную информацию, используем дефолт (доставка)
					deliveryCount++
				}
				
				// Получаем сумму заказа
				orderSlotKey := fmt.Sprintf("order:slot:%s", orderID)
				orderInfo, err := ss.client.HGetAll(ctx, orderSlotKey).Result()
				orderTotal := 0
				if err == nil {
					if priceStr, ok := orderInfo["price"]; ok {
						var orderPrice int64
						fmt.Sscanf(priceStr, "%d", &orderPrice)
						orderTotal = int(orderPrice)
						
						if isPending {
							pendingLoad += orderPrice
						} else if isActive {
							activeLoad += orderPrice
						}
					}
				}
				
				// Добавляем заказ в список
				orders = append(orders, OrderInfo{
					ID:       orderID,
					Total:    orderTotal,
					IsPickup: isPickup,
				})
			}
		}
		log.Printf("📊 GetSlotInfoWithOrders: слот %s - orders_count=%d, delivery=%d, pickup=%d", 
			slotID, ordersCount, deliveryCount, pickupCount)
	} else {
		// Не логируем отсутствие заказов - это нормально для пустых слотов
		// Логируем только реальные ошибки Redis
		if err != nil && err.Error() != "redis: nil" {
			log.Printf("⚠️ GetSlotInfoWithOrders: ошибка Redis для слота %s (ключ: %s): %v", 
				slotID, slotOrdersKey, err)
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
	
	// Получаем планы из Redis, если они есть
	deliveryPlan, pickupPlan, _ := ss.GetSlotPlan(slotID)
	
	if err != nil {
		// Если информации нет, используем переданные времена
		maxCapacity := ss.GetSlotMaxCapacity(slotID)
		// Если планов нет в Redis, вычисляем на основе max_capacity
		if deliveryPlan == 0 && pickupPlan == 0 && maxCapacity > 0 {
			deliveryPlan = int(float64(maxCapacity) * 0.85)
			pickupPlan = int(float64(maxCapacity) * 0.15)
		}
		return &SlotInfo{
			SlotID:        slotID,
			StartTime:     slotStart,
			EndTime:       slotEnd,
			CurrentLoad:   int(totalLoad),
			MaxCapacity:   maxCapacity,
			Disabled:      ss.IsSlotDisabled(slotID),
			OrdersCount:   ordersCount,
			DeliveryCount: deliveryCount,
			PickupCount:   pickupCount,
			DeliveryPlan:  deliveryPlan,
			PickupPlan:    pickupPlan,
			Orders:        orders,
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
	
	maxCapacity := ss.GetSlotMaxCapacity(slotID)
	return &SlotInfo{
		SlotID:        slotID,
		StartTime:     startTime,
		EndTime:       endTime,
		CurrentLoad:   int(totalLoad),
		MaxCapacity:   maxCapacity,
		Disabled:      ss.IsSlotDisabled(slotID),
		OrdersCount:   ordersCount,
		DeliveryCount: deliveryCount,
		PickupCount:   pickupCount,
		Orders:        orders,
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
// КРИТИЧНО: Возвращает слоты только в рабочих часах (openHour:openMin - closeHour:closeMin)
// Включает прошедшие слоты для истории (минимум 1-2 часа назад)
// ВАЖНО: Все времена в UTC, клиент сам конвертирует в свой часовой пояс
func (ss *SlotService) GetAllSlots() ([]*SlotInfo, error) {
	if ss.redisUtil == nil || ss.client == nil {
		return nil, fmt.Errorf("Redis client not initialized")
	}

	// Используем UTC для всех временных операций
	now := time.Now().UTC()
	slots := make([]*SlotInfo, 0)

	// Начинаем с начала рабочего дня (openHour:openMin) для показа истории
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), ss.openHour, ss.openMin, 0, 0, time.UTC)
	// Определяем конец рабочего дня (closeHour:closeMin)
	endOfDay := time.Date(now.Year(), now.Month(), now.Day(), ss.closeHour, ss.closeMin, 0, 0, time.UTC)
	
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

	// Генерируем слоты от начала рабочего дня до конца рабочего дня
	// ВАЖНО: Включаем только слоты в рабочих часах (прошедшие, текущие и будущие)
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

		// Проверяем, что слот находится в рабочих часах пиццерии
		if !ss.isWithinWorkingHours(slotStart) {
			// Пропускаем слот, который не в рабочих часах
			slotStart = slotStart.Add(ss.slotDuration)
			continue
		}

		slotID := ss.generateSlotID(slotStart)

		// Получаем информацию о слоте с учетом pending + active заказов
		slotInfo, err := ss.GetSlotInfoWithOrders(slotID, slotStart, slotEnd)
		if err != nil {
			// Если слот не существует, создаем пустой
			maxCapacity := ss.GetSlotMaxCapacity(slotID)
			// КРИТИЧНО: Загружаем планы из Redis, если они есть
			deliveryPlan, pickupPlan, _ := ss.GetSlotPlan(slotID)
			// Если планов нет в Redis, вычисляем на основе max_capacity
			if deliveryPlan == 0 && pickupPlan == 0 && maxCapacity > 0 {
				deliveryPlan = int(float64(maxCapacity) * 0.85)
				pickupPlan = int(float64(maxCapacity) * 0.15)
			}
			slotInfo = &SlotInfo{
				SlotID:        slotID,
				StartTime:     slotStart,
				EndTime:       slotEnd,
				CurrentLoad:   0,
				MaxCapacity:   maxCapacity,
				Disabled:      ss.IsSlotDisabled(slotID),
				OrdersCount:   0,
				DeliveryCount: 0,
				PickupCount:   0,
				DeliveryPlan:  deliveryPlan,
				PickupPlan:    pickupPlan,
				Orders:        make([]OrderInfo, 0), // Инициализируем пустой массив
			}
		} else {
			// ВСЕГДА используем вычисленные времена, а не из Redis
			slotInfo.StartTime = slotStart
			slotInfo.EndTime = slotEnd
			slotInfo.SlotID = slotID
			// Обновляем индивидуальный лимит и disabled статус
			slotInfo.MaxCapacity = ss.GetSlotMaxCapacity(slotID)
			slotInfo.Disabled = ss.IsSlotDisabled(slotID)
			
			// КРИТИЧНО: Убеждаемся, что планы загружены из Redis/БД
			// Если планы = 0, проверяем Redis/БД - возможно, они были сохранены как 0
			if slotInfo.DeliveryPlan == 0 && slotInfo.PickupPlan == 0 && slotInfo.MaxCapacity > 0 {
				redisDeliveryPlan, redisPickupPlan, err := ss.GetSlotPlan(slotID)
				if err == nil {
					// Если в Redis/БД есть хотя бы один план - используем их
					if redisDeliveryPlan > 0 || redisPickupPlan > 0 {
						slotInfo.DeliveryPlan = redisDeliveryPlan
						slotInfo.PickupPlan = redisPickupPlan
					}
				}
			}
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
