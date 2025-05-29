package client

import (
	"bufio"
	"log"
	"net/url"
	"os"
	"os/signal" // Для Ctrl+C
	"syscall"   // Для os.Interrupt, os.Kill

	"github.com/gorilla/websocket"
)

// ClientApp - основная структура клиентского приложения.
type ClientApp struct {
	conn         *websocket.Conn
	state        *AppState
	reader       *bufio.Reader // Для чтения ввода пользователя
	serverAddr   string
	shutdownChan chan struct{}
}

// NewClientApp создает новый экземпляр клиентского приложения
func NewClientApp(serverAddr string) *ClientApp {
	return &ClientApp{
		state:        NewAppState(),
		reader:       bufio.NewReader(os.Stdin),
		serverAddr:   serverAddr,
		shutdownChan: make(chan struct{}),
	}
}

// Run запускает главный цикл клиентского приложения
func (app *ClientApp) Run() {
	u := url.URL{Scheme: "ws", Host: app.serverAddr, Path: "/ws"}
	serverFullURL := u.String()

	var err error
	app.conn, err = connectToServer(serverFullURL)
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer app.conn.Close()

	log.Println("WebSocket connection established successfully!")

	// Горутина для чтения сообщений от сервера
	go readFromServer(app.conn, app.state.ServerResponses, app.shutdownChan)

	// Обработка сигналов для корректного завершения
	interruptChan := make(chan os.Signal, 1)
	signal.Notify(interruptChan, os.Interrupt, syscall.SIGTERM) // syscall.SIGTERM для Linux/macOS

	running := true
	for running {
		// Проверяем, не пришел ли сигнал на завершение или сообщение о закрытии соединения
		select {
		case <-app.shutdownChan: // Если readFromServer сигнализирует о проблеме с соединением
			log.Println("Connection lost or shutdown signal received from reader. Exiting...")
			running = false
			continue // Выходим из select и затем из for
		case <-interruptChan: // Пользователь нажал Ctrl+C
			log.Println("Interrupt signal received. Exiting...")
			running = false
			continue
		default:
			// Неблокирующая проверка, если нет других событий, продолжаем работу UI
		}

		if !running { // Двойная проверка на случай, если мы вышли из select по сигналу
			break
		}

		app.processCurrentState() // функция для обработки UI и логики состояний
	}

	// Попытка корректного закрытия соединения
	log.Println("ClientApp: Sending close message to server...")
	closeMsg := websocket.FormatCloseMessage(websocket.CloseNormalClosure, "")
	err = app.conn.WriteMessage(websocket.CloseMessage, closeMsg)
	if err != nil {
		// Игнорируем ошибку, если соединение уже закрыто
		if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
			log.Println("ClientApp: Error sending close message:", err)
		}
	}
	log.Println("ClientApp: Exited.")
}

func (app *ClientApp) processCurrentState() {
	switch app.state.Current {
	case StateUnauthenticated:
		handleUnauthenticatedState(app)
	case StateAuthenticating:
		handleAuthenticatingState(app) // Этот обработчик должен читать из app.state.ServerResponses
	case StateMainMenu: // Новое имя
		handleMainMenuState(app)
	case StateFetchingUserList: // Новое состояние
		handleFetchingUserListState(app)
	case StateUserSelectionForPrivateChat: // Новое состояние
		handleUserSelectionForPrivateChatState(app)
	case StateInChat: // Это теперь для Global Chat
		handleGlobalChatState(app) // Новый обработчик для глобального чата
	case StateInPrivateChat: // Новое состояние
		handleInPrivateChatState(app)
	default:
		log.Printf("Unknown client state: %s. Resetting to Unauthenticated.\n", app.state.Current)
		app.state.Current = StateUnauthenticated
	}
}