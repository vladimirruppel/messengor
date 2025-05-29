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

// processBroadcastMessage отображает broadcast сообщение.
// Вызывается из разных обработчиков состояний.
func processBroadcastMessage(payload json.RawMessage) {
	var bPayload protocol.BroadcastTextPayload
	if json.Unmarshal(payload, &bPayload) == nil {
		// Очистка текущей строки ввода (может быть неидеальной)
		// \r - возврат каретки, %*s - строка пробелов для затирания, снова \r
		// Лучше вывести сообщение на новой строке, чтобы не конфликтовать с вводом.
		fmt.Printf("\n[BROADCAST | %s] (%s): %s",
			bPayload.SenderName,
			time.Unix(bPayload.Timestamp, 0).Format("15:04:05"),
			bPayload.Text)
	} else {
		log.Printf("Handler: Failed to unmarshal BroadcastTextPayload: %v", payload)
	}
}

// processErrorNotify отображает сообщение об ошибке от сервера.
func processErrorNotify(payload json.RawMessage) {
	var errPayload protocol.ErrorPayload
	if json.Unmarshal(payload, &errPayload) == nil {
		fmt.Printf("\n!!! SERVER ERROR: [%s] %s !!!\n", errPayload.ErrorCode, errPayload.ErrorMessage)
	} else {
		log.Printf("Handler: Failed to unmarshal ErrorPayload: %v", payload)
	}
}

// consumePendingNonTargetMessages вычитывает из канала сообщения, не являющиеся целевыми для текущего состояния ожидания.
// В основном обрабатывает Broadcast и ошибки.
// Возвращает true, если был получен сигнал к завершению.
func consumePendingNonTargetMessages(app *ClientApp, currentStateName string) (shutdown bool) {
CONSUME_LOOP:
	for {
		select {
		case wsMsg, ok := <-app.state.ServerResponses:
			if !ok {
				log.Printf("%s: ServerResponses channel closed. Signaling shutdown.", currentStateName)
				select {
				case <-app.shutdownChan:
				default:
					close(app.shutdownChan)
				}
				return true // Сигнал к завершению
			}
			switch wsMsg.Type {
			case protocol.MsgTypeBroadcastText:
				processBroadcastMessage(wsMsg.Payload)
			default:
				log.Printf("%s: Received unexpected message type %s while consuming pending. Re-queueing (best effort).", currentStateName, wsMsg.Type)
				// Попытка неблокирующей отправки обратно (может не сработать, если канал полон)
				select {
				case app.state.ServerResponses <- wsMsg:
				default:
					log.Printf("%s: Failed to re-queue message type %s, channel full.", currentStateName, wsMsg.Type)
				}
				break CONSUME_LOOP
			}
		default:
			// Нет больше сообщений в канале для немедленной обработки
			break CONSUME_LOOP
		}
	}
	return false
}

func handleUnauthenticatedState(app *ClientApp) {
	if consumePendingNonTargetMessages(app, "UnauthenticatedState") {
		return
	}
	displayUnauthenticatedMenu()
	fmt.Print("Choose an option: ") // Восстанавливаем приглашение
	input, _ := app.reader.ReadString('\n')
	choice := strings.TrimSpace(input)

	switch choice {
	case "1":
		handleUserInputLogin(app)
	case "2":
		handleUserInputRegister(app)
	case "3":
		log.Println("Exiting...")
		select {
		case <-app.shutdownChan:
		default:
			close(app.shutdownChan)
		}
	default:
		log.Println("Invalid option. Please try again.")
	}
}

func handleUserInputRegister(app *ClientApp) {
	if consumePendingNonTargetMessages(app, "UserInputRegister") {
		return
	} // Показать broadcast перед запросом ввода
	fmt.Print("Enter username: ")
	username, _ := app.reader.ReadString('\n')
	username = strings.TrimSpace(username)
	if username == "" {
		log.Println("Username cannot be empty.")
		return
	}
	fmt.Print("Enter password: ")
	password, _ := app.reader.ReadString('\n')
	password = strings.TrimSpace(password)
	if password == "" {
		log.Println("Password cannot be empty.")
		return
	}
	fmt.Print("Enter display name: ")
	displayName, _ := app.reader.ReadString('\n')
	displayName = strings.TrimSpace(displayName)
	if displayName == "" {
		log.Println("Display name cannot be empty.")
		return
	}
	payload := protocol.RegisterRequestPayload{
		Username:    username,
		Password:    password,
		DisplayName: displayName,
	}
	err := sendMessageToServer(app.conn, protocol.MsgTypeRegisterRequest, payload)
	if err != nil {
		log.Printf("Failed to send registration request: %v. Check connection.", err)
		return
	}
	app.state.Current = StateAuthenticating
}

