-- 004_add_nested_recipes_support.sql
-- Добавление поддержки вложенных рецептов (полуфабрикатов)

-- Добавление поля is_semi_finished в таблицу recipes
DO $$ BEGIN
    ALTER TABLE recipes ADD COLUMN IF NOT EXISTS is_semi_finished BOOLEAN DEFAULT FALSE;
EXCEPTION
    WHEN duplicate_column THEN NULL;
END $$;

-- Добавление поля ingredient_recipe_id в таблицу recipe_ingredients
-- Это поле позволяет использовать другой рецепт как ингредиент
DO $$ BEGIN
    ALTER TABLE recipe_ingredients ADD COLUMN IF NOT EXISTS ingredient_recipe_id UUID;
EXCEPTION
    WHEN duplicate_column THEN NULL;
END $$;

-- Создание индекса для ingredient_recipe_id
CREATE INDEX IF NOT EXISTS idx_recipe_ingredients_ingredient_recipe_id ON recipe_ingredients (ingredient_recipe_id);

-- Добавление внешнего ключа для ingredient_recipe_id
DO $$ BEGIN
    ALTER TABLE recipe_ingredients ADD CONSTRAINT fk_recipe_ingredients_ingredient_recipe
    FOREIGN KEY (ingredient_recipe_id) REFERENCES recipes(id) ON DELETE CASCADE;
EXCEPTION
    WHEN duplicate_object THEN NULL;
END $$;

-- Обновление: nomenclature_id теперь может быть NULL (если используется вложенный рецепт)
-- Но нужно убедиться, что хотя бы одно из полей (nomenclature_id или ingredient_recipe_id) заполнено
-- Это можно сделать через CHECK constraint, но для совместимости оставим как есть

-- Комментарии к полям
COMMENT ON COLUMN recipes.is_semi_finished IS 'Флаг полуфабриката (true если это промежуточный продукт)';
COMMENT ON COLUMN recipe_ingredients.ingredient_recipe_id IS 'ID рецепта-полуфабриката (если ингредиент сам является рецептом)';
COMMENT ON COLUMN recipe_ingredients.nomenclature_id IS 'ID номенклатуры (сырье). Может быть NULL если используется ingredient_recipe_id';

