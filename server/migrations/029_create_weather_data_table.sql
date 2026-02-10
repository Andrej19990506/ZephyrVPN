-- Миграция 029: Создание таблицы для хранения данных о погоде
-- Данные о погоде используются как внешние регрессоры для прогнозирования выручки
-- Интеграция с Open-Meteo API для получения прогноза и исторических данных

CREATE TABLE IF NOT EXISTS weather_data (
    id SERIAL PRIMARY KEY,
    date DATE NOT NULL UNIQUE, -- Дата (UNIQUE - один прогноз на день)
    latitude DECIMAL(10, 8) NOT NULL, -- Широта места
    longitude DECIMAL(11, 8) NOT NULL, -- Долгота места
    timezone VARCHAR(50) NOT NULL DEFAULT 'Europe/Berlin', -- Часовой пояс
    
    -- Температурные данные
    avg_temp DECIMAL(5, 2), -- Средняя температура за день (°C)
    max_temp DECIMAL(5, 2), -- Максимальная температура (°C)
    min_temp DECIMAL(5, 2), -- Минимальная температура (°C)
    temp_at_12 DECIMAL(5, 2), -- Температура в 12:00 (обеденное время)
    temp_at_18 DECIMAL(5, 2), -- Температура в 18:00 (ужин)
    
    -- Метаданные
    source VARCHAR(50) NOT NULL DEFAULT 'open-meteo', -- Источник данных
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Индексы для быстрого поиска
CREATE INDEX IF NOT EXISTS idx_weather_data_date ON weather_data(date DESC);
-- Частичный индекс для недавних данных (без WHERE, так как CURRENT_DATE не immutable)
CREATE INDEX IF NOT EXISTS idx_weather_data_recent ON weather_data(date DESC) WHERE date >= '2025-01-01'::DATE;

-- Комментарии к таблице
COMMENT ON TABLE weather_data IS 'Данные о погоде для использования в прогнозировании выручки. Интеграция с Open-Meteo API.';
COMMENT ON COLUMN weather_data.date IS 'Дата прогноза/данных (UNIQUE - один прогноз на день)';
COMMENT ON COLUMN weather_data.avg_temp IS 'Средняя температура за день в градусах Цельсия';
COMMENT ON COLUMN weather_data.temp_at_12 IS 'Температура в 12:00 (обеденное время) - важный фактор для прогноза выручки';
COMMENT ON COLUMN weather_data.temp_at_18 IS 'Температура в 18:00 (ужин) - важный фактор для прогноза выручки';

-- Триггер для автоматического обновления updated_at
CREATE OR REPLACE FUNCTION update_weather_data_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER weather_data_updated_at_trigger
    BEFORE UPDATE ON weather_data
    FOR EACH ROW
    EXECUTE FUNCTION update_weather_data_updated_at();

