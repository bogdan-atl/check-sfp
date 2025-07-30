package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

var bot *tgbotapi.BotAPI
var chatID int64

// Структура для хранения результата проверки одного интерфейса
type SFPResult struct {
	Timestamp string  `json:"timestamp"`
	Host      string  `json:"host"`
	Interface int     `json:"interface"`
	RxPower   float64 `json:"rx_power"`
	Status    string  `json:"status"`            // "OK" или "LOW"
	Comment   string  `json:"comment,omitempty"` // Новое поле
}

// Глобальные переменные для хранения последних результатов
var (
	lastResults   []SFPResult
	lastResultsMu sync.RWMutex
)

// Конфигурация
type Switch struct {
	Host     string `json:"host"`
	Port     int    `json:"port,omitempty"`
	Username string `json:"username"`
	Password string `json:"password"`
	Comment  string `json:"comment,omitempty"` // Новое поле
}

type Config struct {
	TelegramToken  string   `json:"telegram_token"`
	TelegramChatID int64    `json:"telegram_chat_id"`
	Switches       []Switch `json:"switches"`
	Threshold      float64  `json:"threshold"`
}

var rxPowerRegex = regexp.MustCompile(`(?i)rx.*power.*?(-?\d+(?:\.\d+)?)`)

func initTelegramBot(token string, id int64) {
	var err error
	bot, err = tgbotapi.NewBotAPI(token)
	if err != nil {
		log.Panic(err)
	}
	chatID = id
	log.Printf("✅ Telegram бот авторизован: %s", bot.Self.UserName)
}

func sendTelegramMessage(message string) {
	if bot == nil {
		log.Printf("⚠️ Telegram бот не инициализирован")
		return
	}

	msg := tgbotapi.NewMessage(chatID, message)
	_, err := bot.Send(msg)
	if err != nil {
		log.Printf("❌ Ошибка отправки в Telegram: %v", err)
	}
}

func connectAndParseTransceiverData(sw Switch, threshold float64, allResults *[]SFPResult) {
	config := &ssh.ClientConfig{
		User: sw.Username,
		Auth: []ssh.AuthMethod{
			ssh.Password(sw.Password),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}

	addr := fmt.Sprintf("%s:%d", sw.Host, sw.Port)
	conn, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		log.Printf("❌ Ошибка подключения к %s: %v", sw.Host, err)
		return
	}
	defer conn.Close()

	session, err := conn.NewSession()
	if err != nil {
		log.Printf("❌ Ошибка создания сессии: %v", err)
		return
	}
	defer session.Close()

	modes := ssh.TerminalModes{
		ssh.ECHO: 0,
	}
	if err := session.RequestPty("xterm", 40, 80, modes); err != nil {
		log.Printf("❌ Ошибка RequestPty: %v", err)
		return
	}

	stdin, err := session.StdinPipe()
	if err != nil {
		log.Printf("❌ Ошибка stdin: %v", err)
		return
	}

	stdout, err := session.StdoutPipe()
	if err != nil {
		log.Printf("❌ Ошибка stdout: %v", err)
		return
	}

	if err := session.Shell(); err != nil {
		log.Printf("❌ Ошибка запуска shell: %v", err)
		return
	}

	var output bytes.Buffer
	writer := io.MultiWriter(&output)

	send := func(cmd string) {
		fmt.Fprintf(stdin, "%s\n", cmd)
		time.Sleep(1 * time.Second)
	}

	send("terminal length 0")
	log.Printf("✅ Отправляем команду 'show interface * transceiver' на %s", sw.Host)
	send("show interface * transceiver")
	time.Sleep(3 * time.Second)
	send("exit")

	_, _ = io.Copy(writer, stdout)

	data := output.String()
	allMatches := rxPowerRegex.FindAllStringSubmatch(data, -1)
	if len(allMatches) == 0 {
		log.Printf("❌ Не найдены данные Rx Power в выводе от %s", sw.Host)
		sendTelegramMessage(fmt.Sprintf("❌ На свитче %s не найдены данные Rx Power", sw.Host))
	} else {
		for i, matches := range allMatches {
			if len(matches) > 1 {
				var power float64
				fmt.Sscanf(matches[1], "%f", &power)

				status := "OK"
				if power < threshold {
					status = "LOW"
					warningMsg := fmt.Sprintf(
						"[ПРЕДУПРЕЖДЕНИЕ] На свитче %s (интерфейс #%d) Rx Power ниже порога: %.2f dBm",
						sw.Host, i+1, power,
					)
					log.Println(warningMsg)
					sendTelegramMessage(warningMsg)
				}

				result := SFPResult{
					Timestamp: time.Now().Format("2006/01/02 15:04:05"),
					Host:      sw.Host,
					Interface: i + 1,
					RxPower:   power,
					Status:    status,
					Comment:   sw.Comment, // Передаём комментарий
				}

				*allResults = append(*allResults, result)
				log.Printf("На свитче %s (интерфейс #%d) Rx Power: %.2f dBm", sw.Host, i+1, power)
			}
		}
	}
}

