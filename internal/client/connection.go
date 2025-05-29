package client

import (
	"encoding/json"
	"log"

	"github.com/gorilla/websocket"
	"github.com/vladimirruppel/messengor/internal/protocol"
)

func connectToServer(serverFullURL string) (*websocket.Conn, error) {
	log.Printf("Connecting to %s", serverFullURL)
	conn, resp, err := websocket.DefaultDialer.Dial(serverFullURL, nil) // Используем напрямую
	if err != nil {
		log.Printf("Dial error: %v", err)
		if resp != nil {
			log.Printf("HTTP response status: %s", resp.Status)
		}
		return nil, err
	}
	return conn, nil
}

// readFromServer читает сообщения от сервера и отправляет их в канал responsesChan.
// При ошибке или закрытии соединения отправляет сигнал в shutdownChan.
func readFromServer(conn *websocket.Conn, responsesChan chan<- protocol.WebSocketMessage, shutdownChan chan<- struct{}) {
	shouldCloseShutdownChan := true // Флаг, чтобы закрыть канал только один раз
	defer func() {
		if shouldCloseShutdownChan {
			close(shutdownChan)
		}
	}()

	for {
		_, messageBytes, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) ||
				websocket.IsUnexpectedCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Println("Connection closed by server or client (normal or expected).")
				shouldCloseShutdownChan = false
			} else {
				log.Printf("Read error: %v", err)
			}
			return // Выходим из горутины
		}

		var wsMsg protocol.WebSocketMessage
		if err := json.Unmarshal(messageBytes, &wsMsg); err != nil {
			log.Printf("Failed to unmarshal server message: %v. Raw: %s\n", err, string(messageBytes))
			continue
		}

		// Все типы сообщений отправляем в основной цикл для обработки
		// log.Printf("DEBUG: readFromServer putting message into responsesChan: Type=%s", wsMsg.Type) // Для отладки
		select {
		case responsesChan <- wsMsg:
			// Сообщение успешно отправлено в канал
		default:
			// Канал переполнен. Это плохо, значит основной цикл не успевает обрабатывать.
			// Для курсовой можно залогировать и отбросить, или увеличить буфер канала.
			log.Println("Warning: Client responsesChan is full. Server message dropped. Type:", wsMsg.Type)
		}
	}
}

// sendMessageToServer отправляет структурированное сообщение на сервер.
func sendMessageToServer(conn *websocket.Conn, msgType string, payloadData interface{}) error {
	payloadBytes, err := json.Marshal(payloadData)
	if err != nil {
		log.Printf("Failed to marshal payload for type %s: %v\n", msgType, err)
		return err
	}

	wsMsg := protocol.WebSocketMessage{
		Type:    msgType,
		Payload: payloadBytes,
	}

	messageBytes, err := json.Marshal(wsMsg)
	if err != nil {
		log.Printf("Failed to marshal WebSocket message: %v\n", err)
		return err
	}

	err = conn.WriteMessage(websocket.TextMessage, messageBytes)
	if err != nil {
		log.Printf("Write error for type %s: %v\n", msgType, err)
		return err
	}

	return nil
}
