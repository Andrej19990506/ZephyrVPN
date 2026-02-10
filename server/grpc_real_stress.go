package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"zephyrvpn/server/internal/pb"
)

var (
	// Базовые метрики
	totalRequests   int64
	successRequests int64
	failedRequests  int64
	startTime       time.Time

	// Метрики для поиска race conditions
	overflowAttempts   int64 // Попытки заказать в переполненный слот
	raceConditionHits  int64 // Обнаруженные race conditions
	slotOverflows      int64 // Фактические переполнения слотов
	concurrentRequests int64 // Текущее количество параллельных запросов
	resourceExhaustedErrors int64 // Количество ошибок "All slots are full"

	// Синхронизация
	slotMutex sync.RWMutex
	currentSlots map[string]*SlotInfo // slotID -> SlotInfo
	
	// Детектор race conditions: отслеживаем состояние слотов до и после запроса
	slotStateBefore map[string]int // slotID -> CurrentLoad до запроса
	slotStateAfter  map[string]int // slotID -> CurrentLoad после запроса
	stateMutex      sync.Mutex
	
	// Флаг для остановки всех горутин
	stopWorkers     int32 // Атомарный флаг для остановки
	allSlotsFull    int32 // Флаг, что все слоты заполнены
)

// SlotInfo информация о слоте
type SlotInfo struct {
	SlotID      string `json:"slot_id"`
	StartTime   string `json:"start_time"`
	EndTime     string `json:"end_time"`
	CurrentLoad int    `json:"current_load"`
	MaxCapacity int    `json:"max_capacity"`
}

