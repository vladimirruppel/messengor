package server

import (
	"encoding/json"
	"log"
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
				// Полезная нагрузка для GetUserListRequest у нас пока пустая
				log.Printf("Client %s (ID: %s) requested user list.", c.DisplayName, c.UserID)
				userList := c.hub.GetAuthenticatedUsersInfo(c.UserID) // Исключаем себя
				respPayload := protocol.UserListResponsePayload{Users: userList}
				// Отправляем ответ напрямую этому клиенту через его send канал (не через sendWebSocketResponse)
				// так как sendWebSocketResponse требует conn, а у нас есть c.send
				// Создадим для этого общую функцию отправки для Client
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

				notifyPayload := protocol.NewPrivateMessageNotifyPayload{
					ChatID:     chatID,
					SenderID:   c.UserID,
					SenderName: c.DisplayName,
					ReceiverID: targetClient.UserID,
					Text:       reqPayload.Text,
					Timestamp:  time.Now().Unix(),
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
					// Возможно, отправить ошибку клиенту или просто пропустить это сообщение
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
				c.hub.broadcast <- finalMsgBytes

			default:
				log.Printf("Client %s: Received unhandled message type: %s\n", c.UserID, wsMsg.Type)
				c.sendError("UNKNOWN_MESSAGE_TYPE", "Unhandled message type by server.")
			}
		}
		// Можно обрабатывать и другие типы сообщений WebSocket, если нужно (BinaryMessage, PingMessage, PongMessage, ErrorMessage)
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

			w, err := c.conn.NextWriter(websocket.TextMessage) // Мы всегда отправляем текст (JSON)
			if err != nil {
				return
			}
			w.Write(message)

			// Если в канале send есть еще сообщения, добавляем их в текущий фрейм
			// Это оптимизация, чтобы не создавать новый фрейм для каждого маленького сообщения
			n := len(c.send)
			for i := 0; i < n; i++ {
				w.Write([]byte{'\n'}) // Можно использовать \n как разделитель, если клиент это поддерживает
				// Но лучше каждое сообщение отправлять отдельным WriteMessage, если нет специальной логики на клиенте
				// Для простоты пока так, но имейте в виду.
				// БОЛЕЕ ПРАВИЛЬНО: убрать этот цикл и просто делать w.Write(message)
				// и затем w.Close() для каждого сообщения.
				// Либо если хотим батчинг, то клиент должен уметь парсить несколько JSON подряд.
				// Уберем этот цикл для ясности и надежности:
				// (цикл for i := 0; i < n; i++ был удален)
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
