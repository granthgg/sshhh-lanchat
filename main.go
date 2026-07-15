// Command lanchat is an ephemeral, encrypted, serverless terminal chat for a
// single LAN. Everyone who runs it with the same room and passphrase is tuned
// to the same channel, like a walkie-talkie: messages are sent over UDP
// multicast, never stored, and only seen by sessions that are open at the
// time.
//
// Quick start:
//
//	lanchat                       # join the open "lobby" room
//	lanchat -r team -k hunter2     # a private, encrypted room
//	lanchat -n alice               # set your name
//
// In-session commands: /who /nick /me /clear /boss /help /quit
// Press Ctrl-B at any time to instantly hide the screen (boss key).
package main

import (
	"crypto/cipher"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/term"
)

const version = "2.1.1"

var debugOn = os.Getenv("CHAT_DEBUG") != ""

type app struct {
	id   string
	nick string
	room string

	tr  *transport
	ui  *UI
	ros *roster
	dd  *dedup
	seq seqGen

	aead cipher.AEAD
	quit chan struct{}
	done sync.Once
}

func main() {
	enableVirtualTerminal() // Windows: turn on ANSI + UTF-8; no-op elsewhere

	var (
		room    = flag.String("room", "lobby", "channel name to join")
		nick    = flag.String("nick", defaultNick(), "your display name")
		key     = flag.String("key", "", "room passphrase (prefer CHAT_KEY env or -ask)")
		ask     = flag.Bool("ask", false, "prompt for the passphrase without echoing it")
		iface   = flag.String("iface", "", "network interface to use (default: auto)")
		color   = flag.Bool("color", false, "colorize nicknames")
		stealth = flag.Bool("stealth", false, `disguise the prompt as a shell "$ " for a lower profile`)
		prompt  = flag.String("prompt", "", `input prompt (default "» ", or "$ " with -stealth)`)
		noBcast = flag.Bool("no-broadcast", false, "disable the UDP broadcast fallback")
		showVer = flag.Bool("version", false, "print version and exit")
	)
	// Short aliases.
	flag.StringVar(room, "r", *room, "alias for -room")
	flag.StringVar(nick, "n", *nick, "alias for -nick")
	flag.StringVar(key, "k", "", "alias for -key")
	flag.Usage = usage
	flag.Parse()

	if *showVer {
		fmt.Println("lanchat", version)
		return
	}

	promptStr := *prompt
	if promptStr == "" {
		if *stealth {
			promptStr = "$ "
		} else {
			promptStr = "» "
		}
	}

	*nick = sanitize(*nick)
	if *nick == "" {
		*nick = "anon"
	}
	*nick = clampRunes(*nick, 24)

	passphrase := resolveKey(*key, *ask)

	aeadImpl, err := buildAEAD(*room, passphrase)
	if err != nil {
		fatal("crypto init: " + err.Error())
	}

	tr, err := openTransport(*room, *iface, !*noBcast)
	if err != nil {
		fatal(err.Error() + "\n(is another program using the port, or is the network down?)")
	}

	a := &app{
		id:   newInstanceID(),
		nick: *nick,
		room: *room,
		tr:   tr,
		ui:   newUI(promptStr, *color),
		ros:  newRoster(),
		dd:   newDedup(),
		aead: aeadImpl,
		quit: make(chan struct{}),
	}

	a.installSignals()
	a.banner(passphrase != "", *stealth, tr)

	go a.recvLoop()
	go a.presenceLoop()
	go a.expireLoop()
	go a.ui.run()

	a.send(typeJoin, "")

	for line := range a.ui.lines {
		if a.handleLine(line) {
			break
		}
	}
	a.shutdown()
}

// handleLine processes one line of input; it returns true to quit.
func (a *app) handleLine(line string) bool {
	line = strings.TrimRight(line, "\r\n")
	if strings.TrimSpace(line) == "" {
		return false
	}
	if strings.HasPrefix(line, "/") {
		return a.command(line)
	}
	text := clampRunes(sanitize(line), maxBodyRunes)
	a.ui.chat(a.nick, text) // echo locally right away
	a.send(typeMsg, text)
	return false
}

