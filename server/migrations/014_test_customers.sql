-- Тестовый скрипт для проверки миграции 014_create_customers_tables.sql
-- Использование: psql -U pizza_admin -d pizza_db -f migrations/014_test_customers.sql

-- ============================================
-- ТЕСТ 1: Создание тестового пользователя
-- ============================================
DO $$
DECLARE
    test_user_id UUID;
    test_customer_id UUID;
    test_address_id UUID;
BEGIN
    RAISE NOTICE '=== ТЕСТ 1: Создание тестового пользователя ===';
    
    -- Вставляем пользователя
    INSERT INTO users (phone, email, role, status) 
    VALUES ('+79991234567', 'test@example.com', 'customer', 'active')
    RETURNING id INTO test_user_id;
    
    RAISE NOTICE '✅ Пользователь создан: %', test_user_id;
    
    -- Вставляем данные клиента
    INSERT INTO customers (user_id, first_name, last_name, loyalty_points, total_orders) 
    VALUES (test_user_id, 'Ivan', 'Petrov', 100, 5)
    RETURNING user_id INTO test_customer_id;
    
    RAISE NOTICE '✅ Данные клиента созданы: %', test_customer_id;
    
    -- Вставляем адрес
    INSERT INTO customer_addresses (customer_id, type, address, is_default) 
    VALUES (
        test_customer_id, 
        'home', 
        'Moscow, Red Square, 1',
        true
    )
    RETURNING id INTO test_address_id;
    
    RAISE NOTICE '✅ Адрес создан: %', test_address_id;
    
    -- Проверяем, что данные созданы
    IF EXISTS (SELECT 1 FROM users WHERE id = test_user_id) THEN
        RAISE NOTICE '✅ ТЕСТ 1 ПРОЙДЕН: Пользователь существует';
    ELSE
        RAISE EXCEPTION '❌ ТЕСТ 1 ПРОВАЛЕН: Пользователь не найден';
    END IF;
    
    IF EXISTS (SELECT 1 FROM customers WHERE user_id = test_customer_id) THEN
        RAISE NOTICE '✅ ТЕСТ 1 ПРОЙДЕН: Данные клиента существуют';
    ELSE
        RAISE EXCEPTION '❌ ТЕСТ 1 ПРОВАЛЕН: Данные клиента не найдены';
    END IF;
    
    IF EXISTS (SELECT 1 FROM customer_addresses WHERE id = test_address_id) THEN
        RAISE NOTICE '✅ ТЕСТ 1 ПРОЙДЕН: Адрес существует';
    ELSE
        RAISE EXCEPTION '❌ ТЕСТ 1 ПРОВАЛЕН: Адрес не найден';
    END IF;
    
    -- Сохраняем ID для следующего теста
    PERFORM set_config('test.user_id', test_user_id::text, false);
END $$;

-- ============================================
-- ТЕСТ 2: Проверка каскадного удаления
-- ============================================
DO $$
DECLARE
    test_user_id UUID;
    customer_exists BOOLEAN;
    address_exists BOOLEAN;
BEGIN
    RAISE NOTICE '=== ТЕСТ 2: Проверка каскадного удаления ===';
    
    -- Получаем ID тестового пользователя
    test_user_id := current_setting('test.user_id')::UUID;
    
    -- Удаляем пользователя
    DELETE FROM users WHERE id = test_user_id;
    
    -- Проверяем, что каскадное удаление сработало
    SELECT EXISTS(SELECT 1 FROM customers WHERE user_id = test_user_id) INTO customer_exists;
    SELECT EXISTS(SELECT 1 FROM customer_addresses WHERE customer_id = test_user_id) INTO address_exists;
    
    IF customer_exists THEN
        RAISE EXCEPTION '❌ ТЕСТ 2 ПРОВАЛЕН: Данные клиента не удалены каскадно';
    ELSE
        RAISE NOTICE '✅ ТЕСТ 2 ПРОЙДЕН: Данные клиента удалены каскадно';
    END IF;
    
    IF address_exists THEN
        RAISE EXCEPTION '❌ ТЕСТ 2 ПРОВАЛЕН: Адреса не удалены каскадно';
    ELSE
        RAISE NOTICE '✅ ТЕСТ 2 ПРОЙДЕН: Адреса удалены каскадно';
    END IF;
END $$;

-- ============================================
-- ТЕСТ 3: Проверка уникальности телефона
-- ============================================
DO $$
BEGIN
    RAISE NOTICE '=== ТЕСТ 3: Проверка уникальности телефона ===';
    
    -- Пытаемся создать пользователя с существующим телефоном
    BEGIN
        INSERT INTO users (phone, email, role) 
        VALUES ('+79991234567', 'another@example.com', 'customer');
        
        RAISE EXCEPTION '❌ ТЕСТ 3 ПРОВАЛЕН: Дубликат телефона не обнаружен';
    EXCEPTION
        WHEN unique_violation THEN
            RAISE NOTICE '✅ ТЕСТ 3 ПРОЙДЕН: Уникальность телефона работает';
    END;
