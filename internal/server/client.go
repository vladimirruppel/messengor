package server

import (
	"encoding/json"
	"log"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/vladimirruppel/messengor/internal/protocol"
)

const (
	// Время, разрешенное для записи сообщения клиенту.
	writeWait = 10 * time.Second

	// Время, разрешенное для чтения следующего pong-сообщения от клиента.
	pongWait = 60 * time.Second

	// Отправлять pings клиенту с этим периодом. Должно быть меньше pongWait.
	pingPeriod = (pongWait * 9) / 10

	// Максимальный размер сообщения, разрешенный от клиента.
	maxMessageSize = 1024 * 10 // 10KB, можно настроить
)

// Client представляет одного подключенного пользователя через WebSocket.
type Client struct {
	hub *Hub // Ссылка на хаб, к которому принадлежит клиент

	conn *websocket.Conn // WebSocket соединение

	// Буферизованный канал для исходящих сообщений этому клиенту.
	// Хаб будет писать в этот канал, а writePump клиента будет читать из него.
	send chan []byte

	UserID          string // Идентификатор аутентифицированного пользователя
	DisplayName     string // Отображаемое имя пользователя
	IsAuthenticated bool   // Флаг, что клиент прошел аутентификацию
}

// readPump читает сообщения от клиента и передает их в хаб.
// Запускается в отдельной горутине для каждого клиента.
func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c // Отменяем регистрацию клиента в хабе
		c.conn.Close()        // Закрываем WebSocket соединение
	}()
	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error { c.conn.SetReadDeadline(time.Now().Add(pongWait)); return nil })

	for {
		messageType, messageBytes, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure, websocket.CloseNoStatusReceived) {
				log.Printf("Unexpected close error for client %s: %v", c.UserID, err)
			} else {
				log.Printf("Read error for client %s: %v", c.UserID, err)
			}
			break // Выход из цикла при ошибке чтения
		}

		if messageType == websocket.TextMessage {
			// Десериализуем общее сообщение, чтобы проверить тип
			var wsMsg protocol.WebSocketMessage
			if err := json.Unmarshal(messageBytes, &wsMsg); err != nil {
				log.Printf("Client %s: Failed to unmarshal message: %v. Raw: %s", c.UserID, err, string(messageBytes))
				continue
			}

			switch wsMsg.Type {
			case protocol.MsgTypeGetUserListRequest:
				log.Printf("Client %s (ID: %s) requested user list.", c.DisplayName, c.UserID)
				userList := c.hub.GetAuthenticatedUsersInfo(c.UserID) // Исключаем себя
				respPayload := protocol.UserListResponsePayload{Users: userList}
				c.sendResponse(protocol.MsgTypeUserListResponse, respPayload)

			case protocol.MsgTypeSendPrivateMessageRequest:
				var reqPayload protocol.SendPrivateMessageRequestPayload
				if err := json.Unmarshal(wsMsg.Payload, &reqPayload); err != nil {
					log.Printf("Client %s: Failed to unmarshal SendPrivateMessageRequest payload: %v\n", c.UserID, err)
					c.sendError("INVALID_PAYLOAD", "Could not parse private message request payload.")
					continue
				}

				log.Printf("Client %s sending private message to UserID: %s.", c.DisplayName, reqPayload.TargetUserID)

				targetClient, found := c.hub.FindClientByUserID(reqPayload.TargetUserID)
				if !found {
					log.Printf("Client %s: Target user ID %s for private message not found or not online.", c.UserID, reqPayload.TargetUserID)
					c.sendError("USER_NOT_FOUND", "Recipient is not online or does not exist.")
					continue
				}

				chatID, chatIDErr := GeneratePrivateChatID(c.UserID, targetClient.UserID)
				if chatIDErr != nil {
					log.Printf("Client %s: Error generating ChatID for private message: %v", c.UserID, chatIDErr)
					c.sendError("INTERNAL_ERROR", "Could not process private message.")
					continue
				}

				storedMsg, errSave := SaveMessage(chatID, c.UserID, c.DisplayName, reqPayload.Text)
				if errSave != nil {
					log.Printf("Error saving private message to history for chat %s: %v", chatID, errSave)
					c.sendError("HISTORY_SAVE_FAILED", "Could not save your message.")
				}

				notifyPayload := protocol.NewPrivateMessageNotifyPayload{
					ChatID:     chatID,
					MessageID:  "",
					SenderID:   c.UserID,
					SenderName: c.DisplayName,
					ReceiverID: targetClient.UserID,
					Text:       reqPayload.Text,
					Timestamp:  time.Now().Unix(),
				}

				if storedMsg != nil { // Если сохранение было успешным
					notifyPayload.MessageID = storedMsg.MessageID
					notifyPayload.Timestamp = storedMsg.Timestamp // Используем timestamp сохраненного сообщения
				}

				// Отправляем получателю
				targetClient.sendResponse(protocol.MsgTypeNewPrivateMessageNotify, notifyPayload)
				// Отправляем "эхо" отправителю
				c.sendResponse(protocol.MsgTypeNewPrivateMessageNotify, notifyPayload)

			case protocol.MsgTypeText: // Это для Global Broadcast (если клиент шлет MsgTypeText)
				var textPayload protocol.TextPayload
				if err := json.Unmarshal(wsMsg.Payload, &textPayload); err != nil {
					log.Printf("Client %s: Failed to unmarshal TextPayload for broadcast: %v", c.UserID, err)
					c.sendError("INVALID_PAYLOAD", "Could not parse text payload for broadcast.")
					continue
				}

				broadcastData := protocol.BroadcastTextPayload{
					SenderID:   c.UserID,
					SenderName: c.DisplayName,
					Text:       textPayload.Text,
					Timestamp:  time.Now().Unix(),
				}
				broadcastPayloadBytes, err := json.Marshal(broadcastData)
				if err != nil {
					log.Printf("Client %s: Error marshalling broadcast payload: %v", c.UserID, err)
					continue
				}
				broadcastWsMsg := protocol.WebSocketMessage{
					Type:    protocol.MsgTypeBroadcastText,
					Payload: broadcastPayloadBytes,
				}
				finalMsgBytes, err := json.Marshal(broadcastWsMsg)
				if err != nil {
					log.Printf("Client %s: Error marshalling final broadcast message: %v", c.UserID, err)
					continue
				}

				globalChatID := "global_broadcast"
				_, errSave := SaveMessage(globalChatID, c.UserID, c.DisplayName, textPayload.Text)
				if errSave != nil {
					log.Printf("Error saving broadcast message to history for chat %s: %v", globalChatID, errSave)
					// Решаем, продолжать ли отправку, если сохранение не удалось. Для MVP - да.
				}

				c.hub.broadcast <- finalMsgBytes

			case protocol.MsgTypeGetChatHistoryRequest:
				var reqPayload protocol.GetChatHistoryRequestPayload
				if err := json.Unmarshal(wsMsg.Payload, &reqPayload); err != nil {
					log.Printf("Client %s: Failed to unmarshal GetChatHistoryRequest payload: %v\n", c.UserID, err)
					c.sendError("INVALID_PAYLOAD", "Could not parse get history request payload.")
					continue
				}

				log.Printf("Client %s (ID: %s) requested history for chat: %s (Limit: %d)",
					c.DisplayName, c.UserID, reqPayload.ChatID, reqPayload.Limit)

				// Проверка прав доступа: может ли этот UserID читать историю этого ChatID?
				// Для личных чатов: UserID должен быть одним из участников ChatID.
				// ChatID у нас вида "private:id1:id2". Проверим, что c.UserID есть в нем.
				// Для broadcast чата ("global_broadcast") доступ разрешен всем аутентифицированным.
				canAccess := false
				if reqPayload.ChatID == "global_broadcast" { // Имя для broadcast чата
					canAccess = true
				} else if strings.HasPrefix(reqPayload.ChatID, "private:") {
					parts := strings.Split(reqPayload.ChatID, ":")
					if len(parts) == 3 && (parts[1] == c.UserID || parts[2] == c.UserID) {
						canAccess = true
					}
				}

				if !canAccess {
					log.Printf("Client %s (ID: %s) - Access denied for chat history: %s", c.DisplayName, c.UserID, reqPayload.ChatID)
					c.sendError("ACCESS_DENIED", "You do not have permission to access this chat history.")
					continue
				}

				// Устанавливаем лимит по умолчанию, если не указан или слишком большой
				limit := reqPayload.Limit
				if limit <= 0 || limit > 100 { // Максимум 100 сообщений за раз
					limit = 50
				}

				messages, err := LoadChatHistory(reqPayload.ChatID, limit)
				if err != nil {
					log.Printf("Client %s: Error loading history for chat %s: %v", c.UserID, reqPayload.ChatID, err)
					c.sendError("HISTORY_LOAD_FAILED", "Could not load chat history.")
					continue
				}

				respPayload := protocol.ChatHistoryResponsePayload{
					ChatID:   reqPayload.ChatID,
					Messages: messages,
					// HasMore: true/false - можно добавить, если реализована пагинация
				}
				c.sendResponse(protocol.MsgTypeChatHistoryResponse, respPayload)

			default:
				log.Printf("Client %s: Received unhandled message type: %s\n", c.UserID, wsMsg.Type)
				c.sendError("UNKNOWN_MESSAGE_TYPE", "Unhandled message type by server.")
			}
		}
	}
}

