# Redis как Single Point of Failure: Анализ и Решения

## 🔴 Текущая ситуация

### Проблема: Redis — Single Point of Failure

**Текущая конфигурация** (`docker-compose.yml`):
```yaml
redis:
  image: redis:7-alpine
  container_name: zephyrvpn_redis
  command: ["redis-server", "--save", "", "--appendonly", "no", "--maxmemory", "4gb", "--maxmemory-policy", "allkeys-lru"]
  ports:
    - "6379:6379"
  volumes:
    - redis_data:/data
  restart: unless-stopped
```

**Проблемы**:
- ❌ **Один экземпляр Redis** — нет репликации
- ❌ **Нет Redis Sentinel** — нет автоматического failover
- ❌ **Нет Redis Cluster** — нет распределения нагрузки
- ❌ **AOF отключен** (`--appendonly no`) — нет персистентности при перезапуске
- ❌ **RDB отключен** (`--save ""`) — нет снимков на диск

### Что произойдет при падении Redis?

#### 1. **Слоты (SlotService)**
```go
// internal/services/slot_service.go:176
if ss.redisUtil == nil {
    return "", time.Time{}, time.Time{}, fmt.Errorf("Redis client not initialized")
}
```
- ❌ **Невозможно назначить слоты** — все заказы будут отклоняться
- ❌ **Capacity-Based Scheduling не работает**
- ❌ **Нет информации о загрузке слотов**

#### 2. **Кэш меню (MenuService)**
```go
// internal/services/menu_service.go:218
if ms.redisUtil != nil {
    // Кэширование меню в Redis
}
```
- ❌ **Меню не кэшируется** — каждый запрос идет в PostgreSQL
- ⚠️ **Система работает, но медленнее** (fallback на БД)

#### 3. **Заказы в Redis**
```go
// internal/api/grpc_order_server.go:244
_, err = pipe.Exec(redisCtx)
if err != nil {
    log.Printf("⚠️ Pipeline error при создании заказа через gRPC %s: %v", fullID, err)
    // ❌ ОШИБКА ЛОГИРУЕТСЯ, НО НЕ БЛОКИРУЕТ РАБОТУ
}
```
- ❌ **Заказы не сохраняются в Redis** — нет быстрого доступа
- ⚠️ **Заказы все еще в Kafka и PostgreSQL** — можно восстановить

#### 4. **WebSocket Hub**
- ❌ **Нет Pub/Sub для уведомлений**
- ⚠️ **WebSocket работает напрямую** — не зависит от Redis

---

## 🔍 Анализ кода

### Обработка ошибок Redis

**Текущая реализация**:
```go
// internal/database/redis.go:30
if err := client.Ping(ctx).Err(); err != nil {
    return nil, fmt.Errorf("failed to connect to Redis: %w", err)
}
```

**Проблемы**:
1. **При старте**: Если Redis недоступен, приложение не запустится
2. **Во время работы**: Нет автоматического переподключения
3. **При ошибках**: Операции просто логируются, но не обрабатываются

**Примеры из кода**:
```go
// grpc_order_server.go:244
_, err = pipe.Exec(redisCtx)
if err != nil {
    log.Printf("⚠️ Pipeline error...") // Только логирование!
}

// slot_service.go:176
if ss.redisUtil == nil {
    return "", time.Time{}, time.Time{}, fmt.Errorf("Redis client not initialized")
    // ❌ Блокирует создание заказов!
}
```

---

## ✅ Решения

### Вариант 1: Redis Sentinel (РЕКОМЕНДУЕТСЯ для Production)

**Архитектура**:
- 1 Master + 2 Replicas
- 3 Sentinel узла для мониторинга
- Автоматический failover при падении Master

