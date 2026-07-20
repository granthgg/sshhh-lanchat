// Package chat wires the transport, crypto, roster and UI together into the
// running application. It is the composition root: cmd/lanchat parses flags,
// fills a Config, and calls Run.
package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/granthgg/sshhh-lanchat/internal/crypto"
	"github.com/granthgg/sshhh-lanchat/internal/proto"
	"github.com/granthgg/sshhh-lanchat/internal/roster"
	"github.com/granthgg/sshhh-lanchat/internal/transport"
	"github.com/granthgg/sshhh-lanchat/internal/ui"
	"github.com/granthgg/sshhh-lanchat/internal/update"
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
	Bell       bool   // terminal bell on new messages — the Dock-badge/taskbar signal
	Version    string // version string shown in the banner
	// NoUpdateCheck disables the one-shot GitHub Releases lookup that
	// notifies when a newer version exists. That request is the only
	// internet traffic lanchat ever makes; air-gapped users set this.
	NoUpdateCheck bool
}

// Client is one running chat session.
type Client struct {
	id   string
	room string

	// nick is written by /nick on the input goroutine and read by the
	// presence and receive goroutines; always use currentNick/setNick.
	nickMu sync.RWMutex
	nick   string

	tr  *transport.Transport
	ui  *ui.UI
	ros *roster.Roster
	dd  *proto.Dedup
	seq proto.SeqGen
	snz *snoozer

	aead crypto.AEAD
	quit chan struct{}
	done sync.Once

	stealth   bool
	broadcast bool
	bellOn    bool // mirrors the UI's bell gate, for /snooze feedback
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
		id:   proto.NewInstanceID(),
		nick: cfg.Nick,
		room: cfg.Room,
		tr:   tr,
		ui: ui.New(ui.Options{
			Prompt:   cfg.Prompt,
			Color:    cfg.Color,
			LogStyle: cfg.Stealth,
			// Stealth never bells: a beep draws exactly the attention the
			// mode exists to avoid.
			Bell: cfg.Bell && !cfg.Stealth,
		}),
		ros:       roster.New(),
		dd:        proto.NewDedup(),
		snz:       newSnoozer(),
		aead:      aead,
		quit:      make(chan struct{}),
		stealth:   cfg.Stealth,
		broadcast: cfg.Broadcast,
		bellOn:    cfg.Bell && !cfg.Stealth,
		version:   cfg.Version,
	}
	c.ui.Completer = c.completions

	c.installSignals()
	c.banner(cfg.Passphrase != "")

	go c.recvLoop()
	go c.presenceLoop()
	go c.expireLoop()
	go c.ui.Run()
	if !cfg.Stealth && !cfg.NoUpdateCheck {
		go c.updateNotice()
	}

	c.send(proto.TypeJoin, "")

	for line := range c.ui.Lines {
		if c.handleLine(line) {
			break
		}
	}
	c.shutdown()
	return nil
}

func (c *Client) currentNick() string {
	c.nickMu.RLock()
	defer c.nickMu.RUnlock()
	return c.nick
}

func (c *Client) setNick(n string) {
	c.nickMu.Lock()
	c.nick = n
	c.nickMu.Unlock()
}

// commandNames are the canonical slash commands offered by Tab completion
// (aliases like /exit and /names still work, they just aren't suggested).
var commandNames = []string{"/boss", "/clear", "/help", "/me", "/nick", "/quit", "/snooze", "/who"}

// completions is the UI's Tab-completion source: slash commands at the start
// of the line, and the nicknames currently in the room anywhere.
func (c *Client) completions(word string, lineStart bool) []string {
	if strings.HasPrefix(word, "/") {
		if !lineStart {
			return nil
		}
		return filterPrefixFold(commandNames, word)
	}
	return filterPrefixFold(c.ros.List(), word)
}

