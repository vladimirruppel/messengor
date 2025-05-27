package client

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/vladimirruppel/messengor/internal/protocol"
)

const (
	StateUnauthenticated = "UNAUTHENTICATED"
	StateAuthenticating  = "AUTHENTICATING" // Промежуточное состояние ожидания ответа
	StateChatList        = "CHAT_LIST"
	StateInChat          = "IN_CHAT"
)

// Структура для хранения состояния клиента (можно расширять)
type ClientState struct {
	Current         string // Текущее состояние
	UserID          string
	DisplayName     string
	SessionToken    string
	serverResponses chan protocol.WebSocketMessage // Канал для ответов от сервера
}

var addr = flag.String("addr", "localhost:8088", "http service address")

func RunClient(serverAddr string) {
	u := url.URL{Scheme: "ws", Host: *addr, Path: "/ws"}
	log.Printf("connecting to %s", u.String())

	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		log.Fatalf("Failed to connect to WebSocket server: %v", err)
	}
	defer conn.Close()

	log.Println("WebSocket connection established successfully!")

	// Канал для сигнализации о том, что горутина чтения завершилась
	clientState := &ClientState{
		Current:         StateUnauthenticated,
		serverResponses: make(chan protocol.WebSocketMessage, 10), // Буферизованный канал
	}

	// Горутина для чтения сообщений от сервера
	go func() {
		for {
			_, messageBytes, err := conn.ReadMessage()
			if err != nil {
				log.Println("Read error (server connection closed or error):", err)
				close(clientState.serverResponses)
				return
			}

			var wsMsg protocol.WebSocketMessage
			if err := json.Unmarshal(messageBytes, &wsMsg); err != nil {
				log.Printf("Failed to unmarshal server message: %v. Raw: %s\n", err, string(messageBytes))
				continue
			}

			if wsMsg.Type == protocol.MsgTypeBroadcastText {
				// Если это широковещательное сообщение, обрабатываем и выводим его сразу здесь
				var broadcastPayload protocol.BroadcastTextPayload
				if err := json.Unmarshal(wsMsg.Payload, &broadcastPayload); err != nil {
					log.Printf("Client: Failed to unmarshal BroadcastTextPayload: %v. Raw: %s\n", err, string(wsMsg.Payload))
					continue
				}

				fmt.Printf("\r%*s\r", 80, "") // Очистка текущей строки (ширина 80 символов, можно подобрать)
				fmt.Printf("[%s] (%s): %s\n",
					broadcastPayload.SenderName,
					time.Unix(broadcastPayload.Timestamp, 0).Format("15:04:05"),
					broadcastPayload.Text)
				fmt.Print("> ") // Снова выводим приглашение к вводу
			} else {
				// Для всех остальных типов сообщений (LoginResponse, RegisterResponse и т.д.)
				// отправляем их в канал serverResponses для обработки основным циклом.
				// Это важно, так как они влияют на состояние клиента (clientState.Current).
				select {
				case clientState.serverResponses <- wsMsg:
					// Успешно отправлено в канал
				default:
					// Канал переполнен или что-то пошло не так. Этого не должно быть, если канал буферизован
					// и основной цикл его читает.
					log.Println("Warning: serverResponses channel is full or closed unexpectedly.")
				}
			}
		}
	}()

	// Основной цикл клиента (обработка состояний и ввода пользователя)
	reader := bufio.NewReader(os.Stdin)
	running := true

	for running {
		switch clientState.Current {
		case StateUnauthenticated:
			fmt.Println("\n--- Messenger Menu ---")
			fmt.Println("1. Login")
			fmt.Println("2. Register")
			fmt.Println("3. Exit")
			fmt.Print("Choose an option: ")

			input, _ := reader.ReadString('\n')
			choice := strings.TrimSpace(input)

			switch choice {
			case "1":
				handleLogin(reader, conn, clientState) // Передаем clientState для изменения
			case "2":
				handleRegister(reader, conn, clientState) // Передаем clientState для изменения
			case "3":
				log.Println("Exiting...")
				running = false
			default:
				log.Println("Invalid option. Please try again.")
			}

		case StateAuthenticating:
			log.Println("Waiting for server response...")
			// Ожидаем ответ от сервера через канал clientState.serverResponses
			// Этот select будет блокировать, пока что-то не придет в канал или не произойдет таймаут
			select {
			case wsMsg, ok := <-clientState.serverResponses:
				if !ok { // Канал закрыт (например, из-за ошибки сети в горутине чтения)
					log.Println("Server connection lost. Returning to unauthenticated state.")
					clientState.Current = StateUnauthenticated
					// TODO: Возможно, попытка переподключения?
					continue // На следующую итерацию главного цикла
				}
				// Обрабатываем ответ (например, LoginResponse или RegisterResponse)
				processServerAuthenticationResponse(wsMsg, clientState)
				// Можно добавить time.After для таймаута ожидания ответа
			case <-time.After(10 * time.Second):
				log.Println("Timeout waiting for server response.")
				clientState.Current = StateUnauthenticated
			}

		case StateChatList: // Пока это будет вход в "глобальный чат"
			fmt.Println("\n--- Main Menu ---")
			fmt.Printf("Logged in as: %s (ID: %s)\n", clientState.DisplayName, clientState.UserID)
			fmt.Println("1. Enter Global Chat")
			fmt.Println("2. Logout")
			fmt.Println("3. Exit")
			fmt.Print("Choose an option: ")

			input, _ := reader.ReadString('\n')
			choice := strings.TrimSpace(input)

			switch choice {
			case "1":
				clientState.Current = StateInChat
			case "2":
				// TODO: Отправить C2S_LOGOUT_REQUEST, если он есть в протоколе
				log.Println("Logging out...")
				// Сброс состояния клиента
				clientState.UserID = ""
				clientState.DisplayName = ""
				clientState.SessionToken = ""
				clientState.Current = StateUnauthenticated
			case "3":
				log.Println("Exiting...")
				running = false
			default:
				log.Println("Invalid option.")
			}

		case StateInChat:
			fmt.Println("\n--- In Global Chat (type '[[back]]' to return to menu) ---")
			// Цикл для отправки сообщений внутри чата
			// и параллельно слушаем clientState.serverResponses для входящих сообщений
			// Это более сложная часть, нужно будет совместить чтение ввода и чтение из канала
			// Пока сделаем простой ввод сообщения или команды [[back]]
			print("> ")
			chatInput, _ := reader.ReadString('\n')
			chatInput = strings.TrimSpace(chatInput)

			if chatInput == "[[back]]" {
				clientState.Current = StateChatList
			} else if chatInput != "" {
				// Отправляем текстовое сообщение
				sendTextMessage(conn, chatInput)
			}
			// В реальном InChat нужно будет использовать select для неблокирующего чтения
			// из os.Stdin и clientState.serverResponses
			// Пока что входящие сообщения из serverResponses будут обрабатываться, когда мы вернемся в
			// начало цикла for running и снова попадем в case StateInChat (если там будет select).
			// Либо нужен отдельный механизм отображения чата.
			// Для простоты пока оставим так, основная обработка входящих - в StateAuthenticating.
			// В StateInChat сообщения от сервера будут просто логироваться горутиной чтения.

		default:
			log.Printf("Unknown client state: %s. Resetting to Unauthenticated.\n", clientState.Current)
			clientState.Current = StateUnauthenticated
		}
	}

	// Попытка корректного закрытия соединения при выходе из главного цикла
	log.Println("Sending close message to server...")
	err = conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	if err != nil {
		log.Println("Error sending close message:", err)
	}
}

