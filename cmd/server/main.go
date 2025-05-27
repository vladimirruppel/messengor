package main

import (
	"log"
	"net/http"

	"github.com/vladimirruppel/messengor/internal/server"
)

func main() {
	addr := "localhost:8088"
	hub := server.NewHub()

	go hub.Run()

	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		server.HandleWebSocketConnections(hub, w, r)
	})

	log.Printf("Starting server on %s\n", addr)
	err := http.ListenAndServe(addr, nil)
	if err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
}
