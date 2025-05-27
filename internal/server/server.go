package server

import (
	"encoding/json"
	"io"
	"log"
	"net/http"

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

func HandleWebSocketConnections(w http.ResponseWriter, r *http.Request) {
	log.Println("Received HTTP request on /ws, attempting to upgrade...")

	var conn, err = upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Failed to upgrade connection: %v\n", err)
		return
	}

	defer conn.Close()

	log.Println("WebSocket connection established successfully!")

	for {
		_, p, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure, websocket.CloseNoStatusReceived) {
				log.Printf("Unexpected close error from client: %v\n", err)
			} else if err == io.EOF {
				log.Printf("Client closed connection (EOF): %v\n", err)
			} else {
				log.Printf("Read error from client: %v\n", err)
			}
			break // Выходим из цикла for, завершая обработку этого клиента
		}

		var receivedMsg protocol.WebSocketMessage
		err = json.Unmarshal(p, &receivedMsg)
		if err != nil {
			log.Printf("Failed to unmarshal WebSocket message: %v. Original message: %s\n", err, string(p))
			sendErrorMessage(conn, "INVALID_MESSAGE_FORMAT", "Could not parse message.")
			continue
		}

		// Временно для визуализации как происходит обработка сообщения
		log.Printf("Received from client: Type=%s, RawPayload=%s\n", receivedMsg.Type, string(receivedMsg.Payload))

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
				log.Printf("Failed to unmarshal LoginRequest payload: %v\n", err)
				sendErrorMessage(conn, "INVALID_PAYLOAD", "Could not parse login request payload.")
				continue
			}

			log.Printf("Processing LoginRequest for username: %s\n", reqPayload.Username)
			user, err := AuthenticateUser(reqPayload.Username, reqPayload.Password)

			var respPayload protocol.LoginResponsePayload
			if err != nil {
				log.Printf("Authentication failed for %s: %v\n", reqPayload.Username, err)
				respPayload = protocol.LoginResponsePayload{
					Success:      false,
					ErrorMessage: err.Error(),
				}
			} else {
				// currentUserID = user.ID // Временно сохраняем для этого соединения
				sessionToken := uuid.NewString() // Генерируем простой токен сессии
				log.Printf("Authentication successful for %s, UserID: %s, Token: %s\n", reqPayload.Username, user.ID, sessionToken)
				respPayload = protocol.LoginResponsePayload{
					Success:      true,
					UserID:       user.ID,
					DisplayName:  user.DisplayName,
					SessionToken: sessionToken,
				}
				// TODO: Сохранить связь conn -> user.ID / sessionToken в Hub/Client структуре
			}
			sendWebSocketResponse(conn, protocol.MsgTypeLoginResponse, respPayload)

		case protocol.MsgTypeText:
			// TODO: Проверить, аутентифицирован ли пользователь (currentUserID != "")
			// прежде чем обрабатывать текстовое сообщение. Если нет - отправить ошибку.
			// if currentUserID == "" {
			//  sendErrorMessage(conn, "UNAUTHORIZED", "Please login to send messages.")
			//  continue
			// }

			var textData protocol.TextPayload
			if err := json.Unmarshal(receivedMsg.Payload, &textData); err != nil {
				log.Printf("Failed to unmarshal TextPayload: %v. RawPayload: %s\n", err, string(receivedMsg.Payload))
				sendErrorMessage(conn, "INVALID_PAYLOAD", "Could not parse text message payload.")
				continue
			}
			log.Printf("Text message from UserID %s (placeholder): %s\n", "UNKNOWN_YET", textData.Text)
			// TODO: Разослать это сообщение другим клиентам через Hub

		default:
			log.Printf("Received unknown message type: %s\n", receivedMsg.Type)
			sendErrorMessage(conn, "UNKNOWN_MESSAGE_TYPE", "The_server_does_not_understand_this_message_type.")
		}
	}
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
