# Purchase Order Module - Design Documentation

## Обзор

Модуль закупок (Purchase Orders) предназначен для разделения процесса заказа товаров у поставщиков от физического оприходования товара на складе. Это позволяет менеджерам создавать заказы без доступа к остаткам, а складским работникам автоматически оприходовывать товар при получении.

## Краткое резюме

### Что реализовано:

1. **SQL миграция** (`020_create_purchase_orders_tables.sql`):
   - Таблица `purchase_orders` с полной схемой
   - Таблица `purchase_order_items` для позиций заказа
   - Триггеры для автоматического расчета сумм и генерации номеров
   - Индексы для производительности

2. **Go модели** (`internal/models/purchase_order.go`):
   - `PurchaseOrder` - основная модель заказа
   - `PurchaseOrderItem` - модель позиции заказа
   - Методы для проверки статусов и расчетов

3. **Сервис** (`internal/services/purchase_order_service.go`):
   - CRUD операции для заказов
   - Функция `ReceivePurchaseOrder()` - автоматическое создание накладной и оприходование
   - Фильтрация просроченных заказов
   - Интеграция с `StockService`

### Ключевые особенности:

- ✅ **Исторические данные о ценах**: Цена закупки фиксируется на момент создания заказа
- ✅ **Автоматизация**: При получении заказа автоматически создается накладная и оприходуется товар
- ✅ **Частичное получение**: Поддержка частичного получения товара с обновлением статуса
- ✅ **Фильтрация просроченных**: Легко найти заказы, которые не получены вовремя
- ✅ **Интеграция**: Полная интеграция с существующими модулями (Inventory, Nomenclature, Counterparties)

## Архитектура

### Основные сущности

1. **PurchaseOrder** - Заказ на закупку
   - Создается менеджером
   - Содержит информацию о поставщике, филиале, ожидаемой дате доставки
   - Имеет статусы: `draft`, `ordered`, `partially_received`, `received`, `cancelled`

2. **PurchaseOrderItem** - Позиция заказа
   - Связь с номенклатурой (NomenclatureItem)
   - Хранит заказанное количество и цену закупки на момент создания заказа
   - Отслеживает фактически полученное количество

3. **Invoice** (существующая) - Входящая накладная
   - Создается автоматически при получении заказа
   - Связывается с PurchaseOrder через `invoice_id`

## Схема базы данных

### Таблица `purchase_orders`

```sql
CREATE TABLE purchase_orders (
    id UUID PRIMARY KEY,
    order_number VARCHAR(100) UNIQUE NOT NULL, -- PO-2026-001
    supplier_id UUID NOT NULL REFERENCES counterparties(id),
    branch_id UUID NOT NULL REFERENCES branches(id),
    status VARCHAR(50) NOT NULL DEFAULT 'draft',
    order_date DATE NOT NULL,
    expected_delivery_date DATE NOT NULL, -- Для фильтрации просроченных
    actual_delivery_date DATE,
    total_amount DECIMAL(15,2) NOT NULL DEFAULT 0,
    currency VARCHAR(3) DEFAULT 'RUB',
    payment_terms VARCHAR(255),
    payment_method VARCHAR(50) DEFAULT 'bank',
    created_by VARCHAR(255) NOT NULL,
    approved_by VARCHAR(255),
    received_by VARCHAR(255),
    invoice_id UUID REFERENCES invoices(id), -- Связь с накладной
    notes TEXT,
    internal_notes TEXT,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    deleted_at TIMESTAMP
);
```

### Таблица `purchase_order_items`

```sql
CREATE TABLE purchase_order_items (
    id UUID PRIMARY KEY,
    purchase_order_id UUID NOT NULL REFERENCES purchase_orders(id) ON DELETE CASCADE,
    nomenclature_id UUID NOT NULL REFERENCES nomenclature_items(id),
    ordered_quantity DECIMAL(10,2) NOT NULL,
    unit VARCHAR(20) NOT NULL DEFAULT 'kg',
    purchase_price_per_unit DECIMAL(10,2) NOT NULL, -- Цена на момент заказа (исторические данные)
    total_price DECIMAL(15,2) NOT NULL,
    received_quantity DECIMAL(10,2) DEFAULT 0,
    received_total_price DECIMAL(15,2) DEFAULT 0,
    notes TEXT,
    expiry_date DATE,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);
```

## Бизнес-логика

### 1. Создание заказа на закупку

**Процесс:**
1. Менеджер создает заказ (статус `draft`)
2. Добавляет позиции из номенклатуры
3. Указывает количество и цену закупки (может браться из `NomenclatureItem.LastPrice` или вводиться вручную)
4. Указывает ожидаемую дату доставки
5. Сохраняет заказ

