package client

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

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
	defer close(shutdownChan)

	for {
		_, messageBytes, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) ||
				websocket.IsUnexpectedCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				log.Println("Connection closed by server or client.")
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

		// Прямая обработка BroadcastText
		if wsMsg.Type == protocol.MsgTypeBroadcastText {
			var broadcastPayload protocol.BroadcastTextPayload
			if err := json.Unmarshal(wsMsg.Payload, &broadcastPayload); err != nil {
				log.Printf("Client: Failed to unmarshal BroadcastTextPayload: %v. Raw: %s\n", err, string(wsMsg.Payload))
				continue
			}
			// TODO: Улучшить отображение, чтобы не конфликтовать с вводом пользователя.
			// Возможно, передавать в AppState.ServerResponses и обрабатывать в UI-цикле через select.
			fmt.Printf("\r%*s\r", 80, "")
			fmt.Printf("[%s] (%s): %s\n",
				broadcastPayload.SenderName,
				time.Unix(broadcastPayload.Timestamp, 0).Format("15:04:05"),
				broadcastPayload.Text)
			fmt.Print("> ")
		} else {
			// Другие типы сообщений (ответы на запросы) отправляем в основной цикл для обработки
			select {
			case responsesChan <- wsMsg:
			default:
				// Канал переполнен
				log.Println("Warning: Client responsesChan is full. Message dropped.")
			}
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
