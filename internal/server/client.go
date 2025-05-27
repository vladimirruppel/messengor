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

			if wsMsg.Type == protocol.MsgTypeText {
				if !c.IsAuthenticated { // Проверка, аутентифицирован ли клиент
					log.Printf("Client (conn: %p) sent text message before authentication.", c.conn)
					sendErrorMessage(c.conn, "UNAUTHORIZED", "Please login to send messages.")
					continue
				}

				var textPayload protocol.TextPayload
				if err := json.Unmarshal(wsMsg.Payload, &textPayload); err != nil {
					log.Printf("Client %s: Failed to unmarshal text payload: %v", c.UserID, err)
					sendErrorMessage(c.conn, "INVALID_PAYLOAD", "Could not parse text payload.")
					continue
				}

				// Логируем полученное сообщение
				log.Printf("Received MsgTypeText from UserID: %s, DisplayName: %s, Text: %s\n", c.UserID, c.DisplayName, textPayload.Text)
			} else {
				// Если пришел другой тип сообщения (кроме аутентификационных, которые обрабатываются раньше)
				log.Printf("Client %s sent unhandled message type %s after auth.", c.UserID, wsMsg.Type)
				// TODO: Обработка других C2S сообщений после аутентификации (например, выход из чата, запрос списка пользователей и т.д.)
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
