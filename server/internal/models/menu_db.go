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
	ID                uint    `gorm:"primaryKey" json:"id"`
	Name              string  `gorm:"uniqueIndex:name;not null" json:"name"` // Явное имя индекса
	Price             int     `gorm:"not null" json:"price"` // в рублях
	PortionWeightGrams int    `gorm:"default:50;not null" json:"portion_weight_grams"` // Вес порции допа в граммах (best practice: точное списание)
	NomenclatureID    *string `gorm:"type:uuid;index" json:"nomenclature_id,omitempty"` // Связь с номенклатурой для простых допов
	RecipeID         *string `gorm:"type:uuid;index" json:"recipe_id,omitempty"` // Связь с рецептом для сложных допов (BOM)
	IsActive          bool    `gorm:"default:true" json:"is_active"`
	CreatedAt         int64   `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt         int64   `gorm:"autoUpdateTime" json:"updated_at"`
	
	// Связи
	Nomenclature  *NomenclatureItem `gorm:"foreignKey:NomenclatureID" json:"nomenclature,omitempty"`
	Recipe        *Recipe           `gorm:"foreignKey:RecipeID" json:"recipe,omitempty"`
}

// PizzaExtra - таблица связи пицца-доп
type PizzaExtra struct {
	ID          uint   `gorm:"primaryKey"`
	PizzaName   string `gorm:"index:idx_pizza_extras_pizza_name;not null"`
	ExtraID     uint   `gorm:"index:idx_pizza_extras_extra_id;not null"`
	IsDefault   bool   `gorm:"default:false"` // Доп доступен по умолчанию
	DisplayOrder int   `gorm:"default:0"`     // Порядок отображения
	CreatedAt int64  `gorm:"autoCreateTime"`
	UpdatedAt   int64  `gorm:"autoUpdateTime"`
	
	// Связи
	Extra ExtraDB `gorm:"foreignKey:ExtraID;references:ID"`
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

func (PizzaExtra) TableName() string {
	return "pizza_extras"
}

// AutoMigrate создает таблицы в БД
// Игнорирует ошибки constraint, так как таблицы уже созданы через SQL миграцию
func AutoMigrate(db *gorm.DB) error {
	// Сначала мигрируем существующие таблицы
	err := db.AutoMigrate(
		&PizzaRecipe{},
		&PizzaSetDB{},
		&ExtraDB{},
		&PizzaExtra{},
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
	
	// ПРИМЕЧАНИЕ: Staff мигрируется ниже (после User), так как теперь имеет UserID foreign key
	// Временная заглушка для обратной совместимости - будет удалена после миграции всех данных
	// if err := db.AutoMigrate(&Staff{}); err != nil {
	// 	log.Printf("⚠️ AutoMigrate для Staff failed: %v (continuing)", err)
	// } else {
	// 	log.Println("✅ Staff table migrated successfully")
	// }
	
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

	// Мигрируем RecipeNode (иерархическая структура папок для рецептов)
	if err := db.AutoMigrate(&RecipeNode{}); err != nil {
		log.Printf("❌ AutoMigrate для RecipeNode failed: %v", err)
		return err
	}
	log.Println("✅ RecipeNode table migrated successfully")

	// Мигрируем таблицы для Technologist Workspace
	if err := db.AutoMigrate(&RecipeVersion{}); err != nil {
		log.Printf("❌ AutoMigrate для RecipeVersion failed: %v", err)
		return err
	}
	log.Println("✅ RecipeVersion table migrated successfully")

	if err := db.AutoMigrate(&TrainingMaterial{}); err != nil {
		log.Printf("❌ AutoMigrate для TrainingMaterial failed: %v", err)
		return err
	}
	log.Println("✅ TrainingMaterial table migrated successfully")

	if err := db.AutoMigrate(&RecipeExam{}); err != nil {
		log.Printf("❌ AutoMigrate для RecipeExam failed: %v", err)
		return err
	}
	log.Println("✅ RecipeExam table migrated successfully")

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

	// Очищаем "осиротевшие" invoice_id перед добавлением foreign key constraint
	// Устанавливаем invoice_id в NULL для записей, где invoice_id не существует в invoices
	if db.Migrator().HasTable(&FinanceTransaction{}) {
		result := db.Exec(`
			UPDATE finance_transactions 
			SET invoice_id = NULL 
			WHERE invoice_id IS NOT NULL 
			AND invoice_id NOT IN (SELECT id FROM invoices)
		`)
		if result.Error != nil {
			log.Printf("⚠️ Очистка осиротевших invoice_id: %v (continuing)", result.Error)
		} else if result.RowsAffected > 0 {
			log.Printf("✅ Очищено %d осиротевших invoice_id в finance_transactions", result.RowsAffected)
		}
	}

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

	// ============================================
	// МИГРАЦИЯ ПОЛЬЗОВАТЕЛЕЙ И ПРОФИЛЕЙ
	// Порядок важен: сначала User (базовая таблица), потом профили (Staff, Customer)
	// ============================================

	// Мигрируем User (центральная таблица аутентификации для всех пользователей)
	// Должна быть первой, так как Staff и Customer ссылаются на User
	if err := db.AutoMigrate(&User{}); err != nil {
		log.Printf("❌ AutoMigrate для User failed: %v", err)
		return err
	}
	log.Println("✅ User table migrated successfully")

	// Мигрируем Customer (профиль клиента - связан с User через UserID)
	// Может существовать независимо от Staff (обычный клиент)
	if err := db.AutoMigrate(&Customer{}); err != nil {
		log.Printf("❌ AutoMigrate для Customer failed: %v", err)
		return err
	}
	log.Println("✅ Customer table migrated successfully (with UserID foreign key)")

	// Мигрируем CustomerAddress (адреса доставки клиентов)
	// Зависит от Customer
	if err := db.AutoMigrate(&CustomerAddress{}); err != nil {
		log.Printf("❌ AutoMigrate для CustomerAddress failed: %v", err)
		return err
	}
	log.Println("✅ CustomerAddress table migrated successfully")

	// Мигрируем Staff (профиль сотрудника - связан с User через UserID)
	// Должен мигрироваться ПОСЛЕ User, так как имеет foreign key на User
	// ПРИМЕЧАНИЕ: Старая миграция Staff (строка 115) закомментирована для избежания дубликата
	if err := db.AutoMigrate(&Staff{}); err != nil {
		log.Printf("❌ AutoMigrate для Staff failed: %v", err)
		return err
	}
	log.Println("✅ Staff table migrated successfully (with UserID foreign key)")

	// Мигрируем PurchaseOrder (должна быть создана перед ProcurementPlanItem)
	if err := db.AutoMigrate(&PurchaseOrder{}); err != nil {
		log.Printf("❌ AutoMigrate для PurchaseOrder failed: %v", err)
		return err
	}
	log.Println("✅ PurchaseOrder table migrated successfully")

	// Мигрируем PurchaseOrderItem
	if err := db.AutoMigrate(&PurchaseOrderItem{}); err != nil {
		log.Printf("❌ AutoMigrate для PurchaseOrderItem failed: %v", err)
		return err
	}
	log.Println("✅ PurchaseOrderItem table migrated successfully")

	// Мигрируем ProcurementPlan
	if err := db.AutoMigrate(&ProcurementPlan{}); err != nil {
		log.Printf("❌ AutoMigrate для ProcurementPlan failed: %v", err)
		return err
	}
	log.Println("✅ ProcurementPlan table migrated successfully")

	// Мигрируем ProcurementPlanItem
	if err := db.AutoMigrate(&ProcurementPlanItem{}); err != nil {
		log.Printf("❌ AutoMigrate для ProcurementPlanItem failed: %v", err)
		return err
	}
	log.Println("✅ ProcurementPlanItem table migrated successfully")

	// Мигрируем ProcurementHistory
	if err := db.AutoMigrate(&ProcurementHistory{}); err != nil {
		log.Printf("❌ AutoMigrate для ProcurementHistory failed: %v", err)
		return err
	}
	log.Println("✅ ProcurementHistory table migrated successfully")

	// Мигрируем DemandForecast
	if err := db.AutoMigrate(&DemandForecast{}); err != nil {
		log.Printf("❌ AutoMigrate для DemandForecast failed: %v", err)
		return err
	}
	log.Println("✅ DemandForecast table migrated successfully")

	// Мигрируем SupplierCatalogItem (каталог поставщиков)
	if err := db.AutoMigrate(&SupplierCatalogItem{}); err != nil {
		log.Printf("❌ AutoMigrate для SupplierCatalogItem failed: %v", err)
		return err
	}
	log.Println("✅ SupplierCatalogItem table migrated successfully")
	
	// Мигрируем UoMConversionRule (правила конвертации единиц измерения)
	if err := db.AutoMigrate(&UoMConversionRule{}); err != nil {
		log.Printf("❌ AutoMigrate для UoMConversionRule failed: %v", err)
		return err
	}
	log.Println("✅ UoMConversionRule table migrated successfully")

	// Мигрируем RevenuePlan (планы выручки для аналитики)
	if err := db.AutoMigrate(&RevenuePlan{}); err != nil {
		log.Printf("❌ AutoMigrate для RevenuePlan failed: %v", err)
		return err
	}
	log.Println("✅ RevenuePlan table migrated successfully")

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


