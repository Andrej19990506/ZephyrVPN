package services

import (
	"encoding/json"
	"log"
	"strings"
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

// getIngredientComposition формирует строку состава из ингредиентов
func getIngredientComposition(ingredients []models.RecipeIngredient) []string {
	var names []string
	for _, ingredient := range ingredients {
		if ingredient.Nomenclature != nil && ingredient.Nomenclature.Name != "" {
			names = append(names, ingredient.Nomenclature.Name)
		}
	}
	return names
}

// LoadMenu загружает меню из БД и обновляет in-memory кэш
// Потокобезопасно: сначала создает новые мапы, потом атомарно заменяет
// Оптимизировано: загружает все Recipe с ингредиентами одним запросом (избегает N+1)
func (ms *MenuService) LoadMenu() error {
	// 1. Загружаем все активные Recipe с ингредиентами одним запросом (оптимизация N+1)
	// ВАЖНО: Загружаем только Sales Recipes (IsSemiFinished = false) - готовые товары для продажи
	// Production Recipes (IsSemiFinished = true) - это полуфабрикаты, они не должны быть в меню
	var allRecipes []models.Recipe
	if err := ms.db.Where("is_active = true AND is_semi_finished = false AND deleted_at IS NULL").
		Preload("Ingredients", "is_optional = ?", false). // Только неопциональные ингредиенты
		Preload("Ingredients.Nomenclature").
		Find(&allRecipes).Error; err != nil {
		log.Printf("⚠️ Ошибка загрузки Recipe: %v", err)
		// Продолжаем работу со старыми данными
	}
	
	// Фильтруем Recipe: оставляем только те, у которых MenuItemID связан с NomenclatureItem с IsSaleable=true
	// Это гарантирует, что в меню попадают только товары, явно помеченные как "для продажи"
	var saleableRecipes []models.Recipe
	for i := range allRecipes {
		recipe := &allRecipes[i]
		// Если MenuItemID не указан, пропускаем (старая логика по имени все еще работает)
		if recipe.MenuItemID == nil || *recipe.MenuItemID == "" {
			saleableRecipes = append(saleableRecipes, *recipe)
			continue
		}
		
		// Проверяем, что связанный NomenclatureItem имеет IsSaleable=true И IsReadyForSale=true
		var nomenclature models.NomenclatureItem
		if err := ms.db.Where("id = ? AND is_saleable = true AND is_ready_for_sale = true AND is_active = true AND deleted_at IS NULL", *recipe.MenuItemID).
			First(&nomenclature).Error; err == nil {
			// NomenclatureItem найден, помечен как saleable И готов к продаже - добавляем в список
			saleableRecipes = append(saleableRecipes, *recipe)
		} else {
			// NomenclatureItem не найден, не помечен как saleable или не готов к продаже - пропускаем
			log.Printf("⚠️ Recipe '%s' пропущен: связанный NomenclatureItem (ID: %s) не найден, не IsSaleable=true или не IsReadyForSale=true", 
				recipe.Name, *recipe.MenuItemID)
		}
	}
	
	// Используем отфильтрованный список
	allRecipes = saleableRecipes

	// Создаем индекс Recipe по имени для быстрого поиска
	recipeIndexByName := make(map[string]*models.Recipe)
	recipeIndexByMenuItemID := make(map[string]*models.Recipe)
	for i := range allRecipes {
		recipe := &allRecipes[i]
		// Индекс по имени (case-insensitive ключ)
		key := strings.ToLower(recipe.Name)
		if _, exists := recipeIndexByName[key]; !exists {
			recipeIndexByName[key] = recipe
		}
		// Индекс по MenuItemID (если есть)
		if recipe.MenuItemID != nil && *recipe.MenuItemID != "" {
			recipeIndexByMenuItemID[*recipe.MenuItemID] = recipe
		}
	}

	// 2. Загружаем данные из старой системы PizzaRecipe
	var pizzaRecipes []models.PizzaRecipe
	if err := ms.db.Where("is_active = ?", true).Find(&pizzaRecipes).Error; err != nil {
		return err
	}

	// 3. Создаем НОВЫЕ мапы (не трогаем старые)
	// ВАЖНО: Показываем только пиццы, у которых есть Recipe в новой системе
	// Это гарантирует, что ингредиенты загружаются из номенклатуры, а не из устаревших JSON
	pizzasMap := make(map[string]models.Pizza)
	skippedCount := 0
	
	for _, pizzaRecipe := range pizzaRecipes {
		// Ищем Recipe в новой системе по имени (case-insensitive)
		recipeKey := strings.ToLower(pizzaRecipe.Name)
		recipeModel, found := recipeIndexByName[recipeKey]
		
		if !found {
			// Рецепт не найден в новой системе - ПРОПУСКАЕМ эту пиццу
			// Она не будет отображаться в меню до тех пор, пока не будет создан Recipe
			log.Printf("⚠️ Пицца '%s' пропущена: Recipe не найден в новой системе. Создайте Recipe для отображения в меню.", pizzaRecipe.Name)
			skippedCount++
			continue
		}
		
		// Найден рецепт в новой системе, загружаем названия ингредиентов из номенклатуры
		ingredientNames := getIngredientComposition(recipeModel.Ingredients)
		
		// Парсим старые данные для обратной совместимости (если нужно)
		var ingredients []string
		var ingredientAmounts map[string]int
		if err := json.Unmarshal([]byte(pizzaRecipe.Ingredients), &ingredients); err != nil {
			log.Printf("⚠️ Ошибка парсинга ингредиентов для %s: %v", pizzaRecipe.Name, err)
			ingredients = []string{}
		}
		if err := json.Unmarshal([]byte(pizzaRecipe.IngredientAmounts), &ingredientAmounts); err != nil {
			log.Printf("⚠️ Ошибка парсинга дозировок для %s: %v", pizzaRecipe.Name, err)
			ingredientAmounts = make(map[string]int)
		}
		
		pizzasMap[pizzaRecipe.Name] = models.Pizza{
			Name:              pizzaRecipe.Name,
			Price:             pizzaRecipe.Price,
			Ingredients:       ingredients, // Старые данные для обратной совместимости
			IngredientAmounts: ingredientAmounts,
			IngredientNames:   ingredientNames, // Динамические названия из номенклатуры
		}
		
		log.Printf("✅ Загружена пицца '%s': %d ингредиентов из номенклатуры", pizzaRecipe.Name, len(ingredientNames))
	}
	
	if skippedCount > 0 {
		log.Printf("⚠️ Пропущено пицц без Recipe: %d. Создайте Recipe для этих пицц, чтобы они отображались в меню.", skippedCount)
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
	extrasMapByID := make(map[uint]models.Extra) // Дополнительная мапа по ID для быстрого поиска
	for _, extraDB := range extrasDB {
		extra := models.Extra{
			ID:    extraDB.ID,
			Name:  extraDB.Name,
			Price: extraDB.Price,
		}
		extrasMap[extraDB.Name] = extra
		extrasMapByID[extraDB.ID] = extra
	}

	// 3. Атомарно заменяем глобальные мапы (быстрая операция под мьютексом)
	models.SetPizzas(pizzasMap)
	models.SetSets(setsMap)
	models.SetExtras(extrasMap)

	// 4. Обновляем время последнего обновления
	ms.mu.Lock()
	ms.lastUpdate = time.Now()
	ms.mu.Unlock()

	log.Printf("✅ Меню обновлено из БД: %d пицц (только с Recipe), %d наборов, %d допов", 
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

