package api

import (
	"encoding/json"
	"log"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// ServeERPWS обрабатывает WebSocket подключения от ERP системы
func ServeERPWS(c *gin.Context) {
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("⚠️ Ошибка обновления WebSocket соединения ERP: %v", err)
		return
	}

	// Добавляем клиента в ERP хаб
	ERPHub.AddClient(conn)
	log.Printf("🖥️ ERP клиент подключен. Всего ERP подключений: %d", ERPHub.GetClientsCount())

	// Обрабатываем отключение клиента
	defer func() {
		ERPHub.RemoveClient(conn)
		log.Printf("🖥️ ERP клиент отключен. Осталось подключений: %d", ERPHub.GetClientsCount())
	}()

	// Читаем сообщения от клиента (ping/pong для поддержания соединения)
	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("⚠️ WebSocket ERP ошибка: %v", err)
			}
			break
		}
	}
}

// BroadcastERPUpdate отправляет обновление заказов всем подключенным ERP клиентам
func BroadcastERPUpdate(messageType string, data interface{}) {
	update := map[string]interface{}{
		"type": messageType,
		"data": data,
		"timestamp": time.Now().Unix(),
	}
	
	jsonData, err := json.Marshal(update)
	if err != nil {
		log.Printf("⚠️ Ошибка маршалинга ERP обновления: %v", err)
		return
	}
	
	ERPHub.BroadcastMessage(jsonData)
}