// --- Вспомогательные функции для обработки команд ---

func handleLogin(reader *bufio.Reader, conn *websocket.Conn, cs *ClientState) {
	fmt.Print("Enter username: ")
	username, _ := reader.ReadString('\n')
	username = strings.TrimSpace(username)

	fmt.Print("Enter password: ")
	password, _ := reader.ReadString('\n')
	password = strings.TrimSpace(password)

	payload := protocol.LoginRequestPayload{
		Username: username,
		Password: password,
	}
	sendWebSocketMessage(conn, protocol.MsgTypeLoginRequest, payload)
	cs.Current = StateAuthenticating // Переходим в состояние ожидания ответа
}

func handleRegister(reader *bufio.Reader, conn *websocket.Conn, cs *ClientState) {
	fmt.Print("Enter username: ")
	username, _ := reader.ReadString('\n')
	username = strings.TrimSpace(username)

	fmt.Print("Enter password: ")
	password, _ := reader.ReadString('\n')
	password = strings.TrimSpace(password)

	fmt.Print("Enter display name: ")
	displayName, _ := reader.ReadString('\n')
	displayName = strings.TrimSpace(displayName)

	payload := protocol.RegisterRequestPayload{
		Username:    username,
		Password:    password,
		DisplayName: displayName,
	}
	sendWebSocketMessage(conn, protocol.MsgTypeRegisterRequest, payload)
	cs.Current = StateAuthenticating
}

