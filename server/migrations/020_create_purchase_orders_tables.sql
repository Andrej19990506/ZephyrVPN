-- Миграция: Создание таблиц для модуля закупок (Purchase Orders)
-- Цель: Разделение процесса заказа у поставщика от физического оприходования товара
-- Менеджеры создают заказы, складские работники оприходуют по факту получения

-- ============================================
-- 1. Таблица заказов на закупку (Purchase Orders)
-- ============================================
CREATE TABLE IF NOT EXISTS purchase_orders (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    order_number VARCHAR(100) NOT NULL UNIQUE, -- Внешний номер заказа (PO-2026-001)
    supplier_id UUID NOT NULL REFERENCES counterparties(id) ON DELETE RESTRICT,
    branch_id UUID NOT NULL REFERENCES branches(id) ON DELETE RESTRICT,
    
    -- Статус заказа
    status VARCHAR(50) NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'ordered', 'partially_received', 'received', 'cancelled')),
    
    -- Даты
    order_date DATE NOT NULL DEFAULT CURRENT_DATE, -- Дата создания заказа
    expected_delivery_date DATE NOT NULL, -- Ожидаемая дата доставки (для фильтрации просроченных)
    actual_delivery_date DATE, -- Фактическая дата доставки (заполняется при получении)
    
    -- Финансовые данные
    total_amount DECIMAL(15,2) NOT NULL DEFAULT 0, -- Общая сумма заказа (рассчитывается из items)
    currency VARCHAR(3) DEFAULT 'RUB', -- Валюта заказа
    
    -- Условия оплаты
    payment_terms VARCHAR(255), -- Условия оплаты (например, "Оплата в течение 30 дней")
    payment_method VARCHAR(50) DEFAULT 'bank', -- 'bank', 'cash', 'hybrid'
    
    -- Ответственные лица
    created_by VARCHAR(255) NOT NULL, -- Username или ID менеджера, создавшего заказ
    approved_by VARCHAR(255), -- Username или ID утвердившего заказ
    received_by VARCHAR(255), -- Username или ID складского работника, получившего товар
    
    -- Связь с накладной (создается автоматически при получении)
    invoice_id UUID REFERENCES invoices(id) ON DELETE SET NULL, -- FK на invoices (создается при получении)
    
    -- Дополнительная информация
    notes TEXT, -- Заметки менеджера
    internal_notes TEXT, -- Внутренние заметки (не видны поставщику)
    
    -- Метаданные
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP, -- Soft delete
    
    -- Индексы для производительности
    CONSTRAINT chk_purchase_order_amount_positive CHECK (total_amount >= 0),
    CONSTRAINT chk_purchase_order_dates_valid CHECK (expected_delivery_date >= order_date)
);

-- Индексы для быстрого поиска
CREATE INDEX IF NOT EXISTS idx_purchase_orders_supplier_id ON purchase_orders(supplier_id);
CREATE INDEX IF NOT EXISTS idx_purchase_orders_branch_id ON purchase_orders(branch_id);
CREATE INDEX IF NOT EXISTS idx_purchase_orders_status ON purchase_orders(status);
CREATE INDEX IF NOT EXISTS idx_purchase_orders_expected_delivery_date ON purchase_orders(expected_delivery_date);
CREATE INDEX IF NOT EXISTS idx_purchase_orders_order_date ON purchase_orders(order_date);
CREATE INDEX IF NOT EXISTS idx_purchase_orders_invoice_id ON purchase_orders(invoice_id);
CREATE INDEX IF NOT EXISTS idx_purchase_orders_deleted_at ON purchase_orders(deleted_at);

-- ============================================
-- 2. Таблица позиций заказа на закупку (Purchase Order Items)
-- ============================================
CREATE TABLE IF NOT EXISTS purchase_order_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    purchase_order_id UUID NOT NULL REFERENCES purchase_orders(id) ON DELETE CASCADE,
    
    -- Связь с номенклатурой
    nomenclature_id UUID NOT NULL REFERENCES nomenclature_items(id) ON DELETE RESTRICT,
    
    -- Количество и единицы измерения
    ordered_quantity DECIMAL(10,2) NOT NULL CHECK (ordered_quantity > 0), -- Заказанное количество
    unit VARCHAR(20) NOT NULL DEFAULT 'kg', -- Единица измерения (берется из nomenclature_items.inbound_unit)
    
    -- Цена закупки (фиксируется на момент создания заказа для исторических данных)
    purchase_price_per_unit DECIMAL(10,2) NOT NULL CHECK (purchase_price_per_unit >= 0), -- Цена за единицу на момент заказа
    total_price DECIMAL(15,2) NOT NULL CHECK (total_price >= 0), -- Общая стоимость позиции (ordered_quantity * purchase_price_per_unit)
    
    -- Полученное количество (заполняется при получении товара)
    received_quantity DECIMAL(10,2) DEFAULT 0 CHECK (received_quantity >= 0), -- Фактически полученное количество
    received_total_price DECIMAL(15,2) DEFAULT 0 CHECK (received_total_price >= 0), -- Фактическая стоимость полученного товара
    
    -- Дополнительная информация
    notes TEXT, -- Заметки по позиции
    expiry_date DATE, -- Ожидаемый срок годности (если известен)
    
    -- Метаданные
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    
    -- Индексы
    CONSTRAINT chk_purchase_order_item_received_valid CHECK (received_quantity <= ordered_quantity),
    CONSTRAINT chk_purchase_order_item_price_positive CHECK (purchase_price_per_unit > 0)
);

