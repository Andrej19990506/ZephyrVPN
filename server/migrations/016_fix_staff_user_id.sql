-- Миграция для исправления user_id в таблице staff
-- Проблема: после рефакторинга user_id стал обязательным, но в БД есть записи с NULL

-- Шаг 1: Проверяем, существует ли колонка user_id
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 
        FROM information_schema.columns 
        WHERE table_name = 'staff' 
        AND column_name = 'user_id'
    ) THEN
        -- Если колонки нет, создаем её
        ALTER TABLE staff 
        ADD COLUMN user_id UUID;
        
        RAISE NOTICE '✅ Колонка user_id добавлена в таблицу staff';
    ELSE
        RAISE NOTICE 'Колонка user_id уже существует';
    END IF;
END $$;

-- Шаг 2: Создаем User записи для всех Staff без user_id
DO $$
DECLARE
    staff_ctid TID;
    new_user_id UUID;
    staff_count INTEGER;
    counter INTEGER := 0;
BEGIN
    -- Подсчитываем количество staff без user_id
    SELECT COUNT(*) INTO staff_count 
    FROM staff 
    WHERE user_id IS NULL;
    
    IF staff_count > 0 THEN
        RAISE NOTICE 'Найдено % записей staff без user_id. Создаем User записи...', staff_count;
        
        -- Проходим по всем staff без user_id используя курсор
        FOR staff_ctid IN 
            SELECT ctid FROM staff WHERE user_id IS NULL
        LOOP
            -- Генерируем новый UUID для user_id
            new_user_id := gen_random_uuid();
            counter := counter + 1;
            
            -- Создаем User запись
            -- Используем дефолтные значения, так как в старом staff нет имени/телефона
            INSERT INTO users (
                id,
                phone,
                role,
                status,
                created_at,
                updated_at
            ) VALUES (
                new_user_id,
                'staff_' || EXTRACT(EPOCH FROM NOW())::BIGINT || '_' || counter, -- Временный уникальный телефон
                'kitchen_staff', -- Дефолтная роль
                'active',
                NOW(),
                NOW()
            )
            ON CONFLICT (id) DO NOTHING; -- Если уже существует, пропускаем
            
            -- Обновляем staff.user_id используя ctid (физический идентификатор строки)
            UPDATE staff 
            SET user_id = new_user_id
            WHERE ctid = staff_ctid;
            
            RAISE NOTICE 'Создан User % для Staff (ctid=%, номер %)', new_user_id, staff_ctid, counter;
        END LOOP;
        
        RAISE NOTICE '✅ Все % Staff записи обновлены', counter;
    ELSE
        RAISE NOTICE '✅ Все Staff записи уже имеют user_id';
    END IF;
END $$;

-- Шаг 2: Проверяем, что все staff имеют user_id
DO $$
DECLARE
    null_count INTEGER;
BEGIN
    SELECT COUNT(*) INTO null_count 
    FROM staff 
    WHERE user_id IS NULL;
    
    IF null_count > 0 THEN
        RAISE EXCEPTION 'Осталось % записей staff с NULL user_id. Миграция не может быть завершена.', null_count;
    ELSE
        RAISE NOTICE '✅ Все Staff записи имеют user_id';
    END IF;
END $$;

-- Шаг 3: Теперь можно безопасно добавить NOT NULL constraint
-- Но сначала нужно убедиться, что все user_id существуют в таблице users
DO $$
DECLARE
    orphan_count INTEGER;
BEGIN
    SELECT COUNT(*) INTO orphan_count
    FROM staff s
    LEFT JOIN users u ON s.user_id = u.id
    WHERE s.user_id IS NOT NULL AND u.id IS NULL;
    
    IF orphan_count > 0 THEN
        RAISE EXCEPTION 'Найдено % записей staff с несуществующими user_id. Исправьте перед применением NOT NULL constraint.', orphan_count;
    ELSE
        RAISE NOTICE '✅ Все user_id в staff существуют в таблице users';
    END IF;
END $$;

-- Шаг 4: Добавляем NOT NULL constraint (если его еще нет)
-- Проверяем, есть ли уже constraint
DO $$
BEGIN
    -- Проверяем, является ли колонка уже NOT NULL
    IF EXISTS (
        SELECT 1 
        FROM information_schema.columns 
        WHERE table_name = 'staff' 
        AND column_name = 'user_id' 
        AND is_nullable = 'YES'
    ) THEN
        -- Пытаемся добавить NOT NULL constraint
        ALTER TABLE staff 
        ALTER COLUMN user_id SET NOT NULL;
        
        RAISE NOTICE '✅ NOT NULL constraint добавлен к staff.user_id';
    ELSE
        RAISE NOTICE 'NOT NULL constraint уже существует для staff.user_id';
    END IF;
EXCEPTION
    WHEN others THEN
        RAISE NOTICE 'Ошибка при добавлении NOT NULL constraint: %', SQLERRM;
        RAISE;
END $$;

-- Шаг 5: Добавляем внешний ключ (если его еще нет)
DO $$
BEGIN
    -- Проверяем, существует ли уже внешний ключ
    IF NOT EXISTS (
        SELECT 1 
        FROM pg_constraint 
        WHERE conname = 'fk_staff_user' 
        AND conrelid = 'staff'::regclass
    ) THEN
        ALTER TABLE staff
        ADD CONSTRAINT fk_staff_user 
        FOREIGN KEY (user_id) 
        REFERENCES users(id) 
        ON DELETE CASCADE;
        
        RAISE NOTICE '✅ Внешний ключ fk_staff_user добавлен';
    ELSE
        RAISE NOTICE 'Внешний ключ fk_staff_user уже существует';
    END IF;
END $$;

-- Комментарий для документации
COMMENT ON COLUMN staff.user_id IS 'Связь с таблицей users (обязательное поле). Если User удален, Staff также удаляется (CASCADE).';