**Важно:**
- Цена закупки (`purchase_price_per_unit`) фиксируется на момент создания заказа
- Это позволяет отслеживать изменения цен со временем
- Общая сумма заказа (`total_amount`) рассчитывается автоматически из позиций

### 2. Отправка заказа поставщику

**Процесс:**
1. Менеджер отправляет заказ поставщику (статус меняется на `ordered`)
2. Заказ больше нельзя редактировать (только отменить)
3. Заказ появляется в списке ожидаемых поставок

### 3. Получение заказа (Receive Purchase Order)

**Процесс:**
1. Складской работник получает товар от поставщика
2. Вызывает функцию `ReceivePurchaseOrder(purchaseOrderID, receivedItems, performedBy)`
3. Система создает накладную-черновик (`Invoice` со статусом `draft`):
   - **НЕ оприходует товар автоматически**
   - Создает накладную для сверки бухгалтерией
   - Обновляет статус заказа (`partially_received` или `received`)
   - Связывает накладную с заказом через `invoice_id`
4. Бухгалтерия делает сверку по накладной:
   - Проверяет количество, цены, документы от поставщика
   - Подтверждает накладную (меняет статус на `completed`)
   - Только после подтверждения товар оприходуется через `StockService.ProcessInboundInvoiceBatch()`

**Детали реализации:**

```go
func (s *PurchaseOrderService) ReceivePurchaseOrder(
    purchaseOrderID string,
    receivedItems []ReceivedItem, // Массив с фактически полученными товарами
    performedBy string, // Username складского работника
) error {
    // 1. Загружаем заказ
    var order models.PurchaseOrder
    if err := s.db.Preload("Items").Preload("Items.Nomenclature").First(&order, "id = ?", purchaseOrderID).Error; err != nil {
        return fmt.Errorf("заказ не найден: %w", err)
    }
    
    // 2. Проверяем, что заказ не отменен и не получен полностью
    if order.IsCancelled() {
        return fmt.Errorf("нельзя получить отмененный заказ")
    }
    if order.IsReceived() {
        return fmt.Errorf("заказ уже получен полностью")
    }
    
    // 3. Создаем или обновляем входящую накладную
    var invoice *models.Invoice
    if order.InvoiceID != nil {
        // Обновляем существующую накладную (частичное получение)
        invoice, err = s.stockService.GetInvoice(*order.InvoiceID)
        if err != nil {
            return fmt.Errorf("ошибка загрузки накладной: %w", err)
        }
    } else {
        // Создаем новую накладную
        invoice = &models.Invoice{
            Number:        fmt.Sprintf("INV-%s", order.OrderNumber),
            CounterpartyID: &order.SupplierID,
            BranchID:      order.BranchID,
            Status:        models.InvoiceStatusDraft,
            InvoiceDate:   time.Now(),
            PerformedBy:   performedBy,
            Notes:         fmt.Sprintf("Автоматически создана из заказа %s", order.OrderNumber),
        }
        if err := s.stockService.CreateInvoice(invoice); err != nil {
            return fmt.Errorf("ошибка создания накладной: %w", err)
        }
    }
    
    // 4. Обрабатываем каждую позицию заказа
    var invoiceItems []map[string]interface{}
    totalAmount := 0.0
    
    for _, receivedItem := range receivedItems {
        // Находим соответствующую позицию заказа
        var orderItem *models.PurchaseOrderItem
        for i := range order.Items {
            if order.Items[i].ID == receivedItem.OrderItemID {
                orderItem = &order.Items[i]
                break
            }
        }
        if orderItem == nil {
            return fmt.Errorf("позиция заказа не найдена: %s", receivedItem.OrderItemID)
        }
        
        // Обновляем полученное количество
        orderItem.ReceivedQuantity += receivedItem.Quantity
        orderItem.ReceivedTotalPrice = orderItem.ReceivedQuantity * orderItem.PurchasePricePerUnit
        
        // Добавляем товар в накладную
        invoiceItem := map[string]interface{}{
            "nomenclature_id": orderItem.NomenclatureID,
            "quantity":        receivedItem.Quantity,
            "unit":            orderItem.Unit,
            "price_per_unit":  orderItem.PurchasePricePerUnit,
            "expiry_date":     receivedItem.ExpiryDate, // Если указан
            "branch_id":       order.BranchID,
        }
        invoiceItems = append(invoiceItems, invoiceItem)
        totalAmount += orderItem.ReceivedTotalPrice
    }
    
    // 5. Оприходуем товар через StockService
    if err := s.stockService.ProcessInboundInvoiceBatch(
        invoice.ID,
        invoiceItems,
        performedBy,
        order.SupplierID,
        totalAmount,
        order.PaymentMethod == "cash",
        time.Now().Format("2006-01-02"),
    ); err != nil {
        return fmt.Errorf("ошибка оприходования товара: %w", err)
    }
    
    // 6. Обновляем статус заказа
    allItemsReceived := true
    allItemsPartial := false
    for _, item := range order.Items {
        if !item.IsFullyReceived() {
            allItemsReceived = false
        }
        if item.IsPartiallyReceived() {
            allItemsPartial = true
        }
    }
    
    if allItemsReceived {
        order.Status = models.PurchaseOrderStatusReceived
        order.ActualDeliveryDate = &time.Time{}
        *order.ActualDeliveryDate = time.Now()
    } else if allItemsPartial {
        order.Status = models.PurchaseOrderStatusPartiallyReceived
    }
    
    order.ReceivedBy = &performedBy
    order.InvoiceID = &invoice.ID
    
    // 7. Сохраняем изменения
    tx := s.db.Begin()
    defer func() {
        if r := recover(); r != nil {
            tx.Rollback()
        }
    }()
    
    // Обновляем позиции заказа
    for _, item := range order.Items {
        if err := tx.Save(&item).Error; err != nil {
            tx.Rollback()
            return fmt.Errorf("ошибка обновления позиции: %w", err)
        }
    }
    
    // Обновляем заказ
    if err := tx.Save(&order).Error; err != nil {
        tx.Rollback()
        return fmt.Errorf("ошибка обновления заказа: %w", err)
    }
    
    tx.Commit()
    
    return nil
}
```