**docker-compose.yml**:
```yaml
version: '3.8'

services:
  # Redis Master
  redis-master:
    image: redis:7-alpine
    container_name: zephyrvpn_redis_master
    command: ["redis-server", "--appendonly", "yes", "--maxmemory", "2gb"]
    ports:
      - "6379:6379"
    volumes:
      - redis_master_data:/data
    restart: unless-stopped

  # Redis Replica 1
  redis-replica-1:
    image: redis:7-alpine
    container_name: zephyrvpn_redis_replica_1
    command: ["redis-server", "--replicaof", "redis-master", "6379", "--appendonly", "yes"]
    depends_on:
      - redis-master
    restart: unless-stopped

  # Redis Replica 2
  redis-replica-2:
    image: redis:7-alpine
    container_name: zephyrvpn_redis_replica_2
    command: ["redis-server", "--replicaof", "redis-master", "6379", "--appendonly", "yes"]
    depends_on:
      - redis-master
    restart: unless-stopped

  # Sentinel 1
  redis-sentinel-1:
    image: redis:7-alpine
    container_name: zephyrvpn_redis_sentinel_1
    command: >
      redis-sentinel /etc/redis/sentinel.conf
      --sentinel announce-ip localhost
      --sentinel announce-port 26379
    volumes:
      - ./redis-sentinel.conf:/etc/redis/sentinel.conf
    depends_on:
      - redis-master
    ports:
      - "26379:26379"
    restart: unless-stopped

  # Sentinel 2
  redis-sentinel-2:
    image: redis:7-alpine
    container_name: zephyrvpn_redis_sentinel_2
    command: >
      redis-sentinel /etc/redis/sentinel.conf
      --sentinel announce-ip localhost
      --sentinel announce-port 26380
    volumes:
      - ./redis-sentinel.conf:/etc/redis/sentinel.conf
    depends_on:
      - redis-master
    ports:
      - "26380:26380"
    restart: unless-stopped

  # Sentinel 3
  redis-sentinel-3:
    image: redis:7-alpine
    container_name: zephyrvpn_redis_sentinel_3
    command: >
      redis-sentinel /etc/redis/sentinel.conf
      --sentinel announce-ip localhost
      --sentinel announce-port 26381
    volumes:
      - ./redis-sentinel.conf:/etc/redis/sentinel.conf
    depends_on:
      - redis-master
    ports:
      - "26381:26381"
    restart: unless-stopped

volumes:
  redis_master_data:
```

**redis-sentinel.conf**:
```conf
port 26379
sentinel monitor mymaster redis-master 6379 2
sentinel down-after-milliseconds mymaster 5000
sentinel parallel-syncs mymaster 1
sentinel failover-timeout mymaster 10000
sentinel auth-pass mymaster your_password_here  # Если нужна авторизация
```

**Обновление Go кода**:
```go
// internal/database/redis.go
import (
    "github.com/redis/go-redis/v9"
)

func ConnectRedisWithSentinel(sentinelAddrs []string, masterName, password string) (*redis.Client, error) {
    opt := &redis.FailoverOptions{
        MasterName:    masterName,
        SentinelAddrs: sentinelAddrs,
        Password:      password,
        PoolSize:      1000,
        MinIdleConns:  50,
        MaxRetries:    3,
    }

    client := redis.NewFailoverClient(opt)

    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()

    if err := client.Ping(ctx).Err(); err != nil {
        return nil, fmt.Errorf("failed to connect to Redis Sentinel: %w", err)
    }

    log.Println("✅ Redis Sentinel connected successfully")
    return client, nil
}
```

**Преимущества**:
- ✅ Автоматический failover
- ✅ Высокая доступность (99.9%+)
- ✅ Чтение с реплик (масштабируемость)
- ✅ Минимальные изменения в коде

**Недостатки**:
- ⚠️ Больше ресурсов (3 Redis + 3 Sentinel)
- ⚠️ Сложнее настройка

---

### Вариант 2: Redis Cluster (для масштабирования)

**Архитектура**:
- 6 узлов (3 Master + 3 Replica)
- Автоматическое шардирование
- Высокая производительность

**docker-compose.yml** (упрощенный):
```yaml
redis-cluster:
  image: redis:7-alpine
  command: >
    redis-server
    --cluster-enabled yes
    --cluster-config-file nodes.conf
    --cluster-node-timeout 5000
    --appendonly yes
  ports:
    - "7000-7005:7000-7005"
```

