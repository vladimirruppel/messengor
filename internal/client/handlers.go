package client

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/vladimirruppel/messengor/internal/protocol"
)

// handleUnauthenticatedState будет вызываться из ClientApp.processCurrentState
func handleUnauthenticatedState(app *ClientApp) {
	displayUnauthenticatedMenu()
	input, _ := app.reader.ReadString('\n')
	choice := strings.TrimSpace(input)
	switch choice {
	case "1":
		handleUserInputLogin(app)
	case "2":
		handleUserInputRegister(app)
	case "3":
		log.Println("Exiting...")
		close(app.shutdownChan) // Сигнализируем о завершении
	default:
		log.Println("Invalid option. Please try again.")
	}
}

func handleUserInputRegister(app *ClientApp) {
	fmt.Print("Enter username: ")
	username, _ := app.reader.ReadString('\n')
	username = strings.TrimSpace(username)
	if username == "" {
		log.Println("Username cannot be empty.")
		return // Возвращаемся в предыдущее меню/состояние (обработается в processCurrentState)
	}

	fmt.Print("Enter password: ")
	password, _ := app.reader.ReadString('\n')
	password = strings.TrimSpace(password)
	if password == "" {
		log.Println("Password cannot be empty.")
		return
	}
	// TODO: Можно добавить проверку сложности пароля или подтверждение пароля

	fmt.Print("Enter display name: ")
	displayName, _ := app.reader.ReadString('\n')
	displayName = strings.TrimSpace(displayName)
	if displayName == "" {
		log.Println("Display name cannot be empty.")
		return
	}

	// Формируем полезную нагрузку для запроса регистрации
	payload := protocol.RegisterRequestPayload{
		Username:    username,
		Password:    password, // Пароль отправляется "как есть", сервер будет хешировать
		DisplayName: displayName,
	}

	log.Println("Sending registration request to server...")
	err := sendMessageToServer(app.conn, protocol.MsgTypeRegisterRequest, payload)
	if err != nil {
		log.Printf("Failed to send registration request: %v. Check connection.", err)
		return
	}

	app.state.Current = StateAuthenticating
	log.Println("Registration request sent. Waiting for server response...")
}

// handleUserInputLogin запрашивает данные и отправляет LoginRequest
func handleUserInputLogin(app *ClientApp) {
	fmt.Print("Enter username: ")
	username, _ := app.reader.ReadString('\n')
	username = strings.TrimSpace(username)

	fmt.Print("Enter password: ")
	password, _ := app.reader.ReadString('\n')
	password = strings.TrimSpace(password)

	payload := protocol.LoginRequestPayload{
		Username: username,
		Password: password,
	}
	if err := sendMessageToServer(app.conn, protocol.MsgTypeLoginRequest, payload); err == nil {
		app.state.Current = StateAuthenticating
	} else {
		log.Println("Failed to send login request. Please check connection.")
	}
}

// processServerResponse обрабатывает ответы от сервера, влияющие на состояние
func (app *ClientApp) processServerResponse(wsMsg protocol.WebSocketMessage) {
	switch wsMsg.Type {
	case protocol.MsgTypeLoginResponse:
		var respPayload protocol.LoginResponsePayload
		if err := json.Unmarshal(wsMsg.Payload, &respPayload); err != nil {
			log.Printf("Failed to unmarshal LoginResponse payload: %v\n", err)
			app.state.Current = StateUnauthenticated
			return
		}
		if respPayload.Success {
			log.Printf("Login successful! Welcome, %s.\n", respPayload.DisplayName)
			app.state.UserID = respPayload.UserID
			app.state.DisplayName = respPayload.DisplayName
			app.state.SessionToken = respPayload.SessionToken
			app.state.Current = StateChatList
		} else {
			log.Printf("Login failed: %s\n", respPayload.ErrorMessage)
			app.state.Current = StateUnauthenticated
		}
		// ... другие case для RegisterResponse и т.д. ...
	case protocol.MsgTypeRegisterResponse:
		var respPayload protocol.RegisterResponsePayload
		if err := json.Unmarshal(wsMsg.Payload, &respPayload); err != nil {
			log.Printf("Failed to unmarshal RegisterResponse payload: %v\n", err)
			app.state.Current = StateUnauthenticated
			return
		}
		if respPayload.Success {
			log.Println("Registration successful! Please login.")
			app.state.Current = StateUnauthenticated
		} else {
			log.Printf("Registration failed: %s\n", respPayload.ErrorMessage)
			app.state.Current = StateUnauthenticated
		}
	default:
		log.Printf("Received unhandled server response type %s in main loop.\n", wsMsg.Type)
	}
}

func handleAuthenticatingState(app *ClientApp) {
	log.Println("Waiting for server response...")
	select {
	case wsMsg, ok := <-app.state.ServerResponses:
		if !ok {
			log.Println("Server connection lost while authenticating. Returning to unauthenticated state.")
			app.state.Current = StateUnauthenticated
			close(app.shutdownChan) // Сигнализируем о необходимости завершения
			return
		}
		app.processServerResponse(wsMsg) // Используем метод для обработки ответа
	case <-time.After(15 * time.Second): // Увеличил таймаут
		log.Println("Timeout waiting for server response.")
		app.state.Current = StateUnauthenticated
	case <-app.shutdownChan: // Если readFromServer уже завершился
		log.Println("Shutdown signal received while authenticating.")
		// Ничего не делаем, главный цикл завершится
	}
}

// Заглушки для остальных обработчиков состояний
func handleChatListState(app *ClientApp) {
	displayMainMenu(app.state.DisplayName, app.state.UserID)
	input, _ := app.reader.ReadString('\n')
	choice := strings.TrimSpace(input)

	switch choice {
	case "1":
		app.state.Current = StateInChat
	case "2":
		log.Println("Logging out...")
		// TODO: Отправить C2S_LOGOUT_REQUEST
		app.state.UserID = ""
		app.state.DisplayName = ""
		app.state.SessionToken = ""
		app.state.Current = StateUnauthenticated
	case "3":
		log.Println("Exiting...")
		close(app.shutdownChan)
	default:
		log.Println("Invalid option.")
	}
}

func handleInChatState(app *ClientApp) {
	fmt.Print("> ")
	chatInput, _ := app.reader.ReadString('\n')
	chatInput = strings.TrimSpace(chatInput)

	if chatInput == "[[back]]" {
		app.state.Current = StateChatList
	} else if chatInput != "" {
		// Отправляем текстовое сообщение
		payload := protocol.TextPayload{Text: chatInput}
		sendMessageToServer(app.conn, protocol.MsgTypeText, payload) // Используем новую функцию
	}
}
