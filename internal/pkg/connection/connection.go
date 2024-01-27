package connection

import (
	"github.com/google/uuid"
	"golang.org/x/net/websocket"

	"github.com/chekulaevanton/go-cli-chat/internal/pkg/message"
)

type Conn struct {
	Id   uuid.UUID
	conn *websocket.Conn
}

func New(c *websocket.Conn) *Conn {
	return &Conn{
		Id:   uuid.New(),
		conn: c,
	}
}

func (c *Conn) Send(m message.Message) error {
	return websocket.JSON.Send(c.conn, m)
}

func (c *Conn) Read() (message.Message, error) {
	var m message.Message

	if err := websocket.JSON.Receive(c.conn, &m); err != nil {
		return message.Message{}, err
	}

	return m, nil
}

func (c *Conn) Close() error {
	if c.conn == nil {
		return nil
	}

	err := c.conn.Close()
	c.conn = nil
	return err
}