func main() {
	// Адреса серверов
	grpcAddr := "host.docker.internal:50051"
	httpAddr := "http://host.docker.internal:8080"

	fmt.Printf("🔥 ЗАПУСК АГРЕССИВНОГО СТРЕСС-ТЕСТА (True HighLoad)\n")
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	fmt.Printf("🔌 Подключение к gRPC серверу %s...\n", grpcAddr)

	// Пробуем подключиться с таймаутом
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := grpc.DialContext(ctx, grpcAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(), // Блокируем до подключения
	)
	if err != nil {
		log.Fatalf("❌ Не удалось подключиться к gRPC серверу %s: %v\n💡 Убедись, что сервер запущен и слушает на порту 50051", grpcAddr, err)
	}
	defer conn.Close()

	// Проверяем подключение тестовым запросом
	client := pb.NewOrderServiceClient(conn)
	testCtx, testCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer testCancel()

	_, testErr := client.CreateOrder(testCtx, &pb.PizzaOrderRequest{
		CustomerId: 0,
		PizzaName:  "test",
		Quantity:   1,
	})
	if testErr != nil && testErr.Error() != "rpc error: code = Unavailable" {
		fmt.Println("✅ Подключено к gRPC серверу, тестовый запрос выполнен")
	} else if testErr == nil {
		fmt.Println("✅ Подключено к gRPC серверу, тестовый запрос успешен")
	} else {
		fmt.Printf("⚠️ Подключено, но тестовый запрос не прошел: %v\n", testErr)
	}

	startTime = time.Now()
	currentSlots = make(map[string]*SlotInfo)
	slotStateBefore = make(map[string]int)
	slotStateAfter = make(map[string]int)

	// Инициализируем генератор случайных чисел
	rand.Seed(time.Now().UnixNano())

	// Получаем список доступных пицц через HTTP API (как реальный клиент)
	pizzaData, err := loadPizzasFromAPI(httpAddr)
	if err != nil {
		log.Fatalf("❌ Не удалось загрузить меню пицц через API: %v\n💡 Убедись, что сервер запущен на %s", err, httpAddr)
	}

	pizzaNames := make([]string, 0, len(pizzaData))
	for name := range pizzaData {
		pizzaNames = append(pizzaNames, name)
	}

	fmt.Printf("\n🚀 НАСТРОЙКИ СТРЕСС-ТЕСТА\n")
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	fmt.Printf("📊 Режим: Агрессивная нагрузка (True HighLoad)\n")
	fmt.Printf("👥 Количество горутин: 500+\n")
	fmt.Printf("⏱️  Паузы: Хаотичные (0-50ms)\n")
	fmt.Printf("🎲 Выбор слотов: Случайный (имитация реального спроса)\n")
	fmt.Printf("🎯 Цель: Найти Race Conditions и сломать логику переполнения\n")
	fmt.Printf("🍕 Доступно пицц: %d\n", len(pizzaNames))
	fmt.Printf("🌐 HTTP API: %s\n", httpAddr)
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n\n")

	// Запускаем мониторинг слотов каждые 2 секунды (чаще, чем в старом тесте)
	slotsStop := make(chan bool)
	var slotsWg sync.WaitGroup
	slotsWg.Add(1)
	go func() {
		defer slotsWg.Done()
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-slotsStop:
				return
			case <-ticker.C:
				updateSlotsInfo(httpAddr)
				detectOverflows()
			}
		}
	}()

	// Запускаем сбор статистики каждые 3 секунды (чаще, чем в старом тесте)
	statsStop := make(chan bool)
	var statsWg sync.WaitGroup
	statsWg.Add(1)
	go func() {
		defer statsWg.Done()
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-statsStop:
				return
			case <-ticker.C:
				printStats()
				printSlotsStats()
				printRaceConditionStats()
			}
		}
	}()

	// Запускаем стресс-тест: агрессивная нагрузка
	testDuration := 1 * time.Hour // 1 час теста
	stop := time.After(testDuration)

	// Запускаем 500+ горутин для агрессивной нагрузки
	numWorkers := 500
	fmt.Printf("🔥 Запуск %d горутин-клиентов...\n\n", numWorkers)
	
	// Запускаем горутины и ждем их завершения
	var workersWg sync.WaitGroup
	workersWg.Add(numWorkers)
	fillSlotsWorker(client, pizzaData, httpAddr, numWorkers, &workersWg)

	// Ждем либо завершения времени, либо остановки из-за заполненных слотов
	select {
	case <-stop:
		fmt.Println("\n⏹️  Время теста истекло, завершаем...")
		atomic.StoreInt32(&stopWorkers, 1)
	case <-time.After(1 * time.Second):
		// Проверяем флаг остановки каждую секунду
		for atomic.LoadInt32(&stopWorkers) == 0 {
			time.Sleep(1 * time.Second)
		}
		fmt.Println("\n⏹️  Все слоты заполнены, завершаем тест...")
	}

	// Ждем завершения всех горутин
	fmt.Println("⏳ Ожидание завершения всех горутин...")
	workersWg.Wait()
	
	// Останавливаем мониторинг
	close(slotsStop)
	close(statsStop)
	slotsWg.Wait()
	statsWg.Wait()
	
	// Выводим финальную статистику
	printFinalStats()
	printSlotsStats()
	printRaceConditionStats()
	printDetailedAnalysis()
}

// loadPizzasFromAPI загружает список доступных пицц через HTTP API (как реальный клиент)
func loadPizzasFromAPI(httpAddr string) (map[string]int, error) {
	url := fmt.Sprintf("%s/api/v1/menu/pizzas", httpAddr)
	
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("ошибка HTTP запроса: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP статус: %d", resp.StatusCode)
	}
	
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("ошибка чтения ответа: %w", err)
	}
	
	var menuResponse struct {
		Pizzas map[string]struct {
			Name  string `json:"name"`
			Price int    `json:"price"`
		} `json:"pizzas"`
	}
	
	if err := json.Unmarshal(body, &menuResponse); err != nil {
		return nil, fmt.Errorf("ошибка парсинга JSON: %w", err)
	}
	
	pizzaData := make(map[string]int)
	for name, pizza := range menuResponse.Pizzas {
		pizzaData[name] = pizza.Price
	}
	
	if len(pizzaData) == 0 {
		return nil, fmt.Errorf("меню пустое - нет доступных пицц")
	}
	
	fmt.Printf("✅ Загружено %d пицц из меню через API\n", len(pizzaData))
	return pizzaData, nil
}

