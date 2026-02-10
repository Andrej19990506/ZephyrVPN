# Архитектура Backend системы ERP для пиццерии

## 📋 Оглавление

1. [Обзор системы](#обзор-системы)
2. [Технологический стек](#технологический-стек)
3. [Архитектурные компоненты](#архитектурные-компоненты)
4. [Потоки данных](#потоки-данных)
5. [Базы данных](#базы-данных)
6. [API и протоколы](#api-и-протоколы)
7. [Сервисы и бизнес-логика](#сервисы-и-бизнес-логика)
8. [Интеграции](#интеграции)
9. [Real-time коммуникация](#real-time-коммуникация)
10. [Безопасность и масштабирование](#безопасность-и-масштабирование)

---

## 🎯 Обзор системы

Система представляет собой высоконагруженный ERP-бэкенд для управления пиццерией, построенный на Go. Основные функции:

- **Управление заказами**: Создание, обработка, отслеживание заказов через gRPC и REST API
- **Управление складом**: Учет остатков, сроков годности, движений товаров
- **Управление рецептами**: Технологические карты, версионирование, обучение персонала
- **Финансовый учет**: Контрагенты, транзакции, счета
- **Управление персоналом**: Роли, станции, рабочие смены
- **Real-time мониторинг**: WebSocket для планшетов поваров и ERP-панели

### Ключевые характеристики

- **HighLoad**: Оптимизирован для обработки 500+ одновременных запросов
- **Event-Driven**: Использует Kafka для асинхронной обработки заказов
- **Real-time**: WebSocket для мгновенных обновлений
- **Resilient**: Восстановление состояния после перезапуска через BootstrapState
- **Scalable**: Горизонтальное масштабирование через Kafka Consumer Groups

---

## 🛠 Технологический стек

### Backend
- **Язык**: Go 1.21+
- **Web Framework**: Gin (REST API)
- **gRPC**: Protocol Buffers для высокопроизводительной коммуникации
- **ORM**: GORM для работы с PostgreSQL
- **Message Queue**: Apache Kafka (Confluent Platform 7.5.0)
- **Cache/Pub-Sub**: Redis 7

### Базы данных
- **PostgreSQL 15**: Основное хранилище данных
- **Redis 7**: Кэширование, pub/sub, временные данные

### Инфраструктура
- **Docker Compose**: Локальная разработка
- **Railway**: Production deployment
- **Zookeeper**: Координация Kafka кластера

### Протоколы
- **REST API**: HTTP/JSON для веб-интерфейсов
- **gRPC**: Бинарный протокол для мобильных приложений
- **WebSocket**: Real-time обновления
- **Kafka Protocol**: Асинхронная обработка событий

---

## 🏗 Архитектурные компоненты

### Структура проекта

```
server/
├── main.go                    # Точка входа, инициализация всех сервисов
├── docker-compose.yml         # Инфраструктура (PostgreSQL, Redis, Kafka)
├── internal/
│   ├── api/                   # HTTP/gRPC контроллеры
│   │   ├── router.go          # Маршрутизация (устарело, настройка в main.go)
│   │   ├── order_controller.go
│   │   ├── erp_controller.go
│   │   ├── grpc_order_server.go  # gRPC сервер для заказов
│   │   ├── kafka_ws_consumer.go  # Kafka Consumer → WebSocket
│   │   ├── ws_hub.go          # WebSocket Hub для планшетов и ERP
│   │   └── ...                # Другие контроллеры
│   ├── services/              # Бизнес-логика
│   │   ├── order_service.go   # Управление заказами
│   │   ├── slot_service.go    # Capacity-Based Slot Scheduling
│   │   ├── menu_service.go    # Управление меню
│   │   ├── stock_service.go   # Управление складом
│   │   ├── recipe_service.go  # Управление рецептами
│   │   └── ...                # Другие сервисы
│   ├── models/                # Модели данных
│   │   ├── pizza.go           # PizzaOrder, PizzaItem
│   │   ├── nomenclature.go    # Товары, категории
│   │   ├── stock.go           # Остатки, движения
│   │   └── ...                # Другие модели
│   ├── database/              # Подключения к БД
│   │   ├── postgres.go        # PostgreSQL connection pool
│   │   └── redis.go           # Redis client
│   ├── config/                # Конфигурация
│   │   └── config.go          # Загрузка переменных окружения
│   ├── utils/                 # Утилиты
│   │   └── redis.go           # Redis helper functions
│   └── pb/                    # Сгенерированные Protobuf файлы
│       └── order_grpc.pb.go
└── migrations/                # SQL миграции
    └── 001_init_menu.sql
    └── ...
```

### Компоненты системы

```
┌─────────────────────────────────────────────────────────────┐
│                      Client Applications                     │
│  (Mobile App, Web Frontend, Wails Desktop App)              │
└──────────────┬──────────────────────┬────────────────────────┘
               │                      │
               │ gRPC                 │ REST API
               │ (Port 50051)         │ (Port 8080)
               │                      │
┌──────────────▼──────────────────────▼────────────────────────┐
│                    API Layer (Gin + gRPC)                    │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐      │
│  │ Order        │  │ ERP          │  │ Kitchen      │      │
│  │ Controller   │  │ Controller   │  │ Controller   │      │
│  └──────────────┘  └──────────────┘  └──────────────┘      │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐      │
│  │ Stock        │  │ Recipe       │  │ Staff        │      │
│  │ Controller   │  │ Controller   │  │ Controller   │      │
│  └──────────────┘  └──────────────┘  └──────────────┘      │
└──────────────┬──────────────────────┬────────────────────────┘
               │                      │
               │                      │
┌──────────────▼──────────────────────▼────────────────────────┐
│                    Service Layer                             │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐      │
│  │ Order        │  │ Slot        │  │ Menu         │      │
│  │ Service      │  │ Service     │  │ Service      │      │
│  └──────────────┘  └──────────────┘  └──────────────┘      │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐      │
│  │ Stock        │  │ Recipe       │  │ Nomenclature │      │
│  │ Service      │  │ Service      │  │ Service      │      │
│  └──────────────┘  └──────────────┘  └──────────────┘      │
└──────────────┬──────────────────────┬────────────────────────┘
               │                      │
               │                      │
┌──────────────▼──────────────────────▼────────────────────────┐
│              Data Layer                                      │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐      │
│  │ PostgreSQL   │  │ Redis        │  │ Kafka        │      │
│  │ (Persistent) │  │ (Cache/Pub) │  │ (Events)     │      │
│  └──────────────┘  └──────────────┘  └──────────────┘      │
└─────────────────────────────────────────────────────────────┘
```

---

## 🔄 Потоки данных

### 1. Создание заказа (gRPC → Kafka → WebSocket)

```
Mobile App (gRPC)
    │
    ├─► OrderGRPCServer.CreateOrder()
    │   ├─► Валидация заказа
    │   ├─► SlotService.AssignSlot() (Capacity-Based Scheduling)
    │   ├─► Конвертация в Protobuf
    │   └─► Kafka Producer → topic: "pizza-orders"
    │
    └─► Response (OrderID, DisplayID, SlotTime)

Kafka Topic: "pizza-orders"
    │
    ├─► KafkaWSConsumer (Consumer Group: "order-service-stable-group")
    │   ├─► Чтение Protobuf сообщения
    │   ├─► OrderService.SaveOrder() → PostgreSQL
    │   ├─► Redis: SET order:{id} (для быстрого доступа)
    │   └─► WebSocket Hub.BroadcastMessage() → Планшеты поваров
    │
    └─► ERPHub.BroadcastMessage() → ERP Dashboard
```

### 2. Восстановление состояния (BootstrapState)

```
Server Startup
    │
    ├─► OrderService.BootstrapState()
    │   ├─► PostgreSQL: SELECT * FROM orders WHERE status IN (...)
    │   ├─► Redis: SET order:{id} (восстановление активных заказов)
    │   └─► Kafka Consumer: StartOffset = LastOffset (не обрабатывать старые)
    │
    └─► Система готова к работе
```

### 3. Обновление меню (Database → Redis Pub/Sub)

```
Admin Panel → POST /api/v1/admin/update-menu
    │
    ├─► MenuService.LoadMenu()
    │   ├─► PostgreSQL: SELECT * FROM menu_items
    │   ├─► Redis: SET menu:full (кэш меню)
    │   └─► Redis Pub/Sub: PUBLISH menu:updated
    │
    └─► Все сервисы получают уведомление через Pub/Sub
```

### 4. Обработка заказа на кухне

```
Kitchen Tablet (WebSocket)
    │
    ├─► Подключение к /api/v1/ws
    │   └─► GlobalHub.AddClient()
    │
    ├─► Получение заказа через WebSocket
    │   └─► Отображение на планшете
    │
    ├─► Обновление статуса (preparing → cooking → ready)
    │   ├─► Redis: SET order:{id}:status
    │   ├─► PostgreSQL: UPDATE orders SET status = ...
    │   └─► WebSocket: Broadcast обновление статуса
    │
    └─► ERP Dashboard получает обновление через ERPHub
```

---

## 💾 Базы данных

### PostgreSQL 15

**Назначение**: Основное хранилище данных (persistent storage)

**Таблицы**:

- `orders` (partitioned by `created_at`) - Заказы
- `menu_items` - Меню (пиццы, наборы, дополнения)
- `nomenclature` - Номенклатура товаров
- `stock_items` - Остатки на складе
- `stock_movements` - Движения товаров (аудит)
- `recipes` - Рецепты (технологические карты)
- `recipe_ingredients` - Ингредиенты рецептов
- `recipe_versions` - Версии рецептов
- `staff` - Персонал
- `stations` - Станции кухни
- `branches` - Филиалы
- `counterparties` - Контрагенты
- `finance_transactions` - Финансовые транзакции
- `legal_entities` - Юридические лица
- `technologist_*` - Таблицы для технолога (training materials, exams)

**Оптимизации**:
- Connection Pool: MaxOpenConns=25, MaxIdleConns=10
- Индексы на `(status, created_at)` для быстрого поиска активных заказов
- Партиционирование таблицы `orders` по `created_at` для производительности

### Redis 7

**Назначение**: Кэширование, pub/sub, временные данные

**Использование**:

1. **Кэширование меню**:
   - `menu:full` - Полное меню в JSON
   - `menu:updated` - Pub/Sub канал для уведомлений

2. **Заказы (оперативные данные)**:
   - `order:{id}` - Заказ в JSON (для быстрого доступа)
   - `order:{id}:status` - Статус заказа
   - TTL: 24 часа

3. **Capacity-Based Slot Scheduling**:
   - `slot:{slot_id}` - Информация о слоте (capacity, load)
   - `slot:config` - Конфигурация слотов (maxCapacity, duration)
   - Lua scripts для атомарных операций

4. **Pub/Sub каналы**:
   - `menu:updated` - Обновление меню
   - `order:created` - Новый заказ
   - `order:status:changed` - Изменение статуса

**Настройки**:
- PoolSize: 1000 соединений
- MinIdleConns: 50
- MaxMemory: 4GB
- Eviction Policy: allkeys-lru

### Kafka

**Назначение**: Асинхронная обработка заказов, event streaming

**Топики**:
- `pizza-orders` - Заказы в Protobuf формате

**Consumer Groups**:
- `order-service-stable-group` - Стабильная группа для обработки заказов

**Настройки**:
- Retention: 24 часа (86400000 ms)
- Max Size: 5GB (5368709120 bytes)
- Replication Factor: 1 (для разработки)
- Auto Create Topics: true

**Producer**:
- Async: true (асинхронная отправка)
- Balancer: LeastBytes (балансировка по размеру)

**Consumer**:
- StartOffset: FirstOffset (при первом запуске) / LastOffset (после bootstrap)
- CommitInterval: 5 секунд (автоматический commit offset)
- MinBytes: 10KB (батчинг для производительности)
- MaxBytes: 10MB

---

## 🌐 API и протоколы

### REST API (Gin Framework)

**Base URL**: `http://localhost:8080/api/v1`

#### Основные эндпоинты:

**Заказы**:
- `POST /order` - Создать заказ (для магазина)
- `POST /erp/orders` - Создать заказ (для ERP)
- `GET /erp/orders` - Получить активные заказы
- `GET /erp/orders/pending` - Получить отложенные заказы
- `GET /erp/orders/batch` - Получить заказы батчами (по 50)
- `GET /erp/orders/:id` - Получить заказ по ID
- `POST /erp/orders/:id/processed` - Отметить заказ как обработанный

**Меню**:
- `GET /menu` - Получить полное меню
- `GET /menu/pizzas` - Получить пиццы
- `GET /menu/extras` - Получить дополнения
- `GET /menu/sets` - Получить наборы
- `POST /admin/update-menu` - Обновить меню из БД

**Слоты**:
- `GET /erp/slots` - Получить все слоты
- `GET /erp/slots/config` - Получить конфигурацию слотов
- `PUT /erp/slots/config` - Обновить конфигурацию слотов

**Склад**:
- `GET /inventory/stock` - Получить остатки
- `GET /inventory/stock/at-risk` - Рискованные товары
- `GET /inventory/stock/expiry-alerts` - Уведомления о сроке годности
- `POST /inventory/stock/process-sale` - Списание при продаже
- `POST /inventory/stock/commit-production` - Производство полуфабриката

**Рецепты**:
- `GET /recipes` - Список рецептов
- `GET /recipes/:id` - Получить рецепт
- `POST /recipes` - Создать рецепт
- `PUT /recipes/:id` - Обновить рецепт
- `DELETE /recipes/:id` - Удалить рецепт

**Технолог**:
- `GET /technologist/dashboard` - Production Dashboard
- `GET /technologist/recipes/:id/versions` - Версии рецепта
- `POST /technologist/unified-create` - Unified create (Nomenclature + Recipe)

**Персонал**:
- `GET /erp/staff` - Список сотрудников
- `POST /erp/staff` - Создать сотрудника
- `PUT /erp/staff/:id` - Обновить сотрудника
- `PUT /erp/staff/:id/status` - Обновить статус (State Machine)

**Станции**:
- `GET /erp/stations` - Список станций
- `POST /erp/stations` - Создать станцию
- `PUT /erp/stations/:id` - Обновить станцию

**Кухня**:
- `GET /kitchen/workers` - Статистика воркеров
- `POST /kitchen/workers` - Установить количество воркеров
- `POST /kitchen/workers/add` - Добавить воркера
- `DELETE /kitchen/workers/:id` - Удалить воркера

### gRPC API

**Port**: 50051

**Service**: `OrderService`

**Methods**:
- `CreateOrder(PizzaOrderRequest) → OrderResponse` - Создать заказ

**Protobuf Schema**: `internal/proto/order.proto`

**Особенности**:
- Бинарный формат (высокая производительность)
- Асинхронная отправка в Kafka
- Capacity-Based Slot Scheduling

### WebSocket

**Endpoints**:
- `/api/v1/ws` - WebSocket для планшетов поваров (GlobalHub)
- `/api/v1/erp/ws` - WebSocket для ERP Dashboard (ERPHub)

**Протокол**: TextMessage (JSON)

**Сообщения**:
- `order:created` - Новый заказ
- `order:status:changed` - Изменение статуса заказа
- `menu:updated` - Обновление меню

---

## 🔧 Сервисы и бизнес-логика

### OrderService

**Назначение**: Управление жизненным циклом заказов

**Основные функции**:
- `BootstrapState()` - Восстановление активных заказов из PostgreSQL в Redis при старте
- `SaveOrder()` - Сохранение заказа в PostgreSQL
- `ArchiveOldOrders()` - Архивация старых заказов (фоновый процесс)

**Состояния заказа**:
- `pending` - Ожидает обработки
- `preparing` - Готовится
- `cooking` - Готовится на кухне
- `ready` - Готов
- `delivery` - Доставляется
- `delivered` - Доставлен
- `cancelled` - Отменен
- `archived` - Архивирован

### SlotService

**Назначение**: Capacity-Based Slot Scheduling (распределение заказов по временным слотам)

**Алгоритм**:
1. Разделение рабочего дня на слоты (по умолчанию 15 минут)
2. Каждый слот имеет максимальную емкость в рублях (не количество заказов!)
3. При создании заказа выбирается ближайший доступный слот
4. Использование Redis Lua scripts для атомарных операций

**Конфигурация**:
- `slotDuration`: 15 минут (настраивается)
- `maxCapacityPerSlot`: 10000 рублей (настраивается через API)
- `openHour/openMin`: Время открытия (UTC)
- `closeHour/closeMin`: Время закрытия (UTC)

**Методы**:
- `AssignSlot(orderPrice, preferredTime) → (slotID, startTime, error)` - Назначить слот
- `GetSlots(startTime, endTime) → []SlotInfo` - Получить слоты
- `UpdateSlotConfig(config) → error` - Обновить конфигурацию

### MenuService

**Назначение**: Управление меню с поддержкой hot-reload

**Особенности**:
- Загрузка меню из PostgreSQL при старте
- Кэширование в Redis (`menu:full`)
- Pub/Sub для уведомлений об обновлениях
- Fallback таймер для автообновления (каждые 5 минут)

**Методы**:
- `LoadMenu() → error` - Загрузить меню из БД
- `StartAutoReload()` - Запустить автообновление
- `GetMenu() → Menu` - Получить меню из кэша

### StockService

**Назначение**: Управление складом, остатками, сроками годности

**Функции**:
- Учет остатков товаров
- Отслеживание сроков годности
- Автоматическое создание алертов
- Списание при продаже
- Производство полуфабрикатов
- Расчет себестоимости рецептов

**Фоновые процессы**:
- Проверка сроков годности (каждые 5 минут)

### RecipeService

**Назначение**: Управление технологическими картами (рецептами)

**Функции**:
- Создание/обновление рецептов
- Версионирование рецептов
- Иерархическая структура (папки)
- Расчет себестоимости
- Валидация ингредиентов

**Интеграция**:
- Связан с StockService для расчета себестоимости
- Инвалидация кэша меню при обновлении рецептов

### TechnologistService

**Назначение**: Рабочее пространство технолога

**Функции**:
- Production Dashboard (аналитика рецептов)
- Версионирование рецептов
- Training Materials (обучающие материалы)
- Recipe Exams (экзамены по рецептам)
- Unified Create (создание номенклатуры + рецепта)

### NomenclatureService

**Назначение**: Управление номенклатурой товаров

**Функции**:
- CRUD операции с товарами
- Категории товаров
- Импорт из файлов (CSV, Excel)
- Автоматическая генерация SKU через PLU Service

### PLUService

**Назначение**: Управление PLU кодами (Price Look-Up)

**Функции**:
- Стандартные PLU коды
- Генерация SKU на основе PLU
- Интеграция с NomenclatureService

---

## 🔌 Интеграции

### Kafka Integration

**Producer** (OrderGRPCServer):
- Асинхронная отправка заказов в топик `pizza-orders`
- Формат: Protobuf (бинарный)
- Balancer: LeastBytes

**Consumer** (KafkaWSConsumer):
- Consumer Group: `order-service-stable-group`
- Автоматический commit offset каждые 5 секунд
- StartOffset: FirstOffset (первый запуск) / LastOffset (после bootstrap)
- Батчинг: MinBytes=10KB, MaxBytes=10MB

**Обработка сообщений**:
1. Чтение Protobuf сообщения из Kafka
2. Сохранение в PostgreSQL через OrderService
3. Сохранение в Redis для быстрого доступа
4. Broadcast через WebSocket Hub

### Redis Integration

**Использование**:
1. **Кэширование**: Меню, заказы, конфигурация
2. **Pub/Sub**: Уведомления об обновлениях
3. **Временные данные**: Слоты, сессии
4. **Атомарные операции**: Lua scripts для Capacity-Based Scheduling

**Connection Pool**:
- PoolSize: 1000
- MinIdleConns: 50
- MaxRetries: 3

### PostgreSQL Integration

**ORM**: GORM

**Connection Pool**:
- MaxOpenConns: 25
- MaxIdleConns: 10
- ConnMaxLifetime: 5 минут
- ConnMaxIdleTime: 1 минута

**Миграции**: SQL файлы в `migrations/`

**Партиционирование**: Таблица `orders` партиционирована по `created_at`

---

## 📡 Real-time коммуникация

### WebSocket Hubs

**GlobalHub** (для планшетов поваров):
- Endpoint: `/api/v1/ws`
- Broadcast заказов на планшеты
- Управление статусами заказов

**ERPHub** (для ERP Dashboard):
- Endpoint: `/api/v1/erp/ws`
- Broadcast обновлений для ERP панели
- Мониторинг заказов в реальном времени

**Архитектура**:
```
Hub
├── clients: map[*websocket.Conn]bool
├── broadcast: chan []byte (буферизованный, 256)
└── mutex: sync.RWMutex (потокобезопасность)
```

**Методы**:
- `AddClient(conn)` - Добавить клиента
- `RemoveClient(conn)` - Удалить клиента
- `BroadcastMessage(message)` - Отправить сообщение всем
- `GetClientsCount()` - Количество подключенных клиентов

### Kafka → WebSocket Pipeline

```
Kafka Topic: "pizza-orders"
    │
    ├─► KafkaWSConsumer
    │   ├─► Чтение Protobuf сообщения
    │   ├─► OrderService.SaveOrder() → PostgreSQL
    │   ├─► Redis: SET order:{id}
    │   └─► GlobalHub.BroadcastMessage() → Планшеты
    │
    └─► ERPHub.BroadcastMessage() → ERP Dashboard
```

---

## 🔒 Безопасность и масштабирование

### Безопасность

**Аутентификация**:
- JWT токены для Super Admin
- Endpoint: `/api/v1/auth/super-admin/login`

**CORS**:
- Разрешен для всех источников (`*`)
- Методы: GET, POST, PUT, DELETE, OPTIONS

**Kafka Security** (опционально):
- SASL/PLAIN аутентификация
- TLS шифрование
- Настройка через переменные окружения: `KAFKA_USERNAME`, `KAFKA_PASSWORD`, `KAFKA_CA_CERT`

### Масштабирование

**Горизонтальное масштабирование**:
- Kafka Consumer Groups позволяют масштабировать обработку заказов
- Несколько инстансов сервиса могут работать в одной Consumer Group
- Автоматический rebalance при добавлении/удалении инстансов

**Вертикальное масштабирование**:
- Connection pools настроены для highload
- Redis PoolSize: 1000 соединений
- PostgreSQL MaxOpenConns: 25

**Оптимизации**:
- Батчинг в Kafka Consumer (MinBytes=10KB)
- Асинхронная отправка в Kafka Producer
- Кэширование меню в Redis
- Партиционирование таблицы orders по дате
- Индексы на часто используемых полях

### Мониторинг

**Health Check**:
- `GET /api/v1/health` - Проверка состояния сервиса

**Логирование**:
- Структурированные логи через `log.Printf`
- Логирование всех HTTP запросов (метод, путь, статус, latency)
- Логирование Kafka операций (отправка, получение, commit)

**Метрики** (потенциально):
- Счетчик обработанных заказов (KafkaWSConsumer.processed)
- Счетчик отправленных сообщений (OrderGRPCServer.kafkaSentCount)
- Количество подключенных WebSocket клиентов

---

## 🚀 Deployment

### Docker Compose (локальная разработка)

**Сервисы**:
- `postgres`: PostgreSQL 15
- `redis`: Redis 7
- `kafka`: Confluent Kafka 7.5.0
- `zookeeper`: Zookeeper для Kafka
- `api`: Go приложение
- `adminer`: Web UI для PostgreSQL

**Запуск**:
```bash
docker-compose up -d
```

### Railway (Production)

**Переменные окружения**:
- `DATABASE_URL` - PostgreSQL connection string
- `REDIS_URL` - Redis connection string
- `KAFKA_BROKERS` - Kafka brokers (через запятую)
- `KAFKA_USERNAME`, `KAFKA_PASSWORD`, `KAFKA_CA_CERT` - Kafka security
- `PORT` - Порт сервера (8080 по умолчанию)
- `BUSINESS_OPEN_HOUR`, `BUSINESS_OPEN_MIN` - Время открытия (UTC)
- `BUSINESS_CLOSE_HOUR`, `BUSINESS_CLOSE_MIN` - Время закрытия (UTC)

**Особенности**:
- Автоматическое определение DATABASE_URL из Railway PostgreSQL сервиса
- Поддержка различных форматов URL (postgresql://, postgres://)
- Graceful degradation при отсутствии сервисов (Redis, Kafka)

---

## 📝 Ключевые паттерны и решения

### 1. Event-Driven Architecture

Заказы обрабатываются асинхронно через Kafka:
- Producer отправляет заказ в Kafka
- Consumer обрабатывает заказ и отправляет в WebSocket
- Разделение ответственности: создание заказа не блокируется обработкой

### 2. State Recovery (BootstrapState)

При перезапуске сервера:
1. Загружаются активные заказы из PostgreSQL
2. Восстанавливаются в Redis
3. Kafka Consumer начинает с LastOffset (не обрабатывает старые заказы)

### 3. Capacity-Based Slot Scheduling

Распределение заказов по временным слотам на основе емкости в рублях:
- Атомарные операции через Redis Lua scripts
- Предотвращение перегрузки кухни
- Гибкая конфигурация через API

### 4. Hot Reload Menu

Меню обновляется без перезапуска сервера:
- Pub/Sub уведомления через Redis
- Fallback таймер для автообновления
- Кэширование в Redis для быстрого доступа

### 5. Dual WebSocket Hubs

Разделение WebSocket соединений:
- GlobalHub для планшетов поваров
- ERPHub для ERP Dashboard
- Независимое управление клиентами

### 6. Protobuf для производительности

Использование Protobuf вместо JSON:
- Меньший размер сообщений
- Быстрее сериализация/десериализация
- Типобезопасность

---

## 🔄 Жизненный цикл заказа

```
1. Создание заказа (gRPC)
   ├─► Валидация
   ├─► Назначение слота (SlotService)
   ├─► Отправка в Kafka (Protobuf)
   └─► Response (OrderID, DisplayID, SlotTime)

2. Обработка в Kafka Consumer
   ├─► Чтение из Kafka
   ├─► Сохранение в PostgreSQL
   ├─► Сохранение в Redis
   └─► Broadcast через WebSocket

3. Отображение на планшете повара
   ├─► Получение через WebSocket
   └─► Отображение заказа

4. Обновление статуса
   ├─► preparing → cooking → ready
   ├─► Обновление в Redis
   ├─► Обновление в PostgreSQL
   └─► Broadcast обновления

5. Завершение заказа
   ├─► delivered / cancelled
   ├─► Архивирование (через 1 год)
   └─► Удаление из Redis (TTL 24 часа)
```

---

## 📚 Дополнительная документация

- [Kafka Retention Policy](docs/KAFKA_RETENTION_POLICY.md) - Настройка retention policy для Kafka
- [Technologist Workspace Architecture](TECHNOLOGIST_WORKSPACE_ARCHITECTURE.md) - Архитектура рабочего пространства технолога
- [Data Flow Audit Report](DATA_FLOW_AUDIT_REPORT.md) - Аудит потоков данных
- [Redis Pub/Sub Menu](REDIS_PUBSUB_MENU.md) - Использование Redis Pub/Sub для меню
- [Menu Database](MENU_DATABASE.md) - Структура базы данных меню

---

## 🎯 Заключение

Система построена на современных технологиях и паттернах:
- **Event-Driven**: Асинхронная обработка через Kafka
- **Real-time**: WebSocket для мгновенных обновлений
- **Resilient**: Восстановление состояния после перезапуска
- **Scalable**: Горизонтальное масштабирование через Consumer Groups
- **High Performance**: Оптимизации для highload (батчинг, connection pools, кэширование)

Архитектура позволяет обрабатывать тысячи заказов в день с минимальной задержкой и высокой надежностью.

