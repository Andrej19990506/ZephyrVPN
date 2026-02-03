package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	DatabaseURL    string
	RedisURL       string
	KafkaBrokers   string
	KafkaUsername  string
	KafkaPassword  string
	KafkaCACert    string
	JWTSecret      string
	ServerPort     string
	Environment    string
	OpenVPNPath    string
	WireGuardPath  string
	// Рабочие часы кухни (в UTC, клиент сам конвертирует в свой часовой пояс)
	BusinessOpenHour  int // Час открытия в UTC (по умолчанию 2 = 9:00 KRAT)
	BusinessCloseHour int // Час закрытия в UTC (по умолчанию 16 = 23:00 KRAT)
	BusinessCloseMin  int // Минута закрытия в UTC (по умолчанию 45)
}

func Load() *Config {
	// Railway может использовать разные имена переменных для PostgreSQL
	// Проверяем в порядке приоритета: DATABASE_URL, POSTGRES_URL, PGDATABASE_URL, PGHOST (сборка из частей)
	databaseURL := getEnv("DATABASE_URL", "")
	if databaseURL == "" {
		databaseURL = getEnv("POSTGRES_URL", "")
	}
	if databaseURL == "" {
		databaseURL = getEnv("PGDATABASE_URL", "")
	}
	// Если нет полного URL, пытаемся собрать из отдельных переменных (Railway иногда так делает)
	if databaseURL == "" {
		pgHost := getEnv("PGHOST", "")
		pgPort := getEnv("PGPORT", "5432")
		pgUser := getEnv("PGUSER", "postgres")
		pgPassword := getEnv("PGPASSWORD", "")
		pgDatabase := getEnv("PGDATABASE", "zephyrvpn")
		
		if pgHost != "" {
			if pgPassword != "" {
				databaseURL = fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable",
					pgUser, pgPassword, pgHost, pgPort, pgDatabase)
			} else {
				databaseURL = fmt.Sprintf("postgres://%s@%s:%s/%s?sslmode=disable",
					pgUser, pgHost, pgPort, pgDatabase)
			}
		}
	}
	if databaseURL == "" {
		databaseURL = "postgres://user:password@localhost/zephyrvpn?sslmode=disable" // Fallback
	}

	// Railway может использовать разные имена переменных для Redis
	// Проверяем в порядке приоритета: REDIS_URL, REDISCLOUD_URL, REDISHOST (сборка из частей)
	redisURL := getEnv("REDIS_URL", "")
	if redisURL == "" {
		redisURL = getEnv("REDISCLOUD_URL", "")
	}
	// Если нет полного URL, пытаемся собрать из отдельных переменных
	if redisURL == "" {
		redisHost := getEnv("REDISHOST", "")
		redisPort := getEnv("REDISPORT", "6379")
		redisPassword := getEnv("REDISPASSWORD", "")
		redisDB := getEnv("REDISDB", "0")
		
		if redisHost != "" {
			if redisPassword != "" {
				redisURL = fmt.Sprintf("redis://:%s@%s:%s/%s", redisPassword, redisHost, redisPort, redisDB)
			} else {
				redisURL = fmt.Sprintf("redis://%s:%s/%s", redisHost, redisPort, redisDB)
			}
		}
	}
	if redisURL == "" {
		redisURL = "redis://localhost:6379/0" // Fallback
	}

	return &Config{
		DatabaseURL:      databaseURL,
		RedisURL:         redisURL,
		KafkaBrokers:     getEnv("KAFKA_BROKERS", "localhost:9092"),
		KafkaUsername:    getEnv("KAFKA_USERNAME", ""),
		KafkaPassword:    getEnv("KAFKA_PASSWORD", ""),
		KafkaCACert:       getEnv("KAFKA_CA_CERT", ""),
		JWTSecret:        getEnv("JWT_SECRET", "your-secret-key-change-in-production"),
		ServerPort:       getEnv("PORT", "8080"),
		Environment:      getEnv("ENV", "development"),
		OpenVPNPath:      getEnv("OPENVPN_PATH", "/usr/sbin/openvpn"),
		WireGuardPath:    getEnv("WIREGUARD_PATH", "/usr/bin/wg"),
		BusinessOpenHour: getEnvInt("BUSINESS_OPEN_HOUR", 2),   // 2:00 UTC = 9:00 KRAT (UTC+7)
		BusinessCloseHour: getEnvInt("BUSINESS_CLOSE_HOUR", 16), // 16:00 UTC = 23:00 KRAT (UTC+7)
		BusinessCloseMin:  getEnvInt("BUSINESS_CLOSE_MIN", 45),   // 16:45 UTC = 23:45 KRAT (UTC+7)
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