// updateSlotsInfo получает актуальную информацию о слотах через HTTP API
func updateSlotsInfo(httpAddr string) {
	url := fmt.Sprintf("%s/api/v1/erp/slots", httpAddr)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return // Тихая ошибка, не логируем
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return
	}

	var slotsResponse struct {
		Slots []SlotInfo `json:"slots"`
		Count int        `json:"count"`
	}

	if err := json.Unmarshal(body, &slotsResponse); err != nil {
		return
	}

	slotMutex.Lock()
	defer slotMutex.Unlock()

	// Сохраняем предыдущее состояние для детекции race conditions
	stateMutex.Lock()
	for slotID, slot := range currentSlots {
		slotStateBefore[slotID] = slot.CurrentLoad
	}
	stateMutex.Unlock()

	// Обновляем текущее состояние
	currentSlots = make(map[string]*SlotInfo)
	for i := range slotsResponse.Slots {
		slot := &slotsResponse.Slots[i]
		currentSlots[slot.SlotID] = slot
	}

	// Сохраняем новое состояние
	stateMutex.Lock()
	for slotID, slot := range currentSlots {
		slotStateAfter[slotID] = slot.CurrentLoad
	}
	stateMutex.Unlock()
}

// detectOverflows обнаруживает переполнения слотов и проверяет, все ли слоты заполнены
func detectOverflows() {
	slotMutex.RLock()
	defer slotMutex.RUnlock()

	if len(currentSlots) == 0 {
		return
	}

	allFull := true
	hasOverflow := false
	filledSlots := 0
	totalSlots := len(currentSlots)
	
	for _, slot := range currentSlots {
		if slot.CurrentLoad > slot.MaxCapacity {
			hasOverflow = true
			atomic.AddInt64(&slotOverflows, 1)
			fmt.Printf("🚨 ПЕРЕПОЛНЕНИЕ СЛОТА! %s: %d₽ / %d₽ (превышение на %d₽)\n",
				slot.SlotID[:12], slot.CurrentLoad, slot.MaxCapacity,
				slot.CurrentLoad-slot.MaxCapacity)
		}
		
		// Проверяем, заполнен ли слот (>= 95% загрузки считается заполненным)
		loadPercent := float64(slot.CurrentLoad) / float64(slot.MaxCapacity) * 100
		if loadPercent >= 95.0 {
			filledSlots++
		} else {
			allFull = false
		}
	}

	// Если все слоты заполнены на 95%+, устанавливаем флаг
	// Или если 90%+ слотов заполнены и есть много ошибок ResourceExhausted
	if allFull && !hasOverflow {
		if atomic.CompareAndSwapInt32(&allSlotsFull, 0, 1) {
			fmt.Printf("\n🛑 ВСЕ СЛОТЫ ЗАПОЛНЕНЫ (95%%+)! Останавливаем тест...\n")
			atomic.StoreInt32(&stopWorkers, 1)
		}
	} else if filledSlots >= int(float64(totalSlots)*0.9) {
		// Если 90%+ слотов заполнены, проверяем количество ошибок ResourceExhausted
		resourceExhausted := atomic.LoadInt64(&resourceExhaustedErrors)
		if resourceExhausted > 100 { // Если больше 100 ошибок "All slots are full"
			if atomic.CompareAndSwapInt32(&allSlotsFull, 0, 1) {
				fmt.Printf("\n🛑 90%%+ СЛОТОВ ЗАПОЛНЕНЫ И МНОГО ОШИБОК ResourceExhausted (%d)! Останавливаем тест...\n", resourceExhausted)
				atomic.StoreInt32(&stopWorkers, 1)
			}
		}
	}
}

