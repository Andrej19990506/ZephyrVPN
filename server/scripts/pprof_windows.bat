@echo off
REM Скрипт для анализа памяти с помощью pprof (Windows)

echo 🔍 Диагностика утечек памяти с помощью pprof
echo ==============================================
echo.

REM Проверка доступности pprof
echo 1. Проверка доступности pprof сервера...
curl -s http://localhost:6060/debug/pprof/ >nul 2>&1
if %errorlevel% equ 0 (
    echo ✅ pprof сервер доступен на http://localhost:6060
) else (
    echo ❌ pprof сервер недоступен. Убедитесь, что сервер запущен.
    exit /b 1
)

echo.
echo 2. Создание первого снимка памяти (heap_before.pb.gz)...
go tool pprof -proto http://localhost:6060/debug/pprof/heap > heap_before.pb.gz 2>&1
if %errorlevel% equ 0 (
    echo ✅ Снимок создан: heap_before.pb.gz
) else (
    echo ❌ Ошибка создания снимка
    exit /b 1
)

echo.
echo 3. Текущая статистика памяти:
go tool pprof -top http://localhost:6060/debug/pprof/heap 2>&1 | more

echo.
echo 4. Информация о горутинах:
go tool pprof -top http://localhost:6060/debug/pprof/goroutine 2>&1 | more

echo.
echo ⏳ Подождите 5 минут, затем запустите:
echo    scripts\pprof_compare_windows.bat
echo.
echo Или для интерактивного режима:
echo    go tool pprof http://localhost:6060/debug/pprof/heap

pause


