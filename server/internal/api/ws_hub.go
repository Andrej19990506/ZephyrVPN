package api

import (
	"sync"

	"github.com/gorilla/websocket"
)

// Hub управляет WebSocket соединениями для планшетов поваров
type Hub struct {
	clients   map[*websocket.Conn]bool
	broadcast chan []byte
	mutex     sync.RWMutex
}

// GlobalHub - глобальный хаб для всех WebSocket соединений (планшеты поваров)
var GlobalHub = &Hub{
	clients:   make(map[*websocket.Conn]bool),
	broadcast: make(chan []byte, 256), // Буферизованный канал для производительности
}

// ERPHub - хаб для ERP системы (отдельный от планшетов поваров)
var ERPHub = &Hub{
	clients:   make(map[*websocket.Conn]bool),
	broadcast: make(chan []byte, 256),
}

// Run запускает хаб для обработки сообщений
func (h *Hub) Run() {
	for {
		msg := <-h.broadcast
		h.mutex.RLock()
		for client := range h.clients {
			err := client.WriteMessage(websocket.TextMessage, msg)
			if err != nil {
				// Удаляем клиента при ошибке записи
				h.mutex.RUnlock()
				h.RemoveClient(client)
				h.mutex.RLock()
			}
		}
		h.mutex.RUnlock()
	}
}

// AddClient добавляет нового клиента (планшет повара)
func (h *Hub) AddClient(conn *websocket.Conn) {
	h.mutex.Lock()
	h.clients[conn] = true
	h.mutex.Unlock()
}

// RemoveClient удаляет клиента
func (h *Hub) RemoveClient(conn *websocket.Conn) {
	h.mutex.Lock()
	if _, ok := h.clients[conn]; ok {
		delete(h.clients, conn)
		conn.Close()
	}
	h.mutex.Unlock()
}

// BroadcastMessage отправляет сообщение всем подключенным клиентам
func (h *Hub) BroadcastMessage(message []byte) {
	select {
	case h.broadcast <- message:
	default:
		// Если канал переполнен, пропускаем сообщение (не блокируем)
	}
}

// GetClientsCount возвращает количество подключенных клиентов
func (h *Hub) GetClientsCount() int {
	h.mutex.RLock()
	defer h.mutex.RUnlock()
	return len(h.clients)
}

