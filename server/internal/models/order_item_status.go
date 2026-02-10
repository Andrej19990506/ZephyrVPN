package models

import (
	"time"
)

// OrderItemStatus представляет статус одной позиции в заказе на станции
type OrderItemStatus struct {
	OrderID      string    `json:"order_id"`       // ID заказа
	ItemIndex    int       `json:"item_index"`     // Индекс позиции в массиве Items
	Category     string    `json:"category"`       // Категория: "pizza", "appetizers", "drinks", "consumables" (DEPRECATED)
	StationID    string    `json:"station_id"`     // ID текущей станции, которая обрабатывает эту позицию
	StationIDs   string    `json:"station_ids"`     // JSON массив всех станций, через которые проходит позиция (из Recipe)
	CurrentStationIndex int `json:"current_station_index"` // Индекс текущей станции в массиве StationIDs (0 = первая станция)
	Status       string    `json:"status"`         // Статус: "pending", "preparing", "ready", "completed"
	StartedAt    time.Time `json:"started_at,omitempty"`     // Когда начали готовить
	CompletedAt  time.Time `json:"completed_at,omitempty"`   // Когда завершили
	UpdatedAt    time.Time `json:"updated_at"`     // Когда последний раз обновлялся
}

// OrderStationMapping представляет привязку заказа к станциям
// Хранится в Redis: erp:order:{order_id}:stations
type OrderStationMapping struct {
	OrderID           string                      `json:"order_id"`
	ItemStatuses      []OrderItemStatus           `json:"item_statuses"`      // Статус каждой позиции
	StationAssignments map[string][]int           `json:"station_assignments"` // station_id -> []item_index
	CurrentStage      string                      `json:"current_stage"`     // "preparation", "baking", "packaging"
	CreatedAt         time.Time                   `json:"created_at"`
	UpdatedAt         time.Time                   `json:"updated_at"`
}

// GetItemCategory определяет категорию товара (DEPRECATED - используйте StationID из Recipe)
// Оставлено для обратной совместимости, если Recipe не найден
// По умолчанию возвращает "pizza"
func GetItemCategory(itemName string) string {
	// DEPRECATED: Категория теперь определяется через Recipe.StationID
	// Эта функция используется только как fallback, если Recipe не найден
	return "pizza"
}

