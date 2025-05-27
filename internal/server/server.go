package server

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/vladimirruppel/messengor/internal/protocol"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		// TODO: Разрешаем все источники - пока что
		return true
	},
}

func HandleWebSocketConnections(hub *Hub, w http.ResponseWriter, r *http.Request) {
	log.Println("Received HTTP request on /ws, attempting to upgrade...")

	var conn, err = upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Failed to upgrade connection: %v\n", err)
		return
	}

	defer conn.Close()

	log.Println("WebSocket connection established successfully!")

	var authenticatedUser *User // Сюда запишем данные пользователя после успеха
	var sessionToken string

	// Установим дедлайн на первую аутентификационную операцию
	if err := conn.SetReadDeadline(time.Now().Add(60 * time.Second)); err != nil {
		log.Printf("Auth: Error setting read deadline for client %p: %v", conn, err)
		conn.Close()
		return
	}

AUTH_LOOP:
	for {
		messageType, p, err := conn.ReadMessage()
		if err != nil {
			log.Printf("Auth: Read error from client %p before authentication: %v", conn, err)
			conn.Close() // Закрываем соединение, если ошибка до аутентификации
			return       // Выходим из HandleWebSocketConnections
		}

		if messageType != websocket.TextMessage {
			log.Printf("Auth: Received non-text message from client %p before auth.", conn)
			sendErrorMessage(conn, "INVALID_MESSAGE_TYPE", "Expected text message for authentication.")
			continue // Ждем следующего сообщения
		}

		var receivedMsg protocol.WebSocketMessage
		if err := json.Unmarshal(p, &receivedMsg); err != nil {
			log.Printf("Auth: Failed to unmarshal WebSocket message from client %p: %v. Raw: %s", conn, err, string(p))
			sendErrorMessage(conn, "INVALID_JSON", "Could not parse JSON message.")
			continue
		}

		log.Printf("Auth: Received from client %p: Type=%s", conn, receivedMsg.Type)

		switch receivedMsg.Type {
		case protocol.MsgTypeRegisterRequest:
			var reqPayload protocol.RegisterRequestPayload
			if err := json.Unmarshal(receivedMsg.Payload, &reqPayload); err != nil {
				log.Printf("Failed to unmarshal RegisterRequest payload: %v\n", err)
				sendErrorMessage(conn, "INVALID_PAYLOAD", "Could not parse register request payload.")
				continue
			}

			log.Printf("Processing RegisterRequest for username: %s\n", reqPayload.Username)
			user, err := RegisterNewUser(reqPayload.Username, reqPayload.Password, reqPayload.DisplayName)

			var respPayload protocol.RegisterResponsePayload
			if err != nil {
				log.Printf("Registration failed for %s: %v\n", reqPayload.Username, err)
				respPayload = protocol.RegisterResponsePayload{
					Success:      false,
					ErrorMessage: err.Error(), // Используем сообщение из ошибки хранилища
				}
			} else {
				log.Printf("Registration successful for %s, UserID: %s\n", reqPayload.Username, user.ID)
				respPayload = protocol.RegisterResponsePayload{
					Success: true,
					UserID:  user.ID,
				}
			}
			sendWebSocketResponse(conn, protocol.MsgTypeRegisterResponse, respPayload)

		case protocol.MsgTypeLoginRequest:
			var reqPayload protocol.LoginRequestPayload
			if err := json.Unmarshal(receivedMsg.Payload, &reqPayload); err != nil {
				log.Printf("Auth: Failed to unmarshal LoginRequest payload: %v\n", err)
				sendErrorMessage(conn, "INVALID_PAYLOAD", "Could not parse login request payload.")
				continue
			}

			user, authErr := AuthenticateUser(reqPayload.Username, reqPayload.Password)
			var respPayload protocol.LoginResponsePayload
			if authErr != nil {
				respPayload = protocol.LoginResponsePayload{Success: false, ErrorMessage: authErr.Error()}
				sendWebSocketResponse(conn, protocol.MsgTypeLoginResponse, respPayload)
				continue // Остаемся в цикле AUTH_LOOP для новой попытки или регистрации
			} else {
				authenticatedUser = user
				sessionToken = uuid.NewString() // Генерируем токен
				respPayload = protocol.LoginResponsePayload{
					Success:      true,
					UserID:       user.ID,
					DisplayName:  user.DisplayName,
					SessionToken: sessionToken,
				}
				sendWebSocketResponse(conn, protocol.MsgTypeLoginResponse, respPayload)
				log.Printf("Client %s (ID: %s) authenticated successfully. Token: %s", user.DisplayName, user.ID, sessionToken)
				break AUTH_LOOP // Успешная аутентификация, выходим из цикла AUTH_LOOP
			}

		default:
			log.Printf("Auth: Received unexpected message type %s from client %p before authentication.", receivedMsg.Type, conn)
			sendErrorMessage(conn, "UNEXPECTED_MESSAGE_TYPE", "Expected LoginRequest or RegisterRequest.")
		}

		// Сбрасываем дедлайн после каждого успешно обработанного сообщения в цикле аутентификации
		if err := conn.SetReadDeadline(time.Now().Add(60 * time.Second)); err != nil {
			log.Printf("Auth: Error resetting read deadline for client %p: %v", conn, err)
			conn.Close()
			return
		}
	}

	// Если мы вышли из цикла AUTH_LOOP без authenticatedUser, значит что-то пошло не так
	if authenticatedUser == nil {
		log.Printf("Client %p did not authenticate. Closing connection.", conn)
		conn.Close() // Закрываем, если аутентификация так и не прошла
		return
	}

	// Убираем дедлайн на чтение, так как readPump будет использовать свой механизм с Pong
	if err := conn.SetReadDeadline(time.Time{}); err != nil { // time.Time{} - нулевое время, отключает дедлайн
		log.Printf("Error clearing read deadline for client %s (ID: %s): %v", authenticatedUser.DisplayName, authenticatedUser.ID, err)
		// Не обязательно фатально, но стоит залогировать
	}

	client := &Client{
		hub:             hub,
		conn:            conn,
		send:            make(chan []byte, 256), // Буфер на 256 сообщений
		UserID:          authenticatedUser.ID,
		DisplayName:     authenticatedUser.DisplayName,
		IsAuthenticated: true,
	}

	client.hub.register <- client

	go client.writePump()
	client.readPump()

	log.Printf("HandleWebSocketConnections finished for client %s (ID: %s)", client.DisplayName, client.UserID)
}