// handleUserInputLogin запрашивает данные и отправляет LoginRequest
func handleUserInputLogin(app *ClientApp) {
	if consumePendingNonTargetMessages(app, "UserInputLogin") {
		return
	} // Показать broadcast перед запросом ввода
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
		// Остаемся в StateUnauthenticated
	}
}

// processServerResponse обрабатывает ответы от сервера, влияющие на состояние
func (app *ClientApp) processServerResponse(wsMsg protocol.WebSocketMessage) {
	// Эта функция вызывается из handleAuthenticatingState
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
	case protocol.MsgTypeRegisterResponse:
		var respPayload protocol.RegisterResponsePayload
		if err := json.Unmarshal(wsMsg.Payload, &respPayload); err != nil {
			log.Printf("Failed to unmarshal RegisterResponse payload: %v\n", err)
			app.state.Current = StateUnauthenticated
			return
		}
		if respPayload.Success {
			log.Println("Registration successful! Please login.")
			app.state.Current = StateUnauthenticated // Возвращаем в меню для логина
		} else {
			log.Printf("Registration failed: %s\n", respPayload.ErrorMessage)
			app.state.Current = StateUnauthenticated
		}
	default:
		log.Printf("processServerResponse: Received unhandled server response type %s.\n", wsMsg.Type)
	}
}

