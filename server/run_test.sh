#!/bin/bash

echo "🚀 Запуск нагрузочного теста через Docker"
echo ""
echo "Убедитесь что сервер запущен: docker-compose up"
echo ""
read -p "Нажмите Enter для продолжения..."

docker run --rm -v "$(pwd)":/app -w /app --network host golang:1.23-alpine go run bomb.go

