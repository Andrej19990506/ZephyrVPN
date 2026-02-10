#!/bin/bash
# Скрипт для безопасной очистки топика pizza-orders
# ВНИМАНИЕ: Это удалит ВСЕ сообщения из топика!

set -e

KAFKA_CONTAINER="zephyrvpn_kafka"
TOPIC="pizza-orders"
BOOTSTRAP_SERVER="localhost:9092"

echo "⚠️  ВНИМАНИЕ: Этот скрипт удалит ВСЕ сообщения из топика $TOPIC"
echo "   Убедитесь, что все consumer'ы остановлены!"
echo ""
read -p "Продолжить? (yes/no): " confirm

if [ "$confirm" != "yes" ]; then
    echo "❌ Операция отменена"
    exit 1
fi

echo ""
echo "🗑️  Шаг 1: Установка retention на 1 секунду (1000 ms)..."
docker exec -it $KAFKA_CONTAINER kafka-configs \
  --bootstrap-server $BOOTSTRAP_SERVER \
  --entity-type topics \
  --entity-name $TOPIC \
  --alter \
  --add-config retention.ms=1000

echo ""
echo "⏳ Шаг 2: Ожидание удаления сообщений (это может занять 1-5 минут)..."
echo "   Проверяем количество сообщений каждые 10 секунд..."

# Ждем, пока Kafka удалит все сообщения
for i in {1..30}; do
    sleep 10
    count=$(docker exec -it $KAFKA_CONTAINER kafka-run-class kafka.tools.GetOffsetShell \
      --bootstrap-server $BOOTSTRAP_SERVER \
      --topic $TOPIC \
      --time -1 2>/dev/null | awk -F: '{sum += $3} END {print sum}')
    
    echo "   Попытка $i/30: сообщений в топике: $count"
    
    if [ "$count" = "0" ] || [ -z "$count" ]; then
        echo "✅ Все сообщения удалены!"
        break
    fi
done

echo ""
echo "🔄 Шаг 3: Восстановление нормального retention (24 часа)..."
docker exec -it $KAFKA_CONTAINER kafka-configs \
  --bootstrap-server $BOOTSTRAP_SERVER \
  --entity-type topics \
  --entity-name $TOPIC \
  --alter \
  --add-config retention.ms=86400000

echo ""
echo "✅ Очистка топика завершена!"
echo ""
echo "🔍 Финальная проверка:"
docker exec -it $KAFKA_CONTAINER kafka-run-class kafka.tools.GetOffsetShell \
  --bootstrap-server $BOOTSTRAP_SERVER \
  --topic $TOPIC \
  --time -1






