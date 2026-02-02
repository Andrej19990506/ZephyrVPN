package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"zephyrvpn/server/internal/models"
	"zephyrvpn/server/internal/pb"
)

var (
	totalRequests   int64
	successRequests int64
	failedRequests  int64
	startTime       time.Time
	
	slotMutex sync.RWMutex
	currentSlots map[string]*SlotInfo // slotID -> SlotInfo
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
	
	// Инициализируем генератор случайных чисел
	rand.Seed(time.Now().UnixNano())
	
	// Получаем список доступных пицц из модели с ценами
	pizzaData := make(map[string]int) // name -> price
	for name, pizza := range models.AvailablePizzas {
		pizzaData[name] = pizza.Price
	}
	
	pizzaNames := make([]string, 0, len(pizzaData))
	for name := range pizzaData {
		pizzaNames = append(pizzaNames, name)
	}
	
	fmt.Printf("\n🚀 ЗАПУСК СТРЕСС-ТЕСТА СИСТЕМЫ СЛОТОВ\n")
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	fmt.Printf("📊 Режим: Заполнение слотов до максимальной емкости\n")
	fmt.Printf("🍕 Доступно пицц: %d\n", len(pizzaNames))
	fmt.Printf("🌐 HTTP API: %s\n", httpAddr)
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n\n")

	// Запускаем мониторинг слотов каждые 3 секунды
	slotsStop := make(chan bool)
	var slotsWg sync.WaitGroup
	slotsWg.Add(1)
	go func() {
		defer slotsWg.Done()
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-slotsStop:
				return
			case <-ticker.C:
				updateSlotsInfo(httpAddr)
			}
		}
	}()

	// Запускаем сбор статистики каждые 5 секунд
	statsStop := make(chan bool)
	var statsWg sync.WaitGroup
	statsWg.Add(1)
	go func() {
		defer statsWg.Done()
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-statsStop:
				return
			case <-ticker.C:
				printStats()
				printSlotsStats()
			}
		}
	}()

	// Запускаем стресс-тест: заполняем слоты
	testDuration := 1 * time.Hour // 1 час теста
	stop := time.After(testDuration)
	
	// Запускаем заполнение слотов
	fillSlotsWorker(client, pizzaData, httpAddr)
	
	// Ждем завершения теста
	<-stop
	fmt.Println("\n⏹️  Время теста истекло, завершаем...")
	close(slotsStop)
	close(statsStop)
	slotsWg.Wait()
	statsWg.Wait()
	printFinalStats()
	printSlotsStats()
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
	
	currentSlots = make(map[string]*SlotInfo)
	for i := range slotsResponse.Slots {
		slot := &slotsResponse.Slots[i]
		currentSlots[slot.SlotID] = slot
	}
}

// fillSlotsWorker заполняет слоты заказами до максимальной емкости
func fillSlotsWorker(client pb.OrderServiceClient, pizzaData map[string]int, httpAddr string) {
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
	
	// Находим самую дешевую пиццу для fallback
	cheapestPizza := pizzaNames[0]
	cheapestPrice := pizzaPrices[0]
	for i, price := range pizzaPrices {
		if price < cheapestPrice {
			cheapestPrice = price
			cheapestPizza = pizzaNames[i]
		}
	}
	
	// Запускаем воркеры для заполнения слотов (уменьшили до 2 для избежания race condition)
	for i := 0; i < 2; i++ {
		go func(workerID int) {
			for {
				// Получаем актуальную информацию о слотах перед каждым заказом
				updateSlotsInfo(httpAddr)
				
				slotMutex.RLock()
				slots := make([]*SlotInfo, 0, len(currentSlots))
				for _, slot := range currentSlots {
					slots = append(slots, slot)
				}
				slotMutex.RUnlock()
				
				if len(slots) == 0 {
					// Нет слотов, ждем
					time.Sleep(2 * time.Second)
					continue
				}
				
				// Находим слот с наибольшим свободным местом
				// НО: выбираем только слоты, заполненные менее чем на 80% (защита от race condition)
				var targetSlot *SlotInfo
				maxRemaining := 0
				for _, slot := range slots {
					loadPercent := float64(slot.CurrentLoad) / float64(slot.MaxCapacity) * 100
					remaining := slot.MaxCapacity - slot.CurrentLoad
					
					// Выбираем слот, который заполнен менее чем на 80% и имеет достаточно места
					if loadPercent < 80.0 && remaining > maxRemaining && remaining >= cheapestPrice*2 {
						maxRemaining = remaining
						targetSlot = slot
					}
				}
				
				if targetSlot == nil {
					// Все слоты заполнены более чем на 80%, ждем
					time.Sleep(500 * time.Millisecond)
					continue
				}
				
				// Вычисляем, сколько места осталось
				remaining := targetSlot.MaxCapacity - targetSlot.CurrentLoad
				
				// Выбираем пиццу и количество, которые точно поместятся
				// Увеличиваем запас до 500₽ для защиты от race condition
				var selectedPizza string
				var quantity int32 = 1
				
				// Пробуем найти подходящую пиццу
				found := false
				for attempts := 0; attempts < 50; attempts++ {
					idx := rand.Intn(len(pizzaNames))
					pizzaName := pizzaNames[idx]
					price := pizzaPrices[idx]
					
					// Вычисляем максимальное количество, которое поместится
					// Оставляем запас 500₽ на случай race condition
					maxQty := (remaining - 500) / price
					if maxQty > 3 {
						maxQty = 3 // Максимум 3 штуки
					}
					if maxQty < 1 {
						maxQty = 1
					}
					
					// Выбираем случайное количество от 1 до maxQty
					qty := int32(rand.Intn(int(maxQty)) + 1)
					total := price * int(qty)
					
					// Проверяем, что заказ точно поместится (с запасом 500₽)
					if total <= remaining-500 {
						selectedPizza = pizzaName
						quantity = qty
						found = true
						break
					}
				}
				
				if !found {
					// Не нашли подходящую комбинацию, берем самую дешевую пиццу
					if cheapestPrice <= remaining-500 {
						selectedPizza = cheapestPizza
						quantity = 1
						found = true
					}
				}
				
				if !found || selectedPizza == "" {
					// Не можем отправить заказ, пропускаем этот слот
					time.Sleep(300 * time.Millisecond)
					continue
				}
				
				// Отправляем заказ
				atomic.AddInt64(&totalRequests, 1)
				
				reqCtx, reqCancel := context.WithTimeout(context.Background(), 10*time.Second)
				_, err := client.CreateOrder(reqCtx, &pb.PizzaOrderRequest{
					CustomerId: 777 + int32(workerID),
					PizzaName:  selectedPizza,
					Quantity:   quantity,
				})
				reqCancel()
				
				if err == nil {
					atomic.AddInt64(&successRequests, 1)
					// Задержка перед следующим заказом (увеличена для избежания race condition)
					time.Sleep(200 * time.Millisecond)
				} else {
					atomic.AddInt64(&failedRequests, 1)
					// При ошибке ждем дольше
					time.Sleep(1 * time.Second)
				}
			}
		}(i)
	}
}

