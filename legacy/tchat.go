//go:build ignore

// Preserved original single-file prototype (TCP star-relay model).
// Kept for reference; excluded from the build via the ignore tag above.
// The current implementation lives in the repository root (UDP multicast).

// tchat: a zero-dependency, ephemeral terminal chat for a single LAN.
//
// Model:
//   - Run with no arguments and the first instance on the network becomes the
//     host (a lightweight relay). Later instances discover it over UDP and join.
//   - The host holds no history. It fans out each line to connected peers and
//     forgets it. Nothing is written to disk. You only see messages that arrive
//     while your session is open.
//   - If the network blocks UDP discovery, use explicit host / join by IP.
//
// Commands inside a session:
//   /name <x>   set your display name
//   /boss       clear the screen and print fake build output
//   /help       list commands
//   /quit       leave
//
// Build: go build -o chat tchat.go   (chat.exe on Windows)

package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	discoveryPort = 48710       // UDP port used only to announce/find the host
	chatPort      = 48711       // TCP port the host relay listens on
	beaconPrefix  = "TCHAT1|"   // beacon payload prefix, followed by the chat port
)

var nick = defaultNick()

func defaultNick() string {
	if h, err := os.Hostname(); err == nil && h != "" {
		if i := strings.IndexByte(h, '.'); i > 0 {
			return h[:i]
		}
		return h
	}
	return "anon"
}

func main() {
	args := os.Args[1:]

	switch {
	case len(args) >= 1 && args[0] == "host":
		startHost()
		printJoinHint()
		runClient(fmt.Sprintf("127.0.0.1:%d", chatPort))

	case len(args) >= 2 && args[0] == "join":
		addr := args[1]
		if !strings.Contains(addr, ":") {
			addr = fmt.Sprintf("%s:%d", addr, chatPort)
		}
		runClient(addr)

	default:
		// Auto mode: try to find an existing session, otherwise start one.
		if addr, ok := discover(1500 * time.Millisecond); ok {
			fmt.Fprintln(os.Stderr, sysline("connected to existing session"))
			runClient(addr)
			return
		}
		fmt.Fprintln(os.Stderr, sysline("no session found, starting one"))
		startHost()
		printJoinHint()
		runClient(fmt.Sprintf("127.0.0.1:%d", chatPort))
	}
}

// ---- host relay ----

type relay struct {
	mu    sync.Mutex
	conns map[net.Conn]struct{}
}

func startHost() {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", chatPort))
	if err != nil {
		fmt.Fprintln(os.Stderr, sysline("could not start session: "+err.Error()))
		os.Exit(1)
	}
	r := &relay{conns: make(map[net.Conn]struct{})}

	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			r.add(c)
			go r.handle(c)
		}
	}()

	go beacon() // best effort auto-discovery; safe to fail
}

func (r *relay) add(c net.Conn) {
	r.mu.Lock()
	r.conns[c] = struct{}{}
	r.mu.Unlock()
}

func (r *relay) remove(c net.Conn) {
	r.mu.Lock()
	delete(r.conns, c)
	r.mu.Unlock()
	c.Close()
}

func (r *relay) handle(c net.Conn) {
	defer r.remove(c)
	sc := bufio.NewScanner(c)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		r.broadcast(sc.Text(), c)
	}
}

func (r *relay) broadcast(line string, from net.Conn) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for c := range r.conns {
		if c == from { // sender already printed its own line locally
			continue
		}
		fmt.Fprintln(c, line)
	}
}

// ---- client ----

func runClient(addr string) {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		fmt.Fprintln(os.Stderr, sysline("could not connect: "+err.Error()))
		os.Exit(1)
	}
	defer conn.Close()

	go func() {
		sc := bufio.NewScanner(conn)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			name, text := parseWire(sc.Text())
			fmt.Println(logLine(name, text))
		}
		fmt.Fprintln(os.Stderr, sysline("session ended"))
		os.Exit(0)
	}()

	fmt.Fprintln(os.Stderr, sysline("ready. /help for commands, /boss to hide, /quit to exit"))

	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for in.Scan() {
		line := strings.TrimRight(in.Text(), "\r\n")
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "/") {
			if handleCommand(line) {
				return
			}
			continue
		}
		fmt.Println(logLine(nick, line)) // echo own message locally
		fmt.Fprintf(conn, "%s\t%s\n", nick, line)
	}
}

