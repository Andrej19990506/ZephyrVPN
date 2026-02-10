# Kafka Retention Policy & Topic Cleanup Optimization

## 1. Inspection: Проверка текущих настроек retention

### Проверка retention для топика `pizza-orders`:

```bash
# Если Kafka запущен в Docker
docker exec -it zephyrvpn_kafka kafka-configs \
  --bootstrap-server localhost:9092 \
  --entity-type topics \
  --entity-name pizza-orders \
  --describe

# Или если Kafka доступен напрямую
kafka-configs \
  --bootstrap-server localhost:9092 \
  --entity-type topics \
  --entity-name pizza-orders \
  --describe
```

**Ожидаемый вывод:**
```
Configs for topic 'pizza-orders' are:
  retention.ms=604800000
  retention.bytes=-1
```

Где:
- `retention.ms` - время хранения сообщений в миллисекундах (по умолчанию 7 дней)
- `retention.bytes` - максимальный размер топика в байтах (-1 = без ограничения)

---

## 2. Configuration: Установка retention policy

### Установка retention на 24 часа (86,400,000 ms) и лимит 5GB:

```bash
# Установка retention.ms = 24 часа (86,400,000 миллисекунд)
docker exec -it zephyrvpn_kafka kafka-configs \
  --bootstrap-server localhost:9092 \
  --entity-type topics \
  --entity-name pizza-orders \
  --alter \
  --add-config retention.ms=86400000

# Установка retention.bytes = 5GB (5 * 1024 * 1024 * 1024 = 5368709120 байт)
docker exec -it zephyrvpn_kafka kafka-configs \
  --bootstrap-server localhost:9092 \
  --entity-type topics \
  --entity-name pizza-orders \
  --alter \
  --add-config retention.bytes=5368709120

# Или одной командой:
docker exec -it zephyrvpn_kafka kafka-configs \
  --bootstrap-server localhost:9092 \
  --entity-type topics \
  --entity-name pizza-orders \
  --alter \
  --add-config retention.ms=86400000,retention.bytes=5368709120
```

**Проверка применения настроек:**
```bash
docker exec -it zephyrvpn_kafka kafka-configs \
  --bootstrap-server localhost:9092 \
  --entity-type topics \
  --entity-name pizza-orders \
  --describe
```

**Важно:** Kafka удалит старые сообщения автоматически при следующем запуске фоновой задачи очистки (обычно каждые 5 минут).

---

## 3. Go Consumer Logic: Правильная конфигурация Consumer Group

### Обновленный код для `kafka_ws_consumer.go`:

