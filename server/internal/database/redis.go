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
// Если указаны sentinelAddrs и masterName, используется Sentinel
// Иначе используется прямое подключение через redisURL
func ConnectRedis(redisURL string, sentinelAddrs []string, masterName string) (*redis.Client, error) {
	// Если указаны адреса Sentinel, используем их
	if len(sentinelAddrs) > 0 && masterName != "" {
		return ConnectRedisWithSentinel(sentinelAddrs, masterName, "")
	}

	// Иначе используем прямое подключение (fallback или для разработки)
	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse Redis URL: %w", err)
	}

	// Используем распарсенные опции и переопределяем настройки пула
	opt.PoolSize = 1000      // Увеличиваем до 1000 (дефолт всего 10 на ядро)
	opt.MinIdleConns = 50    // Держим 50 соединений всегда готовыми
	opt.MaxRetries = 3       // Если не достучался — попробуй еще раз

	client := redis.NewClient(opt)

	// Проверяем подключение
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

	if len(addrs) == 0 {
		return nil, fmt.Errorf("no Sentinel addresses provided")
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

	// Проверяем подключение (увеличиваем таймаут для Sentinel)
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

