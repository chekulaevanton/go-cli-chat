package client

import (
	"context"
	"fmt"
	"io"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/jroimartin/gocui"
	"golang.org/x/net/websocket"

	"github.com/chekulaevanton/go-cli-chat/internal/pkg/connection"
	"github.com/chekulaevanton/go-cli-chat/internal/pkg/crypter"
	"github.com/chekulaevanton/go-cli-chat/internal/pkg/message"
)

const (
	WebsocketEndpoint = "ws://:3000/"
	WebsocketOrigin   = "http://"

	MessageWidget  = "messages"
	UsersWidget    = "users"
	UsernameWidget = "username"
	InputWidget    = "send"
)

const (
	UserHelp   = "HELP"
	UserSystem = "SYSTEM"
	UserServer = "SERVER"
)

const (
	StateDisconnected = "disconnected"
	StateConnected    = "connected"
	StateJoined       = "joined"
	StateWaitingChat  = "waiting"
	StateInChat       = "chat"
)

const (
	CommandConnect = "/connect"
	CommandJoin    = "/join"
	CommandLeave   = "/leave"
	CommandClear   = "/clear"
	CommandAccept  = "/accept"
	CommandReq     = "/req"
	CommandDeny    = "/deny"
	CommandEnd     = "/end"
)

var timeZone *time.Location

func init() {
	var err error
	timeZone, err = time.LoadLocation("Local")
	if err != nil {
		log.Println(err)
	}
}

type App struct {
	ctx context.Context
	c   context.CancelFunc

	tui *gocui.Gui

	username string
	conn     *connection.Conn
	state    string

	reqs map[string]string
	mu   sync.Mutex

	cr *crypter.Crypter

	pub string
	pr  string

	pubChat string
}

func NewApp() (*App, error) {
	gui, err := gocui.NewGui(gocui.OutputNormal)
	if err != nil {
		return nil, err
	}

	ctx, c := context.WithCancel(context.Background())

	app := &App{
		ctx: ctx,
		c:   c,

		tui:   gui,
		state: StateDisconnected,

		reqs: make(map[string]string),

		cr: crypter.New(),
	}

	app.pub, app.pr, err = app.cr.GenKeys()
	if err != nil {
		return nil, err
	}

	app.tui.SetManagerFunc(app.Layout)
	if err = app.SetKeyBindings(app.tui); err != nil {
		return nil, err
	}

	return app, nil
}

func (a *App) Layout(g *gocui.Gui) error {
	maxX, maxY := g.Size()
	g.Cursor = true

	if messages, err := g.SetView(MessageWidget, 0, 0, maxX-20, maxY-5); err != nil {
		if err != gocui.ErrUnknownView {
			return err
		}
		messages.Title = MessageWidget
		messages.Autoscroll = true
		messages.Wrap = true
	}

	if input, err := g.SetView(InputWidget, 0, maxY-5, maxX-20, maxY-1); err != nil {
		if err != gocui.ErrUnknownView {
			return err
		}
		input.Title = InputWidget
		input.Autoscroll = false
		input.Wrap = true
		input.Editable = true
	}

	if a.username != "" {
		if username, err := g.SetView(UsernameWidget, maxX-20, 0, maxX-1, 2); err != nil {
			if err != gocui.ErrUnknownView {
				return err
			}
			username.Title = UsernameWidget
			username.Autoscroll = false
			username.Wrap = true
			username.Clear()
			fmt.Fprint(username, a.username)
		}
		if users, err := g.SetView(UsersWidget, maxX-20, 3, maxX-1, maxY-1); err != nil {
			if err != gocui.ErrUnknownView {
				return err
			}
			users.Title = UsersWidget
			users.Autoscroll = false
			users.Wrap = true
		}
	} else {
		g.DeleteView(UsernameWidget)
		if users, err := g.SetView(UsersWidget, maxX-20, 0, maxX-1, maxY-1); err != nil {
			if err != gocui.ErrUnknownView {
				return err
			}
			users.Title = UsersWidget
			users.Autoscroll = false
			users.Wrap = true
		}
	}

	g.SetCurrentView(InputWidget)

	return nil
}

