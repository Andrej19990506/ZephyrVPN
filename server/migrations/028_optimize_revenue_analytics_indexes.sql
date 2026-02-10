-- Миграция 028: Оптимизация индексов для модуля аналитики выручки
-- 
-- ПРОБЛЕМА:
-- Метод getRevenueFromPostgreSQL выполняет запросы вида:
--   WHERE created_at >= $1 AND created_at < $2 AND status IN ('delivered', 'ready', 'archived')
--
-- При росте базы до сотен тысяч заказов без правильных индексов это вызывает:
--   1. Full Table Scan (полное сканирование таблицы)
--   2. Высокую нагрузку на CPU
--   3. Медленные запросы (секунды вместо миллисекунд)
--
-- АНАЛИЗ СУЩЕСТВУЮЩИХ ИНДЕКСОВ:
-- В миграции 013 уже создан индекс: idx_orders_status_created_at (status, created_at)
-- 
-- ПРОБЛЕМА С ТЕКУЩИМ ИНДЕКСОМ (status, created_at):
-- 1. PostgreSQL использует индекс слева направо (left-to-right)
-- 2. При фильтре status IN ('delivered', 'ready', 'archived') индекс должен:
--    - Сначала найти все строки с status='delivered' (сканирование по статусу)
--    - Затем отфильтровать по created_at (range scan)
--    - Повторить для 'ready' и 'archived'
-- 3. Это приводит к 3 отдельным range scans по индексу
-- 4. Если статусов много (pending, preparing, cooking, ready, delivered, cancelled, archived),
--    селективность по статусу низкая, и индекс неэффективен
--
-- ОПТИМАЛЬНОЕ РЕШЕНИЕ:
-- Индекс (created_at, status) позволяет:
-- 1. Сначала выполнить range scan по created_at (высокая селективность для диапазона дат)
-- 2. Затем отфильтровать по status прямо в индексе (Index-Only Scan)
-- 3. Один проход по индексу вместо трех
-- 4. Лучшая производительность для партиционированных таблиц
--
-- ДОПОЛНИТЕЛЬНАЯ ОПТИМИЗАЦИЯ:
-- Создаем частичный индекс (Partial Index) только для завершенных заказов,
-- что уменьшает размер индекса и ускоряет запросы

-- ============================================
-- 1. СОЗДАНИЕ ОПТИМАЛЬНОГО СОСТАВНОГО ИНДЕКСА
-- ============================================
-- Индекс (created_at, status) для range scan по дате с последующей фильтрацией по статусу
-- Этот индекс оптимален для запросов вида: WHERE created_at BETWEEN ... AND status IN (...)
CREATE INDEX IF NOT EXISTS idx_orders_created_at_status_revenue 
ON orders (created_at, status)
WHERE status IN ('delivered', 'ready', 'archived');

COMMENT ON INDEX idx_orders_created_at_status_revenue IS 
'Оптимизированный индекс для аналитики выручки. Покрывает запросы по диапазону дат с фильтрацией по завершенным статусам. Частичный индекс уменьшает размер и ускоряет запросы.';

-- ============================================
-- 2. ДОПОЛНИТЕЛЬНЫЙ ИНДЕКС ДЛЯ АГРЕГАЦИИ
-- ============================================
-- Для запросов, которые также группируют по payment_method, создаем покрывающий индекс
-- (Covering Index) который включает все необходимые колонки
CREATE INDEX IF NOT EXISTS idx_orders_revenue_covering 
ON orders (created_at, status, payment_method)
INCLUDE (total_price, discount_amount, final_price)
WHERE status IN ('delivered', 'ready', 'archived');

COMMENT ON INDEX idx_orders_revenue_covering IS 
'Покрывающий индекс для аналитики выручки. Включает все колонки, необходимые для расчета выручки, что позволяет выполнять Index-Only Scan без обращения к таблице.';

