# Деплой на Railway

Этот документ описывает процесс деплоя Go сервера на [Railway](https://railway.com/).

## Подготовка

### 1. Создайте аккаунт на Railway

1. Перейдите на https://railway.com/
2. Зарегистрируйтесь через GitHub
3. Создайте новый проект

### 2. Подключите репозиторий

1. В Railway Dashboard нажмите "New Project"
2. Выберите "Deploy from GitHub repo"
3. Выберите ваш репозиторий
4. Railway автоматически определит Dockerfile

## Настройка переменных окружения

В Railway Dashboard перейдите в Settings → Variables и добавьте:

### Обязательные переменные:

```env
# JWT Secret (минимум 32 символа)
JWT_SECRET=your-super-secret-jwt-key-min-32-chars-change-this

# Environment
ENV=production
```

### Опциональные переменные:

```env
# Kafka (если используется)
KAFKA_BROKERS=your-kafka-brokers

# Business hours (UTC)
BUSINESS_OPEN_HOUR=2
BUSINESS_CLOSE_HOUR=16
BUSINESS_CLOSE_MIN=45
```

## Подключение базы данных PostgreSQL

1. В Railway Dashboard нажмите "+ New" → "Database" → "PostgreSQL"
2. Railway автоматически создаст базу данных
3. Railway автоматически установит переменную `DATABASE_URL`
4. Сервер автоматически подключится к базе данных

## Подключение Redis (опционально)

1. В Railway Dashboard нажмите "+ New" → "Database" → "Redis"
2. Railway автоматически установит переменную `REDIS_URL`
3. Сервер автоматически подключится к Redis

## Порт

Railway автоматически устанавливает переменную `PORT`. Сервер уже настроен для чтения этой переменной.

## Деплой

1. Railway автоматически деплоит при каждом push в репозиторий
2. Или нажмите "Deploy" вручную в Dashboard
3. Следите за логами в разделе "Deployments"

## Проверка деплоя

После успешного деплоя:

1. **Найдите публичный URL:**
   
   **Способ 1: Через Settings (рекомендуется)**
   - Откройте ваш сервис в Railway Dashboard
   - В правой панели нажмите **"Settings"**
   - Прокрутите до раздела **"Networking"** или **"Domains"**
   - Найдите **"Public Networking"** и включите его (если выключен)
   - Railway автоматически создаст домен вида: `https://your-service-name.up.railway.app`
   - Скопируйте этот URL
   
   **Способ 2: Через Deployments**
   - Откройте вкладку **"Deployments"**
   - В последнем успешном деплое будет показан URL сервиса
   
   **Способ 3: В верхней части карточки сервиса**
   - Иногда Railway показывает URL прямо в карточке сервиса
   - Или кнопку **"Generate Domain"** для создания домена

2. **API будет доступен по адресу:**
   - Основной API: `https://your-service-name.up.railway.app/api/v1`
   - Health check: `https://your-service-name.up.railway.app/api/v1/health`
   - WebSocket: `wss://your-service-name.up.railway.app/api/v1/ws`
   - gRPC: `your-service-name.up.railway.app:50051` (если настроен)

3. **Проверьте здоровье сервера:**
   ```bash
   curl https://your-service-name.up.railway.app/api/v1/health
   ```
   
   Ожидаемый ответ:
   ```json
   {
     "status": "ok",
     "service": "ERP Server",
     "version": "1.0.0"
   }
   ```

4. **Настройка custom domain (опционально):**
   - В разделе **"Settings"** → **"Networking"** → **"Custom Domain"**
   - Добавьте свой домен (например: `api.yourdomain.com`)
   - Railway автоматически настроит SSL сертификат
   - Обновите DNS записи согласно инструкциям Railway

## Миграции базы данных

Миграции выполняются автоматически при старте сервера через `models.AutoMigrate()`.

Если нужно выполнить миграции вручную:

1. Подключитесь к базе данных через Railway CLI или Dashboard
2. Выполните SQL файлы из папки `migrations/` в порядке:
   - `001_init_menu.sql`
   - `002_add_branch_vilskogo.sql`
   - `003_create_invoices_table.sql`
   - `004_add_nested_recipes_support.sql`

## Мониторинг

Railway предоставляет:
- Логи в реальном времени
- Метрики использования ресурсов
- Алерты (настраиваются в Settings)

## Troubleshooting

### Сервер не запускается

1. Проверьте логи в Railway Dashboard
2. Убедитесь, что все обязательные переменные окружения установлены
3. Проверьте, что `DATABASE_URL` правильно настроен

### Ошибки подключения к базе данных

1. Убедитесь, что PostgreSQL сервис запущен
2. Проверьте `DATABASE_URL` в переменных окружения
3. Railway автоматически добавляет SSL параметры в `DATABASE_URL`

### Порт не определен

Railway автоматически устанавливает `PORT`. Если проблема:
1. Проверьте, что сервер читает `PORT` из переменных окружения (уже настроено)
2. Сервер использует fallback на порт 8080

## Дополнительные ресурсы

- [Railway Documentation](https://docs.railway.app/)
- [Railway Discord](https://discord.gg/railway)

