// Command lanchat is an ephemeral, encrypted, serverless terminal chat for a
// single LAN. Everyone who runs it with the same room and passphrase is tuned
// to the same channel, like a walkie-talkie: messages are sent over UDP
// multicast, never stored, and only seen by sessions that are open at the time.
//
// Quick start:
//
//	lanchat                        # join the open "lobby" room
//	lanchat -r team -k hunter2     # a private, encrypted room
//	lanchat -n alice               # set your name
//
// In-session commands: /who /nick /me /clear /boss /help /quit
// Press Ctrl-B at any time to instantly hide the screen (boss key).
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/granthgg/sshhh-lanchat/internal/chat"
	"github.com/granthgg/sshhh-lanchat/internal/proto"
	"github.com/granthgg/sshhh-lanchat/internal/ui"
)

const version = "2.3.0"

func main() {
	ui.EnableVirtualTerminal() // Windows: turn on ANSI + UTF-8; no-op elsewhere

	var (
		room    = flag.String("room", "lobby", "channel name to join")
		nick    = flag.String("nick", defaultNick(), "your display name")
		key     = flag.String("key", "", "room passphrase (prefer CHAT_KEY env or -ask)")
		ask     = flag.Bool("ask", false, "prompt for the passphrase without echoing it")
		iface   = flag.String("iface", "", "network interface to use (default: auto)")
		ttl     = flag.Int("ttl", 1, "multicast TTL: 1 stays on the local segment; raise only on LANs that route multicast between subnets")
		color   = flag.Bool("color", false, "colorize nicknames")
		stealth = flag.Bool("stealth", false, `disguise the prompt as a shell "$ " for a lower profile`)
		prompt  = flag.String("prompt", "", `input prompt (default "» ", or "$ " with -stealth)`)
		noBcast = flag.Bool("no-broadcast", false, "disable the UDP broadcast fallback")
		noBell  = flag.Bool("no-bell", false, "don't ring the terminal bell when your name is mentioned")
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

	nickStr := proto.Sanitize(*nick)
	if nickStr == "" {
		nickStr = "anon"
	}
	nickStr = proto.ClampRunes(nickStr, 24)

	passphrase, err := resolveKey(*key, *ask)
	if err != nil {
		fatal(err.Error())
	}

	err = chat.Run(chat.Config{
		Room:       *room,
		Nick:       nickStr,
		Passphrase: passphrase,
		Iface:      *iface,
		Color:      *color,
		Stealth:    *stealth,
		Prompt:     promptStr,
		Broadcast:  !*noBcast,
		TTL:        *ttl,
		Bell:       !*noBell,
		Version:    version,
	})
	if err != nil {
		fatal(err.Error())
	}
}

// resolveKey picks the passphrase from, in order: -key flag, CHAT_KEY env, or an
// interactive no-echo prompt when -ask was given. Passing secrets on the command
// line is discouraged (they linger in shell history and process lists) so
// CHAT_KEY or -ask are preferred.
//
// A failed -ask is an error, never a fallback: the user asked for a private
// room, and silently opening an unencrypted one instead would betray that.
func resolveKey(flagKey string, ask bool) (string, error) {
	if flagKey != "" {
		return flagKey, nil
	}
	if env := os.Getenv("CHAT_KEY"); env != "" {
		return env, nil
	}
	if ask {
		fmt.Fprint(os.Stderr, "room passphrase: ")
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", fmt.Errorf("could not read the passphrase (-ask needs an interactive terminal): %w", err)
		}
		return strings.TrimSpace(string(b)), nil
	}
	return "", nil // open room
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
      -ttl <n>       multicast TTL; raise above 1 only on LANs that route
                     multicast between subnets (default 1)
      -color         colorize nicknames
      -stealth       disguise the prompt as a shell "$ "
      -prompt <str>  custom input prompt
      -no-broadcast  disable the broadcast fallback
      -no-bell       don't ring the bell when your name is mentioned
      -version       print version

examples:
  lanchat                             join the open "lobby"
  lanchat -r team -ask                private room, prompt for passphrase
  CHAT_KEY=hunter2 lanchat -r team    private room via environment
  lanchat -n alice -color             named, with colored nicks

in session: type a message and press Enter to send
            /who  /nick <name>  /me <action>  /clear  /boss  /quit
            Tab completes names & commands; Ctrl-B hides the screen (boss key)
`, version)
}

func fatal(msg string) {
	fmt.Fprintln(os.Stderr, "lanchat: "+msg)
	os.Exit(1)
}
