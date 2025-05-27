package main

import (
	"github.com/vladimirruppel/messengor/internal/client"
)

func main() {
	var serverAddr = "localhost:8088"

	client.RunClient(serverAddr)
}
