package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/smtp"
	"os"
	"time"

	"github.com/joho/godotenv"
)

// Константы для настройки приложения
const (
	WindGustThreshold = 15.0 // Пороговое значение для порывов ветра в м/с
	CheckInterval     = 3 * time.Hour
)

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
	Dt        int64         `json:"dt"`
	WindSpeed float64       `json:"speed"`
	WindGust  float64       `json:"gust"`
	Weather   []WeatherDesc `json:"weather"`
}

type WeatherDesc struct {
	Main        string `json:"main"`
	Description string `json:"description"`
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

// Получение данных о погоде
func getWeatherData(config *Config) (*WeatherResponse, error) {
	url := fmt.Sprintf("https://api.openweathermap.org/data/2.5/forecast/daily?q=%s&units=metric&cnt=1&appid=%s",
		config.City, config.OpenWeatherAPIKey)

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

// Отправка электронного письма через Microsoft Exchange
func sendEmail(config *Config, subject, body string) error {
	auth := smtp.PlainAuth("", config.SMTPUser, config.SMTPPassword, config.SMTPServer)

	// Формирование заголовков письма
	header := make(map[string]string)
	header["From"] = config.EmailFrom
	header["To"] = config.EmailTo[0] // Для простоты берем первого получателя
	header["Subject"] = subject
	header["MIME-Version"] = "1.0"
	header["Content-Type"] = "text/plain; charset=\"utf-8\""
	header["Content-Transfer-Encoding"] = "base64"

	// Формирование сообщения
	message := ""
	for k, v := range header {
		message += fmt.Sprintf("%s: %s\r\n", k, v)
	}
	message += "\r\n" + body

	// Отправка письма
	err := smtp.SendMail(
		config.SMTPServer+":"+config.SMTPPort,
		auth,
		config.EmailFrom,
		config.EmailTo,
		[]byte(message),
	)
	if err != nil {
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

	// Получение данных о ветре
	todayForecast := weatherData.List[0]
	windGust := todayForecast.WindGust

	log.Printf("Текущие порывы ветра: %.2f м/с\n", windGust)

	// Проверка превышения порога
	if windGust > WindGustThreshold {
		log.Println("Порывы ветра превышают пороговое значение, отправляю предупреждение...")

		subject := "ВНИМАНИЕ: Сильный ветер сегодня"
		body := fmt.Sprintf(
			"Внимание!\n\n"+
				"Сегодня ожидаются сильные порывы ветра (%.2f м/с), что превышает безопасный порог (%.2f м/с).\n"+
				"Рекомендуется не открывать окна в офисе в течение дня.\n\n"+
				"Это автоматическое уведомление от системы мониторинга погоды.",
			windGust, WindGustThreshold)

		if err := sendEmail(config, subject, body); err != nil {
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