-- Индексы для быстрого поиска
CREATE INDEX IF NOT EXISTS idx_purchase_order_items_order_id ON purchase_order_items(purchase_order_id);
CREATE INDEX IF NOT EXISTS idx_purchase_order_items_nomenclature_id ON purchase_order_items(nomenclature_id);

-- ============================================
-- 3. Триггер для автоматического обновления updated_at
-- ============================================
CREATE OR REPLACE FUNCTION update_purchase_orders_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = CURRENT_TIMESTAMP;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trigger_update_purchase_orders_updated_at
    BEFORE UPDATE ON purchase_orders
    FOR EACH ROW
    EXECUTE FUNCTION update_purchase_orders_updated_at();

CREATE TRIGGER trigger_update_purchase_order_items_updated_at
    BEFORE UPDATE ON purchase_order_items
    FOR EACH ROW
    EXECUTE FUNCTION update_purchase_orders_updated_at();

-- ============================================
-- 4. Триггер для автоматического расчета total_amount при изменении items
-- ============================================
CREATE OR REPLACE FUNCTION calculate_purchase_order_total()
RETURNS TRIGGER AS $$
BEGIN
    -- Пересчитываем общую сумму заказа при изменении позиций
    UPDATE purchase_orders
    SET total_amount = (
        SELECT COALESCE(SUM(total_price), 0)
        FROM purchase_order_items
        WHERE purchase_order_id = COALESCE(NEW.purchase_order_id, OLD.purchase_order_id)
    )
    WHERE id = COALESCE(NEW.purchase_order_id, OLD.purchase_order_id);
    
    RETURN COALESCE(NEW, OLD);
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trigger_calculate_purchase_order_total
    AFTER INSERT OR UPDATE OR DELETE ON purchase_order_items
    FOR EACH ROW
    EXECUTE FUNCTION calculate_purchase_order_total();

-- ============================================
-- 5. Функция для генерации номера заказа
-- ============================================
CREATE OR REPLACE FUNCTION generate_purchase_order_number()
RETURNS TRIGGER AS $$
DECLARE
    year_part VARCHAR(4);
    sequence_num INTEGER;
    new_number VARCHAR(100);
BEGIN
    -- Если номер уже задан, не генерируем
    IF NEW.order_number IS NOT NULL AND NEW.order_number != '' THEN
        RETURN NEW;
    END IF;
    
    -- Генерируем номер в формате PO-YYYY-NNN
    year_part := TO_CHAR(CURRENT_DATE, 'YYYY');
    
    -- Получаем следующий номер в последовательности для текущего года
    SELECT COALESCE(MAX(CAST(SUBSTRING(order_number FROM 'PO-\d{4}-(\d+)') AS INTEGER)), 0) + 1
    INTO sequence_num
    FROM purchase_orders
    WHERE order_number LIKE 'PO-' || year_part || '-%'
      AND deleted_at IS NULL;
    
    -- Форматируем номер: PO-2026-001
    new_number := 'PO-' || year_part || '-' || LPAD(sequence_num::TEXT, 3, '0');
    
    NEW.order_number := new_number;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trigger_generate_purchase_order_number
    BEFORE INSERT ON purchase_orders
    FOR EACH ROW
    EXECUTE FUNCTION generate_purchase_order_number();

-- ============================================
-- 6. Комментарии к таблицам и полям
-- ============================================
COMMENT ON TABLE purchase_orders IS 'Заказы на закупку товаров у поставщиков';
COMMENT ON TABLE purchase_order_items IS 'Позиции заказа на закупку';

COMMENT ON COLUMN purchase_orders.status IS 'Статус заказа: draft (черновик), ordered (отправлен поставщику), partially_received (частично получен), received (получен полностью), cancelled (отменен)';
COMMENT ON COLUMN purchase_orders.expected_delivery_date IS 'Ожидаемая дата доставки - используется для фильтрации просроченных заказов';
COMMENT ON COLUMN purchase_orders.invoice_id IS 'Связь с накладной (invoices) - создается автоматически при получении товара';
COMMENT ON COLUMN purchase_order_items.purchase_price_per_unit IS 'Цена за единицу на момент создания заказа (исторические данные, не меняется)';
COMMENT ON COLUMN purchase_order_items.received_quantity IS 'Фактически полученное количество (может отличаться от ordered_quantity)';



