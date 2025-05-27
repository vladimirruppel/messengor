package main

import (
	"flag"
	"log"
	"net/url"
	"time"

	"github.com/gorilla/websocket"
)

var addr = flag.String("addr", "localhost:8088", "http service address")

func main() {
	u := url.URL{Scheme: "ws", Host: *addr, Path: "/ws"}
	log.Printf("connecting to %s", u.String())

	conn, resp, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		log.Printf("Failed to connect to WebSocket server: %v", err)
		if resp != nil {
			log.Printf("  HTTP Response Status: %s", resp.Status)
		}
		return
	}
	defer conn.Close()

	log.Println("WebSocket connection established successfully!")
	time.Sleep(2 * time.Second)
	log.Println("Client exiting.")
}