// writePump отправляет сообщения из хаба (через канал client.send) клиенту.
// Запускается в отдельной горутине для каждого клиента.
func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close() // Закрываем соединение, если выходим из writePump
	}()
	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// Канал c.send был закрыт (хаб удалил этого клиента).
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			// Если в канале send есть еще сообщения, добавляем их в текущий фрейм
			// Это оптимизация, чтобы не создавать новый фрейм для каждого маленького сообщения
			n := len(c.send)
			for i := 0; i < n; i++ {
				w.Write([]byte{'\n'})
			}

			if err := w.Close(); err != nil {
				return
			}
		case <-ticker.C: // Таймер для отправки ping-сообщений
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return // Ошибка при отправке ping, вероятно, соединение потеряно
			}
		}
	}
}

// sendResponse - вспомогательный метод для Client для отправки ответа/уведомления
func (c *Client) sendResponse(msgType string, payloadData interface{}) {
	payloadBytes, err := json.Marshal(payloadData)
	if err != nil {
		log.Printf("Client %s: Error marshalling payload for type %s: %v\n", c.UserID, msgType, err)
		return
	}
	wsMsg := protocol.WebSocketMessage{Type: msgType, Payload: payloadBytes}
	messageBytes, err := json.Marshal(wsMsg)
	if err != nil {
		log.Printf("Client %s: Error marshalling WebSocket message for type %s: %v\n", c.UserID, msgType, err)
		return
	}

	select {
	case c.send <- messageBytes:
	default:
		log.Printf("Client %s: Send channel full or closed when trying to send %s.", c.UserID, msgType)
		// Хаб должен будет обработать отписку этого клиента, если он не может принимать сообщения.
	}
}

// sendError - вспомогательный метод для Client для отправки сообщения об ошибке
func (c *Client) sendError(errorCode string, errorMessage string) {
	payload := protocol.ErrorPayload{
		ErrorCode:    errorCode,
		ErrorMessage: errorMessage,
	}
	log.Printf("Sending error to client %s: Code=%s, Message=%s\n", c.UserID, errorCode, errorMessage)
	c.sendResponse(protocol.MsgTypeErrorNotify, payload)
}
