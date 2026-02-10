-- Миграция: Добавление связи допов с номенклатурой
-- Проблема: допы не связаны с номенклатурой, поэтому невозможно автоматически списывать ингредиенты

-- Шаг 1: Добавляем поле nomenclature_id в таблицу extras
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 
        FROM information_schema.columns 
        WHERE table_name = 'extras' 
        AND column_name = 'nomenclature_id'
    ) THEN
        ALTER TABLE extras 
        ADD COLUMN nomenclature_id UUID;
        
        RAISE NOTICE '✅ Колонка nomenclature_id добавлена в таблицу extras';
    ELSE
        RAISE NOTICE 'Колонка nomenclature_id уже существует';
    END IF;
END $$;

-- Шаг 2: Добавляем индекс для быстрого поиска
CREATE INDEX IF NOT EXISTS idx_extras_nomenclature_id ON extras(nomenclature_id) WHERE nomenclature_id IS NOT NULL;

-- Шаг 3: Добавляем внешний ключ на nomenclature_items
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 
        FROM pg_constraint 
        WHERE conname = 'fk_extras_nomenclature' 
        AND conrelid = 'extras'::regclass
    ) THEN
        ALTER TABLE extras
        ADD CONSTRAINT fk_extras_nomenclature 
        FOREIGN KEY (nomenclature_id) 
        REFERENCES nomenclature_items(id) 
        ON DELETE SET NULL; -- Если номенклатура удалена, доп остается, но без связи
        
        RAISE NOTICE '✅ Внешний ключ fk_extras_nomenclature создан';
    ELSE
        RAISE NOTICE 'Внешний ключ fk_extras_nomenclature уже существует';
    END IF;
END $$;

-- Шаг 4: Добавляем поле recipe_id для допов, которые имеют сложный состав (BOM)
-- Например, "Сырный борт" может состоять из нескольких ингредиентов
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 
        FROM information_schema.columns 
        WHERE table_name = 'extras' 
        AND column_name = 'recipe_id'
    ) THEN
        ALTER TABLE extras 
        ADD COLUMN recipe_id UUID;
        
        RAISE NOTICE '✅ Колонка recipe_id добавлена в таблицу extras';
    ELSE
        RAISE NOTICE 'Колонка recipe_id уже существует';
    END IF;
END $$;

-- Шаг 5: Добавляем внешний ключ на recipes (если доп имеет сложный состав)
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 
        FROM pg_constraint 
        WHERE conname = 'fk_extras_recipe' 
        AND conrelid = 'extras'::regclass
    ) THEN
        ALTER TABLE extras
        ADD CONSTRAINT fk_extras_recipe 
        FOREIGN KEY (recipe_id) 
        REFERENCES recipes(id) 
        ON DELETE SET NULL; -- Если рецепт удален, доп остается, но без рецепта
        
        RAISE NOTICE '✅ Внешний ключ fk_extras_recipe создан';
    ELSE
        RAISE NOTICE 'Внешний ключ fk_extras_recipe уже существует';
    END IF;
END $$;

-- Комментарии для документации
COMMENT ON COLUMN extras.nomenclature_id IS 'Связь с номенклатурой для простых допов (например, "Доп. сыр" -> "Сыр моцарелла"). Используется для прямого списания ингредиента.';
COMMENT ON COLUMN extras.recipe_id IS 'Связь с рецептом для сложных допов (например, "Сырный борт" состоит из нескольких ингредиентов). Используется для списания через BOM (Bill of Materials).';

-- Логика использования:
-- 1. Если доп простой (только один ингредиент) -> используем nomenclature_id
-- 2. Если доп сложный (несколько ингредиентов) -> используем recipe_id
-- 3. При списании:
--    - Если есть recipe_id -> списываем через Recipe -> RecipeIngredient
--    - Если нет recipe_id, но есть nomenclature_id -> списываем напрямую nomenclature_id
--    - Если нет ни того, ни другого -> доп не списывается (только цена)