END $$;

-- ============================================
-- ТЕСТ 4: Проверка единственного адреса по умолчанию
-- ============================================
DO $$
DECLARE
    test_user_id UUID;
    test_customer_id UUID;
    default_count INT;
BEGIN
    RAISE NOTICE '=== ТЕСТ 4: Проверка единственного адреса по умолчанию ===';
    
    -- Создаем тестового пользователя
    INSERT INTO users (phone, email, role) 
    VALUES ('+79998887766', 'test4@example.com', 'customer')
    RETURNING id INTO test_user_id;
    
    INSERT INTO customers (user_id, first_name) 
    VALUES (test_user_id, 'Test')
    RETURNING user_id INTO test_customer_id;
    
    -- Создаем первый адрес по умолчанию
    INSERT INTO customer_addresses (customer_id, type, address, is_default) 
    VALUES (test_customer_id, 'home', 'Address 1', true);
    
    -- Создаем второй адрес по умолчанию (должен снять default с первого)
    INSERT INTO customer_addresses (customer_id, type, address, is_default) 
    VALUES (test_customer_id, 'work', 'Address 2', true);
    
    -- Проверяем, что только один адрес имеет is_default = true
    SELECT COUNT(*) INTO default_count
    FROM customer_addresses
    WHERE customer_id = test_customer_id AND is_default = true;
    
    IF default_count = 1 THEN
        RAISE NOTICE '✅ ТЕСТ 4 ПРОЙДЕН: Только один адрес по умолчанию';
    ELSE
        RAISE EXCEPTION '❌ ТЕСТ 4 ПРОВАЛЕН: Найдено % адресов по умолчанию (ожидалось 1)', default_count;
    END IF;
    
    -- Очистка
    DELETE FROM users WHERE id = test_user_id;
END $$;

-- ============================================
-- ТЕСТ 5: Проверка индексов
-- ============================================
DO $$
DECLARE
    index_exists BOOLEAN;
BEGIN
    RAISE NOTICE '=== ТЕСТ 5: Проверка индексов ===';
    
    -- Проверяем индекс на email
    SELECT EXISTS (
        SELECT 1 FROM pg_indexes 
        WHERE tablename = 'users' AND indexname = 'idx_users_email'
    ) INTO index_exists;
    
    IF NOT index_exists THEN
        RAISE EXCEPTION '❌ ТЕСТ 5 ПРОВАЛЕН: Индекс idx_users_email не найден';
    END IF;
    
    -- Проверяем индекс на phone
    SELECT EXISTS (
        SELECT 1 FROM pg_indexes 
        WHERE tablename = 'users' AND indexname = 'idx_users_phone'
    ) INTO index_exists;
    
    IF NOT index_exists THEN
        RAISE EXCEPTION '❌ ТЕСТ 5 ПРОВАЛЕН: Индекс idx_users_phone не найден';
    END IF;
    
    -- Проверяем индекс на customer_addresses
    SELECT EXISTS (
        SELECT 1 FROM pg_indexes 
        WHERE tablename = 'customer_addresses' AND indexname = 'idx_customer_addresses_customer'
    ) INTO index_exists;
    
    IF NOT index_exists THEN
        RAISE EXCEPTION '❌ ТЕСТ 5 ПРОВАЛЕН: Индекс idx_customer_addresses_customer не найден';
    END IF;
    
    RAISE NOTICE '✅ ТЕСТ 5 ПРОЙДЕН: Все индексы созданы';
END $$;

-- ============================================
-- ТЕСТ 6: Проверка триггера updated_at
-- ============================================
DO $$
DECLARE
    test_user_id UUID;
    old_updated_at TIMESTAMP WITH TIME ZONE;
    new_updated_at TIMESTAMP WITH TIME ZONE;
BEGIN
    RAISE NOTICE '=== ТЕСТ 6: Проверка триггера updated_at ===';
    
    -- Создаем тестового пользователя
    INSERT INTO users (phone, email, role) 
    VALUES ('+79991112233', 'test6@example.com', 'customer')
    RETURNING id, updated_at INTO test_user_id, old_updated_at;
    
    -- Ждем немного, чтобы время изменилось
    PERFORM pg_sleep(1);
    
    -- Обновляем пользователя
    UPDATE users SET email = 'updated@example.com' WHERE id = test_user_id;
    
    -- Получаем новое время
    SELECT updated_at INTO new_updated_at FROM users WHERE id = test_user_id;
    
    IF new_updated_at > old_updated_at THEN
        RAISE NOTICE '✅ ТЕСТ 6 ПРОЙДЕН: Триггер updated_at работает';
    ELSE
        RAISE EXCEPTION '❌ ТЕСТ 6 ПРОВАЛЕН: Триггер updated_at не обновил время';
    END IF;
    
    -- Очистка
    DELETE FROM users WHERE id = test_user_id;
END $$;

RAISE NOTICE '========================================';
RAISE NOTICE '✅ ВСЕ ТЕСТЫ ЗАВЕРШЕНЫ';
RAISE NOTICE '========================================';



