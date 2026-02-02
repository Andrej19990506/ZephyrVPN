package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

type OrderRequest struct {
	Items []PizzaItem `json:"items"`
	IsSet bool        `json:"is_set"`
}

type PizzaItem struct {
	PizzaName   string   `json:"pizza_name"`
	Ingredients []string `json:"ingredients"`
	Extras      []string `json:"extras"`
	Quantity    int      `json:"quantity"`
	Price       int      `json:"price"`
}

var (
	totalRequests    int64
	successRequests  int64
	failedRequests   int64
	totalLatency     int64
	minLatency       int64 = 999999999
	maxLatency       int64
	startTime        time.Time
)

func main() {
	url := "http://localhost:8080/api/v1/order"
	concurrency := 100   // Количество одновременных горутин
	duration := 10       // Длительность теста в секундах
	targetRPS := 1000    // Целевое количество запросов в секунду (для 10,000 заказов за 10 сек)

	fmt.Printf("🚀 Нагрузочное тестирование Go сервера\n")
	fmt.Printf("📍 URL: %s\n", url)
	fmt.Printf("👥 Concurrency: %d горутин\n", concurrency)
	fmt.Printf("⏱️  Длительность: %d секунд\n", duration)
	fmt.Printf("🎯 Цель: %d запросов/сек\n", targetRPS)
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n\n")

	// Тестовый заказ
	order := OrderRequest{
		Items: []PizzaItem{
			{
				PizzaName:   "Пепперони",
				Ingredients: []string{"сыр моцарелла", "пепперони", "соус"},
				Extras:      []string{"Сырный бортик"},
				Quantity:    1,
				Price:       748,
			},
		},
		IsSet: false,
	}

	orderJSON, err := json.Marshal(order)
	if err != nil {
		log.Fatalf("Ошибка создания JSON: %v", err)
	}

	// Канал для остановки
	stopChan := make(chan bool)
	var wg sync.WaitGroup

	// Запускаем горутины
	startTime = time.Now()
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go worker(url, orderJSON, stopChan, &wg, targetRPS/concurrency)
	}

	// Запускаем сбор статистики
	go statsCollector()

	// Ждем указанное время
	time.Sleep(time.Duration(duration) * time.Second)
	close(stopChan)

	// Ждем завершения всех горутин
	wg.Wait()

	// Финальная статистика
	printFinalStats()
}

func worker(url string, orderJSON []byte, stopChan chan bool, wg *sync.WaitGroup, rpsPerWorker int) {
	defer wg.Done()

	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 100,
			IdleConnTimeout:     90 * time.Second,
		},
	}

	// Интервал между запросами для достижения целевого RPS
	interval := time.Second / time.Duration(rpsPerWorker)
	if interval < time.Microsecond {
		interval = time.Microsecond
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-stopChan:
			return
		case <-ticker.C:
			sendRequest(client, url, orderJSON)
		}
	}
}

func sendRequest(client *http.Client, url string, orderJSON []byte) {
	start := time.Now()

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(orderJSON))
	if err != nil {
		atomic.AddInt64(&failedRequests, 1)
		return
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		atomic.AddInt64(&failedRequests, 1)
		atomic.AddInt64(&totalRequests, 1)
		return
	}
	defer resp.Body.Close()

	latency := time.Since(start).Microseconds()
	atomic.AddInt64(&totalRequests, 1)

	if resp.StatusCode == http.StatusOK {
		atomic.AddInt64(&successRequests, 1)
	} else {
		atomic.AddInt64(&failedRequests, 1)
	}

	// Обновляем статистику латентности
	atomic.AddInt64(&totalLatency, latency)

	// Минимальная латентность
	for {
		old := atomic.LoadInt64(&minLatency)
		if latency >= old || atomic.CompareAndSwapInt64(&minLatency, old, latency) {
			break
		}
	}

	// Максимальная латентность
	for {
		old := atomic.LoadInt64(&maxLatency)
		if latency <= old || atomic.CompareAndSwapInt64(&maxLatency, old, latency) {
			break
		}
	}
}

func statsCollector() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		elapsed := time.Since(startTime).Seconds()
		if elapsed == 0 {
			continue
		}

		total := atomic.LoadInt64(&totalRequests)
		success := atomic.LoadInt64(&successRequests)
		failed := atomic.LoadInt64(&failedRequests)
		currentRPS := float64(total) / elapsed

		avgLatency := int64(0)
		if total > 0 {
			avgLatency = atomic.LoadInt64(&totalLatency) / total
		}

		fmt.Printf("⏱️  [%.0fs] RPS: %.0f | Всего: %d | ✅ Успешно: %d | ❌ Ошибок: %d | ⚡ Средняя латентность: %d мкс\n",
			elapsed, currentRPS, total, success, failed, avgLatency)
	}
}

func printFinalStats() {
	elapsed := time.Since(startTime).Seconds()
	total := atomic.LoadInt64(&totalRequests)
	success := atomic.LoadInt64(&successRequests)
	failed := atomic.LoadInt64(&failedRequests)

	avgRPS := float64(total) / elapsed
	successRate := float64(success) / float64(total) * 100

	avgLatency := int64(0)
	if total > 0 {
		avgLatency = atomic.LoadInt64(&totalLatency) / total
	}

	minLat := atomic.LoadInt64(&minLatency)
	maxLat := atomic.LoadInt64(&maxLatency)

	fmt.Printf("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	fmt.Printf("📊 ФИНАЛЬНАЯ СТАТИСТИКА\n")
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	fmt.Printf("⏱️  Время теста: %.2f секунд\n", elapsed)
	fmt.Printf("📈 Средний RPS: %.0f запросов/сек\n", avgRPS)
	fmt.Printf("📊 Всего запросов: %d\n", total)
	fmt.Printf("✅ Успешных: %d (%.2f%%)\n", success, successRate)
	fmt.Printf("❌ Ошибок: %d (%.2f%%)\n", failed, 100-successRate)
	fmt.Printf("⚡ Средняя латентность: %d мкс (%.2f мс)\n", avgLatency, float64(avgLatency)/1000)
	fmt.Printf("🚀 Минимальная латентность: %d мкс (%.2f мс)\n", minLat, float64(minLat)/1000)
	fmt.Printf("🐌 Максимальная латентность: %d мкс (%.2f мс)\n", maxLat, float64(maxLat)/1000)
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")

	if total >= 10000 {
		fmt.Printf("🎉 УСПЕХ! Обработано 10,000+ заказов!\n")
		fmt.Printf("📊 Средний RPS: %.0f запросов/сек\n", avgRPS)
	} else {
		fmt.Printf("⚠️  Цель не достигнута. Обработано: %d из 10,000\n", total)
		fmt.Printf("📊 Средний RPS: %.0f запросов/сек\n", avgRPS)
	}
}

