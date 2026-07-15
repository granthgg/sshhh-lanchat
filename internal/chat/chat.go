// Package chat wires the transport, crypto, roster and UI together into the
// running application. It is the composition root: cmd/lanchat parses flags,
// fills a Config, and calls Run.
package chat

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/granthgg/sshhh-lanchat/internal/crypto"
	"github.com/granthgg/sshhh-lanchat/internal/proto"
	"github.com/granthgg/sshhh-lanchat/internal/roster"
	"github.com/granthgg/sshhh-lanchat/internal/transport"
	"github.com/granthgg/sshhh-lanchat/internal/ui"
)

var debugOn = os.Getenv("CHAT_DEBUG") != ""

// Config is the fully-resolved set of options for one session. The caller is
// responsible for sanitizing and defaulting fields (e.g. Nick) before Run.
type Config struct {
	Room       string // channel name
	Nick       string // display name (already sanitized/clamped)
	Passphrase string // room passphrase; empty means an open room
	Iface      string // pinned network interface, or "" to auto-detect
	Color      bool   // colorize nicknames
	Stealth    bool   // quieter banner and shell-like prompt
	Prompt     string // input prompt string
	Broadcast  bool   // send a broadcast copy alongside multicast
	Version    string // version string shown in the banner
}

// Client is one running chat session.
type Client struct {
	id   string
	nick string
	room string

	tr  *transport.Transport
	ui  *ui.UI
	ros *roster.Roster
	dd  *proto.Dedup
	seq proto.SeqGen

	aead crypto.AEAD
	quit chan struct{}
	done sync.Once

	stealth bool
	version string
}

// Run builds a session from cfg and blocks until the user quits or input ends.
// It returns an error only for setup failures (bad key, port in use); once the
// session is live it exits the process on shutdown.
func Run(cfg Config) error {
	aead, err := crypto.New(cfg.Room, cfg.Passphrase)
	if err != nil {
		return fmt.Errorf("crypto init: %w", err)
	}

	tr, err := transport.Open(cfg.Room, cfg.Iface, cfg.Broadcast)
	if err != nil {
		return fmt.Errorf("%w\n(is another program using the port, or is the network down?)", err)
	}

	c := &Client{
		id:      proto.NewInstanceID(),
		nick:    cfg.Nick,
		room:    cfg.Room,
		tr:      tr,
		ui:      ui.New(cfg.Prompt, cfg.Color),
		ros:     roster.New(),
		dd:      proto.NewDedup(),
		aead:    aead,
		quit:    make(chan struct{}),
		stealth: cfg.Stealth,
		version: cfg.Version,
	}

	c.installSignals()
	c.banner(cfg.Passphrase != "")

	go c.recvLoop()
	go c.presenceLoop()
	go c.expireLoop()
	go c.ui.Run()

	c.send(proto.TypeJoin, "")

	for line := range c.ui.Lines {
		if c.handleLine(line) {
			break
		}
	}
	c.shutdown()
	return nil
}

// handleLine processes one line of input; it returns true to quit.
func (c *Client) handleLine(line string) bool {
	line = strings.TrimRight(line, "\r\n")
	if strings.TrimSpace(line) == "" {
		return false
	}
	if strings.HasPrefix(line, "/") {
		return c.command(line)
	}
	text := proto.ClampRunes(proto.Sanitize(line), proto.MaxBodyRunes)
	c.ui.Chat(c.nick, text) // echo locally right away
	c.send(proto.TypeMsg, text)
	return false
}

func (c *Client) command(line string) (quit bool) {
	fields := strings.Fields(line)
	switch fields[0] {
	case "/quit", "/exit", "/q":
		return true
	case "/nick", "/name":
		if len(fields) < 2 {
			c.ui.System("usage: /nick <name>")
			return false
		}
		newNick := proto.ClampRunes(proto.Sanitize(fields[1]), 24)
		if newNick == "" {
			c.ui.System("that name is empty after removing control characters")
			return false
		}
		old := c.nick
		c.nick = newNick
		c.ui.System(old + " is now " + c.nick)
		c.send(proto.TypeJoin, "") // re-announce under the new name
	case "/me":
		if len(fields) < 2 {
			c.ui.System("usage: /me <action>")
			return false
		}
		text := proto.ClampRunes(proto.Sanitize(strings.TrimSpace(line[len("/me"):])), proto.MaxBodyRunes)
		c.ui.Action(c.nick, text)
		c.send(proto.TypeMe, text)
	case "/who", "/names":
		names := c.ros.List()
		if len(names) == 0 {
			c.ui.System("nobody else is here right now (just you)")
		} else {
			c.ui.System(fmt.Sprintf("here now (%d): %s", len(names), strings.Join(names, ", ")))
		}
	case "/clear":
		c.ui.ClearScreen()
	case "/boss":
		c.ui.ToggleBoss()
	case "/help", "/?":
		c.ui.System("commands: /who  /nick <name>  /me <action>  /clear  /boss  /quit   (Ctrl-B = instant hide)")
	default:
		c.ui.System("unknown command " + fields[0] + " — try /help")
	}
	return false
}