func (a *App) SetKeyBindings(g *gocui.Gui) error {
	if err := g.SetKeybinding(InputWidget, gocui.KeyCtrlC, gocui.ModNone, a.Quit); err != nil {
		return err
	}

	if err := g.SetKeybinding(InputWidget, gocui.KeyEnter, gocui.ModNone, a.WriteCommand); err != nil {
		return err
	}

	return nil
}

func (a *App) Connect() error {
	if a.conn != nil {
		if err := a.conn.Send(message.Message{Type: message.TypeStatus}); err == nil {
			return nil
		}
	}

	config, err := websocket.NewConfig(WebsocketEndpoint, WebsocketOrigin)
	if err != nil {
		return err
	}

	c, err := websocket.DialConfig(config)
	if err != nil {
		return err
	}

	a.conn = connection.New(c)

	if err = a.conn.Send(message.Message{Type: message.TypeStatus}); err != nil {
		return err
	}

	a.state = StateConnected
	return nil
}

func (a *App) WriteCommand(_ *gocui.Gui, v *gocui.View) error {
	msgs, err := a.tui.View(MessageWidget)
	if err != nil {
		return err
	}

	users, err := a.tui.View(UsersWidget)
	if err != nil {
		return err
	}

	if a.state == StateWaitingChat {
		a.writeNow(msgs, UserSystem, "Waiting...")
		v.SetCursor(0, 0)
		v.Clear()
		return nil
	}

	command := strings.TrimSpace(v.Buffer())
	if command == CommandConnect {
		a.writeNow(msgs, a.username, command)
		switch a.state {
		case StateDisconnected:
			a.writeNow(msgs, UserSystem, "Connecting...")
			if err := a.Connect(); err != nil {
				a.writeNow(msgs, UserSystem, fmt.Sprintf("Connection failed: %s", err))
				users.Clear()
			}
			a.writeNow(msgs, UserSystem, "Connected!")

		default:
			a.writeNow(msgs, UserSystem, "Already connected.")
		}

	} else if strings.HasPrefix(command, CommandJoin+" ") || command == CommandJoin {
		a.writeNow(msgs, a.username, command)
		switch a.state {
		case StateConnected:
			args := strings.Split(command, " ")
			if len(args) != 2 || args[1] == "" {
				a.writeNow(msgs, UserHelp, fmt.Sprintf("%s <username>", CommandJoin))
			} else {
				a.writeNow(msgs, UserSystem, "Joining...")
				err = a.conn.Send(message.Message{
					Type:     message.TypeJoin,
					Username: args[1],
				})
				if err != nil {
					a.writeNow(msgs, UserSystem, fmt.Sprintf("Join failed: %s", err))
				}
			}

		case StateDisconnected:
			a.writeNow(msgs, UserSystem, "Disconnected. Connect first.")

		default:
			a.writeNow(msgs, UserSystem, "Already joined.")
		}

	} else if strings.HasPrefix(command, CommandAccept+" ") || command == CommandAccept {
		a.writeNow(msgs, a.username, command)
		switch a.state {
		case StateInChat:
			a.writeNow(msgs, UserSystem, "Already in chat. Leave first")

		case StateConnected:
			a.writeNow(msgs, UserSystem, "Join first.")

		case StateDisconnected:
			a.writeNow(msgs, UserSystem, "Connect first.")

		case StateJoined:
			args := strings.Split(command, " ")
			if len(args) != 2 || args[1] == "" {
				a.writeNow(msgs, UserHelp, fmt.Sprintf("%s <username>", CommandAccept))
			} else {
				a.writeNow(msgs, UserSystem, "Accepting...")
				err = a.conn.Send(message.Message{
					Type:     message.TypeAccept,
					Username: args[1],
					Payload:  a.pub,
				})
				if err != nil {
					a.writeNow(msgs, UserSystem, fmt.Sprintf("Accept failed: %s", err))
				}
			}
		}
	} else if command == CommandEnd {
		a.writeNow(msgs, a.username, command)
		switch a.state {
		case StateJoined:
			a.writeNow(msgs, UserSystem, "Already ended.")

		case StateConnected:
			a.writeNow(msgs, UserSystem, "Join first.")

		case StateDisconnected:
			a.writeNow(msgs, UserSystem, "Connect first.")

		case StateInChat:
			a.writeNow(msgs, UserSystem, "Ending...")
			err = a.conn.Send(message.Message{Type: message.TypeEnd})
			if err != nil {
				a.writeNow(msgs, UserSystem, fmt.Sprintf("Ending failed: %s", err))
			}
		}
	} else if strings.HasPrefix(command, CommandReq+" ") || command == CommandReq {
		a.writeNow(msgs, a.username, command)
		switch a.state {
		case StateInChat:
			a.writeNow(msgs, UserSystem, "Already in chat. Leave first")

		case StateConnected:
			a.writeNow(msgs, UserSystem, "Join first.")

		case StateDisconnected:
			a.writeNow(msgs, UserSystem, "Connect first.")

		case StateJoined:
			args := strings.Split(command, " ")
			if len(args) != 2 || args[1] == "" {
				a.writeNow(msgs, UserHelp, fmt.Sprintf("%s <username>", CommandReq))
			} else {
				a.writeNow(msgs, UserSystem, "Requesting...")
				err = a.conn.Send(message.Message{
					Type:     message.TypeReq,
					Username: args[1],
					Payload:  a.pub,
				})
				if err != nil {
					a.writeNow(msgs, UserSystem, fmt.Sprintf("Request failed: %s", err))
				}
				a.state = StateWaitingChat
			}
		}
	} else if strings.HasPrefix(command, CommandDeny+" ") || command == CommandDeny {
		a.writeNow(msgs, a.username, command)
		switch a.state {
		case StateInChat:
			a.writeNow(msgs, UserSystem, "Already in chat. Leave first")

		case StateConnected:
			a.writeNow(msgs, UserSystem, "Join first.")

		case StateDisconnected:
			a.writeNow(msgs, UserSystem, "Connect first.")

		case StateJoined:
			args := strings.Split(command, " ")
			if len(args) != 2 || args[1] == "" {
				a.writeNow(msgs, UserHelp, fmt.Sprintf("%s <username>", CommandDeny))
			} else {
				a.writeNow(msgs, UserSystem, "Denying...")
				err = a.conn.Send(message.Message{
					Type:     message.TypeDeny,
					Username: args[1],
				})
				if err != nil {
					a.writeNow(msgs, UserSystem, fmt.Sprintf("Denying failed: %s", err))
				}
			}
		}
	} else if command == CommandLeave {
		a.writeNow(msgs, a.username, command)
		switch a.state {
		case StateInChat:
			fallthrough
		case StateJoined:
			a.writeNow(msgs, UserSystem, "Leaving...")
			err = a.conn.Send(message.Message{
				Type: message.TypeLeave,
			})
			if err != nil {
				a.writeNow(msgs, UserSystem, fmt.Sprintf("Leaving failed: %s", err))
			}

		case StateDisconnected:
			a.writeNow(msgs, UserSystem, "Disconnected. Connect first.")

		case StateConnected:
			a.writeNow(msgs, UserSystem, "Already left.")
		}
	} else if command == CommandClear {
		msgs.Clear()
		a.writeNow(msgs, a.username, command)
	} else if strings.HasPrefix(command, "/") {
		a.writeNow(msgs, UserSystem, fmt.Sprintf("Unknown command: %q", command))
	} else {
		switch a.state {
		case StateInChat:

			encrypted, err := a.cr.Encrypt(command, a.pubChat)
			if err != nil {
				a.writeNow(msgs, UserSystem, fmt.Sprintf("Message sending failed: %s %s", err, a.pubChat))
			} else {
				err = a.conn.Send(message.Message{
					Type:     message.TypeMessage,
					Username: a.username,
					Payload:  encrypted,
				})
				if err != nil {
					a.writeNow(msgs, UserSystem, fmt.Sprintf("Message sending failed: %s", err))
				}
			}
			a.writeNow(msgs, a.username, command)

		case StateJoined:
			a.writeNow(msgs, UserSystem, "Joined. Start chat first.")

		case StateDisconnected:
			a.writeNow(msgs, UserSystem, "Disconnected. Connect first.")

		case StateConnected:
			a.writeNow(msgs, UserSystem, "Connected. Join first.")
		}
	}

	v.SetCursor(0, 0)
	v.Clear()

	return nil
}

