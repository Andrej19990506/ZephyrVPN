#!/bin/bash

# Скрипт для автоматической установки WireGuard на Ubuntu/Debian сервер
# Использование: ./setup-wireguard-server.sh

set -e

echo "🚀 Установка WireGuard сервера для ZephyrVPN"

# Проверка root прав
if [ "$EUID" -ne 0 ]; then 
    echo "❌ Пожалуйста, запустите скрипт от root или с sudo"
    exit 1
fi

# Обновление системы
echo "📦 Обновление системы..."
apt update && apt upgrade -y

# Установка WireGuard
echo "📦 Установка WireGuard..."
apt install -y wireguard wireguard-tools qrencode

# Включение IP forwarding
echo "🔧 Настройка IP forwarding..."
echo "net.ipv4.ip_forward=1" >> /etc/sysctl.conf
sysctl -p

# Создание директории для конфигов
mkdir -p /etc/wireguard
cd /etc/wireguard

# Генерация ключей сервера
echo "🔑 Генерация ключей сервера..."
wg genkey | tee server_private.key | wg pubkey > server_public.key
chmod 600 server_private.key

# Получение приватного и публичного ключей
SERVER_PRIVATE_KEY=$(cat server_private.key)
SERVER_PUBLIC_KEY=$(cat server_public.key)

# Получение IP адреса сервера
SERVER_IP=$(curl -s ifconfig.me || hostname -I | awk '{print $1}')

# Определение сетевого интерфейса
echo "🔍 Определение сетевого интерфейса..."
INTERFACE=$(ip route | grep default | awk '{print $5}' | head -n1)
if [ -z "$INTERFACE" ]; then
    INTERFACE="eth0"  # Fallback для старых систем
fi
echo "   Используется интерфейс: $INTERFACE"

# Создание конфига сервера
echo "📝 Создание конфига сервера..."
cat > /etc/wireguard/wg0.conf <<EOF
[Interface]
PrivateKey = $SERVER_PRIVATE_KEY
Address = 10.0.0.1/24
ListenPort = 51820
PostUp = iptables -A FORWARD -i wg0 -j ACCEPT; iptables -A FORWARD -o wg0 -j ACCEPT; iptables -t nat -A POSTROUTING -o $INTERFACE -j MASQUERADE
PostDown = iptables -D FORWARD -i wg0 -j ACCEPT; iptables -D FORWARD -o wg0 -j ACCEPT; iptables -t nat -D POSTROUTING -o $INTERFACE -j MASQUERADE
EOF

# Запуск WireGuard
echo "🚀 Запуск WireGuard..."
systemctl enable wg-quick@wg0
systemctl start wg-quick@wg0

# Настройка firewall (если установлен ufw)
if command -v ufw &> /dev/null; then
    echo "🔥 Настройка firewall..."
    ufw allow 51820/udp
    ufw allow 22/tcp
    ufw --force enable
fi

echo ""
echo "✅ WireGuard сервер установлен и запущен!"
echo ""
echo "📋 Информация о сервере:"
echo "   Публичный ключ: $SERVER_PUBLIC_KEY"
echo "   IP адрес: $SERVER_IP"
echo "   Порт: 51820"
echo ""
echo "💡 Добавьте эту информацию в базу данных ZephyrVPN:"
echo "   UPDATE vpn_servers SET public_key = '$SERVER_PUBLIC_KEY' WHERE host = '$SERVER_IP';"
echo ""
echo "📖 Для добавления клиентов используйте:"
echo "   wg set wg0 peer <CLIENT_PUBLIC_KEY> allowed-ips 10.0.0.2/32"
echo ""

