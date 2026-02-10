-- Миграция: Добавление поля is_ready_for_sale в nomenclature_items
-- Это поле указывает, готов ли товар к продаже (есть ли связанный рецепт с ингредиентами)

ALTER TABLE nomenclature_items
ADD COLUMN IF NOT EXISTS is_ready_for_sale BOOLEAN NOT NULL DEFAULT FALSE;

-- Комментарий к полю
COMMENT ON COLUMN nomenclature_items.is_ready_for_sale IS 'Флаг готовности товара к продаже. Устанавливается в true только когда есть связанный Recipe с заполненными ингредиентами.';

-- Индекс для быстрого поиска товаров, готовых к продаже
CREATE INDEX IF NOT EXISTS idx_nomenclature_is_ready_for_sale 
ON nomenclature_items(is_ready_for_sale) 
WHERE is_ready_for_sale = true;

-- Индекс для поиска товаров, требующих внимания (IsSaleable=true, но IsReadyForSale=false)
CREATE INDEX IF NOT EXISTS idx_nomenclature_needs_recipe 
ON nomenclature_items(is_saleable, is_ready_for_sale) 
WHERE is_saleable = true AND is_ready_for_sale = false;









