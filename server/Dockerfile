FROM golang:1.24-alpine AS builder
WORKDIR /app

# Настраиваем Go proxy: используем альтернативные прокси для ускорения
# goproxy.io - быстрый альтернативный прокси, fallback на direct
ENV GOPROXY=https://goproxy.io,direct
ENV GOSUMDB=off
ENV GOPRIVATE=
ENV GOTIMEOUT=600s
ENV GO111MODULE=on

RUN apk add --no-cache git ca-certificates

# 1. Копируем go.mod и go.sum сначала (для кеширования слоя)
COPY go.mod go.sum ./

# 2. Скачиваем зависимости (с таймаутом и retry)
RUN go mod download || (sleep 5 && go mod download) || (sleep 10 && go mod download)

# 3. Копируем ВООБЩЕ ВСЁ остальное
COPY . .

# 4. Проверяем go.sum (пропускаем go mod tidy для избежания проблем с proxy)
# RUN go mod tidy || (sleep 2 && go mod tidy) || (sleep 5 && go mod tidy)
RUN go mod verify

# 5. Качаем либы еще раз (на случай если что-то добавилось)
RUN go mod download || go mod download

# 6. Собираем
RUN CGO_ENABLED=0 GOOS=linux go build -o main ./main.go

FROM alpine:latest
# Устанавливаем ca-certificates для HTTPS запросов
RUN apk --no-cache add ca-certificates tzdata
WORKDIR /app
COPY --from=builder /app/main /app/main
# Railway автоматически устанавливает PORT, но оставляем 8080 как fallback
EXPOSE 8080
# Используем exec form для правильной обработки сигналов
ENTRYPOINT ["/app/main"]
