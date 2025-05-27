package server

import "log"

// Hub управляет набором активных клиентов и рассылает им сообщения.
type Hub struct {
	clients    map[*Client]bool // Зарегистрированные клиенты
	broadcast  chan []byte      // Входящие сообщения от клиентов для рассылки
	register   chan *Client     // Канал для регистрации клиентов
	unregister chan *Client     // Канал для отмены регистрации клиентов
}

func NewHub() *Hub {
	return &Hub{
		broadcast:  make(chan []byte),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		clients:    make(map[*Client]bool),
	}
}

// Run запускает основной цикл хаба в отдельной горутине.
func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.clients[client] = true
			if client.IsAuthenticated {
				log.Printf("Hub: Client %s (ID: %s) registered. Total clients: %d", client.DisplayName, client.UserID, len(h.clients))
			} else {
				log.Printf("Hub: New client (conn: %p) registered (pending authentication). Total clients: %d", client.conn, len(h.clients))
			}
			// TODO: (Будущее) Уведомить других пользователей о новом подключении, если нужно.

		case client := <-h.unregister:
			// Клиент отключается. Проверяем, есть ли он в нашей карте.
			if _, ok := h.clients[client]; ok {
				// Удаляем клиента из карты.
				delete(h.clients, client)
				// Закрываем его канал `send`, чтобы `writePump` этого клиента завершился.
				close(client.send)

				if client.IsAuthenticated {
					log.Printf("Hub: Client %s (ID: %s) unregistered. Total clients: %d", client.DisplayName, client.UserID, len(h.clients))
				} else {
					log.Printf("Hub: Client (conn: %p) unregistered. Total clients: %d", client.conn, len(h.clients))
				}
				// TODO: (Будущее) Уведомить других пользователей об отключении, если нужно.
			}

		case message := <-h.broadcast:
			// Рассылаем сообщение всем подключенным клиентам
			log.Printf("Hub broadcasting message to %d clients. Message: %s", len(h.clients), string(message))
			for client := range h.clients {
				// Проверяем, не заблокируется ли отправка (если канал client.send переполнен или закрыт)
				select {
				case client.send <- message:
					// Сообщение успешно отправлено в канал клиента
				default:
					// Канал client.send закрыт или переполнен, клиент, вероятно, отвалился
					log.Printf("Failed to send message to client %s (ID: %s), unregistering.", client.DisplayName, client.UserID)
					close(client.send)        // Закрываем его канал (если еще не закрыт)
					delete(h.clients, client) // Удаляем из хаба
				}
			}
		}
	}
}
