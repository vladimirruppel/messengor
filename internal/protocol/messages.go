package protocol

import (
	"encoding/json"
)

const (
	MsgTypeText             = "TEXT_MESSAGE"
	MsgTypeRegisterRequest  = "REGISTER_REQUEST"
	MsgTypeRegisterResponse = "REGISTER_RESPONSE"
	MsgTypeLoginRequest     = "LOGIN_REQUEST"
	MsgTypeLoginResponse    = "LOGIN_RESPONSE"
)

type WebSocketMessage struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// TextPayload содержит данные для текстового сообщения.
type TextPayload struct {
	Text string `json:"text"`
}

type RegisterRequestPayload struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
}

type RegisterResponsePayload struct {
	
}

type LoginRequestPayload struct {
}

type LoginResponsePayload struct {
}
