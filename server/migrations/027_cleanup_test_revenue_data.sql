-- Скрипт для очистки тестовых данных о продажах
-- Удаляет все заказы с display_id, начинающимся с 'TEST-'

-- Проверяем количество тестовых заказов перед удалением
DO $$
DECLARE
    test_orders_count BIGINT;
BEGIN
    SELECT COUNT(*) INTO test_orders_count
    FROM orders
    WHERE display_id LIKE 'TEST-%';
    
    RAISE NOTICE 'Найдено тестовых заказов для удаления: %', test_orders_count;
    
    IF test_orders_count > 0 THEN
        DELETE FROM orders
        WHERE display_id LIKE 'TEST-%';
        
        RAISE NOTICE 'Удалено % тестовых заказов', test_orders_count;
    ELSE
        RAISE NOTICE 'Тестовые заказы не найдены';
    END IF;
END $$;

