package database

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// ConnectRedis подключается к Redis
func ConnectRedis(redisURL string) (*redis.Client, error) {
	if redisURL == "" {
		return nil, fmt.Errorf("REDIS_URL is empty")
	}

	// Парсим Redis URL
	// Формат: redis://[password@]host:port[/db] или redis://host:port[/db]
	// go-redis ожидает только host:port, пароль и DB нужно извлечь отдельно
	var addr string
	var password string
	var db int

	// Если это полный URL (redis://...)
	if len(redisURL) > 7 && redisURL[:7] == "redis://" {
		// Убираем префикс redis://
		urlWithoutScheme := redisURL[7:]
		
		// Проверяем наличие пароля
		if atIdx := strings.Index(urlWithoutScheme, "@"); atIdx > 0 {
			password = urlWithoutScheme[:atIdx]
			urlWithoutScheme = urlWithoutScheme[atIdx+1:]
		}
		
		// Проверяем наличие DB номера
		if slashIdx := strings.Index(urlWithoutScheme, "/"); slashIdx > 0 {
			dbStr := urlWithoutScheme[slashIdx+1:]
			if dbNum, err := strconv.Atoi(dbStr); err == nil {
				db = dbNum
			}
			urlWithoutScheme = urlWithoutScheme[:slashIdx]
		}
		
		addr = urlWithoutScheme
	} else {
		// Если это просто host:port
		addr = redisURL
	}

	log.Printf("🔄 Подключение к Redis: %s (DB: %d)", addr, db)

	client := redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     password,
		DB:           db,
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

