package client

import (
	"bufio"
	"encoding/json"
	"flag"
	"log"
	"net/url"
	"os"
	"strings"

	"github.com/gorilla/websocket"
	"github.com/vladimirruppel/messengor/internal/protocol"
)

var addr = flag.String("addr", "localhost:8088", "http service address")

func RunClient(serverAddr string) {
	u := url.URL{Scheme: "ws", Host: *addr, Path: "/ws"}
	log.Printf("connecting to %s", u.String())

	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		log.Fatalf("Failed to connect to WebSocket server: %v", err)
	}
	defer conn.Close()

	log.Println("WebSocket connection established successfully!")

	// Канал для сигнализации о том, что горутина чтения завершилась
	done := make(chan struct{})

	// Горутина для чтения сообщений от сервера
	go func() {
		defer close(done) // Закрываем канал done, когда эта горутина завершается
		for {
			messageType, message, err := conn.ReadMessage()
			if err != nil {
				log.Println("Read error:", err)
				// Если ошибка чтения, значит соединение, скорее всего, разорвано.
				// Выходим из цикла (и горутины).
				return
			}
			log.Printf("Received from server (type %d): %s\n", messageType, string(message))
		}
	}()

	// Основной цикл для чтения пользовательского ввода и отправки сообщений
	// Этот цикл будет работать в основной горутине (main)
	reader := bufio.NewReader(os.Stdin)
	log.Println("Enter message to send (or type '[[exit]]' to quit):")

	for {
		print("> ")
		text, _ := reader.ReadString('\n') // Читаем строку до Enter
		text = strings.TrimSpace(text)     // Убираем лишние пробелы и символ новой строки

		if text == "[[exit]]" { // Условие для выхода из клиента
			log.Println("Exiting client...")
			err := conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			if err != nil {
				log.Println("write close:", err)
				return
			}
			break
		}

		// Создаем полезную нагрузку (payload) для текстового сообщения
		textData := protocol.TextPayload{
			Text: text,
		}

		// Сериализуем полезную нагрузку в JSON.
		payloadBytes, err := json.Marshal(textData)
		if err != nil {
			log.Printf("Failed to marshal text payload: %v\n", err)
			continue // Пропускаем отправку этого сообщения, если не смогли сериализовать payload
		}

		// Создаем "конверт" WebSocketMessage
		wsMsg := protocol.WebSocketMessage{
			Type:    protocol.MsgTypeText,
			Payload: payloadBytes,
		}

		// Сериализуем весь "конверт" в JSON
		messageBytes, err := json.Marshal(wsMsg)
		if err != nil {
			log.Printf("Failed to marshal WebSocket message: %v\n", err)
			continue // Пропускаем отправку
		}

		// Отправляем текстовое сообщение на сервер
		err = conn.WriteMessage(websocket.TextMessage, messageBytes)
		if err != nil {
			log.Println("Write error:", err)
			break // вероятно, потеряно соединение
		}
		log.Printf("Sent to server: %s\n", text)
	}

	log.Println("Client main input loop finished.")

	<-done // Ждем сигнала от горутины чтения, что она завершилась

	log.Println("Client exiting completely.")
}
