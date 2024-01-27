package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/net/websocket"

	"github.com/chekulaevanton/go-cli-chat/internal/pkg/connection"
	"github.com/chekulaevanton/go-cli-chat/internal/pkg/message"
)

var (
	ErrAlreadyJoined    = errors.New("already joined")
	ErrAlreadyLeft      = errors.New("already left")
	ErrAlreadyInChat    = errors.New("user already in chat")
	ErrNotInChat        = errors.New("not in chat")
	ErrChatWithYourself = errors.New("chat with yourself")
	ErrEmptyUsername    = errors.New("empty username")
	ErrUsernameNotFound = errors.New("username not found")
	ErrReqNotFound      = errors.New("req not found")
	ErrNotJoined        = errors.New("user not joined")
)

type Server struct {
	ctx context.Context
	c   context.CancelFunc

	users     map[string]*User
	usersUuid map[uuid.UUID]*User
	conns     map[uuid.UUID]*connection.Conn
	reqs      map[string]map[string]chan string

	umu sync.Mutex
	rmu sync.Mutex
	cmu sync.Mutex
}

func NewServer() *Server {
	ctx, c := context.WithCancel(context.Background())
	return &Server{
		ctx:       ctx,
		c:         c,
		users:     make(map[string]*User),
		usersUuid: make(map[uuid.UUID]*User),
		conns:     make(map[uuid.UUID]*connection.Conn),
		reqs:      make(map[string]map[string]chan string),
	}
}

func (s *Server) Serve(addr string) {
	http.Handle("/", websocket.Server{
		Handler: s.handler,
	})

	go s.broadcastStatus()

	err := http.ListenAndServe(addr, nil)
	if !errors.Is(err, http.ErrServerClosed) {
		s.c()
		log.Fatal(err)
	}
}

func (s *Server) handler(c *websocket.Conn) {
	conn := connection.New(c)

	if err := s.connect(conn); err != nil {
		log.Println(err)
		if err = conn.Close(); err != nil {
			log.Println(err)
		}
		return
	}

	if err := s.read(conn); err != nil {
		if err = conn.Close(); err != nil {
			log.Println(err)
		}

		if err = s.Leave(conn); err != nil {
			log.Println(err)
		}

		if err = s.Disconnect(conn); err != nil {
			log.Println(err)
		}
		return
	}
}

func (s *Server) connect(conn *connection.Conn) error {
	s.cmu.Lock()
	defer s.cmu.Unlock()

	if _, ok := s.conns[conn.Id]; ok {
		return fmt.Errorf("already connected: %s", conn.Id.String())
	}

	s.conns[conn.Id] = conn
	return nil
}

