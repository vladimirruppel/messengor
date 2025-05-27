package server

import (
	"errors"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// User представляет пользователя в системе.
type User struct {
	ID           string
	Username     string
	PasswordHash string
	DisplayName  string
	CreatedAt    time.Time
}

var (
	// userStore хранит пользователей. Ключ - username.
	userStore      = make(map[string]*User)
	userStoreMutex = &sync.RWMutex{}

	// Ошибки, специфичные для хранилища/аутентификации
	ErrUserNotFound    = errors.New("user not found")
	ErrUsernameTaken   = errors.New("username is already taken")
	ErrInvalidPassword = errors.New("invalid password")
	ErrPasswordHashing = errors.New("failed to hash password")
)

// RegisterNewUser создает и сохраняет нового пользователя.
func RegisterNewUser(username, password, displayName string) (*User, error) {
	userStoreMutex.Lock()
	defer userStoreMutex.Unlock()

	if _, exists := userStore[username]; exists {
		return nil, ErrUsernameTaken
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("Error hashing password for %s: %v", username, err)
		return nil, ErrPasswordHashing
	}

	newUser := &User{
		ID:           uuid.NewString(), // Генерируем UUID
		Username:     username,
		PasswordHash: string(hashedPassword),
		DisplayName:  displayName,
		CreatedAt:    time.Now().UTC(),
	}

	userStore[username] = newUser
	log.Printf("User registered: %s (ID: %s)", newUser.Username, newUser.ID)
	return newUser, nil
}

// AuthenticateUser проверяет учетные данные пользователя.
func AuthenticateUser(username, password string) (*User, error) {
	userStoreMutex.RLock()
	user, exists := userStore[username]
	userStoreMutex.RUnlock() // Разблокируем сразу после чтения из map

	if !exists {
		return nil, ErrUserNotFound
	}

	err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password))
	if err != nil {
		// Ошибка может быть bcrypt.ErrMismatchedHashAndPassword (неверный пароль)
		// или другая ошибка bcrypt.
		if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
			return nil, ErrInvalidPassword
		}
		log.Printf("Error comparing password for %s: %v", username, err)
		return nil, err // Возвращаем оригинальную ошибку bcrypt, если она не ErrMismatchedHashAndPassword
	}

	log.Printf("User authenticated: %s (ID: %s)", user.Username, user.ID)
	return user, nil
}

func GetUserByID(userID string) (*User, bool) {
	userStoreMutex.RLock()
	defer userStoreMutex.RUnlock()
	// Это будет неэффективно, если userStore индексирован по username.
	// Для этого лучше иметь отдельную карту map[string]*User (userID -> User)
	// или перебирать:
	for _, u := range userStore {
		if u.ID == userID {
			return u, true
		}
	}
	return nil, false
}