func (a *App) ReadMessage() error {
	for {
		select {
		case <-a.ctx.Done():
			return nil
		default:
			if a.state == StateDisconnected {
				continue
			}

			m, err := a.conn.Read()
			if err != nil {
				a.StateToDisconnected()
				continue
			}

			a.tui.Update(func(g *gocui.Gui) error {
				msgs, err := a.tui.View(MessageWidget)
				if err != nil {
					return err
				}

				users, err := a.tui.View(UsersWidget)
				if err != nil {
					return err
				}

				switch m.Type {
				case message.TypeStatus:
					users.Clear()
					fmt.Fprint(users, m.Payload)

				case message.TypeJoin:
					a.write(msgs, m.Time(), UserServer, m.Payload)
					a.username = m.Username
					a.state = StateJoined
					a.conn.Send(message.Message{Type: message.TypeStatus})

				case message.TypeLeave:
					a.write(msgs, m.Time(), UserServer, m.Payload)
					a.username = ""
					a.pr = ""
					a.state = StateConnected
					a.conn.Send(message.Message{Type: message.TypeStatus})

				case message.TypeErr:
					a.write(msgs, m.Time(), UserServer, m.Payload)

				case message.TypeReq:
					a.mu.Lock()
					a.reqs[m.Username] = m.Payload
					a.mu.Unlock()
					a.write(msgs, m.Time(), m.Username,
						fmt.Sprintf("Wants to chat with you.\n To start, enter: %q", CommandAccept+" "+m.Username))

				case message.TypeDeny:
					switch m.Payload {
					case "Timeout":
						a.write(msgs, m.Time(), m.Username, "No longer waiting to chat with you.")
						fallthrough

					case "Accept another":
						a.mu.Lock()
						delete(a.reqs, m.Username)
						a.mu.Unlock()

					case "":
						a.write(msgs, m.Time(), m.Username, "Denied your request.")

					default:
						a.write(msgs, m.Time(), UserServer, m.Payload)

					}
					a.state = StateJoined

				case message.TypeAccept:
					switch m.Payload {
					case "Accepted!":
						a.pubChat = a.reqs[m.Username]
						a.reqs = make(map[string]string)
						a.state = StateInChat
						msgs.Clear()
						a.writeNow(msgs, UserServer, "Started chat with "+m.Username)

					default:
						a.pubChat = m.Payload
						a.reqs = make(map[string]string)
						a.state = StateInChat
						msgs.Clear()
						a.writeNow(msgs, UserServer, "Started chat with "+m.Username)

					}

				case message.TypeEnd:
					a.write(msgs, m.Time(), UserServer, m.Payload)
					a.state = StateJoined

				case message.TypeMessage:
					decrypted, err := a.cr.Decrypt(m.Payload, a.pr)
					if err != nil {
						a.writeNow(msgs, UserSystem, fmt.Sprintf("Message receiving failed: %s", err))
					} else {
						a.write(msgs, m.Time(), m.Username, decrypted)
					}
				}

				return nil
			})
		}
	}
}

