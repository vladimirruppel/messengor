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
			log.Printf("DEBUG: Server for %s (ID: %s) - Registering client INSIDE hub...", client.DisplayName, client.UserID)
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
			if len(message) > 0 {
				log.Printf("Hub: Broadcasting message to %d clients. Message approx: %s", len(h.clients), string(message))
				for client := range h.clients {
					// Рассылаем только аутентифицированным клиентам
					// (Хотя в нашем Hub.clients должны быть только аутентифицированные, если
					// регистрация в Hub происходит после успешной аутентификации в HandleWebSocketConnections)
					if client.IsAuthenticated { // Двойная проверка не помешает
						select {
						case client.send <- message:
							// Сообщение успешно поставлено в очередь на отправку клиенту
						default:
							log.Printf("Hub: Failed to send broadcast to client %s (ID: %s), unregistering.", client.DisplayName, client.UserID)
							// Канал client.send закрыт или переполнен.
							// Удаляем клиента из хаба, так как он, вероятно, "завис" или отсоединился.
							// Важно: не закрывать client.send здесь снова, если он уже может быть закрыт
							// в процессе отмены регистрации. Просто удаляем из карты.
							// close(client.send) // Это может вызвать панику, если канал уже закрыт.
							// Лучше, если unregister сам этим занимается.
							// Давайте упростим: если не можем отправить, просто удаляем из карты.
							// Hub.unregister позаботится о закрытии канала send.
							// h.unregister <- client // Отправляем в канал unregister для корректной отписки
							// НО! Отправка в unregister из этого же select-цикла может привести к дедлоку,
							// если unregister-канал не буферизован и никто его не читает.
							// Безопаснее просто удалить из карты и положиться на то, что read/write pump
							// этого клиента сами отпишутся.
							// Или запустить отписку в горутине.
							// Самое простое и безопасное на данном этапе:
							delete(h.clients, client)
							// Если мы хотим, чтобы writePump клиента корректно завершился,
							// его канал send нужно закрыть.
							// Но если мы просто делаем delete, а клиент еще жив, его writePump не узнает,
							// что его удалили из рассылки.
							// Поэтому правильнее инициировать unregister.
							// Для избежания дедлока, можно запустить в горутине:
							go func(clToUnregister *Client) {
								log.Printf("Hub: Client %s (ID: %s) send channel full/closed during broadcast. Initiating unregister.", clToUnregister.DisplayName, clToUnregister.UserID)
								h.unregister <- clToUnregister
							}(client)
						}
					}
				}
			}
		}
	}
}
