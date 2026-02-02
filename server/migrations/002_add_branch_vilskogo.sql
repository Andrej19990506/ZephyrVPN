-- Миграция: Добавление филиала "Вильского 34"
-- Создает филиал и связывает его с дефолтным ИП "Юсупов"

-- Получаем ID дефолтного ИП "Юсупов"
DO $$
DECLARE
    yusupov_legal_entity_id UUID;
    branch_id UUID := gen_random_uuid();
BEGIN
    -- Находим ИП "Юсупов"
    SELECT id INTO yusupov_legal_entity_id
    FROM legal_entities
    WHERE name = 'Юсупов' AND deleted_at IS NULL
    LIMIT 1;

    -- Если ИП не найден, создаем его (только если нет другого ИП с пустым INN)
    IF yusupov_legal_entity_id IS NULL THEN
        -- Пытаемся найти любой ИП с пустым INN
        SELECT id INTO yusupov_legal_entity_id
        FROM legal_entities
        WHERE (inn = '' OR inn IS NULL) AND deleted_at IS NULL
        LIMIT 1;
        
        -- Если не нашли, создаем новый
        IF yusupov_legal_entity_id IS NULL THEN
            INSERT INTO legal_entities (id, name, inn, type, is_active, created_at, updated_at)
            VALUES (
                gen_random_uuid(),
                'Юсупов',
                NULL, -- Используем NULL вместо пустой строки для уникального индекса
                'IP',
                true,
                NOW(),
                NOW()
            )
            RETURNING id INTO yusupov_legal_entity_id;
            
            RAISE NOTICE 'Создано ИП "Юсупов" с ID: %', yusupov_legal_entity_id;
        ELSE
            RAISE NOTICE 'Используется существующее ИП с ID: %', yusupov_legal_entity_id;
        END IF;
    ELSE
        RAISE NOTICE 'Используется существующее ИП "Юсупов" с ID: %', yusupov_legal_entity_id;
    END IF;

    -- Проверяем, не существует ли уже филиал "Вильского 34"
    IF NOT EXISTS (
        SELECT 1 FROM branches 
        WHERE name = 'Вильского 34' 
        AND deleted_at IS NULL
    ) THEN
        -- Создаем филиал "Вильского 34"
        INSERT INTO branches (
            id,
            name,
            address,
            phone,
            email,
            legal_entity_id,
            super_admin_id,
            is_active,
            created_at,
            updated_at
        ) VALUES (
            branch_id,
            'Вильского 34',
            'Вильского, 34',
            '',
            '',
            yusupov_legal_entity_id,
            NULL, -- SuperAdminID опционален
            true,
            NOW(),
            NOW()
        );
        
        RAISE NOTICE 'Создан филиал "Вильского 34" с ID: %', branch_id;
    ELSE
        RAISE NOTICE 'Филиал "Вильского 34" уже существует';
    END IF;
END $$;

