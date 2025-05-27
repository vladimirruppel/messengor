package client

import "github.com/vladimirruppel/messengor/internal/protocol"

// Состояния клиента
const (
	StateUnauthenticated = "UNAUTHENTICATED"
	StateAuthenticating  = "AUTHENTICATING"
	StateChatList        = "CHAT_LIST"
	StateInChat          = "IN_CHAT"
)

// AppState хранит текущее состояние всего клиентского приложения.
type AppState struct {
	Current         string
	UserID          string
	DisplayName     string
	SessionToken    string
	ServerResponses chan protocol.WebSocketMessage // Канал для получения сообщений от сервера
}

func NewAppState() *AppState {
	return &AppState{
		Current:         StateUnauthenticated,
		ServerResponses: make(chan protocol.WebSocketMessage, 10), // Буферизованный
	}
}