// filterPrefixFold returns the entries of list that start with prefix,
// compared case-insensitively and rune-safely.
func filterPrefixFold(list []string, prefix string) []string {
	pr := []rune(prefix)
	var out []string
	for _, s := range list {
		sr := []rune(s)
		if len(sr) >= len(pr) && strings.EqualFold(string(sr[:len(pr)]), prefix) {
			out = append(out, s)
		}
	}
	return out
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
	c.ui.Chat(c.id, c.currentNick(), text, false, false) // echo locally, exactly what will be sent
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
		old := c.currentNick()
		c.setNick(newNick)
		c.ui.System(old + " is now " + newNick)
		c.send(proto.TypeJoin, "") // re-announce under the new name
	case "/me":
		if len(fields) < 2 {
			c.ui.System("usage: /me <action>")
			return false
		}
		text := clampBody(strings.TrimSpace(line[len("/me"):]))
		c.ui.Action(c.id, c.currentNick(), text, false, false)
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
	case "/snooze":
		c.snoozeCmd(fields)
	case "/help", "/?":
		c.ui.System("commands: /who  /nick <name>  /me <action>  /snooze [time|off]  /clear  /boss  /quit")
		c.ui.System("keys: Tab completes names & commands (repeat to cycle) · Ctrl-B = instant hide")
	default:
		c.ui.System("unknown command " + fields[0] + " — try /help")
	}
	return false
}

// defaultSnooze is how long a bare /snooze quiets the message bell.
const defaultSnooze = 15 * time.Minute

// maxSnooze caps /snooze so a typo ("/snooze 1000h") can't silence the bell
// effectively forever; -no-bell is the way to opt out.
const maxSnooze = 24 * time.Hour

// snoozeCmd implements /snooze [duration|off]: it pauses the message bell for
// the given time (default 15m) without touching the in-terminal message flow —
// you keep chatting, your terminal stops calling for attention.
func (c *Client) snoozeCmd(fields []string) {
	if !c.bellOn {
		c.ui.System("the message bell is already off in this session (-no-bell or -stealth)")
		return
	}
	if len(fields) >= 2 && strings.EqualFold(fields[1], "off") {
		if c.snz.remaining() == 0 {
			c.ui.System("the message bell wasn't snoozed")
			return
		}
		c.snz.clear()
		c.ui.System("snooze lifted — the message bell is back on")
		return
	}
	d := defaultSnooze
	if len(fields) >= 2 {
		var err error
		if d, err = parseSnooze(fields[1]); err != nil {
			c.ui.System("usage: /snooze [minutes | 10m | 1h30m | off]   (default 15m)")
			return
		}
	}
	c.snz.set(d)
	c.ui.System("message bell snoozed for " + formatDur(d) + " — /snooze off to undo")
}

// parseSnooze turns a /snooze argument into a duration. A bare number means
// minutes ("/snooze 10"); anything else uses Go duration syntax ("90s",
// "1h30m"). Non-positive durations are rejected; long ones clamp to maxSnooze
// (the confirmation echoes the clamped value, so nothing is silent).
func parseSnooze(arg string) (time.Duration, error) {
	d, err := time.ParseDuration(arg)
	if err != nil {
		mins, convErr := strconv.Atoi(arg)
		if convErr != nil {
			return 0, err
		}
		d = time.Duration(mins) * time.Minute
	}
	if d <= 0 {
		return 0, errors.New("duration must be positive")
	}
	if d > maxSnooze {
		d = maxSnooze
	}
	return d, nil
}

// formatDur renders a duration compactly ("15m", "1h30m", "45s") for system
// notices; time.Duration.String would print "15m0s".
func formatDur(d time.Duration) string {
	d = d.Round(time.Second)
	h := int(d / time.Hour)
	m := int(d % time.Hour / time.Minute)
	s := int(d % time.Minute / time.Second)
	switch {
	case h > 0 && m > 0:
		return fmt.Sprintf("%dh%dm", h, m)
	case h > 0:
		return fmt.Sprintf("%dh", h)
	case m > 0 && s > 0:
		return fmt.Sprintf("%dm%ds", m, s)
	case m > 0:
		return fmt.Sprintf("%dm", m)
	default:
		return fmt.Sprintf("%ds", s)
	}
}

// attend reports whether an arriving message may ring the terminal bell right
// now. The bell is more than a beep: terminals surface a background bell as a
// Dock badge/bounce (macOS Terminal), a tab marker (iTerm2, tmux) or a
// taskbar flash (Windows Terminal) — the "you have a message" signal when the
// window isn't visible. /snooze pauses it; the UI itself drops the bell in
// stealth (-no-bell) and while boss-hidden.
func (c *Client) attend() bool {
	return c.snz.remaining() == 0
}

