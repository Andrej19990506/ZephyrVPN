package api

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
	"google.golang.org/protobuf/proto"
	"zephyrvpn/server/internal/models"
	"zephyrvpn/server/internal/pb"
	"zephyrvpn/server/internal/utils"
)

// KitchenWorkerPool управляет воркерами-поварами
type KitchenWorkerPool struct {
	redisUtil    *utils.RedisClient
	workers      map[int]*Worker
	workerID     int64
	mu           sync.RWMutex
	queueName    string
	activeCount  int64
	totalCooked  int64
	stopChan     chan struct{}
}

// Worker представляет одного воркера-повара
type Worker struct {
	ID         int
	IsActive   bool
	CurrentOrder *models.PizzaOrder
	CookedCount int64
	stopChan   chan struct{}
}

// NewKitchenWorkerPool создает новый пул воркеров
func NewKitchenWorkerPool(redisUtil *utils.RedisClient) *KitchenWorkerPool {
	return &KitchenWorkerPool{
		redisUtil: redisUtil,
		workers:   make(map[int]*Worker),
		queueName: "erp:orders:list",
		stopChan:  make(chan struct{}),
	}
}

// StartWorker запускает одного воркера-повара (публичный метод с блокировкой)
func (kwp *KitchenWorkerPool) StartWorker() int {
	kwp.mu.Lock()
	defer kwp.mu.Unlock()
	return kwp.startWorkerUnlocked()
}

// startWorkerUnlocked внутренний метод без блокировки мьютекса
func (kwp *KitchenWorkerPool) startWorkerUnlocked() int {
	id := int(atomic.AddInt64(&kwp.workerID, 1))
	worker := &Worker{
		ID:       id,
		IsActive: true,
		stopChan: make(chan struct{}),
	}
	kwp.workers[id] = worker

	// Запускаем горутину воркера
	go kwp.workerLoop(worker)

	atomic.AddInt64(&kwp.activeCount, 1)
	log.Printf("👨‍🍳 Повар #%d начал работу", id)
	return id
}

// StopWorker останавливает воркера по ID (публичный метод с блокировкой)
func (kwp *KitchenWorkerPool) StopWorker(workerID int) bool {
	kwp.mu.Lock()
	defer kwp.mu.Unlock()
	return kwp.stopWorkerUnlocked(workerID)
}

// stopWorkerUnlocked внутренний метод без блокировки мьютекса
func (kwp *KitchenWorkerPool) stopWorkerUnlocked(workerID int) bool {
	worker, exists := kwp.workers[workerID]
	if !exists || !worker.IsActive {
		return false
	}

	close(worker.stopChan)
	worker.IsActive = false
	delete(kwp.workers, workerID)
	atomic.AddInt64(&kwp.activeCount, -1)
	log.Printf("👨‍🍳 Повар #%d закончил работу", workerID)
	return true
}