func (a *app) command(line string) (quit bool) {
	fields := strings.Fields(line)
	switch fields[0] {
	case "/quit", "/exit", "/q":
		return true
	case "/nick", "/name":
		if len(fields) < 2 {
			a.ui.system("usage: /nick <name>")
			return false
		}
		newNick := clampRunes(sanitize(fields[1]), 24)
		if newNick == "" {
			a.ui.system("that name is empty after removing control characters")
			return false
		}
		old := a.nick
		a.nick = newNick
		a.ui.system(old + " is now " + a.nick)
		a.send(typeJoin, "") // re-announce under the new name
	case "/me":
		if len(fields) < 2 {
			a.ui.system("usage: /me <action>")
			return false
		}
		text := clampRunes(sanitize(strings.TrimSpace(line[len("/me"):])), maxBodyRunes)
		a.ui.action(a.nick, text)
		a.send(typeMe, text)
	case "/who", "/names":
		names := a.ros.list()
		if len(names) == 0 {
			a.ui.system("nobody else is here right now (just you)")
		} else {
			a.ui.system(fmt.Sprintf("here now (%d): %s", len(names), strings.Join(names, ", ")))
		}
	case "/clear":
		a.ui.clearScreen()
	case "/boss":
		a.ui.toggleBoss()
	case "/help", "/?":
		a.ui.system("commands: /who  /nick <name>  /me <action>  /clear  /boss  /quit   (Ctrl-B = instant hide)")
	default:
		a.ui.system("unknown command " + fields[0] + " — try /help")
	}
	return false
}

// ---- networking loops ------------------------------------------------------

func (a *app) send(t, body string) {
	m := Msg{T: t, ID: a.id, N: a.nick, S: a.seq.next(), B: body}
	raw, err := json.Marshal(m)
	if err != nil {
		return
	}
	frame, err := seal(a.aead, raw)
	if err != nil {
		return
	}
	a.tr.send(frame)
}

func (a *app) recvLoop() {
	buf := make([]byte, 2048)
	for {
		select {
		case <-a.quit:
			return
		default:
		}
		n, src, err := a.tr.read(buf)
		if err != nil {
			select {
			case <-a.quit:
				return
			default:
				continue
			}
		}
		if debugOn {
			fmt.Fprintf(os.Stderr, "[dbg] recv %d bytes from %v\n", n, src)
		}
		plaintext, err := open(a.aead, buf[:n])
		if err != nil {
			if debugOn {
				fmt.Fprintf(os.Stderr, "[dbg] decrypt failed (not our key?)\n")
			}
			continue // not our room / not decryptable
		}
		var m Msg
		if json.Unmarshal(plaintext, &m) != nil {
			continue
		}
		if m.ID == "" || m.ID == a.id {
			continue // ignore malformed and our own loopback echoes
		}
		if !a.dd.firstSeen(m.ID, m.S) {
			continue // duplicate from multicast+broadcast / multiple interfaces
		}

		m.N = sanitize(m.N)
		if m.N == "" {
			m.N = "?"
		}
		if a.ros.seen(m.ID, m.N) && m.T != typeLeave {
			a.ui.system("→ " + m.N + " is here")
		}

		switch m.T {
		case typeMsg:
			a.ui.chat(m.N, sanitize(m.B))
		case typeMe:
			a.ui.action(m.N, sanitize(m.B))
		case typeLeave:
			if nick, ok := a.ros.leave(m.ID); ok {
				a.ui.system("← " + nick + " left")
			}
		case typeJoin, typePing:
			// presence already recorded above
		}
	}
}

func (a *app) presenceLoop() {
	t := time.NewTicker(4 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-a.quit:
			return
		case <-t.C:
			a.send(typePing, "")
		}
	}
}

func (a *app) expireLoop() {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-a.quit:
			return
		case <-t.C:
			for _, nick := range a.ros.expire() {
				a.ui.system("← " + nick + " left (timed out)")
			}
		}
	}
}

// ---- lifecycle -------------------------------------------------------------

