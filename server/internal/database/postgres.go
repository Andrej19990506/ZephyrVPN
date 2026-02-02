package database

import (
	"fmt"
	"log"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var DB *gorm.DB

// ConnectPostgres подключается к PostgreSQL и возвращает *gorm.DB
func ConnectPostgres(databaseURL string) (*gorm.DB, error) {
	if databaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is empty")
	}

	// Настройки GORM для production
	config := &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent), // Отключаем логи для скорости
		NowFunc: func() time.Time {
			return time.Now().UTC()
		},
	}

	db, err := gorm.Open(postgres.Open(databaseURL), config)
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







