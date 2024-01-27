package server

import (
	"github.com/chekulaevanton/go-cli-chat/internal/pkg/connection"
)

type User struct {
	Username string

	Conn     *connection.Conn
	ChatWith *User
}
