# Создание таблицы orders

Таблица `orders` создается через SQL-миграцию, а не через GORM AutoMigrate, так как она партиционирована.

## Выполнение миграции

### Вариант 1: Через Docker (рекомендуется)

```bash
# Выполнить SQL-миграцию в контейнере PostgreSQL
docker exec -i zephyrvpn_postgres psql -U your_user -d pizza_db < migrations/013_create_partitioned_orders_table.sql
```

### Вариант 2: Напрямую в PostgreSQL

```bash
# Подключиться к базе данных
psql -U your_user -d pizza_db

# Выполнить миграцию
\i migrations/013_create_partitioned_orders_table.sql
```

### Вариант 3: Через Railway (если используется)

1. Подключитесь к PostgreSQL через Railway CLI или веб-интерфейс
2. Выполните содержимое файла `migrations/013_create_partitioned_orders_table.sql`

## Проверка

После выполнения миграции проверьте:

```sql
-- Проверить существование таблицы
SELECT EXISTS (
    SELECT FROM information_schema.tables 
    WHERE table_name = 'orders'
);

-- Проверить партиции
SELECT schemaname, tablename 
FROM pg_tables 
WHERE tablename LIKE 'orders_%';
```

## После создания таблицы

После создания таблицы `orders` можно выполнить скрипт генерации тестовых данных:

```bash
docker exec -i zephyrvpn_postgres psql -U your_user -d pizza_db < migrations/026_generate_test_revenue_data.sql
```