**Обновление Go кода**:
```go
func ConnectRedisCluster(addrs []string, password string) (*redis.ClusterClient, error) {
    opt := &redis.ClusterOptions{
        Addrs:        addrs,
        Password:     password,
        PoolSize:     1000,
        MinIdleConns: 50,
        MaxRetries:   3,
    }

    client := redis.NewClusterClient(opt)

    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()

    if err := client.Ping(ctx).Err(); err != nil {
        return nil, fmt.Errorf("failed to connect to Redis Cluster: %w", err)
    }

    log.Println("✅ Redis Cluster connected successfully")
    return client, nil
}
```

**Преимущества**:
- ✅ Горизонтальное масштабирование
- ✅ Высокая производительность
- ✅ Автоматическое шардирование

**Недостатки**:
- ⚠️ Сложная настройка
- ⚠️ Требует больше ресурсов
- ⚠️ Некоторые команды не поддерживаются (например, транзакции)

---

### Вариант 3: Fallback на PostgreSQL (для критичных операций)

**Идея**: При недоступности Redis использовать PostgreSQL как fallback

**Обновление SlotService**:
```go
// internal/services/slot_service.go
func (ss *SlotService) AssignSlot(orderID string, orderPrice int, itemsCount int) (string, time.Time, time.Time, error) {
    // Пробуем Redis
    if ss.redisUtil != nil {
        slotID, slotStart, visibleAt, err := ss.assignSlotRedis(orderID, orderPrice, itemsCount)
        if err == nil {
            return slotID, slotStart, visibleAt, nil
        }
        log.Printf("⚠️ Redis недоступен, используем PostgreSQL fallback: %v", err)
    }

    // Fallback на PostgreSQL
    return ss.assignSlotPostgreSQL(orderID, orderPrice, itemsCount)
}

func (ss *SlotService) assignSlotPostgreSQL(orderID string, orderPrice int, itemsCount int) (string, time.Time, time.Time, error) {
    // Используем PostgreSQL для хранения состояния слотов
    // Создаем таблицу slot_assignments если её нет
    query := `
        INSERT INTO slot_assignments (slot_id, order_id, order_price, created_at)
        VALUES ($1, $2, $3, NOW())
        ON CONFLICT (slot_id, order_id) DO NOTHING
        RETURNING slot_id, created_at
    `
    
    // Логика поиска свободного слота через SQL
    // ...
}
```

**Преимущества**:
- ✅ Система работает даже при падении Redis
- ✅ Минимальные изменения в архитектуре
- ✅ PostgreSQL уже есть в системе

**Недостатки**:
- ⚠️ Медленнее, чем Redis
- ⚠️ Больше нагрузка на PostgreSQL
- ⚠️ Нужно синхронизировать данные при восстановлении Redis

---

### Вариант 4: Circuit Breaker Pattern

**Идея**: Автоматически переключаться на fallback при частых ошибках Redis

**Реализация**:
```go
// internal/utils/circuit_breaker.go
type CircuitBreaker struct {
    maxFailures int
    timeout     time.Duration
    failures    int
    lastFailure time.Time
    state       string // "closed", "open", "half-open"
    mutex       sync.RWMutex
}

func (cb *CircuitBreaker) Call(fn func() error) error {
    cb.mutex.RLock()
    state := cb.state
    cb.mutex.RUnlock()

    if state == "open" {
        if time.Since(cb.lastFailure) > cb.timeout {
            cb.mutex.Lock()
            cb.state = "half-open"
            cb.mutex.Unlock()
        } else {
            return fmt.Errorf("circuit breaker is open")
        }
    }

    err := fn()
    if err != nil {
        cb.mutex.Lock()
        cb.failures++
        cb.lastFailure = time.Now()
        if cb.failures >= cb.maxFailures {
            cb.state = "open"
        }
        cb.mutex.Unlock()
        return err
    }

    // Успешный вызов - сбрасываем счетчик
    cb.mutex.Lock()
    cb.failures = 0
    cb.state = "closed"
    cb.mutex.Unlock()
    return nil
}
```

