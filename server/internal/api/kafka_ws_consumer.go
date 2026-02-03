package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync/atomic"
	"time"

	"github.com/segmentio/kafka-go"
	"google.golang.org/protobuf/proto"
	"zephyrvpn/server/internal/models"
	"zephyrvpn/server/internal/pb"
	"zephyrvpn/server/internal/utils"
)

// KafkaWSConsumer читает заказы из Kafka и отправляет их в WebSocket
type KafkaWSConsumer struct {
	brokers    []string
	topic      string
	groupID    string
	reader     *kafka.Reader
	ctx        context.Context
	cancel     context.CancelFunc
	redisUtil  *utils.RedisClient
	processed  int64 // Счетчик обработанных заказов
	lastLog    int64 // Время последнего лога
}

// NewKafkaWSConsumer создает новый Kafka Consumer для WebSocket
func NewKafkaWSConsumer(brokers string, topic string, redisUtil *utils.RedisClient, username, password, caCert string) *KafkaWSConsumer {
	brokerList := ParseKafkaBrokers(brokers)
	ctx, cancel := context.WithCancel(context.Background())
	
	// Создаем dialer с SASL/PLAIN и TLS если нужно
	dialer := CreateKafkaDialer(username, password, caCert)
	
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:     brokerList,
		Topic:       topic,
		GroupID:     "kitchen-ws-group-v3", // Новый GroupID чтобы читать все заказы заново
		StartOffset: kafka.FirstOffset,      // Начинаем с самого начала очереди
		MinBytes:    1,
		MaxBytes:    10e6,
		MaxWait:     1 * time.Second,
		Dialer:      dialer, // Используем dialer с SASL/TLS
	})
	
	return &KafkaWSConsumer{
		brokers:   brokerList,
		topic:     topic,
		groupID:  "kitchen-ws-group-v3",
		reader:   reader,
		ctx:      ctx,
		cancel:   cancel,
		redisUtil: redisUtil,
		lastLog:  time.Now().Unix(),
	}
}

