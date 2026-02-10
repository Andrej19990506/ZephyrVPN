-- Миграция для исправления внешнего ключа recipe_exams.staff_id
-- Проблема: после рефакторинга staff.id больше не существует, теперь primary key - staff.user_id (UUID)

-- Шаг 1: Удаляем старый внешний ключ, если он существует
DO $$
BEGIN
    -- Проверяем, существует ли старый внешний ключ
    IF EXISTS (
        SELECT 1 
        FROM pg_constraint 
        WHERE conname = 'recipe_exams_staff_id_fkey' 
        AND conrelid = 'recipe_exams'::regclass
    ) THEN
        ALTER TABLE recipe_exams
        DROP CONSTRAINT recipe_exams_staff_id_fkey;
        
        RAISE NOTICE '✅ Старый внешний ключ recipe_exams_staff_id_fkey удален';
    ELSIF EXISTS (
        SELECT 1 
        FROM pg_constraint 
        WHERE conname LIKE '%recipe_exams%staff%' 
        AND conrelid = 'recipe_exams'::regclass
    ) THEN
        -- Удаляем любой внешний ключ, связанный со staff
        EXECUTE (
            SELECT 'ALTER TABLE recipe_exams DROP CONSTRAINT ' || conname
            FROM pg_constraint 
            WHERE conname LIKE '%recipe_exams%staff%' 
            AND conrelid = 'recipe_exams'::regclass
            LIMIT 1
        );
        
        RAISE NOTICE '✅ Старый внешний ключ на staff удален';
    ELSE
        RAISE NOTICE 'Старый внешний ключ не найден (возможно, уже удален)';
    END IF;
END $$;

-- Шаг 2: Проверяем структуру таблицы staff
DO $$
DECLARE
    staff_pk_column TEXT;
    staff_pk_type TEXT;
BEGIN
    -- Определяем primary key колонку в staff
    SELECT 
        a.attname,
        pg_catalog.format_type(a.atttypid, a.atttypmod)
    INTO 
        staff_pk_column,
        staff_pk_type
    FROM 
        pg_index i
        JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = ANY(i.indkey)
    WHERE 
        i.indrelid = 'staff'::regclass
        AND i.indisprimary
    LIMIT 1;
    
    IF staff_pk_column IS NULL THEN
        RAISE EXCEPTION 'Не удалось определить primary key колонку в таблице staff';
    END IF;
    
    RAISE NOTICE 'Primary key колонка в staff: % (тип: %)', staff_pk_column, staff_pk_type;
    
    -- Проверяем, что это UUID
    IF staff_pk_type NOT LIKE '%uuid%' THEN
        RAISE WARNING 'Primary key в staff имеет тип %, ожидался UUID. Возможно, нужна дополнительная миграция.', staff_pk_type;
    END IF;
END $$;

-- Шаг 3: Проверяем, что staff_id в recipe_exams имеет правильный тип
DO $$
BEGIN
    -- Проверяем тип колонки staff_id
    IF EXISTS (
        SELECT 1 
        FROM information_schema.columns 
        WHERE table_name = 'recipe_exams' 
        AND column_name = 'staff_id'
        AND data_type != 'uuid'
    ) THEN
        -- Если тип не UUID, изменяем его
        ALTER TABLE recipe_exams
        ALTER COLUMN staff_id TYPE UUID USING staff_id::uuid;
        
        RAISE NOTICE '✅ Тип колонки staff_id изменен на UUID';
    ELSE
        RAISE NOTICE 'Тип колонки staff_id уже UUID';
    END IF;
END $$;

-- Шаг 4: Проверяем, что все staff_id в recipe_exams ссылаются на существующие user_id в staff
DO $$
DECLARE
    orphan_count INTEGER;
BEGIN
    SELECT COUNT(*) INTO orphan_count
    FROM recipe_exams re
    LEFT JOIN staff s ON re.staff_id::text = s.user_id::text
    WHERE s.user_id IS NULL;
    
    IF orphan_count > 0 THEN
        RAISE WARNING 'Найдено % записей recipe_exams с несуществующими staff_id. Они будут удалены.', orphan_count;
        
        -- Удаляем orphan записи
        DELETE FROM recipe_exams re
        WHERE NOT EXISTS (
            SELECT 1 FROM staff s WHERE s.user_id::text = re.staff_id::text
        );
        
        RAISE NOTICE '✅ Удалено % orphan записей', orphan_count;
    ELSE
        RAISE NOTICE '✅ Все staff_id в recipe_exams существуют в staff';
    END IF;
END $$;

-- Шаг 5: Создаем уникальный индекс на staff.user_id (если его еще нет)
DO $$
BEGIN
    -- Проверяем, существует ли уникальный индекс на user_id
    IF NOT EXISTS (
        SELECT 1 
        FROM pg_indexes 
        WHERE tablename = 'staff' 
        AND indexname LIKE '%user_id%unique%'
    ) THEN
        -- Создаем уникальный индекс на user_id
        CREATE UNIQUE INDEX IF NOT EXISTS idx_staff_user_id_unique ON staff(user_id);
        
        RAISE NOTICE '✅ Уникальный индекс на staff.user_id создан';
    ELSE
        RAISE NOTICE 'Уникальный индекс на staff.user_id уже существует';
    END IF;
END $$;

-- Шаг 6: Обновляем staff_id в recipe_exams, чтобы они ссылались на user_id из staff
DO $$
DECLARE
    updated_count INTEGER;
BEGIN
    -- Обновляем staff_id в recipe_exams, заменяя старые id на user_id
    UPDATE recipe_exams re
    SET staff_id = s.user_id::text::uuid
    FROM staff s
    WHERE re.staff_id::text = s.id::text
    AND s.user_id IS NOT NULL
    AND re.staff_id::text != s.user_id::text;
    
    GET DIAGNOSTICS updated_count = ROW_COUNT;
    
    IF updated_count > 0 THEN
        RAISE NOTICE '✅ Обновлено % записей recipe_exams: staff_id теперь ссылается на user_id', updated_count;
    ELSE
        RAISE NOTICE 'Все staff_id в recipe_exams уже ссылаются на user_id';
    END IF;
END $$;

-- Шаг 7: Создаем новый внешний ключ на staff.user_id
DO $$
BEGIN
    -- Проверяем, существует ли уже правильный внешний ключ
    IF NOT EXISTS (
        SELECT 1 
        FROM pg_constraint 
        WHERE conname = 'fk_recipe_exams_staff' 
        AND conrelid = 'recipe_exams'::regclass
    ) THEN
        -- Создаем внешний ключ на staff.user_id
        ALTER TABLE recipe_exams
        ADD CONSTRAINT fk_recipe_exams_staff 
        FOREIGN KEY (staff_id) 
        REFERENCES staff(user_id) 
        ON DELETE CASCADE;
        
        RAISE NOTICE '✅ Внешний ключ fk_recipe_exams_staff создан (ссылается на staff.user_id)';
    ELSE
        RAISE NOTICE 'Внешний ключ fk_recipe_exams_staff уже существует';
    END IF;
END $$;

-- Комментарий для документации
COMMENT ON CONSTRAINT fk_recipe_exams_staff ON recipe_exams IS 'Связь с таблицей staff через user_id (UUID). Если Staff удален, RecipeExam также удаляется (CASCADE).';

