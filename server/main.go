package main

import (
	"log"
	"net"          // Оставляем один net
	"net/http"     // Оставляем net/http
	_ "net/http/pprof" // Для профилирования памяти
	"os"
	"runtime"      // Для мониторинга памяти
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"google.golang.org/grpc"

	"zephyrvpn/server/internal/api"
	"zephyrvpn/server/internal/config"
	"zephyrvpn/server/internal/database"
	"zephyrvpn/server/internal/models"
	"zephyrvpn/server/internal/pb"
	"zephyrvpn/server/internal/services"
	"zephyrvpn/server/internal/utils"
)

func main() {
	// Загружаем переменные окружения из .env файла (если существует)
	// Игнорируем ошибку, если файл не найден (для production окружений)
	if err := godotenv.Load(); err != nil {
		log.Printf("ℹ️ .env файл не найден, используем переменные окружения системы")
	} else {
		log.Printf("✅ Переменные окружения загружены из .env файла")
	}

	// Загрузка конфигурации
	cfg := config.Load()

	// Логируем наличие DATABASE_URL (без пароля)
	if cfg.DatabaseURL != "" {
		safeURL := cfg.DatabaseURL
		if idx := strings.Index(safeURL, "@"); idx > 0 {
			if schemeIdx := strings.Index(safeURL, "://"); schemeIdx > 0 {
				safeURL = safeURL[:schemeIdx+3] + "***@" + safeURL[idx+1:]
			}
		}
		log.Printf("📋 DATABASE_URL установлен: %s", safeURL)
	} else {
		log.Printf("⚠️ DATABASE_URL не установлен, используется значение по умолчанию")
	}

	// Логируем KAFKA_BROKERS
	if cfg.KafkaBrokers != "" {
		log.Printf("📡 KAFKA_BROKERS установлен: %s", cfg.KafkaBrokers)
	} else {
		log.Printf("⚠️ KAFKA_BROKERS не установлен, используется значение по умолчанию: localhost:9092")
	}

	// Подключение к PostgreSQL
	db, err := database.ConnectPostgres(cfg.DatabaseURL)
	if err != nil {
		log.Printf("❌ PostgreSQL connection failed: %v", err)
		log.Printf("⚠️ Продолжаем без БД (ограниченная функциональность)")
		log.Printf("💡 Убедитесь, что:")
		log.Printf("   1. PostgreSQL сервис добавлен в Railway")
		log.Printf("   2. Переменная DATABASE_URL установлена автоматически")
		log.Printf("   3. Сервисы связаны в Railway Dashboard")
		db = nil
	} else {
		defer database.ClosePostgres(db)
		
		// Выполняем миграции
		if err := models.AutoMigrate(db); err != nil {
			log.Printf("❌ Migration failed: %v", err)
			// Не продолжаем, если миграция критически важных таблиц не прошла
			log.Printf("⚠️ Continuing with limited functionality")
		} else {
			log.Println("✅ Database migrations completed")
		}
	}

	// Подключение к Redis
	// Подключение к Redis (с поддержкой Sentinel)
	redisClient, err := database.ConnectRedis(
		cfg.RedisURL,
		cfg.RedisSentinelAddrs,
		cfg.RedisMasterName,
	)
	var redisUtil *utils.RedisClient
	if err != nil {
		log.Printf("⚠️ Redis connection failed: %v (continuing without Redis)", err)
		redisClient = nil
		redisUtil = nil
	} else {
		redisUtil = utils.NewRedisClient(redisClient)
	}
	defer database.CloseRedis(redisClient)

	// Инициализация сервиса меню и загрузка из БД
	var menuService *services.MenuService
	if db != nil {
		menuService = services.NewMenuService(db, redisUtil)
		if err := menuService.LoadMenu(); err != nil {
			log.Printf("⚠️ Failed to load menu from DB: %v (using default menu)", err)
		} else {
			log.Println("✅ Menu loaded from database")
			// Запускаем автообновление меню (Redis Pub/Sub + fallback таймер)
			menuService.StartAutoReload()
		}
	} else {
		log.Println("⚠️ Menu service not started: PostgreSQL not available")
	}
	
	// Инициализируем филиалы по умолчанию
	// Филиалы теперь хранятся в БД через GORM, инициализация не требуется
	// Дефолтные филиалы можно создать через миграцию или API
	
	// Инициализация сервиса номенклатуры
	var nomenclatureService *services.NomenclatureService
	var pluService *services.PLUService
	if db != nil {
		nomenclatureService = services.NewNomenclatureService(db)
		
		// Инициализация сервиса PLU
		pluService = services.NewPLUService(db)
		// Загружаем стандартные PLU коды при старте
		if err := pluService.LoadStandardPLUCodes(); err != nil {
			log.Printf("⚠️ Failed to load standard PLU codes: %v", err)
		} else {
			log.Println("✅ PLU service initialized with standard codes")
		}
		
		// Связываем PLU сервис с Nomenclature сервисом для автоматической генерации SKU
		nomenclatureService.SetPLUService(pluService)
		log.Println("✅ Nomenclature service initialized with PLU support")
	} else {
		log.Println("⚠️ Nomenclature service not started: PostgreSQL not available")
	}

	// Инициализация сервиса юридических лиц
	var legalEntityService *services.LegalEntityService
	if db != nil {
		legalEntityService = services.NewLegalEntityService(db)
		log.Println("✅ LegalEntity service initialized")
	} else {
		log.Println("⚠️ LegalEntity service not started: PostgreSQL not available")
	}

	// Инициализация сервиса контрагентов
	var counterpartyService *services.CounterpartyService
	if db != nil {
		counterpartyService = services.NewCounterpartyService(db)
		log.Println("✅ Counterparty service initialized")
	} else {
		log.Println("⚠️ Counterparty service not started: PostgreSQL not available")
	}

	// Инициализация сервиса финансов
	var financeService *services.FinanceService
	if db != nil {
		financeService = services.NewFinanceService(db)
		log.Println("✅ Finance service initialized")
	} else {
		log.Println("⚠️ Finance service not started: PostgreSQL not available")
	}

	// Инициализация сервиса филиалов
	var branchService *services.BranchService
	if db != nil {
		branchService = services.NewBranchService(db)
		log.Println("✅ Branch service initialized")
	} else {
		log.Println("⚠️ Branch service not started: PostgreSQL not available")
	}

	// Инициализация сервиса остатков
	var stockService *services.StockService
	if db != nil {
		stockService = services.NewStockService(db)
		log.Println("✅ Stock service initialized")
		
		// Связываем сервис контрагентов и финансов со сервисом остатков (если доступны)
		if counterpartyService != nil {
			stockService.SetCounterpartyService(counterpartyService)
			log.Println("✅ Stock service linked with Counterparty service")
		}
		if financeService != nil {
			stockService.SetFinanceService(financeService)
			log.Println("✅ Stock service linked with Finance service")
		}
		
		// Запускаем периодическую проверку сроков годности (каждые 5 минут)
		go func() {
			ticker := time.NewTicker(5 * time.Minute)
			defer ticker.Stop()
			for range ticker.C {
				if err := stockService.CheckAndCreateExpiryAlerts(); err != nil {
					log.Printf("⚠️ Ошибка проверки сроков годности: %v", err)
				}
			}
		}()
		log.Println("⏰ Автоматическая проверка сроков годности запущена (каждые 5 минут)")
	} else {
		log.Println("⚠️ Stock service not started: PostgreSQL not available")
	}

	// Инициализация сервиса рецептов
	var recipeService *services.RecipeService
	if db != nil {
		recipeService = services.NewRecipeService(db)
		if stockService != nil {
			recipeService.SetStockService(stockService)
		}
		// Настраиваем Redis для инвалидации кэша меню при обновлении рецептов
		if redisUtil != nil {
			recipeService.SetRedisUtil(redisUtil)
		}
		log.Println("✅ Recipe service initialized")
	} else {
		log.Println("⚠️ Recipe service not started: PostgreSQL not available")
	}

	// Инициализация сервиса технолога
	var technologistService *services.TechnologistService
	if db != nil {
		technologistService = services.NewTechnologistService(db)
		log.Println("✅ Technologist service initialized")
	} else {
		log.Println("⚠️ Technologist service not started: PostgreSQL not available")
	}

	// Инициализация сервиса заказов на закупку
	var purchaseOrderService *services.PurchaseOrderService
	if db != nil && stockService != nil {
		purchaseOrderService = services.NewPurchaseOrderService(db, stockService)
		log.Println("✅ Purchase Order service initialized")
	} else {
		log.Println("⚠️ Purchase Order service not started: PostgreSQL or Stock service not available")
	}

	// Инициализация сервиса прогнозирования спроса
	var demandForecastService *services.DemandForecastService
	if db != nil {
		demandForecastService = services.NewDemandForecastService(db)
		log.Println("✅ Demand Forecast service initialized")
	} else {
		log.Println("⚠️ Demand Forecast service not started: PostgreSQL not available")
	}

	// Инициализация сервиса планирования закупок
	var procurementPlanningService *services.ProcurementPlanningService
	if db != nil && purchaseOrderService != nil && demandForecastService != nil {
		procurementPlanningService = services.NewProcurementPlanningService(db, purchaseOrderService, demandForecastService)
		log.Println("✅ Procurement Planning service initialized")
	} else {
		log.Println("⚠️ Procurement Planning service not started: required services not available")
	}

	// Инициализация сервиса каталога поставщиков
	var procurementCatalogService *services.ProcurementCatalogService
	if db != nil {
		procurementCatalogService = services.NewProcurementCatalogService(db)
		log.Println("✅ Procurement Catalog service initialized")
	} else {
		log.Println("⚠️ Procurement Catalog service not started: PostgreSQL not available")
	}

	// Отключаем логи для бешеной скорости
	gin.SetMode(gin.ReleaseMode)
	
	// Создаем пустой движок без лишних прослоек
	r := gin.New()

	// Health check endpoint (должен быть до CORS для Railway)
	r.GET("/api/v1/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":  "ok",
			"service": "ERP Server",
			"version": "1.0.0",
		})
	})

	// Логирование всех запросов
	r.Use(func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		method := c.Request.Method
		
		c.Next()
		
		latency := time.Since(start)
		status := c.Writer.Status()
		log.Printf("🌐 %s %s - Status: %d - Latency: %v", method, path, status, latency)
	})

	// CORS для фронтенда
	r.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		
		c.Next()
	})

	// API routes
	apiGroup := r.Group("/api/v1")
	
	// Авторизация (доступна без БД для тестирования, но лучше с БД)
	var authController *api.AuthController
	if db != nil {
		authController = api.NewAuthController(db)
		authGroup := apiGroup.Group("/auth")
		{
			authGroup.POST("/super-admin/login", authController.SuperAdminLogin)
		}
		log.Println("🔐 Auth endpoints enabled: /api/v1/auth/super-admin/login")
	} else {
		log.Println("⚠️ Auth endpoints not enabled: PostgreSQL not available")
	}
	
	// Контроллеры
	var orderController *api.OrderController
	if stockService != nil {
		orderController = api.NewOrderController(redisUtil, stockService, db, cfg.BusinessOpenHour, cfg.BusinessOpenMin, cfg.BusinessCloseHour, cfg.BusinessCloseMin)
	} else {
		// Если StockService не доступен, создаем OrderController без проверки остатков
		orderController = api.NewOrderController(redisUtil, nil, db, cfg.BusinessOpenHour, cfg.BusinessOpenMin, cfg.BusinessCloseHour, cfg.BusinessCloseMin)
		log.Println("⚠️ OrderController создан без StockService: проверка остатков отключена")
	}
	erpController := api.NewERPController(redisUtil, cfg.KafkaBrokers, db, cfg.BusinessOpenHour, cfg.BusinessOpenMin, cfg.BusinessCloseHour, cfg.BusinessCloseMin)
	stationsController := api.NewStationsController(db, redisUtil)
	staffController := api.NewStaffController(db, redisUtil)
	
	// Analytics Controller (для прогнозирования выручки)
	var analyticsController *api.AnalyticsController
	if redisUtil != nil && db != nil {
		revenueService := services.NewRevenueService(redisUtil, db)
		
		// Инициализация Nixtla AI для прогнозирования выручки
		if cfg.NixtlaAPIKey != "" {
			revenueService.SetNixtlaClient(cfg.NixtlaAPIKey)
			log.Printf("✅ Nixtla AI инициализирован для прогнозирования выручки")
		} else {
			log.Printf("⚠️ NIXTLA_API_KEY не установлен, прогнозирование будет недоступно (линейная экстраполяция отключена)")
		}
		
		// Инициализация Weather клиента для получения данных о погоде
		revenueService.SetWeatherClient(cfg.WeatherLatitude, cfg.WeatherLongitude, cfg.WeatherTimezone)
		
		revenuePlanService := services.NewRevenuePlanService(db)
		analyticsController = api.NewAnalyticsController(revenueService, revenuePlanService)
		log.Println("✅ Analytics Controller инициализирован")
	} else {
		log.Println("⚠️ Analytics Controller НЕ инициализирован: Redis или DB недоступны")
	}
	
	// Инициализация воркер-пула кухни
	kitchenWorkerPool := api.NewKitchenWorkerPool(redisUtil)
	// Запускаем 5 воркеров по умолчанию
	if redisUtil != nil {
		kitchenWorkerPool.SetWorkerCount(5)
		log.Println("👨‍🍳 Кухня: запущено 5 поваров по умолчанию")
	}
	kitchenController := api.NewKitchenController(kitchenWorkerPool)
	
	// Запускаем WebSocket Hub для планшетов поваров
	go api.GlobalHub.Run()
	log.Println("📱 WebSocket Hub запущен для планшетов поваров")
	
	// Запускаем WebSocket Hub для ERP системы
	go api.ERPHub.Run()
	log.Println("🖥️ WebSocket Hub запущен для ERP системы")
	
	// Инициализация OrderService для управления заказами и восстановления состояния
	var orderService *services.OrderService
	if db != nil && redisUtil != nil {
		// Конвертируем *gorm.DB в *sql.DB
		sqlDB, err := db.DB()
		if err != nil {
			log.Printf("⚠️ Ошибка получения *sql.DB из *gorm.DB: %v", err)
			orderService = nil
		} else {
			orderService = services.NewOrderService(sqlDB, redisUtil)
			log.Println("✅ OrderService инициализирован")
			
			// КРИТИЧНО: BootstrapState ПЕРЕД запуском Kafka consumer
			// Восстанавливаем активные заказы из PostgreSQL в Redis
			log.Println("🔄 Выполнение BootstrapState: восстановление состояния из PostgreSQL...")
			if err := orderService.BootstrapState(); err != nil {
				log.Printf("⚠️ BootstrapState завершился с ошибкой: %v (продолжаем работу)", err)
			} else {
				log.Println("✅ BootstrapState успешно завершен")
			}
		}
	} else {
		log.Println("⚠️ OrderService НЕ инициализирован: требуется PostgreSQL и Redis")
	}

	// Запускаем фоновую задачу архивирования старых заказов (раз в день)
	if orderService != nil {
		go func() {
			// Первый запуск через 1 час после старта
			time.Sleep(1 * time.Hour)
			
			// Затем каждые 24 часа
			ticker := time.NewTicker(24 * time.Hour)
			defer ticker.Stop()
			
			for {
				log.Println("🗄️ Запуск фоновой задачи архивирования старых заказов...")
				if err := orderService.ArchiveOldOrders(); err != nil {
					log.Printf("⚠️ Ошибка архивирования заказов: %v", err)
				}
				<-ticker.C
			}
		}()
		log.Println("✅ Фоновая задача архивирования заказов запущена (каждые 24 часа)")
	}

	// Запускаем Kafka Consumer для отправки заказов в WebSocket
	// ПОСЛЕ BootstrapState используем LastOffset, чтобы не обрабатывать старые заказы повторно
	if cfg.KafkaBrokers != "" && redisUtil != nil {
		log.Printf("📡 Kafka WS Consumer: используем брокеры: %s", cfg.KafkaBrokers)
		// startFromLatest = true, так как мы уже восстановили состояние из БД
		startFromLatest := orderService != nil
		kafkaConsumer := api.NewKafkaWSConsumer(cfg.KafkaBrokers, "pizza-orders", redisUtil, cfg.KafkaUsername, cfg.KafkaPassword, cfg.KafkaCACert, startFromLatest, orderService)
		kafkaConsumer.Start()
		log.Printf("📡 Kafka WS Consumer запущен: GroupID=order-service-stable-group, StartOffset=%s", 
			map[bool]string{true: "LastOffset (после bootstrap)", false: "FirstOffset"}[startFromLatest])
		defer kafkaConsumer.Stop()
	} else {
		if cfg.KafkaBrokers == "" {
			log.Println("⚠️ Kafka WS Consumer НЕ запущен: KAFKA_BROKERS не установлен")
		} else {
			log.Println("⚠️ Kafka WS Consumer НЕ запущен: Redis не настроен")
		}
	}

	// Магазин "Пицца Тест" - создание заказов
	apiGroup.POST("/order", orderController.CreateOrder)
	
	// Staff Management (для Wails)
	if db != nil && staffController != nil {
		apiGroup.GET("/staff", staffController.GetStaff) // Получить список сотрудников (для Wails)
	}
	
	apiGroup.GET("/menu", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"pizzas": api.GetAvailablePizzas(),
			"extras": api.GetAvailableExtras(),
			"sets":   api.GetAvailableSets(),
		})
	})
	// Отдельные эндпоинты для меню
	menuGroup := apiGroup.Group("/menu")
	{
		menuGroup.GET("/pizzas", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{
				"pizzas": api.GetAvailablePizzas(),
			})
		})
		menuGroup.GET("/extras", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{
				"extras": api.GetAvailableExtras(),
			})
		})
		menuGroup.GET("/sets", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{
				"sets": api.GetAvailableSets(),
			})
		})
	}

	// ERP "ЕРПИ ТЕСТ" - просмотр заказов
	erpGroup := apiGroup.Group("/erp")
	{
		// ВАЖНО: POST должен быть ПЕРЕД GET, чтобы избежать конфликта маршрутов
		if orderController != nil {
			erpGroup.POST("/orders", orderController.CreateOrder)             // Создать заказ (для Wails)
			log.Println("✅ POST /api/v1/erp/orders зарегистрирован")
		} else {
			log.Println("⚠️ POST /api/v1/erp/orders НЕ зарегистрирован: orderController == nil")
		}
		erpGroup.GET("/orders", erpController.GetOrders)                 // Активные заказы
		erpGroup.GET("/orders/pending", erpController.GetPendingOrders)  // Отложенные (будущие) заказы
		erpGroup.GET("/orders/batch", erpController.GetOrdersBatch)      // Новая партия по 50
		erpGroup.POST("/orders/:id/processed", erpController.MarkOrderProcessed) // Отметить конкретный заказ
		erpGroup.GET("/orders/:id", erpController.GetOrder)
		erpGroup.GET("/stats", erpController.GetStats)
		erpGroup.GET("/revenue/forecast", erpController.GetRevenueForecast) // Прогноз выручки на конец дня (должен быть ПЕРЕД /revenue)
		erpGroup.GET("/revenue", erpController.GetRevenue)              // Выручка за день
		erpGroup.GET("/daily-plan", erpController.GetDailyPlan)        // План на день
		erpGroup.PUT("/daily-plan", erpController.SetDailyPlan)         // Установить план на день
		erpGroup.GET("/kitchen-load", erpController.GetKitchenLoad)     // Загрузка кухни (оперативная)
		erpGroup.GET("/kafka-orders-count", erpController.GetKafkaOrdersCount)   // Количество заказов в Kafka
		erpGroup.GET("/kafka-orders-sample", erpController.GetKafkaOrdersSample) // Примеры заказов из Kafka
		
		// Управление слотами
		erpGroup.GET("/slots", erpController.GetSlots)                    // Получить все слоты
		erpGroup.GET("/slots/config", erpController.GetSlotConfig)        // Получить конфигурацию слотов
		erpGroup.PUT("/slots/config", erpController.UpdateSlotConfig)     // Обновить конфигурацию слотов
		erpGroup.PUT("/slots/:slot_id/toggle", erpController.ToggleSlot)  // Отключить/включить слот
		erpGroup.PUT("/slots/:slot_id/disabled", erpController.UpdateSlotDisabled) // Обновить статус отключения слота
		erpGroup.PUT("/slots/:slot_id/plan", erpController.UpdateSlotPlan) // Обновить план слота
		erpGroup.PUT("/slots/plan/batch", erpController.UpdateSlotsPlanBatch) // Обновить планы для нескольких слотов (батч)
		erpGroup.PUT("/slots/:slot_id/capacity", erpController.UpdateSlotCapacity) // Обновить лимит слота
		
		// Управление станциями кухни
		erpGroup.GET("/stations", stationsController.GetStations)                    // Получить все станции
		erpGroup.GET("/stations/capabilities", stationsController.GetCapabilities)  // Получить capabilities и категории
		erpGroup.POST("/stations", stationsController.CreateStation)                // Создать станцию
		erpGroup.PUT("/stations/:id", stationsController.UpdateStation)             // Обновить станцию
		erpGroup.DELETE("/stations/:id", stationsController.DeleteStation)          // Удалить станцию
		erpGroup.PUT("/stations/:id/orders/:order_id/items/:item_index", stationsController.UpdateOrderItemStatus) // Обновить статус позиции заказа

		// Staff Management
		erpGroup.GET("/staff", staffController.GetStaff)                           // Получить список сотрудников
		erpGroup.GET("/staff/roles", staffController.GetAvailableRoles)           // Получить доступные роли
		erpGroup.POST("/staff", staffController.CreateStaff)                       // Создать сотрудника
		erpGroup.PUT("/staff/:id", staffController.UpdateStaff)                   // Обновить сотрудника
		erpGroup.PUT("/staff/:id/status", staffController.UpdateStaffStatus)       // Обновить статус сотрудника (с валидацией State Machine)
		erpGroup.DELETE("/staff/:id", staffController.DeleteStaff)                // Удалить сотрудника
		erpGroup.POST("/staff/pin-auth", staffController.PinCodeAuth)              // Авторизация по PIN-коду для KDS
		erpGroup.POST("/staff/bind-station", staffController.BindStation)         // Привязать станцию к сессии
		erpGroup.POST("/staff/pulse", staffController.SendPulse)                   // Отправить пульс для отслеживания онлайн статуса
	}

	// Админ-панель для управления меню
	if menuService != nil {
		adminController := api.NewAdminController(menuService)
		adminGroup := apiGroup.Group("/admin")
		{
			adminGroup.POST("/update-menu", adminController.UpdateMenu)     // Hot-reload меню из БД
			adminGroup.GET("/menu-status", adminController.GetMenuStatus)    // Статус меню
		}
		log.Println("🔧 Admin endpoints enabled: /api/v1/admin/update-menu, /api/v1/admin/menu-status")
	}
	
	
	// Управление филиалами
	if db != nil && branchService != nil {
		branchController := api.NewBranchController(branchService)
		branchGroup := apiGroup.Group("/branches")
		{
			branchGroup.GET("", branchController.GetBranches)           // Список филиалов
			branchGroup.GET("/:id", branchController.GetBranch)         // Получить филиал
			branchGroup.POST("", branchController.CreateBranch)         // Создать филиал
			branchGroup.PUT("/:id", branchController.UpdateBranch)      // Обновить филиал
			branchGroup.DELETE("/:id", branchController.DeleteBranch)   // Удалить филиал
		}
		log.Println("🏢 Branch endpoints enabled: /api/v1/branches")
	} else {
		log.Println("⚠️ Branch endpoints not enabled: PostgreSQL not available")
	}
	
	// Номенклатура товаров
	if db != nil && nomenclatureService != nil && pluService != nil {
		nomenclatureController := api.NewNomenclatureController(nomenclatureService, pluService)
			nomenclatureGroup := apiGroup.Group("/inventory/nomenclature")
			{
				// Товары
				nomenclatureGroup.GET("", nomenclatureController.GetNomenclatureItems)                    // Список товаров
				nomenclatureGroup.GET("/suggest-sku", nomenclatureController.SuggestSKU)                 // Предложение SKU на основе PLU
			nomenclatureGroup.GET("/:id", nomenclatureController.GetNomenclatureItem)                // Получить товар
			nomenclatureGroup.POST("", nomenclatureController.CreateNomenclatureItem)                // Создать товар
			nomenclatureGroup.PUT("/:id", nomenclatureController.UpdateNomenclatureItem)              // Обновить товар
			nomenclatureGroup.DELETE("/:id", nomenclatureController.DeleteNomenclatureItem)          // Удалить товар
			
			// Импорт
			nomenclatureGroup.POST("/upload-file", nomenclatureController.UploadNomenclatureFile)        // Определение заголовков файла
			nomenclatureGroup.POST("/parse-file", nomenclatureController.ParseNomenclatureFile)         // Парсинг файла с маппингом
			nomenclatureGroup.POST("/validate-import", nomenclatureController.ValidateNomenclatureImport) // Валидация импорта
			nomenclatureGroup.POST("/import", nomenclatureController.ImportNomenclature)                  // Массовый импорт
			
			// Категории
			nomenclatureGroup.GET("/categories", nomenclatureController.GetNomenclatureCategories)        // Список категорий
			nomenclatureGroup.POST("/categories", nomenclatureController.CreateNomenclatureCategory)       // Создать категорию
			nomenclatureGroup.PUT("/categories/:id", nomenclatureController.UpdateNomenclatureCategory)    // Обновить категорию
			nomenclatureGroup.DELETE("/categories/:id", nomenclatureController.DeleteNomenclatureCategory) // Удалить категорию
		}
		log.Println("📦 Nomenclature endpoints enabled: /api/v1/inventory/nomenclature")
	} else {
		log.Println("⚠️ Nomenclature endpoints not enabled: PostgreSQL not available")
	}

	// Управление остатками и сроками годности
	if db != nil && stockService != nil {
		stockController := api.NewStockController(stockService)
		stockGroup := apiGroup.Group("/inventory/stock")
		{
			stockGroup.GET("", stockController.GetStockItems)                    // Список остатков
			stockGroup.GET("/at-risk", stockController.GetAtRiskInventory)       // Рискованные товары
			stockGroup.GET("/expiry-alerts", stockController.GetExpiryAlerts)    // Уведомления о сроке годности
			stockGroup.GET("/movements", stockController.GetStockMovements)      // Журнал движений склада (аудит)
			stockGroup.GET("/batches-history", stockController.GetBatchesHistory) // История батчей по номенклатуре
			stockGroup.POST("/process-sale", stockController.ProcessSaleDepletion)           // Автоматическое списание при продаже
		stockGroup.POST("/commit-production", stockController.CommitProduction)          // Ручное производство полуфабриката
		stockGroup.GET("/recipes/:id/prime-cost", stockController.GetRecipePrimeCost)   // Расчет себестоимости рецепта
		stockGroup.POST("/check-expiry-alerts", stockController.CheckExpiryAlerts) // Ручная проверка сроков
		stockGroup.POST("/process-inbound-invoice", stockController.ProcessInboundInvoice) // Обработка входящей накладной (оприходование)
		// CRUD для накладных
		stockGroup.GET("/invoices", stockController.GetInvoices)                    // Список накладных
		stockGroup.POST("/invoices", stockController.CreateInvoice)                 // Создать накладную (черновик)
		stockGroup.PUT("/invoices/:id", stockController.UpdateInvoice)              // Обновить накладную (черновик)
		stockGroup.DELETE("/invoices/:id", stockController.DeleteInvoice)          // Удалить накладную (черновик)
		}
		log.Println("📊 Stock endpoints enabled: /api/v1/inventory/stock")
	} else {
		log.Println("⚠️ Stock endpoints not enabled: PostgreSQL not available")
	}

	// Управление рецептами
	if db != nil && recipeService != nil {
		log.Println("✅ Условия для регистрации роутов рецептов выполнены: db != nil && recipeService != nil")
		recipeController := api.NewRecipeController(recipeService)
		recipeGroup := apiGroup.Group("/recipes")
		{
			recipeGroup.GET("", recipeController.GetRecipes)           // Список рецептов
			recipeGroup.GET("/:id", recipeController.GetRecipe)         // Получить рецепт
			recipeGroup.POST("", recipeController.CreateRecipe)         // Создать рецепт
			recipeGroup.POST("/unified-create", recipeController.UnifiedCreateMenuItem) // Unified create: Nomenclature + Recipe + PizzaRecipe
			recipeGroup.PUT("/:id", recipeController.UpdateRecipe)      // Обновить рецепт
			recipeGroup.DELETE("/:id", recipeController.DeleteRecipe)   // Удалить рецепт
			recipeGroup.GET("/orphaned-ingredients", recipeController.FindOrphanedIngredients) // Найти осиротевшие ингредиенты
			
			// Иерархическая структура папок
			recipeGroup.GET("/folder", recipeController.GetFolderContent)        // Получить содержимое папки
			recipeGroup.POST("/nodes", recipeController.CreateNode)             // Создать узел (папку или рецепт)
			recipeGroup.GET("/nodes/:id/path", recipeController.GetNodePath)    // Получить путь к узлу
			recipeGroup.PUT("/nodes/:id", recipeController.UpdateNode)          // Обновить узел
			recipeGroup.PUT("/nodes/:id/position", recipeController.UpdateNodePosition) // Обновить позицию узла в сетке
			recipeGroup.DELETE("/nodes/:id", recipeController.DeleteNode)        // Удалить узел
		}
		log.Println("📋 Recipe endpoints enabled: /api/v1/recipes")
		log.Println("   - GET    /api/v1/recipes")
		log.Println("   - GET    /api/v1/recipes/:id")
		log.Println("   - POST   /api/v1/recipes")
		log.Println("   - PUT    /api/v1/recipes/:id")
		log.Println("   - DELETE /api/v1/recipes/:id")
	} else {
		if db == nil {
			log.Println("⚠️ Recipe endpoints NOT enabled: db == nil")
		}
		if recipeService == nil {
			log.Println("⚠️ Recipe endpoints NOT enabled: recipeService == nil")
		}
	}

	// Technologist Workspace (требует роль TECHNOLOGIST или SUPER_ADMIN)
	if db != nil && technologistService != nil && recipeService != nil {
		log.Println("✅ Условия для регистрации роутов Technologist Workspace выполнены")
		technologistController := api.NewTechnologistController(technologistService, recipeService)
		technologistGroup := apiGroup.Group("/technologist")
		// ВРЕМЕННО ОТКЛЮЧЕНО: RBAC middleware для разработки
		// technologistGroup.Use(api.RequireTechnologistRole()) // RBAC middleware
		{
			// Production Dashboard
			technologistGroup.GET("/dashboard", technologistController.GetProductionDashboard) // Production Dashboard
			
			// Recipe Versioning
			technologistGroup.GET("/recipes/:id/versions", technologistController.GetRecipeVersions) // Версии рецепта
			technologistGroup.GET("/recipes/:id/usage-tree", technologistController.GetRecipeUsageTree) // Дерево использования
			
			// Training Materials
			technologistGroup.POST("/training-materials", technologistController.CreateTrainingMaterial) // Создать материал
			technologistGroup.GET("/recipes/:id/training-materials", technologistController.GetTrainingMaterials) // Материалы рецепта
			
			// Recipe Exams
			technologistGroup.POST("/recipe-exams", technologistController.CreateRecipeExam) // Создать/обновить экзамен
			technologistGroup.GET("/recipes/:id/exams", technologistController.GetRecipeExams) // Экзамены по рецепту
			technologistGroup.GET("/staff/:id/recipe-exams", technologistController.GetStaffRecipeExams) // Экзамены сотрудника
			
			// Unified Create (расширенная версия с версионированием)
			technologistGroup.POST("/unified-create", technologistController.UnifiedCreateMenuItem) // Unified create Menu Item
			
			// Activate for Menu
			technologistGroup.POST("/activate-for-menu", technologistController.ActivateForMenu) // Активировать товар для меню
			
			// Управление допами (Extras)
			technologistGroup.GET("/extras", technologistController.GetExtras)                    // Получить все допы
			technologistGroup.POST("/extras", technologistController.CreateExtra)                 // Создать доп
			technologistGroup.PUT("/extras/:id", technologistController.UpdateExtra)               // Обновить доп
			technologistGroup.DELETE("/extras/:id", technologistController.DeleteExtra)           // Удалить доп
			
			// Управление связями пицца-доп
			technologistGroup.GET("/pizzas/:pizza_name/extras", technologistController.GetPizzaExtras)           // Получить допы для пиццы
			technologistGroup.POST("/pizzas/:pizza_name/extras", technologistController.AddPizzaExtra)           // Привязать доп к пицце
			technologistGroup.PUT("/pizzas/:pizza_name/extras/:extra_id", technologistController.UpdatePizzaExtra) // Обновить связь
			technologistGroup.DELETE("/pizzas/:pizza_name/extras/:extra_id", technologistController.RemovePizzaExtra) // Отвязать доп от пиццы
		}
		log.Println("✅ Technologist Workspace endpoints registered")
		log.Println("📋 Technologist endpoints enabled: /api/v1/technologist")
		log.Println("   - GET    /api/v1/technologist/dashboard")
		log.Println("   - GET    /api/v1/technologist/recipes/:id/versions")
		log.Println("   - GET    /api/v1/technologist/recipes/:id/usage-tree")
		log.Println("   - POST   /api/v1/technologist/training-materials")
		log.Println("   - GET    /api/v1/technologist/recipes/:id/training-materials")
		log.Println("   - POST   /api/v1/technologist/recipe-exams")
		log.Println("   - GET    /api/v1/technologist/recipes/:id/exams")
		log.Println("   - GET    /api/v1/technologist/staff/:id/recipe-exams")
		log.Println("   - POST   /api/v1/technologist/unified-create")
	} else {
		log.Println("⚠️ Technologist endpoints NOT enabled: db or services == nil")
	}

	// Analytics & Reports (прогнозирование выручки)
	if analyticsController != nil {
		analyticsGroup := apiGroup.Group("/analytics")
		{
			analyticsGroup.POST("/run-forecast", analyticsController.RunForecast)        // Запустить прогнозирование
			analyticsGroup.GET("/latest-plan", analyticsController.GetLatestPlan)       // Получить последний план
		}
		log.Println("✅ Analytics endpoints enabled: /api/v1/analytics")
		log.Println("   - POST   /api/v1/analytics/run-forecast")
		log.Println("   - GET    /api/v1/analytics/latest-plan")
	} else {
		log.Println("⚠️ Analytics endpoints NOT enabled: analyticsController == nil")
	}

	// Управление юридическими лицами
	if db != nil && legalEntityService != nil {
		legalEntityController := api.NewLegalEntityController(legalEntityService)
		legalEntityGroup := apiGroup.Group("/legal-entities")
		{
			legalEntityGroup.GET("", legalEntityController.GetLegalEntities)
			legalEntityGroup.GET("/:id", legalEntityController.GetLegalEntity)
		}
		log.Println("🏢 LegalEntity endpoints enabled: /api/v1/legal-entities")
	} else {
		log.Println("⚠️ LegalEntity endpoints not enabled: PostgreSQL not available")
	}

	// Управление контрагентами и финансовыми транзакциями
	if db != nil {
		financeGroup := apiGroup.Group("/finance")
		
		// Контрагенты
		if counterpartyService != nil {
			counterpartyController := api.NewCounterpartyController(counterpartyService)
			counterpartyGroup := financeGroup.Group("/counterparties")
			{
				counterpartyGroup.GET("", counterpartyController.GetCounterparties)           // Список контрагентов
				counterpartyGroup.POST("", counterpartyController.CreateCounterparty)        // Создать контрагента
				// Специфичные маршруты должны быть ДО параметрических /:id
				counterpartyGroup.GET("/fetch-by-inn", counterpartyController.FetchCounterpartyByINN) // Получить контрагента по ИНН
				counterpartyGroup.GET("/check-inn", counterpartyController.CheckINNDuplicate)  // Проверить дубликат ИНН
				counterpartyGroup.POST("/invoices", counterpartyController.CreateInvoice)    // Создать счет для контрагента
				// Параметрические маршруты в конце
				counterpartyGroup.GET("/:id", counterpartyController.GetCounterparty)         // Получить контрагента
				counterpartyGroup.PUT("/:id", counterpartyController.UpdateCounterparty)    // Обновить контрагента
				counterpartyGroup.DELETE("/:id", counterpartyController.DeleteCounterparty)  // Удалить контрагента
			}
			log.Println("🤝 Counterparty endpoints enabled: /api/v1/finance/counterparties")
		}
		
		// Финансовые транзакции
		if financeService != nil {
			financeController := api.NewFinanceController(financeService)
			transactionGroup := financeGroup.Group("/transactions")
			{
				transactionGroup.GET("", financeController.GetTransactions)           // Список транзакций
				transactionGroup.GET("/:id", financeController.GetTransaction)        // Получить транзакцию
				transactionGroup.POST("", financeController.CreateTransaction)         // Создать транзакцию
			}
			financeGroup.GET("/counterparties/with-balances", financeController.GetCounterpartiesWithBalances) // Контрагенты с балансами
			log.Println("💰 Finance transaction endpoints enabled: /api/v1/finance/transactions")
		}
	} else {
		log.Println("⚠️ Finance endpoints not enabled: PostgreSQL not available")
	}

	// Заказы на закупку (Purchase Orders)
	if db != nil && purchaseOrderService != nil {
		purchaseOrderController := api.NewPurchaseOrderController(purchaseOrderService)
		purchaseOrderGroup := apiGroup.Group("/purchase-orders")
		{
			purchaseOrderGroup.GET("", purchaseOrderController.GetPurchaseOrders)                    // Список заказов
			purchaseOrderGroup.GET("/:id", purchaseOrderController.GetPurchaseOrder)                  // Получить заказ
			purchaseOrderGroup.POST("", purchaseOrderController.CreatePurchaseOrder)                // Создать заказ
			purchaseOrderGroup.PUT("/:id", purchaseOrderController.UpdatePurchaseOrder)             // Обновить заказ
			purchaseOrderGroup.DELETE("/:id", purchaseOrderController.DeletePurchaseOrder)          // Отменить заказ
			purchaseOrderGroup.POST("/:id/send", purchaseOrderController.SendPurchaseOrder)           // Отправить заказ
			purchaseOrderGroup.POST("/:id/receive", purchaseOrderController.ReceivePurchaseOrder)    // Получить заказ
			purchaseOrderGroup.POST("/:id/cancel", purchaseOrderController.CancelPurchaseOrder)      // Отменить заказ
		}
		log.Println("📦 Purchase Order endpoints enabled: /api/v1/purchase-orders")
	} else {
		log.Println("⚠️ Purchase Order endpoints not enabled: PostgreSQL or services not available")
	}

	// Планирование закупок
	if db != nil && procurementPlanningService != nil {
		planningController := api.NewProcurementPlanningController(procurementPlanningService)
		procurementGroup := apiGroup.Group("/procurement")
		{
			procurementGroup.GET("/monthly-plan", planningController.GetMonthlyPlan)           // Получить месячный план
			procurementGroup.PUT("/plan-cell", planningController.UpdatePlanCell)              // Обновить ячейку плана
			procurementGroup.POST("/submit-plan", planningController.SubmitPlan)               // Отправить план (создать заказы)
		}
		log.Println("📅 Procurement Planning endpoints enabled: /api/v1/procurement")
	} else {
		log.Println("⚠️ Procurement Planning endpoints not enabled: PostgreSQL or services not available")
	}

	// Каталог поставщиков
	var uomConversionService *services.UoMConversionService
	if db != nil {
		uomConversionService = services.NewUoMConversionService(db)
		log.Println("✅ UoM Conversion service initialized")
	} else {
		log.Println("⚠️ UoM Conversion service not started: PostgreSQL not available")
	}
	
	if db != nil && procurementCatalogService != nil && uomConversionService != nil {
		catalogController := api.NewProcurementCatalogController(procurementCatalogService, uomConversionService)
		procurementCatalogGroup := apiGroup.Group("/procurement")
		{
			procurementCatalogGroup.GET("/setup-template", catalogController.GetSetupTemplate)  // Получить шаблон каталога
			procurementCatalogGroup.POST("/save-catalog", catalogController.SaveCatalog)         // Сохранить каталог
			procurementCatalogGroup.GET("/catalog-item-price", catalogController.GetCatalogItemPrice) // Получить цену товара из каталога
			procurementCatalogGroup.GET("/uom-rules", catalogController.GetUoMConversionRules)    // Получить правила конвертации
			procurementCatalogGroup.POST("/uom-rules", catalogController.CreateUoMConversionRule) // Создать правило конвертации
			procurementCatalogGroup.PUT("/uom-rules/:id", catalogController.UpdateUoMConversionRule) // Обновить правило конвертации
			procurementCatalogGroup.DELETE("/uom-rules/:id", catalogController.DeleteUoMConversionRule) // Удалить правило конвертации
			procurementCatalogGroup.POST("/calculate-multiplier", catalogController.CalculateMultiplier) // Вычислить множитель конвертации
		}
		log.Println("📋 Procurement Catalog endpoints enabled: /api/v1/procurement")
	} else {
		log.Println("⚠️ Procurement Catalog endpoints not enabled: PostgreSQL or services not available")
	}

	// Кухня - управление воркерами-поварами
	kitchenGroup := apiGroup.Group("/kitchen")
	{
		kitchenGroup.GET("/workers", kitchenController.GetWorkersStats)           // Статистика воркеров
		kitchenGroup.POST("/workers", kitchenController.SetWorkersCount)         // Установить количество воркеров
		kitchenGroup.POST("/workers/add", kitchenController.AddWorker)            // Добавить одного воркера
		kitchenGroup.DELETE("/workers/:id", kitchenController.RemoveWorker)      // Удалить воркера по ID
		kitchenGroup.POST("/workers/stop", kitchenController.StopAllWorkers)     // Остановить всех воркеров
		kitchenGroup.POST("/workers/start", kitchenController.StartWorkers)      // Запустить воркеров (с указанием количества)
	}
	
	// WebSocket для планшетов поваров
	apiGroup.GET("/ws", api.ServeWS)
	
	// WebSocket для ERP системы
	erpGroup.GET("/ws", api.ServeERPWS)
	go func() {
		lis, err := net.Listen("tcp", ":50051")
		if err != nil {
			log.Fatalf("failed to listen gRPC: %v", err)
		}
	
		grpcServer := grpc.NewServer()
		// Регистрируем наш сервис с Kafka интеграцией
		grpcOrderServer := api.NewOrderGRPCServer(redisUtil, cfg.KafkaBrokers, db, cfg.BusinessOpenHour, cfg.BusinessOpenMin, cfg.BusinessCloseHour, cfg.BusinessCloseMin, cfg.KafkaUsername, cfg.KafkaPassword, cfg.KafkaCACert, orderService)
		pb.RegisterOrderServiceServer(grpcServer, grpcOrderServer)
	
		log.Printf("📡 gRPC Server starting on port 50051")
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatalf("failed to serve gRPC: %v", err)
		}
	}()
	// Запуск на порту из конфига
	port := cfg.ServerPort
	if port == "" {
		port = os.Getenv("PORT")
		if port == "" {
			port = "8080"
		}
	}

	// Запуск HTTP сервера для pprof (профилирование памяти)
	// Доступен на http://localhost:6060/debug/pprof/
	go func() {
		pprofPort := "6060"
		log.Printf("🔍 pprof доступен на http://localhost:%s/debug/pprof/", pprofPort)
		log.Printf("   Используйте: go tool pprof http://localhost:%s/debug/pprof/heap", pprofPort)
		if err := http.ListenAndServe("localhost:"+pprofPort, nil); err != nil {
			log.Printf("⚠️ pprof server failed to start: %v", err)
		}
	}()

	// Периодическое логирование статистики памяти
	go func() {
		ticker := time.NewTicker(30 * time.Second) // Каждые 30 секунд
		defer ticker.Stop()
		
		for range ticker.C {
			logMemoryStats()
		}
	}()

	log.Printf("🚀 Server starting on port %s", port)
	log.Printf("📡 API доступен на http://0.0.0.0:%s/api/v1", port)
	if err := r.Run("0.0.0.0:" + port); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

// logMemoryStats логирует текущую статистику использования памяти
func logMemoryStats() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	
	// Конвертируем байты в мегабайты
	heapAllocMB := float64(m.HeapAlloc) / 1024 / 1024
	heapSysMB := float64(m.HeapSys) / 1024 / 1024
	heapInuseMB := float64(m.HeapInuse) / 1024 / 1024
	sysMB := float64(m.Sys) / 1024 / 1024
	numGC := m.NumGC
	numGoroutines := runtime.NumGoroutine()
	
	log.Printf("💾 Memory Stats: HeapAlloc=%.2f MB, HeapSys=%.2f MB, HeapInuse=%.2f MB, Sys=%.2f MB, GC=%d, Goroutines=%d",
		heapAllocMB, heapSysMB, heapInuseMB, sysMB, numGC, numGoroutines)
	
	// Предупреждение при большом количестве горутин
	if numGoroutines > 100 {
		log.Printf("⚠️ WARNING: High number of goroutines detected: %d (possible goroutine leak)", numGoroutines)
	}
	
	// Предупреждение при большом использовании памяти
	if heapAllocMB > 500 {
		log.Printf("⚠️ WARNING: High memory usage detected: %.2f MB (possible memory leak)", heapAllocMB)
	}
}
