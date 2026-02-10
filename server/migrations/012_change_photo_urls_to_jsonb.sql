-- Миграция: Изменение типа photo_urls с TEXT на JSONB для лучшей производительности и индексации
-- JSONB позволяет эффективно индексировать и запрашивать массивы ссылок на фото

-- Сначала конвертируем существующие данные из TEXT в JSONB
-- Если photo_urls уже является валидным JSON, просто меняем тип
-- Если это не JSON или пусто, устанавливаем NULL или пустой массив

-- Шаг 1: Создаем временную колонку с типом JSONB
ALTER TABLE recipes
ADD COLUMN IF NOT EXISTS photo_urls_jsonb JSONB;

-- Шаг 2: Конвертируем данные из TEXT в JSONB
UPDATE recipes
SET photo_urls_jsonb = CASE
    WHEN photo_urls IS NULL OR photo_urls = '' THEN NULL
    WHEN photo_urls::text ~ '^\[.*\]$' THEN photo_urls::jsonb -- Если это валидный JSON массив
    ELSE NULL -- Если не валидный JSON, устанавливаем NULL
END;

-- Шаг 3: Удаляем старую колонку
ALTER TABLE recipes
DROP COLUMN IF EXISTS photo_urls;

-- Шаг 4: Переименовываем новую колонку
ALTER TABLE recipes
RENAME COLUMN photo_urls_jsonb TO photo_urls;

-- Шаг 5: Обновляем комментарий
COMMENT ON COLUMN recipes.photo_urls IS 'JSONB массив ссылок на фотографии процесса приготовления в S3. Формат: ["url1", "url2", ...]';

-- Шаг 6: Создаем GIN индекс для эффективного поиска по массиву фото
CREATE INDEX IF NOT EXISTS idx_recipes_photo_urls_gin 
ON recipes USING GIN (photo_urls)
WHERE photo_urls IS NOT NULL;