func (s *Server) read(conn *connection.Conn) error {
	for {
		select {
		case <-s.ctx.Done():
			if err := conn.Close(); err != nil {
				log.Println(err)
			}
			return nil

		default:
			m, err := conn.Read()
			if err != nil {
				return err
			}

			switch m.Type {
			case message.TypeJoin:
				if err = s.Join(conn, m.Username); err != nil {
					if errors.Is(err, ErrAlreadyJoined) {
						err := conn.Send(message.Message{
							Type:    message.TypeLeave,
							Payload: fmt.Sprintf("User %q already joined. Use different username.", m.Username),
						}.SetTimestamp(time.Now()))
						if err != nil {
							return err
						}
					} else if errors.Is(err, ErrEmptyUsername) {
						err := conn.Send(message.Message{
							Type:    message.TypeLeave,
							Payload: fmt.Sprintf("Empty username."),
						}.SetTimestamp(time.Now()))
						if err != nil {
							return err
						}
					} else {
						conn.Send(message.Message{
							Type:    message.TypeErr,
							Payload: fmt.Sprintf("Error: %s", err),
						}.SetTimestamp(time.Now()))
						return err
					}
				}

				err = conn.Send(message.Message{
					Type:     message.TypeJoin,
					Username: m.Username,
					Payload:  fmt.Sprintf("Joined! Your username is %q.", m.Username),
				}.SetTimestamp(time.Now()))
				if err != nil {
					return err
				}

			case message.TypeLeave:
				if err = s.Leave(conn); err != nil {
					if errors.Is(err, ErrAlreadyLeft) {
						err = conn.Send(message.Message{
							Type:    message.TypeLeave,
							Payload: "Already left!",
						}.SetTimestamp(time.Now()))
						if err != nil {
							return err
						}
					} else {
						conn.Send(message.Message{
							Type:    message.TypeErr,
							Payload: fmt.Sprintf("Error: %s", err),
						}.SetTimestamp(time.Now()))
						return err
					}
				}

				err = conn.Send(message.Message{
					Type:    message.TypeLeave,
					Payload: "Left!",
				}.SetTimestamp(time.Now()))
				if err != nil {
					return err
				}

			case message.TypeReq:
				t, p, err := s.Req(conn, m)
				if err != nil {
					if errors.Is(err, ErrAlreadyInChat) {
						err = conn.Send(message.Message{
							Type:    message.TypeDeny,
							Payload: fmt.Sprintf("User %q already in chat", m.Username),
						}.SetTimestamp(time.Now()))
						if err != nil {
							return err
						}
					} else if errors.Is(err, ErrChatWithYourself) {
						err = conn.Send(message.Message{
							Type:    message.TypeDeny,
							Payload: "Chat with yourself not allowed",
						}.SetTimestamp(time.Now()))
						if err != nil {
							return err
						}
					} else if errors.Is(err, ErrUsernameNotFound) {
						err = conn.Send(message.Message{
							Type:    message.TypeDeny,
							Payload: fmt.Sprintf("Username %q not found", m.Username),
						}.SetTimestamp(time.Now()))
						if err != nil {
							return err
						}
					} else if errors.Is(err, ErrNotJoined) {
						err = conn.Send(message.Message{
							Type:    message.TypeLeave,
							Payload: "User not joined",
						}.SetTimestamp(time.Now()))
						if err != nil {
							return err
						}
					} else {
						conn.Send(message.Message{
							Type:    message.TypeErr,
							Payload: fmt.Sprintf("Error: %s", err),
						}.SetTimestamp(time.Now()))
						return err
					}
				}

				err = conn.Send(message.Message{
					Type:     t,
					Username: m.Username,
					Payload:  p,
				}.SetTimestamp(time.Now()))
				if err != nil {
					return err
				}

			case message.TypeAccept:
				if err = s.Accept(conn, m); err != nil {
					if errors.Is(err, ErrReqNotFound) {
						err = conn.Send(message.Message{
							Type:    message.TypeDeny,
							Payload: "Request not found",
						}.SetTimestamp(time.Now()))
						if err != nil {
							return err
						}
					} else if errors.Is(err, ErrUsernameNotFound) {
						err = conn.Send(message.Message{
							Type:    message.TypeDeny,
							Payload: fmt.Sprintf("Username %q not found", m.Username),
						}.SetTimestamp(time.Now()))
						if err != nil {
							return err
						}
					} else if errors.Is(err, ErrNotJoined) {
						err = conn.Send(message.Message{
							Type:    message.TypeLeave,
							Payload: "User not joined",
						}.SetTimestamp(time.Now()))
						if err != nil {
							return err
						}
					} else {
						conn.Send(message.Message{
							Type:    message.TypeErr,
							Payload: fmt.Sprintf("Error: %s", err),
						}.SetTimestamp(time.Now()))
						return err
					}
				}

				err = conn.Send(message.Message{
					Type:     message.TypeAccept,
					Username: m.Username,
					Payload:  "Accepted!",
				}.SetTimestamp(time.Now()))
				if err != nil {
					return err
				}

			case message.TypeDeny:
				if err = s.Deny(conn, m); err != nil {
					if errors.Is(err, ErrReqNotFound) {
						err = conn.Send(message.Message{
							Type:    message.TypeDeny,
							Payload: "Request not found",
						}.SetTimestamp(time.Now()))
						if err != nil {
							return err
						}
					} else if errors.Is(err, ErrUsernameNotFound) {
						err = conn.Send(message.Message{
							Type:    message.TypeDeny,
							Payload: fmt.Sprintf("Username %q not found", m.Username),
						}.SetTimestamp(time.Now()))
						if err != nil {
							return err
						}
					} else if errors.Is(err, ErrNotJoined) {
						err = conn.Send(message.Message{
							Type:    message.TypeLeave,
							Payload: "User not joined",
						}.SetTimestamp(time.Now()))
						if err != nil {
							return err
						}
					} else {
						conn.Send(message.Message{
							Type:    message.TypeErr,
							Payload: fmt.Sprintf("Error: %s", err),
						}.SetTimestamp(time.Now()))
						return err
					}
				}

				err = conn.Send(message.Message{
					Type:     message.TypeDeny,
					Username: m.Username,
					Payload:  "Denied!",
				}.SetTimestamp(time.Now()))
				if err != nil {
					return err
				}

			case message.TypeEnd:
				if err = s.End(conn); err != nil {
					if errors.Is(err, ErrNotInChat) {
						err = conn.Send(message.Message{
							Type:    message.TypeEnd,
							Payload: "Not in chat",
						}.SetTimestamp(time.Now()))
						if err != nil {
							return err
						}
					} else if errors.Is(err, ErrNotJoined) {
						err = conn.Send(message.Message{
							Type:    message.TypeLeave,
							Payload: "User not joined",
						}.SetTimestamp(time.Now()))
						if err != nil {
							return err
						}
					} else {
						conn.Send(message.Message{
							Type:    message.TypeErr,
							Payload: fmt.Sprintf("Error: %s", err),
						}.SetTimestamp(time.Now()))
						return err
					}
				}

				err = conn.Send(message.Message{
					Type:    message.TypeEnd,
					Payload: "Ended!",
				}.SetTimestamp(time.Now()))
				if err != nil {
					return err
				}

			case message.TypeMessage:
				if err = s.Message(conn, m); err != nil {
					if errors.Is(err, ErrNotInChat) {
						err = conn.Send(message.Message{
							Type:    message.TypeEnd,
							Payload: "Not in chat",
						}.SetTimestamp(time.Now()))
						if err != nil {
							return err
						}
					} else if errors.Is(err, ErrNotJoined) {
						err = conn.Send(message.Message{
							Type:    message.TypeLeave,
							Payload: "User not joined",
						}.SetTimestamp(time.Now()))
						if err != nil {
							return err
						}
					} else {
						conn.Send(message.Message{
							Type:    message.TypeErr,
							Payload: fmt.Sprintf("Error: %s", err),
						}.SetTimestamp(time.Now()))
						return err
					}
				}

			case message.TypeStatus:
				var sb strings.Builder
				sb.WriteString(fmt.Sprintf("Connected %d\n", len(s.conns)))
				s.umu.Lock()
				for u := range s.users {
					sb.WriteString(u)
					sb.WriteByte('\n')
				}
				s.umu.Unlock()

				err = conn.Send(message.Message{
					Type:    message.TypeStatus,
					Payload: sb.String(),
				}.SetTimestamp(time.Now()))
				if err != nil {
					return err
				}

			default:
				log.Printf("Unknown message type %q", m.Type)
			}

		}
	}
}

