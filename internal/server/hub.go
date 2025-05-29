package server

import (
	"log"
	"sync"

	"github.com/vladimirruppel/messengor/internal/protocol"
)

// Hub управляет набором активных клиентов и рассылает им сообщения.
type Hub struct {
	broadcast  chan []byte  // Входящие сообщения от клиентов для рассылки
	register   chan *Client // Канал для регистрации клиентов
	unregister chan *Client // Канал для отмены регистрации клиентов

	clients      map[*Client]bool // Зарегистрированные клиенты
	clientsMutex sync.RWMutex     // Мьютекс для защиты доступа к карте clients
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
			h.clientsMutex.Lock()
			h.clients[client] = true
			h.clientsMutex.Unlock()
			if client.IsAuthenticated {
				log.Printf("Hub: Client %s (ID: %s) registered. Total clients: %d", client.DisplayName, client.UserID, len(h.clients))
			} else {
				log.Printf("Hub: New client (conn: %p) registered (pending authentication). Total clients: %d", client.conn, len(h.clients))
			}

		case client := <-h.unregister:
			h.clientsMutex.Lock()
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
			}
			h.clientsMutex.Unlock()

		case message := <-h.broadcast:
			h.clientsMutex.RLock()
			log.Printf("Hub: Broadcasting message to %d clients. Message approx: %s", len(h.clients), string(message))
			for client := range h.clients {
				if client.IsAuthenticated {
					select {
					case client.send <- message:
					default:
						log.Printf("Hub: Client %s (ID: %s) send channel full/closed during broadcast. Initiating unregister.", client.DisplayName, client.UserID)
						go func(clToUnregister *Client) {
							h.unregister <- clToUnregister
						}(client)
					}
				}
			}
			h.clientsMutex.RUnlock()
		}
	}
}

// getClientCount возвращает текущее количество клиентов (вспомогательная функция для использования внутри Lock/RLock).
// Эту функцию лучше вызывать, когда мьютекс уже захвачен.
func (h *Hub) getClientCount() int {
	return len(h.clients)
}

// GetAuthenticatedUsersInfo возвращает список информации об аутентифицированных пользователях,
// исключая пользователя с excludeUserID.
func (h *Hub) GetAuthenticatedUsersInfo(excludeUserID string) []protocol.UserInfo {
	h.clientsMutex.RLock()
	defer h.clientsMutex.RUnlock()

	var usersInfo []protocol.UserInfo
	for client := range h.clients {
		if client.IsAuthenticated && client.UserID != excludeUserID {
			usersInfo = append(usersInfo, protocol.UserInfo{
				UserID:      client.UserID,
				DisplayName: client.DisplayName,
				IsOnline:    true, // Если клиент в хабе и аутентифицирован, он онлайн
			})
		}
	}
	return usersInfo
}

// FindClientByUserID ищет активного и аутентифицированного клиента по его UserID.
func (h *Hub) FindClientByUserID(userID string) (*Client, bool) {
	h.clientsMutex.RLock()
	defer h.clientsMutex.RUnlock()

	for client := range h.clients {
		// Убедимся, что ищем только среди аутентифицированных клиентов
		if client.IsAuthenticated && client.UserID == userID {
			return client, true
		}
	}
	return nil, false
}
