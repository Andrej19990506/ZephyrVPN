package services

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"strings"
	"time"

	"github.com/lib/pq"
	"zephyrvpn/server/internal/models"
	"zephyrvpn/server/internal/utils"
)

// OrderService управляет заказами и их состоянием
type OrderService struct {
	db        *sql.DB
	redisUtil *utils.RedisClient
}

// NewOrderService создает новый сервис заказов
func NewOrderService(db *sql.DB, redisUtil *utils.RedisClient) *OrderService {
	return &OrderService{
		db:        db,
		redisUtil: redisUtil,
	}
}

// BootstrapState восстанавливает состояние активных заказов из PostgreSQL в Redis
// Выполняется при старте сервера ПЕРЕД запуском Kafka consumer
// Цель: восстановить операционное состояние после перезапуска
func (os *OrderService) BootstrapState() error {
	if os.db == nil {
		return fmt.Errorf("database connection not available")
	}
	if os.redisUtil == nil {
		return fmt.Errorf("Redis connection not available")
	}

	startTime := time.Now()
	log.Printf("🔄 BootstrapState: начало восстановления состояния из PostgreSQL...")

	// Запрашиваем все активные заказы (pending, preparing, cooking, ready, delivery)
	// Используем индекс (status, created_at) для быстрого поиска
	query := `
		SELECT 
			id, display_id, customer_id, customer_first_name, customer_last_name,
			customer_phone, delivery_address, payment_method, is_pickup, pickup_location_id,
			call_before_minutes, items, is_set, set_name, total_price, discount_amount,
			discount_percent, final_price, notes, status, created_at, updated_at,
			completed_at, cancelled_at, target_slot_id, target_slot_start_time, visible_at,
			branch_id, station_id, staff_id
		FROM orders
		WHERE status IN ('pending', 'preparing', 'cooking', 'ready', 'delivery')
		ORDER BY created_at DESC
		LIMIT 10000
	`

	rows, err := os.db.Query(query)
	if err != nil {
		return fmt.Errorf("ошибка запроса активных заказов: %w", err)
	}
	defer rows.Close()

	var ordersLoaded int
	var ordersRestored int
	var ordersPending int
	var ordersActive int

	ctx := os.redisUtil.Context()

	// Обрабатываем заказы батчами для оптимизации Redis операций
	batchSize := 100
	orderBatch := make([]models.PizzaOrder, 0, batchSize)

	for rows.Next() {
		var order models.PizzaOrder
		var itemsJSON []byte
		var targetSlotStartTime, visibleAt, completedAt, cancelledAt, updatedAt sql.NullTime
		var customerID, callBeforeMinutes, discountAmount, discountPercent, finalPrice sql.NullInt64
		var displayID, customerFirstName, customerLastName, customerPhone, deliveryAddress sql.NullString
		var paymentMethod, pickupLocationID, setName, notes, targetSlotID sql.NullString
		var branchID, stationID, staffID sql.NullString

		err := rows.Scan(
			&order.ID, &displayID, &customerID, &customerFirstName, &customerLastName,
			&customerPhone, &deliveryAddress, &paymentMethod, &order.IsPickup, &pickupLocationID,
			&callBeforeMinutes, &itemsJSON, &order.IsSet, &setName, &order.TotalPrice,
			&discountAmount, &discountPercent, &finalPrice, &notes, &order.Status,
			&order.CreatedAt, &updatedAt, &completedAt, &cancelledAt,
			&targetSlotID, &targetSlotStartTime, &visibleAt, &branchID, &stationID, &staffID,
		)
		if err != nil {
			log.Printf("⚠️ BootstrapState: ошибка сканирования заказа: %v", err)
			continue
		}

		// Заполняем опциональные поля
		if displayID.Valid {
			order.DisplayID = displayID.String
		}
		if customerID.Valid {
			order.CustomerID = int(customerID.Int64)
		}
		if customerFirstName.Valid {
			order.CustomerFirstName = customerFirstName.String
		}
		if customerLastName.Valid {
			order.CustomerLastName = customerLastName.String
		}
		if customerPhone.Valid {
			order.CustomerPhone = customerPhone.String
		}
		if deliveryAddress.Valid {
			order.DeliveryAddress = deliveryAddress.String
		}
		if paymentMethod.Valid {
			order.PaymentMethod = paymentMethod.String
		}
		if pickupLocationID.Valid {
			order.PickupLocationID = pickupLocationID.String
		}
		if callBeforeMinutes.Valid {
			order.CallBeforeMinutes = int(callBeforeMinutes.Int64)
		}
		if setName.Valid {
			order.SetName = setName.String
		}
		if discountAmount.Valid {
			order.DiscountAmount = int(discountAmount.Int64)
		}
		if discountPercent.Valid {
			order.DiscountPercent = int(discountPercent.Int64)
		}
		if finalPrice.Valid {
			order.FinalPrice = int(finalPrice.Int64)
		}
		if notes.Valid {
			order.Notes = notes.String
		}
		if targetSlotID.Valid {
			order.TargetSlotID = targetSlotID.String
		}
		if targetSlotStartTime.Valid {
			order.TargetSlotStartTime = targetSlotStartTime.Time
		}
		if visibleAt.Valid {
			order.VisibleAt = visibleAt.Time
		}
		if completedAt.Valid {
			// Можно добавить поле CompletedAt в модель, если нужно
		}
		if cancelledAt.Valid {
			// Можно добавить поле CancelledAt в модель, если нужно
		}

		// Парсим JSON items
		if err := json.Unmarshal(itemsJSON, &order.Items); err != nil {
			log.Printf("⚠️ BootstrapState: ошибка парсинга items для заказа %s: %v", order.ID, err)
			continue
		}

		ordersLoaded++
		orderBatch = append(orderBatch, order)

		// Обрабатываем батч
		if len(orderBatch) >= batchSize {
			restored, pending, active := os.restoreOrderBatch(ctx, orderBatch)
			ordersRestored += restored
			ordersPending += pending
			ordersActive += active
			orderBatch = orderBatch[:0] // Очищаем батч
		}
	}

	// Обрабатываем оставшиеся заказы
	if len(orderBatch) > 0 {
		restored, pending, active := os.restoreOrderBatch(ctx, orderBatch)
		ordersRestored += restored
		ordersPending += pending
		ordersActive += active
	}

	duration := time.Since(startTime)
	log.Printf("✅ BootstrapState: завершено за %v", duration)
	log.Printf("   📊 Загружено из БД: %d заказов", ordersLoaded)
	log.Printf("   ✅ Восстановлено в Redis: %d заказов", ordersRestored)
	log.Printf("   📅 В pending_slots: %d заказов", ordersPending)
	log.Printf("   🔥 В active: %d заказов", ordersActive)

	if duration > 1*time.Second {
		log.Printf("⚠️ BootstrapState: восстановление заняло %.2f секунд (цель: < 1 секунда)", duration.Seconds())
	} else {
		log.Printf("✅ BootstrapState: восстановление выполнено за %.2f секунд (цель достигнута)", duration.Seconds())
	}

	return nil
}