func (s *Server) broadcast(message message.Message) {
	s.cmu.Lock()
	for _, c := range s.conns {
		if err := c.Send(message); err != nil {
			log.Println(err)
		}
	}
	s.cmu.Unlock()
}

func (s *Server) broadcastStatus() {
	t := time.NewTicker(5 * time.Second)

	for {
		select {
		case <-t.C:
			log.Println("Broadcast status")

			var sb strings.Builder
			sb.WriteString(fmt.Sprintf("Connected %d\n", len(s.conns)))
			s.umu.Lock()
			for u := range s.users {
				sb.WriteString(u)
				sb.WriteByte('\n')
			}
			s.umu.Unlock()

			s.broadcast(message.Message{
				Type:    message.TypeStatus,
				Payload: sb.String(),
			}.SetTimestamp(time.Now()))

		case <-s.ctx.Done():
			return
		}
	}
}

func (s *Server) Join(conn *connection.Conn, username string) error {
	s.umu.Lock()
	defer s.umu.Unlock()

	if username == "" {
		return ErrEmptyUsername
	}

	if _, ok := s.users[username]; ok {
		return ErrAlreadyJoined
	}

	u := &User{
		Username: username,
		Conn:     conn,
	}

	s.users[username] = u
	s.usersUuid[conn.Id] = u

	return nil
}

func (s *Server) Leave(conn *connection.Conn) error {
	s.umu.Lock()
	defer s.umu.Unlock()

	u1, ok := s.usersUuid[conn.Id]
	if !ok {
		return ErrAlreadyLeft
	}

	if u1.ChatWith != nil {
		if u1.ChatWith != nil {
			u2 := u1.ChatWith

			u1.Conn.Send(message.Message{Type: message.TypeEnd})
			err2 := u2.Conn.Send(message.Message{Type: message.TypeEnd})

			u1.ChatWith = nil
			u2.ChatWith = nil

			if err2 != nil {
				if err := s.Leave(u2.Conn); err != nil {
					log.Println(err)
				}
			}
		}
	}

	delete(s.usersUuid, conn.Id)
	delete(s.users, u1.Username)

	return nil
}