**Использование**:
```go
var redisCircuitBreaker = NewCircuitBreaker(5, 30*time.Second)

func (ss *SlotService) AssignSlot(orderID string, orderPrice int, itemsCount int) (string, time.Time, time.Time, error) {
    var result string
    var slotStart time.Time
    var visibleAt time.Time
    var err error

    redisErr := redisCircuitBreaker.Call(func() error {
        result, slotStart, visibleAt, err = ss.assignSlotRedis(orderID, orderPrice, itemsCount)
        return err
    })

    if redisErr != nil {
        // Fallback на PostgreSQL
        return ss.assignSlotPostgreSQL(orderID, orderPrice, itemsCount)
    }

    return result, slotStart, visibleAt, nil
}
```

---

## 🎯 Рекомендации

### Для Production (HighLoad):

1. **Redis Sentinel** (Вариант 1)
   - 1 Master + 2 Replicas
   - 3 Sentinel узла
   - Автоматический failover

2. **Circuit Breaker** (Вариант 4)
   - Автоматическое переключение на fallback
   - Защита от каскадных сбоев

3. **Fallback на PostgreSQL** (Вариант 3)
   - Для критичных операций (слоты)
   - Синхронизация при восстановлении Redis

### Для Development:

1. **Один Redis с персистентностью**
   ```yaml
   redis:
     command: ["redis-server", "--appendonly", "yes", "--save", "60 1000"]
   ```

2. **Health checks и автоматический перезапуск**
   ```yaml
   healthcheck:
     test: ["CMD", "redis-cli", "ping"]
     interval: 5s
     timeout: 3s
     retries: 5
   ```

---

## 📊 Сравнение решений

| Решение | Доступность | Производительность | Сложность | Ресурсы |
|---------|-------------|-------------------|-----------|---------|
| Текущее (1 Redis) | ❌ Низкая | ✅ Высокая | ✅ Простая | ✅ Низкие |
| Redis Sentinel | ✅ Высокая | ✅ Высокая | ⚠️ Средняя | ⚠️ Средние |
| Redis Cluster | ✅ Очень высокая | ✅ Очень высокая | ❌ Высокая | ❌ Высокие |
| Fallback PostgreSQL | ⚠️ Средняя | ⚠️ Средняя | ⚠️ Средняя | ✅ Низкие |
| Circuit Breaker | ⚠️ Средняя | ✅ Высокая | ⚠️ Средняя | ✅ Низкие |

---

## 🚀 План внедрения

### Этап 1: Немедленные улучшения (1-2 дня)

1. **Включить персистентность Redis**:
   ```yaml
   command: ["redis-server", "--appendonly", "yes", "--save", "60 1000"]
   ```

2. **Добавить обработку ошибок**:
   ```go
   if err := pipe.Exec(redisCtx); err != nil {
       log.Printf("⚠️ Redis error: %v", err)
       // Fallback на PostgreSQL для критичных операций
   }
   ```

3. **Добавить health checks**:
   ```go
   func (r *RedisClient) HealthCheck() error {
       ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
       defer cancel()
       return r.client.Ping(ctx).Err()
   }
   ```

### Этап 2: Redis Sentinel (1 неделя)

1. Настроить Redis Sentinel
2. Обновить код для работы с Sentinel
3. Протестировать failover

### Этап 3: Circuit Breaker (1 неделя)

1. Реализовать Circuit Breaker
2. Добавить fallback на PostgreSQL
3. Мониторинг и алерты

---

## 📝 Вывод

**Текущая ситуация**: Redis — **single point of failure**

**Риски**:
- ❌ При падении Redis система не может создавать заказы (слоты)
- ❌ Нет автоматического восстановления
- ❌ Потеря данных в памяти (нет персистентности)

**Рекомендация**: 
1. **Краткосрочно**: Включить персистентность + обработка ошибок
2. **Среднесрочно**: Redis Sentinel для HA
3. **Долгосрочно**: Circuit Breaker + Fallback на PostgreSQL

