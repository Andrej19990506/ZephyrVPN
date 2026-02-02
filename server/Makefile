.PHONY: run build test docker-up docker-down migrate

# Запуск сервера
run:
	go run main.go

# Сборка
build:
	go build -o server main.go

# Тесты
test:
	go test ./...

# Запуск Docker Compose
docker-up:
	docker-compose up -d

# Остановка Docker Compose
docker-down:
	docker-compose down

# Миграция базы данных (запускается автоматически при старте)
migrate:
	go run main.go

# Установка зависимостей
deps:
	go mod download
	go mod tidy

# Генерация protobuf файлов (через Docker)
proto:
	docker run --rm -v "%cd%:/app" -w /app golang:1.23-alpine sh -c "apk add --no-cache protoc protobuf-dev && go install google.golang.org/protobuf/cmd/protoc-gen-go@latest && go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest && protoc --go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative internal/proto/order.proto"

