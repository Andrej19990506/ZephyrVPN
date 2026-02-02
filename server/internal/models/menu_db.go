package models

import (
	"log"
	"strings"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// PizzaRecipe - таблица рецептов пицц в БД
type PizzaRecipe struct {
	ID                uint   `gorm:"primaryKey"`
	Name              string `gorm:"uniqueIndex:name;not null"` // Явное имя индекса для совместимости с SQL миграцией
	Price             int    `gorm:"not null"` // в рублях
	Ingredients       string `gorm:"type:text"` // JSON массив ингредиентов
	IngredientAmounts string `gorm:"type:text"` // JSON map ингредиент -> граммы
	IsActive          bool   `gorm:"default:true"`
	CreatedAt         int64  `gorm:"autoCreateTime"`
	UpdatedAt         int64  `gorm:"autoUpdateTime"`
}

// PizzaSetDB - таблица наборов пицц в БД
type PizzaSetDB struct {
	ID          uint   `gorm:"primaryKey"`
	Name        string `gorm:"uniqueIndex:name;not null"` // Явное имя индекса
	Description string `gorm:"type:text"`
	Pizzas      string `gorm:"type:text"` // JSON массив названий пицц
	Price       int    `gorm:"not null"`  // Общая цена набора
	IsActive    bool   `gorm:"default:true"`
	CreatedAt   int64  `gorm:"autoCreateTime"`
	UpdatedAt   int64  `gorm:"autoUpdateTime"`
}

// ExtraDB - таблица допов в БД
type ExtraDB struct {
	ID        uint   `gorm:"primaryKey"`
	Name      string `gorm:"uniqueIndex:name;not null"` // Явное имя индекса
	Price     int    `gorm:"not null"` // в рублях
	IsActive  bool   `gorm:"default:true"`
	CreatedAt int64  `gorm:"autoCreateTime"`
	UpdatedAt int64  `gorm:"autoUpdateTime"`
}

// TableName для правильных имен таблиц
func (PizzaRecipe) TableName() string {
	return "pizza_recipes"
}

func (PizzaSetDB) TableName() string {
	return "pizza_sets"
}

func (ExtraDB) TableName() string {
	return "extras"
}

// AutoMigrate создает таблицы в БД
// Игнорирует ошибки constraint, так как таблицы уже созданы через SQL миграцию
func AutoMigrate(db *gorm.DB) error {
	// Сначала мигрируем существующие таблицы
	err := db.AutoMigrate(
		&PizzaRecipe{},
		&PizzaSetDB{},
		&ExtraDB{},
	)
	if err != nil {
		errStr := err.Error()
		// Игнорируем только ошибки constraint "does not exist" (не критично)
		if !(contains(errStr, "constraint") && contains(errStr, "does not exist") && contains(errStr, "SQLSTATE 42704")) {
			log.Printf("⚠️ AutoMigrate для существующих таблиц: %v", err)
		}
	}

	// Отдельно мигрируем Station, чтобы видеть ошибки
	if err := db.AutoMigrate(&Station{}); err != nil {
		log.Printf("❌ AutoMigrate для Station failed: %v", err)
		return err
	}
	log.Println("✅ Station table migrated successfully")
	
	// Мигрируем Role
	if err := db.AutoMigrate(&Role{}); err != nil {
		log.Printf("❌ AutoMigrate для Role failed: %v", err)
		return err
	}
	log.Println("✅ Role table migrated successfully")
	
	// Инициализируем роли по умолчанию
	if err := InitDefaultRoles(db); err != nil {
		log.Printf("⚠️ Ошибка инициализации ролей: %v", err)
	}
	
	// Мигрируем Staff с обработкой существующих данных
	// Сначала проверяем, существует ли колонка role_name
	var roleNameExists bool
	err = db.Raw("SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'staff' AND column_name = 'role_name')").Scan(&roleNameExists).Error
	if err == nil && !roleNameExists {
		// Колонки нет, добавляем её как nullable сначала
		if err := db.Exec("ALTER TABLE staff ADD COLUMN role_name VARCHAR(100)").Error; err != nil {
			log.Printf("⚠️ Не удалось добавить колонку role_name: %v", err)
		} else {
			// Заполняем существующие записи значением по умолчанию
			if err := db.Exec("UPDATE staff SET role_name = 'employee' WHERE role_name IS NULL").Error; err != nil {
				log.Printf("⚠️ Не удалось заполнить role_name: %v", err)
			}
			// Теперь делаем колонку NOT NULL
			if err := db.Exec("ALTER TABLE staff ALTER COLUMN role_name SET NOT NULL").Error; err != nil {
				log.Printf("⚠️ Не удалось сделать role_name NOT NULL: %v", err)
			}
		}
	}
	
	// Теперь выполняем AutoMigrate
	if err := db.AutoMigrate(&Staff{}); err != nil {
		log.Printf("⚠️ AutoMigrate для Staff failed: %v (continuing)", err)
		// Не возвращаем ошибку, продолжаем миграцию других таблиц
	} else {
		log.Println("✅ Staff table migrated successfully")
	}
	
	// Мигрируем NomenclatureCategory
	if err := db.AutoMigrate(&NomenclatureCategory{}); err != nil {
		log.Printf("❌ AutoMigrate для NomenclatureCategory failed: %v", err)
		return err
	}
	log.Println("✅ NomenclatureCategory table migrated successfully")
	
	// Мигрируем NomenclatureItem
	if err := db.AutoMigrate(&NomenclatureItem{}); err != nil {
		log.Printf("❌ AutoMigrate для NomenclatureItem failed: %v", err)
		return err
	}
	log.Println("✅ NomenclatureItem table migrated successfully")

	// Мигрируем PLUCode
	if err := db.AutoMigrate(&PLUCode{}); err != nil {
		log.Printf("❌ AutoMigrate для PLUCode failed: %v", err)
		return err
	}
	log.Println("✅ PLUCode table migrated successfully")

	// Мигрируем StockBatch
	if err := db.AutoMigrate(&StockBatch{}); err != nil {
		log.Printf("❌ AutoMigrate для StockBatch failed: %v", err)
		return err
	}
	log.Println("✅ StockBatch table migrated successfully")

	// Мигрируем Recipe
	if err := db.AutoMigrate(&Recipe{}); err != nil {
		log.Printf("❌ AutoMigrate для Recipe failed: %v", err)
		return err
	}
	log.Println("✅ Recipe table migrated successfully")

	// Мигрируем RecipeIngredient
	if err := db.AutoMigrate(&RecipeIngredient{}); err != nil {
		log.Printf("❌ AutoMigrate для RecipeIngredient failed: %v", err)
		return err
	}
	log.Println("✅ RecipeIngredient table migrated successfully")

	// Мигрируем StockMovement
	if err := db.AutoMigrate(&StockMovement{}); err != nil {
		log.Printf("❌ AutoMigrate для StockMovement failed: %v", err)
		return err
	}
	log.Println("✅ StockMovement table migrated successfully")

	// Мигрируем ExpiryAlert
	if err := db.AutoMigrate(&ExpiryAlert{}); err != nil {
		log.Printf("❌ AutoMigrate для ExpiryAlert failed: %v", err)
		return err
	}
	log.Println("✅ ExpiryAlert table migrated successfully")

	// Мигрируем Counterparty
	if err := db.AutoMigrate(&Counterparty{}); err != nil {
		log.Printf("❌ AutoMigrate для Counterparty failed: %v", err)
		return err
	}
	log.Println("✅ Counterparty table migrated successfully")

	// Мигрируем Invoice (должна быть создана перед StockBatch, StockMovement, FinanceTransaction)
	if err := db.AutoMigrate(&Invoice{}); err != nil {
		log.Printf("❌ AutoMigrate для Invoice failed: %v", err)
		return err
	}
	log.Println("✅ Invoice table migrated successfully")

	// Мигрируем FinanceTransaction
	if err := db.AutoMigrate(&FinanceTransaction{}); err != nil {
		log.Printf("❌ AutoMigrate для FinanceTransaction failed: %v", err)
		return err
	}
	log.Println("✅ FinanceTransaction table migrated successfully")

	// Мигрируем LegalEntity
	if err := db.AutoMigrate(&LegalEntity{}); err != nil {
		log.Printf("❌ AutoMigrate для LegalEntity failed: %v", err)
		return err
	}
	log.Println("✅ LegalEntity table migrated successfully")

	// Мигрируем SuperAdmin
	if err := db.AutoMigrate(&SuperAdmin{}); err != nil {
		log.Printf("❌ AutoMigrate для SuperAdmin failed: %v", err)
		return err
	}
	log.Println("✅ SuperAdmin table migrated successfully")

	// Мигрируем Branch
	if err := db.AutoMigrate(&Branch{}); err != nil {
		log.Printf("❌ AutoMigrate для Branch failed: %v", err)
		return err
	}
	log.Println("✅ Branch table migrated successfully")

	// Инициализируем дефолтные данные
	if err := InitDefaultData(db); err != nil {
		log.Printf("⚠️ Ошибка инициализации дефолтных данных: %v", err)
	}
	
	return nil
}

// InitDefaultData создает дефолтные данные: ИП и супер-админа
func InitDefaultData(db *gorm.DB) error {
	// Создаем дефолтное ИП "Юсупов"
	var yusupovEntity LegalEntity
	result := db.Where("name = ?", "Юсупов").First(&yusupovEntity)
	
	var yusupovID string
	if result.Error == gorm.ErrRecordNotFound {
		// Создаем ИП "Юсупов"
		yusupovEntity = LegalEntity{
			Name:     "Юсупов",
			INN:      "", // Можно заполнить позже
			Type:     "IP",
			IsActive: true,
		}
		if err := db.Create(&yusupovEntity).Error; err != nil {
			return err
		}
		yusupovID = yusupovEntity.ID
		log.Println("✅ Создано дефолтное ИП: Юсупов")
	} else if result.Error == nil {
		yusupovID = yusupovEntity.ID
		log.Println("✅ ИП 'Юсупов' уже существует")
	} else {
		return result.Error
	}

	// Проверяем, есть ли уже админ с логином "admin"
	var existingAdmin SuperAdmin
	result = db.Where("username = ?", "admin").First(&existingAdmin)
	
	if result.Error == nil {
		// Админ уже существует, обновляем его ИП если не установлен
		if existingAdmin.LegalEntityID == nil {
			existingAdmin.LegalEntityID = &yusupovID
			if err := db.Save(&existingAdmin).Error; err != nil {
				log.Printf("⚠️ Ошибка обновления ИП для админа: %v", err)
			} else {
				log.Println("✅ Обновлен ИП для дефолтного супер-админа")
			}
		}
		return nil
	}
	
	if result.Error != gorm.ErrRecordNotFound {
		// Другая ошибка
		return result.Error
	}
	
	// Хешируем пароль "admin"
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("admin"), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	
	// Создаем дефолтного админа с ИП "Юсупов"
	admin := SuperAdmin{
		Username:      "admin",
		PasswordHash:  string(passwordHash),
		LegalEntityID: &yusupovID,
		IsActive:      true,
	}
	
	if err := db.Create(&admin).Error; err != nil {
		return err
	}
	
	log.Printf("✅ Создан дефолтный супер-админ: username=admin, password=admin, ИП=Юсупов")
	return nil
}

// contains проверяет наличие подстроки
func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}


