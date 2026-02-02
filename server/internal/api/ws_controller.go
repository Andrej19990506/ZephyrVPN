package api

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		// Разрешаем подключения с любого origin (для разработки)
		// В продакшене лучше проверять конкретные домены
		return true
	},
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

// ServeWS обрабатывает WebSocket подключения от планшетов поваров
func ServeWS(c *gin.Context) {
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("⚠️ Ошибка обновления WebSocket соединения: %v", err)
		return
	}

	// Добавляем клиента в хаб
	GlobalHub.AddClient(conn)
	log.Printf("📱 Планшет повара подключен. Всего подключений: %d", GlobalHub.GetClientsCount())

	// Обрабатываем отключение клиента
	defer func() {
		GlobalHub.RemoveClient(conn)
		log.Printf("📱 Планшет повара отключен. Осталось подключений: %d", GlobalHub.GetClientsCount())
	}()

	// Читаем сообщения от клиента (ping/pong для поддержания соединения)
	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("⚠️ WebSocket ошибка: %v", err)
			}
			break
		}
	}
}

