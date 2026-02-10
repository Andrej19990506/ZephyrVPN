-- Миграция: Генерация тестовых данных о продажах за последние 60 дней
-- Для тестирования интеграции с Nixtla TimeGPT
-- Период: с 2025-12-10 по 2026-02-08 (60 дней)
--
-- Данные записываются в таблицу: orders
-- Таблица должна быть создана миграцией 013_create_partitioned_orders_table.sql
--
-- Выручка по дням недели:
--   - Будние дни (пн-чт): 250,000 - 300,000 рублей
--   - Пятница-суббота: 450,000 - 550,000 рублей
--   - Воскресенье: 200,000 - 300,000 рублей
--
-- Все тестовые заказы имеют display_id с префиксом 'TEST-'

-- Функция для генерации случайного числа в диапазоне
CREATE OR REPLACE FUNCTION random_between(min_val NUMERIC, max_val NUMERIC)
RETURNS NUMERIC AS $$
BEGIN
    RETURN min_val + (random() * (max_val - min_val));
END;
$$ LANGUAGE plpgsql;

-- Функция для определения дня недели (0=воскресенье, 6=суббота)
CREATE OR REPLACE FUNCTION get_day_of_week(date_val DATE)
RETURNS INTEGER AS $$
BEGIN
    RETURN EXTRACT(DOW FROM date_val)::INTEGER;
END;
$$ LANGUAGE plpgsql;

-- Функция для получения базовой выручки в зависимости от дня недели
CREATE OR REPLACE FUNCTION get_base_revenue_for_day(date_val DATE)
RETURNS NUMERIC AS $$
DECLARE
    day_of_week INTEGER;
    base_revenue NUMERIC;
BEGIN
    day_of_week := get_day_of_week(date_val);
    
    -- Пятница (5) и суббота (6) - пиковые дни: 450,000 - 550,000 рублей
    IF day_of_week = 5 OR day_of_week = 6 THEN
        base_revenue := random_between(450000, 550000);
    -- Воскресенье (0) - средняя выручка: 200,000 - 300,000 рублей
    ELSIF day_of_week = 0 THEN
        base_revenue := random_between(200000, 300000);
    -- Будние дни (понедельник-четверг, 1-4) - обычная выручка: 250,000 - 300,000 рублей
    ELSE
        base_revenue := random_between(250000, 300000);
    END IF;
    
    RETURN base_revenue;
END;
$$ LANGUAGE plpgsql;

-- Проверяем существование таблицы orders
DO $$
BEGIN
    IF NOT EXISTS (SELECT FROM information_schema.tables WHERE table_name = 'orders') THEN
        RAISE EXCEPTION 'Таблица orders не найдена! Сначала выполните миграцию 013_create_partitioned_orders_table.sql';
    END IF;
    RAISE NOTICE 'Таблица orders найдена, продолжаем генерацию данных...';
END $$;

-- Убеждаемся, что партиции для нужных месяцев созданы
DO $$
DECLARE
    partition_date DATE;
    months_to_create INTEGER[] := ARRAY[-2, -1, 0, 1]; -- Декабрь 2025, Январь 2026, Февраль 2026, Март 2026
    i INTEGER;
BEGIN
    FOR i IN 1..array_length(months_to_create, 1) LOOP
        partition_date := DATE_TRUNC('month', CURRENT_DATE) + (months_to_create[i] || ' months')::INTERVAL;
        PERFORM create_orders_partition(partition_date);
    END LOOP;
    RAISE NOTICE 'Партиции для тестовых данных проверены/созданы';
END $$;

-- Генерируем тестовые заказы за последние 60 дней
DO $$
DECLARE
    loop_date DATE;
    end_date DATE;
    base_revenue NUMERIC;
    final_revenue NUMERIC;
    order_count INTEGER;
    i INTEGER;
    payment_methods TEXT[] := ARRAY['CASH', 'CARD_ONLINE', 'CARD', 'ONLINE'];
    statuses TEXT[] := ARRAY['delivered', 'ready', 'archived'];
    selected_payment TEXT;
    selected_status TEXT;
    order_id UUID;
    display_id TEXT;
    created_at TIMESTAMP WITH TIME ZONE;
    items_json JSONB;
    discount_amount INTEGER;
    discount_percent INTEGER;
    final_price INTEGER;
    target_total INTEGER;
    current_total INTEGER;
    avg_order_price NUMERIC;