// Start запускает чтение из Kafka и отправку в WebSocket
func (kc *KafkaWSConsumer) Start() {
	log.Printf("📡 Kafka WS Consumer запущен: topic=%s, groupID=%s, startOffset=FirstOffset", kc.topic, kc.groupID)
	
	go func() {
		for {
			select {
			case <-kc.ctx.Done():
				log.Println("🛑 Kafka WS Consumer остановлен")
				return
			default:
				// Читаем сообщение из Kafka
				msg, err := kc.reader.ReadMessage(kc.ctx)
				if err != nil {
					if err == context.Canceled {
						return
					}
					log.Printf("⚠️ Kafka WS Consumer ошибка чтения: %v", err)
					time.Sleep(1 * time.Second)
					continue
				}
				
				log.Printf("📨 Kafka WS Consumer: получено сообщение offset=%d, partition=%d, size=%d bytes", 
					msg.Offset, msg.Partition, len(msg.Value))
				
				// Пробуем распарсить Protobuf
				pbOrder := &pb.PizzaOrder{}
				var order models.PizzaOrder
				
				if err := proto.Unmarshal(msg.Value, pbOrder); err == nil {
					// Успешно распарсили Protobuf - конвертируем в models.PizzaOrder
					order = models.PizzaOrder{
						ID:               pbOrder.Id,
						DisplayID:        pbOrder.DisplayId,
						CustomerID:       int(pbOrder.CustomerId),
						CustomerFirstName: pbOrder.CustomerFirstName,
						CustomerLastName:  pbOrder.CustomerLastName,
						CustomerPhone:     pbOrder.CustomerPhone,
						DeliveryAddress:   pbOrder.DeliveryAddress,
						IsPickup:          pbOrder.IsPickup,
						PickupLocationID:  pbOrder.PickupLocationId,
						TotalPrice:        int(pbOrder.TotalPrice),
						CreatedAt:         time.Unix(0, pbOrder.CreatedAt),
						Status:            pbOrder.Status,
						IsSet:             pbOrder.IsSet,
						SetName:           pbOrder.SetName,
						TargetSlotID:      pbOrder.TargetSlotId,
					}
					
					// Получаем VisibleAt из protobuf, если есть
					if pbOrder.VisibleAt != "" {
						if visibleAt, err := time.Parse(time.RFC3339, pbOrder.VisibleAt); err == nil {
							order.VisibleAt = visibleAt
						}
					}
					// Конвертируем Items если есть
					for _, pbItem := range pbOrder.Items {
						item := models.PizzaItem{
							PizzaName:   pbItem.PizzaName,
							Ingredients: pbItem.Ingredients,
							Extras:      pbItem.Extras,
							Quantity:    int(pbItem.Quantity),
							Price:       int(pbItem.Price),
						}
						// Конвертируем дозировки из protobuf (map[string]int32 -> map[string]int)
						if pbItem.IngredientAmounts != nil && len(pbItem.IngredientAmounts) > 0 {
							item.IngredientAmounts = make(map[string]int)
							for k, v := range pbItem.IngredientAmounts {
								item.IngredientAmounts[k] = int(v)
							}
						} else {
							// Если дозировок нет в protobuf, берем из модели пиццы
							if pizza, exists := models.GetPizza(pbItem.PizzaName); exists && pizza.IngredientAmounts != nil {
								item.IngredientAmounts = pizza.IngredientAmounts
							}
						}
						order.Items = append(order.Items, item)
					}
				} else {
					// Fallback на JSON
					if err := json.Unmarshal(msg.Value, &order); err != nil {
						// Не логируем каждую ошибку парсинга, чтобы не спамить
						continue
					}
				}
				
				// 1. Сохраняем заказ в Redis (для быстрого доступа)
				if kc.redisUtil != nil {
					orderJSON, _ := json.Marshal(order)
					orderKey := fmt.Sprintf("erp:order:%s", order.ID)
					err := kc.redisUtil.SetBytes(orderKey, orderJSON, 24*time.Hour)
					if err != nil {
						log.Printf("⚠️ Ошибка сохранения заказа %s в Redis: %v", order.ID, err)
					}
					
					// 2. Проверяем VisibleAt перед добавлением в активные
					// Если VisibleAt не заполнен в Kafka сообщении, проверяем Redis (заказ мог быть создан ранее)
					if order.VisibleAt.IsZero() {
						// Пробуем получить VisibleAt из Redis
						visibleAtKey := fmt.Sprintf("order:visible_at:%s", order.ID)
						if visibleAtStr, err := kc.redisUtil.Get(visibleAtKey); err == nil && visibleAtStr != "" {
							if visibleAt, err := time.Parse(time.RFC3339, visibleAtStr); err == nil {
								order.VisibleAt = visibleAt
							}
						}
						
						// Если все еще нет VisibleAt, но есть TargetSlotID, пробуем получить время начала слота
						if order.VisibleAt.IsZero() && order.TargetSlotID != "" {
							slotStartKey := fmt.Sprintf("order:slot:start:%s", order.ID)
							if slotStartStr, err := kc.redisUtil.Get(slotStartKey); err == nil && slotStartStr != "" {
								if slotStartTime, err := time.Parse(time.RFC3339, slotStartStr); err == nil {
									// Вычисляем VisibleAt как время начала слота минус 15 минут
									order.VisibleAt = slotStartTime.Add(-15 * time.Minute)
									// Сохраняем вычисленное VisibleAt
									kc.redisUtil.Set(visibleAtKey, order.VisibleAt.Format(time.RFC3339), 24*time.Hour)
								}
							}
						}
					}
					
					// НЕ добавляем заказ в активные сразу - он появится только когда наступит VisibleAt
					if !order.VisibleAt.IsZero() {
						// Сохраняем время показа для проверки (если еще не сохранено)
						visibleAtKey := fmt.Sprintf("order:visible_at:%s", order.ID)
						kc.redisUtil.Set(visibleAtKey, order.VisibleAt.Format(time.RFC3339), 24*time.Hour)
						
						// Если есть TargetSlotStartTime, сохраняем его тоже
						if !order.TargetSlotStartTime.IsZero() {
							kc.redisUtil.Set(fmt.Sprintf("order:slot:start:%s", order.ID), order.TargetSlotStartTime.Format(time.RFC3339), 24*time.Hour)
						}
						
						// Проверяем, не находится ли заказ уже в active (защита от дублирования)
						isActive, _ := kc.redisUtil.SIsMember("erp:orders:active", order.ID)
						if isActive {
							// Заказ уже в active - удаляем его оттуда и добавляем в pending
							kc.redisUtil.SRem("erp:orders:active", order.ID)
							log.Printf("🔄 Заказ %s перемещен из active в pending_slots (будет показан: %s UTC)", 
								order.ID, order.VisibleAt.Format("15:04:05"))
						}
						
						// Добавляем в список ожидающих заказов (не в активные!)
						err = kc.redisUtil.SAdd("erp:orders:pending_slots", order.ID)
						if err != nil {
							log.Printf("⚠️ Ошибка добавления заказа %s в pending_slots: %v", order.ID, err)
						} else {
							log.Printf("📅 Заказ %s добавлен в erp:orders:pending_slots (будет показан: %s UTC)", 
								order.ID, order.VisibleAt.Format("15:04:05"))
						}
					} else {
						// Если нет VisibleAt, добавляем сразу в активные (старая логика для обратной совместимости)
						// Но сначала проверяем, не находится ли заказ уже в pending_slots
						isPending, _ := kc.redisUtil.SIsMember("erp:orders:pending_slots", order.ID)
						if isPending {
							// Заказ уже в pending - не добавляем в active
							log.Printf("ℹ️ Заказ %s уже в pending_slots, пропускаем добавление в active", order.ID)
						} else {
							err = kc.redisUtil.SAdd("erp:orders:active", order.ID)
							if err != nil {
								log.Printf("⚠️ Ошибка добавления заказа %s в активные: %v", order.ID, err)
							} else {
								log.Printf("✅ Заказ %s добавлен в erp:orders:active", order.ID)
							}
						}
					}
					
					// 3. Инкремент счетчиков для статистики
					kc.redisUtil.Increment("erp:orders:total")
					kc.redisUtil.Increment("erp:orders:pending")
					
					// НЕ добавляем в очередь воркеров - обработка только вручную через ERP
				}
				
				// 4. Отправляем заказ в WebSocket
				// Отправляем на планшеты поваров
				orderJSON, err := json.Marshal(order)
				if err != nil {
					continue
				}
				GlobalHub.BroadcastMessage(orderJSON)
				
				// Отправляем в ERP систему для real-time обновлений
				BroadcastERPUpdate("new_order", map[string]interface{}{
					"order_id": order.ID,
					"display_id": order.DisplayID,
					"message": "Новый заказ получен",
				})
				
				// Логируем только раз в 5 секунд для прогресса
				processed := atomic.AddInt64(&kc.processed, 1)
				now := time.Now().Unix()
				if now-kc.lastLog >= 5 {
					atomic.StoreInt64(&kc.lastLog, now)
					log.Printf("📊 Kafka WS Consumer: обработано %d заказов", processed)
				}
			}
		}
	}()
}

// Stop останавливает Kafka Consumer
func (kc *KafkaWSConsumer) Stop() {
	kc.cancel()
	if kc.reader != nil {
		kc.reader.Close()
	}
	log.Println("🛑 Kafka WS Consumer остановлен")
}

