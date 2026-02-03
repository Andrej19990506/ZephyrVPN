package database

import (
	"context"
	"fmt"
	"log"
	"net/url"
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

	// Если это просто host:port (без схемы), используем как есть
	if !strings.Contains(redisURL, "://") {
		log.Printf("🔄 Подключение к Redis: %s (простой адрес)", redisURL)
		client := redis.NewClient(&redis.Options{
			Addr:         redisURL,
			PoolSize:     1000,
			MinIdleConns: 50,
			MaxRetries:   3,
		})
		
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		
		if err := client.Ping(ctx).Err(); err != nil {
			return nil, fmt.Errorf("failed to connect to Redis: %w", err)
		}
		
		log.Println("✅ Redis connected successfully")
		return client, nil
	}

	// Парсим URL используя стандартный парсер Go
	parsedURL, err := url.Parse(redisURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse Redis URL: %w", err)
	}

	// Извлекаем компоненты
	addr := parsedURL.Host
	if parsedURL.Port() == "" {
		// Если порт не указан, используем стандартный для Redis
		if parsedURL.Scheme == "rediss" {
			addr = parsedURL.Hostname() + ":6380" // TLS порт по умолчанию
		} else {
			addr = parsedURL.Hostname() + ":6379" // Стандартный порт Redis
		}
	}

	// Пароль из UserInfo
	password, _ := parsedURL.User.Password()
	
	// DB номер из пути (например, /0, /1)
	db := 0
	if parsedURL.Path != "" && len(parsedURL.Path) > 1 {
		if dbNum, err := strconv.Atoi(parsedURL.Path[1:]); err == nil {
			db = dbNum
		}
	}

	// Логируем безопасную версию (без пароля)
	safeURL := redisURL
	if password != "" {
		if parsedURL.User != nil {
			username := parsedURL.User.Username()
			safeURL = strings.Replace(redisURL, password, "***", 1)
			if username != "" {
				// Заменяем username:password на username:***
				safeURL = strings.Replace(safeURL, username+":"+password, username+":***", 1)
			}
		}
	}
	log.Printf("🔄 Подключение к Redis: %s", safeURL)
	log.Printf("   📍 Адрес: %s, DB: %d", addr, db)

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

