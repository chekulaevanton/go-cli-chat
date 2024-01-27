package message

import (
	"time"
)

type Message struct {
	Type      Type   `json:"type"`
	Username  string `json:"username"`
	Payload   string `json:"payload"`
	Timestamp int64  `json:"timestamp"`
}

func (m Message) SetTimestamp(t time.Time) Message {
	m.Timestamp = t.UTC().Unix()
	return m
}

func (m Message) Time() time.Time {
	return time.Unix(m.Timestamp, 0)
}

type Type string

const (
	TypeReq    Type = "req"
	TypeAccept Type = "accept"
	TypeDeny   Type = "deny"
	TypeEnd    Type = "end"

	TypeJoin  Type = "join"
	TypeLeave Type = "leave"
	TypeErr   Type = "err"

	TypeMessage Type = "message"

	TypeStatus Type = "status"
)
