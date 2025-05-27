package server

import (
	"encoding/json"
	"io"
	"log"
	"net/http"

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
	log.Println("Received request on /ws")

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
			continue
		}

		// Временно для визуализации как происходит обработка сообщения
		log.Printf("Received from client: Type=%s, RawPayload=%s\n", receivedMsg.Type, string(receivedMsg.Payload))

		switch receivedMsg.Type {
		case protocol.MsgTypeText:
			var textData protocol.TextPayload                     // ожидаем, что Payload для MsgTypeText - это TextPayload
			err := json.Unmarshal(receivedMsg.Payload, &textData) // распаковываем json.RawMessage
			if err != nil {
				log.Printf("Failed to unmarshal TextPayload: %v. RawPayload: %s\n", err, string(receivedMsg.Payload))
				continue
			}

			// вывод текста сообщения
			log.Printf("Text message from client: %s\n", textData.Text)

		default:
			log.Printf("Received unknown message type: %s\n", receivedMsg.Type)
		}
	}
}