// workerLoop основной цикл воркера - блокирующее получение заказов через BRPOP
// Остановка реализована через select и канал stopChan
// BRPOP с таймаутом 2 секунды - воркер периодически "просыпается" и проверяет stopChan
func (kwp *KitchenWorkerPool) workerLoop(worker *Worker) {
	
	for {
		// Проверяем stopChan перед ожиданием заказа
		select {
		case <-worker.stopChan:
			return
		default:
		}

		// BRPOP с таймаутом 2 секунды - эффективно ждем заказы и периодически проверяем stopChan
		// Используем горутину для неблокирующей проверки stopChan во время BRPOP
		type brpopResult struct {
			orderID string
			err     error
		}
		resultChan := make(chan brpopResult, 1)
		
		go func() {
			// BRPOP блокирует максимум 2 секунды, затем вернется (таймаут или заказ)
			orderID, err := kwp.redisUtil.BRPop(kwp.queueName, 2*time.Second)
			resultChan <- brpopResult{orderID: orderID, err: err}
		}()

		// Ждем либо результат BRPOP, либо сигнал остановки
		select {
		case <-worker.stopChan:
			return
		case result := <-resultChan:
			if result.err != nil {
				// Проверяем тип ошибки: таймаут (redis.Nil) или реальная ошибка
				if result.err == redis.Nil {
					// Таймаут - это нормально, просто нет заказов
					// Продолжаем цикл, проверим stopChan на следующей итерации
					continue
				}
				// Реальная ошибка Redis - логируем и продолжаем
				log.Printf("⚠️ Повар #%d: ошибка BRPop из очереди %s: %v", worker.ID, kwp.queueName, result.err)
				// Небольшая задержка перед повтором, чтобы не спамить логи
				continue
			}

			orderID := result.orderID

			// Получаем заказ из Redis (поддержка Protobuf и JSON)
			orderKey := fmt.Sprintf("erp:order:%s", orderID)
			orderBytes, err := kwp.redisUtil.GetBytes(orderKey)
			if err != nil {
				log.Printf("❌ Повар #%d: не удалось получить заказ %s: %v", worker.ID, orderID, err)
				continue
			}

			var order models.PizzaOrder
			// Пробуем сначала Protobuf (быстрее!)
			pbOrder := &pb.PizzaOrder{}
			if err := proto.Unmarshal(orderBytes, pbOrder); err == nil {
				// Успешно распарсили Protobuf - конвертируем в models.PizzaOrder
				order = models.PizzaOrder{
					ID:               pbOrder.Id,
					DisplayID:        pbOrder.DisplayId,
					CustomerID:       int(pbOrder.CustomerId),
					CustomerFirstName: pbOrder.CustomerFirstName,
					CustomerLastName:  pbOrder.CustomerLastName,
					CustomerPhone:     pbOrder.CustomerPhone,
					DeliveryAddress:   pbOrder.DeliveryAddress,
					IsPickup:          pbOrder.IsPickup,
					PickupLocationID:  pbOrder.PickupLocationId,
					TotalPrice:        int(pbOrder.TotalPrice),
					CreatedAt:         time.Unix(0, pbOrder.CreatedAt),
					Status:            pbOrder.Status,
					IsSet:             pbOrder.IsSet,
					SetName:           pbOrder.SetName,
				}
				// Конвертируем Items если есть
				for _, pbItem := range pbOrder.Items {
					item := models.PizzaItem{
						PizzaName:   pbItem.PizzaName,
						Ingredients: pbItem.Ingredients,
						Extras:      pbItem.Extras,
						Quantity:    int(pbItem.Quantity),
						Price:       int(pbItem.Price),
					}
					// Конвертируем дозировки из protobuf (map[string]int32 -> map[string]int)
					if pbItem.IngredientAmounts != nil && len(pbItem.IngredientAmounts) > 0 {
						item.IngredientAmounts = make(map[string]int)
						for k, v := range pbItem.IngredientAmounts {
							item.IngredientAmounts[k] = int(v)
						}
					} else {
						// Если дозировок нет в protobuf, берем из модели пиццы
						if pizza, exists := models.GetPizza(pbItem.PizzaName); exists && pizza.IngredientAmounts != nil {
							item.IngredientAmounts = pizza.IngredientAmounts
						}
					}
					order.Items = append(order.Items, item)
				}
			} else {
				// Fallback на JSON для обратной совместимости
				if err := json.Unmarshal(orderBytes, &order); err != nil {
					log.Printf("❌ Повар #%d: ошибка парсинга заказа %s (ни Protobuf, ни JSON): %v", worker.ID, orderID, err)
					continue
				}
			}

			// Проверяем stopChan перед началом готовки
			select {
			case <-worker.stopChan:
				log.Printf("🛑 Повар #%d получил сигнал остановки перед готовкой заказа %s", worker.ID, orderID)
				// Возвращаем заказ в очередь
				kwp.redisUtil.LPush(kwp.queueName, orderID)
				return
			default:
			}

			// Обновляем статус заказа
			worker.CurrentOrder = &order
			order.Status = "preparing"
			kwp.updateOrderStatus(&order)
			
			// Отправляем заказ на планшеты поваров через WebSocket
			orderJSON, _ := json.Marshal(order)
			GlobalHub.BroadcastMessage(orderJSON)


			// "Готовим" пиццу (симуляция времени готовки)
			// Во время готовки также проверяем stopChan периодически
			cookingTime := kwp.calculateCookingTime(&order)
			if !kwp.sleepWithStopCheck(cookingTime, worker.stopChan) {
				// Получен сигнал остановки во время готовки
				log.Printf("🛑 Повар #%d получил сигнал остановки во время готовки заказа %s", worker.ID, orderID)
				order.Status = "pending" // Возвращаем в pending
				kwp.updateOrderStatus(&order)
				// Возвращаем заказ в очередь
				kwp.redisUtil.LPush(kwp.queueName, orderID)
				worker.CurrentOrder = nil
				return
			}

			// Заказ готов
			order.Status = "ready"
			kwp.updateOrderStatus(&order)
			atomic.AddInt64(&worker.CookedCount, 1)
			atomic.AddInt64(&kwp.totalCooked, 1)

			worker.CurrentOrder = nil


		}
	}
}

