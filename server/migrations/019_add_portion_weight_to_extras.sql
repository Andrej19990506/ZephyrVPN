-- Миграция: Добавление веса порции допа для точного расчета списания инвентаря
-- Best Practice: Каждый доп должен иметь точный вес порции для корректного списания

-- Шаг 1: Добавляем поле portion_weight_grams в таблицу extras
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 
        FROM information_schema.columns 
        WHERE table_name = 'extras' 
        AND column_name = 'portion_weight_grams'
    ) THEN
        ALTER TABLE extras 
        ADD COLUMN portion_weight_grams INTEGER DEFAULT 50; -- Значение по умолчанию 50г (стандартная порция допа)
        
        RAISE NOTICE '✅ Колонка portion_weight_grams добавлена в таблицу extras';
    ELSE
        RAISE NOTICE 'Колонка portion_weight_grams уже существует';
    END IF;
END $$;

-- Шаг 2: Добавляем комментарий для документации
COMMENT ON COLUMN extras.portion_weight_grams IS 'Вес порции допа в граммах. Используется для точного расчета списания инвентаря при заказе. Например: "Доп. сыр" = 50г, "Двойной сыр" = 100г.';

-- Шаг 3: Обновляем существующие допы с номенклатурой - устанавливаем вес на основе unit_weight номенклатуры
-- Если доп связан с номенклатурой, используем unit_weight как базовое значение
DO $$
BEGIN
    -- Проверяем, существует ли колонка nomenclature_id
    IF EXISTS (
        SELECT 1 
        FROM information_schema.columns 
        WHERE table_name = 'extras' 
        AND column_name = 'nomenclature_id'
    ) THEN
        UPDATE extras e
        SET portion_weight_grams = COALESCE(
            (SELECT n.unit_weight::INTEGER 
             FROM nomenclature_items n 
             WHERE n.id::text = e.nomenclature_id::text AND n.unit_weight > 0),
            50 -- Fallback: 50г если unit_weight не указан
        )
        WHERE e.nomenclature_id IS NOT NULL 
          AND (e.portion_weight_grams IS NULL OR e.portion_weight_grams = 0);
        
        RAISE NOTICE '✅ Обновлены существующие допы с номенклатурой';
    ELSE
        RAISE NOTICE '⚠️ Колонка nomenclature_id не существует, пропускаем обновление';
    END IF;
END $$;

-- Шаг 4: Добавляем CHECK constraint для валидации (вес должен быть положительным)
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 
        FROM pg_constraint 
        WHERE conname = 'chk_extras_portion_weight_positive' 
        AND conrelid = 'extras'::regclass
    ) THEN
        ALTER TABLE extras
        ADD CONSTRAINT chk_extras_portion_weight_positive 
        CHECK (portion_weight_grams > 0);
        
        RAISE NOTICE '✅ Constraint chk_extras_portion_weight_positive создан';
    ELSE
        RAISE NOTICE 'Constraint chk_extras_portion_weight_positive уже существует';
    END IF;
END $$;

