package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/wneessen/go-mail"
)

// HTML шаблон для письма с предупреждением
const emailHTMLTemplate = `<!DOCTYPE html>
<html lang="ru">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Уведомление о погоде</title>
    <style>
        body {
            font-family: Arial, sans-serif;
            background-color: #f4f4f4;
            margin: 0;
            padding: 20px;
        }
        .container {
            max-width: 600px;
            margin: 0 auto;
            background-color: #ffffff;
            padding: 20px;
            border-radius: 8px;
            box-shadow: 0 0 10px rgba(0, 0, 0, 0.1);
        }
        h1 {
            color: #d9534f;
            font-size: 24px;
            text-align: center;
        }
        p {
            font-size: 16px;
            line-height: 1.5;
            color: #333333;
        }
        .highlight {
            font-weight: bold;
            color: #d9534f;
        }
        .footer {
            margin-top: 20px;
            font-size: 14px;
            color: #777777;
            text-align: center;
        }
        ul {
            margin-left: 20px;
        }
        li {
            margin-bottom: 5px;
        }
        @media only screen and (max-width: 600px) {
            .container {
                padding: 10px;
            }
            h1 {
                font-size: 20px;
            }
            p {
                font-size: 14px;
            }
        }
    </style>
</head>
<body>
    <div class="container">
        <h1>Внимание!</h1>
        <p>Сегодня ожидаются <span class="highlight">сильные порывы ветра</span> до <span class="highlight">%.2f м/с</span>, что превышает безопасный порог (%.2f м/с).</p>
        %s
        <p>Рекомендуется <span class="highlight">не открывать окна в офисе</span> в течение дня.</p>
        <div class="footer">
            <p>Это автоматическое уведомление от системы мониторинга погоды.</p>
        </div>
    </div>
</body>
</html>`

// Структура для хранения времени прогноза с сильным ветром
type WindGustForecast struct {
	Time     time.Time
	WindGust float64
}

// Конфигурация приложения
type Config struct {
	OpenWeatherAPIKey string
	City              string
	EmailFrom         string
	EmailTo           []string
	SMTPServer        string
	SMTPPort          string
	SMTPUser          string
	SMTPPassword      string
	WindGustThreshold float64 // Пороговое значение порывов ветра в м/с
	NotificationHour  int     // Час отправки уведомления
	NotificationMin   int     // Минуты отправки уведомления
}

// Структуры для парсинга ответа от OpenWeatherMap API
type WeatherResponse struct {
	List []DailyForecast `json:"list"`
}

type DailyForecast struct {
	Dt   int64 `json:"dt"`
	Main struct {
		Temp float64 `json:"temp"`
	} `json:"main"`
	Wind struct {
		Speed float64 `json:"speed"`
		Gust  float64 `json:"gust"`
	} `json:"wind"`
	Weather []WeatherDesc `json:"weather"`
}

type WeatherDesc struct {
	Main        string `json:"main"`
	Description string `json:"description"`
}

// Структура для Geocoding API
type GeoLocation struct {
	Name    string  `json:"name"`
	Lat     float64 `json:"lat"`
	Lon     float64 `json:"lon"`
	Country string  `json:"country"`
	State   string  `json:"state"`
}