// fillSlotsWorker запускает агрессивную нагрузку с 500+ горутинами
func fillSlotsWorker(client pb.OrderServiceClient, pizzaData map[string]int, httpAddr string, numWorkers int, wg *sync.WaitGroup) {
	// Получаем список пицц с ценами
	pizzaNames := make([]string, 0, len(pizzaData))
	pizzaPrices := make([]int, 0, len(pizzaData))
	for name, price := range pizzaData {
		pizzaNames = append(pizzaNames, name)
		pizzaPrices = append(pizzaPrices, price)
	}

	if len(pizzaNames) == 0 {
		fmt.Printf("❌ Нет доступных пицц в меню!\n")
		return
	}

	// Находим самую дешевую пиццу для проверки переполнения
	cheapestPrice := pizzaPrices[0]
	for _, price := range pizzaPrices {
		if price < cheapestPrice {
			cheapestPrice = price
		}
	}

	// Запускаем 500+ горутин для агрессивной нагрузки
	for i := 0; i < numWorkers; i++ {
		go func(workerID int) {
			defer wg.Done()
			// Каждая горутина работает независимо с хаотичными паузами
			for {
				// Проверяем флаг остановки
				if atomic.LoadInt32(&stopWorkers) == 1 {
					return
				}
				// Получаем актуальную информацию о слотах (реже, чем в старом тесте)
				// Обновляем только каждые 5-10 запросов для имитации реального поведения
				if rand.Intn(10) == 0 {
					updateSlotsInfo(httpAddr)
				}

				slotMutex.RLock()
				slots := make([]*SlotInfo, 0, len(currentSlots))
				for _, slot := range currentSlots {
					slots = append(slots, slot)
				}
				slotMutex.RUnlock()

				if len(slots) == 0 {
					// Нет слотов, ждем с хаотичной паузой
					chaoticSleep(100, 2000) // 100ms - 2s
					continue
				}

				// 🎲 СЛУЧАЙНЫЙ ВЫБОР СЛОТА (имитация реального спроса)
				// НЕ выбираем по свободному месту - просто случайный слот!
				targetSlot := slots[rand.Intn(len(slots))]

				// Проверяем, не переполнен ли слот (для метрик)
				remaining := targetSlot.MaxCapacity - targetSlot.CurrentLoad

				if remaining < cheapestPrice {
					// Слот почти заполнен, но все равно пытаемся заказать (для поиска race conditions)
					atomic.AddInt64(&overflowAttempts, 1)
				}

				// Выбираем случайную пиццу и количество
				var selectedPizza string
				var quantity int32

				// Случайное количество от 1 до 5 (более агрессивно, чем в старом тесте)
				quantity = int32(rand.Intn(5) + 1)

				// Случайная пицца
				idx := rand.Intn(len(pizzaNames))
				selectedPizza = pizzaNames[idx]
				price := pizzaPrices[idx]

				// Вычисляем стоимость заказа
				totalCost := price * int(quantity)

				// НЕ проверяем, поместится ли заказ - просто отправляем!
				// Это и есть агрессивный тест для поиска race conditions

				// Отправляем заказ
				atomic.AddInt64(&totalRequests, 1)
				atomic.AddInt64(&concurrentRequests, 1)

				reqCtx, reqCancel := context.WithTimeout(context.Background(), 10*time.Second)
				_, err := client.CreateOrder(reqCtx, &pb.PizzaOrderRequest{
					CustomerId: int32(1000 + workerID), // Уникальный ID для каждого воркера
					PizzaName:  selectedPizza,
					Quantity:   quantity,
				})
				reqCancel()

				atomic.AddInt64(&concurrentRequests, -1)

				if err == nil {
					atomic.AddInt64(&successRequests, 1)

					// Детекция race condition: проверяем, не изменилось ли состояние слота
					// между моментом выбора и моментом обработки запроса
					slotMutex.RLock()
					updatedSlot, exists := currentSlots[targetSlot.SlotID]
					slotMutex.RUnlock()

					if exists {
						// Проверяем, не произошло ли неожиданное изменение загрузки
						expectedLoad := targetSlot.CurrentLoad + totalCost
						if updatedSlot.CurrentLoad != expectedLoad && updatedSlot.CurrentLoad > targetSlot.CurrentLoad {
							// Возможная race condition обнаружена
							atomic.AddInt64(&raceConditionHits, 1)
						}
					}

					// 🎲 ХАОТИЧНАЯ ПАУЗА (0-50ms вместо фиксированных 200ms)
					chaoticSleep(0, 50)
				} else {
					atomic.AddInt64(&failedRequests, 1)
					
					// Проверяем, является ли ошибка ResourceExhausted (All slots are full)
					errStr := err.Error()
					if contains(errStr, "ResourceExhausted") || contains(errStr, "All slots are full") {
						atomic.AddInt64(&resourceExhaustedErrors, 1)
						
						// Если накопилось много ошибок ResourceExhausted, останавливаем тест
						resourceExhausted := atomic.LoadInt64(&resourceExhaustedErrors)
						if resourceExhausted > 50 { // Порог: 50 ошибок
							if atomic.CompareAndSwapInt32(&allSlotsFull, 0, 1) {
								fmt.Printf("\n🛑 ОБНАРУЖЕНО МНОГО ОШИБОК 'All slots are full' (%d)! Останавливаем тест...\n", resourceExhausted)
								atomic.StoreInt32(&stopWorkers, 1)
							}
						}
					}
					
					// При ошибке ждем дольше, но тоже хаотично
					chaoticSleep(100, 1000) // 100ms - 1s
				}
			}
		}(i)
	}
}

