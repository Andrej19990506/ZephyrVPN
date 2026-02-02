package api

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/segmentio/kafka-go"
	"google.golang.org/protobuf/proto"
	"zephyrvpn/server/internal/models"
	"zephyrvpn/server/internal/pb" // Наш сгенерированный код
	"zephyrvpn/server/internal/services"
	"zephyrvpn/server/internal/utils"
)

type OrderGRPCServer struct {
	pb.UnimplementedOrderServiceServer
	redisUtil     *utils.RedisClient
	slotService   *services.SlotService
	kafkaWriter   *kafka.Writer
	kafkaSentCount int64 // Счетчик отправленных сообщений
}

func NewOrderGRPCServer(redisUtil *utils.RedisClient, kafkaBrokers string, openHour, closeHour, closeMin int) *OrderGRPCServer {
	var kafkaWriter *kafka.Writer
	if kafkaBrokers != "" {
		// Создаем Kafka writer для отправки Protobuf сообщений
		brokers := strings.Split(kafkaBrokers, ",")
		kafkaWriter = &kafka.Writer{
			Addr:     kafka.TCP(brokers...),
			Topic:    "pizza-orders", // Топик для заказов (бинарный Protobuf)
			Balancer: &kafka.LeastBytes{}, // Балансировка по наименьшему количеству байт
			Async:    true, // Асинхронная отправка для максимальной скорости
		}
		log.Printf("✅ Kafka producer подключен к %s", kafkaBrokers)
	}

	slotService := services.NewSlotService(redisUtil, openHour, closeHour, closeMin)
	
	return &OrderGRPCServer{
		redisUtil:   redisUtil,
		slotService: slotService,
		kafkaWriter: kafkaWriter,
	}
}

// Close закрывает Kafka writer
func (s *OrderGRPCServer) Close() error {
	if s.kafkaWriter != nil {
		return s.kafkaWriter.Close()
	}
	return nil
}