func handleCommand(line string) (quit bool) {
	fields := strings.Fields(line)
	switch fields[0] {
	case "/quit", "/exit":
		return true
	case "/name", "/nick":
		if len(fields) >= 2 {
			nick = fields[1]
			fmt.Println(sysline("name set to " + nick))
		} else {
			fmt.Println(sysline("usage: /name <yourname>"))
		}
	case "/boss", "/clear":
		bossScreen()
	case "/help":
		fmt.Println(sysline("commands: /name <x>   /boss   /quit"))
	default:
		fmt.Println(sysline("unknown command, try /help"))
	}
	return false
}

// ---- discovery ----

func beacon() {
	dst := &net.UDPAddr{IP: net.IPv4bcast, Port: discoveryPort}
	conn, err := net.DialUDP("udp4", nil, dst)
	if err != nil {
		return // discovery unavailable; join-by-ip still works
	}
	defer conn.Close()
	payload := []byte(fmt.Sprintf("%s%d", beaconPrefix, chatPort))
	for {
		conn.Write(payload) // ignore errors on locked-down networks
		time.Sleep(time.Second)
	}
}

func discover(timeout time.Duration) (string, bool) {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: discoveryPort})
	if err != nil {
		return "", false
	}
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(timeout))
	buf := make([]byte, 512)
	for {
		n, src, err := conn.ReadFromUDP(buf)
		if err != nil {
			return "", false
		}
		msg := string(buf[:n])
		if strings.HasPrefix(msg, beaconPrefix) {
			port, perr := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(msg, beaconPrefix)))
			if perr != nil {
				port = chatPort
			}
			return fmt.Sprintf("%s:%d", src.IP.String(), port), true
		}
	}
}

// ---- helpers ----

func parseWire(s string) (name, text string) {
	if i := strings.IndexByte(s, '\t'); i >= 0 {
		return s[:i], s[i+1:]
	}
	return "peer", s
}

func logLine(name, text string) string {
	return fmt.Sprintf("%s  %-10s %s", time.Now().Format("2006-01-02 15:04:05"), name, text)
}

func sysline(msg string) string {
	return fmt.Sprintf("%s  %-10s %s", time.Now().Format("2006-01-02 15:04:05"), "system", msg)
}

func localIPs() []string {
	var out []string
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return out
	}
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ip4 := ipnet.IP.To4(); ip4 != nil {
				out = append(out, ip4.String())
			}
		}
	}
	return out
}

func printJoinHint() {
	ips := localIPs()
	if len(ips) == 0 {
		fmt.Fprintln(os.Stderr, sysline("share your LAN IP; others run: chat join <your-ip>"))
		return
	}
	for _, ip := range ips {
		fmt.Fprintln(os.Stderr, sysline("if auto-connect fails, others run: chat join "+ip))
	}
}

func bossScreen() {
	fmt.Print(strings.Repeat("\n", 50))
	for _, l := range []string{
		"$ npm run build",
		"",
		"> app@1.0.0 build",
		"> next build",
		"",
		"   Next.js 15.1.0",
		"",
		"   Creating an optimized production build ...",
		"   Compiled successfully",
		"   Linting and checking validity of types ...",
		"   Collecting page data",
		"   Generating static pages (12/12)",
		"   Finalizing page optimization ...",
		"",
		"Route (app)                       Size     First Load JS",
		"/                                 1.2 kB          92 kB",
		"/dashboard                        3.4 kB          97 kB",
		"/settings                         2.1 kB          95 kB",
		"",
		"Build completed in 8.42s",
		"",
	} {
		fmt.Println(l)
	}
}