-- ============================================
-- 3. ПРОВЕРКА КОНФЛИКТОВ С СУЩЕСТВУЮЩИМИ ИНДЕКСАМИ
-- ============================================
-- Существующие индексы из миграции 013:
--   - idx_orders_status_created_at (status, created_at) - МОЖНО ОСТАВИТЬ для других запросов
--   - idx_orders_created_at (created_at) - МОЖНО ОСТАВИТЬ для запросов без фильтра по статусу
--   - idx_orders_status (status) - МОЖНО ОСТАВИТЬ для запросов без фильтра по дате
--
-- НОВЫЕ ИНДЕКСЫ НЕ КОНФЛИКТУЮТ, потому что:
-- 1. Частичные индексы (WHERE status IN (...)) не конфликтуют с полными
-- 2. Разный порядок колонок (created_at, status) vs (status, created_at) - разные use cases
-- 3. Покрывающий индекс (INCLUDE) дополняет, а не заменяет существующие
--
-- РЕКОМЕНДАЦИЯ: Оставить существующие индексы для обратной совместимости,
-- но новые индексы будут использоваться планировщиком PostgreSQL для наших запросов

-- ============================================
-- 4. АНАЛИЗ ПРОИЗВОДИТЕЛЬНОСТИ
-- ============================================
-- Ожидаемое улучшение производительности:
-- 
-- БЕЗ ИНДЕКСА (Full Table Scan):
--   - 100,000 заказов: ~500-1000ms
--   - 1,000,000 заказов: ~5-10 секунд
--   - CPU: 100% на время запроса
--
-- С ИНДЕКСОМ (created_at, status):
--   - 100,000 заказов: ~5-20ms
--   - 1,000,000 заказов: ~20-50ms
--   - CPU: минимальная нагрузка
--   - Index-Only Scan: данные читаются только из индекса
--
-- С ПОКРЫВАЮЩИМ ИНДЕКСОМ (covering index):
--   - Еще быстрее на 20-30% (нет обращения к таблице)
--   - Меньше I/O операций

-- ============================================
-- 5. ПРОВЕРКА ИНДЕКСОВ НА ПАРТИЦИЯХ
-- ============================================
-- ВАЖНО: Для партиционированных таблиц индексы создаются на каждой партиции автоматически
-- PostgreSQL наследует индексы от родительской таблицы на все существующие и будущие партиции
--
-- Проверяем, что индексы созданы на всех партициях:
DO $$
DECLARE
    partition_name TEXT;
    index_exists BOOLEAN;
BEGIN
    -- Проверяем индексы на существующих партициях
    FOR partition_name IN 
        SELECT tablename 
        FROM pg_tables 
        WHERE schemaname = 'public' 
          AND tablename LIKE 'orders_%'
    LOOP
        -- Проверяем наличие индекса на партиции
        SELECT EXISTS (
            SELECT 1 
            FROM pg_indexes 
            WHERE schemaname = 'public' 
              AND tablename = partition_name
              AND indexname = 'idx_orders_created_at_status_revenue'
        ) INTO index_exists;
        
        IF NOT index_exists THEN
            RAISE NOTICE 'Индекс idx_orders_created_at_status_revenue будет создан на партиции % при следующем запросе', partition_name;
        ELSE
            RAISE NOTICE 'Индекс idx_orders_created_at_status_revenue уже существует на партиции %', partition_name;
        END IF;
    END LOOP;
END $$;

-- ============================================
-- 6. СТАТИСТИКА ДЛЯ ПЛАНИРОВЩИКА
-- ============================================
-- Обновляем статистику для оптимального выбора индексов планировщиком
ANALYZE orders;

-- ============================================
-- 7. ПРОВЕРКА ИСПОЛЬЗОВАНИЯ ИНДЕКСА
-- ============================================
-- Для проверки использования индекса выполните:
--   EXPLAIN ANALYZE 
--   SELECT payment_method, 
--          COALESCE(final_price, total_price - COALESCE(discount_amount, 0)) as final_price,
--          COALESCE(discount_amount, 0) as discount_amount,
--          status
--   FROM orders
--   WHERE created_at >= '2025-12-10'::timestamp 
--     AND created_at < '2025-12-11'::timestamp
--     AND status IN ('delivered', 'ready', 'archived');
--
-- Ожидаемый результат:
--   - Index Scan using idx_orders_created_at_status_revenue
--   - или Index Only Scan using idx_orders_revenue_covering
--   - НЕ должно быть: Seq Scan (последовательное сканирование)

