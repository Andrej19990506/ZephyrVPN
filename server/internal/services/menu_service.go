package services

import (
	"encoding/json"
	"log"
	"sync"
	"time"

	"zephyrvpn/server/internal/models"
	"zephyrvpn/server/internal/utils"
	"gorm.io/gorm"
)

const MenuUpdateChannel = "menu:update" // Канал для Pub/Sub обновлений меню

// MenuService управляет загрузкой и кэшированием меню из БД
type MenuService struct {
	db            *gorm.DB
	redisUtil     *utils.RedisClient // Redis для Pub/Sub
	mu            sync.RWMutex
	lastUpdate    time.Time
	updateInterval time.Duration
	stopPubSub    chan struct{} // Канал для остановки Pub/Sub
}

// NewMenuService создает новый сервис меню
func NewMenuService(db *gorm.DB, redisUtil *utils.RedisClient) *MenuService {
	return &MenuService{
		db:             db,
		redisUtil:      redisUtil,
		updateInterval: 5 * time.Minute, // Fallback: обновляем каждые 5 минут
		stopPubSub:     make(chan struct{}),
	}
}

// LoadMenu загружает меню из БД и обновляет in-memory кэш
// Потокобезопасно: сначала создает новые мапы, потом атомарно заменяет
func (ms *MenuService) LoadMenu() error {
	// 1. Загружаем данные из БД (БЕЗ блокировки - это может быть долго)
	var recipes []models.PizzaRecipe
	if err := ms.db.Where("is_active = ?", true).Find(&recipes).Error; err != nil {
		return err
	}

	// 2. Создаем НОВЫЕ мапы (не трогаем старые)
	pizzasMap := make(map[string]models.Pizza)
	for _, recipe := range recipes {
		var ingredients []string
		var ingredientAmounts map[string]int

		// Парсим JSON ингредиентов
		if err := json.Unmarshal([]byte(recipe.Ingredients), &ingredients); err != nil {
			log.Printf("⚠️ Ошибка парсинга ингредиентов для %s: %v", recipe.Name, err)
			ingredients = []string{}
		}

		// Парсим JSON дозировок
		if err := json.Unmarshal([]byte(recipe.IngredientAmounts), &ingredientAmounts); err != nil {
			log.Printf("⚠️ Ошибка парсинга дозировок для %s: %v", recipe.Name, err)
			ingredientAmounts = make(map[string]int)
		}

		pizzasMap[recipe.Name] = models.Pizza{
			Name:              recipe.Name,
			Price:             recipe.Price,
			Ingredients:       ingredients,
			IngredientAmounts: ingredientAmounts,
		}
	}

	// Загружаем наборы
	var setsDB []models.PizzaSetDB
	if err := ms.db.Where("is_active = ?", true).Find(&setsDB).Error; err != nil {
		return err
	}

	setsMap := make(map[string]models.PizzaSet)
	for _, setDB := range setsDB {
		var pizzas []string
		if err := json.Unmarshal([]byte(setDB.Pizzas), &pizzas); err != nil {
			log.Printf("⚠️ Ошибка парсинга пицц для набора %s: %v", setDB.Name, err)
			pizzas = []string{}
		}

		setsMap[setDB.Name] = models.PizzaSet{
			Name:        setDB.Name,
			Description: setDB.Description,
			Pizzas:      pizzas,
			Price:       setDB.Price,
		}
	}

	// Загружаем допы
	var extrasDB []models.ExtraDB
	if err := ms.db.Where("is_active = ?", true).Find(&extrasDB).Error; err != nil {
		return err
	}

	extrasMap := make(map[string]models.Extra)
	for _, extraDB := range extrasDB {
		extrasMap[extraDB.Name] = models.Extra{
			Name:  extraDB.Name,
			Price: extraDB.Price,
		}
	}

	// 3. Атомарно заменяем глобальные мапы (быстрая операция под мьютексом)
	models.SetPizzas(pizzasMap)
	models.SetSets(setsMap)
	models.SetExtras(extrasMap)

	// 4. Обновляем время последнего обновления
	ms.mu.Lock()
	ms.lastUpdate = time.Now()
	ms.mu.Unlock()

	log.Printf("✅ Меню обновлено из БД: %d пицц, %d наборов, %d допов", 
		len(pizzasMap), len(setsMap), len(extrasMap))
	
	return nil
}

// StartAutoReload запускает автоматическое обновление меню
// Использует Redis Pub/Sub для мгновенного обновления + таймер как fallback
func (ms *MenuService) StartAutoReload() {
	// 1. Redis Pub/Sub для мгновенного обновления (Level: Senior)
	if ms.redisUtil != nil {
		go ms.startPubSubListener()
		log.Println("📡 Redis Pub/Sub для меню запущен (мгновенное обновление)")
	}

	// 2. Таймер как fallback (на случай если Redis недоступен)
	go func() {
		ticker := time.NewTicker(ms.updateInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if err := ms.LoadMenu(); err != nil {
					log.Printf("⚠️ Ошибка автообновления меню: %v", err)
				}
			case <-ms.stopPubSub:
				return
			}
		}
	}()
	log.Println("🔄 Fallback автообновление меню запущено (каждые 5 минут)")
}

// startPubSubListener слушает Redis канал для мгновенного обновления меню
func (ms *MenuService) startPubSubListener() {
	if ms.redisUtil == nil {
		return
	}

	ch, closeFn := ms.redisUtil.Subscribe(MenuUpdateChannel)
	defer func() {
		if err := closeFn(); err != nil {
			log.Printf("⚠️ Ошибка закрытия Pub/Sub: %v", err)
		}
	}()

	log.Printf("👂 Слушаем канал Redis: %s", MenuUpdateChannel)

	for {
		select {
		case msg, ok := <-ch:
			if !ok {
				// Канал закрыт, пытаемся переподписаться
				log.Println("⚠️ Pub/Sub канал закрыт, переподписываемся...")
				ch, closeFn = ms.redisUtil.Subscribe(MenuUpdateChannel)
				continue
			}
			if msg != nil {
				log.Printf("🔔 Получено событие обновления меню из Redis: %s", msg.Payload)
				if err := ms.LoadMenu(); err != nil {
					log.Printf("⚠️ Ошибка обновления меню по Pub/Sub: %v", err)
				} else {
					log.Println("✅ Меню обновлено мгновенно через Redis Pub/Sub")
				}
			}
		case <-ms.stopPubSub:
			log.Println("🛑 Остановка Pub/Sub listener для меню")
			return
		}
	}
}

// PublishUpdate публикует событие обновления меню в Redis (для админки)
func (ms *MenuService) PublishUpdate() error {
	if ms.redisUtil == nil {
		return nil // Если Redis нет, просто обновляем локально
	}
	return ms.redisUtil.Publish(MenuUpdateChannel, "now")
}

// ForceReload принудительно обновляет меню (для админ-эндпоинта)
func (ms *MenuService) ForceReload() error {
	return ms.LoadMenu()
}

// GetLastUpdate возвращает время последнего обновления
func (ms *MenuService) GetLastUpdate() time.Time {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	return ms.lastUpdate
}

