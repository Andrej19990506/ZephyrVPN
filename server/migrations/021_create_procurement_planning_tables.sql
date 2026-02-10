-- Миграция: Создание таблиц для модуля планирования закупок на месяц
-- Цель: Аналитическое планирование закупок с прогнозированием спроса

-- ============================================
-- 1. Таблица планов закупок (Procurement Plans)
-- ============================================
CREATE TABLE IF NOT EXISTS procurement_plans (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    plan_number VARCHAR(100) NOT NULL UNIQUE, -- PLAN-2026-02
    branch_id UUID NOT NULL REFERENCES branches(id) ON DELETE RESTRICT,
    month DATE NOT NULL, -- Первый день месяца (2026-02-01)
    year INTEGER NOT NULL, -- 2026
    month_number INTEGER NOT NULL, -- 2 (февраль)
    
    -- Статус плана
    status VARCHAR(50) NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'submitted', 'approved', 'executed', 'cancelled')),
    
    -- Ответственные
    created_by VARCHAR(255) NOT NULL, -- Username менеджера
    approved_by VARCHAR(255), -- Username утвердившего
    submitted_at TIMESTAMP, -- Когда план был отправлен (созданы PurchaseOrders)
    
    -- Метаданные
    notes TEXT, -- Заметки к плану
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP, -- Soft delete
    
    -- Индексы
    CONSTRAINT chk_procurement_plan_month_valid CHECK (month_number >= 1 AND month_number <= 12),
    CONSTRAINT uq_procurement_plan_branch_month UNIQUE (branch_id, year, month_number, deleted_at)
);

CREATE INDEX IF NOT EXISTS idx_procurement_plans_branch_id ON procurement_plans(branch_id);
CREATE INDEX IF NOT EXISTS idx_procurement_plans_month ON procurement_plans(year, month_number);
CREATE INDEX IF NOT EXISTS idx_procurement_plans_status ON procurement_plans(status);
CREATE INDEX IF NOT EXISTS idx_procurement_plans_deleted_at ON procurement_plans(deleted_at);

-- ============================================
-- 2. Таблица позиций плана (Plan Items - матрица день × товар)
-- ============================================
CREATE TABLE IF NOT EXISTS procurement_plan_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    plan_id UUID NOT NULL REFERENCES procurement_plans(id) ON DELETE CASCADE,
    nomenclature_id UUID NOT NULL REFERENCES nomenclature_items(id) ON DELETE RESTRICT,
    plan_date DATE NOT NULL, -- Конкретная дата в месяце (2026-02-15)
    
    -- Планируемое количество
    planned_quantity DECIMAL(10,2) NOT NULL DEFAULT 0 CHECK (planned_quantity >= 0),
    unit VARCHAR(20) NOT NULL DEFAULT 'kg', -- Единица измерения
    
    -- Прогноз спроса (автоматически рассчитанный)
    forecasted_quantity DECIMAL(10,2) DEFAULT 0, -- Прогнозируемое количество на основе истории
    forecast_confidence DECIMAL(5,2) DEFAULT 0, -- Уверенность прогноза (0-100%)
    predicted_kitchen_load VARCHAR(20), -- 'high', 'medium', 'low'
    
    -- Исторические данные (для аналитики)
    last_month_quantity DECIMAL(10,2) DEFAULT 0, -- Количество в прошлом месяце в этот день недели
    avg_last_3_months DECIMAL(10,2) DEFAULT 0, -- Среднее за последние 3 месяца
    
    -- Рекомендации по поставщику
    suggested_supplier_id UUID REFERENCES counterparties(id), -- Рекомендуемый поставщик
    suggested_price_per_unit DECIMAL(10,2) DEFAULT 0, -- Последняя известная цена закупки
    
    -- Метаданные
    notes TEXT, -- Заметки по позиции
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    
    -- Уникальность: один товар на одну дату в одном плане
    CONSTRAINT uq_plan_item_date_nomenclature UNIQUE (plan_id, plan_date, nomenclature_id)
);

CREATE INDEX IF NOT EXISTS idx_procurement_plan_items_plan_id ON procurement_plan_items(plan_id);
CREATE INDEX IF NOT EXISTS idx_procurement_plan_items_nomenclature_id ON procurement_plan_items(nomenclature_id);
CREATE INDEX IF NOT EXISTS idx_procurement_plan_items_date ON procurement_plan_items(plan_date);
CREATE INDEX IF NOT EXISTS idx_procurement_plan_items_supplier ON procurement_plan_items(suggested_supplier_id);

-- ============================================
-- 3. Таблица исторических данных закупок (для прогнозирования)
-- ============================================
CREATE TABLE IF NOT EXISTS procurement_history (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    branch_id UUID NOT NULL REFERENCES branches(id) ON DELETE RESTRICT,
    nomenclature_id UUID NOT NULL REFERENCES nomenclature_items(id) ON DELETE RESTRICT,
    order_date DATE NOT NULL, -- Дата заказа
    delivery_date DATE NOT NULL, -- Дата доставки
    day_of_week INTEGER NOT NULL, -- 1-7 (понедельник-воскресенье) для анализа по дням недели
    week_of_month INTEGER NOT NULL, -- 1-5 (неделя месяца)
    
    -- Данные заказа
    ordered_quantity DECIMAL(10,2) NOT NULL,
    received_quantity DECIMAL(10,2) NOT NULL,
    unit VARCHAR(20) NOT NULL,
    purchase_price_per_unit DECIMAL(10,2) NOT NULL,
    supplier_id UUID REFERENCES counterparties(id),
    
    -- Связь с заказом
    purchase_order_id UUID REFERENCES purchase_orders(id),
    
    -- Метаданные
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    
    -- Индексы для быстрого поиска исторических данных
    CONSTRAINT chk_procurement_history_day_of_week CHECK (day_of_week >= 1 AND day_of_week <= 7),
    CONSTRAINT chk_procurement_history_week_of_month CHECK (week_of_month >= 1 AND week_of_month <= 5)
);

