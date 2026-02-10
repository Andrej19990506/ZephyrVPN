@echo off
REM Скрипт для применения миграции 016_fix_staff_user_id.sql
REM Исправляет проблему с NULL user_id в таблице staff

echo Применение миграции 016_fix_staff_user_id.sql...
echo.

REM Проверяем, какой контейнер PostgreSQL используется
docker ps --filter "name=postgres" --format "{{.Names}}" > temp_containers.txt
set /p CONTAINER_NAME=<temp_containers.txt
del temp_containers.txt

if "%CONTAINER_NAME%"=="" (
    echo Ошибка: Контейнер PostgreSQL не найден
    echo Доступные контейнеры:
    docker ps --format "{{.Names}}"
    pause
    exit /b 1
)

echo Найден контейнер: %CONTAINER_NAME%
echo.

REM Применяем миграцию
echo Применяем миграцию...
echo Используем: пользователь=pizza_admin, база=pizza_db
docker exec -i %CONTAINER_NAME% psql -U pizza_admin -d pizza_db < migrations\016_fix_staff_user_id.sql

if %ERRORLEVEL% EQU 0 (
    echo.
    echo ✅ Миграция успешно применена!
    echo Теперь можно перезапустить сервер.
) else (
    echo.
    echo ❌ Ошибка при применении миграции
    echo Код ошибки: %ERRORLEVEL%
)

pause

