package server

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/vladimirruppel/messengor/internal/protocol"
)

const historyDir = "./chat_history" // Директория для хранения файлов истории

var historyFileMutexes = make(map[string]*sync.Mutex) // Мьютексы для каждого файла чата
var globalHistoryMutex = &sync.Mutex{}                // Для доступа к map historyFileMutexes

// initStore проверяет и создает директорию для истории, если ее нет.
func initHistoryStore() {
	if err := os.MkdirAll(historyDir, 0755); err != nil {
		log.Fatalf("Failed to create history directory %s: %v", historyDir, err)
	}
	log.Printf("Chat history will be stored in: %s", historyDir)
}

// getChatFilePath возвращает путь к файлу истории для данного ChatID.
func getChatFilePath(chatID string) string {
	return filepath.Join(historyDir, fmt.Sprintf("%s.jsonl", chatID))
}

// getFileMutex возвращает мьютекс для данного файла чата, создавая его при необходимости.
func getFileMutex(chatID string) *sync.Mutex {
	globalHistoryMutex.Lock()
	defer globalHistoryMutex.Unlock()

	m, exists := historyFileMutexes[chatID]
	if !exists {
		m = &sync.Mutex{}
		historyFileMutexes[chatID] = m
	}
	return m
}

// SaveMessage сохраняет сообщение в файл истории для указанного ChatID.
func SaveMessage(chatID string, senderID string, senderName string, text string) (*protocol.StoredMessage, error) {
	if chatID == "" { // Добавим проверку
		log.Println("SaveMessage: Attempted to save message with empty ChatID")
		return nil, fmt.Errorf("chatID cannot be empty")
	}

	fileMutex := getFileMutex(chatID)
	fileMutex.Lock()
	defer fileMutex.Unlock()

	filePath := getChatFilePath(chatID)
	file, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("Error opening history file %s for chat %s: %v", filePath, chatID, err)
		return nil, err
	}
	defer file.Close()

	storedMsg := &protocol.StoredMessage{
		ChatID:     chatID,           // Сохраняем для возможной проверки
		MessageID:  uuid.NewString(), // Генерируем новый ID для каждого сообщения
		SenderID:   senderID,
		SenderName: senderName,
		Text:       text,
		Timestamp:  time.Now().Unix(),
	}

	messageBytes, err := json.Marshal(storedMsg)
	if err != nil {
		log.Printf("Error marshalling stored message for chat %s: %v", chatID, err)
		return nil, err
	}

	if _, err := file.Write(append(messageBytes, '\n')); err != nil {
		log.Printf("Error writing to history file for chat %s: %v", chatID, err)
		return nil, err
	}
	// log.Printf("Message saved to chat %s: (ID: %s) %s: %s", chatID, storedMsg.MessageID, senderName, text)
	return storedMsg, nil
}

func LoadChatHistory(chatID string, limit int) ([]protocol.StoredMessage, error) {
	if chatID == "" {
		log.Println("LoadChatHistory: Attempted to load history with empty ChatID")
		return nil, fmt.Errorf("chatID cannot be empty")
	}

	fileMutex := getFileMutex(chatID) 
	fileMutex.Lock()
	defer fileMutex.Unlock()

	filePath := getChatFilePath(chatID)
	file, err := os.Open(filePath) // Открываем только на чтение
	if err != nil {
		if os.IsNotExist(err) {
			log.Printf("History file for chat %s does not exist yet.", chatID)
			return []protocol.StoredMessage{}, nil // Нет истории - это не ошибка
		}
		log.Printf("Error opening history file %s for chat %s: %v", filePath, chatID, err)
		return nil, err
	}
	defer file.Close()

	var messages []protocol.StoredMessage
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var msg protocol.StoredMessage
		if err := json.Unmarshal(scanner.Bytes(), &msg); err == nil {
			messages = append(messages, msg)
		} else {
			log.Printf("Error unmarshalling stored message from chat %s: %v. Line: %s", chatID, err, scanner.Text())
			// Пропускаем поврежденную строку
		}
	}

	if err := scanner.Err(); err != nil {
		log.Printf("Error scanning history file for chat %s: %v", chatID, err)
		return nil, err
	}

	// Если есть лимит, возвращаем последние N сообщений
	if limit > 0 && len(messages) > limit {
		messages = messages[len(messages)-limit:]
	}
	log.Printf("Loaded %d messages for chat %s", len(messages), chatID)
	return messages, nil
}

// Инициализация хранилища при старте пакета server
func init() {
	initHistoryStore()
}
