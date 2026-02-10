-- Миграция: Создание таблицы правил конвертации единиц измерения

CREATE TABLE IF NOT EXISTS uom_conversion_rules (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL,
    description TEXT,
    input_uom VARCHAR(255) NOT NULL,
    base_unit VARCHAR(50) NOT NULL,
    multiplier DECIMAL(10,4) NOT NULL,
    is_active BOOLEAN DEFAULT true,
    is_default BOOLEAN DEFAULT false,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP
);

-- Создаем индексы
CREATE INDEX IF NOT EXISTS idx_uom_conversion_rules_is_active ON uom_conversion_rules(is_active) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_uom_conversion_rules_is_default ON uom_conversion_rules(is_default) WHERE deleted_at IS NULL AND is_active = true;

-- Добавляем колонку uom_rule_id в supplier_catalog_items
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'supplier_catalog_items' AND column_name = 'uom_rule_id') THEN
        ALTER TABLE supplier_catalog_items
        ADD COLUMN uom_rule_id UUID;
        
        CREATE INDEX IF NOT EXISTS idx_supplier_catalog_items_uom_rule_id ON supplier_catalog_items(uom_rule_id);
        
        -- Добавляем внешний ключ
        ALTER TABLE supplier_catalog_items
        ADD CONSTRAINT fk_supplier_catalog_items_uom_rule
        FOREIGN KEY (uom_rule_id) REFERENCES uom_conversion_rules(id) ON DELETE SET NULL;
        
        RAISE NOTICE 'Колонка uom_rule_id добавлена в supplier_catalog_items';
    ELSE
        RAISE NOTICE 'Колонка uom_rule_id уже существует в supplier_catalog_items';
    END IF;
END $$;

-- Вставляем стандартные правила конвертации
INSERT INTO uom_conversion_rules (id, name, description, input_uom, base_unit, multiplier, is_active, is_default)
VALUES
    (gen_random_uuid(), 'Килограмм', 'Стандартная конвертация: 1 кг = 1000 г', 'кг', 'g', 1000.0, true, true),
    (gen_random_uuid(), 'Литр', 'Стандартная конвертация: 1 л = 1000 мл', 'л', 'ml', 1000.0, true, true),
    (gen_random_uuid(), 'Упак (6 шт х 2 л)', 'Упаковка из 6 штук по 2 литра', 'упак (6 шт х 2 л)', 'pcs', 6.0, true, false),
    (gen_random_uuid(), 'Упаковка', 'Стандартная упаковка', 'упаковка', 'pcs', 1.0, true, false),
    (gen_random_uuid(), 'Штука', 'Стандартная единица: 1 шт = 1 шт', 'шт', 'pcs', 1.0, true, false)
ON CONFLICT DO NOTHING;


