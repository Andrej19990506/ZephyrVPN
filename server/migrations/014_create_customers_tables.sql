-- Миграция: Создание таблиц для управления клиентами
-- Дата: 2026-02-07
-- Описание: Создает таблицы users, customers и customer_addresses для управления клиентами и их адресами доставки

-- ============================================
-- TABLE: users (базовая таблица аутентификации)
-- ============================================
CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email VARCHAR(255) UNIQUE,
    phone VARCHAR(20) UNIQUE NOT NULL,
    password_hash VARCHAR(255),
    role VARCHAR(50) NOT NULL DEFAULT 'customer',
    -- Роли: 'customer', 'courier', 'kitchen_staff', 'technologist', 'admin'
    status VARCHAR(20) NOT NULL DEFAULT 'active',
    -- Статусы: 'active', 'inactive', 'suspended'
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    
    -- Ограничения
    CONSTRAINT users_role_check CHECK (role IN ('customer', 'courier', 'kitchen_staff', 'technologist', 'admin')),
    CONSTRAINT users_status_check CHECK (status IN ('active', 'inactive', 'suspended'))
);

-- ============================================
-- TABLE: customers (расширенная информация о клиентах)
-- ============================================
CREATE TABLE IF NOT EXISTS customers (
    user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    first_name VARCHAR(100),
    last_name VARCHAR(100),
    loyalty_points INT NOT NULL DEFAULT 0,
    total_orders INT NOT NULL DEFAULT 0,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

-- ============================================
-- TABLE: customer_addresses (адреса доставки клиентов)
-- ============================================
CREATE TABLE IF NOT EXISTS customer_addresses (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    customer_id UUID NOT NULL REFERENCES customers(user_id) ON DELETE CASCADE,
    type VARCHAR(20) NOT NULL DEFAULT 'home',
    -- Типы: 'home', 'work', 'other'
    address TEXT NOT NULL,
    coordinates POINT, -- GPS координаты для доставки
    floor VARCHAR(10),
    entrance VARCHAR(10),
    comment TEXT,
    is_default BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    
    -- Ограничения
    CONSTRAINT customer_addresses_type_check CHECK (type IN ('home', 'work', 'other'))
);

-- ============================================
-- INDEXES для производительности
-- ============================================

-- Индексы для users
CREATE INDEX IF NOT EXISTS idx_users_email ON users(email) WHERE email IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_users_phone ON users(phone);
CREATE INDEX IF NOT EXISTS idx_users_role_status ON users(role, status);
CREATE INDEX IF NOT EXISTS idx_users_status ON users(status) WHERE status = 'active';

-- Индексы для customers
CREATE INDEX IF NOT EXISTS idx_customers_user_id ON customers(user_id);

-- Индексы для customer_addresses
CREATE INDEX IF NOT EXISTS idx_customer_addresses_customer ON customer_addresses(customer_id);
CREATE INDEX IF NOT EXISTS idx_customer_addresses_default ON customer_addresses(customer_id, is_default) WHERE is_default = true;
CREATE INDEX IF NOT EXISTS idx_customer_addresses_type ON customer_addresses(customer_id, type);

-- ============================================
-- FUNCTION: Автоматическое обновление updated_at
-- ============================================
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- ============================================
-- TRIGGERS: Автоматическое обновление updated_at
-- ============================================

-- Триггер для users
DROP TRIGGER IF EXISTS update_users_updated_at ON users;
CREATE TRIGGER update_users_updated_at
    BEFORE UPDATE ON users
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- Триггер для customers
DROP TRIGGER IF EXISTS update_customers_updated_at ON customers;
CREATE TRIGGER update_customers_updated_at
    BEFORE UPDATE ON customers
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- Триггер для customer_addresses
DROP TRIGGER IF EXISTS update_customer_addresses_updated_at ON customer_addresses;
CREATE TRIGGER update_customer_addresses_updated_at
    BEFORE UPDATE ON customer_addresses
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- ============================================
-- FUNCTION: Обеспечение единственного адреса по умолчанию
-- ============================================
CREATE OR REPLACE FUNCTION ensure_single_default_address()
RETURNS TRIGGER AS $$
BEGIN
    -- Если устанавливаем адрес как default, снимаем default с других адресов этого клиента
    IF NEW.is_default = true THEN
        UPDATE customer_addresses
        SET is_default = false
        WHERE customer_id = NEW.customer_id
          AND id != NEW.id
          AND is_default = true;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Триггер для обеспечения единственного адреса по умолчанию
DROP TRIGGER IF EXISTS ensure_single_default_address_trigger ON customer_addresses;
CREATE TRIGGER ensure_single_default_address_trigger
    BEFORE INSERT OR UPDATE ON customer_addresses
    FOR EACH ROW
    EXECUTE FUNCTION ensure_single_default_address();

-- ============================================
-- COMMENTS для документации
-- ============================================
COMMENT ON TABLE users IS 'Базовая таблица аутентификации для всех типов пользователей';
COMMENT ON TABLE customers IS 'Расширенная информация для клиентов';
COMMENT ON TABLE customer_addresses IS 'Адреса доставки для клиентов';

COMMENT ON COLUMN users.role IS 'Роль пользователя: customer, courier, kitchen_staff, technologist, admin';
COMMENT ON COLUMN users.status IS 'Статус аккаунта: active, inactive, suspended';
COMMENT ON COLUMN users.phone IS 'Телефон (обязательное поле, уникальное)';
COMMENT ON COLUMN users.email IS 'Email (опциональное поле, уникальное если указан)';

COMMENT ON COLUMN customers.user_id IS 'Ссылка на users.id (ON DELETE CASCADE)';
COMMENT ON COLUMN customers.loyalty_points IS 'Баллы лояльности клиента';
COMMENT ON COLUMN customers.total_orders IS 'Общее количество заказов клиента';

COMMENT ON COLUMN customer_addresses.type IS 'Тип адреса: home, work, other';
COMMENT ON COLUMN customer_addresses.coordinates IS 'GPS координаты для расчета маршрута доставки';
COMMENT ON COLUMN customer_addresses.is_default IS 'Адрес по умолчанию (только один адрес может быть default)';



