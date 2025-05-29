package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	"github.com/vladimirruppel/messengor/internal/protocol" // Путь к вашему пакету protocol
)

var (
	addr         = flag.String("addr", "localhost:8088", "http service address")
	conn         *websocket.Conn
	mu           sync.Mutex // Для защиты conn и других общих ресурсов, если понадобится
	loggedInUser struct {
		ID          string
		DisplayName string
		Token       string
	}
	isAuthenticated = false
	currentChatID   = "global_broadcast"                 // По умолчанию - глобальный чат
	knownUsers      = make(map[string]protocol.UserInfo) // Карта UserID -> UserInfo
	inputPrompt     = "> "
)

// generatePrivateChatIDClient - клиентская версия генерации ID приватного чата
// Должна быть идентична серверной для консистентности.
func generatePrivateChatIDClient(userID1, userID2 string) (string, error) {
	if userID1 == "" || userID2 == "" {
		return "", fmt.Errorf("user IDs cannot be empty for generating chat ID")
	}
	if userID1 == userID2 {
		return "", fmt.Errorf("cannot create a private chat with oneself using this method")
	}
	ids := []string{userID1, userID2}
	sort.Strings(ids)
	return fmt.Sprintf("private:%s:%s", ids[0], ids[1]), nil
}

func updatePrompt() {
	if !isAuthenticated {
		inputPrompt = "> "
		return
	}
	chatDisplayName := currentChatID
	if strings.HasPrefix(currentChatID, "private:") {
		parts := strings.Split(currentChatID, ":")
		if len(parts) == 3 {
			otherUserID := ""
			if parts[1] == loggedInUser.ID {
				otherUserID = parts[2]
			} else {
				otherUserID = parts[1]
			}
			if user, ok := knownUsers[otherUserID]; ok {
				chatDisplayName = fmt.Sprintf("PM with %s", user.DisplayName)
			} else {
				chatDisplayName = fmt.Sprintf("PM with %s", otherUserID)
			}
		}
	} else if currentChatID == "global_broadcast" {
		chatDisplayName = "Global Chat"
	}
	inputPrompt = fmt.Sprintf("[%s] %s: ", chatDisplayName, loggedInUser.DisplayName)
}

