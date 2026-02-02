package models

import (
	"sync"
	"time"
)

var (
	// Мьютексы для защиты глобальных мап от concurrent access
	// Критично для Pub/Sub обновлений и высоких нагрузок (50k+ RPS)
	pizzasMu sync.RWMutex
	setsMu   sync.RWMutex
	extrasMu sync.RWMutex
)

// Доступные пиццы
var AvailablePizzas = map[string]Pizza{
	"Английский завтрак": {
		Name:        "Английский завтрак",
		Price:       599,
		Ingredients: []string{"сыр моцарелла", "бекон", "яйцо", "помидоры", "лук", "соус"},
		IngredientAmounts: map[string]int{
			"сыр моцарелла": 150,
			"бекон":         80,
			"яйцо":          100,
			"помидоры":      120,
			"лук":           60,
			"соус":          80,
		},
	},
	"Солянка Злодейская": {
		Name:        "Солянка Злодейская",
		Price:       799,
		Ingredients: []string{"сыр моцарелла", "колбаса", "огурцы маринованные", "оливки", "пепперони", "бекон", "острый перец", "соус"},
		IngredientAmounts: map[string]int{
			"сыр моцарелла":     150,
			"колбаса":           100,
			"огурцы маринованные": 80,
			"оливки":            50,
			"пепперони":         100,
			"бекон":             80,
			"острый перец":      30,
			"соус":              80,
		},
	},
	"Классическая": {
		Name:        "Классическая",
		Price:       499,
		Ingredients: []string{"сыр моцарелла", "помидоры", "базилик", "соус"},
		IngredientAmounts: map[string]int{
			"сыр моцарелла": 150,
			"помидоры":      120,
			"базилик":       10,
			"соус":          80,
		},
	},
	"New York": {
		Name:        "New York",
		Price:       699,
		Ingredients: []string{"сыр моцарелла", "пепперони", "грибы", "лук", "соус"},
		IngredientAmounts: map[string]int{
			"сыр моцарелла": 150,
			"пепперони":     100,
			"грибы":         100,
			"лук":           60,
			"соус":          80,
		},
	},
	"Пепперони": {
		Name:        "Пепперони",
		Price:       549,
		Ingredients: []string{"сыр моцарелла", "пепперони", "соус"},
		IngredientAmounts: map[string]int{
			"сыр моцарелла": 150,
			"пепперони":     100,
			"соус":          80,
		},
	},
	"Мясная": {
		Name:        "Мясная",
		Price:       749,
		Ingredients: []string{"сыр моцарелла", "бекон", "колбаса", "ветчина", "соус"},
		IngredientAmounts: map[string]int{
			"сыр моцарелла": 150,
			"бекон":         80,
			"колбаса":       100,
			"ветчина":       80,
			"соус":          80,
		},
	},
	"Охотничья": {
		Name:        "Охотничья",
		Price:       699,
		Ingredients: []string{"сыр моцарелла", "колбаса охотничья", "грибы", "лук", "соус"},
		IngredientAmounts: map[string]int{
			"сыр моцарелла":     150,
			"колбаса охотничья": 100,
			"грибы":             100,
			"лук":               60,
			"соус":              80,
		},
	},
	"Курица и Грибы": {
		Name:        "Курица и Грибы",
		Price:       899,
		Ingredients: []string{"сыр моцарелла", "курица", "грибы", "соус"},
		IngredientAmounts: map[string]int{
			"сыр моцарелла": 150,
			"курица":        120,
			"грибы":         100,
			"соус":          80,
		},
	},
}

// Допы
var AvailableExtras = map[string]Extra{
	"Сырный бортик": {
		Name:  "Сырный бортик",
		Price: 199,
	},
}

// Наборы пицц
var AvailableSets = map[string]PizzaSet{
	"Семейный набор": {
		Name:        "Семейный набор",
		Description: "2 пиццы на выбор + сырный бортик",
		Pizzas:      []string{"Классическая", "Пепперони"},
		Price:       1200, // Скидка от обычной цены
	},
	"Мясной набор": {
		Name:        "Мясной набор",
		Description: "Мясная + New York + Английский завтрак + Классическая + Курица и Грибы",
		Pizzas:      []string{"Мясная", "Охотничья"},
		Price:       1500,
	},
	"Пицца-пати": {
		Name:        "Пицца-пати",
		Description: "3 пиццы: New York + Солянка Злодейская + Английский завтрак",
		Pizzas:      []string{"New York", "Солянка Злодейская", "Английский завтрак"},
		Price:       2000,
	},
}

type PizzaSet struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Pizzas      []string `json:"pizzas"`
	Price       int      `json:"price"` // Общая цена набора
}

type Pizza struct {
	Name              string          `json:"name"`
	Price             int             `json:"price"` // в рублях
	Ingredients       []string        `json:"ingredients"`
	IngredientAmounts map[string]int  `json:"ingredient_amounts"` // Дозировка ингредиентов в граммах
}

type Extra struct {
	Name  string `json:"name"`
	Price int    `json:"price"` // в рублях
}