// Загрузка конфигурации из переменных окружения
func loadConfig() (*Config, error) {
	err := godotenv.Load()
	if err != nil {
		log.Println("Предупреждение: Файл .env не найден, используются переменные окружения системы")
	}

	// Получение списка адресов из строки, разделенной запятыми
	emailToStr := os.Getenv("EMAIL_TO")
	var emailTo []string
	if emailToStr != "" {
		emailTo = []string{emailToStr}
	}

	// Настройки порога ветра и времени уведомления с значениями по умолчанию
	windGustThreshold := 15.0 // По умолчанию 15 м/с
	notificationHour := 9     // По умолчанию 9 часов
	notificationMin := 0      // По умолчанию 0 минут

	// Загрузка значений из переменных окружения, если они указаны
	if envThreshold := os.Getenv("WIND_GUST_THRESHOLD"); envThreshold != "" {
		if val, err := strconv.ParseFloat(envThreshold, 64); err == nil {
			windGustThreshold = val
		} else {
			log.Printf("Ошибка парсинга WIND_GUST_THRESHOLD: %v, используется значение по умолчанию", err)
		}
	}

	if envHour := os.Getenv("NOTIFICATION_HOUR"); envHour != "" {
		if val, err := strconv.Atoi(envHour); err == nil && val >= 0 && val < 24 {
			notificationHour = val
		} else {
			log.Printf("Ошибка парсинга NOTIFICATION_HOUR: %v, используется значение по умолчанию", err)
		}
	}

	if envMin := os.Getenv("NOTIFICATION_MIN"); envMin != "" {
		if val, err := strconv.Atoi(envMin); err == nil && val >= 0 && val < 60 {
			notificationMin = val
		} else {
			log.Printf("Ошибка парсинга NOTIFICATION_MIN: %v, используется значение по умолчанию", err)
		}
	}

	config := &Config{
		OpenWeatherAPIKey: os.Getenv("OPENWEATHER_API_KEY"),
		City:              os.Getenv("CITY"),
		EmailFrom:         os.Getenv("EMAIL_FROM"),
		EmailTo:           emailTo,
		SMTPServer:        os.Getenv("SMTP_SERVER"),
		SMTPPort:          os.Getenv("SMTP_PORT"),
		SMTPUser:          os.Getenv("SMTP_USER"),
		SMTPPassword:      os.Getenv("SMTP_PASSWORD"),
		WindGustThreshold: windGustThreshold,
		NotificationHour:  notificationHour,
		NotificationMin:   notificationMin,
	}

	// Проверка обязательных полей
	if config.OpenWeatherAPIKey == "" {
		return nil, fmt.Errorf("не указан API ключ для OpenWeatherMap")
	}
	if config.City == "" {
		return nil, fmt.Errorf("не указан город для проверки погоды")
	}
	if len(config.EmailTo) == 0 {
		return nil, fmt.Errorf("не указаны адреса получателей")
	}
	if config.SMTPServer == "" || config.SMTPPort == "" {
		return nil, fmt.Errorf("не указаны настройки SMTP сервера")
	}

	return config, nil
}

// Получение координат города с помощью Geocoding API
func getGeoCoordinates(config *Config) (*GeoLocation, error) {
	url := fmt.Sprintf("http://api.openweathermap.org/geo/1.0/direct?q=%s&limit=1&appid=%s",
		config.City, config.OpenWeatherAPIKey)

	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("ошибка при запросе к Geocoding API: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("ошибка при чтении ответа: %w", err)
	}

	var locations []GeoLocation
	if err := json.Unmarshal(body, &locations); err != nil {
		return nil, fmt.Errorf("ошибка при разборе JSON: %w", err)
	}

	if len(locations) == 0 {
		return nil, fmt.Errorf("не найдены координаты для города: %s", config.City)
	}

	return &locations[0], nil
}

// Получение данных о погоде по координатам
func getWeatherData(config *Config) (*WeatherResponse, error) {
	// Получаем координаты города
	location, err := getGeoCoordinates(config)
	if err != nil {
		return nil, fmt.Errorf("ошибка при получении координат: %w", err)
	}

	log.Printf("Получены координаты для %s: широта %.4f, долгота %.4f",
		location.Name, location.Lat, location.Lon)

	// Используем координаты для запроса прогноза погоды
	url := fmt.Sprintf("https://api.openweathermap.org/data/2.5/forecast?lat=%.4f&lon=%.4f&units=metric&appid=%s",
		location.Lat, location.Lon, config.OpenWeatherAPIKey)

	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("ошибка при запросе к API: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("ошибка при чтении ответа: %w", err)
	}

	var weatherData WeatherResponse
	if err := json.Unmarshal(body, &weatherData); err != nil {
		return nil, fmt.Errorf("ошибка при разборе JSON: %w", err)
	}

	return &weatherData, nil
}

