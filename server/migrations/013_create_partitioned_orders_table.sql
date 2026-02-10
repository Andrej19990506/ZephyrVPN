-- Миграция: Создание партиционированной таблицы orders для хранения исторических данных
-- Партиционирование по месяцам для оптимизации запросов и управления жизненным циклом данных

-- Создаем основную таблицу с партиционированием по RANGE (created_at)
-- ВАЖНО: PRIMARY KEY в партиционированной таблице должен включать колонку партиционирования
CREATE TABLE IF NOT EXISTS orders (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    display_id VARCHAR(50) NOT NULL,
    customer_id INTEGER,
    customer_first_name VARCHAR(255),
    customer_last_name VARCHAR(255),
    customer_phone VARCHAR(50),
    delivery_address TEXT,
    payment_method VARCHAR(50),
    is_pickup BOOLEAN DEFAULT FALSE,
    pickup_location_id UUID,
    call_before_minutes INTEGER,
    
    -- Данные заказа
    items JSONB NOT NULL, -- Массив PizzaItem в JSON формате
    is_set BOOLEAN DEFAULT FALSE,
    set_name VARCHAR(255),
    total_price INTEGER NOT NULL,
    discount_amount INTEGER DEFAULT 0,
    discount_percent INTEGER DEFAULT 0,
    final_price INTEGER,
    notes TEXT,
    
    -- Статус и временные метки
    status VARCHAR(50) NOT NULL DEFAULT 'pending', -- pending, preparing, cooking, ready, delivered, cancelled, archived
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMP WITH TIME ZONE,
    cancelled_at TIMESTAMP WITH TIME ZONE,
    
    -- Capacity-Based Slot Scheduling
    target_slot_id VARCHAR(100),
    target_slot_start_time TIMESTAMP WITH TIME ZONE,
    visible_at TIMESTAMP WITH TIME ZONE,
    
    -- Метаданные
    branch_id UUID,
    station_id UUID,
    staff_id UUID, -- ID сотрудника, который принял заказ
    
    -- Индексы для партиционированной таблицы (будут созданы на каждой партиции)
    CONSTRAINT orders_status_check CHECK (status IN ('pending', 'preparing', 'cooking', 'ready', 'delivered', 'cancelled', 'archived')),
    -- PRIMARY KEY должен включать created_at (колонку партиционирования)
    PRIMARY KEY (id, created_at)
) PARTITION BY RANGE (created_at);

-- Создаем индексы на основной таблице (будут наследоваться партициями)
CREATE INDEX IF NOT EXISTS idx_orders_status_created_at ON orders (status, created_at);
CREATE INDEX IF NOT EXISTS idx_orders_created_at ON orders (created_at);
CREATE INDEX IF NOT EXISTS idx_orders_status ON orders (status);
CREATE INDEX IF NOT EXISTS idx_orders_display_id ON orders (display_id);
CREATE INDEX IF NOT EXISTS idx_orders_customer_id ON orders (customer_id);
CREATE INDEX IF NOT EXISTS idx_orders_target_slot_id ON orders (target_slot_id);
CREATE INDEX IF NOT EXISTS idx_orders_branch_id ON orders (branch_id);
CREATE INDEX IF NOT EXISTS idx_orders_visible_at ON orders (visible_at) WHERE visible_at IS NOT NULL;

-- GIN индекс для быстрого поиска по JSONB полю items
CREATE INDEX IF NOT EXISTS idx_orders_items_gin ON orders USING GIN (items);

-- Функция для автоматического создания партиций (вызывается по расписанию или вручную)
CREATE OR REPLACE FUNCTION create_orders_partition(partition_date DATE)
RETURNS VOID AS $$
DECLARE
    partition_name TEXT;
    start_date DATE;
    end_date DATE;
BEGIN
    -- Вычисляем начало и конец месяца
    start_date := DATE_TRUNC('month', partition_date);
    end_date := start_date + INTERVAL '1 month';
    
    -- Формируем имя партиции (orders_YYYY_MM)
    partition_name := 'orders_' || TO_CHAR(start_date, 'YYYY_MM');
    
    -- Создаем партицию, если она еще не существует
    EXECUTE format('
        CREATE TABLE IF NOT EXISTS %I PARTITION OF orders
        FOR VALUES FROM (%L) TO (%L)',
        partition_name,
        start_date,
        end_date
    );
    
    RAISE NOTICE 'Partition % created for period % to %', partition_name, start_date, end_date;
END;
$$ LANGUAGE plpgsql;

-- Создаем партиции на 12 месяцев вперед (текущий месяц + 11 следующих)
DO $$
DECLARE
    i INTEGER;
    partition_date DATE;
BEGIN
    FOR i IN 0..11 LOOP
        partition_date := DATE_TRUNC('month', CURRENT_DATE) + (i || ' months')::INTERVAL;
        PERFORM create_orders_partition(partition_date);
    END LOOP;
END $$;

-- Функция для автоматического создания партиций (можно вызывать по cron)
CREATE OR REPLACE FUNCTION ensure_orders_partitions()
RETURNS VOID AS $$
DECLARE
    months_ahead INTEGER := 3; -- Создаем партиции на 3 месяца вперед
    i INTEGER;
    partition_date DATE;
BEGIN
    FOR i IN 0..months_ahead LOOP
        partition_date := DATE_TRUNC('month', CURRENT_DATE) + (i || ' months')::INTERVAL;
        PERFORM create_orders_partition(partition_date);
    END LOOP;
END;
$$ LANGUAGE plpgsql;

-- Комментарии для документации
COMMENT ON TABLE orders IS 'Партиционированная таблица заказов. Партиции создаются по месяцам для оптимизации запросов и управления жизненным циклом данных.';
COMMENT ON COLUMN orders.status IS 'Статус заказа: pending, cooking, ready, delivered, cancelled, archived';
COMMENT ON COLUMN orders.items IS 'JSONB массив элементов заказа (PizzaItem)';
COMMENT ON COLUMN orders.target_slot_id IS 'ID временного слота для Capacity-Based Slot Scheduling';
COMMENT ON COLUMN orders.visible_at IS 'Время, когда заказ должен появиться на планшете повара';

-- Триггер для автоматического обновления updated_at
CREATE OR REPLACE FUNCTION update_orders_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER orders_updated_at_trigger
    BEFORE UPDATE ON orders
    FOR EACH ROW
    EXECUTE FUNCTION update_orders_updated_at();

