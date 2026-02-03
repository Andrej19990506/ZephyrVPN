package database

import (
	"fmt"
	"log"
	"strings"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var DB *gorm.DB

// normalizeDatabaseURL нормализует DATABASE_URL для GORM
// Railway предоставляет postgresql://, но GORM ожидает postgres://
func normalizeDatabaseURL(url string) string {
	// Заменяем postgresql:// на postgres:// для совместимости с GORM
	if strings.HasPrefix(url, "postgresql://") {
		url = strings.Replace(url, "postgresql://", "postgres://", 1)
		log.Printf("🔧 Нормализован DATABASE_URL: postgresql:// → postgres://")
	}
	return url
}

// ConnectPostgres подключается к PostgreSQL и возвращает *gorm.DB
func ConnectPostgres(databaseURL string) (*gorm.DB, error) {
	if databaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is empty")
	}

	// Нормализуем URL для GORM (Railway использует postgresql://, GORM ожидает postgres://)
	normalizedURL := normalizeDatabaseURL(databaseURL)
	
	// Логируем подключение (без пароля)
	safeURL := normalizedURL
	if idx := strings.Index(safeURL, "@"); idx > 0 {
		if schemeIdx := strings.Index(safeURL, "://"); schemeIdx > 0 {
			safeURL = safeURL[:schemeIdx+3] + "***@" + safeURL[idx+1:]
		}
	}
	log.Printf("🔄 Подключение к PostgreSQL: %s", safeURL)

	// Настройки GORM для production
	config := &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent), // Отключаем логи для скорости
		NowFunc: func() time.Time {
			return time.Now().UTC()
		},
	}

	db, err := gorm.Open(postgres.Open(normalizedURL), config)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// Настройка connection pool для highload
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get sql.DB: %w", err)
	}

	// Оптимизация для highload
	sqlDB.SetMaxOpenConns(25)        // Максимум открытых соединений
	sqlDB.SetMaxIdleConns(10)        // Максимум idle соединений
	sqlDB.SetConnMaxLifetime(5 * time.Minute) // Время жизни соединения
	sqlDB.SetConnMaxIdleTime(1 * time.Minute) // Время простоя idle соединения

	// Проверяем подключение
	if err := sqlDB.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	DB = db
	log.Println("✅ PostgreSQL подключен успешно")
	return db, nil
}

// ClosePostgres закрывает соединение с PostgreSQL
func ClosePostgres(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}