// restoreOrderBatch восстанавливает батч заказов в Redis
func (os *OrderService) restoreOrderBatch(ctx context.Context, orders []models.PizzaOrder) (restored, pending, active int) {
	for _, order := range orders {
		// Сохраняем заказ в Redis
		orderJSON, err := json.Marshal(order)
		if err != nil {
			log.Printf("⚠️ restoreOrderBatch: ошибка сериализации заказа %s: %v", order.ID, err)
			continue
		}

		orderKey := fmt.Sprintf("erp:order:%s", order.ID)
		if err := os.redisUtil.SetBytes(orderKey, orderJSON, 24*time.Hour); err != nil {
			log.Printf("⚠️ restoreOrderBatch: ошибка сохранения заказа %s в Redis: %v", order.ID, err)
			continue
		}

		// Сохраняем метаданные слота
		if order.TargetSlotID != "" {
			slotKey := fmt.Sprintf("order:slot:start:%s", order.ID)
			if !order.TargetSlotStartTime.IsZero() {
				os.redisUtil.Set(slotKey, order.TargetSlotStartTime.Format(time.RFC3339), 24*time.Hour)
			}
		}

		if !order.VisibleAt.IsZero() {
			visibleAtKey := fmt.Sprintf("order:visible_at:%s", order.ID)
			os.redisUtil.Set(visibleAtKey, order.VisibleAt.Format(time.RFC3339), 24*time.Hour)
		}

		// Определяем, в какой набор добавить заказ
		now := time.Now().UTC()
		if !order.VisibleAt.IsZero() && order.VisibleAt.After(now) {
			// Заказ еще не должен быть показан - добавляем в pending_slots
			os.redisUtil.SAdd("erp:orders:pending_slots", order.ID)
			pending++
		} else {
			// Заказ должен быть показан - добавляем в active
			os.redisUtil.SAdd("erp:orders:active", order.ID)
			active++
		}

		// Обновляем счетчики
		os.redisUtil.Increment("erp:orders:pending")
		restored++
	}

	return restored, pending, active
}