// chaoticSleep делает хаотичную паузу в заданном диапазоне
func chaoticSleep(minMs, maxMs int) {
	if maxMs <= minMs {
		time.Sleep(time.Duration(minMs) * time.Millisecond)
		return
	}
	delay := time.Duration(rand.Intn(maxMs-minMs)+minMs) * time.Millisecond
	time.Sleep(delay)
}

// contains проверяет, содержит ли строка подстроку (case-insensitive)
func contains(s, substr string) bool {
	s = strings.ToLower(s)
	substr = strings.ToLower(substr)
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && 
		(s[:len(substr)] == substr || s[len(s)-len(substr):] == substr || 
		 contains(s[1:], substr)))
}

func printStats() {
	elapsed := time.Since(startTime).Seconds()
	if elapsed == 0 {
		return
	}

	total := atomic.LoadInt64(&totalRequests)
	success := atomic.LoadInt64(&successRequests)
	failed := atomic.LoadInt64(&failedRequests)
	concurrent := atomic.LoadInt64(&concurrentRequests)
	currentRPS := float64(total) / elapsed
	successRate := float64(0)
	if total > 0 {
		successRate = float64(success) / float64(total) * 100
	}

	hours := int(elapsed) / 3600
	minutes := (int(elapsed) % 3600) / 60
	seconds := int(elapsed) % 60

	fmt.Printf("⏱️  [%02d:%02d:%02d] Всего: %d | ✅ Успешно: %d (%.1f%%) | ❌ Ошибок: %d | 🔥 Параллельно: %d | RPS: %.1f\n",
		hours, minutes, seconds, total, success, successRate, failed, concurrent, currentRPS)
}