// sendRequest отправляет сообщение на WebSocket сервер
func sendRequest(msgType string, payload interface{}) error {
	mu.Lock()
	defer mu.Unlock()
	if conn == nil {
		return fmt.Errorf("not connected")
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	wsMsg := protocol.WebSocketMessage{
		Type:    msgType,
		Payload: json.RawMessage(payloadBytes),
	}

	msgBytes, err := json.Marshal(wsMsg)
	if err != nil {
		return fmt.Errorf("failed to marshal WebSocketMessage: %w", err)
	}
	// log.Printf("CLIENT: Sending message: Type=%s, Payload=%s\n", wsMsg.Type, string(wsMsg.Payload))
	return conn.WriteMessage(websocket.TextMessage, msgBytes)
}

// listenToServer читает сообщения от сервера и обрабатывает их
func listenToServer() {
	defer func() {
		if conn != nil {
			conn.Close()
		}
		log.Println("Disconnected from server. Listener stopped.")
	}()

	for {
		if conn == nil {
			time.Sleep(1 * time.Second) // Если соединение потеряно, ждем
			continue
		}
		_, messageBytes, err := conn.ReadMessage()
		if err != nil {
			log.Printf("Read error: %v. Attempting to reconnect or exiting...", err)
			// Здесь можно добавить логику переподключения или просто выход
			isAuthenticated = false // Сбрасываем аутентификацию при потере соединения
			// Попытка переподключения (простая)
			for {
				log.Println("Attempting to reconnect...")
				if e := connectToServer(); e == nil {
					log.Println("Reconnected. Please log in again.")
					// Нужно будет снова пройти аутентификацию
					// Для простоты, сейчас просто выведем сообщение
					// В более сложном клиенте, можно автоматически пытаться логиниться по токену
					// или переводить в состояние ожидания команды /login
					fmt.Print(inputPrompt) // Показать промпт снова
					break                  // выходим из цикла переподключения
				}
				time.Sleep(5 * time.Second)
			}
			continue // Продолжаем слушать на новом соединении
		}

		var wsMsg protocol.WebSocketMessage
		if err := json.Unmarshal(messageBytes, &wsMsg); err != nil {
			log.Printf("Failed to unmarshal WebSocketMessage: %v. Raw: %s", err, string(messageBytes))
			continue
		}
		// log.Printf("CLIENT: Received message: Type=%s, Payload=%s\n", wsMsg.Type, string(wsMsg.Payload))
		clearLineAndPrint := func(a ...interface{}) {
			// Простой способ "очистить" текущую строку ввода перед печатью сообщения от сервера
			// В реальном TUI это делается культурнее
			fmt.Print("\r" + strings.Repeat(" ", len(inputPrompt)+50) + "\r") // Стираем побольше
			fmt.Println(a...)
			fmt.Print(inputPrompt) // Печатаем промпт снова
		}
		clearLineAndPrintf := func(format string, a ...interface{}) {
			fmt.Print("\r" + strings.Repeat(" ", len(inputPrompt)+50) + "\r")
			fmt.Printf(format, a...)
			fmt.Print(inputPrompt)
		}

		switch wsMsg.Type {
		case protocol.MsgTypeRegisterResponse:
			var resp protocol.RegisterResponsePayload
			if err := json.Unmarshal(wsMsg.Payload, &resp); err != nil {
				clearLineAndPrintf("CLIENT: Error unmarshalling RegisterResponse: %v\n", err)
				continue
			}
			if resp.Success {
				clearLineAndPrintf("CLIENT: Registration successful! UserID: %s. Please log in.\n", resp.UserID)
			} else {
				clearLineAndPrintf("CLIENT: Registration failed: %s\n", resp.ErrorMessage)
			}

		case protocol.MsgTypeLoginResponse:
			var resp protocol.LoginResponsePayload
			if err := json.Unmarshal(wsMsg.Payload, &resp); err != nil {
				clearLineAndPrintf("CLIENT: Error unmarshalling LoginResponse: %v\n", err)
				continue
			}
			if resp.Success {
				loggedInUser.ID = resp.UserID
				loggedInUser.DisplayName = resp.DisplayName
				loggedInUser.Token = resp.SessionToken
				isAuthenticated = true
				clearLineAndPrintf("CLIENT: Login successful! Welcome, %s (ID: %s)\n", resp.DisplayName, resp.UserID)
				updatePrompt()
				// Запросим список пользователей после успешного логина
				if err := sendRequest(protocol.MsgTypeGetUserListRequest, protocol.GetUserListRequestPayload{}); err != nil {
					log.Printf("Error requesting user list after login: %v", err)
				}
				// Запросим историю текущего (глобального) чата
				if err := sendRequest(protocol.MsgTypeGetChatHistoryRequest, protocol.GetChatHistoryRequestPayload{ChatID: currentChatID, Limit: 20}); err != nil {
					log.Printf("Error requesting initial chat history: %v", err)
				}

			} else {
				clearLineAndPrintf("CLIENT: Login failed: %s\n", resp.ErrorMessage)
				isAuthenticated = false
			}

		case protocol.MsgTypeBroadcastText:
			var bcastMsg protocol.BroadcastTextPayload
			if err := json.Unmarshal(wsMsg.Payload, &bcastMsg); err != nil {
				clearLineAndPrintf("CLIENT: Error unmarshalling BroadcastText: %v\n", err)
				continue
			}
			// Обновляем knownUsers, если отправитель неизвестен
			if _, ok := knownUsers[bcastMsg.SenderID]; !ok && bcastMsg.SenderID != "" {
				knownUsers[bcastMsg.SenderID] = protocol.UserInfo{UserID: bcastMsg.SenderID, DisplayName: bcastMsg.SenderName, IsOnline: true}
			}

			timestamp := time.Unix(bcastMsg.Timestamp, 0).Format("15:04:05")
			clearLineAndPrintf("[%s Global] %s (%s): %s\n", timestamp, bcastMsg.SenderName, bcastMsg.SenderID, bcastMsg.Text)

		case protocol.MsgTypeNewPrivateMessageNotify:
			var pm protocol.NewPrivateMessageNotifyPayload
			if err := json.Unmarshal(wsMsg.Payload, &pm); err != nil {
				clearLineAndPrintf("CLIENT: Error unmarshalling NewPrivateMessageNotify: %v\n", err)
				continue
			}
			// Обновляем knownUsers
			if _, ok := knownUsers[pm.SenderID]; !ok && pm.SenderID != "" {
				knownUsers[pm.SenderID] = protocol.UserInfo{UserID: pm.SenderID, DisplayName: pm.SenderName, IsOnline: true}
			}
			if _, ok := knownUsers[pm.ReceiverID]; !ok && pm.ReceiverID != "" {
				// Получателя может не быть в списке, если это мы, но на всякий случай
				// Если сервер присылает имя получателя, можно было бы его тоже сохранить
				// Но в текущем payload его нет, только ID
				// knownUsers[pm.ReceiverID] = protocol.UserInfo{UserID: pm.ReceiverID, DisplayName: "User_"+pm.ReceiverID[:4], IsOnline: true}
			}

			timestamp := time.Unix(pm.Timestamp, 0).Format("15:04:05")
			direction := "To"
			interlocutorName := pm.ReceiverID // По умолчанию ID
			if otherUser, ok := knownUsers[pm.ReceiverID]; ok {
				interlocutorName = otherUser.DisplayName
			}

			if pm.SenderID != loggedInUser.ID { // Сообщение пришло нам
				direction = "From"
				interlocutorName = pm.SenderName
			} else { // Это "эхо" нашего отправленного сообщения
				// interlocutorName уже должен быть именем получателя
			}

			// Если текущий чат не совпадает с чатом сообщения, уведомить и не менять активный чат
			// Иначе, просто показать сообщение
			if pm.ChatID == currentChatID {
				clearLineAndPrintf("[%s PM %s %s (%s)] %s\n", timestamp, direction, interlocutorName, pm.SenderID, pm.Text)
			} else {
				clearLineAndPrintf("[%s PM %s %s (%s) in chat %s] %s\n", timestamp, direction, interlocutorName, pm.SenderID, pm.ChatID, pm.Text)
				clearLineAndPrint("(To switch: /chat <user_id_or_name> or /chatid <chat_id>)")
			}

		case protocol.MsgTypeUserListResponse:
			var resp protocol.UserListResponsePayload
			if err := json.Unmarshal(wsMsg.Payload, &resp); err != nil {
				clearLineAndPrintf("CLIENT: Error unmarshalling UserListResponse: %v\n", err)
				continue
			}
			clearLineAndPrint("CLIENT: Online Users:")
			// Очистим старых известных пользователей, чтобы isOnline был актуален
			// Или лучше обновлять? Пока просто перезапишем тех, кто пришел
			tempKnownUsers := make(map[string]protocol.UserInfo)
			for _, u := range resp.Users {
				clearLineAndPrintf(" - %s (ID: %s, Online: %v)\n", u.DisplayName, u.UserID, u.IsOnline)
				tempKnownUsers[u.UserID] = u
			}
			// Добавим себя, если нас нет (сервер может не присылать себя)
			if loggedInUser.ID != "" {
				if _, ok := tempKnownUsers[loggedInUser.ID]; !ok {
					tempKnownUsers[loggedInUser.ID] = protocol.UserInfo{UserID: loggedInUser.ID, DisplayName: loggedInUser.DisplayName, IsOnline: true}
				}
			}
			knownUsers = tempKnownUsers

		case protocol.MsgTypeChatHistoryResponse:
			var resp protocol.ChatHistoryResponsePayload
			if err := json.Unmarshal(wsMsg.Payload, &resp); err != nil {
				clearLineAndPrintf("CLIENT: Error unmarshalling ChatHistoryResponse: %v\n", err)
				continue
			}
			clearLineAndPrintf("CLIENT: Chat History for %s (Last %d messages):\n", resp.ChatID, len(resp.Messages))
			for _, msg := range resp.Messages {
				timestamp := time.Unix(msg.Timestamp, 0).Format("02.01.06 15:04:05")
				senderDisplayName := msg.SenderName
				if sender, ok := knownUsers[msg.SenderID]; ok {
					senderDisplayName = sender.DisplayName
				}
				clearLineAndPrintf("  [%s] %s: %s\n", timestamp, senderDisplayName, msg.Text)
			}
			if len(resp.Messages) == 0 {
				clearLineAndPrint("  (No messages in this chat yet)")
			}

		case protocol.MsgTypeErrorNotify:
			var errMsg protocol.ErrorPayload
			if err := json.Unmarshal(wsMsg.Payload, &errMsg); err != nil {
				clearLineAndPrintf("CLIENT: Error unmarshalling ErrorNotify: %v\n", err)
				continue
			}
			clearLineAndPrintf("CLIENT: Server Error [%s]: %s\n", errMsg.ErrorCode, errMsg.ErrorMessage)

		default:
			clearLineAndPrintf("CLIENT: Received unknown message type: %s\n", wsMsg.Type)
		}
	}
}

func connectToServer() error {
	u := "ws://" + *addr + "/ws"
	log.Printf("Connecting to %s", u)
	c, _, err := websocket.DefaultDialer.Dial(u, nil)
	if err != nil {
		log.Printf("Failed to connect to %s: %v", u, err)
		return err
	}
	conn = c
	log.Println("Connected to server.")
	return nil
}

func handleUserInput() {
	reader := bufio.NewReader(os.Stdin)
	fmt.Println("Console Client Started. Type /help for commands.")

	for {
		updatePrompt()
		fmt.Print(inputPrompt)
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)

		if input == "" {
			continue
		}

		parts := strings.Fields(input)
		command := parts[0]

		if !isAuthenticated {
			switch command {
			case "/register":
				if len(parts) != 4 {
					fmt.Println("Usage: /register <username> <password> <display_name>")
					continue
				}
				req := protocol.RegisterRequestPayload{Username: parts[1], Password: parts[2], DisplayName: parts[3]}
				if err := sendRequest(protocol.MsgTypeRegisterRequest, req); err != nil {
					log.Printf("Error sending register request: %v", err)
				}
			case "/login":
				if len(parts) != 3 {
					fmt.Println("Usage: /login <username> <password>")
					continue
				}
				req := protocol.LoginRequestPayload{Username: parts[1], Password: parts[2]}
				if err := sendRequest(protocol.MsgTypeLoginRequest, req); err != nil {
					log.Printf("Error sending login request: %v", err)
				}
			case "/exit":
				fmt.Println("Exiting...")
				if conn != nil {
					conn.Close()
				}
				os.Exit(0)
			case "/help":
				fmt.Println("Available commands (when not logged in):")
				fmt.Println("  /register <username> <password> <display_name>")
				fmt.Println("  /login <username> <password>")
				fmt.Println("  /exit")
				fmt.Println("  /help")
			default:
				fmt.Println("You are not logged in. Please /login or /register.")
			}
			continue // Ждем следующую команду, пока не залогинимся
		}

		// Команды для аутентифицированных пользователей
		switch command {
		case "/users":
			req := protocol.GetUserListRequestPayload{} // Пустой payload
			if err := sendRequest(protocol.MsgTypeGetUserListRequest, req); err != nil {
				log.Printf("Error requesting user list: %v", err)
			}
		case "/pm":
			if len(parts) < 3 {
				fmt.Println("Usage: /pm <target_user_id_or_display_name> <message>")
				continue
			}
			targetIdentifier := parts[1]
			text := strings.Join(parts[2:], " ")

			var targetUserID string
			// Пытаемся найти пользователя по DisplayName, затем по UserID
			found := false
			for _, u := range knownUsers {
				if u.DisplayName == targetIdentifier || u.UserID == targetIdentifier {
					targetUserID = u.UserID
					found = true
					break
				}
			}
			if !found {
				// Если не нашли, считаем, что это UserID (может, пользователь еще не в knownUsers)
				fmt.Printf("Warning: User '%s' not in known users list. Assuming it's a UserID.\n", targetIdentifier)
				targetUserID = targetIdentifier
			}

			req := protocol.SendPrivateMessageRequestPayload{
				TargetUserID: targetUserID,
				Text:         text,
			}
			if err := sendRequest(protocol.MsgTypeSendPrivateMessageRequest, req); err != nil {
				log.Printf("Error sending private message: %v", err)
			}
		case "/history":
			var chatIDForHistory string
			limit := 20 // Default limit
			if len(parts) > 1 {
				chatIDForHistory = parts[1]
				// Проверим, является ли аргумент именем пользователя, чтобы получить историю с ним
				foundUser := false
				for _, u := range knownUsers {
					if u.DisplayName == chatIDForHistory || u.UserID == chatIDForHistory {
						var genErr error
						chatIDForHistory, genErr = generatePrivateChatIDClient(loggedInUser.ID, u.UserID)
						if genErr != nil {
							fmt.Printf("Error generating chat ID for history with %s: %v\n", u.DisplayName, genErr)
							continue
						}
						foundUser = true
						break
					}
				}
				if !foundUser && !strings.Contains(chatIDForHistory, ":") && chatIDForHistory != "global_broadcast" {
					fmt.Printf("Cannot determine chat ID for history: '%s'. Use user ID, display name, 'global_broadcast', or a full private chat ID (private:X:Y).\n", chatIDForHistory)
					continue
				}
			} else {
				chatIDForHistory = currentChatID
			}
			req := protocol.GetChatHistoryRequestPayload{
				ChatID: chatIDForHistory,
				Limit:  limit,
			}
			if err := sendRequest(protocol.MsgTypeGetChatHistoryRequest, req); err != nil {
				log.Printf("Error requesting chat history for %s: %v", chatIDForHistory, err)
			}
		case "/chat": // Переключиться на чат с пользователем
			if len(parts) != 2 {
				fmt.Println("Usage: /chat <user_id_or_display_name>")
				continue
			}
			targetIdentifier := parts[1]
			var targetUserID string
			found := false
			for _, u := range knownUsers {
				if u.DisplayName == targetIdentifier || u.UserID == targetIdentifier {
					targetUserID = u.UserID
					found = true
					break
				}
			}
			if !found {
				fmt.Printf("User '%s' not found in known users list. Cannot switch chat.\n", targetIdentifier)
				fmt.Println("Known users:")
				for _, ku := range knownUsers {
					fmt.Printf(" - %s (%s)\n", ku.DisplayName, ku.UserID)
				}
				continue
			}
			if targetUserID == loggedInUser.ID {
				fmt.Println("Cannot start a private chat with yourself this way.")
				continue
			}

			newChatID, err := generatePrivateChatIDClient(loggedInUser.ID, targetUserID)
			if err != nil {
				fmt.Printf("Error creating private chat ID: %v\n", err)
				continue
			}
			currentChatID = newChatID
			updatePrompt()
			fmt.Printf("Switched to private chat with %s (Chat ID: %s).\n", targetIdentifier, currentChatID)
			// Запросим историю для нового чата
			reqHistory := protocol.GetChatHistoryRequestPayload{ChatID: currentChatID, Limit: 20}
			if err := sendRequest(protocol.MsgTypeGetChatHistoryRequest, reqHistory); err != nil {
				log.Printf("Error requesting chat history for new chat %s: %v", currentChatID, err)
			}

		case "/chatid": // Переключиться на чат по его ID (для отладки или если ID известен)
			if len(parts) != 2 {
				fmt.Println("Usage: /chatid <chat_id (e.g., global_broadcast or private:X:Y)>")
				continue
			}
			newChatID := parts[1]
			if newChatID != "global_broadcast" && !strings.HasPrefix(newChatID, "private:") {
				fmt.Println("Invalid chat ID format. Must be 'global_broadcast' or start with 'private:'.")
				continue
			}
			currentChatID = newChatID
			updatePrompt()
			fmt.Printf("Switched to chat ID: %s.\n", currentChatID)
			reqHistory := protocol.GetChatHistoryRequestPayload{ChatID: currentChatID, Limit: 20}
			if err := sendRequest(protocol.MsgTypeGetChatHistoryRequest, reqHistory); err != nil {
				log.Printf("Error requesting chat history for new chat %s: %v", currentChatID, err)
			}

		case "/global": // Переключиться на глобальный чат
			currentChatID = "global_broadcast"
			updatePrompt()
			fmt.Println("Switched to Global Chat.")
			// Запросим историю для глобального чата
			reqHistory := protocol.GetChatHistoryRequestPayload{ChatID: currentChatID, Limit: 20}
			if err := sendRequest(protocol.MsgTypeGetChatHistoryRequest, reqHistory); err != nil {
				log.Printf("Error requesting chat history for global chat: %v", err)
			}

		case "/exit":
			fmt.Println("Exiting...")
			if conn != nil {
				// Попытка отправить корректное сообщение о закрытии
				// Это опционально, сервер и так обнаружит разрыв
				err := conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
				if err != nil {
					log.Println("Write close error:", err)
				}
				conn.Close()
			}
			os.Exit(0)
		case "/help":
			fmt.Println("Available commands (when logged in):")
			fmt.Println("  <message text>             - Send to current chat (global or private)")
			fmt.Println("  /pm <user_id_or_name> <msg> - Send private message directly")
			fmt.Println("  /users                     - List online users")
			fmt.Println("  /history [chat_id|user_name] - Show history for current/specified chat (last 20)")
			fmt.Println("  /chat <user_id_or_name>    - Switch to private chat with user")
			fmt.Println("  /chatid <full_chat_id>     - Switch to chat by its full ID")
			fmt.Println("  /global                    - Switch to global chat")
			fmt.Println("  /exit                      - Exit the client")
			fmt.Println("  /help                      - Show this help message")

		default: // Считаем, что это текст сообщения для текущего чата
			text := input
			if currentChatID == "global_broadcast" {
				req := protocol.TextPayload{Text: text}
				if err := sendRequest(protocol.MsgTypeText, req); err != nil { // MsgTypeText для broadcast
					log.Printf("Error sending broadcast message: %v", err)
				}
			} else if strings.HasPrefix(currentChatID, "private:") {
				parts := strings.Split(currentChatID, ":")
				if len(parts) != 3 {
					fmt.Println("Error: Current private chat ID is invalid:", currentChatID)
					continue
				}
				targetUserID := ""
				if parts[1] == loggedInUser.ID {
					targetUserID = parts[2]
				} else {
					targetUserID = parts[1]
				}
				req := protocol.SendPrivateMessageRequestPayload{
					TargetUserID: targetUserID,
					Text:         text,
				}
				if err := sendRequest(protocol.MsgTypeSendPrivateMessageRequest, req); err != nil {
					log.Printf("Error sending private message to current chat: %v", err)
				}
			} else {
				fmt.Println("Unknown current chat ID type:", currentChatID, " - Cannot send message.")
			}
		}
	}
}

func main() {
	flag.Parse()
	log.SetFlags(0) // Можно убрать временные метки из логов клиента для чистоты

	// Обработка Ctrl+C
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt, syscall.SIGTERM)

	if err := connectToServer(); err != nil {
		// Если не удалось подключиться при старте, выходим
		log.Fatalf("Initial connection failed: %v. Exiting.", err)
	}

	go listenToServer() // Запускаем слушателя сообщений от сервера в отдельной горутине

	// Горутина для обработки Ctrl+C
	go func() {
		<-interrupt
		fmt.Println("\nInterrupt received, shutting down...")
		if conn != nil {
			// Попытка отправить корректное сообщение о закрытии
			err := conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			if err != nil && !strings.Contains(err.Error(), "use of closed network connection") { // Игнорируем ошибку если соединение уже закрыто
				log.Println("Write close error:", err)
			}
			conn.Close()
		}
		os.Exit(0)
	}()

	handleUserInput() // Основной поток обрабатывает ввод пользователя
}