func (a *App) Quit(_ *gocui.Gui, _ *gocui.View) error {
	// TODO: disconnect
	a.c()
	return gocui.ErrQuit
}

func (a *App) Serve() error {
	go a.ReadMessage()

	if err := a.tui.MainLoop(); err != nil && err != gocui.ErrQuit {
		return err
	}

	return nil
}

func (a *App) Close() {
	a.tui.Close()
}

func (a *App) write(w io.Writer, t time.Time, user, msg string) error {
	if user == "" {
		user = "USER"
	}

	st := t.In(timeZone).Format(time.Kitchen)
	_, err := fmt.Fprintf(w, "%s [%s] %s\n", st, user, msg)
	return err
}

func (a *App) writeNow(w io.Writer, user, msg string) error {
	return a.write(w, time.Now().UTC(), user, msg)
}

func (a *App) StateToDisconnected() {
	a.conn.Close()
	a.conn = nil
	a.state = StateDisconnected
	a.username = ""
	a.tui.Update(func(g *gocui.Gui) error {
		msgs, err := a.tui.View(MessageWidget)
		if err != nil {
			return err
		}
		users, err := a.tui.View(UsersWidget)
		if err != nil {
			return err
		}

		a.writeNow(msgs, UserSystem, "Disconnected!")
		users.Clear()

		return nil
	})
}