// sleepWithStopCheck делает sleep с периодической проверкой stopChan
// Разбивает длинный sleep на короткие интервалы (500мс) для быстрой реакции на остановку
// Возвращает false если получен сигнал остановки
func (kwp *KitchenWorkerPool) sleepWithStopCheck(duration time.Duration, stopChan chan struct{}) bool {
	checkInterval := 500 * time.Millisecond // Проверяем каждые 500мс
	elapsed := time.Duration(0)
	
	for elapsed < duration {
		// Спим небольшими порциями, чтобы быстро реагировать на stopChan
		sleepTime := checkInterval
		if remaining := duration - elapsed; remaining < sleepTime {
			sleepTime = remaining
		}
		
		select {
		case <-stopChan:
			return false // Получен сигнал остановки
		case <-time.After(sleepTime):
			elapsed += sleepTime
		}
	}
	return true // Время вышло, сигнала остановки не было
}

// calculateCookingTime вычисляет время готовки на основе количества пицц
func (kwp *KitchenWorkerPool) calculateCookingTime(order *models.PizzaOrder) time.Duration {
	totalPizzas := 0
	for _, item := range order.Items {
		totalPizzas += item.Quantity
	}
	// Базовая готовка: 2 секунды на пиццу, минимум 3 секунды
	cookingTime := time.Duration(totalPizzas*2) * time.Second
	if cookingTime < 3*time.Second {
		cookingTime = 3 * time.Second
	}
	// Максимум 10 секунд на заказ
	if cookingTime > 10*time.Second {
		cookingTime = 10 * time.Second
	}
	return cookingTime
}

func (kwp *KitchenWorkerPool) updateOrderStatus(order *models.PizzaOrder) {
	if kwp.redisUtil == nil {
		return
	}
	
	// 1. Сохраняем в ОБА ключа (для надежности и для ERP)
	orderJSON, _ := json.Marshal(order)
	kwp.redisUtil.Set(fmt.Sprintf("order:%s", order.ID), string(orderJSON), 24*time.Hour)
	kwp.redisUtil.Set(fmt.Sprintf("erp:order:%s", order.ID), string(orderJSON), 24*time.Hour)

	// 2. Если статус стал "ready", отмечаем в статистике ERP и удаляем из Redis
	if order.Status == "ready" {
		kwp.redisUtil.SAdd("erp:processed:set", order.ID)
		kwp.redisUtil.Decrement("erp:orders:pending")
		kwp.redisUtil.Increment("erp:orders:processed")
		
		// Удаляем заказ из Redis после обработки (источник истины - Kafka)
		kwp.redisUtil.Delete(fmt.Sprintf("erp:order:%s", order.ID))
		kwp.redisUtil.Delete(fmt.Sprintf("order:%s", order.ID))
		kwp.redisUtil.SRem("erp:orders:active", order.ID)
	}
}

// SetWorkerCount устанавливает количество активных воркеров
func (kwp *KitchenWorkerPool) SetWorkerCount(count int) {
	kwp.mu.Lock()
	defer kwp.mu.Unlock()

	currentCount := len(kwp.workers)

	if count > currentCount {
		// Добавляем воркеров (используем внутренний метод без блокировки)
		for i := 0; i < count-currentCount; i++ {
			kwp.startWorkerUnlocked()
		}
	} else if count < currentCount {
		// Удаляем воркеров (останавливаем последних, используем внутренний метод)
		stopped := 0
		for id := range kwp.workers {
			if stopped >= currentCount-count {
				break
			}
			if kwp.stopWorkerUnlocked(id) {
				stopped++
			}
		}
	}
}

// GetStats возвращает статистику воркеров
func (kwp *KitchenWorkerPool) GetStats() map[string]interface{} {
	kwp.mu.RLock()
	defer kwp.mu.RUnlock()

	workersInfo := make([]map[string]interface{}, 0)
	for _, worker := range kwp.workers {
		var currentOrderID string
		if worker.CurrentOrder != nil {
			currentOrderID = worker.CurrentOrder.ID
		}
		workersInfo = append(workersInfo, map[string]interface{}{
			"id":           worker.ID,
			"is_active":    worker.IsActive,
			"current_order": currentOrderID,
			"cooked_count": atomic.LoadInt64(&worker.CookedCount),
		})
	}

	queueLength := int64(0)
	if kwp.redisUtil != nil {
		queueLength, _ = kwp.redisUtil.LLen(kwp.queueName)
	}

	return map[string]interface{}{
		"active_workers": atomic.LoadInt64(&kwp.activeCount),
		"total_cooked":   atomic.LoadInt64(&kwp.totalCooked),
		"queue_length":   queueLength,
		"workers":        workersInfo,
	}
}

// StopAll останавливает всех воркеров
func (kwp *KitchenWorkerPool) StopAll() {
	kwp.mu.Lock()
	defer kwp.mu.Unlock()

	for id := range kwp.workers {
		kwp.stopWorkerUnlocked(id)
	}
}

