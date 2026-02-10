-- Миграция: Добавление полей для единиц измерения поставщика в supplier_catalog_items
-- Цель: Поддержка сложных единиц измерения (например, "упак (6 шт х 2 л)") и множителя конвертации

-- ============================================
-- Добавление полей в supplier_catalog_items
-- ============================================

-- Проверяем существование таблицы
DO $$
BEGIN
    IF EXISTS (SELECT FROM information_schema.tables WHERE table_name = 'supplier_catalog_items') THEN
        
        -- Добавляем поле InputUOM (текстовое описание единицы измерения поставщика)
        IF NOT EXISTS (
            SELECT 1 FROM information_schema.columns 
            WHERE table_name = 'supplier_catalog_items' 
            AND column_name = 'input_uom'
        ) THEN
            ALTER TABLE supplier_catalog_items 
            ADD COLUMN input_uom VARCHAR(255);
            
            -- Копируем данные из input_unit в input_uom для существующих записей
            UPDATE supplier_catalog_items 
            SET input_uom = input_unit 
            WHERE input_uom IS NULL AND input_unit IS NOT NULL;
            
            RAISE NOTICE 'Колонка input_uom добавлена в supplier_catalog_items';
        ELSE
            RAISE NOTICE 'Колонка input_uom уже существует';
        END IF;
        
        -- Добавляем поле ConversionMultiplier (множитель конвертации к базовой единице)
        IF NOT EXISTS (
            SELECT 1 FROM information_schema.columns 
            WHERE table_name = 'supplier_catalog_items' 
            AND column_name = 'conversion_multiplier'
        ) THEN
            ALTER TABLE supplier_catalog_items 
            ADD COLUMN conversion_multiplier DECIMAL(10,4) NOT NULL DEFAULT 1.0 
            CHECK (conversion_multiplier > 0);
            
            RAISE NOTICE 'Колонка conversion_multiplier добавлена в supplier_catalog_items';
        ELSE
            RAISE NOTICE 'Колонка conversion_multiplier уже существует';
        END IF;
        
    ELSE
        RAISE NOTICE 'Таблица supplier_catalog_items не существует, пропускаем миграцию';
    END IF;
END $$;

-- Комментарии к полям
COMMENT ON COLUMN supplier_catalog_items.input_uom IS 'Единица измерения поставщика (текст, например: "упак (6 шт х 2 л)")';
COMMENT ON COLUMN supplier_catalog_items.conversion_multiplier IS 'Множитель конвертации к базовой единице склада (например, 6 для упаковки из 6 штук)';