// ---- networking loops ------------------------------------------------------

func (c *Client) send(t, body string) {
	m := proto.Msg{T: t, ID: c.id, N: c.nick, S: c.seq.Next(), B: body}
	raw, err := json.Marshal(m)
	if err != nil {
		return
	}
	frame, err := crypto.Seal(c.aead, raw)
	if err != nil {
		return
	}
	c.tr.Send(frame)
}

func (c *Client) recvLoop() {
	buf := make([]byte, 2048)
	for {
		select {
		case <-c.quit:
			return
		default:
		}
		n, src, err := c.tr.Read(buf)
		if err != nil {
			select {
			case <-c.quit:
				return
			default:
				continue
			}
		}
		if debugOn {
			fmt.Fprintf(os.Stderr, "[dbg] recv %d bytes from %v\n", n, src)
		}
		plaintext, err := crypto.Open(c.aead, buf[:n])
		if err != nil {
			if debugOn {
				fmt.Fprintf(os.Stderr, "[dbg] decrypt failed (not our key?)\n")
			}
			continue // not our room / not decryptable
		}
		var m proto.Msg
		if json.Unmarshal(plaintext, &m) != nil {
			continue
		}
		if m.ID == "" || m.ID == c.id {
			continue // ignore malformed and our own loopback echoes
		}
		if !c.dd.FirstSeen(m.ID, m.S) {
			continue // duplicate from multicast+broadcast / multiple interfaces
		}

		m.N = proto.Sanitize(m.N)
		if m.N == "" {
			m.N = "?"
		}
		if c.ros.Seen(m.ID, m.N) && m.T != proto.TypeLeave {
			c.ui.System("→ " + m.N + " is here")
		}

		switch m.T {
		case proto.TypeMsg:
			c.ui.Chat(m.N, proto.Sanitize(m.B))
		case proto.TypeMe:
			c.ui.Action(m.N, proto.Sanitize(m.B))
		case proto.TypeLeave:
			if nick, ok := c.ros.Leave(m.ID); ok {
				c.ui.System("← " + nick + " left")
			}
		case proto.TypeJoin, proto.TypePing:
			// presence already recorded above
		}
	}
}

func (c *Client) presenceLoop() {
	t := time.NewTicker(4 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-c.quit:
			return
		case <-t.C:
			c.send(proto.TypePing, "")
		}
	}
}

func (c *Client) expireLoop() {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-c.quit:
			return
		case <-t.C:
			for _, nick := range c.ros.Expire() {
				c.ui.System("← " + nick + " left (timed out)")
			}
		}
	}
}

// ---- lifecycle -------------------------------------------------------------

func (c *Client) installSignals() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-ch
		c.shutdown()
	}()
}

func (c *Client) shutdown() {
	c.done.Do(func() {
		close(c.quit)
		c.send(proto.TypeLeave, "") // best-effort goodbye
		time.Sleep(30 * time.Millisecond)
		c.ui.Restore()
		_ = c.tr.Close()
		fmt.Fprintln(os.Stderr) // land the shell prompt on a fresh line
		os.Exit(0)
	})
}

func (c *Client) banner(private bool) {
	if c.stealth {
		c.ui.System(fmt.Sprintf("room %q as %q — type to chat, /quit to leave", c.room, c.nick))
	} else {
		c.ui.System("welcome to lanchat " + c.version)
		c.ui.System(fmt.Sprintf("you're in room %q as %q", c.room, c.nick))
		c.ui.System("→ type a message and press Enter to send it")
		c.ui.System("→ everyone on this Wi-Fi in the same room sees it in real time")
		c.ui.System("→ /help = commands   /nick = rename   /quit = leave   Ctrl-B = quick-hide")
	}
	if private {
		c.ui.System("this room is PRIVATE — encrypted, only people with the passphrase can read it")
	} else {
		c.ui.System(`this room is OPEN — anyone on this Wi-Fi can read it (add -k "secret" to make it private)`)
	}
	if c.tr.Joined() == 0 {
		c.ui.System("note: couldn't join multicast; using broadcast fallback (some networks block this)")
	}
	if !c.stealth {
		c.ui.System("nothing is saved — you only see messages sent while you're here. waiting…")
	}
}