// Отправка электронного письма через Microsoft Exchange с использованием библиотеки go-mail
func sendEmail(config *Config, subject, htmlBody, plainTextBody string) error {
	// Создание нового сообщения
	msg := mail.NewMsg()
	if err := msg.FromFormat("Система мониторинга погоды", config.EmailFrom); err != nil {
		return fmt.Errorf("ошибка при указании отправителя: %w", err)
	}

	// Добавление получателей
	for _, recipient := range config.EmailTo {
		if err := msg.To(recipient); err != nil {
			return fmt.Errorf("ошибка при указании получателя %s: %w", recipient, err)
		}
	}

	// Установка темы письма
	msg.Subject(subject)

	// Установка HTML тела письма и текстовой альтернативы
	msg.SetBodyString(mail.TypeTextHTML, htmlBody)
	msg.AddAlternativeString(mail.TypeTextPlain, plainTextBody)

	// Установка кодировки для поддержки кириллицы
	msg.SetCharset(mail.CharsetUTF8)

	// Парсинг порта
	portInt, err := strconv.Atoi(config.SMTPPort)
	if err != nil {
		return fmt.Errorf("ошибка при парсинге порта: %w", err)
	}

	// Создание клиента с различными опциями для Microsoft Exchange
	client, err := mail.NewClient(config.SMTPServer,
		mail.WithPort(portInt),
		mail.WithSMTPAuth(mail.SMTPAuthLogin), // Microsoft Exchange часто требует LOGIN аутентификацию
		mail.WithUsername(config.SMTPUser),
		mail.WithPassword(config.SMTPPassword),
		mail.WithTLSPolicy(mail.TLSOpportunistic), // Пробуем STARTTLS, но продолжаем без него если не поддерживается
		mail.WithTimeout(30*time.Second),          // Увеличенный таймаут
	)
	if err != nil {
		return fmt.Errorf("ошибка при создании клиента: %w", err)
	}

	// Включаем отладочный режим
	client.SetDebugLog(true)

	// Отправка письма с контекстом для возможности отмены при длительных операциях
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := client.DialAndSendWithContext(ctx, msg); err != nil {
		return fmt.Errorf("ошибка при отправке письма: %w", err)
	}

	return nil
}

// Проверка прогноза погоды на весь день и поиск сильных порывов ветра
func checkWeatherForTheDay(weatherData *WeatherResponse, threshold float64) (bool, []WindGustForecast) {
	now := time.Now()
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	endOfDay := startOfDay.Add(19 * time.Hour)

	var forecasts []WindGustForecast
	exceedsThreshold := false

	for _, forecast := range weatherData.List {
		// Преобразуем время прогноза
		forecastTime := time.Unix(forecast.Dt, 0)

		// Проверяем, что прогноз относится к текущему дню
		if forecastTime.After(startOfDay) && forecastTime.Before(endOfDay) {
			windGust := forecast.Wind.Gust

			log.Printf("Прогноз на %s: порывы ветра %.2f м/с\n",
				forecastTime.Format("15:04"), windGust)

			// Если порывы ветра превышают порог
			if windGust > threshold {
				exceedsThreshold = true
				forecasts = append(forecasts, WindGustForecast{
					Time:     forecastTime,
					WindGust: windGust,
				})
			}
		}
	}

	// Сортируем прогнозы по силе ветра (необязательно)
	// sort.Slice(forecasts, func(i, j int) bool {
	//     return forecasts[i].WindGust > forecasts[j].WindGust
	// })

	return exceedsThreshold, forecasts
}

// Формирование HTML-блока с временными интервалами сильного ветра
func formatWindGustTimeHTML(forecasts []WindGustForecast) string {
	if len(forecasts) == 0 {
		return ""
	}

	// Если ветер дует весь день, просто указываем на это
	if len(forecasts) > 6 {
		return "<p>Сильные порывы ветра ожидаются <span class=\"highlight\">в течение всего дня</span>.</p>"
	}

	var builder strings.Builder
	builder.WriteString("<p>Сильные порывы ветра ожидаются в следующие часы:</p>")
	builder.WriteString("<ul>")

	for _, forecast := range forecasts {
		timeStr := forecast.Time.Format("15:04")
		builder.WriteString(fmt.Sprintf("<li>%s - <span class=\"highlight\">%.2f м/с</span></li>",
			timeStr, forecast.WindGust))
	}

	builder.WriteString("</ul>")
	return builder.String()
}

// Формирование текстового блока с временными интервалами сильного ветра
func formatWindGustTimePlain(forecasts []WindGustForecast) string {
	if len(forecasts) == 0 {
		return ""
	}

	// Если ветер дует весь день, просто указываем на это
	if len(forecasts) > 6 {
		return "Сильные порывы ветра ожидаются в течение всего дня.\n"
	}

	var builder strings.Builder
	builder.WriteString("Сильные порывы ветра ожидаются в следующие часы:\n")

	for _, forecast := range forecasts {
		timeStr := forecast.Time.Format("15:04")
		builder.WriteString(fmt.Sprintf("- %s - %.2f м/с\n", timeStr, forecast.WindGust))
	}

	builder.WriteString("\n")
	return builder.String()
}