func loadConfig(path string) (*Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	byteValue, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}

	var config Config
	err = json.Unmarshal(byteValue, &config)
	return &config, err
}

func sfpHandler(w http.ResponseWriter, r *http.Request) {
	config, err := loadConfig("config.json")
	if err != nil {
		http.Error(w, "Cannot load config", http.StatusInternalServerError)
		return
	}

	var results []SFPResult
	var mu sync.Mutex // Теперь будем использовать!
	var wg sync.WaitGroup

	for _, sw := range config.Switches {
		wg.Add(1)
		go func(s Switch) {
			defer wg.Done()

			// Временный срез для результатов одного свитча
			var localResults []SFPResult
			connectAndParseTransceiverData(s, config.Threshold, &localResults)

			// Потокобезопасно добавляем в общий срез
			mu.Lock()
			results = append(results, localResults...)
			mu.Unlock()
		}(sw)
	}

	wg.Wait()

	// Сохраняем результаты
	lastResultsMu.Lock()
	lastResults = results
	lastResultsMu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}

// Фоновая проверка (как раньше)
func startPeriodicChecks() {
	config, err := loadConfig("config.json")
	if err != nil {
		log.Fatalf("❌ Не могу загрузить конфиг: %v", err)
	}

	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		<-ticker.C
		var results []SFPResult
		var wg sync.WaitGroup

		for _, sw := range config.Switches {
			wg.Add(1)
			go func(s Switch) {
				defer wg.Done()
				connectAndParseTransceiverData(s, config.Threshold, &results)
			}(sw)
		}
		wg.Wait()

		// Обновляем кэш
		lastResultsMu.Lock()
		lastResults = results
		lastResultsMu.Unlock()
	}
}

func main() {
	config, err := loadConfig("config.json")
	if err != nil {
		log.Fatalf("❌ Не могу загрузить конфиг: %v", err)
	}

	// Инициализация Telegram из config.json
	initTelegramBot(config.TelegramToken, config.TelegramChatID) // Замени

	// Запускаем HTTP сервер
	http.HandleFunc("/sfp", sfpHandler)
	go func() {
		log.Println("🌐 HTTP сервер запущен на :8080")
		if err := http.ListenAndServe(":8080", nil); err != nil {
			log.Fatalf("❌ Ошибка HTTP сервера: %v", err)
		}
	}()

	// Запускаем периодические проверки
	go startPeriodicChecks()

	// Первая проверка при старте
	go func() {
		time.Sleep(2 * time.Second) // Даем серверу стартовать
		resp, err := http.Get("http://localhost:8080/sfp")
		if err != nil {
			log.Printf("❌ Не удалось выполнить первую проверку: %v", err)
		} else {
			defer resp.Body.Close()
			log.Printf("✅ Первая проверка выполнена, статус: %s", resp.Status)
		}
	}()

	// Бесконечный цикл
	select {}
}
