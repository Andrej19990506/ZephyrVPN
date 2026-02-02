@echo off
REM Скрипт для запуска add_branch_vilskogo.go через Docker
REM Использование: run_add_branch_docker.bat

echo 🔄 Запуск скрипта добавления филиала через Docker...

REM Переходим в директорию проекта
cd /d %~dp0\..

REM Проверяем, запущен ли контейнер postgres
docker ps | findstr zephyrvpn_postgres >nul
if %ERRORLEVEL% NEQ 0 (
    echo ❌ Контейнер postgres не запущен. Запустите: docker-compose up -d postgres
    exit /b 1
)

REM Создаем временный контейнер для запуска скрипта
echo ✅ Создаем временный контейнер для запуска скрипта
REM Используем docker-compose run для правильной сети
docker-compose run --rm -e DATABASE_URL=postgres://pizza_admin:pizza_secure_pass_2024@postgres:5432/pizza_db?sslmode=disable api sh -c "apk add --no-cache git && go mod download && go run scripts/add_branch_vilskogo.go"

if %ERRORLEVEL% EQU 0 (
    echo ✅ Филиал успешно добавлен!
) else (
    echo ❌ Ошибка при добавлении филиала
    exit /b %ERRORLEVEL%
)

