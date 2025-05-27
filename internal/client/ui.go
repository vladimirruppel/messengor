package client

import (
	"fmt"
)

func displayUnauthenticatedMenu() {
	fmt.Println("\n--- Messenger Menu ---")
	fmt.Println("1. Login")
	fmt.Println("2. Register")
	fmt.Println("3. Exit")
	fmt.Print("Choose an option: ")
}

func displayMainMenu(displayName string, userID string) {
	fmt.Println("\n--- Main Menu ---")
	fmt.Printf("Logged in as: %s (ID: %s)\n", displayName, userID)
	fmt.Println("1. Enter Global Chat")
	fmt.Println("2. Logout")
	fmt.Println("3. Exit")
	fmt.Print("Choose an option: ")
}