type PizzaItem struct {
	PizzaName   string   `json:"pizza_name"`
	Ingredients []string `json:"ingredients"`
	IngredientAmounts map[string]int `json:"ingredient_amounts,omitempty"` // Дозировка ингредиентов в граммах
	Extras      []string `json:"extras,omitempty"` // Допы (сырный бортик и т.д.)
	ExcludeIngredients []string `json:"exclude_ingredients,omitempty"` // Что НЕ класть (для поваров)
	Quantity    int      `json:"quantity"`
	Price       int      `json:"price"`
	SetName     string   `json:"set_name,omitempty"` // Название набора, если это элемент набора
	IsSetItem   bool     `json:"is_set_item,omitempty"` // Флаг что это элемент набора
}

type PizzaOrder struct {
	ID          string      `json:"id"`
	DisplayID   string      `json:"display_id"`
	CustomerID  int         `json:"customer_id,omitempty"`
	Items       []PizzaItem `json:"items"` // Может быть набор или отдельные пиццы
	IsSet       bool        `json:"is_set"` // Набор или отдельные пиццы
	SetName     string      `json:"set_name,omitempty"` // Название набора если is_set=true
	TotalPrice  int         `json:"total_price"`
	CreatedAt   time.Time   `json:"created_at"`
	Status      string      `json:"status"` // "pending", "preparing", "ready", "delivered"
	
	// Информация для курьеров
	CustomerFirstName string `json:"customer_first_name,omitempty"` // Имя клиента
	CustomerLastName  string `json:"customer_last_name,omitempty"`  // Фамилия клиента
	DeliveryAddress    string `json:"delivery_address,omitempty"`    // Адрес доставки
	CustomerPhone      string `json:"customer_phone,omitempty"`      // Телефон клиента
	CallBeforeMinutes  int    `json:"call_before_minutes,omitempty"` // Позвонить за N минут до доставки
	PaymentMethod      string `json:"payment_method,omitempty"`      // CASH, CARD_ONLINE, CRYPTO
	IsPickup           bool   `json:"is_pickup"`                     // Самовывоз
	PickupLocationID   string `json:"pickup_location_id,omitempty"`  // ID филиала для самовывоза
	
	// Информация для админов
	DiscountAmount    int    `json:"discount_amount,omitempty"`    // Сумма скидки
	DiscountPercent   int    `json:"discount_percent,omitempty"`   // Процент скидки
	FinalPrice        int    `json:"final_price,omitempty"`        // Итоговая цена со скидкой
	Notes             string `json:"notes,omitempty"`               // Дополнительные заметки
	
	// Capacity-Based Slot Scheduling
	TargetSlotID      string    `json:"target_slot_id,omitempty"`     // ID временного слота
	TargetSlotStartTime time.Time `json:"target_slot_start_time,omitempty"` // Время начала слота (UTC, RFC3339)
	VisibleAt         time.Time `json:"visible_at,omitempty"`         // Время, когда заказ должен появиться на планшете (UTC, RFC3339)
}

// Потокобезопасные геттеры для чтения меню (критично для Pub/Sub и высоких нагрузок)

// GetPizza безопасно получает пиццу по имени (с RLock)
func GetPizza(name string) (Pizza, bool) {
	pizzasMu.RLock()
	defer pizzasMu.RUnlock()
	pizza, ok := AvailablePizzas[name]
	return pizza, ok
}

// GetSet безопасно получает набор по имени (с RLock)
func GetSet(name string) (PizzaSet, bool) {
	setsMu.RLock()
	defer setsMu.RUnlock()
	set, ok := AvailableSets[name]
	return set, ok
}

// GetExtra безопасно получает доп по имени (с RLock)
func GetExtra(name string) (Extra, bool) {
	extrasMu.RLock()
	defer extrasMu.RUnlock()
	extra, ok := AvailableExtras[name]
	return extra, ok
}

// GetAllPizzas безопасно возвращает копию всех пицц (для итерации)
func GetAllPizzas() map[string]Pizza {
	pizzasMu.RLock()
	defer pizzasMu.RUnlock()
	result := make(map[string]Pizza, len(AvailablePizzas))
	for k, v := range AvailablePizzas {
		result[k] = v
	}
	return result
}

// GetAllSets безопасно возвращает копию всех наборов (для итерации)
func GetAllSets() map[string]PizzaSet {
	setsMu.RLock()
	defer setsMu.RUnlock()
	result := make(map[string]PizzaSet, len(AvailableSets))
	for k, v := range AvailableSets {
		result[k] = v
	}
	return result
}

// GetAllExtras безопасно возвращает копию всех допов (для итерации)
func GetAllExtras() map[string]Extra {
	extrasMu.RLock()
	defer extrasMu.RUnlock()
	result := make(map[string]Extra, len(AvailableExtras))
	for k, v := range AvailableExtras {
		result[k] = v
	}
	return result
}

// SetPizzas атомарно заменяет мапу пицц (используется MenuService)
func SetPizzas(newPizzas map[string]Pizza) {
	pizzasMu.Lock()
	defer pizzasMu.Unlock()
	AvailablePizzas = newPizzas
}

// SetSets атомарно заменяет мапу наборов (используется MenuService)
func SetSets(newSets map[string]PizzaSet) {
	setsMu.Lock()
	defer setsMu.Unlock()
	AvailableSets = newSets
}

// SetExtras атомарно заменяет мапу допов (используется MenuService)
func SetExtras(newExtras map[string]Extra) {
	extrasMu.Lock()
	defer extrasMu.Unlock()
	AvailableExtras = newExtras
}
