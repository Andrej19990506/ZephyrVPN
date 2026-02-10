-- Миграция: Добавление полей для Recipe Book (Frontend Knowledge Base)
-- Эти поля используются для визуального представления рецепта как цифровой поваренной книги

ALTER TABLE recipes
ADD COLUMN IF NOT EXISTS instruction_text TEXT,
ADD COLUMN IF NOT EXISTS video_url TEXT,
ADD COLUMN IF NOT EXISTS photo_urls TEXT; -- JSON массив ссылок на фото в S3

-- Комментарии к полям
COMMENT ON COLUMN recipes.instruction_text IS 'Пошаговая инструкция приготовления в формате Markdown для Recipe Book';
COMMENT ON COLUMN recipes.video_url IS 'Ссылка на видео-инструкцию в S3';
COMMENT ON COLUMN recipes.photo_urls IS 'JSON массив ссылок на фотографии процесса приготовления в S3';

-- Индекс для быстрого поиска рецептов с видео
CREATE INDEX IF NOT EXISTS idx_recipes_has_video 
ON recipes(video_url) 
WHERE video_url IS NOT NULL AND video_url != '';