// Нахождение максимального значения порыва ветра
func findMaxWindGust(forecasts []WindGustForecast) float64 {
	if len(forecasts) == 0 {
		return 0
	}

	max := forecasts[0].WindGust
	for _, forecast := range forecasts {
		if forecast.WindGust > max {
			max = forecast.WindGust
		}
	}

	return max
}

// Проверка погоды и отправка предупреждения
func checkWeatherAndAlert(config *Config) {
	log.Println("Запуск проверки погодных условий...")

	weatherData, err := getWeatherData(config)
	if err != nil {
		log.Printf("Ошибка при получении данных о погоде: %v\n", err)
		return
	}

	// Проверка наличия данных
	if len(weatherData.List) == 0 {
		log.Println("Нет данных о погоде в ответе API")
		return
	}

	// Проверяем весь день на наличие сильных порывов ветра
	exceedsThreshold, forecasts := checkWeatherForTheDay(weatherData, config.WindGustThreshold)

	if exceedsThreshold {
		log.Println("Порывы ветра превышают пороговое значение в течение дня, отправляю предупреждение...")

		// Получаем максимальную силу ветра за день
		maxWindGust := findMaxWindGust(forecasts)

		// Формируем блоки с информацией о времени сильного ветра
		timeBlockHTML := formatWindGustTimeHTML(forecasts)
		timeBlockPlain := formatWindGustTimePlain(forecasts)

		subject := "ВНИМАНИЕ: Сильный ветер сегодня"

		// Формирование HTML версии письма
		htmlBody := fmt.Sprintf(emailHTMLTemplate, maxWindGust, config.WindGustThreshold, timeBlockHTML)

		// Формирование текстовой версии письма (для клиентов без поддержки HTML)
		plainTextBody := fmt.Sprintf(
			"Внимание!\n\n"+
				"Сегодня ожидаются сильные порывы ветра до %.2f м/с, что превышает безопасный порог (%.2f м/с).\n"+
				"%s"+
				"Рекомендуется не открывать окна в офисе в течение дня.\n\n"+
				"Это автоматическое уведомление от системы мониторинга погоды.",
			maxWindGust, config.WindGustThreshold, timeBlockPlain)

		if err := sendEmail(config, subject, htmlBody, plainTextBody); err != nil {
			log.Printf("Ошибка при отправке предупреждения: %v\n", err)
		} else {
			log.Println("Предупреждение успешно отправлено")
		}
	} else {
		log.Println("Порывы ветра в норме на весь день, предупреждение не требуется")
	}
}

// Получение следующего времени отправки
func getNextSendTime(config *Config) time.Time {
	now := time.Now()
	nextSend := time.Date(now.Year(), now.Month(), now.Day(), config.NotificationHour, config.NotificationMin, 0, 0, now.Location())

	// Если уже позже времени отправки, переходим на следующий день
	if now.After(nextSend) {
		nextSend = nextSend.Add(24 * time.Hour)
	}

	return nextSend
}

func main() {
	log.Println("Запуск сервиса мониторинга порывов ветра...")

	// Загрузка конфигурации
	config, err := loadConfig()
	if err != nil {
		log.Fatalf("Ошибка при загрузке конфигурации: %v", err)
	}

	log.Printf("Загружена конфигурация: порог ветра = %.2f м/с, время отправки = %02d:%02d",
		config.WindGustThreshold, config.NotificationHour, config.NotificationMin)

	// Запускаем первую проверку сразу при старте (но уведомление отправляем только если сейчас время отправки)
	now := time.Now()
	if now.Hour() == config.NotificationHour && now.Minute() >= config.NotificationMin && now.Minute() < config.NotificationMin+5 {
		// Запускаем проверку только если мы находимся в 5-минутном окне после времени отправки
		checkWeatherAndAlert(config)
	} else {
		log.Printf("Первая проверка будет выполнена в %02d:%02d", config.NotificationHour, config.NotificationMin)
	}

	// Основной цикл программы
	for {
		// Получаем время следующей отправки
		nextSend := getNextSendTime(config)

		// Вычисляем время ожидания до следующей отправки
		waitDuration := nextSend.Sub(time.Now())
		log.Printf("Следующая проверка запланирована на %s (через %s)",
			nextSend.Format("2006-01-02 15:04:05"), waitDuration.String())

		// Ждем до следующего времени отправки
		time.Sleep(waitDuration)

		// Выполняем проверку и отправку
		checkWeatherAndAlert(config)
	}
}
