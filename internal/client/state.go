package client

import "github.com/vladimirruppel/messengor/internal/protocol"

// Состояния клиента
const (
	StateUnauthenticated             = "UNAUTHENTICATED"
	StateAuthenticating              = "AUTHENTICATING"
	StateMainMenu                    = "MAIN_MENU"
	StateFetchingUserList            = "FETCHING_USER_LIST"
	StateUserSelectionForPrivateChat = "USER_SELECTION_PRIVATE_CHAT"
	StateOpeningPrivateChat          = "OPENING_PRIVATE_CHAT"
	StateInChat                      = "IN_CHAT"
	StateInPrivateChat               = "IN_PRIVATE_CHAT"
)

// AppState хранит текущее состояние всего клиентского приложения.
type AppState struct {
	Current         string
	UserID          string
	DisplayName     string
	SessionToken    string
	ServerResponses chan protocol.WebSocketMessage // Канал для получения сообщений от сервера

	AvailableUsers               []protocol.UserInfo // Список пользователей для выбора
	CurrentChatID                string              // ID текущего активного чата (личного или группового)
	CurrentChatTargetUserID      string              // Для личных чатов - ID собеседника
	CurrentChatTargetDisplayName string              // Для личных чатов - Имя собеседника
}

func NewAppState() *AppState {
	return &AppState{
		Current:         StateUnauthenticated,
		ServerResponses: make(chan protocol.WebSocketMessage, 10), // Буферизованный
		AvailableUsers:  make([]protocol.UserInfo, 0),
	}
}

func (s *AppState) ClearAuthData() {
	s.UserID = ""
	s.DisplayName = ""
	s.SessionToken = ""
	s.CurrentChatID = ""
	s.CurrentChatTargetUserID = ""
	s.CurrentChatTargetDisplayName = ""
	s.AvailableUsers = nil
}
