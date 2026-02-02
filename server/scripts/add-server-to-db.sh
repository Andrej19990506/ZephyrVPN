#!/bin/bash

# Скрипт для добавления WireGuard сервера в базу данных ZephyrVPN
# Использование: ./add-server-to-db.sh <SERVER_NAME> <SERVER_IP> <PUBLIC_KEY> [COUNTRY] [PORT]

set -e

if [ "$#" -lt 3 ]; then
    echo "Использование: $0 <SERVER_NAME> <SERVER_IP> <PUBLIC_KEY> [COUNTRY] [PORT]"
    echo "Пример: $0 'US Server 1' '1.2.3.4' 'PUBLIC_KEY_HERE' 'US' 51820"
    exit 1
fi

SERVER_NAME=$1
SERVER_IP=$2
PUBLIC_KEY=$3
COUNTRY=${4:-"US"}
PORT=${5:-51820}

# Определяем флаг страны
case $COUNTRY in
    US) FLAG="🇺🇸" ;;
    GB|UK) FLAG="🇬🇧" ;;
    DE) FLAG="🇩🇪" ;;
    FR) FLAG="🇫🇷" ;;
    NL) FLAG="🇳🇱" ;;
    JP) FLAG="🇯🇵" ;;
    SG) FLAG="🇸🇬" ;;
    *) FLAG="🌐" ;;
esac

echo "📝 Добавление сервера в базу данных..."
echo "   Имя: $SERVER_NAME"
echo "   IP: $SERVER_IP"
echo "   Порт: $PORT"
echo "   Страна: $COUNTRY"
echo ""

# SQL запрос для добавления сервера
SQL="INSERT INTO vpn_servers (name, country, flag, host, port, protocol, is_active, public_key, created_at, updated_at) 
VALUES ('$SERVER_NAME', '$COUNTRY', '$FLAG', '$SERVER_IP', $PORT, 'udp', true, '$PUBLIC_KEY', NOW(), NOW())
ON CONFLICT (host, port) 
DO UPDATE SET 
    name = EXCLUDED.name,
    country = EXCLUDED.country,
    flag = EXCLUDED.flag,
    public_key = EXCLUDED.public_key,
    updated_at = NOW();"

# Проверяем, используется ли Docker Compose
if [ -f "docker-compose.yml" ]; then
    echo "🐳 Использование Docker Compose..."
    docker-compose exec -T postgres psql -U zephyrvpn -d zephyrvpn -c "$SQL"
else
    echo "💾 Прямое подключение к PostgreSQL..."
    # Настройте эти переменные под вашу среду
    PGHOST=${PGHOST:-localhost}
    PGPORT=${PGPORT:-5432}
    PGUSER=${PGUSER:-zephyrvpn}
    PGDATABASE=${PGDATABASE:-zephyrvpn}
    
    psql -h $PGHOST -p $PGPORT -U $PGUSER -d $PGDATABASE -c "$SQL"
fi

echo ""
echo "✅ Сервер добавлен в базу данных!"
echo ""
echo "📋 Проверка добавленного сервера:"
if [ -f "docker-compose.yml" ]; then
    docker-compose exec -T postgres psql -U zephyrvpn -d zephyrvpn -c "SELECT id, name, host, port, country, public_key FROM vpn_servers WHERE host = '$SERVER_IP';"
else
    psql -h $PGHOST -p $PGPORT -U $PGUSER -d $PGDATABASE -c "SELECT id, name, host, port, country, public_key FROM vpn_servers WHERE host = '$SERVER_IP';"
fi