func (a *app) installSignals() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-ch
		a.shutdown()
	}()
}

func (a *app) shutdown() {
	a.done.Do(func() {
		close(a.quit)
		a.send(typeLeave, "") // best-effort goodbye
		time.Sleep(30 * time.Millisecond)
		a.ui.restore()
		_ = a.tr.close()
		fmt.Fprintln(os.Stderr) // land the shell prompt on a fresh line
		os.Exit(0)
	})
}

func (a *app) banner(private, stealth bool, tr *transport) {
	if stealth {
		a.ui.system(fmt.Sprintf("room %q as %q — type to chat, /quit to leave", a.room, a.nick))
	} else {
		a.ui.system("welcome to lanchat " + version)
		a.ui.system(fmt.Sprintf("you're in room %q as %q", a.room, a.nick))
		a.ui.system("→ type a message and press Enter to send it")
		a.ui.system("→ everyone on this Wi-Fi in the same room sees it in real time")
		a.ui.system("→ /help = commands   /nick = rename   /quit = leave   Ctrl-B = quick-hide")
	}
	if private {
		a.ui.system("this room is PRIVATE — encrypted, only people with the passphrase can read it")
	} else {
		a.ui.system(`this room is OPEN — anyone on this Wi-Fi can read it (add -k "secret" to make it private)`)
	}
	if tr.joined == 0 {
		a.ui.system("note: couldn't join multicast; using broadcast fallback (some networks block this)")
	}
	if !stealth {
		a.ui.system("nothing is saved — you only see messages sent while you're here. waiting…")
	}
}

// ---- helpers ---------------------------------------------------------------

func buildAEAD(room, passphrase string) (cipher.AEAD, error) {
	key, err := deriveKey(room, passphrase)
	if err != nil {
		return nil, err
	}
	return newAEAD(key)
}

// resolveKey picks the passphrase from, in order: -key flag, CHAT_KEY env, or
// an interactive no-echo prompt when -ask was given. Passing secrets on the
// command line is discouraged (they linger in shell history and process lists)
// so CHAT_KEY or -ask are preferred.
func resolveKey(flagKey string, ask bool) string {
	if flagKey != "" {
		return flagKey
	}
	if env := os.Getenv("CHAT_KEY"); env != "" {
		return env
	}
	if ask {
		fmt.Fprint(os.Stderr, "room passphrase: ")
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err == nil {
			return strings.TrimSpace(string(b))
		}
	}
	return "" // open room
}

func defaultNick() string {
	if u := os.Getenv("USER"); u != "" {
		return u
	}
	if u := os.Getenv("USERNAME"); u != "" { // Windows
		return u
	}
	if h, err := os.Hostname(); err == nil && h != "" {
		if i := strings.IndexByte(h, '.'); i > 0 {
			return h[:i]
		}
		return h
	}
	return "anon"
}

func usage() {
	fmt.Fprintf(os.Stderr, `lanchat %s — ephemeral encrypted terminal chat for your LAN

usage: lanchat [flags]

flags:
  -r, -room <name>   channel to join (default "lobby")
  -n, -nick <name>   your display name (default: your username)
  -k, -key  <phrase> room passphrase (prefer CHAT_KEY env or -ask)
      -ask           prompt for the passphrase without echoing it
      -iface <name>  pin a network interface (default: auto-detect)
      -color         colorize nicknames
      -stealth       disguise the prompt as a shell "$ "
      -prompt <str>  custom input prompt
      -no-broadcast  disable the broadcast fallback
      -version       print version

examples:
  lanchat                             join the open "lobby"
  lanchat -r team -ask                private room, prompt for passphrase
  CHAT_KEY=hunter2 lanchat -r team    private room via environment
  lanchat -n alice -color             named, with colored nicks

in session: type a message and press Enter to send
            /who  /nick <name>  /me <action>  /clear  /boss  /quit
            Ctrl-B hides the screen instantly (boss key)
`, version)
}

func fatal(msg string) {
	fmt.Fprintln(os.Stderr, "lanchat: "+msg)
	os.Exit(1)
}