// ArchiveOldOrders архивирует старые заказы (старше 1 года) для переноса в холодное хранилище
// Вызывается фоновым воркером раз в день
func (os *OrderService) ArchiveOldOrders() error {
	if os.db == nil {
		return fmt.Errorf("database connection not available")
	}

	startTime := time.Now()
	log.Printf("🗄️ ArchiveOldOrders: начало архивирования старых заказов...")

	// Находим заказы старше 1 года со статусом delivered или cancelled
	cutoffDate := time.Now().AddDate(-1, 0, 0)
	
	query := `
		UPDATE orders
		SET status = 'archived', updated_at = NOW()
		WHERE status IN ('delivered', 'cancelled')
		AND created_at < $1
		AND status != 'archived'
	`

	result, err := os.db.Exec(query, cutoffDate)
	if err != nil {
		return fmt.Errorf("ошибка архивирования заказов: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("ошибка получения количества заархивированных заказов: %w", err)
	}

	duration := time.Since(startTime)
	log.Printf("✅ ArchiveOldOrders: заархивировано %d заказов за %v", rowsAffected, duration)

	return nil
}

// SaveOrder сохраняет заказ в PostgreSQL (использует транзакционную версию)
func (os *OrderService) SaveOrder(order models.PizzaOrder) error {
	return os.SaveOrderWithTransaction(order)
}

// SaveOrderWithTransaction сохраняет заказ в PostgreSQL с SERIALIZABLE изоляцией
// Использует retry logic для обработки serialization failures
func (os *OrderService) SaveOrderWithTransaction(order models.PizzaOrder) error {
	if os.db == nil {
		return fmt.Errorf("database connection not available")
	}

	maxRetries := 5
	baseDelay := 10 * time.Millisecond

	for attempt := 0; attempt < maxRetries; attempt++ {
		err := os.saveOrderInTransaction(order)
		if err == nil {
			// Success
			if attempt > 0 {
				log.Printf("✅ SaveOrderWithTransaction: успешно после %d попыток (order: %s)", attempt+1, order.ID)
			}
			return nil
		}

		// Check if it's a serialization failure
		if isSerializationFailure(err) {
			if attempt < maxRetries-1 {
				// Exponential backoff with jitter
				delay := baseDelay * time.Duration(1<<uint(attempt))
				jitter := time.Duration(rand.Intn(10)) * time.Millisecond
				totalDelay := delay + jitter
				
				log.Printf("⚠️ SaveOrderWithTransaction: serialization failure (попытка %d/%d, order: %s), retry через %v", 
					attempt+1, maxRetries, order.ID, totalDelay)
				time.Sleep(totalDelay)
				continue
			}
			// Max retries reached
			return fmt.Errorf("serialization failure after %d attempts: %w", maxRetries, err)
		}

		// Non-serialization error - return immediately
		return fmt.Errorf("ошибка сохранения заказа: %w", err)
	}

	return fmt.Errorf("unreachable code")
}

// saveOrderInTransaction выполняет сохранение заказа в транзакции с SERIALIZABLE изоляцией
func (os *OrderService) saveOrderInTransaction(order models.PizzaOrder) error {
	ctx := context.Background()
	
	// Начинаем транзакцию с SERIALIZABLE изоляцией
	tx, err := os.db.BeginTx(ctx, &sql.TxOptions{
		Isolation: sql.LevelSerializable,
		ReadOnly:  false,
	})
	if err != nil {
		return fmt.Errorf("ошибка начала транзакции: %w", err)
	}
	defer tx.Rollback()

	itemsJSON, err := json.Marshal(order.Items)
	if err != nil {
		return fmt.Errorf("ошибка сериализации items: %w", err)
	}

	query := `
		INSERT INTO orders (
			id, display_id, customer_id, customer_first_name, customer_last_name,
			customer_phone, delivery_address, payment_method, is_pickup, pickup_location_id,
			call_before_minutes, items, is_set, set_name, total_price, discount_amount,
			discount_percent, final_price, notes, status, created_at, updated_at,
			target_slot_id, target_slot_start_time, visible_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25
		)
		ON CONFLICT (id) DO UPDATE SET
			status = EXCLUDED.status,
			updated_at = NOW(),
			completed_at = CASE WHEN EXCLUDED.status = 'delivered' THEN NOW() ELSE orders.completed_at END,
			cancelled_at = CASE WHEN EXCLUDED.status = 'cancelled' THEN NOW() ELSE orders.cancelled_at END
	`

	_, err = tx.ExecContext(ctx, query,
		order.ID, order.DisplayID, order.CustomerID, order.CustomerFirstName, order.CustomerLastName,
		order.CustomerPhone, order.DeliveryAddress, order.PaymentMethod, order.IsPickup, order.PickupLocationID,
		order.CallBeforeMinutes, itemsJSON, order.IsSet, order.SetName, order.TotalPrice,
		order.DiscountAmount, order.DiscountPercent, order.FinalPrice, order.Notes, order.Status,
		order.CreatedAt, time.Now(), order.TargetSlotID, order.TargetSlotStartTime, order.VisibleAt,
	)

	if err != nil {
		return fmt.Errorf("ошибка выполнения INSERT: %w", err)
	}

	// Commit транзакции
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("ошибка commit транзакции: %w", err)
	}

	return nil
}

