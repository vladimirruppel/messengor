package protocol

import (
	"encoding/json"
)

type WebSocketMessage struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

const (
	MsgTypeText                      = "TEXT_MESSAGE"
	MsgTypeRegisterRequest           = "REGISTER_REQUEST"
	MsgTypeRegisterResponse          = "REGISTER_RESPONSE"
	MsgTypeLoginRequest              = "LOGIN_REQUEST"
	MsgTypeLoginResponse             = "LOGIN_RESPONSE"
	MsgTypeBroadcastText             = "BROADCAST_TEXT_MESSAGE"
	MsgTypeGetUserListRequest        = "GET_USER_LIST_REQUEST"        // C->S: Запрос списка пользователей
	MsgTypeUserListResponse          = "USER_LIST_RESPONSE"           // S->C: Ответ со списком пользователей
	MsgTypeSendPrivateMessageRequest = "SEND_PRIVATE_MESSAGE_REQUEST" // C->S: Отправка личного сообщения
	MsgTypeNewPrivateMessageNotify   = "NEW_PRIVATE_MESSAGE_NOTIFY"   // S->C: Уведомление о новом личном сообщении (обоим участникам)
)

///
/// PAYLOAD STRUCTURES
///

type TextPayload struct {
	Text string `json:"text"`
}

// RegisterRequestPayload содержит данные для запроса регистрации.
type RegisterRequestPayload struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
}

// RegisterResponsePayload содержит данные для ответа на регистрацию.
type RegisterResponsePayload struct {
	Success      bool   `json:"success"`
	UserID       string `json:"user_id,omitempty"`       // Используем string для UUID, omitempty если ошибка
	ErrorMessage string `json:"error_message,omitempty"` // omitempty если успех
}

// LoginRequestPayload содержит данные для запроса входа.
type LoginRequestPayload struct {
	Username string `json:"username"`
	Password string `json:"password"` // Клиент отправляет пароль, сервер проверяет хеш
}

// LoginResponsePayload содержит данные для ответа на вход.
type LoginResponsePayload struct {
	Success      bool   `json:"success"`
	UserID       string `json:"user_id,omitempty"`       // omitempty если ошибка
	DisplayName  string `json:"display_name,omitempty"`  // omitempty если ошибка
	SessionToken string `json:"session_token,omitempty"` // Токен сессии, omitempty если ошибка
	ErrorMessage string `json:"error_message,omitempty"` // omitempty если успех
}

// Общая структура для сообщений об ошибках от сервера,
// которые не являются ответом на конкретный запрос (или для общих ошибок в ответах)
type ErrorPayload struct {
	ErrorCode    string `json:"error_code"`
	ErrorMessage string `json:"error_message"`
}

type BroadcastTextPayload struct {
	SenderID   string `json:"sender_id"`
	SenderName string `json:"sender_name"`
	Text       string `json:"text"`
	Timestamp  int64  `json:"timestamp"`
}

// UserInfo содержит публичную информацию о пользователе.
type UserInfo struct {
	UserID      string `json:"user_id"`
	DisplayName string `json:"display_name"`
	IsOnline    bool   `json:"is_online"`
}

// GetUserListRequestPayload - полезная нагрузка для запроса списка пользователей.
// Может быть пустой или содержать фильтры в будущем.
type GetUserListRequestPayload struct {
	// Filter string `json:"filter,omitempty"` // Например, для поиска по имени
}

// UserListResponsePayload содержит список пользователей.
type UserListResponsePayload struct {
	Users []UserInfo `json:"users"`
}

// SendPrivateMessageRequestPayload содержит данные для отправки личного сообщения.
type SendPrivateMessageRequestPayload struct {
	TargetUserID string `json:"target_user_id"` // Кому предназначено сообщение
	Text         string `json:"text"`           // Текст сообщения
	// ClientMessageID string `json:"client_message_id,omitempty"` // Для отслеживания доставки, если нужно
}

// NewPrivateMessageNotifyPayload содержит данные нового личного сообщения.
// Отправляется и получателю, и отправителю (для синхронизации UI).
type NewPrivateMessageNotifyPayload struct {
	ChatID       string `json:"chat_id"`        // Уникальный ID для этой личной беседы (например, user1ID:user2ID)
	SenderID     string `json:"sender_id"`    // ID отправителя
	SenderName   string `json:"sender_name"`  // Имя отправителя
	ReceiverID   string `json:"receiver_id"`  // ID получателя (полезно для клиента, чтобы понять, это ему или от него)
	Text         string `json:"text"`
	Timestamp    int64  `json:"timestamp"`    // Unix time
	// ServerMessageID string `json:"server_message_id"` // ID сообщения на сервере, для истории и т.д.
}