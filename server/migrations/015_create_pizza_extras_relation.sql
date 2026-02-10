-- Миграция для создания таблицы связи пицца-доп
-- Позволяет привязывать допы к конкретным пиццам

-- Таблица связи многие-ко-многим между пиццами и допами
CREATE TABLE IF NOT EXISTS pizza_extras (
    id SERIAL PRIMARY KEY,
    pizza_name VARCHAR(255) NOT NULL REFERENCES pizza_recipes(name) ON DELETE CASCADE,
    extra_id INTEGER NOT NULL REFERENCES extras(id) ON DELETE CASCADE,
    is_default BOOLEAN DEFAULT false, -- Доп доступен по умолчанию для этой пиццы
    display_order INTEGER DEFAULT 0, -- Порядок отображения допа в списке
    created_at BIGINT DEFAULT EXTRACT(EPOCH FROM NOW())::BIGINT,
    updated_at BIGINT DEFAULT EXTRACT(EPOCH FROM NOW())::BIGINT,
    UNIQUE(pizza_name, extra_id) -- Один доп может быть привязан к пицце только один раз
);

-- Индексы для быстрого поиска
CREATE INDEX IF NOT EXISTS idx_pizza_extras_pizza_name ON pizza_extras(pizza_name);
CREATE INDEX IF NOT EXISTS idx_pizza_extras_extra_id ON pizza_extras(extra_id);
CREATE INDEX IF NOT EXISTS idx_pizza_extras_is_default ON pizza_extras(pizza_name, is_default) WHERE is_default = true;

-- Комментарии для документации
COMMENT ON TABLE pizza_extras IS 'Связь многие-ко-многим между пиццами и допами. Позволяет ограничить доступные допы для каждой пиццы.';
COMMENT ON COLUMN pizza_extras.is_default IS 'Если true, доп доступен по умолчанию для этой пиццы (отображается в конфигураторе)';
COMMENT ON COLUMN pizza_extras.display_order IS 'Порядок отображения допа в списке (меньше = выше)';



