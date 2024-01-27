package main

import (
	"log"

	"github.com/chekulaevanton/go-cli-chat/internal/client"
)

func main() {
	app, err := client.NewApp()
	if err != nil {
		log.Fatal(err)
	}
	defer app.Close()

	if err = app.Serve(); err != nil {
		log.Fatal(err)
	}
}
