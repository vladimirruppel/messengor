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

	log.Println("WebSocket connection established successfully!")

	// Горутина для чтения сообщений от сервера
	go readFromServer(app.conn, app.state.ServerResponses, app.shutdownChan)

	// Обработка сигналов для корректного завершения
	interruptChan := make(chan os.Signal, 1)
	signal.Notify(interruptChan, os.Interrupt, syscall.SIGTERM) // syscall.SIGTERM для Linux/macOS

	// Обработка сигналов завершения
	running := true
	for running {
		select {
		case <-app.shutdownChan:
			log.Println("ClientApp.Run: Shutdown signal received from reader goroutine. Exiting loop...")
			running = false
			continue // Переходим к проверке running и выходу из цикла
		case <-interruptChan:
			log.Println("ClientApp.Run: Interrupt signal (Ctrl+C) received. Exiting loop...")
			running = false
			continue // Переходим к проверке running и выходу из цикла
		default:
			// Нет сигналов на завершение, продолжаем.
		}

		if !running {
			break
		}

		// Вся логика UI, обработки пользовательского ввода и обработки сообщений от сервера
		// для текущего состояния инкапсулирована здесь
		app.processCurrentState()
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
		handleAuthenticatingState(app)
	case StateMainMenu:
		handleMainMenuState(app)
	case StateFetchingUserList:
		handleFetchingUserListState(app)
	case StateUserSelectionForPrivateChat:
		handleUserSelectionForPrivateChatState(app)
	case StateInChat: // Global Chat
		handleGlobalChatState(app)
	case StateInPrivateChat:
		handleInPrivateChatState(app)
	default:
		log.Printf("Unknown client state: %s. Resetting to Unauthenticated.\n", app.state.Current)
		app.state.Current = StateUnauthenticated
	}
}