```go
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync/atomic"
	"time"

	"github.com/segmentio/kafka-go"
	"google.golang.org/protobuf/proto"
	"zephyrvpn/server/internal/models"
	"zephyrvpn/server/internal/pb"
	"zephyrvpn/server/internal/services"
	"zephyrvpn/server/internal/utils"
)

// NewKafkaWSConsumer создает новый Kafka Consumer с правильной конфигурацией
func NewKafkaWSConsumer(
	brokers string, 
	topic string, 
	redisUtil *utils.RedisClient, 
	username, password, caCert string, 
	startFromLatest bool,
	orderService *services.OrderService,
) *KafkaWSConsumer {
	brokerList := ParseKafkaBrokers(brokers)
	ctx, cancel := context.WithCancel(context.Background())
	
	dialer := CreateKafkaDialer(username, password, caCert)
	
	// Определяем начальный offset
	startOffset := kafka.FirstOffset
	if startFromLatest {
		startOffset = kafka.LastOffset
		log.Printf("📡 Kafka Consumer: startOffset=LastOffset (после bootstrap из БД)")
	} else {
		log.Printf("📡 Kafka Consumer: startOffset=FirstOffset (начальный запуск)")
	}
	
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:     brokerList,
		Topic:       topic,
		GroupID:     "order-service-stable-group", // Стабильный group.id
		
		// КРИТИЧНО: StartOffset используется только при первом подключении
		// После этого Kafka использует сохраненный offset из __consumer_offsets
		StartOffset: startOffset,
		
		// Настройки производительности
		MinBytes:    10e3,  // Минимум 10KB для батчинга (улучшает throughput)
		MaxBytes:    10e6,  // Максимум 10MB за один fetch
		MaxWait:     1 * time.Second, // Максимальное ожидание для батчинга
		
		// Настройки Consumer Group
		SessionTimeout:    60 * time.Second,   // Таймаут сессии (consumer считается мертвым)
		HeartbeatInterval: 20 * time.Second,   // Интервал heartbeat (должен быть < SessionTimeout/3)
		RebalanceTimeout:  30 * time.Second,   // Время на rebalance при добавлении/удалении consumer
		
		// CommitInterval: автоматический commit offset каждые N секунд
		// Если не установлен, commit происходит только при вызове CommitMessages()
		CommitInterval: 5 * time.Second, // Автоматический commit каждые 5 секунд
		
		Dialer: dialer,
	})
	
	return &KafkaWSConsumer{
		brokers:      brokerList,
		topic:        topic,
		groupID:      "order-service-stable-group",
		reader:       reader,
		ctx:          ctx,
		cancel:       cancel,
		redisUtil:    redisUtil,
		orderService: orderService,
		lastLog:      time.Now().Unix(),
	}
}

// Start запускает чтение из Kafka с правильным commit offset
func (kc *KafkaWSConsumer) Start() {
	log.Printf("📡 Kafka WS Consumer запущен: topic=%s, groupID=%s", kc.topic, kc.groupID)
	
	go func() {
		for {
			select {
			case <-kc.ctx.Done():
				log.Println("🛑 Kafka WS Consumer остановлен")
				return
			default:
				// Читаем сообщение из Kafka
				msg, err := kc.reader.ReadMessage(kc.ctx)
				if err != nil {
					if err == context.Canceled {
						return
					}
					log.Printf("⚠️ Kafka WS Consumer ошибка чтения: %v", err)
					time.Sleep(1 * time.Second)
					continue
				}
				
				// Обрабатываем сообщение
				if err := kc.processMessage(msg); err != nil {
					log.Printf("⚠️ Kafka WS Consumer ошибка обработки сообщения: %v", err)
					// НЕ commit'им offset при ошибке обработки - сообщение будет обработано повторно
					continue
				}
				
				// КРИТИЧНО: Commit offset только после успешной обработки
				// Это гарантирует, что сообщение не будет потеряно при сбое
				if err := kc.reader.CommitMessages(kc.ctx, msg); err != nil {
					log.Printf("⚠️ Kafka WS Consumer ошибка commit offset: %v", err)
					// Продолжаем работу, так как CommitInterval также делает автоматический commit
				}
				
				atomic.AddInt64(&kc.processed, 1)
			}
		}
	}()
}

// processMessage обрабатывает одно сообщение из Kafka
func (kc *KafkaWSConsumer) processMessage(msg kafka.Message) error {
	// ... существующая логика обработки ...
	return nil
}
```

### Ключевые моменты конфигурации:

1. **CommitInterval**: Автоматический commit каждые 5 секунд
   - Уменьшает нагрузку на Kafka
   - Гарантирует, что обработанные сообщения не будут повторно обработаны

2. **CommitMessages()**: Явный commit после успешной обработки
   - Гарантирует at-least-once delivery
   - Offset commit'ится только после успешной обработки

3. **GroupID**: Стабильный идентификатор
   - Kafka сохраняет offset в `__consumer_offsets`
   - При перезапуске consumer продолжит с последнего commit'нутого offset

---

## 4. auto.offset.reset: earliest vs latest

### Разница между `earliest` и `latest`:

**`earliest`** (FirstOffset):
- Consumer начинает читать с самого старого сообщения в топике
- Используется для:
  - Первого запуска consumer
  - Восстановления всех сообщений после сбоя
  - Обработки исторических данных

**`latest`** (LastOffset):
- Consumer начинает читать только новые сообщения (после подключения)
- Используется для:
  - Production окружения после bootstrap из БД
  - Реального времени (только новые события)
  - Избежания повторной обработки старых сообщений

### Рекомендация для Production:

```go
// После BootstrapState из PostgreSQL используем LastOffset
startOffset := kafka.LastOffset

// При первом запуске (без БД) используем FirstOffset
if !hasBootstrapFromDB {
    startOffset = kafka.FirstOffset
}
```

**Важно:** После первого commit offset, Kafka игнорирует `StartOffset` и использует сохраненный offset из Consumer Group.

---

## 5. Manual Purge: Безопасная очистка топика

### Метод 1: Временное снижение retention (рекомендуется)

```bash
# Шаг 1: Устанавливаем retention на 1 секунду (1000 ms)
docker exec -it zephyrvpn_kafka kafka-configs \
  --bootstrap-server localhost:9092 \
  --entity-type topics \
  --entity-name pizza-orders \
  --alter \
  --add-config retention.ms=1000

# Шаг 2: Ждем, пока Kafka удалит все сообщения (обычно 1-5 минут)
# Проверяем количество сообщений:
docker exec -it zephyrvpn_kafka kafka-run-class kafka.tools.GetOffsetShell \
  --bootstrap-server localhost:9092 \
  --topic pizza-orders \
  --time -1

# Шаг 3: Восстанавливаем нормальный retention (24 часа)
docker exec -it zephyrvpn_kafka kafka-configs \
  --bootstrap-server localhost:9092 \
  --entity-type topics \
  --entity-name pizza-orders \
  --alter \
  --add-config retention.ms=86400000
```