CREATE INDEX IF NOT EXISTS idx_procurement_history_branch_nomenclature ON procurement_history(branch_id, nomenclature_id);
CREATE INDEX IF NOT EXISTS idx_procurement_history_delivery_date ON procurement_history(delivery_date);
CREATE INDEX IF NOT EXISTS idx_procurement_history_day_of_week ON procurement_history(day_of_week);
CREATE INDEX IF NOT EXISTS idx_procurement_history_supplier ON procurement_history(supplier_id);

-- ============================================
-- 4. Таблица прогнозов спроса (Demand Forecasts)
-- ============================================
CREATE TABLE IF NOT EXISTS demand_forecasts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    branch_id UUID NOT NULL REFERENCES branches(id) ON DELETE RESTRICT,
    nomenclature_id UUID NOT NULL REFERENCES nomenclature_items(id) ON DELETE RESTRICT,
    forecast_date DATE NOT NULL, -- Дата прогноза
    
    -- Прогнозируемое количество
    forecasted_quantity DECIMAL(10,2) NOT NULL,
    unit VARCHAR(20) NOT NULL,
    
    -- Метрики прогноза
    confidence_score DECIMAL(5,2) DEFAULT 0, -- Уверенность (0-100%)
    forecast_method VARCHAR(50), -- 'moving_average', 'seasonal', 'ml_model', 'manual'
    predicted_kitchen_load VARCHAR(20), -- 'high', 'medium', 'low'
    
    -- Факторы влияния
    seasonal_factor DECIMAL(5,2) DEFAULT 1.0, -- Сезонный коэффициент
    trend_factor DECIMAL(5,2) DEFAULT 1.0, -- Тренд (растет/падает)
    day_of_week_factor DECIMAL(5,2) DEFAULT 1.0, -- Коэффициент дня недели
    
    -- Метаданные
    calculated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    valid_until TIMESTAMP, -- До какой даты прогноз актуален
    
    -- Уникальность: один прогноз на товар на дату
    CONSTRAINT uq_demand_forecast_date_nomenclature UNIQUE (branch_id, nomenclature_id, forecast_date)
);

CREATE INDEX IF NOT EXISTS idx_demand_forecasts_branch_nomenclature ON demand_forecasts(branch_id, nomenclature_id);
CREATE INDEX IF NOT EXISTS idx_demand_forecasts_date ON demand_forecasts(forecast_date);
CREATE INDEX IF NOT EXISTS idx_demand_forecasts_valid_until ON demand_forecasts(valid_until);

-- ============================================
-- 5. Триггеры для автоматического обновления updated_at
-- ============================================
CREATE OR REPLACE FUNCTION update_procurement_plans_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = CURRENT_TIMESTAMP;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trigger_update_procurement_plans_updated_at
    BEFORE UPDATE ON procurement_plans
    FOR EACH ROW
    EXECUTE FUNCTION update_procurement_plans_updated_at();

CREATE TRIGGER trigger_update_procurement_plan_items_updated_at
    BEFORE UPDATE ON procurement_plan_items
    FOR EACH ROW
    EXECUTE FUNCTION update_procurement_plans_updated_at();

-- ============================================
-- 6. Функция для генерации номера плана
-- ============================================
CREATE OR REPLACE FUNCTION generate_procurement_plan_number()
RETURNS TRIGGER AS $$
DECLARE
    year_part VARCHAR(4);
    month_part VARCHAR(2);
    new_number VARCHAR(100);
BEGIN
    IF NEW.plan_number IS NOT NULL AND NEW.plan_number != '' THEN
        RETURN NEW;
    END IF;
    
    year_part := TO_CHAR(NEW.month, 'YYYY');
    month_part := TO_CHAR(NEW.month, 'MM');
    new_number := 'PLAN-' || year_part || '-' || month_part;
    
    NEW.plan_number := new_number;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trigger_generate_procurement_plan_number
    BEFORE INSERT ON procurement_plans
    FOR EACH ROW
    EXECUTE FUNCTION generate_procurement_plan_number();

-- ============================================
-- 7. Комментарии к таблицам
-- ============================================
COMMENT ON TABLE procurement_plans IS 'Планы закупок на месяц';
COMMENT ON TABLE procurement_plan_items IS 'Позиции плана закупок (матрица день × товар)';
COMMENT ON TABLE procurement_history IS 'Исторические данные закупок для прогнозирования';
COMMENT ON TABLE demand_forecasts IS 'Прогнозы спроса на товары';

COMMENT ON COLUMN procurement_plan_items.forecasted_quantity IS 'Автоматически рассчитанное прогнозируемое количество';
COMMENT ON COLUMN procurement_plan_items.predicted_kitchen_load IS 'Прогнозируемая загрузка кухни: high/medium/low';
COMMENT ON COLUMN procurement_plan_items.last_month_quantity IS 'Количество в прошлом месяце в этот же день недели';
COMMENT ON COLUMN procurement_history.day_of_week IS 'День недели (1=понедельник, 7=воскресенье) для анализа по дням';



