-- Миграция: Создание таблиц для модуля Technologist Workspace

-- Таблица версий рецептов (Recipe Versioning)
CREATE TABLE IF NOT EXISTS recipe_versions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    recipe_id UUID NOT NULL REFERENCES recipes(id) ON DELETE CASCADE,
    version INTEGER NOT NULL,
    changed_by VARCHAR(255) NOT NULL,
    change_reason TEXT,
    ingredients_json TEXT NOT NULL, -- JSON снимок ингредиентов
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    
    UNIQUE(recipe_id, version) -- Одна версия на рецепт
);

CREATE INDEX IF NOT EXISTS idx_recipe_versions_recipe_id ON recipe_versions(recipe_id);
CREATE INDEX IF NOT EXISTS idx_recipe_versions_created_at ON recipe_versions(created_at DESC);

COMMENT ON TABLE recipe_versions IS 'Версии рецептов для отслеживания изменений';
COMMENT ON COLUMN recipe_versions.version IS 'Номер версии (1, 2, 3...)';
COMMENT ON COLUMN recipe_versions.changed_by IS 'ID пользователя или имя, кто внес изменения';
COMMENT ON COLUMN recipe_versions.ingredients_json IS 'JSON снимок ингредиентов на момент изменения';

-- Таблица обучающих материалов (Training Materials)
CREATE TABLE IF NOT EXISTS training_materials (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    recipe_id UUID NOT NULL REFERENCES recipes(id) ON DELETE CASCADE,
    type VARCHAR(50) NOT NULL CHECK (type IN ('video', 'photo', 'document')),
    title VARCHAR(255) NOT NULL,
    description TEXT,
    s3_url TEXT NOT NULL,
    thumbnail_url TEXT,
    "order" INTEGER NOT NULL DEFAULT 0,
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_by VARCHAR(255) NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_training_materials_recipe_id ON training_materials(recipe_id);
CREATE INDEX IF NOT EXISTS idx_training_materials_type ON training_materials(type);
CREATE INDEX IF NOT EXISTS idx_training_materials_is_active ON training_materials(is_active) WHERE is_active = true;

COMMENT ON TABLE training_materials IS 'Обучающие материалы для рецептов (видео, фото, документы)';
COMMENT ON COLUMN training_materials.type IS 'Тип материала: video, photo, document';
COMMENT ON COLUMN training_materials.s3_url IS 'URL файла в S3 хранилище';
COMMENT ON COLUMN training_materials.thumbnail_url IS 'URL превью для видео/фото';

-- Таблица экзаменов по рецептам (Recipe Exams)
CREATE TABLE IF NOT EXISTS recipe_exams (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    recipe_id UUID NOT NULL REFERENCES recipes(id) ON DELETE CASCADE,
    staff_id UUID NOT NULL REFERENCES staff(id) ON DELETE CASCADE,
    status VARCHAR(50) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'passed', 'failed')),
    score INTEGER NOT NULL DEFAULT 0 CHECK (score >= 0 AND score <= 100),
    passed_at TIMESTAMP,
    examined_by VARCHAR(255),
    notes TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    
    UNIQUE(recipe_id, staff_id) -- Один экзамен на рецепт для каждого сотрудника
);

CREATE INDEX IF NOT EXISTS idx_recipe_exams_recipe_id ON recipe_exams(recipe_id);
CREATE INDEX IF NOT EXISTS idx_recipe_exams_staff_id ON recipe_exams(staff_id);
CREATE INDEX IF NOT EXISTS idx_recipe_exams_status ON recipe_exams(status);

COMMENT ON TABLE recipe_exams IS 'Экзамены по рецептам для сотрудников';
COMMENT ON COLUMN recipe_exams.status IS 'Статус: pending, passed, failed';
COMMENT ON COLUMN recipe_exams.score IS 'Баллы (0-100)';
COMMENT ON COLUMN recipe_exams.examined_by IS 'ID технолога, который проверил экзамен';









