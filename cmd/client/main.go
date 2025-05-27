package main

import (
	"flag"
	"log"

	"github.com/vladimirruppel/messengor/internal/client"
)

var serverAddr = flag.String("addr", "localhost:8088", "host:port of the server")

func main() {
	flag.Parse()
	log.SetFlags(0)

	app := client.NewClientApp(*serverAddr)
	app.Run()
}