### 4. Фильтрация просроченных заказов (Overdue Orders)

**Логика:**
- Заказ считается просроченным, если:
  - `expected_delivery_date < CURRENT_DATE`
  - Статус заказа НЕ `received` и НЕ `cancelled`
  - Статус заказа `ordered` или `partially_received`

**SQL запрос для фильтрации:**

```sql
SELECT *
FROM purchase_orders
WHERE expected_delivery_date < CURRENT_DATE
  AND status IN ('ordered', 'partially_received')
  AND deleted_at IS NULL
ORDER BY expected_delivery_date ASC;
```

**Go функция:**

```go
func (s *PurchaseOrderService) GetOverdueOrders(branchID string) ([]models.PurchaseOrder, error) {
    var orders []models.PurchaseOrder
    
    query := s.db.Model(&models.PurchaseOrder{}).
        Preload("Supplier").
        Preload("Branch").
        Preload("Items").
        Preload("Items.Nomenclature").
        Where("expected_delivery_date < ?", time.Now()).
        Where("status IN ?", []models.PurchaseOrderStatus{
            models.PurchaseOrderStatusOrdered,
            models.PurchaseOrderStatusPartiallyReceived,
        }).
        Where("deleted_at IS NULL")
    
    if branchID != "" && branchID != "all" {
        query = query.Where("branch_id = ?", branchID)
    }
    
    query = query.Order("expected_delivery_date ASC")
    
    if err := query.Find(&orders).Error; err != nil {
        return nil, fmt.Errorf("ошибка получения просроченных заказов: %w", err)
    }
    
    return orders, nil
}
```

## Интеграция с существующими модулями

### 1. Интеграция с Inventory Module

**Связь через Invoice:**
- При получении заказа создается `Invoice` (входящая накладная)
- `Invoice` обрабатывается через `StockService.ProcessInboundInvoiceBatch()`
- Автоматически создаются `StockBatch` (партии товара)
- Обновляются остатки в `StockService`

**Преимущества:**
- Единая точка входа для оприходования товара
- Переиспользование существующей логики обработки накладных
- Автоматическое создание финансовых транзакций

### 2. Интеграция с Nomenclature Module

**Связь через NomenclatureItem:**
- Позиции заказа ссылаются на `NomenclatureItem.ID`
- Цена закупки может браться из `NomenclatureItem.LastPrice`
- Единицы измерения берутся из `NomenclatureItem.InboundUnit`

**Обновление LastPrice:**
- При получении заказа можно обновлять `NomenclatureItem.LastPrice` последней ценой закупки
- Это помогает при создании следующих заказов

### 3. Интеграция с Counterparties Module

**Связь через Counterparty:**
- Заказ ссылается на `Counterparty` (поставщика)
- Используется информация о поставщике (название, контакты, условия оплаты)

## API Endpoints

### Purchase Orders

