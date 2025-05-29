package client

import (
	"encoding/json"
	"fmt"
	"log"
	"strconv"
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
			app.state.Current = StateMainMenu
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

func handleMainMenuState(app *ClientApp) {
	displayMainMenu(app.state.DisplayName, app.state.UserID)
	input, _ := app.reader.ReadString('\n')
	choice := strings.TrimSpace(input)

	switch choice {
	case "1": // Enter Global Chat
		app.state.Current = StateInChat // StateInChat будет для глобального чата
	case "2": // Start Private Chat
		log.Println("Requesting user list from server...")
		// Формируем пустой payload, так как GetUserListRequestPayload у нас пока пустой
		payload := protocol.GetUserListRequestPayload{}
		err := sendMessageToServer(app.conn, protocol.MsgTypeGetUserListRequest, payload)
		if err != nil {
			log.Printf("Failed to send GetUserListRequest: %v. Returning to main menu.", err)
			// Остаемся в StateMainMenu или можно перейти в Unauthenticated, если критичная ошибка
			app.state.Current = StateMainMenu
		} else {
			app.state.Current = StateFetchingUserList // Переходим в состояние ожидания списка
		}
	case "3": // Logout
		log.Println("Logging out...")
		// TODO: Отправить C2S_LOGOUT_REQUEST на сервер, если такой протокол будет
		app.state.ClearAuthData() // Сбрасываем данные аутентификации
		app.state.Current = StateUnauthenticated
	case "4": // Exit
		log.Println("Exiting...")
		select {
		case <-app.shutdownChan:
		default:
			close(app.shutdownChan)
		}
	default:
		log.Println("Invalid option.")
	}
}

// handleFetchingUserListState ожидает ответ от сервера со списком пользователей.
func handleFetchingUserListState(app *ClientApp) {
	log.Println("Waiting for user list from server...")
	select {
	case wsMsg, ok := <-app.state.ServerResponses:
		if !ok {
			log.Println("Server connection lost while fetching user list. Returning to main menu.")
			app.state.Current = StateMainMenu
			// Сигнализируем о необходимости завершения, т.к. канал закрыт
			select {
			case <-app.shutdownChan:
			default:
				close(app.shutdownChan)
			}
			return
		}

		if wsMsg.Type == protocol.MsgTypeUserListResponse {
			var respPayload protocol.UserListResponsePayload
			if err := json.Unmarshal(wsMsg.Payload, &respPayload); err != nil {
				log.Printf("Failed to unmarshal UserListResponse payload: %v\n", err)
				app.state.Current = StateMainMenu // Возврат в меню при ошибке парсинга
				return
			}
			app.state.AvailableUsers = respPayload.Users
			log.Printf("Received user list with %d users.\n", len(respPayload.Users))
			app.state.Current = StateUserSelectionForPrivateChat // Переход к выбору пользователя
		} else {
			log.Printf("Received unexpected message type %s while fetching user list. Expected %s.\n", wsMsg.Type, protocol.MsgTypeUserListResponse)
			// Обрабатываем другие сообщения, если они важны, или возвращаемся в меню
			app.processServerResponse(wsMsg)                // Попробуем обработать как стандартный ответ
			if app.state.Current == StateFetchingUserList { // Если состояние не изменилось
				app.state.Current = StateMainMenu
			}
		}
	case <-time.After(15 * time.Second): // Таймаут ожидания
		log.Println("Timeout waiting for user list response from server.")
		app.state.Current = StateMainMenu
	case <-app.shutdownChan:
		log.Println("Shutdown signal received while fetching user list.")
		// Ничего не делаем, главный цикл в client.go завершится
	}
}

// handleUserSelectionForPrivateChatState отображает список пользователей и обрабатывает выбор.
func handleUserSelectionForPrivateChatState(app *ClientApp) {
	displayUserList(app.state.AvailableUsers)
	input, _ := app.reader.ReadString('\n')
	choiceStr := strings.TrimSpace(input)

	choice, err := strconv.Atoi(choiceStr)
	if err != nil || choice < 0 || choice > len(app.state.AvailableUsers) {
		log.Println("Invalid selection. Please enter a number from the list.")
		// Остаемся в том же состоянии для повторного выбора
		// app.state.Current = StateUserSelectionForPrivateChat // это не нужно, мы и так здесь
		return
	}

	if choice == 0 { // 0. Back to Main Menu
		app.state.Current = StateMainMenu
		app.state.AvailableUsers = nil // Очищаем список, чтобы при следующем входе он запросился заново
		return
	}

	selectedUser := app.state.AvailableUsers[choice-1] // -1 потому что нумерация для пользователя с 1
	app.state.CurrentChatTargetUserID = selectedUser.UserID
	app.state.CurrentChatTargetDisplayName = selectedUser.DisplayName
	// Очищаем список пользователей, он больше не нужен для этого чата
	app.state.AvailableUsers = nil

	log.Printf("Attempting to start a private chat with %s (ID: %s)\n", selectedUser.DisplayName, selectedUser.UserID)
	// Пока что мы не отправляем C2S_START_PRIVATE_CHAT_REQUEST,
	// а сразу переходим в состояние чата. Первое сообщение, отправленное в этом состоянии,
	// будет C2S_SEND_PRIVATE_MESSAGE_REQUEST.
	// Это упрощение для текущего шага.
	app.state.Current = StateInPrivateChat
	// Очистим CurrentChatID, т.к. мы еще не знаем его для нового чата.
	// Сервер его сгенерирует и пришлет с первым сообщением в NewPrivateMessageNotifyPayload.
	app.state.CurrentChatID = ""
}

// handleInPrivateChatState - логика для состояния внутри личного чата
func handleInPrivateChatState(app *ClientApp) {
	fmt.Printf("\n--- Chat with %s (type '[[back]]' to return) ---\n", app.state.CurrentChatTargetDisplayName)
	// Здесь нужно будет отображать историю сообщений этого чата. Пока пропустим.

	// Отображение входящих сообщений для этого чата (если они есть в канале)
	// Это нужно делать неблокирующе или в отдельной горутине/секции UI
	// Пока что входящие сообщения логируются в readFromServer
	// Мы можем здесь проверять канал serverResponses неблокирующе
	select {
	case wsMsg, ok := <-app.state.ServerResponses:
		if !ok {
			log.Println("Server connection lost while in private chat.")
			app.state.Current = StateMainMenu
			select {
			case <-app.shutdownChan:
			default:
				close(app.shutdownChan)
			}
			return
		}
		// Обрабатываем ТОЛЬКО NewPrivateMessageNotify, относящиеся к этому чату
		if wsMsg.Type == protocol.MsgTypeNewPrivateMessageNotify {
			var notifyPayload protocol.NewPrivateMessageNotifyPayload
			if err := json.Unmarshal(wsMsg.Payload, &notifyPayload); err == nil {
				// Проверяем, относится ли сообщение к текущему чату.
				// ChatID будет установлен сервером, и он должен быть одинаков для сообщений
				// от нас и к нам в рамках одного диалога.
				// Пока ChatID не установлен, ориентируемся на Sender/Target.
				isMyMessage := notifyPayload.SenderID == app.state.UserID
				isToMe := (notifyPayload.SenderID == app.state.CurrentChatTargetUserID && notifyPayload.ReceiverID == app.state.UserID) ||
					(notifyPayload.SenderID == app.state.UserID && notifyPayload.ReceiverID == app.state.CurrentChatTargetUserID)

				if app.state.CurrentChatID == "" && isToMe { // Если ChatID еще не известен, но это наш диалог
					app.state.CurrentChatID = notifyPayload.ChatID // Сохраняем ChatID
				}

				if app.state.CurrentChatID == notifyPayload.ChatID || (app.state.CurrentChatID == "" && isToMe) {
					senderDisplayName := notifyPayload.SenderName
					if isMyMessage {
						senderDisplayName = "You" // Или ваше DisplayName
					}
					// Очистка строки и вывод сообщения
					fmt.Printf("\r%*s\r", 80, "") // Очистка текущей строки ввода
					fmt.Printf("[%s] (%s): %s\n",
						senderDisplayName,
						time.Unix(notifyPayload.Timestamp, 0).Format("15:04:05"),
						notifyPayload.Text)
				} else {
					// Сообщение для другого чата, пока просто логируем или позже добавим уведомление
					log.Printf("Received private message for another chat (ID: %s) while in chat with %s.\n", notifyPayload.ChatID, app.state.CurrentChatTargetDisplayName)
					// Можно вернуть сообщение в канал, если он буферизован и другие состояния его обработают
					// go func() { app.state.ServerResponses <- wsMsg }() // Осторожно с этим
				}
			}
		} else {
			// Другие типы ответов (например, ошибки) обрабатываются processServerResponse
			// Но если мы уже в чате, то processServerResponse не должен менять состояние чата,
			// если только это не критическая ошибка.
			log.Printf("Received non-private message type %s while in private chat, passing to general handler.\n", wsMsg.Type)
			app.processServerResponse(wsMsg) // Обработаем как общий ответ, если это не текстовое сообщение для чата
		}
	default:
		// Нет новых сообщений от сервера, продолжаем к вводу пользователя
	}

	fmt.Print("> ")
	chatInput, _ := app.reader.ReadString('\n')
	chatInput = strings.TrimSpace(chatInput)

	if chatInput == "[[back]]" {
		app.state.Current = StateMainMenu // Возвращаемся в главное меню
		app.state.CurrentChatTargetUserID = ""
		app.state.CurrentChatTargetDisplayName = ""
		app.state.CurrentChatID = ""
		app.state.AvailableUsers = nil // Очищаем на всякий случай
	} else if chatInput != "" {
		payload := protocol.SendPrivateMessageRequestPayload{
			TargetUserID: app.state.CurrentChatTargetUserID, // Отправляем конкретному пользователю
			Text:         chatInput,
		}
		err := sendMessageToServer(app.conn, protocol.MsgTypeSendPrivateMessageRequest, payload)
		if err != nil {
			log.Println("Failed to send private message, connection may be lost.")
			// Можно попробовать перейти в StateMainMenu или показать ошибку
			// и остаться в текущем состоянии, чтобы пользователь видел проблему.
		}
	}
}

// handleInChatState (для Global Broadcast) - переименуем
func handleGlobalChatState(app *ClientApp) {
	fmt.Println("\n--- In Global Chat (type '[[back]]' to return to menu) ---")
	// Входящие Broadcast сообщения обрабатываются напрямую в readFromServer

	fmt.Print("> ")
	chatInput, _ := app.reader.ReadString('\n')
	chatInput = strings.TrimSpace(chatInput)

	if chatInput == "[[back]]" {
		app.state.Current = StateMainMenu
	} else if chatInput != "" {
		// Отправляем текстовое сообщение как обычный TextMessage,
		// сервер его обернет в BroadcastTextPayload
		payload := protocol.TextPayload{Text: chatInput}
		err := sendMessageToServer(app.conn, protocol.MsgTypeText, payload)
		if err != nil {
			log.Println("Failed to send global message, connection may be lost.")
		}
	}
}
