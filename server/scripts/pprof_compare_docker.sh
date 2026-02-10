#!/bin/bash
# Скрипт для сравнения снимков памяти через Docker

echo "🔍 Сравнение снимков памяти (Docker)"
echo "======================================"
echo ""

if [ ! -f "heap_before.pb.gz" ]; then
    echo "❌ Файл heap_before.pb.gz не найден. Сначала запустите pprof_docker.sh"
    exit 1
fi

# Определяем контейнер
CONTAINER_NAME=$(docker ps --format "{{.Names}}" | grep -i server | head -1)

if [ -z "$CONTAINER_NAME" ]; then
    echo "❌ Контейнер сервера не найден"
    exit 1
fi

echo "✅ Используется контейнер: $CONTAINER_NAME"
echo ""

echo "1. Создание второго снимка памяти (heap_after.pb.gz)..."
go tool pprof -proto http://localhost:6060/debug/pprof/heap > heap_after.pb.gz 2>&1
if [ $? -eq 0 ]; then
    echo "✅ Снимок создан: heap_after.pb.gz"
else
    echo "❌ Ошибка создания снимка"
    exit 1
fi

echo ""
echo "2. Сравнение снимков (что увеличилось):"
go tool pprof -base heap_before.pb.gz -top heap_after.pb.gz 2>&1 | head -30

echo ""
echo "3. Разница в размере (cumulative):"
go tool pprof -base heap_before.pb.gz -top -cum heap_after.pb.gz 2>&1 | head -30

echo ""
echo "4. Текущие логи памяти из контейнера:"
docker logs $CONTAINER_NAME 2>&1 | grep "💾 Memory Stats" | tail -5

echo ""
echo "✅ Для детального анализа запустите:"
echo "   go tool pprof -base heap_before.pb.gz heap_after.pb.gz"
echo ""
echo "   В интерактивном режиме используйте команды:"
echo "   - top10        # Топ 10 функций по росту памяти"
echo "   - list <func>  # Показать код функции"
echo "   - web          # Визуализация (требует graphviz)"
echo ""
echo "   Для просмотра графа в браузере:"
echo "   - Установите graphviz: sudo apt-get install graphviz (Linux) или brew install graphviz (Mac)"
echo "   - В pprof выполните: web"


