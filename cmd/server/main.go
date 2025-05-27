package main

import (
	"log"
	"net/http"

	"github.com/vladimirruppel/messengor/internal/server"
)

func main() {
	addr := "localhost:8088"

	http.HandleFunc("/ws", server.HandleWebSocketConnections)

	log.Printf("Starting server on %s\n", addr)
	err := http.ListenAndServe(addr, nil)
	if err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
}
