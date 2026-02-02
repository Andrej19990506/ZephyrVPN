#!/bin/bash

# Скрипт для добавления клиента WireGuard
# Использование: ./add-wireguard-client.sh <CLIENT_NAME> <CLIENT_IP>

set -e

if [ "$#" -ne 2 ]; then
    echo "Использование: $0 <CLIENT_NAME> <CLIENT_IP>"
    echo "Пример: $0 user1 10.0.0.2"
    exit 1
fi

CLIENT_NAME=$1
CLIENT_IP=$2

# Проверка root прав
if [ "$EUID" -ne 0 ]; then 
    echo "❌ Пожалуйста, запустите скрипт от root или с sudo"
    exit 1
fi

# Генерация ключей клиента
echo "🔑 Генерация ключей для клиента $CLIENT_NAME..."
CLIENT_PRIVATE_KEY=$(wg genkey)
CLIENT_PUBLIC_KEY=$(echo $CLIENT_PRIVATE_KEY | wg pubkey)

# Получение информации о сервере
SERVER_PRIVATE_KEY=$(cat /etc/wireguard/server_private.key)
SERVER_PUBLIC_KEY=$(cat /etc/wireguard/server_public.key)
SERVER_IP=$(curl -s ifconfig.me || hostname -I | awk '{print $1}')
SERVER_PORT=51820

# Добавление клиента в WireGuard
echo "➕ Добавление клиента в WireGuard..."
wg set wg0 peer $CLIENT_PUBLIC_KEY allowed-ips $CLIENT_IP/32

# Создание конфига клиента
echo "📝 Создание конфига клиента..."
mkdir -p /etc/wireguard/clients
cat > /etc/wireguard/clients/${CLIENT_NAME}.conf <<EOF
[Interface]
PrivateKey = $CLIENT_PRIVATE_KEY
Address = $CLIENT_IP/24
DNS = 1.1.1.1, 8.8.8.8

[Peer]
PublicKey = $SERVER_PUBLIC_KEY
Endpoint = $SERVER_IP:$SERVER_PORT
AllowedIPs = 0.0.0.0/0
PersistentKeepalive = 25
EOF

echo ""
echo "✅ Клиент $CLIENT_NAME добавлен!"
echo ""
echo "📋 Информация о клиенте:"
echo "   Имя: $CLIENT_NAME"
echo "   IP: $CLIENT_IP"
echo "   Приватный ключ: $CLIENT_PRIVATE_KEY"
echo "   Публичный ключ: $CLIENT_PUBLIC_KEY"
echo ""
echo "📄 Конфиг сохранен в: /etc/wireguard/clients/${CLIENT_NAME}.conf"
echo ""
echo "💡 Добавьте эту информацию в базу данных ZephyrVPN:"
echo "   INSERT INTO users (email, wireguard_private_key, wireguard_public_key) VALUES ('$CLIENT_NAME@example.com', '$CLIENT_PRIVATE_KEY', '$CLIENT_PUBLIC_KEY');"
echo ""

