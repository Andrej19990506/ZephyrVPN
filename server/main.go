package main

import (
	"log"
	"net"          // Оставляем один net
	"net/http"     // Оставляем net/http
	"os"
	"time"

	"github.com/gin-gonic/gin"
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
	// Загрузка конфигурации
	cfg := config.Load()

	// Подключение к PostgreSQL
	db, err := database.ConnectPostgres(cfg.DatabaseURL)
	if err != nil {
		log.Printf("⚠️ PostgreSQL connection failed: %v (continuing without DB)", err)
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
	redisClient, err := database.ConnectRedis(cfg.RedisURL)
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
		log.Println("✅ Recipe service initialized")
	} else {
		log.Println("⚠️ Recipe service not started: PostgreSQL not available")
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

	// CORS для фронтенда
	r.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		
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
	orderController := api.NewOrderController(redisUtil, cfg.BusinessOpenHour, cfg.BusinessCloseHour, cfg.BusinessCloseMin)
	erpController := api.NewERPController(redisUtil, cfg.KafkaBrokers, cfg.BusinessOpenHour, cfg.BusinessCloseHour, cfg.BusinessCloseMin)
	stationsController := api.NewStationsController(db, redisUtil)
	staffController := api.NewStaffController(db, redisUtil)
	
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
	
	// Запускаем Kafka Consumer для отправки заказов в WebSocket
	if cfg.KafkaBrokers != "" && redisUtil != nil {
		kafkaConsumer := api.NewKafkaWSConsumer(cfg.KafkaBrokers, "pizza-orders", redisUtil)
		kafkaConsumer.Start()
		log.Println("📡 Kafka WS Consumer запущен: читает с FirstOffset, GroupID=kitchen-ws-group-v3")
		defer kafkaConsumer.Stop()
	} else {
		log.Println("⚠️ Kafka WS Consumer НЕ запущен: KafkaBrokers или Redis не настроены")
	}

	// Магазин "Пицца Тест" - создание заказов
	apiGroup.POST("/order", orderController.CreateOrder)
	apiGroup.GET("/menu", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"pizzas": api.GetAvailablePizzas(),
			"extras": api.GetAvailableExtras(),
			"sets":   api.GetAvailableSets(),
		})
	})

	// ERP "ЕРПИ ТЕСТ" - просмотр заказов
	erpGroup := apiGroup.Group("/erp")
	{
		erpGroup.GET("/orders", erpController.GetOrders)                 // Активные заказы
		erpGroup.GET("/orders/pending", erpController.GetPendingOrders)  // Отложенные (будущие) заказы
		erpGroup.GET("/orders/batch", erpController.GetOrdersBatch)      // Новая партия по 50
		erpGroup.POST("/orders/:id/processed", erpController.MarkOrderProcessed) // Отметить конкретный заказ
		erpGroup.GET("/orders/:id", erpController.GetOrder)
		erpGroup.GET("/stats", erpController.GetStats)
		erpGroup.GET("/kafka-orders-count", erpController.GetKafkaOrdersCount)   // Количество заказов в Kafka
		erpGroup.GET("/kafka-orders-sample", erpController.GetKafkaOrdersSample) // Примеры заказов из Kafka
		
		// Управление слотами
		erpGroup.GET("/slots", erpController.GetSlots)                    // Получить все слоты
		erpGroup.GET("/slots/config", erpController.GetSlotConfig)        // Получить конфигурацию слотов
		erpGroup.PUT("/slots/config", erpController.UpdateSlotConfig)     // Обновить конфигурацию слотов
		
		// Управление станциями кухни
		erpGroup.GET("/stations", stationsController.GetStations)                    // Получить все станции
		erpGroup.GET("/stations/capabilities", stationsController.GetCapabilities)  // Получить capabilities и категории
		erpGroup.POST("/stations", stationsController.CreateStation)                // Создать станцию
		erpGroup.PUT("/stations/:id", stationsController.UpdateStation)             // Обновить станцию
		erpGroup.DELETE("/stations/:id", stationsController.DeleteStation)          // Удалить станцию

		// Staff Management
		erpGroup.GET("/staff", staffController.GetStaff)                           // Получить список сотрудников
		erpGroup.GET("/staff/roles", staffController.GetAvailableRoles)           // Получить доступные роли
		erpGroup.POST("/staff", staffController.CreateStaff)                       // Создать сотрудника
		erpGroup.PUT("/staff/:id", staffController.UpdateStaff)                   // Обновить сотрудника
		erpGroup.PUT("/staff/:id/status", staffController.UpdateStaffStatus)       // Обновить статус сотрудника (с валидацией State Machine)
		erpGroup.DELETE("/staff/:id", staffController.DeleteStaff)                // Удалить сотрудника
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
			stockGroup.POST("/process-sale", stockController.ProcessSaleDepletion)           // Автоматическое списание при продаже
		stockGroup.POST("/commit-production", stockController.CommitProduction)          // Ручное производство полуфабриката
		stockGroup.GET("/recipes/:id/prime-cost", stockController.GetRecipePrimeCost)   // Расчет себестоимости рецепта
			stockGroup.POST("/check-expiry-alerts", stockController.CheckExpiryAlerts) // Ручная проверка сроков
			stockGroup.POST("/process-inbound-invoice", stockController.ProcessInboundInvoice) // Обработка входящей накладной
		}
		log.Println("📊 Stock endpoints enabled: /api/v1/inventory/stock")
	} else {
		log.Println("⚠️ Stock endpoints not enabled: PostgreSQL not available")
	}

	// Управление рецептами
	if db != nil && recipeService != nil {
		recipeController := api.NewRecipeController(recipeService)
		recipeGroup := apiGroup.Group("/recipes")
		{
			recipeGroup.GET("", recipeController.GetRecipes)           // Список рецептов
			recipeGroup.GET("/:id", recipeController.GetRecipe)         // Получить рецепт
			recipeGroup.POST("", recipeController.CreateRecipe)         // Создать рецепт
			recipeGroup.PUT("/:id", recipeController.UpdateRecipe)      // Обновить рецепт
			recipeGroup.DELETE("/:id", recipeController.DeleteRecipe)   // Удалить рецепт
		}
		log.Println("📋 Recipe endpoints enabled: /api/v1/recipes")
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
				counterpartyGroup.GET("/:id", counterpartyController.GetCounterparty)         // Получить контрагента
				counterpartyGroup.POST("", counterpartyController.CreateCounterparty)        // Создать контрагента
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
		grpcOrderServer := api.NewOrderGRPCServer(redisUtil, cfg.KafkaBrokers, cfg.BusinessOpenHour, cfg.BusinessCloseHour, cfg.BusinessCloseMin)
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

	log.Printf("🚀 Server starting on port %s", port)
	log.Printf("📡 API доступен на http://0.0.0.0:%s/api/v1", port)
	if err := r.Run("0.0.0.0:" + port); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
