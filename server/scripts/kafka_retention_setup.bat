@echo off
REM Скрипт для настройки Kafka Retention Policy для топика pizza-orders (Windows)

set KAFKA_CONTAINER=zephyrvpn_kafka
set TOPIC=pizza-orders
set BOOTSTRAP_SERVER=localhost:9092

echo 🔍 Проверка текущих настроек retention для топика %TOPIC%...
echo.

docker exec -it %KAFKA_CONTAINER% kafka-configs --bootstrap-server %BOOTSTRAP_SERVER% --entity-type topics --entity-name %TOPIC% --describe

echo.
echo ⚙️  Установка retention policy:
echo    - retention.ms = 86400000 (24 часа)
echo    - retention.bytes = 5368709120 (5GB)
echo.

docker exec -it %KAFKA_CONTAINER% kafka-configs --bootstrap-server %BOOTSTRAP_SERVER% --entity-type topics --entity-name %TOPIC% --alter --add-config retention.ms=86400000,retention.bytes=5368709120

echo.
echo ✅ Retention policy установлена!
echo.
echo 🔍 Проверка применения настроек...
echo.

docker exec -it %KAFKA_CONTAINER% kafka-configs --bootstrap-server %BOOTSTRAP_SERVER% --entity-type topics --entity-name %TOPIC% --describe

echo.
echo 📊 Текущее количество сообщений в топике:
docker exec -it %KAFKA_CONTAINER% kafka-run-class kafka.tools.GetOffsetShell --bootstrap-server %BOOTSTRAP_SERVER% --topic %TOPIC% --time -1

echo.
echo ℹ️  Kafka автоматически удалит старые сообщения при следующем запуске фоновой задачи (обычно каждые 5 минут)
pause






