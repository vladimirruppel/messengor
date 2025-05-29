package client

import (
	"fmt"

	"github.com/vladimirruppel/messengor/internal/protocol"
)

func displayUnauthenticatedMenu() {
	fmt.Println("\n--- Messenger Menu ---")
	fmt.Println("1. Login")
	fmt.Println("2. Register")
	fmt.Println("3. Exit")
	fmt.Print("Choose an option: ")
}

// Отображает главное меню для аутентифицированного пользователя
func displayMainMenu(displayName string, userID string) {
	fmt.Println("\n--- Main Menu ---")
	fmt.Printf("Logged in as: %s (ID: %s)\n", displayName, userID)
	fmt.Println("1. Enter Global Chat (Broadcast)")
	fmt.Println("2. Start Private Chat")
	fmt.Println("3. Logout")
	fmt.Println("4. Exit")
	fmt.Print("Choose an option: ")
}

// Отображает список пользователей для выбора
func displayUserList(users []protocol.UserInfo) {
	fmt.Println("\n--- Select User to Chat With ---")
	if len(users) == 0 {
		fmt.Println("No other users available.")
		fmt.Println("0. Back to Main Menu")
		fmt.Print("Choose an option: ")
		return
	}

	for i, user := range users {
		status := "Offline"
		if user.IsOnline {
			status = "Online"
		}
		fmt.Printf("%d. %s (%s)\n", i+1, user.DisplayName, status)
	}
	fmt.Println("0. Back to Main Menu")
	fmt.Print("Enter user number: ")
}