BEGIN
    -- Начальная дата: 2025-12-10
    loop_date := '2025-12-10'::DATE;
    -- Конечная дата: сегодня (2026-02-08)
    end_date := CURRENT_DATE;
    
    RAISE NOTICE 'Генерация тестовых данных о продажах с % по %', loop_date, end_date;
    
    -- Проходим по каждому дню
    WHILE loop_date <= end_date LOOP
        -- Получаем базовую выручку для этого дня недели
        base_revenue := get_base_revenue_for_day(loop_date);
        final_revenue := base_revenue;
        
        -- Генерируем от 30 до 80 заказов в день (для соответствия выручке 250-550 тысяч)
        order_count := floor(random_between(30, 80))::INTEGER;
        
        RAISE NOTICE 'Дата: %, день недели: %, выручка: %, заказов: %', 
            loop_date, 
            get_day_of_week(loop_date),
            final_revenue,
            order_count;
        
        -- Создаем заказы для этого дня
        -- Рассчитываем средний чек для распределения выручки
        target_total := floor(final_revenue)::INTEGER;
        current_total := 0;
        avg_order_price := final_revenue / order_count;
        
        FOR i IN 1..order_count LOOP
            -- Генерируем случайные значения
            selected_payment := payment_methods[floor(random() * array_length(payment_methods, 1) + 1)::INTEGER];
            selected_status := statuses[floor(random() * array_length(statuses, 1) + 1)::INTEGER];
            
            -- Распределяем выручку между заказами
            -- Для последнего заказа используем остаток, чтобы сумма точно совпала
            IF i = order_count THEN
                final_price := GREATEST(500, target_total - current_total); -- Минимум 500 рублей
            ELSE
                -- Генерируем цену заказа: средний чек ± 50% (для реалистичности)
                final_price := floor(avg_order_price * random_between(0.5, 1.5))::INTEGER;
                -- Ограничиваем разумными пределами: от 500 до 15000 рублей
                final_price := GREATEST(500, LEAST(15000, final_price));
            END IF;
            
            -- Скидка: 0-10% (иногда)
            IF random() < 0.3 THEN -- 30% заказов со скидкой
                discount_percent := floor(random_between(5, 10))::INTEGER;
                discount_amount := floor(final_price * discount_percent / 100.0)::INTEGER;
            ELSE
                discount_percent := 0;
                discount_amount := 0;
            END IF;
            
            -- Генерируем UUID и display_id
            order_id := gen_random_uuid();
            display_id := 'TEST-' || TO_CHAR(loop_date, 'YYYYMMDD') || '-' || LPAD(i::TEXT, 3, '0');
            
            -- Время создания заказа (в течение дня, случайное)
            created_at := (loop_date::TIMESTAMP + (random() * INTERVAL '1 day'))::TIMESTAMP WITH TIME ZONE;
            
            -- Создаем простой JSON для items (минимум 1 пицца)
            items_json := jsonb_build_array(
                jsonb_build_object(
                    'id', gen_random_uuid()::TEXT,
                    'name', 'Тестовая пицца',
                    'quantity', floor(random_between(1, 3))::INTEGER,
                    'price', final_price
                )
            );
            
            -- Вставляем заказ в таблицу orders
            INSERT INTO orders (
                id,
                display_id,
                customer_id,
                payment_method,
                is_pickup,
                items,
                is_set,
                total_price,
                discount_amount,
                discount_percent,
                final_price,
                status,
                created_at,
                updated_at,
                completed_at
            ) VALUES (
                order_id,
                display_id,
                NULL, -- customer_id (опционально)
                selected_payment,
                CASE WHEN random() < 0.3 THEN TRUE ELSE FALSE END, -- 30% самовывоз
                items_json,
                FALSE,
                final_price,
                discount_amount,
                discount_percent,
                final_price - discount_amount,
                selected_status,
                created_at,
                created_at,
                CASE 
                    WHEN selected_status IN ('delivered', 'ready', 'archived') 
                    THEN created_at + (random() * INTERVAL '2 hours')
                    ELSE NULL
                END
            );
            
            -- Учитываем итоговую цену после скидки для расчета общей выручки
            current_total := current_total + (final_price - discount_amount);
        END LOOP;
        
        -- Переходим к следующему дню
        loop_date := loop_date + INTERVAL '1 day';
    END LOOP;
    
    RAISE NOTICE 'Генерация тестовых данных завершена';
END $$;

-- Проверяем результаты
DO $$
DECLARE
    total_orders BIGINT;
    total_revenue NUMERIC;
    date_range TEXT;
    avg_daily_revenue NUMERIC;
BEGIN
    SELECT COUNT(*), SUM(COALESCE(final_price, total_price))
    INTO total_orders, total_revenue
    FROM orders
    WHERE display_id LIKE 'TEST-%'
      AND status IN ('delivered', 'ready', 'archived')
      AND created_at >= '2025-12-10'::DATE
      AND created_at < CURRENT_DATE + INTERVAL '1 day';
    
    SELECT 
        MIN(created_at::DATE)::TEXT || ' - ' || MAX(created_at::DATE)::TEXT
    INTO date_range
    FROM orders
    WHERE display_id LIKE 'TEST-%';
    
    avg_daily_revenue := total_revenue / 60.0;
    
    RAISE NOTICE '========================================';
    RAISE NOTICE 'Статистика тестовых данных:';
    RAISE NOTICE 'Период: %', date_range;
    RAISE NOTICE 'Всего заказов: %', total_orders;
    RAISE NOTICE 'Общая выручка: %.2f руб.', total_revenue;
    RAISE NOTICE 'Средняя выручка в день: %.2f руб.', avg_daily_revenue;
    RAISE NOTICE '========================================';
END $$;

-- Очистка временных функций (опционально, можно оставить для будущего использования)
-- DROP FUNCTION IF EXISTS random_between(NUMERIC, NUMERIC);
-- DROP FUNCTION IF EXISTS get_day_of_week(DATE);
-- DROP FUNCTION IF EXISTS get_seasonality_multiplier(DATE);

COMMENT ON FUNCTION random_between IS 'Генерирует случайное число в заданном диапазоне';
COMMENT ON FUNCTION get_day_of_week IS 'Возвращает день недели (0=воскресенье, 6=суббота)';
COMMENT ON FUNCTION get_base_revenue_for_day IS 'Возвращает базовую выручку для дня недели: будни 250-300к, пт-сб 450-550к, вс 200-300к';

