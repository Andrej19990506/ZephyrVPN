@echo off
REM Простой скрипт для добавления филиала через psql в Docker
echo 🔄 Добавление филиала "Вильского 34" через Docker...

REM Выполняем SQL скрипт в контейнере postgres
docker exec -i zephyrvpn_postgres psql -U pizza_admin -d pizza_db < migrations\002_add_branch_vilskogo.sql

if %ERRORLEVEL% EQU 0 (
    echo ✅ Филиал успешно добавлен!
) else (
    echo ❌ Ошибка при добавлении филиала
    exit /b %ERRORLEVEL%
)