func printStats() {
	elapsed := time.Since(startTime).Seconds()
	if elapsed == 0 {
		return
	}

	total := atomic.LoadInt64(&totalRequests)
	success := atomic.LoadInt64(&successRequests)
	failed := atomic.LoadInt64(&failedRequests)
	currentRPS := float64(total) / elapsed
	successRate := float64(0)
	if total > 0 {
		successRate = float64(success) / float64(total) * 100
	}

	hours := int(elapsed) / 3600
	minutes := (int(elapsed) % 3600) / 60
	seconds := int(elapsed) % 60

	fmt.Printf("⏱️  [%02d:%02d:%02d] Всего: %d | ✅ Успешно: %d (%.1f%%) | ❌ Ошибок: %d | RPS: %.1f\n",
		hours, minutes, seconds, total, success, successRate, failed, currentRPS)
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
	totalLoad := 0
	totalCapacity := 0
	
	for _, slot := range currentSlots {
		totalLoad += slot.CurrentLoad
		totalCapacity += slot.MaxCapacity
		if slot.CurrentLoad >= slot.MaxCapacity {
			filledSlots++
		}
	}
	
	avgLoad := float64(0)
	if totalCapacity > 0 {
		avgLoad = float64(totalLoad) / float64(totalCapacity) * 100
	}
	
	fmt.Printf("📦 Всего слотов: %d\n", totalSlots)
	fmt.Printf("✅ Заполнено полностью: %d (%.1f%%)\n", filledSlots, float64(filledSlots)/float64(totalSlots)*100)
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
		fmt.Printf("  %d. %s: %d₽ / %d₽ (%.1f%%)\n", 
			i+1, slot.SlotID[:12], slot.CurrentLoad, slot.MaxCapacity, loadPercent)
	}
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
}

func printFinalStats() {
	duration := time.Since(startTime)
	total := atomic.LoadInt64(&totalRequests)
	success := atomic.LoadInt64(&successRequests)
	failed := atomic.LoadInt64(&failedRequests)
	rps := float64(0)
	if duration.Seconds() > 0 {
		rps = float64(total) / duration.Seconds()
	}
	successRate := float64(0)
	if total > 0 {
		successRate = float64(success) / float64(total) * 100
	}

	fmt.Printf("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	fmt.Printf("🏁 СТРЕСС-ТЕСТ ОКОНЧЕН\n")
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	fmt.Printf("⏱️  Время работы: %v (%.2f секунд)\n", duration, duration.Seconds())
	fmt.Printf("📈 Всего заказов отправлено: %d\n", total)
	fmt.Printf("✅ Успешных: %d (%.2f%%)\n", success, successRate)
	fmt.Printf("❌ Ошибок: %d (%.2f%%)\n", failed, 100-successRate)
	fmt.Printf("⚡ Средний RPS: %.2f заказов/сек\n", rps)
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
}