- `GET /api/v1/purchase-orders` - Список заказов (с фильтрацией по статусу, филиалу, просроченным)
- `GET /api/v1/purchase-orders/:id` - Детали заказа
- `POST /api/v1/purchase-orders` - Создание заказа
- `PUT /api/v1/purchase-orders/:id` - Обновление заказа (только для `draft`)
- `DELETE /api/v1/purchase-orders/:id` - Отмена заказа (soft delete)
- `POST /api/v1/purchase-orders/:id/send` - Отправка заказа поставщику (draft → ordered)
- `POST /api/v1/purchase-orders/:id/receive` - Получение заказа (создание накладной и оприходование)

### Purchase Order Items

- `GET /api/v1/purchase-orders/:id/items` - Позиции заказа
- `POST /api/v1/purchase-orders/:id/items` - Добавление позиции
- `PUT /api/v1/purchase-orders/:id/items/:itemId` - Обновление позиции
- `DELETE /api/v1/purchase-orders/:id/items/:itemId` - Удаление позиции

## Безопасность и права доступа

### Роли и разрешения

1. **Менеджер по закупкам:**
   - Создание заказов (`draft`)
   - Редактирование заказов (`draft`)
   - Отправка заказов поставщику (`draft` → `ordered`)
   - Просмотр всех заказов

2. **Складской работник:**
   - Просмотр заказов со статусом `ordered` или `partially_received`
   - Получение заказов (создание накладных и оприходование)
   - НЕ может создавать или редактировать заказы

3. **Администратор:**
   - Все права менеджера и складского работника
   - Отмена заказов
   - Просмотр внутренних заметок

## Примеры использования

### Пример 1: Создание заказа

```go
order := &models.PurchaseOrder{
    SupplierID:          "supplier-uuid",
    BranchID:            "branch-uuid",
    ExpectedDeliveryDate: time.Now().AddDate(0, 0, 7), // Через 7 дней
    PaymentTerms:        "Оплата в течение 30 дней",
    PaymentMethod:       "bank",
    CreatedBy:           "manager_username",
    Notes:               "Срочный заказ для нового меню",
}

items := []models.PurchaseOrderItem{
    {
        NomenclatureID:      "nomenclature-uuid-1",
        OrderedQuantity:     100.0,
        Unit:                "kg",
        PurchasePricePerUnit: 250.0, // 250₽ за кг
    },
    {
        NomenclatureID:      "nomenclature-uuid-2",
        OrderedQuantity:     50.0,
        Unit:                "pcs",
        PurchasePricePerUnit: 150.0, // 150₽ за шт
    },
}

order.Items = items
order.TotalAmount = order.CalculateTotalAmount() // 100 * 250 + 50 * 150 = 32,500₽
```

### Пример 2: Получение заказа

```go
receivedItems := []ReceivedItem{
    {
        OrderItemID: "item-uuid-1",
        Quantity:    100.0, // Получено 100 кг
        ExpiryDate:  nil,
    },
    {
        OrderItemID: "item-uuid-2",
        Quantity:    45.0, // Получено 45 шт (заказано 50)
        ExpiryDate:  &expiryDate,
    },
}

err := purchaseOrderService.ReceivePurchaseOrder(
    "purchase-order-uuid",
    receivedItems,
    "warehouse_worker_username",
)
// Результат:
// - Создана накладная Invoice
// - Созданы партии StockBatch для каждого товара
// - Статус заказа: partially_received (вторая позиция не получена полностью)
```

## Миграция данных

Если в системе уже есть заказы в другом формате, можно создать миграцию для переноса данных:

```sql
-- Пример: перенос данных из старой таблицы orders (если существует)
INSERT INTO purchase_orders (
    id, order_number, supplier_id, branch_id, status,
    order_date, expected_delivery_date, total_amount,
    created_by, created_at, updated_at
)
SELECT 
    id,
    'PO-' || TO_CHAR(created_at, 'YYYY') || '-' || LPAD(ROW_NUMBER() OVER (ORDER BY created_at)::TEXT, 3, '0'),
    supplier_id,
    branch_id,
    CASE 
        WHEN status = 'completed' THEN 'received'
        WHEN status = 'pending' THEN 'ordered'
        ELSE 'draft'
    END,
    created_at::DATE,
    COALESCE(delivery_date, created_at::DATE + INTERVAL '7 days'),
    total_amount,
    created_by,
    created_at,
    updated_at
FROM old_orders
WHERE deleted_at IS NULL;
```

## Заключение

Модуль закупок обеспечивает:
- ✅ Разделение ответственности (менеджеры заказывают, склад получает)
- ✅ Исторические данные о ценах закупки
- ✅ Автоматизацию оприходования товара
- ✅ Отслеживание просроченных заказов
- ✅ Интеграцию с существующими модулями (Inventory, Nomenclature, Counterparties)

