package protocol

import (
	"encoding/json"
)

type WebSocketMessage struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

const (
	MsgTypeText             = "TEXT_MESSAGE"
	MsgTypeRegisterRequest  = "REGISTER_REQUEST"
	MsgTypeRegisterResponse = "REGISTER_RESPONSE"
	MsgTypeLoginRequest     = "LOGIN_REQUEST"
	MsgTypeLoginResponse    = "LOGIN_RESPONSE"
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