// isSerializationFailure проверяет, является ли ошибка serialization failure
func isSerializationFailure(err error) bool {
	if err == nil {
		return false
	}

	// PostgreSQL error codes:
	// 40001 - serialization_failure
	// 40P01 - deadlock_detected
	if pgErr, ok := err.(*pq.Error); ok {
		return pgErr.Code == "40001" || pgErr.Code == "40P01"
	}

	// Check error message as fallback
	errMsg := strings.ToLower(err.Error())
	return strings.Contains(errMsg, "serialization") || 
		   strings.Contains(errMsg, "deadlock") ||
		   strings.Contains(errMsg, "could not serialize")
}

// UpdateOrderStatus обновляет статус заказа в PostgreSQL
func (os *OrderService) UpdateOrderStatus(orderID string, status string) error {
	if os.db == nil {
		return fmt.Errorf("database connection not available")
	}

	query := `
		UPDATE orders
		SET status = $1, updated_at = NOW(),
			completed_at = CASE WHEN $1 = 'delivered' THEN NOW() ELSE completed_at END,
			cancelled_at = CASE WHEN $1 = 'cancelled' THEN NOW() ELSE cancelled_at END
		WHERE id = $2
	`

	_, err := os.db.Exec(query, status, orderID)
	if err != nil {
		return fmt.Errorf("ошибка обновления статуса заказа: %w", err)
	}

	return nil
}

