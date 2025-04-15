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
	"time"

	"github.com/joho/godotenv"
	"github.com/wneessen/go-mail"
)

// Константы для настройки приложения
const (
	WindGustThreshold = 15.0 // Пороговое значение для порывов ветра в м/с
	CheckInterval     = 3 * time.Hour
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
        <p>Сегодня ожидаются <span class="highlight">сильные порывы ветра (%.2f м/с)</span>, что превышает безопасный порог (%.2f м/с).</p>
        <p>Рекомендуется <span class="highlight">не открывать окна в офисе</span> в течение дня.</p>
        <div class="footer">
            <p>Это автоматическое уведомление от системы мониторинга погоды.</p>
        </div>
    </div>
</body>
</html>`

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

	config := &Config{
		OpenWeatherAPIKey: os.Getenv("OPENWEATHER_API_KEY"),
		City:              os.Getenv("CITY"),
		EmailFrom:         os.Getenv("EMAIL_FROM"),
		EmailTo:           emailTo,
		SMTPServer:        os.Getenv("SMTP_SERVER"),
		SMTPPort:          os.Getenv("SMTP_PORT"),
		SMTPUser:          os.Getenv("SMTP_USER"),
		SMTPPassword:      os.Getenv("SMTP_PASSWORD"),
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

	// Получение данных о ветре из первого прогноза (ближайшего по времени)
	todayForecast := weatherData.List[0]
	windGust := todayForecast.Wind.Gust

	log.Printf("Текущие порывы ветра: %.2f м/с\n", windGust)

	// Проверка превышения порога
	if windGust > WindGustThreshold {
		log.Println("Порывы ветра превышают пороговое значение, отправляю предупреждение...")

		subject := "ВНИМАНИЕ: Сильный ветер сегодня"

		// Формирование HTML версии письма
		htmlBody := fmt.Sprintf(emailHTMLTemplate, windGust, WindGustThreshold)

		// Формирование текстовой версии письма (для клиентов без поддержки HTML)
		plainTextBody := fmt.Sprintf(
			"Внимание!\n\n"+
				"Сегодня ожидаются сильные порывы ветра (%.2f м/с), что превышает безопасный порог (%.2f м/с).\n"+
				"Рекомендуется не открывать окна в офисе в течение дня.\n\n"+
				"Это автоматическое уведомление от системы мониторинга погоды.",
			windGust, WindGustThreshold)

		if err := sendEmail(config, subject, htmlBody, plainTextBody); err != nil {
			log.Printf("Ошибка при отправке предупреждения: %v\n", err)
		} else {
			log.Println("Предупреждение успешно отправлено")
		}
	} else {
		log.Println("Порывы ветра в норме, предупреждение не требуется")
	}
}

func main() {
	log.Println("Запуск сервиса мониторинга порывов ветра...")

	// Загрузка конфигурации
	config, err := loadConfig()
	if err != nil {
		log.Fatalf("Ошибка при загрузке конфигурации: %v", err)
	}

	// Первая проверка сразу при запуске
	checkWeatherAndAlert(config)

	// Настройка периодических проверок
	ticker := time.NewTicker(CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			checkWeatherAndAlert(config)
		}
	}
}