func (s *OrderGRPCServer) CreateOrder(ctx context.Context, req *pb.PizzaOrderRequest) (*pb.OrderResponse, error) {
	// 1. Конвертируем gRPC запрос в Protobuf заказ (БЕЗ JSON Marshal - это ключевая оптимизация!)
	fullID := uuid.New().String()
	// Извлекаем только цифры из UUID и берем последние 4
	re := regexp.MustCompile(`\d+`)
	digits := re.FindAllString(fullID, -1)
	digitsOnly := strings.Join(digits, "")
	if len(digitsOnly) < 4 {
		digitsOnly = "0000" // Fallback если цифр мало
	}
	displayID := digitsOnly[len(digitsOnly)-4:] // Последние 4 цифры
	now := time.Now()

	// Конвертируем Items из запроса
	var pbItems []*pb.PizzaItem
	totalPrice := int64(0)
	isSet := false
	setName := ""
	
	// 1. Проверяем, не заказал ли юзер СЕТ
	if set, ok := models.GetSet(req.PizzaName); ok {
		isSet = true
		setName = set.Name
		totalPrice = int64(set.Price)
		
		// Разбираем сет на отдельные пиццы для поваров
		for _, pName := range set.Pizzas {
			if recipe, ok := models.GetPizza(pName); ok {
				// Конвертируем дозировки ингредиентов из модели пиццы
				var ingredientAmounts map[string]int32
				if recipe.IngredientAmounts != nil {
					ingredientAmounts = make(map[string]int32)
					for k, v := range recipe.IngredientAmounts {
						ingredientAmounts[k] = int32(v)
					}
				}
				
				pbItems = append(pbItems, &pb.PizzaItem{
					PizzaName:         recipe.Name,
					Ingredients:       recipe.Ingredients,
					IngredientAmounts: ingredientAmounts,
					Quantity:          1, // В сете обычно по одной
					Price:             0, // Цена уже в TotalPrice сета
					SetName:           set.Name,
					IsSetItem:         true, // ВОТ ЭТОТ ФЛАГ РЕШАЕТ!
				})
			}
		}
	} else if req.PizzaName != "" {
		// 2. Если это просто одиночная пицца
		// Вычисляем цену из меню
		itemPrice := int64(500) // Базовая цена по умолчанию
		if pizza, exists := models.GetPizza(req.PizzaName); exists {
			itemPrice = int64(pizza.Price)
		}
		
		// Добавляем стоимость допов
		for _, extraName := range req.Extras {
			if extra, exists := models.GetExtra(extraName); exists {
				itemPrice += int64(extra.Price)
			}
		}
		
		// Умножаем на количество
		quantity := int32(1)
		if req.Quantity > 0 {
			quantity = req.Quantity
		}
		itemPrice = itemPrice * int64(quantity)
		totalPrice = itemPrice
		
		// Получаем ингредиенты из меню или из запроса
		ingredients := req.Ingredients
		if len(ingredients) == 0 {
			if pizza, exists := models.GetPizza(req.PizzaName); exists {
				ingredients = pizza.Ingredients
			}
		}
		
		// Конвертируем int64 в int32 для protobuf
		itemPriceInt32 := int32(itemPrice)
		if itemPrice > int64(^uint32(0)>>1) { // Проверка на переполнение
			itemPriceInt32 = int32(^uint32(0) >> 1) // Максимальное значение int32
		}
		
		// Берем дозировки ингредиентов из модели пиццы
		var ingredientAmounts map[string]int32
		if pizza, exists := models.GetPizza(req.PizzaName); exists && pizza.IngredientAmounts != nil {
			// Конвертируем map[string]int в map[string]int32 для protobuf
			ingredientAmounts = make(map[string]int32)
			for k, v := range pizza.IngredientAmounts {
				ingredientAmounts[k] = int32(v)
			}
		}
		
		pbItems = append(pbItems, &pb.PizzaItem{
			PizzaName:         req.PizzaName,
			Quantity:         quantity,
			Price:            itemPriceInt32,
			Ingredients:      ingredients,
			IngredientAmounts: ingredientAmounts,
			Extras:           req.Extras,
			IsSetItem:        false,
		})
	}

	// Конвертируем totalPrice в int32 для protobuf
	totalPriceInt32 := int32(totalPrice)
	if totalPrice > int64(^uint32(0)>>1) { // Проверка на переполнение
		totalPriceInt32 = int32(^uint32(0) >> 1) // Максимальное значение int32
	}

	// 🎯 Capacity-Based Slot Scheduling: назначаем слот ПЕРЕД созданием заказа
	// Считаем общее количество элементов (пицц) в заказе
	itemsCount := 0
	for _, item := range pbItems {
		itemsCount += int(item.Quantity)
	}
	
	slotID, slotStartTime, visibleAt, err := s.slotService.AssignSlot(fullID, int(totalPrice), itemsCount)
	if err != nil {
		// Если не удалось назначить слот, возвращаем ошибку
		log.Printf("❌ OrderGRPCServer: не удалось назначить слот для заказа %s: %v", fullID, err)
		return nil, fmt.Errorf("не удалось назначить временной слот для заказа: %w", err)
	}

	// Создаем Protobuf заказ напрямую
	pbOrder := &pb.PizzaOrder{
		Id:               fullID,
		DisplayId:        displayID,
		CustomerId:       req.CustomerId,
		CreatedAt:       now.UnixNano(),
		Status:           "pending",
		TotalPrice:       totalPriceInt32,
		Items:            pbItems, // ✅ Добавляем Items!
		IsSet:            isSet,
		SetName:          setName,
		TargetSlotId:     slotID,                    // 🎯 Сохраняем ID слота в заказе
		VisibleAt:        visibleAt.Format(time.RFC3339), // 🎯 Сохраняем время показа заказа
		CustomerFirstName: req.CustomerFirstName,     // Данные клиента
		CustomerLastName:  req.CustomerLastName,
		CustomerPhone:     req.CustomerPhone,
		DeliveryAddress:  req.DeliveryAddress,
		IsPickup:         req.IsPickup,
		PickupLocationId: req.PickupLocationId,
	}

	// 2. Сериализуем в Protobuf (быстрее JSON в 2-3 раза!)
	orderBytes, err := proto.Marshal(pbOrder)
	if err != nil {
		log.Printf("⚠️ Protobuf Marshal error: %v", err)
		return nil, err
	}

	// 3. Пуляем в Redis через Pipeline (БЕЗ JSON Marshal - экономия CPU!)
	pipe := s.redisUtil.Pipeline()
	redisCtx := s.redisUtil.Context()
	
	// Накидываем команды в пачку (они еще не ушли в сеть!)
	// Используем SetBytes для бинарных данных Protobuf
	pipe.Set(redisCtx, fmt.Sprintf("erp:order:%s", fullID), orderBytes, 24*time.Hour)
	// Сохраняем время начала слота отдельно (для фильтрации заказов по времени)
	if !slotStartTime.IsZero() {
		pipe.Set(redisCtx, fmt.Sprintf("order:slot:start:%s", fullID), slotStartTime.Format(time.RFC3339), 24*time.Hour)
	}
	pipe.LPush(redisCtx, "kitchen:orders:queue", fullID)
	pipe.Incr(redisCtx, "orders:total")
	pipe.Incr(redisCtx, "erp:orders:pending")
	
	// Отправляем ВСЁ ОДНИМ выстрелом (экономия сетевых вызовов!)
	_, err = pipe.Exec(redisCtx)
	if err != nil {
		log.Printf("⚠️ Pipeline error при создании заказа через gRPC %s: %v", fullID, err)
	}
	
	// НЕ добавляем заказ в активные сразу - он появится только когда наступит VisibleAt
	// Сохраняем заказ в отдельный список ожидающих заказов
	if !visibleAt.IsZero() {
		// Сохраняем время начала слота и время показа для проверки
		s.redisUtil.Set(fmt.Sprintf("order:slot:start:%s", fullID), slotStartTime.Format(time.RFC3339), 24*time.Hour)
		s.redisUtil.Set(fmt.Sprintf("order:visible_at:%s", fullID), visibleAt.Format(time.RFC3339), 24*time.Hour)
		
		// Добавляем в список ожидающих заказов (не в активные!)
		s.redisUtil.SAdd("erp:orders:pending_slots", fullID)
		
		log.Printf("📅 Заказ %s назначен на слот %s (время начала: %s UTC, будет показан: %s UTC)", 
			fullID, slotID, slotStartTime.Format("15:04:05"), visibleAt.Format("15:04:05"))
	}

	// 4. Отправляем бинарный Protobuf в Kafka (асинхронно, не блокируем ответ!)
	if s.kafkaWriter != nil {
		go func() {
			// Используем background context с таймаутом для асинхронной отправки
			// (не ctx из запроса, он может быть отменен!)
			bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			
			// Отправляем бинарные Protobuf данные в Kafka
			err := s.kafkaWriter.WriteMessages(bgCtx, kafka.Message{
				Key:   []byte(fullID), // Ключ = ID заказа
				Value: orderBytes,     // Бинарный Protobuf (БЕЗ JSON!)
			})
			if err != nil {
				// Игнорируем ошибку "Unknown Topic Or Partition" - топик создастся автоматически
				errStr := err.Error()
				if !strings.Contains(errStr, "Unknown Topic Or Partition") && 
				   !strings.Contains(errStr, "context canceled") {
					log.Printf("⚠️ Kafka error при отправке заказа %s: %v", fullID, err)
				}
			} else {
				// Логируем успешную отправку (только первые 10 для проверки)
				atomic.AddInt64(&s.kafkaSentCount, 1)
				if atomic.LoadInt64(&s.kafkaSentCount) <= 10 {
					log.Printf("✅ Kafka: отправлен заказ %s (%d байт Protobuf)", fullID, len(orderBytes))
				}
			}
		}()
	}

	// 5. Отвечаем мгновенно (не ждем Kafka!)
	return &pb.OrderResponse{
		OrderId:   fullID,
		DisplayId: displayID,
		Status:    "accepted_via_grpc",
	}, nil
}