func printSlotsStats() {
	slotMutex.RLock()
	defer slotMutex.RUnlock()

	if len(currentSlots) == 0 {
		return
	}

	fmt.Printf("\n📊 СТАТИСТИКА СЛОТОВ:\n")
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")

	totalSlots := len(currentSlots)
	filledSlots := 0
	overflowSlots := 0
	totalLoad := 0
	totalCapacity := 0

	for _, slot := range currentSlots {
		totalLoad += slot.CurrentLoad
		totalCapacity += slot.MaxCapacity
		if slot.CurrentLoad >= slot.MaxCapacity {
			filledSlots++
		}
		if slot.CurrentLoad > slot.MaxCapacity {
			overflowSlots++
		}
	}

	avgLoad := float64(0)
	if totalCapacity > 0 {
		avgLoad = float64(totalLoad) / float64(totalCapacity) * 100
	}

	fmt.Printf("📦 Всего слотов: %d\n", totalSlots)
	fmt.Printf("✅ Заполнено полностью: %d (%.1f%%)\n", filledSlots, float64(filledSlots)/float64(totalSlots)*100)
	if overflowSlots > 0 {
		fmt.Printf("🚨 ПЕРЕПОЛНЕНО: %d слотов (%.1f%%)\n", overflowSlots, float64(overflowSlots)/float64(totalSlots)*100)
	}
	fmt.Printf("💰 Общая загрузка: %d₽ / %d₽ (%.1f%%)\n", totalLoad, totalCapacity, avgLoad)

	// Показываем топ-5 самых загруженных слотов
	fmt.Printf("\n🔝 Топ-5 самых загруженных слотов:\n")
	slotsList := make([]*SlotInfo, 0, len(currentSlots))
	for _, slot := range currentSlots {
		slotsList = append(slotsList, slot)
	}

	// Сортируем по загрузке (простая сортировка)
	for i := 0; i < len(slotsList) && i < 5; i++ {
		maxIdx := i
		for j := i + 1; j < len(slotsList); j++ {
			if slotsList[j].CurrentLoad > slotsList[maxIdx].CurrentLoad {
				maxIdx = j
			}
		}
		slotsList[i], slotsList[maxIdx] = slotsList[maxIdx], slotsList[i]

		slot := slotsList[i]
		loadPercent := float64(slot.CurrentLoad) / float64(slot.MaxCapacity) * 100
		status := "✅"
		if slot.CurrentLoad > slot.MaxCapacity {
			status = "🚨"
		} else if slot.CurrentLoad == slot.MaxCapacity {
			status = "⚠️"
		}
		fmt.Printf("  %d. %s %s: %d₽ / %d₽ (%.1f%%)\n",
			i+1, status, slot.SlotID[:12], slot.CurrentLoad, slot.MaxCapacity, loadPercent)
	}
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
}

func printRaceConditionStats() {
	overflowAttempts := atomic.LoadInt64(&overflowAttempts)
	raceConditionHits := atomic.LoadInt64(&raceConditionHits)
	slotOverflows := atomic.LoadInt64(&slotOverflows)
	resourceExhausted := atomic.LoadInt64(&resourceExhaustedErrors)

	if overflowAttempts > 0 || raceConditionHits > 0 || slotOverflows > 0 || resourceExhausted > 0 {
		fmt.Printf("\n🔍 ДЕТЕКЦИЯ RACE CONDITIONS:\n")
		fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
		fmt.Printf("⚠️  Попыток заказа в переполненный слот: %d\n", overflowAttempts)
		fmt.Printf("🔴 Обнаружено race conditions: %d\n", raceConditionHits)
		fmt.Printf("🚨 Фактических переполнений слотов: %d\n", slotOverflows)
		if resourceExhausted > 0 {
			fmt.Printf("🛑 Ошибок 'All slots are full': %d\n", resourceExhausted)
		}
		fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	}
}

