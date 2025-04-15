FROM golang:1.21-alpine AS builder

WORKDIR /app

# Копирование модулей Go и загрузка зависимостей
COPY go.mod go.sum* ./
RUN go mod download

# Копирование исходного кода
COPY *.go ./

# Сборка бинарного файла
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o weather-alert

# Создание финального образа
FROM alpine:latest

WORKDIR /app

# Копирование бинарного файла из builder
COPY --from=builder /app/weather-alert .

# Создание примера .env файла
COPY .env.example .env.example

# Настройка переменных окружения
ENV OPENWEATHER_API_KEY=""
ENV CITY="Moscow,RU"
ENV EMAIL_FROM=""
ENV EMAIL_TO=""
ENV SMTP_SERVER="mail.agroconcern.ru"
ENV SMTP_PORT="587"
ENV SMTP_USER=""
ENV SMTP_PASSWORD=""

# Запуск приложения
CMD ["./weather-alert"] 