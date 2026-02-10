package main

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"gorm.io/gorm"
	"zephyrvpn/server/internal/database"
	"zephyrvpn/server/internal/models"
)

func main() {
	// Загружаем переменные окружения
	if err := godotenv.Load(); err != nil {
		log.Printf("⚠️ .env файл не найден, используем переменные окружения системы")
	}

	// Получаем DATABASE_URL используя ту же логику, что и config.Load()
	// НО: для seed скрипта приоритет - использовать значения из docker-compose.yml
	databaseURL := os.Getenv("DATABASE_URL")
	
	// Если DATABASE_URL указывает на localhost с неправильными учетными данными,
	// переопределяем на правильные из docker-compose.yml
	if databaseURL != "" && (strings.Contains(databaseURL, "@localhost") || strings.Contains(databaseURL, "@127.0.0.1")) {
		// Проверяем, правильные ли учетные данные
		if strings.Contains(databaseURL, "user:") || strings.Contains(databaseURL, "/zephyrvpn") {
			log.Printf("⚠️ Обнаружен DATABASE_URL с неправильными учетными данными, используем значения из docker-compose.yml")
			databaseURL = "" // Сбрасываем, чтобы использовать правильные значения по умолчанию
		}
	}
	
	if databaseURL == "" {
		databaseURL = os.Getenv("POSTGRES_URL")
	}
	if databaseURL == "" {
		databaseURL = os.Getenv("PGDATABASE_URL")
	}
	// Если нет полного URL, пытаемся собрать из отдельных переменных
	if databaseURL == "" {
		pgHost := os.Getenv("PGHOST")
		pgPort := os.Getenv("PGPORT")
		if pgPort == "" {
			pgPort = "5432"
		}
		pgUser := os.Getenv("PGUSER")
		if pgUser == "" {
			pgUser = "pizza_admin" // Используем из docker-compose.yml
		}
		pgPassword := os.Getenv("PGPASSWORD")
		if pgPassword == "" {
			pgPassword = "pizza_secure_pass_2024" // Используем из docker-compose.yml
		}
		pgDatabase := os.Getenv("PGDATABASE")
		if pgDatabase == "" {
			pgDatabase = "pizza_db" // Используем из docker-compose.yml
		}
		
		if pgHost != "" {
			databaseURL = fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable",
				pgUser, pgPassword, pgHost, pgPort, pgDatabase)
		} else {
			// Используем значения по умолчанию из docker-compose.yml
			databaseURL = "postgres://pizza_admin:pizza_secure_pass_2024@localhost:5432/pizza_db?sslmode=disable"
		}
	}
	
	// Логируем безопасную версию URL
	safeURL := databaseURL
	if idx := strings.Index(safeURL, "@"); idx > 0 {
		if schemeIdx := strings.Index(safeURL, "://"); schemeIdx > 0 {
			safeURL = safeURL[:schemeIdx+3] + "***@" + safeURL[idx+1:]
		}
	}
	log.Printf("📋 Используется DATABASE_URL: %s", safeURL)

	// Подключаемся к БД используя ту же функцию, что и основное приложение
	db, err := database.ConnectPostgres(databaseURL)
	if err != nil {
		log.Fatalf("❌ Ошибка подключения к БД: %v", err)
		log.Fatalf("💡 Проверьте, что:")
		log.Fatalf("   1. PostgreSQL запущен")
		log.Fatalf("   2. DATABASE_URL установлен в .env файле или переменных окружения")
		log.Fatalf("   3. Пользователь и пароль корректны")
	}
	defer database.ClosePostgres(db)

	log.Println("✅ Подключение к БД установлено")

	// Начинаем транзакцию для филиала
	tx := db.Begin()
	
	// 1. Создаем тестовый филиал (если не существует)
	var branch models.Branch
	if err := tx.Where("is_active = ?", true).First(&branch).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			// Ищем или создаем LegalEntity для филиала
			var legalEntity models.LegalEntity
			if err := tx.Where("is_active = ?", true).First(&legalEntity).Error; err != nil {
				if err == gorm.ErrRecordNotFound {
					// Создаем тестовый LegalEntity (ID будет сгенерирован автоматически через BeforeCreate)
					legalEntity = models.LegalEntity{
						// ID не указываем - GORM автоматически сгенерирует UUID через BeforeCreate hook
						Name:     "Тестовое ИП",
						INN:      "123456789012",
						Type:     "IP",
						IsActive: true,
					}
					if err := tx.Create(&legalEntity).Error; err != nil {
						log.Fatalf("❌ Ошибка создания LegalEntity: %v", err)
					}
					log.Printf("✅ Создан тестовый LegalEntity: %s", legalEntity.Name)
				} else {
					log.Fatalf("❌ Ошибка поиска LegalEntity: %v", err)
				}
			}
			
			// Создаем тестовый филиал (ID будет сгенерирован автоматически через BeforeCreate)
			legalEntityID := legalEntity.ID
			branch = models.Branch{
				// ID не указываем - GORM автоматически сгенерирует UUID через BeforeCreate hook
				Name:          "Тестовый филиал",
				IsActive:      true,
				LegalEntityID: &legalEntityID,
			}
			if err := tx.Create(&branch).Error; err != nil {
				log.Fatalf("❌ Ошибка создания филиала: %v", err)
			}
			log.Printf("✅ Создан тестовый филиал: %s (ID: %s)", branch.Name, branch.ID)
		} else {
			log.Fatalf("❌ Ошибка поиска филиала: %v", err)
		}
	} else {
		log.Printf("ℹ️ Используем существующий филиал: %s (ID: %s)", branch.Name, branch.ID)
	}
	
	// Коммитим транзакцию филиала
	if err := tx.Commit().Error; err != nil {
		log.Fatalf("❌ Ошибка коммита транзакции филиала: %v", err)
	}

	// 2. Создаем тестовых пользователей (повара) - каждый в своей транзакции
	testUsers := []struct {
		Name     string
		Phone    string
		Role     models.UserRole
		RoleName string
	}{
		{
			Name:     "Иван Поваров",
			Phone:    "1234",
			Role:     models.RoleKitchenStaff,
			RoleName: "Cook",
		},
		{
			Name:     "Мария Кулинарова",
			Phone:    "5678",
			Role:     models.RoleKitchenStaff,
			RoleName: "Cook",
		},
		{
			Name:     "Петр Пекарев",
			Phone:    "9012",
			Role:     models.RoleKitchenStaff,
			RoleName: "Oven Operator",
		},
	}

	var createdUsers []models.User
	for _, userData := range testUsers {
		// Каждый пользователь в отдельной транзакции
		userTx := db.Begin()
		
		var testUser models.User
		userName := userData.Name
		if err := userTx.Where("phone = ?", userData.Phone).First(&testUser).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				// Создаем тестового пользователя (ID будет сгенерирован автоматически через BeforeCreate)
				testUser = models.User{
					// ID не указываем - GORM автоматически сгенерирует UUID через BeforeCreate hook
					Name:   &userName,
					Phone:  userData.Phone,
					Role:   userData.Role,
					Status: models.UserStatusActive,
				}
				if err := userTx.Create(&testUser).Error; err != nil {
					log.Printf("⚠️ Ошибка создания пользователя %s: %v", userData.Name, err)
					userTx.Rollback()
					continue
				}
				log.Printf("✅ Создан тестовый пользователь: %s (ID=%s, Phone=%s, Role=%s)", 
					userData.Name, testUser.ID, testUser.Phone, testUser.Role)
			} else {
				log.Printf("⚠️ Ошибка поиска пользователя %s: %v", userData.Name, err)
				userTx.Rollback()
				continue
			}
		} else {
			log.Printf("ℹ️ Пользователь с PIN %s уже существует, обновляем...", userData.Phone)
			// Обновляем имя и статус
			testUser.Name = &userName
			testUser.Status = models.UserStatusActive
			testUser.Role = userData.Role
			if err := userTx.Save(&testUser).Error; err != nil {
				log.Printf("⚠️ Ошибка обновления пользователя %s: %v", userData.Name, err)
				userTx.Rollback()
				continue
			}
		}

		// Создаем Staff профиль для каждого пользователя
		var testStaff models.Staff
		if err := userTx.Where("user_id = ?", testUser.ID).First(&testStaff).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				// Создаем Staff профиль
				testStaff = models.Staff{
					UserID:          testUser.ID,
					RoleName:        userData.RoleName,
					Status:          models.StatusActive,
					BranchID:        branch.ID,
					PerformanceScore: 0.0,
				}
				if err := userTx.Create(&testStaff).Error; err != nil {
					log.Printf("⚠️ Ошибка создания Staff профиля для %s: %v", userData.Name, err)
					userTx.Rollback()
					continue
				}
				log.Printf("✅ Создан Staff профиль: %s (RoleName=%s)", userData.Name, testStaff.RoleName)
			} else {
				log.Printf("⚠️ Ошибка поиска Staff профиля для %s: %v", userData.Name, err)
				userTx.Rollback()
				continue
			}
		} else {
			log.Printf("ℹ️ Staff профиль для %s уже существует, обновляем...", userData.Name)
			// Обновляем статус
			testStaff.Status = models.StatusActive
			testStaff.BranchID = branch.ID
			testStaff.RoleName = userData.RoleName
			if err := userTx.Save(&testStaff).Error; err != nil {
				log.Printf("⚠️ Ошибка обновления Staff профиля для %s: %v", userData.Name, err)
				userTx.Rollback()
				continue
			}
		}
		
		// Коммитим транзакцию пользователя
		if err := userTx.Commit().Error; err != nil {
			log.Printf("⚠️ Ошибка коммита транзакции для %s: %v", userData.Name, err)
			continue
		}

		createdUsers = append(createdUsers, testUser)
	}

	if len(createdUsers) == 0 {
		log.Fatalf("❌ Не удалось создать ни одного пользователя")
	}

	// Используем первого пользователя для вывода информации
	testUser := createdUsers[0]

	// 3. Создаем Staff профиль для тестового пользователя
	var testStaff models.Staff
	if err := tx.Where("user_id = ?", testUser.ID).First(&testStaff).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			// Создаем Staff профиль
			testStaff = models.Staff{
				UserID:          testUser.ID,
				RoleName:        "Cook",
				Status:          models.StatusActive,
				BranchID:        branch.ID,
				PerformanceScore: 0.0,
			}
			if err := tx.Create(&testStaff).Error; err != nil {
				log.Fatalf("❌ Ошибка создания Staff профиля: %v", err)
			}
			log.Printf("✅ Создан Staff профиль: RoleName=%s, BranchID=%s", testStaff.RoleName, testStaff.BranchID)
		} else {
			log.Fatalf("❌ Ошибка поиска Staff профиля: %v", err)
		}
	} else {
		log.Printf("ℹ️ Staff профиль уже существует, обновляем...")
		// Обновляем статус на активный
		testStaff.Status = models.StatusActive
		testStaff.BranchID = branch.ID
		tx.Save(&testStaff)
	}

	// 4. Создаем тестовые станции (каждая в отдельной транзакции)
	stations := []struct {
		Name         string
		Icon         string
		Capabilities []string
		Categories   []string
	}{
		{
			Name:         "Горячие блюда",
			Icon:         "Flame",
			Capabilities: []string{"view_composition"},
			Categories:   []string{"pizza"},
		},
		{
			Name:         "Пицца",
			Icon:         "ChefHat",
			Capabilities: []string{"view_composition"},
			Categories:   []string{"pizza"},
		},
		{
			Name:         "Холодные закуски",
			Icon:         "Utensils",
			Capabilities: []string{"view_composition"},
			Categories:   []string{"appetizers"},
		},
		{
			Name:         "Печь",
			Icon:         "Flame",
			Capabilities: []string{"view_oven_queue"},
			Categories:   []string{"pizza"},
		},
		{
			Name:         "Упаковка",
			Icon:         "Package",
			Capabilities: []string{"order_assembly"},
			Categories:   []string{"pizza", "appetizers"},
		},
	}

	for _, stationData := range stations {
		stationTx := db.Begin()
		
		var station models.Station
		// Ищем по имени и branch_id
		if err := stationTx.Where("name = ? AND branch_id = ?", stationData.Name, branch.ID).First(&station).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				// Создаем станцию (ID нужно указать вручную, так как Station не имеет BeforeCreate)
				// Генерируем UUID для ID
				stationID := uuid.New().String()
				station = models.Station{
					ID:         stationID, // Station.ID - varchar(36), используем UUID формат
					Name:       stationData.Name,
					Icon:       stationData.Icon,
					Status:     "offline",
					QueueCount: 0,
					BranchID:   branch.ID,
					Config: models.StationConfig{
						Icon:          stationData.Icon,
						Capabilities:  stationData.Capabilities,
						Categories:    stationData.Categories,
						TriggerStatus: "ready",
						TargetStatus:  "completed",
					},
				}
				if err := stationTx.Create(&station).Error; err != nil {
					log.Printf("⚠️ Ошибка создания станции %s: %v", stationData.Name, err)
					stationTx.Rollback()
					continue
				}
				log.Printf("✅ Создана станция: %s (ID: %s)", station.Name, station.ID)
			} else {
				log.Printf("⚠️ Ошибка поиска станции %s: %v", stationData.Name, err)
				stationTx.Rollback()
				continue
			}
		} else {
			log.Printf("ℹ️ Станция %s уже существует (ID: %s)", stationData.Name, station.ID)
		}
		
		// Коммитим транзакцию станции
		if err := stationTx.Commit().Error; err != nil {
			log.Printf("⚠️ Ошибка коммита транзакции для станции %s: %v", stationData.Name, err)
			continue
		}
	}

	log.Println("\n✅ Тестовые данные успешно загружены!")
	log.Println("\n📋 Данные для входа:")
	for _, user := range createdUsers {
		var staff models.Staff
		if err := db.Where("user_id = ?", user.ID).First(&staff).Error; err == nil {
			userName := "Неизвестно"
			if user.Name != nil {
				userName = *user.Name
			}
			log.Printf("   Пользователь: %s", userName)
			log.Printf("   PIN-код: %s", user.Phone)
			log.Printf("   Роль: %s", staff.RoleName)
			log.Println()
		}
	}
	log.Printf("   Филиал: %s", branch.Name)
	log.Println("\n📋 Созданные станции:")
	// Получаем список созданных станций из БД
	var dbStations []models.Station
	if err := db.Where("branch_id = ?", branch.ID).Find(&dbStations).Error; err == nil {
		for _, station := range dbStations {
			log.Printf("   - %s (ID: %s)", station.Name, station.ID)
		}
	}
	log.Println("\n💡 Используйте следующие PIN-коды для входа в KDS приложение:")
	for _, user := range createdUsers {
		userName := "Неизвестно"
		if user.Name != nil {
			userName = *user.Name
		}
		log.Printf("   - %s: PIN %s", userName, user.Phone)
	}
}

