-- Миграция 025: Создание таблицы для планов выручки
-- Планы выручки сохраняются в PostgreSQL для персистентности
-- Используется для хранения результатов прогнозирования выручки

CREATE TABLE IF NOT EXISTS revenue_plans (
    id SERIAL PRIMARY KEY,
    plan_date DATE NOT NULL UNIQUE, -- Дата, на которую создан план
    forecast_total DECIMAL(15, 2) NOT NULL, -- Прогнозируемая выручка на конец дня
    current_revenue DECIMAL(15, 2) NOT NULL DEFAULT 0, -- Текущая выручка на момент создания плана
    remaining_hours DECIMAL(5, 2) NOT NULL, -- Оставшиеся часы до закрытия
    average_hourly DECIMAL(15, 2) NOT NULL DEFAULT 0, -- Средняя выручка в час (на основе истории)
    current_hourly DECIMAL(15, 2) NOT NULL DEFAULT 0, -- Текущая выручка в час (сегодня)
    historical_avg DECIMAL(15, 2) NOT NULL DEFAULT 0, -- Средняя выручка за аналогичные дни недели
    confidence DECIMAL(5, 2) NOT NULL DEFAULT 0, -- Уверенность в прогнозе (0-100%)
    method VARCHAR(50) NOT NULL, -- Метод прогнозирования (linear_extrapolation, weighted_average, etc.)
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Индексы для быстрого поиска
CREATE INDEX IF NOT EXISTS idx_revenue_plans_plan_date ON revenue_plans(plan_date DESC);
CREATE INDEX IF NOT EXISTS idx_revenue_plans_created_at ON revenue_plans(created_at DESC);

-- Комментарии к таблице
COMMENT ON TABLE revenue_plans IS 'Планы выручки (результаты прогнозирования) для персистентного хранения';
COMMENT ON COLUMN revenue_plans.plan_date IS 'Дата, на которую создан план (UNIQUE - один план на день)';
COMMENT ON COLUMN revenue_plans.forecast_total IS 'Прогнозируемая выручка на конец дня в рублях';
COMMENT ON COLUMN revenue_plans.confidence IS 'Уверенность в прогнозе в процентах (0-100)';
COMMENT ON COLUMN revenue_plans.method IS 'Метод прогнозирования (linear_extrapolation, weighted_average, etc.)';

