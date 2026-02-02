package main

import (
	"bytes"
	"fmt"
	"net/http"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

var (
	totalRequests   int64
	successRequests int64
	failedRequests  int64
)

func getMemStats() (allocated, total, sys uint64) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.Alloc, m.TotalAlloc, m.Sys
}

func printSystemStats(prefix string) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	fmt.Printf("%s📊 Системные показатели:\n", prefix)
	fmt.Printf("%s  💾 Память клиента:\n", prefix)
	fmt.Printf("%s     - Выделено: %.2f MB\n", prefix, float64(m.Alloc)/1024/1024)
	fmt.Printf("%s     - Всего выделено: %.2f MB\n", prefix, float64(m.TotalAlloc)/1024/1024)
	fmt.Printf("%s     - Системная: %.2f MB\n", prefix, float64(m.Sys)/1024/1024)
	fmt.Printf("%s     - Количество GC: %d\n", prefix, m.NumGC)
	fmt.Printf("%s  🔧 Go runtime:\n", prefix)
	fmt.Printf("%s     - Горутин: %d\n", prefix, runtime.NumGoroutine())
	fmt.Printf("%s     - CPU ядер: %d\n", prefix, runtime.NumCPU())
}

func main() {
	url := "http://localhost:8080/api/v1/order"

	payload := []byte(`{
		"items": [{
			"pizza_name": "Пепперони",
			"ingredients": ["сыр моцарелла", "пепперони", "соус"],
			"extras": ["Сырный бортик"],
			"quantity": 1,
			"price": 748
		}],
		"is_set": false
	}`)

	var wg sync.WaitGroup
	start := time.Now()

	// Настройка времени теста
	testDuration := 5 * time.Minute
	stopTest := time.After(testDuration)

	fmt.Println("🚀 Нагрузочное тестирование Go сервера [РЕЖИМ 5 МИНУТ]")
	fmt.Println("📍 URL:", url)
	fmt.Println("🎯 Цель: Максимальный RPS в течение", testDuration)
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	fmt.Println("\n📊 Начальные показатели системы:")
	printSystemStats("")

	startMem, _, startSys := getMemStats()

	fmt.Println("\n⏳ Запускаем ТЯЖЕЛЫЙ тест...\n")

	// Мониторинг ресурсов
	monitorStop := make(chan bool)
	var monitorWg sync.WaitGroup
	monitorWg.Add(1)
	go func() {
		defer monitorWg.Done()
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-monitorStop:
				return
			case <-ticker.C:
				elapsed := time.Since(start).Seconds()
				currentTotal := atomic.LoadInt64(&totalRequests)
				fmt.Printf("⏱️  [%.0fs] RPS: %.0f | Всего: %d\n", elapsed, float64(currentTotal)/elapsed, currentTotal)
				printSystemStats("   ")
				fmt.Println()
			}
		}
	}()

	// Запускаем 1000 горутин-стрелков
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			client := &http.Client{
				Timeout: 30 * time.Second,
				Transport: &http.Transport{
					MaxIdleConnsPerHost: 2000,
					MaxIdleConns:        5000,
					IdleConnTimeout:     90 * time.Second,
					DisableKeepAlives:   false, // Используем Keep-Alive для скорости
				},
			}
			for {
				select {
				case <-stopTest:
					return // Время вышло, прекращаем стрельбу
				default:
					atomic.AddInt64(&totalRequests, 1)
					resp, err := client.Post(url, "application/json", bytes.NewBuffer(payload))
					if err == nil && resp.StatusCode == 200 {
						atomic.AddInt64(&successRequests, 1)
						resp.Body.Close()
					} else {
						atomic.AddInt64(&failedRequests, 1)
						if resp != nil {
							resp.Body.Close()
						}
					}
				}
			}
		}()
	}

	wg.Wait()

	// Завершаем мониторинг
	close(monitorStop)
	monitorWg.Wait()

	duration := time.Since(start)
	total := atomic.LoadInt64(&totalRequests)
	success := atomic.LoadInt64(&successRequests)
	failed := atomic.LoadInt64(&failedRequests)
	rps := float64(total) / duration.Seconds()

	endMem, _, endSys := getMemStats()
	memUsedChange := float64(int64(endMem)-int64(startMem)) / 1024 / 1024
	sysMemUsedChange := float64(int64(endSys)-int64(startSys)) / 1024 / 1024

	fmt.Println("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("📊 ФИНАЛЬНАЯ СТАТИСТИКА (5 МИНУТ)")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("⏱️  Время теста: %v\n", duration)
	fmt.Printf("📈 Всего запросов: %d\n", total)
	fmt.Printf("✅ Успешных: %d (%.1f%%)\n", success, float64(success)/float64(total)*100)
	fmt.Printf("❌ Ошибок: %d (%.1f%%)\n", failed, float64(failed)/float64(total)*100)
	fmt.Printf("⚡ Средний RPS: %.0f\n", rps)
	fmt.Println()
	fmt.Println("💾 Память (клиент обстрела):")
	fmt.Printf("   - Изменение Heap: %.2f MB\n", memUsedChange)
	fmt.Printf("   - Изменение System: %.2f MB\n", sysMemUsedChange)
	fmt.Println()
	printSystemStats("")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	fmt.Println("\n💡 Проверка результатов в ERP:")
	fmt.Println("   👉 http://localhost:8080/api/v1/erp/stats")
}
