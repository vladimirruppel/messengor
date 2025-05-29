package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os" // Используем os для работы с файлами
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// User представляет пользователя в системе.
type User struct {
	ID           string    `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"password_hash"`
	DisplayName  string    `json:"display_name"`
	CreatedAt    time.Time `json:"created_at"`
}

var (
	// userStore хранит пользователей. Ключ - username.
	userStore      map[string]*User // Будет инициализирован в loadUsersFromFile или init
	userStoreMutex = &sync.RWMutex{}

	// Ошибки, специфичные для хранилища/аутентификации
	ErrUserNotFound    = errors.New("user not found")
	ErrUsernameTaken   = errors.New("username is already taken")
	ErrInvalidPassword = errors.New("invalid password")
	ErrPasswordHashing = errors.New("failed to hash password")
)

const userStoreFile = "users_data.json" // Файл для хранения данных пользователей

// init вызывается один раз при импорте пакета.
func init() {
	if err := loadUsersFromFile(); err != nil {
		// Если не удалось загрузить, сервер продолжит работу с пустым хранилищем (или создаст новый файл при первой регистрации).
		log.Printf("Warning: Could not load users from '%s': %v. Starting with an empty user store.", userStoreFile, err)
		// Убедимся, что userStore инициализирован в любом случае
		userStoreMutex.Lock()
		if userStore == nil {
			userStore = make(map[string]*User)
		}
		userStoreMutex.Unlock()
	}
}

// loadUsersFromFile загружает данные пользователей из JSON-файла.
func loadUsersFromFile() error {
	userStoreMutex.Lock() // Полная блокировка на время загрузки
	defer userStoreMutex.Unlock()

	// Инициализируем userStore, даже если файл не существует или пуст
	userStore = make(map[string]*User)

	data, err := os.ReadFile(userStoreFile)
	if err != nil {
		if os.IsNotExist(err) {
			log.Printf("User data file '%s' not found. A new one will be created on first registration.", userStoreFile)
			return nil // Это не ошибка, просто нет сохраненных пользователей
		}
		return fmt.Errorf("failed to read user data file '%s': %w", userStoreFile, err)
	}

	// Если файл пустой, также считаем, что нет сохраненных пользователей.
	if len(data) == 0 {
		log.Printf("User data file '%s' is empty.", userStoreFile)
		return nil
	}

	if err := json.Unmarshal(data, &userStore); err != nil {
		// Если файл поврежден, лучше начать с чистого листа, чем с некорректными данными.
		log.Printf("Error unmarshalling user data from '%s': %v. Store will be treated as empty.", userStoreFile, err)
		userStore = make(map[string]*User) // Сбрасываем до пустого состояния
		return fmt.Errorf("failed to unmarshal user data: %w", err)
	}

	log.Printf("Successfully loaded %d users from '%s'.", len(userStore), userStoreFile)
	return nil
}

// saveUsersToFile сохраняет текущее состояние userStore в JSON-файл.
// Эта функция должна вызываться, когда userStoreMutex уже ЗАХВАЧЕН НА ЗАПИСЬ (Lock),
// так как она отражает состояние userStore, которое только что было изменено.
func saveUsersToFile() error {
	// Данные для сохранения (userStore) уже должны быть защищены мьютексом в вызывающей функции.
	data, err := json.MarshalIndent(userStore, "", "  ") // Используем MarshalIndent для читаемости файла
	if err != nil {
		return fmt.Errorf("failed to marshal user store: %w", err)
	}

	if err := os.WriteFile(userStoreFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write user data to file '%s': %w", userStoreFile, err)
	}
	// log.Printf("User store successfully saved to '%s'.", userStoreFile) // Можно раскомментировать для отладки
	return nil
}

// RegisterNewUser создает и сохраняет нового пользователя.
func RegisterNewUser(username, password, displayName string) (*User, error) {
	userStoreMutex.Lock() // Блокируем на время проверки и модификации userStore и сохранения в файл
	defer userStoreMutex.Unlock()

	if userStore == nil { // Дополнительная страховка, если init не смог инициализировать
		log.Println("User store was nil in RegisterNewUser, re-initializing.")
		userStore = make(map[string]*User)
	}

	if _, exists := userStore[username]; exists {
		return nil, ErrUsernameTaken
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("Error hashing password for %s: %v", username, err)
		return nil, ErrPasswordHashing
	}

	newUser := &User{
		ID:           uuid.NewString(),
		Username:     username,
		PasswordHash: string(hashedPassword),
		DisplayName:  displayName,
		CreatedAt:    time.Now().UTC(),
	}

	userStore[username] = newUser // Добавляем в map в памяти

	// Сохраняем обновленный userStore в файл
	if err := saveUsersToFile(); err != nil {
		log.Printf("CRITICAL: User %s registered in memory, but FAILED TO SAVE to '%s': %v. Rolling back in-memory registration.", username, userStoreFile, err)
		delete(userStore, username) // Откатываем добавление в память
		return nil, fmt.Errorf("failed to save new user to persistent store: %w", err)
	}

	log.Printf("User registered and saved: %s (ID: %s)", newUser.Username, newUser.ID)
	return newUser, nil
}

// AuthenticateUser проверяет учетные данные пользователя.
func AuthenticateUser(username, password string) (*User, error) {
	userStoreMutex.RLock() // Блокировка на чтение
	// Проверка на nil, если вдруг init не отработал
	if userStore == nil {
		userStoreMutex.RUnlock()
		log.Printf("CRITICAL: userStore is nil during AuthenticateUser for %s. This should not happen.", username)
		return nil, errors.New("user store not initialized, please try again or contact admin")
	}
	user, exists := userStore[username]
	userStoreMutex.RUnlock()

	if !exists {
		return nil, ErrUserNotFound
	}

	err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password))
	if err != nil {
		if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
			return nil, ErrInvalidPassword
		}
		log.Printf("Error comparing password for %s: %v", username, err)
		return nil, err // Возвращаем оригинальную ошибку bcrypt
	}

	log.Printf("User authenticated: %s (ID: %s)", user.Username, user.ID)
	return user, nil
}

// GetUserByID находит пользователя по ID.
func GetUserByID(userID string) (*User, bool) {
	userStoreMutex.RLock()
	defer userStoreMutex.RUnlock()

	if userStore == nil {
		log.Printf("CRITICAL: userStore is nil during GetUserByID for %s. This should not happen.", userID)
		return nil, false
	}

	for _, u := range userStore {
		if u.ID == userID {
			return u, true
		}
	}
	return nil, false
}
