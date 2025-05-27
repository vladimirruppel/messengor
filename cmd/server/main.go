package main

import (
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

func handleWebSocketConnections(w http.ResponseWriter, r *http.Request) {
	log.Println("Received request on /ws")

	var conn, err = upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Failed to upgrade connection: %v\n", err)
		return
	}

	log.Println("Соединение установлено успешно!")
	conn.Close()
}

func main() {
	addr := "localhost:8088"

	http.HandleFunc("/ws", handleWebSocketConnections)

	log.Printf("Starting server on %s\n", addr)
	err := http.ListenAndServe(addr, nil)
	if err != nil {
		log.Fatal("ListenAndServer: ", err)
	}
}