func printFinalStats() {
	duration := time.Since(startTime)
	total := atomic.LoadInt64(&totalRequests)
	success := atomic.LoadInt64(&successRequests)
	failed := atomic.LoadInt64(&failedRequests)
	overflowAttempts := atomic.LoadInt64(&overflowAttempts)
	raceConditionHits := atomic.LoadInt64(&raceConditionHits)
	slotOverflows := atomic.LoadInt64(&slotOverflows)
	resourceExhausted := atomic.LoadInt64(&resourceExhaustedErrors)
	rps := float64(0)
	if duration.Seconds() > 0 {
		rps = float64(total) / duration.Seconds()
	}
	successRate := float64(0)
	if total > 0 {
		successRate = float64(success) / float64(total) * 100
	}

	stopReason := "Время истекло"
	if atomic.LoadInt32(&allSlotsFull) == 1 {
		if resourceExhausted > 50 {
			stopReason = fmt.Sprintf("Все слоты заполнены (ошибок ResourceExhausted: %d)", resourceExhausted)
		} else {
			stopReason = "Все слоты заполнены (95%+ загрузки)"
		}
	}

	fmt.Printf("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	fmt.Printf("🏁 АГРЕССИВНЫЙ СТРЕСС-ТЕСТ ОКОНЧЕН\n")
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	fmt.Printf("🛑 Причина остановки: %s\n", stopReason)
	fmt.Printf("⏱️  Время работы: %v (%.2f секунд)\n", duration, duration.Seconds())
	fmt.Printf("📈 Всего заказов отправлено: %d\n", total)
	fmt.Printf("✅ Успешных: %d (%.2f%%)\n", success, successRate)
	fmt.Printf("❌ Ошибок: %d (%.2f%%)\n", failed, 100-successRate)
	fmt.Printf("⚡ Средний RPS: %.2f заказов/сек\n", rps)
	fmt.Printf("\n🔍 РЕЗУЛЬТАТЫ ПОИСКА RACE CONDITIONS:\n")
	fmt.Printf("⚠️  Попыток заказа в переполненный слот: %d\n", overflowAttempts)
	fmt.Printf("🔴 Обнаружено race conditions: %d\n", raceConditionHits)
	fmt.Printf("🚨 Фактических переполнений слотов: %d\n", slotOverflows)
	if resourceExhausted > 0 {
		fmt.Printf("🛑 Ошибок 'All slots are full': %d\n", resourceExhausted)
	}
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
}

// printDetailedAnalysis выводит детальный анализ результатов теста
func printDetailedAnalysis() {
	slotMutex.RLock()
	defer slotMutex.RUnlock()

	fmt.Printf("\n📊 ДЕТАЛЬНЫЙ АНАЛИЗ РЕЗУЛЬТАТОВ:\n")
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")

	if len(currentSlots) == 0 {
		fmt.Printf("⚠️  Нет данных о слотах для анализа\n")
		return
	}

	// Статистика по слотам
	totalSlots := len(currentSlots)
	filledSlots := 0
	overflowSlots := 0
	nearlyFullSlots := 0 // 90-95% загрузки
	emptySlots := 0      // < 10% загрузки
	totalLoad := 0
	totalCapacity := 0
	maxLoad := 0
	maxLoadSlotID := ""
	minLoad := int(^uint(0) >> 1)
	minLoadSlotID := ""

	for _, slot := range currentSlots {
		totalLoad += slot.CurrentLoad
		totalCapacity += slot.MaxCapacity
		
		loadPercent := float64(slot.CurrentLoad) / float64(slot.MaxCapacity) * 100
		
		if slot.CurrentLoad >= slot.MaxCapacity {
			filledSlots++
		}
		if slot.CurrentLoad > slot.MaxCapacity {
			overflowSlots++
		}
		if loadPercent >= 90 && loadPercent < 95 {
			nearlyFullSlots++
		}
		if loadPercent < 10 {
			emptySlots++
		}
		
		if slot.CurrentLoad > maxLoad {
			maxLoad = slot.CurrentLoad
			maxLoadSlotID = slot.SlotID
		}
		if slot.CurrentLoad < minLoad {
			minLoad = slot.CurrentLoad
			minLoadSlotID = slot.SlotID
		}
	}

	avgLoad := float64(0)
	if totalCapacity > 0 {
		avgLoad = float64(totalLoad) / float64(totalCapacity) * 100
	}

	fmt.Printf("📦 Статистика слотов:\n")
	fmt.Printf("   • Всего слотов: %d\n", totalSlots)
	fmt.Printf("   • Полностью заполнено: %d (%.1f%%)\n", filledSlots, float64(filledSlots)/float64(totalSlots)*100)
	fmt.Printf("   • Переполнено: %d (%.1f%%)\n", overflowSlots, float64(overflowSlots)/float64(totalSlots)*100)
	fmt.Printf("   • Почти заполнено (90-95%%): %d (%.1f%%)\n", nearlyFullSlots, float64(nearlyFullSlots)/float64(totalSlots)*100)
	fmt.Printf("   • Пустых (< 10%%): %d (%.1f%%)\n", emptySlots, float64(emptySlots)/float64(totalSlots)*100)
	fmt.Printf("\n💰 Загрузка:\n")
	fmt.Printf("   • Общая загрузка: %d₽ / %d₽ (%.1f%%)\n", totalLoad, totalCapacity, avgLoad)
	fmt.Printf("   • Максимальная загрузка: %d₽ (слот: %s)\n", maxLoad, maxLoadSlotID[:12])
	fmt.Printf("   • Минимальная загрузка: %d₽ (слот: %s)\n", minLoad, minLoadSlotID[:12])

	// Анализ производительности
	duration := time.Since(startTime)
	total := atomic.LoadInt64(&totalRequests)
	success := atomic.LoadInt64(&successRequests)
	
	if duration.Seconds() > 0 {
		rps := float64(total) / duration.Seconds()
		successRPS := float64(success) / duration.Seconds()
		
		fmt.Printf("\n⚡ Производительность:\n")
		fmt.Printf("   • Средний RPS: %.2f запросов/сек\n", rps)
		fmt.Printf("   • Успешный RPS: %.2f заказов/сек\n", successRPS)
		fmt.Printf("   • Время на заказ: %.2f мс\n", duration.Seconds()/float64(total)*1000)
	}

	// Анализ ошибок
	failed := atomic.LoadInt64(&failedRequests)
	overflowAttempts := atomic.LoadInt64(&overflowAttempts)
	
	if failed > 0 {
		fmt.Printf("\n❌ Анализ ошибок:\n")
		fmt.Printf("   • Всего ошибок: %d (%.1f%% от всех запросов)\n", failed, float64(failed)/float64(total)*100)
		fmt.Printf("   • Попыток заказа в переполненный слот: %d\n", overflowAttempts)
		if overflowAttempts > 0 {
			fmt.Printf("   • Процент попыток переполнения: %.1f%%\n", float64(overflowAttempts)/float64(total)*100)
		}
	}

	// Выводы и рекомендации
	raceConditionHits := atomic.LoadInt64(&raceConditionHits)
	successRate := float64(0)
	if total > 0 {
		successRate = float64(success) / float64(total) * 100
	}
	
	fmt.Printf("\n💡 Выводы:\n")
	if overflowSlots > 0 {
		fmt.Printf("   ⚠️  Обнаружены переполнения слотов - требуется проверка логики capacity\n")
	}
	if avgLoad > 95 {
		fmt.Printf("   ✅ Система достигла максимальной загрузки - все слоты заполнены\n")
	}
	if raceConditionHits > 0 {
		fmt.Printf("   🔴 Обнаружены race conditions (%d) - требуется использование транзакций\n", raceConditionHits)
	}
	if successRate > 95 {
		fmt.Printf("   ✅ Высокий процент успешных заказов (%.1f%%)\n", successRate)
	} else if successRate < 80 {
		fmt.Printf("   ⚠️  Низкий процент успешных заказов (%.1f%%) - требуется оптимизация\n", successRate)
	}
	if duration.Seconds() > 0 {
		rps := float64(total) / duration.Seconds()
		if rps > 500 {
			fmt.Printf("   ✅ Высокая производительность: %.1f RPS\n", rps)
		}
	}

	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
}

