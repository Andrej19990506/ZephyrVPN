-- Миграция для инициализации меню
-- Запускается автоматически при первом старте PostgreSQL контейнера

-- Создаем таблицы (если их еще нет)
CREATE TABLE IF NOT EXISTS pizza_recipes (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) UNIQUE NOT NULL,
    price INTEGER NOT NULL,
    ingredients TEXT, -- JSON массив
    ingredient_amounts TEXT, -- JSON map
    is_active BOOLEAN DEFAULT true,
    created_at BIGINT DEFAULT EXTRACT(EPOCH FROM NOW())::BIGINT,
    updated_at BIGINT DEFAULT EXTRACT(EPOCH FROM NOW())::BIGINT
);

CREATE TABLE IF NOT EXISTS pizza_sets (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) UNIQUE NOT NULL,
    description TEXT,
    pizzas TEXT, -- JSON массив названий пицц
    price INTEGER NOT NULL,
    is_active BOOLEAN DEFAULT true,
    created_at BIGINT DEFAULT EXTRACT(EPOCH FROM NOW())::BIGINT,
    updated_at BIGINT DEFAULT EXTRACT(EPOCH FROM NOW())::BIGINT
);

CREATE TABLE IF NOT EXISTS extras (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) UNIQUE NOT NULL,
    price INTEGER NOT NULL,
    is_active BOOLEAN DEFAULT true,
    created_at BIGINT DEFAULT EXTRACT(EPOCH FROM NOW())::BIGINT,
    updated_at BIGINT DEFAULT EXTRACT(EPOCH FROM NOW())::BIGINT
);

-- Вставляем начальные данные (только если таблицы пустые)
DO $$
BEGIN
    -- Пиццы
    IF NOT EXISTS (SELECT 1 FROM pizza_recipes WHERE name = 'Английский завтрак') THEN
        INSERT INTO pizza_recipes (name, price, ingredients, ingredient_amounts) VALUES
        ('Английский завтрак', 599, 
         '["сыр моцарелла", "бекон", "яйцо", "помидоры", "лук", "соус"]',
         '{"сыр моцарелла": 150, "бекон": 80, "яйцо": 100, "помидоры": 120, "лук": 60, "соус": 80}'),
        ('Солянка Злодейская', 799,
         '["сыр моцарелла", "колбаса", "огурцы маринованные", "оливки", "пепперони", "бекон", "острый перец", "соус"]',
         '{"сыр моцарелла": 150, "колбаса": 100, "огурцы маринованные": 80, "оливки": 50, "пепперони": 100, "бекон": 80, "острый перец": 30, "соус": 80}'),
        ('Классическая', 499,
         '["сыр моцарелла", "помидоры", "базилик", "соус"]',
         '{"сыр моцарелла": 150, "помидоры": 120, "базилик": 10, "соус": 80}'),
        ('New York', 699,
         '["сыр моцарелла", "пепперони", "грибы", "лук", "соус"]',
         '{"сыр моцарелла": 150, "пепперони": 100, "грибы": 100, "лук": 60, "соус": 80}'),
        ('Пепперони', 549,
         '["сыр моцарелла", "пепперони", "соус"]',
         '{"сыр моцарелла": 150, "пепперони": 100, "соус": 80}'),
        ('Мясная', 749,
         '["сыр моцарелла", "бекон", "колбаса", "ветчина", "соус"]',
         '{"сыр моцарелла": 150, "бекон": 80, "колбаса": 100, "ветчина": 80, "соус": 80}'),
        ('Охотничья', 699,
         '["сыр моцарелла", "колбаса охотничья", "грибы", "лук", "соус"]',
         '{"сыр моцарелла": 150, "колбаса охотничья": 100, "грибы": 100, "лук": 60, "соус": 80}'),
        ('Курица и Грибы', 899,
         '["сыр моцарелла", "курица", "грибы", "соус"]',
         '{"сыр моцарелла": 150, "курица": 120, "грибы": 100, "соус": 80}');
    END IF;

    -- Наборы
    IF NOT EXISTS (SELECT 1 FROM pizza_sets WHERE name = 'Семейный набор') THEN
        INSERT INTO pizza_sets (name, description, pizzas, price) VALUES
        ('Семейный набор', '2 пиццы на выбор + сырный бортик',
         '["Классическая", "Пепперони"]', 1200),
        ('Мясной набор', 'Мясная + New York + Английский завтрак + Классическая + Курица и Грибы',
         '["Мясная", "Охотничья"]', 1500),
        ('Пицца-пати', '3 пиццы: New York + Солянка Злодейская + Английский завтрак',
         '["New York", "Солянка Злодейская", "Английский завтрак"]', 2000);
    END IF;

    -- Допы
    IF NOT EXISTS (SELECT 1 FROM extras WHERE name = 'Сырный бортик') THEN
        INSERT INTO extras (name, price) VALUES
        ('Сырный бортик', 199);
    END IF;
END $$;







