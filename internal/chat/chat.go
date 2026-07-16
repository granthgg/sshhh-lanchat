// Package chat wires the transport, crypto, roster and UI together into the
// running application. It is the composition root: cmd/lanchat parses flags,
// fills a Config, and calls Run.
package chat

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
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
	Broadcast  bool   // send directed-broadcast copies alongside multicast
	TTL        int    // multicast TTL (1 = stay on the local segment)
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

	stealth   bool
	broadcast bool
	version   string
}

// Run builds a session from cfg and blocks until the user quits, input ends,
// or a shutdown signal arrives. It returns an error only for setup failures
// (bad key, port in use); a live session always exits cleanly through the
// same idempotent shutdown path.
func Run(cfg Config) error {
	aead, err := crypto.New(cfg.Room, cfg.Passphrase)
	if err != nil {
		return fmt.Errorf("crypto init: %w", err)
	}

	tr, err := transport.Open(transport.Options{
		Room:      cfg.Room,
		Iface:     cfg.Iface,
		Broadcast: cfg.Broadcast,
		TTL:       cfg.TTL,
	})
	if err != nil {
		return fmt.Errorf("%w\n(is another program using the port, or is the network down?)", err)
	}

	c := &Client{
		id:        proto.NewInstanceID(),
		nick:      cfg.Nick,
		room:      cfg.Room,
		tr:        tr,
		ui:        ui.New(cfg.Prompt, cfg.Color, cfg.Stealth),
		ros:       roster.New(),
		dd:        proto.NewDedup(),
		aead:      aead,
		quit:      make(chan struct{}),
		stealth:   cfg.Stealth,
		broadcast: cfg.Broadcast,
		version:   cfg.Version,
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
	text := clampBody(line)
	c.ui.Chat(c.nick, text) // echo locally, exactly what will be sent
	c.send(proto.TypeMsg, text)
	return false
}

// clampBody sanitizes outgoing text and enforces both caps: runes for what a
// line may hold, bytes so the encrypted frame fits one unfragmented datagram
// (fragments are the first thing flaky networks drop).
func clampBody(s string) string {
	return proto.ClampBytes(proto.ClampRunes(proto.Sanitize(s), proto.MaxBodyRunes), proto.MaxBodyBytes)
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
		text := clampBody(strings.TrimSpace(line[len("/me"):]))
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
	raw, err := m.EncodeBounded(proto.MaxRawBytes)
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
	// 64 KB: the largest possible UDP datagram. Our own frames are bounded far
	// below the MTU, but a too-small buffer would silently truncate any
	// oversized frame (e.g. from an older build) and turn it into a decrypt
	// failure — a message lost with no trace.
	buf := make([]byte, 64*1024)
	for {
		select {
		case <-c.quit:
			return
		default:
		}
		n, src, err := c.tr.Read(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			select {
			case <-c.quit:
				return
			default:
			}
			time.Sleep(50 * time.Millisecond) // don't spin on a persistent error
			continue
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
		prev, isNew := c.ros.Seen(m.ID, m.N)
		if m.T != proto.TypeLeave {
			switch {
			case isNew:
				c.ui.System("→ " + m.N + " is here")
			case prev != m.N:
				c.ui.System(prev + " is now " + m.N)
			}
		}
		if m.T == proto.TypeJoin {
			// Answer a newcomer's join with one delayed ping so their roster
			// (and /who) fills within ~½s instead of waiting for the next 4s
			// heartbeat. The jitter keeps a full room from replying at once.
			c.pongSoon()
		}

		switch m.T {
		case proto.TypeMsg:
			c.ui.Chat(m.N, proto.ClampRunes(proto.Sanitize(m.B), proto.MaxBodyRunes))
		case proto.TypeMe:
			c.ui.Action(m.N, proto.ClampRunes(proto.Sanitize(m.B), proto.MaxBodyRunes))
		case proto.TypeLeave:
			if nick, ok := c.ros.Leave(m.ID); ok {
				c.ui.System("← " + nick + " left")
			}
		case proto.TypeJoin, proto.TypePing:
			// presence already recorded above
		}
	}
}

func (c *Client) pongSoon() {
	delay := time.Duration(100+rand.IntN(300)) * time.Millisecond
	go func() {
		select {
		case <-c.quit:
		case <-time.After(delay):
			c.send(proto.TypePing, "")
		}
	}()
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

// installSignals turns SIGINT/SIGTERM/SIGHUP into a graceful shutdown. SIGHUP
// covers the terminal window being closed, so peers get a "left" immediately
// rather than a timeout 13 seconds later.
func (c *Client) installSignals() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		<-ch
		c.shutdown()
		os.Exit(0)
	}()
}

// shutdown broadcasts a best-effort goodbye and releases the terminal and the
// socket. It is idempotent and safe to call from any goroutine; it does not
// exit the process, so Run can return normally.
func (c *Client) shutdown() {
	c.done.Do(func() {
		close(c.quit)
		c.send(proto.TypeLeave, "") // best-effort goodbye
		time.Sleep(30 * time.Millisecond)
		c.ui.Restore()
		_ = c.tr.Close()
		fmt.Fprintln(os.Stderr) // land the shell prompt on a fresh line
	})
}

// ---- banner ------------------------------------------------------------

// style wraps text in ANSI SGR codes when the UI is interactive, and leaves it
// untouched when output is piped (tests, CI), so captured text stays plain.
type style bool

func (s style) wrap(code, t string) string {
	if !s {
		return t
	}
	return "\x1b[" + code + "m" + t + "\x1b[0m"
}

func (s style) bold(t string) string   { return s.wrap("1", t) }
func (s style) dim(t string) string    { return s.wrap("2", t) }
func (s style) green(t string) string  { return s.wrap("32", t) }
func (s style) yellow(t string) string { return s.wrap("33", t) }

// banner prints the welcome header. Unlike chat traffic it skips the
// timestamp/name columns: a boxed, labeled layout reads as a designed start
// screen rather than stray log lines. In stealth mode it stays a deliberately
// quiet one-liner instead.
func (c *Client) banner(private bool) {
	if c.stealth {
		c.ui.System(fmt.Sprintf("room %q as %q — type to chat, /quit to leave", c.room, c.nick))
		if !private {
			c.ui.System(`open room — anyone on this Wi-Fi can read it (-k "secret" makes it private)`)
		}
		if c.tr.Joined() == 0 && !c.broadcast {
			c.ui.System("warning: no multicast and -no-broadcast is set — you may not reach anyone")
		}
		return
	}

	s := style(c.ui.Interactive())
	width := c.ui.Width() - 2
	if width > 64 {
		width = 64
	}
	if width < 24 {
		width = 24
	}
	rule := s.dim(strings.Repeat("─", width))
	row := func(label, text string) {
		c.ui.Plain("  " + s.dim(fmt.Sprintf("%-5s", label)) + "  " + text)
	}

	c.ui.Plain("")
	c.ui.Plain("  " + s.bold("lanchat "+c.version) + s.dim(" — ephemeral encrypted LAN chat"))
	c.ui.Plain("  " + rule)
	if private {
		row("room", fmt.Sprintf("%q · ", c.room)+s.green("PRIVATE")+" — encrypted; passphrase holders only")
	} else {
		row("room", fmt.Sprintf("%q · ", c.room)+s.yellow("OPEN")+" — anyone on this Wi-Fi can read it")
	}
	row("you", fmt.Sprintf("%q · rename with /nick <name>", c.nick))
	row("send", "type a message and press Enter — the room sees it live")
	row("keys", "/help · /who · /clear · /quit · Ctrl-B = instant hide")
	row("saved", "nothing — messages exist only while the window is open")
	if c.tr.Joined() == 0 {
		if c.broadcast {
			row("note", s.yellow("couldn't join multicast — using broadcast fallback"))
		} else {
			row("note", s.yellow("no multicast + -no-broadcast — you may be unreachable"))
		}
	}
	c.ui.Plain("  " + rule)
	if !private {
		c.ui.Plain(s.dim(`  tip: add -k "secret" to go private — share room + passphrase`))
	}
	c.ui.Plain(s.dim("  waiting for messages…"))
	c.ui.Plain("")
}