func (s *Server) Req(conn *connection.Conn, m message.Message) (message.Type, string, error) {
	s.umu.Lock()

	u1, ok := s.usersUuid[conn.Id]
	if !ok {
		return "", "", ErrNotJoined
	}

	u2, ok := s.users[m.Username]
	if !ok {
		return "", "", ErrUsernameNotFound
	}

	s.umu.Unlock()

	if u1.Username == u2.Username {
		return "", "", ErrChatWithYourself
	}

	if u2.ChatWith != nil {
		return "", "", ErrAlreadyInChat
	}

	err := u2.Conn.Send(message.Message{
		Type:     message.TypeReq,
		Username: u1.Username,
		Payload:  m.Payload,
	}.SetTimestamp(time.Now()))
	if err != nil {
		return "", "", err
	}

	c := make(chan string)
	s.rmu.Lock()
	if _, ok := s.reqs[u2.Username]; !ok {
		s.reqs[u2.Username] = make(map[string]chan string)
	}
	s.reqs[u2.Username][u1.Username] = c
	s.rmu.Unlock()

	t := time.NewTicker(time.Minute)
	select {
	case <-t.C:
		s.rmu.Lock()
		delete(s.reqs[u2.Username], u1.Username)
		s.rmu.Unlock()

		u2.Conn.Send(message.Message{
			Type:     message.TypeDeny,
			Username: u1.Username,
			Payload:  "Timeout",
		}.SetTimestamp(time.Now()))

		return message.TypeDeny, "", nil
	case payload, ok := <-c:
		if !ok {
			s.rmu.Lock()
			delete(s.reqs[u2.Username], u1.Username)
			s.rmu.Unlock()

			u2.Conn.Send(message.Message{
				Type:     message.TypeDeny,
				Username: u1.Username,
				Payload:  "Accept another",
			}.SetTimestamp(time.Now()))
			return message.TypeDeny, "", nil
		}

		s.rmu.Lock()
		chs := make([]chan string, 0, len(s.reqs[u2.Username]))
		for _, ch := range s.reqs[u2.Username] {
			chs = append(chs, ch)
		}
		s.rmu.Unlock()

		for _, ch := range chs {
			close(ch)
		}

		s.rmu.Lock()
		delete(s.reqs, u2.Username)
		s.rmu.Unlock()

		return message.TypeAccept, payload, nil
	}
}

func (s *Server) Disconnect(conn *connection.Conn) error {
	s.umu.Lock()
	defer s.umu.Unlock()

	u, ok := s.usersUuid[conn.Id]
	if ok {
		if u.ChatWith != nil {
			u2 := u.ChatWith

			u.Conn.Send(message.Message{Type: message.TypeEnd})
			err2 := u2.Conn.Send(message.Message{Type: message.TypeEnd})

			u.ChatWith = nil
			u2.ChatWith = nil

			if err2 != nil {
				if err := s.Leave(u2.Conn); err != nil {
					log.Println(err)
				}
			}
		}
		delete(s.usersUuid, conn.Id)
		delete(s.users, u.Username)
		s.cmu.Lock()
		delete(s.conns, conn.Id)
		s.cmu.Unlock()

		return u.Conn.Close()
	}

	s.cmu.Lock()
	delete(s.conns, conn.Id)
	s.cmu.Unlock()
	return conn.Close()
}

func (s *Server) Accept(conn *connection.Conn, m message.Message) error {
	s.umu.Lock()

	u2, ok := s.usersUuid[conn.Id]
	if !ok {
		return ErrNotJoined
	}

	u1, ok := s.users[m.Username]
	if !ok {
		return ErrUsernameNotFound
	}

	s.umu.Unlock()

	s.rmu.Lock()
	reqs, ok := s.reqs[u2.Username]
	if !ok {
		return ErrReqNotFound
	}
	ch, ok := reqs[u1.Username]
	if !ok {
		return ErrReqNotFound
	}
	s.rmu.Unlock()

	ch <- m.Payload

	u1.ChatWith = u2
	u2.ChatWith = u1

	return nil
}

func (s *Server) Deny(conn *connection.Conn, m message.Message) error {
	s.umu.Lock()

	u2, ok := s.usersUuid[conn.Id]
	if !ok {
		return ErrNotJoined
	}

	u1, ok := s.users[m.Username]
	if !ok {
		return ErrUsernameNotFound
	}

	s.umu.Unlock()

	s.rmu.Lock()
	reqs, ok := s.reqs[u2.Username]
	if !ok {
		return ErrReqNotFound
	}
	ch, ok := reqs[u1.Username]
	if !ok {
		return ErrReqNotFound
	}
	s.rmu.Unlock()

	close(ch)

	return nil
}

func (s *Server) Message(conn *connection.Conn, m message.Message) error {
	s.umu.Lock()
	u1, ok := s.usersUuid[conn.Id]
	if !ok {
		return ErrNotJoined
	}
	s.umu.Unlock()

	if u1.ChatWith == nil {
		return ErrNotInChat
	}

	u2 := u1.ChatWith
	u2.Conn.Send(message.Message{
		Type:     message.TypeMessage,
		Username: u1.Username,
		Payload:  m.Payload,
	})
	u1.Conn.Send(message.Message{
		Type:     message.TypeMessage,
		Username: u1.Username,
		Payload:  m.Payload,
	})

	return nil
}

func (s *Server) End(conn *connection.Conn) error {
	s.umu.Lock()
	u1, ok := s.usersUuid[conn.Id]
	if !ok {
		return ErrNotJoined
	}
	s.umu.Unlock()

	if u1.ChatWith == nil {
		return ErrNotInChat
	}

	u2 := u1.ChatWith
	u2.Conn.Send(message.Message{
		Type: message.TypeEnd,
	})

	return nil
}