func sendTextMessage(conn *websocket.Conn, text string) {
	payload := protocol.TextPayload{Text: text}
	sendWebSocketMessage(conn, protocol.MsgTypeText, payload)
}

// Общая функция для отправки сообщений на сервер
func sendWebSocketMessage(conn *websocket.Conn, msgType string, payloadData interface{}) {
	payloadBytes, err := json.Marshal(payloadData)
	if err != nil {
		log.Printf("Failed to marshal payload for type %s: %v\n", msgType, err)
		return
	}

	wsMsg := protocol.WebSocketMessage{
		Type:    msgType,
		Payload: payloadBytes,
	}

	messageBytes, err := json.Marshal(wsMsg)
	if err != nil {
		log.Printf("Failed to marshal WebSocket message: %v\n", err)
		return
	}

	err = conn.WriteMessage(websocket.TextMessage, messageBytes)
	if err != nil {
		log.Printf("Write error for type %s: %v\n", msgType, err)
		// Здесь можно добавить логику обработки потери соединения
	} else {
		log.Printf("Sent to server: Type=%s\n", msgType)
	}
}

// Функция для обработки ответов аутентификации от сервера
func processServerAuthenticationResponse(wsMsg protocol.WebSocketMessage, cs *ClientState) {
	switch wsMsg.Type {
	case protocol.MsgTypeLoginResponse:
		var respPayload protocol.LoginResponsePayload
		if err := json.Unmarshal(wsMsg.Payload, &respPayload); err != nil {
			log.Printf("Failed to unmarshal LoginResponse payload: %v\n", err)
			cs.Current = StateUnauthenticated // Возврат в исходное состояние при ошибке парсинга
			return
		}
		if respPayload.Success {
			log.Printf("Login successful! Welcome, %s.\n", respPayload.DisplayName)
			cs.UserID = respPayload.UserID
			cs.DisplayName = respPayload.DisplayName
			cs.SessionToken = respPayload.SessionToken
			cs.Current = StateChatList // Переход в следующее состояние
		} else {
			log.Printf("Login failed: %s\n", respPayload.ErrorMessage)
			cs.Current = StateUnauthenticated
		}

	case protocol.MsgTypeRegisterResponse:
		var respPayload protocol.RegisterResponsePayload
		if err := json.Unmarshal(wsMsg.Payload, &respPayload); err != nil {
			log.Printf("Failed to unmarshal RegisterResponse payload: %v\n", err)
			cs.Current = StateUnauthenticated
			return
		}
		if respPayload.Success {
			log.Println("Registration successful! Please login.")
			// После успешной регистрации обычно просят пользователя залогиниться
			cs.Current = StateUnauthenticated
		} else {
			log.Printf("Registration failed: %s\n", respPayload.ErrorMessage)
			cs.Current = StateUnauthenticated
		}
	default:
		log.Printf("Received unexpected message type %s while authenticating/waiting.\n", wsMsg.Type)
		// Можно ничего не делать или вернуть в StateUnauthenticated
	}
}
