-- Миграция 024: Создание таблицы для планов слотов (delivery_plan и pickup_plan)
-- Планы слотов сохраняются в PostgreSQL для персистентности
-- Redis используется как кэш для быстрого доступа

CREATE TABLE IF NOT EXISTS slot_plans (
    slot_id VARCHAR(255) PRIMARY KEY,
    delivery_plan INTEGER NOT NULL DEFAULT 0,
    pickup_plan INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Индекс для быстрого поиска
CREATE INDEX IF NOT EXISTS idx_slot_plans_slot_id ON slot_plans(slot_id);

-- Комментарии к таблице
COMMENT ON TABLE slot_plans IS 'Планы слотов (delivery_plan и pickup_plan) для персистентного хранения';
COMMENT ON COLUMN slot_plans.slot_id IS 'ID слота (например, slot:1770508800)';
COMMENT ON COLUMN slot_plans.delivery_plan IS 'План для доставки в рублях';
COMMENT ON COLUMN slot_plans.pickup_plan IS 'План для самовывоза в рублях';

