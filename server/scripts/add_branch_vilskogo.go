package main

import (
	"fmt"
	"log"
	"os"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"zephyrvpn/server/internal/models"
)

func main() {
	// Получаем строку подключения из переменной окружения или используем дефолтную
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		// Дефолтные параметры для Docker Compose
		dsn = "host=postgres user=pizza_admin password=pizza_secure_pass_2024 dbname=pizza_db port=5432 sslmode=disable"
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalf("❌ Ошибка подключения к БД: %v", err)
	}

	// Находим или создаем ИП "Юсупов"
	var yusupovEntity models.LegalEntity
	result := db.Where("name = ?", "Юсупов").First(&yusupovEntity)
	
	var yusupovID string
	if result.Error == gorm.ErrRecordNotFound {
		// Создаем ИП "Юсупов"
		yusupovEntity = models.LegalEntity{
			Name:     "Юсупов",
			INN:      "",
			Type:     "IP",
			IsActive: true,
		}
		if err := db.Create(&yusupovEntity).Error; err != nil {
			log.Fatalf("❌ Ошибка создания ИП: %v", err)
		}
		yusupovID = yusupovEntity.ID
		fmt.Printf("✅ Создано ИП 'Юсупов' с ID: %s\n", yusupovID)
	} else if result.Error != nil {
		log.Fatalf("❌ Ошибка поиска ИП: %v", result.Error)
	} else {
		yusupovID = yusupovEntity.ID
		fmt.Printf("✅ Используется существующее ИП 'Юсупов' с ID: %s\n", yusupovID)
	}

	// Проверяем, не существует ли уже филиал
	var existingBranch models.Branch
	checkResult := db.Where("name = ? AND deleted_at IS NULL", "Вильского 34").First(&existingBranch)
	
	if checkResult.Error == nil {
		fmt.Printf("⚠️ Филиал 'Вильского 34' уже существует с ID: %s\n", existingBranch.ID)
		return
	}

	// Создаем филиал "Вильского 34"
	branch := models.Branch{
		Name:          "Вильского 34",
		Address:       "Вильского, 34",
		Phone:         "",
		Email:         "",
		LegalEntityID: &yusupovID,
		SuperAdminID:  nil, // Опционально
		IsActive:      true,
	}

	if err := db.Create(&branch).Error; err != nil {
		log.Fatalf("❌ Ошибка создания филиала: %v", err)
	}

	fmt.Printf("✅ Создан филиал 'Вильского 34' с ID: %s\n", branch.ID)
	fmt.Printf("   Адрес: %s\n", branch.Address)
	fmt.Printf("   ИП: %s (ID: %s)\n", yusupovEntity.Name, yusupovID)
}