func handleAuthenticatingState(app *ClientApp) {
	log.Println("Authenticating... Waiting for server response...")
AUTHENTICATING_LOOP:
	for {
		select {
		case wsMsg, ok := <-app.state.ServerResponses:
			if !ok {
				log.Println("AuthenticatingState: ServerResponses channel closed. Connection lost.")
				app.state.Current = StateUnauthenticated
				select {
				case <-app.shutdownChan:
				default:
					close(app.shutdownChan)
				}
				return
			}

			switch wsMsg.Type {
			case protocol.MsgTypeLoginResponse:
				var respPayload protocol.LoginResponsePayload
				if err := json.Unmarshal(wsMsg.Payload, &respPayload); err != nil {
					log.Printf("Failed to unmarshal LoginResponse: %v", err)
					app.state.Current = StateUnauthenticated
					break AUTHENTICATING_LOOP // Выход из цикла ожидания
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
				break AUTHENTICATING_LOOP // Ответ получен, выходим из цикла ожидания
			case protocol.MsgTypeRegisterResponse:
				var respPayload protocol.RegisterResponsePayload
				if err := json.Unmarshal(wsMsg.Payload, &respPayload); err != nil {
					log.Printf("Failed to unmarshal RegisterResponse: %v", err)
					app.state.Current = StateUnauthenticated
					break AUTHENTICATING_LOOP
				}
				if respPayload.Success {
					log.Println("Registration successful! Please login.")
				} else {
					log.Printf("Registration failed: %s\n", respPayload.ErrorMessage)
				}
				app.state.Current = StateUnauthenticated // В любом случае после регистрации возвращаем в меню для логина
				break AUTHENTICATING_LOOP                // Ответ получен
			case protocol.MsgTypeBroadcastText:
				processBroadcastMessage(wsMsg.Payload)
				fmt.Print("Authenticating... Waiting for server response...\n") // Перерисовываем состояние
			default:
				log.Printf("AuthenticatingState: Received unexpected message type %s. Ignoring.", wsMsg.Type)
			}
		case <-time.After(20 * time.Second): // Увеличенный таймаут для аутентификации
			log.Println("Timeout waiting for authentication response.")
			app.state.Current = StateUnauthenticated
			break AUTHENTICATING_LOOP
		case <-app.shutdownChan:
			log.Println("Shutdown signal received while authenticating.")
			app.state.Current = StateUnauthenticated // Гарантируем выход
			return                                   // Выходим из обработчика
		}
	}
}

func handleMainMenuState(app *ClientApp) {
	if consumePendingNonTargetMessages(app, "MainMenuState") {
		return
	}
	displayMainMenu(app.state.DisplayName, app.state.UserID)
	fmt.Print("Choose an option: ")
	input, _ := app.reader.ReadString('\n')
	choice := strings.TrimSpace(input)

	switch choice {
	case "1":
		app.state.Current = StateInChat // Global Chat
	case "2":
		log.Println("Requesting user list from server...")
		payload := protocol.GetUserListRequestPayload{}
		err := sendMessageToServer(app.conn, protocol.MsgTypeGetUserListRequest, payload)
		if err != nil {
			log.Printf("Failed to send GetUserListRequest: %v.", err)
		} else {
			app.state.Current = StateFetchingUserList
		}
	case "3":
		log.Println("Logging out...")
		app.state.ClearAuthData()
		app.state.Current = StateUnauthenticated
	case "4":
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
	log.Println("Fetching user list... Waiting for server response...")
FETCHING_USERS_LOOP:
	for {
		select {
		case wsMsg, ok := <-app.state.ServerResponses:
			if !ok {
				log.Println("FetchingUserListState: ServerResponses channel closed. Connection lost.")
				app.state.Current = StateMainMenu
				select {
				case <-app.shutdownChan:
				default:
					close(app.shutdownChan)
				}
				return
			}
			switch wsMsg.Type {
			case protocol.MsgTypeUserListResponse:
				var respPayload protocol.UserListResponsePayload
				if err := json.Unmarshal(wsMsg.Payload, &respPayload); err != nil {
					log.Printf("Failed to unmarshal UserListResponse: %v\n", err)
					app.state.Current = StateMainMenu
				} else {
					app.state.AvailableUsers = respPayload.Users
					log.Printf("Received user list with %d users.\n", len(respPayload.Users))
					app.state.Current = StateUserSelectionForPrivateChat
				}
				break FETCHING_USERS_LOOP // Целевой ответ получен
			case protocol.MsgTypeBroadcastText:
				processBroadcastMessage(wsMsg.Payload)
				fmt.Print("Fetching user list... Waiting for server response...\n")
			// case protocol.MsgTypeErrorNotify:
			//  processErrorNotify(wsMsg.Payload)
			//  fmt.Print("Fetching user list... Waiting for server response...\n")
			default:
				log.Printf("FetchingUserListState: Received unexpected message type %s. Ignoring.", wsMsg.Type)
			}
		case <-time.After(15 * time.Second):
			log.Println("Timeout waiting for user list response.")
			app.state.Current = StateMainMenu
			break FETCHING_USERS_LOOP
		case <-app.shutdownChan:
			log.Println("Shutdown signal received while fetching user list.")
			app.state.Current = StateMainMenu
			return
		}
	}
}

// handleUserSelectionForPrivateChatState отображает список пользователей и обрабатывает выбор.
func handleUserSelectionForPrivateChatState(app *ClientApp) {
	if consumePendingNonTargetMessages(app, "UserSelectionState") {
		return
	}
	displayUserList(app.state.AvailableUsers) // Меню и приглашение к вводу здесь
	// fmt.Print("Enter user number: ") // displayUserList уже это делает

	input, _ := app.reader.ReadString('\n')
	choiceStr := strings.TrimSpace(input)

	choice, err := strconv.Atoi(choiceStr)
	if err != nil || choice < 0 || choice > len(app.state.AvailableUsers) {
		log.Println("Invalid selection. Please enter a number from the list.")
		return // Остаемся в этом состоянии, главный цикл вызовет снова
	}

	if choice == 0 {
		app.state.Current = StateMainMenu
		app.state.AvailableUsers = nil
		return
	}

	selectedUser := app.state.AvailableUsers[choice-1]
	app.state.CurrentChatTargetUserID = selectedUser.UserID
	app.state.CurrentChatTargetDisplayName = selectedUser.DisplayName
	app.state.AvailableUsers = nil // Очищаем, больше не нужно

	log.Printf("Starting private chat with %s (ID: %s)\n", selectedUser.DisplayName, selectedUser.UserID)
	// Генерируем ChatID на клиенте для немедленного использования, сервер его подтвердит/создаст такой же
	// Это нужно, чтобы клиент мог фильтровать сообщения для этого чата сразу.
	// Сервер при отправке NewPrivateMessageNotify должен использовать тот же алгоритм генерации ChatID.
	var chatIDErr error
	app.state.CurrentChatID, chatIDErr = GenerateClientPrivateChatID(app.state.UserID, selectedUser.UserID)
	if chatIDErr != nil {
		log.Printf("Error generating client-side ChatID: %v. Proceeding without it.", chatIDErr)
		app.state.CurrentChatID = "" // Оставляем пустым, если ошибка
	}
	app.state.Current = StateInPrivateChat
}

// handleInPrivateChatState - логика для состояния внутри личного чата
func handleInPrivateChatState(app *ClientApp) {
PROCESS_PENDING_PRIVATE_CHAT:
	for {
		select {
		case wsMsg, ok := <-app.state.ServerResponses:
			if !ok {
				log.Println("InPrivateChat: ServerResponses channel closed. Signaling shutdown.")
				app.state.Current = StateMainMenu
				select {
				case <-app.shutdownChan:
				default:
					close(app.shutdownChan)
				}
				return
			}
			// Обрабатываем Broadcast или личные сообщения для ЭТОГО чата
			if wsMsg.Type == protocol.MsgTypeBroadcastText {
				processBroadcastMessage(wsMsg.Payload)
			} else if wsMsg.Type == protocol.MsgTypeNewPrivateMessageNotify {
				var notifyPayload protocol.NewPrivateMessageNotifyPayload
				if err := json.Unmarshal(wsMsg.Payload, &notifyPayload); err == nil {
					// Проверяем, относится ли сообщение к текущему чату
					// Клиент теперь сам генерирует предполагаемый ChatID при входе в чат.
					// Сервер должен использовать тот же алгоритм для генерации ChatID в уведомлении.
					if app.state.CurrentChatID == notifyPayload.ChatID {
						isMyMessage := notifyPayload.SenderID == app.state.UserID
						senderDisplayName := notifyPayload.SenderName
						if isMyMessage {
							senderDisplayName = "You"
						}
						fmt.Printf("\n[%s] (%s): %s\n", // \n в начале
							senderDisplayName,
							time.Unix(notifyPayload.Timestamp, 0).Format("15:04:05"),
							notifyPayload.Text)
					} else {
						// Сообщение для другого чата, пока просто логируем
						log.Printf(">>> PM for another chat from %s (Expected ChatID: %s, Got: %s) <<<",
							notifyPayload.SenderName, app.state.CurrentChatID, notifyPayload.ChatID)
						// TODO: Увеличить счетчик непрочитанных для notifyPayload.ChatID
					}
				} else {
					log.Printf("InPrivateChat: Failed to unmarshal NewPrivateMessageNotifyPayload: %v", err)
				}
			} else {
				log.Printf("InPrivateChatState: Received unhandled message type %s. Ignoring.", wsMsg.Type)
			}
		default:
			break PROCESS_PENDING_PRIVATE_CHAT
		}
	}

	fmt.Printf("\n--- Chat with %s (ID: %s) (type '[[back]]' to return) ---\n",
		app.state.CurrentChatTargetDisplayName, app.state.CurrentChatID)
	fmt.Print("> ")
	chatInput, _ := app.reader.ReadString('\n')
	chatInput = strings.TrimSpace(chatInput)

	if chatInput == "[[back]]" {
		app.state.Current = StateMainMenu
		// app.state.ClearAuthData() // Не нужно полностью сбрасывать, если мы просто выходим из чата в меню
		app.state.CurrentChatID = ""
		app.state.CurrentChatTargetUserID = ""
		app.state.CurrentChatTargetDisplayName = ""
	} else if chatInput == "[[update]]" {
		// Ничего не делаем
	} else if chatInput != "" {
		payload := protocol.SendPrivateMessageRequestPayload{
			TargetUserID: app.state.CurrentChatTargetUserID,
			Text:         chatInput,
			// В будущем, если CurrentChatID уже известен и сервер его поддерживает,
			// можно отправлять ChatID вместо/в дополнение к TargetUserID.
			// ChatID: app.state.CurrentChatID,
		}
		err := sendMessageToServer(app.conn, protocol.MsgTypeSendPrivateMessageRequest, payload)
		if err != nil {
			log.Println("Failed to send private message.")
		}
	}
}

// handleInChatState (для Global Broadcast) - переименуем
func handleGlobalChatState(app *ClientApp) {
	// Сначала обработаем все ожидающие сообщения из канала
PROCESS_PENDING_GLOBAL_CHAT:
	for {
		select {
		case wsMsg, ok := <-app.state.ServerResponses:
			if !ok {
				log.Println("GlobalChat: ServerResponses channel closed. Signaling shutdown.")
				app.state.Current = StateMainMenu
				select {
				case <-app.shutdownChan:
				default:
					close(app.shutdownChan)
				}
				return
			}
			if wsMsg.Type == protocol.MsgTypeBroadcastText {
				processBroadcastMessage(wsMsg.Payload)
			} else {
				// В глобальном чате мы не ожидаем других типов сообщений, влияющих на состояние,
				// кроме, возможно, ошибок от сервера или системных уведомлений.
				log.Printf("GlobalChatState: Received non-broadcast message type %s. Ignoring.", wsMsg.Type)
			}
		default:
			break PROCESS_PENDING_GLOBAL_CHAT
		}
	}

	fmt.Println("\n--- In Global Chat (type '[[back]]' to return to menu) ---")
	fmt.Print("> ")
	chatInput, _ := app.reader.ReadString('\n')
	chatInput = strings.TrimSpace(chatInput)

	if chatInput == "[[back]]" {
		app.state.Current = StateMainMenu
	} else if chatInput == "[[update]]" {
		// Ничего не делаем, этот State снова будет
	} else if chatInput != "" {
		payload := protocol.TextPayload{Text: chatInput}
		err := sendMessageToServer(app.conn, protocol.MsgTypeText, payload)
		if err != nil {
			log.Println("Failed to send global message.")
			// Можно добавить обработку ошибки, например, возврат в меню
		}
	}
}
