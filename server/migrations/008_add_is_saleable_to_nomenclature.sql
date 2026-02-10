-- Миграция: Добавление поля is_saleable в nomenclature_items
-- Это поле определяет, может ли товар быть продан (отображаться в меню "Make Order")

-- Добавляем колонку is_saleable
DO $$ 
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns 
        WHERE table_name = 'nomenclature_items' 
        AND column_name = 'is_saleable'
    ) THEN
        ALTER TABLE nomenclature_items 
        ADD COLUMN is_saleable BOOLEAN NOT NULL DEFAULT false;
        
        -- Создаем индекс для быстрого поиска товаров для продажи
        CREATE INDEX IF NOT EXISTS idx_nomenclature_items_is_saleable 
        ON nomenclature_items(is_saleable) 
        WHERE is_saleable = true AND is_active = true AND deleted_at IS NULL;
        
        RAISE NOTICE 'Колонка is_saleable добавлена в nomenclature_items';
    ELSE
        RAISE NOTICE 'Колонка is_saleable уже существует';
    END IF;
END $$;

-- Комментарий к колонке
COMMENT ON COLUMN nomenclature_items.is_saleable IS 
'Флаг определяет, может ли товар быть продан (отображаться в меню "Make Order"). 
true = товар для продажи (пиццы, напитки, десерты), 
false = сырье/полуфабрикаты (мука, соус, тесто)';