// Вспомогательная функция для отправки ответов клиенту
func sendWebSocketResponse(conn *websocket.Conn, msgType string, payloadData interface{}) {
	payloadBytes, err := json.Marshal(payloadData)
	if err != nil {
		log.Printf("Error marshalling payload for type %s: %v\n", msgType, err)
		// Не отправляем ничего клиенту, если не можем сериализовать наш собственный ответ
		return
	}

	wsMsg := protocol.WebSocketMessage{
		Type:    msgType,
		Payload: payloadBytes,
	}

	messageBytes, err := json.Marshal(wsMsg)
	if err != nil {
		log.Printf("Error marshalling WebSocket message for type %s: %v\n", msgType, err)
		return
	}

	if err := conn.WriteMessage(websocket.TextMessage, messageBytes); err != nil {
		log.Printf("Error sending %s to client: %v\n", msgType, err)
	} else {
		log.Printf("Sent to client: Type=%s\n", msgType)
	}
}

// Вспомогательная функция для отправки сообщений об ошибках клиенту
func sendErrorMessage(conn *websocket.Conn, errorCode string, errorMessage string) {
	// errorPayload := protocol.ErrorPayload{ // Предполагается, что у вас есть ErrorPayload в protocol
	// 	ErrorCode:    errorCode,
	// 	ErrorMessage: errorMessage,
	// }
	// Можно создать специальный тип сообщения для ошибок или использовать существующий
	// для ответа, но с флагом ошибки. Для простоты пока используем специальный тип,
	// которого у нас еще нет, или просто логируем и не отправляем.
	// Либо, если ErrorPayload используется в Register/LoginResponse, то тип ответа остается тот же.

	// Для универсальности, давайте предположим, что у нас есть общий тип S2C_ERROR_NOTIFY
	// sendWebSocketResponse(conn, protocol.MsgTypeErrorNotify, errorPayload)
	// Поскольку у нас нет такого типа, пока просто залогируем, что надо бы отправить.
	log.Printf("Should send error to client: Code=%s, Message=%s\n", errorCode, errorMessage)
	// В реальном приложении здесь был бы вызов sendWebSocketResponse с соответствующим типом.
}