### Метод 2: Удаление и пересоздание топика (более радикальный)

```bash
# ВНИМАНИЕ: Это удалит топик полностью!
# Убедитесь, что все consumer'ы остановлены

# Удаление топика
docker exec -it zephyrvpn_kafka kafka-topics \
  --bootstrap-server localhost:9092 \
  --delete \
  --topic pizza-orders

# Пересоздание топика с правильными настройками
docker exec -it zephyrvpn_kafka kafka-topics \
  --bootstrap-server localhost:9092 \
  --create \
  --topic pizza-orders \
  --partitions 1 \
  --replication-factor 1 \
  --config retention.ms=86400000 \
  --config retention.bytes=5368709120
```

### Метод 3: Программная очистка через Go (для тестирования)

```go
// Очистка топика через producer (отправка "tombstone" сообщений)
func PurgeTopic(brokers []string, topic string) error {
    writer := &kafka.Writer{
        Addr:  kafka.TCP(brokers...),
        Topic: topic,
    }
    defer writer.Close()
    
    // Отправляем пустое сообщение с ключом для удаления
    // Это не удалит все сообщения, но может помочь в некоторых случаях
    // Лучше использовать Method 1 или 2
    return nil
}
```

---

## 6. Как работают Kafka Segments (сегменты)

### Концепция сегментов:

Kafka хранит данные в **сегментах (segments)** - файлах на диске. Каждая партиция состоит из множества сегментов.

### Структура сегментов:

```
partition-0/
  ├── 00000000000000000000.log  (сегмент 1: offset 0-1000)
  ├── 00000000000000000100.log  (сегмент 2: offset 1000-2000)
  ├── 00000000000000000200.log  (сегмент 3: offset 2000-3000)
  └── ...
```

### Процесс удаления данных:

1. **Retention по времени (`retention.ms`)**:
   - Kafka проверяет каждый сегмент
   - Если все сообщения в сегменте старше `retention.ms`, сегмент помечается для удаления
   - Сегмент удаляется только если он **полностью** устарел (не частично!)

2. **Retention по размеру (`retention.bytes`)**:
   - Kafka проверяет общий размер всех сегментов партиции
   - Если размер превышает `retention.bytes`, удаляются самые старые сегменты
   - Удаление происходит по принципу "целый сегмент", не частично

3. **Активный сегмент (active segment)**:
   - Текущий сегмент, в который пишутся новые сообщения, **никогда не удаляется**
   - Даже если он превышает retention policy
   - Это гарантирует, что новые сообщения всегда можно записать

### Логирование сегментов:

```bash
# Просмотр сегментов топика
docker exec -it zephyrvpn_kafka kafka-log-dirs \
  --bootstrap-server localhost:9092 \
  --topic-list pizza-orders \
  --describe
```

### Важные моменты:

- **Сегменты удаляются целиком**, не частично
- **Активный сегмент защищен** от удаления
- **Удаление происходит асинхронно** (обычно каждые 5 минут)
- **Retention проверяется на уровне партиции**, не топика

---

## 7. Мониторинг retention

### Проверка размера топика:

```bash
# Размер топика в байтах
docker exec -it zephyrvpn_kafka kafka-log-dirs \
  --bootstrap-server localhost:9092 \
  --topic-list pizza-orders \
  --describe | grep "size"
```

### Проверка количества сообщений:

```bash
# Количество сообщений в топике
docker exec -it zephyrvpn_kafka kafka-run-class kafka.tools.GetOffsetShell \
  --bootstrap-server localhost:9092 \
  --topic pizza-orders \
  --time -1
```

### Проверка Consumer Group offset:

```bash
# Показывает текущий offset consumer group
docker exec -it zephyrvpn_kafka kafka-consumer-groups \
  --bootstrap-server localhost:9092 \
  --group order-service-stable-group \
  --describe
```

---

## Рекомендации для Production:

1. **Retention Policy**: 24 часа достаточно для event bus
2. **Size Limit**: 5GB предотвращает переполнение диска
3. **Consumer Group**: Всегда используйте стабильный `group.id`
4. **Commit Strategy**: Автоматический commit + явный commit после обработки
5. **Monitoring**: Регулярно проверяйте размер топика и количество сообщений


