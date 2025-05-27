package server

import (
	"io"
	"log"
	"net/http"

	"github.com/gorilla/websocket"
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
		messageType, p, err := conn.ReadMessage()
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

		log.Printf("Received from client (type %d): %s\n", messageType, string(p))
	}
}
