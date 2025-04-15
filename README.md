# Сервис мониторинга погоды

Сервис на Go, который проверяет показатель "Порывы ветра" по OpenWeatherMap API и отправляет уведомление на электронную почту через Microsoft Exchange, если порывы ветра превышают 15 м/с.

## Требования

- Go 1.21 или выше
- Доступ к OpenWeatherMap API (бесплатный или платный ключ)
- Доступ к Microsoft Exchange серверу для отправки электронной почты

## Установка

1. Клонировать репозиторий:
   ```bash
   git clone <URL репозитория>
   cd WeatherMapAPI
   ```

2. Установить зависимости:
   ```bash
   go mod tidy
   ```

3. Создать файл `.env` на основе `.env.example` и заполнить его своими данными:
   ```bash
   cp .env.example .env
   ```

4. Настроить переменные окружения в файле `.env`:
   - `OPENWEATHER_API_KEY` - ключ API OpenWeatherMap
   - `CITY` - город для проверки погоды (формат: `Город,Код_страны`, например: `Moscow,RU`)
   - `EMAIL_FROM` - адрес отправителя
   - `EMAIL_TO` - адрес получателя
   - `SMTP_SERVER` - адрес SMTP сервера (mail.agroconcern.ru)
   - `SMTP_PORT` - порт SMTP сервера (обычно 587 для TLS)
   - `SMTP_USER` - имя пользователя для SMTP
   - `SMTP_PASSWORD` - пароль для SMTP

## Запуск

```bash
go run main.go
```

Для запуска в фоновом режиме (для продакшен среды) можно использовать системные средства, такие как `systemd` или `supervisord`.

## Принцип работы

1. При запуске сервис загружает конфигурацию из переменных окружения
2. Выполняет проверку прогноза погоды через OpenWeatherMap API
3. Если порывы ветра превышают 15 м/с, отправляет уведомление по электронной почте
4. Повторяет проверку каждые 3 часа

## Настройка сервиса в systemd (Linux)

Для запуска сервиса как systemd службы, создайте файл `/etc/systemd/system/weather-alert.service`:

```
[Unit]
Description=Weather Alert Service
After=network.target

[Service]
ExecStart=/path/to/weather-alert
WorkingDirectory=/path/to/
User=serviceuser
Group=serviceuser
Restart=always
RestartSec=5
StandardOutput=syslog
StandardError=syslog
SyslogIdentifier=weather-alert

[Install]
WantedBy=multi-user.target
```

Затем выполните:

```bash
sudo systemctl enable weather-alert.service
sudo systemctl start weather-alert.service
``` 