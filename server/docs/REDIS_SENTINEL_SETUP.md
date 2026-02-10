# Настройка Redis Sentinel для отказоустойчивости

## 📋 Оглавление

1. [Обзор архитектуры](#обзор-архитектуры)
2. [Установка и запуск](#установка-и-запуск)
3. [Обновление Go-кода](#обновление-go-кода)
4. [Тестирование failover](#тестирование-failover)
5. [Мониторинг](#мониторинг)

---

## 🏗 Обзор архитектуры

### Компоненты

1. **Redis Master** (1 экземпляр)
   - Основной сервер для записи
   - Без персистентности (максимальная производительность)
   - Порт: 6379

2. **Redis Replicas** (2 экземпляра)
   - Реплики мастера для чтения
   - С персистентностью (AOF + RDB)
   - Автоматическое переключение на мастер при failover

3. **Redis Sentinel** (3 экземпляра)
   - Мониторинг мастера и реплик
   - Автоматический failover
   - Порты: 26379, 26380, 26381

### Схема работы

```
┌─────────────────┐
│  Go Application │
│   (API Server)  │
└────────┬────────┘
         │
         │ Запросы через Sentinel
         ▼
┌─────────────────────────────────────┐
│      Redis Sentinel Cluster         │
│  ┌──────────┐  ┌──────────┐        │
│  │Sentinel 1│  │Sentinel 2│  ...   │
│  └──────────┘  └──────────┘        │
└────────┬────────────────────────────┘
         │ Определяет текущего мастера
         ▼
┌─────────────────┐
│  Redis Master   │ ◄───┐
│   (Write)       │     │ Репликация
└─────────────────┘     │
                        │
         ┌──────────────┴──────────────┐
         │                             │
┌────────▼────────┐          ┌─────────▼────────┐
│ Redis Replica 1 │          │ Redis Replica 2  │
│   (Read)        │          │   (Read)         │
└─────────────────┘          └──────────────────┘
```

---

## 🚀 Установка и запуск

### Шаг 1: Создать файл конфигурации Sentinel

Создайте файл `redis-sentinel.conf` в корне проекта:

```bash
# redis-sentinel.conf
port 26379
sentinel monitor mymaster redis-master 6379 2
sentinel down-after-milliseconds mymaster 5000
sentinel failover-timeout mymaster 60000
sentinel parallel-syncs mymaster 1
loglevel notice
```

### Шаг 2: Запустить сервисы

```bash
# Запустить все сервисы
docker-compose up -d

# Проверить статус
docker-compose ps

# Проверить логи Sentinel
docker-compose logs redis-sentinel-1
```

### Шаг 4: Проверить конфигурацию Sentinel

```bash
# Подключиться к Sentinel
docker exec -it zephyrvpn_redis_sentinel_1 redis-cli -p 26379

# Проверить информацию о мастере
SENTINEL masters

# Проверить реплики
SENTINEL replicas mymaster

# Проверить Sentinel узлы
SENTINEL sentinels mymaster
```

---

## 💻 Обновление Go-кода

### Шаг 1: Обновить `internal/database/redis.go`

**Текущий код** (подключение напрямую к Redis):
```go
func ConnectRedis(redisURL string) (*redis.Client, error) {
    opt, err := redis.ParseURL(redisURL)
    if err != nil {
        return nil, fmt.Errorf("failed to parse Redis URL: %w", err)
    }
    
    client := redis.NewClient(opt)
    // ...
}
```

**Новый код** (подключение через Sentinel):
```go
package database

import (
    "context"
    "fmt"
    "log"
    "strings"
    "time"

    "github.com/redis/go-redis/v9"
)

// ConnectRedis подключается к Redis (с поддержкой Sentinel)
func ConnectRedis(redisURL string, sentinelAddrs []string, masterName string) (*redis.Client, error) {
    // Если указаны адреса Sentinel, используем их
    if len(sentinelAddrs) > 0 && masterName != "" {
        return ConnectRedisWithSentinel(sentinelAddrs, masterName, "")
    }
    
    // Иначе используем прямое подключение (fallback)
    opt, err := redis.ParseURL(redisURL)
    if err != nil {
        return nil, fmt.Errorf("failed to parse Redis URL: %w", err)
    }

    opt.PoolSize = 1000
    opt.MinIdleConns = 50
    opt.MaxRetries = 3

    client := redis.NewClient(opt)

    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()

    if err := client.Ping(ctx).Err(); err != nil {
        return nil, fmt.Errorf("failed to connect to Redis: %w", err)
    }

    log.Println("✅ Redis connected successfully (direct connection)")
    return client, nil
}

// ConnectRedisWithSentinel подключается к Redis через Sentinel
func ConnectRedisWithSentinel(sentinelAddrs []string, masterName, password string) (*redis.Client, error) {
    // Парсим адреса Sentinel (может быть строка через запятую или массив)
    var addrs []string
    if len(sentinelAddrs) == 1 && strings.Contains(sentinelAddrs[0], ",") {
        // Если передан один элемент с запятыми, разбиваем
        addrs = strings.Split(sentinelAddrs[0], ",")
        // Убираем пробелы
        for i := range addrs {
            addrs[i] = strings.TrimSpace(addrs[i])
        }
    } else {
        addrs = sentinelAddrs
    }

    opt := &redis.FailoverOptions{
        MasterName:    masterName,
        SentinelAddrs: addrs,
        Password:      password,
        PoolSize:      1000,
        MinIdleConns:  50,
        MaxRetries:    3,
        // Настройки для автоматического переподключения
        DialTimeout:  5 * time.Second,
        ReadTimeout:  3 * time.Second,
        WriteTimeout: 3 * time.Second,
    }

    client := redis.NewFailoverClient(opt)

    // Проверяем подключение
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    if err := client.Ping(ctx).Err(); err != nil {
        return nil, fmt.Errorf("failed to connect to Redis Sentinel: %w", err)
    }

    log.Printf("✅ Redis Sentinel connected successfully (master: %s, sentinels: %v)", masterName, addrs)
    return client, nil
}

// CloseRedis закрывает подключение к Redis
func CloseRedis(client *redis.Client) error {
    if client != nil {
        return client.Close()
    }
    return nil
}
```

### Шаг 2: Обновить `internal/config/config.go`

Добавить переменные окружения для Sentinel:

```go
// В структуру Config добавить:
type Config struct {
    // ... существующие поля ...
    
    RedisSentinelAddrs []string // Адреса Sentinel (через запятую)
    RedisMasterName    string   // Имя мастера в Sentinel
}

// В функцию LoadConfig добавить:
func LoadConfig() (*Config, error) {
    // ... существующий код ...
    
    // Redis Sentinel настройки
    sentinelAddrsStr := os.Getenv("REDIS_SENTINEL_ADDRS")
    var sentinelAddrs []string
    if sentinelAddrsStr != "" {
        sentinelAddrs = strings.Split(sentinelAddrsStr, ",")
        for i := range sentinelAddrs {
            sentinelAddrs[i] = strings.TrimSpace(sentinelAddrs[i])
        }
    }
    
    masterName := os.Getenv("REDIS_MASTER_NAME")
    if masterName == "" {
        masterName = "mymaster" // Дефолтное значение
    }
    
    config.RedisSentinelAddrs = sentinelAddrs
    config.RedisMasterName = masterName
    
    // ... остальной код ...
}
```

### Шаг 3: Обновить `main.go`

Изменить вызов `ConnectRedis`:

```go
// Старый код:
// redisClient, err := database.ConnectRedis(cfg.RedisURL)

// Новый код:
redisClient, err := database.ConnectRedis(
    cfg.RedisURL,
    cfg.RedisSentinelAddrs,
    cfg.RedisMasterName,
)
if err != nil {
    log.Fatalf("Failed to connect to Redis: %v", err)
}
```

---

## 🧪 Тестирование failover

### Тест 1: Проверка работы Sentinel

```bash
# Подключиться к Sentinel
docker exec -it zephyrvpn_redis_sentinel_1 redis-cli -p 26379

# Проверить текущего мастера
SENTINEL get-master-addr-by-name mymaster

# Должен вернуть: 1) "redis-master" 2) "6379"
```

### Тест 2: Симуляция падения мастера

```bash
# Остановить мастер
docker stop zephyrvpn_redis_master

# Подождать 5-10 секунд (Sentinel обнаружит падение)

# Проверить, кто стал новым мастером
docker exec -it zephyrvpn_redis_sentinel_1 redis-cli -p 26379 SENTINEL get-master-addr-by-name mymaster

# Должен вернуть адрес одной из реплик
```

### Тест 3: Проверка работы приложения

```bash
# Проверить логи API
docker logs zephyrvpn_api

# Должны быть сообщения о переподключении к новому мастеру
# Приложение должно продолжать работать без ошибок
```

### Тест 4: Восстановление мастера

```bash
# Запустить старый мастер обратно
docker start zephyrvpn_redis_master

# Он автоматически станет репликой нового мастера
docker exec -it zephyrvpn_redis_master redis-cli INFO replication
```

---

## 📊 Мониторинг

### Команды для мониторинга

```bash
# Статус всех Redis узлов
docker exec -it zephyrvpn_redis_master redis-cli INFO replication
docker exec -it zephyrvpn_redis_replica_1 redis-cli INFO replication
docker exec -it zephyrvpn_redis_replica_2 redis-cli INFO replication

# Статус Sentinel
docker exec -it zephyrvpn_redis_sentinel_1 redis-cli -p 26379 SENTINEL masters
docker exec -it zephyrvpn_redis_sentinel_1 redis-cli -p 26379 SENTINEL replicas mymaster
docker exec -it zephyrvpn_redis_sentinel_1 redis-cli -p 26379 SENTINEL sentinels mymaster

# Проверить текущего мастера
docker exec -it zephyrvpn_redis_sentinel_1 redis-cli -p 26379 SENTINEL get-master-addr-by-name mymaster
```

### Логи для мониторинга

```bash
# Логи мастера
docker logs -f zephyrvpn_redis_master

# Логи реплик
docker logs -f zephyrvpn_redis_replica_1
docker logs -f zephyrvpn_redis_replica_2

# Логи Sentinel
docker logs -f zephyrvpn_redis_sentinel_1
docker logs -f zephyrvpn_redis_sentinel_2
docker logs -f zephyrvpn_redis_sentinel_3
```

---

## 🔧 Настройка переменных окружения

### В docker-compose.yml

```yaml
api:
  environment:
    # Адреса Sentinel (через запятую)
    REDIS_SENTINEL_ADDRS: redis-sentinel-1:26379,redis-sentinel-2:26379,redis-sentinel-3:26379
    # Имя мастера в Sentinel
    REDIS_MASTER_NAME: mymaster
    # Fallback URL (для обратной совместимости)
    REDIS_URL: redis://redis-master:6379/0
```

### Локальная разработка (.env)

```env
REDIS_SENTINEL_ADDRS=localhost:26379,localhost:26380,localhost:26381
REDIS_MASTER_NAME=mymaster
REDIS_URL=redis://localhost:6379/0
```

---

## ⚠️ Важные замечания

### 1. Quorum Sentinel

Для принятия решения о failover нужно минимум 2 голоса из 3 Sentinel. Это означает:
- Если упадет 1 Sentinel — система продолжит работать
- Если упадут 2 Sentinel — failover не произойдет (недостаточно голосов)

### 2. Время обнаружения падения

- `down-after-milliseconds: 5000` — мастер считается упавшим через 5 секунд
- `failover-timeout: 60000` — failover должен завершиться за 60 секунд

### 3. Производительность

- Master без персистентности — максимальная скорость записи
- Replicas с персистентностью — данные сохраняются на диск

### 4. Сеть Docker

Все сервисы должны быть в одной сети (`redis-network`) для корректной работы Sentinel.

---

## 🐛 Решение проблем

### Проблема: Sentinel не видит мастера

**Решение**:
```bash
# Проверить, что все сервисы в одной сети
docker network inspect zephyrvpn_redis_network

# Проверить конфигурацию Sentinel
docker exec -it zephyrvpn_redis_sentinel_1 cat /etc/redis/sentinel.conf
```

### Проблема: Failover не происходит

**Решение**:
```bash
# Проверить количество доступных Sentinel
docker exec -it zephyrvpn_redis_sentinel_1 redis-cli -p 26379 SENTINEL sentinels mymaster

# Должно быть минимум 2 Sentinel (для quorum=2)
```

### Проблема: Приложение не подключается

**Решение**:
```bash
# Проверить переменные окружения
docker exec -it zephyrvpn_api env | grep REDIS

# Проверить логи приложения
docker logs zephyrvpn_api | grep -i redis
```

---

## 📝 Резюме

### Что получили:

1. ✅ **Высокая доступность** — автоматический failover при падении мастера
2. ✅ **Производительность** — мастер без персистентности для максимальной скорости
3. ✅ **Надежность** — реплики с персистентностью для сохранения данных
4. ✅ **Масштабируемость** — чтение с реплик распределяет нагрузку

### Следующие шаги:

1. Протестировать failover в staging окружении
2. Настроить мониторинг и алерты
3. Документировать процедуры восстановления
4. Рассмотреть Redis Cluster для горизонтального масштабирования

