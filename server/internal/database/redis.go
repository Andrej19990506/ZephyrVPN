package database

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
)

// ConnectRedis подключается к Redis
func ConnectRedis(redisURL string) (*redis.Client, error) {
	client := redis.NewClient(&redis.Options{
		Addr:         redisURL,
		PoolSize:     1000,          // Увеличиваем до 1000 (дефолт всего 10 на ядро)
		MinIdleConns: 50,            // Держим 50 соединений всегда готовыми
		MaxRetries:   3,             // Если не достучался — попробуй еще раз
	})

	// Проверяем подключение
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	log.Println("✅ Redis connected successfully")
	return client, nil
}

// CloseRedis закрывает подключение к Redis
func CloseRedis(client *redis.Client) error {
	if client != nil {
		return client.Close()
	}
	return nil
}

