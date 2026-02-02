#!/bin/bash
# Скрипт для запуска add_branch_vilskogo.go через Docker
# Использование: ./run_add_branch_docker.sh

echo "🔄 Запуск скрипта добавления филиала через Docker..."

# Переходим в директорию проекта
cd "$(dirname "$0")/.."

# Проверяем, запущен ли контейнер postgres
if ! docker ps | grep -q zephyrvpn_postgres; then
    echo "❌ Контейнер postgres не запущен. Запустите: docker-compose up -d postgres"
    exit 1
fi

# Запускаем скрипт в контейнере api (если он запущен) или создаем временный контейнер
if docker ps | grep -q zephyrvpn_api; then
    echo "✅ Используем существующий контейнер api"
    docker exec -e DATABASE_URL="postgres://pizza_admin:pizza_secure_pass_2024@postgres:5432/pizza_db?sslmode=disable" \
        zephyrvpn_api \
        go run /app/scripts/add_branch_vilskogo.go
else
    echo "✅ Создаем временный контейнер для запуска скрипта"
    docker run --rm \
        --network zephyrvpn_default \
        -v "$(pwd):/app" \
        -w /app \
        -e DATABASE_URL="postgres://pizza_admin:pizza_secure_pass_2024@postgres:5432/pizza_db?sslmode=disable" \
        golang:1.24-alpine \
        sh -c "apk add --no-cache git && go mod download && go run scripts/add_branch_vilskogo.go"
fi

if [ $? -eq 0 ]; then
    echo "✅ Филиал успешно добавлен!"
else
    echo "❌ Ошибка при добавлении филиала"
    exit 1
fi

