package main

import (
	"github.com/chekulaevanton/go-cli-chat/internal/server"
)

func main() {
	server.NewServer().Serve(":3000")
}