// snoozer holds the /snooze state: a deadline before which arriving messages
// must not ring the attention bell. Thread-safe because /snooze runs on the
// input goroutine while attend is called from the receive goroutine.
type snoozer struct {
	mu    sync.Mutex
	until time.Time
	now   func() time.Time // clock; swapped in tests
}

func newSnoozer() *snoozer { return &snoozer{now: time.Now} }

// set silences the bell for d from now, replacing (not extending) any snooze
// already running.
func (s *snoozer) set(d time.Duration) {
	s.mu.Lock()
	s.until = s.now().Add(d)
	s.mu.Unlock()
}

// clear lifts an active snooze immediately.
func (s *snoozer) clear() {
	s.mu.Lock()
	s.until = time.Time{}
	s.mu.Unlock()
}

// remaining reports how much of an active snooze is left, or zero when the
// bell is not snoozed.
func (s *snoozer) remaining() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r := s.until.Sub(s.now()); r > 0 {
		return r
	}
	return 0
}

// ---- networking loops ------------------------------------------------------

func (c *Client) send(t, body string) {
	m := proto.Msg{T: t, ID: c.id, N: c.currentNick(), S: c.seq.Next(), B: body}
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
			body := proto.ClampRunes(proto.Sanitize(m.B), proto.MaxBodyRunes)
			c.ui.Chat(m.ID, m.N, body, proto.Mentions(body, c.currentNick()), c.attend())
		case proto.TypeMe:
			body := proto.ClampRunes(proto.Sanitize(m.B), proto.MaxBodyRunes)
			c.ui.Action(m.ID, m.N, body, proto.Mentions(body, c.currentNick()), c.attend())
		case proto.TypeLeave:
			if nick, ok := c.ros.Leave(m.ID); ok {
				c.ui.ForgetColor(m.ID) // free the color for the next joiner
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
			for _, d := range c.ros.Expire() {
				c.ui.ForgetColor(d.ID)
				c.ui.System("← " + d.Nick + " left (timed out)")
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
		c.ui.System(fmt.Sprintf("room %q as %q — type to chat, /quit to leave", c.room, c.currentNick()))
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
	row("you", fmt.Sprintf("%q · rename with /nick <name>", c.currentNick()))
	row("send", "type a message and press Enter — the room sees it live")
	row("keys", "/help · /quit · Tab = complete · Ctrl-B = instant hide")
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

// ---- update notice ----------------------------------------------------------

// updateNotice checks GitHub Releases once per session and, if a newer stable
// release exists, prints a dim system notice with the one-line installer.
// It is a courtesy, not a phone-home: a single GET to a public endpoint with
// no identifying data, bounded to a few seconds, and every failure — offline,
// DNS, timeout, rate limit — is silently dropped. Stealth mode and
// -no-update-check never start this goroutine.
func (c *Client) updateNotice() {
	// Let the banner settle and the join land first; an update hint should
	// never be the first thing on screen. If the session ends meanwhile,
	// don't bother at all.
	select {
	case <-time.After(2 * time.Second):
	case <-c.quit:
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Tear the request down the instant the session ends — the notice has
	// nowhere to go after that. AfterFunc's returned stop call also drops
	// the registration so it can't fire after cancel().
	go func() {
		select {
		case <-c.quit:
			cancel()
		case <-ctx.Done():
		}
	}()

	latest, err := update.Latest(ctx, update.LatestURL)
	if err != nil {
		if debugOn {
			fmt.Fprintf(os.Stderr, "update check: %v\n", err)
		}
		return
	}
	if !update.Newer(c.version, latest) {
		return
	}

	// The session may have ended while the request was in flight.
	select {
	case <-c.quit:
		return
	default:
	}

	s := style(c.ui.Interactive())
	c.ui.System(s.yellow("lanchat "+latest+" is out") + s.dim(" — you're on "+c.version))
	c.ui.System(s.dim("update: " + update.Command()))
}
