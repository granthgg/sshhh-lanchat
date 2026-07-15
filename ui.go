package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"
)

// UI owns the terminal. It runs a tiny line editor in raw mode so that
// messages arriving asynchronously never land in the middle of what you are
// typing: incoming lines are printed above a fixed input line, which is then
// redrawn. All writes to the terminal go through one mutex, so the reader
// goroutine (key input) and the network goroutine (incoming messages) can
// never interleave their output.
//
// If stdin is not a terminal (piped input, CI, tests), it degrades to plain
// line-buffered mode with no cursor control.
type UI struct {
	mu       sync.Mutex
	out      *os.File
	in       *bufio.Reader
	fd       int
	raw      bool
	oldState *term.State
	color    bool
	prompt   string

	buf     []rune // current input line
	cur     int    // cursor position within buf
	history [][]rune
	histPos int // index into history while browsing; == len(history) when not

	hidden bool // boss-mode: incoming messages are suppressed
	missed int  // messages dropped while hidden

	lines chan string // completed input lines delivered to the main loop
}

func newUI(prompt string, color bool) *UI {
	u := &UI{
		out:    os.Stdout,
		in:     bufio.NewReader(os.Stdin),
		fd:     int(os.Stdin.Fd()),
		color:  color,
		prompt: prompt,
		lines:  make(chan string, 16),
	}
	u.histPos = 0
	if term.IsTerminal(u.fd) {
		if st, err := term.MakeRaw(u.fd); err == nil {
			u.raw = true
			u.oldState = st
		}
	}
	return u
}

// restore puts the terminal back the way we found it. Safe to call twice.
func (u *UI) restore() {
	if u.raw && u.oldState != nil {
		_ = term.Restore(u.fd, u.oldState)
		u.oldState = nil
	}
}

// ---- output ----------------------------------------------------------------

// emit prints one finished display line above the input line. In boss mode it
// is swallowed (and counted) so nothing pops up over the decoy screen.
func (u *UI) emit(line string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.hidden {
		u.missed++
		return
	}
	if !u.raw {
		fmt.Fprintln(u.out, line)
		return
	}
	fmt.Fprint(u.out, "\r\x1b[K", line, "\r\n")
	u.drawLocked()
}

// drawLocked repaints the input line and positions the cursor. Caller holds mu.
func (u *UI) drawLocked() {
	if !u.raw {
		return
	}
	fmt.Fprint(u.out, "\r\x1b[K", u.prompt, string(u.buf))
	if back := len(u.buf) - u.cur; back > 0 {
		fmt.Fprintf(u.out, "\x1b[%dD", back)
	}
}

func (u *UI) draw() {
	u.mu.Lock()
	u.drawLocked()
	u.mu.Unlock()
}

// timestamp/format helpers ---------------------------------------------------

func (u *UI) chat(nick, text string) {
	u.emit(u.format(nick, text, false))
}

func (u *UI) action(nick, text string) {
	u.emit(u.format("*", nick+" "+text, false))
}

func (u *UI) system(text string) {
	u.emit(u.format("·", text, true))
}

func (u *UI) format(nick, text string, system bool) string {
	ts := time.Now().Format("15:04:05")
	name := fmt.Sprintf("%-12s", clampRunes(nick, 12))
	if u.color && !system {
		name = fmt.Sprintf("\x1b[%dm%s\x1b[0m", colorFor(nick), name)
	}
	return ts + " " + name + " " + text
}

func colorFor(s string) int {
	var h uint32 = 2166136261
	for _, c := range s {
		h = (h ^ uint32(c)) * 16777619
	}
	palette := []int{31, 32, 33, 34, 35, 36, 91, 92, 93, 94, 95, 96}
	return palette[int(h%uint32(len(palette)))]
}

// ---- boss mode -------------------------------------------------------------

func (u *UI) toggleBoss() {
	u.mu.Lock()
	defer u.mu.Unlock()
	if !u.hidden {
		u.hidden = true
		u.missed = 0
		fmt.Fprint(u.out, "\x1b[2J\x1b[H")
		printDecoy(u.out, u.raw)
		return
	}
	u.hidden = false
	fmt.Fprint(u.out, "\x1b[2J\x1b[H")
	if u.missed > 0 {
		note := fmt.Sprintf("(%d message(s) arrived while hidden and were not shown)", u.missed)
		fmt.Fprint(u.out, u.format("·", note, true), lineEnd(u.raw))
	}
	u.drawLocked()
}

// ---- input -----------------------------------------------------------------

// run reads input until EOF/quit and delivers completed lines on u.lines,
// which it closes when input ends.
func (u *UI) run() {
	defer close(u.lines)
	if !u.raw {
		sc := bufio.NewScanner(u.in)
		sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
		for sc.Scan() {
			u.lines <- strings.TrimRight(sc.Text(), "\r\n")
		}
		return
	}

	u.draw()
	for {
		r, _, err := u.in.ReadRune()
		if err != nil {
			return
		}

		// Any keystroke while hidden restores the screen and is swallowed.
		if u.isHidden() {
			u.toggleBoss()
			continue
		}

		switch r {
		case '\r', '\n':
			u.commit()
		case 3: // Ctrl-C
			u.lines <- "/quit"
			return
		case 4: // Ctrl-D on an empty line quits
			if len(u.buf) == 0 {
				u.lines <- "/quit"
				return
			}
		case 127, 8: // Backspace
			u.backspace()
		case 21: // Ctrl-U — clear line
			u.setLine(nil)
		case 23: // Ctrl-W — delete previous word
			u.deleteWord()
		case 12: // Ctrl-L — clear screen
			u.clearScreen()
		case 2: // Ctrl-B — instant boss key
			u.toggleBoss()
		case 1: // Ctrl-A — home
			u.moveTo(0)
		case 5: // Ctrl-E — end
			u.moveTo(len(u.buf))
		case 27: // ESC — arrow keys and friends
			u.handleEscape()
		default:
			if r >= 0x20 && r != 0x7f {
				u.insert(r)
			}
		}
	}
}

func (u *UI) isHidden() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.hidden
}

func (u *UI) commit() {
	u.mu.Lock()
	line := string(u.buf)
	if t := strings.TrimSpace(line); t != "" {
		u.history = append(u.history, append([]rune(nil), u.buf...))
	}
	u.histPos = len(u.history)
	u.buf = u.buf[:0]
	u.cur = 0
	u.drawLocked()
	u.mu.Unlock()
	u.lines <- line
}

func (u *UI) insert(r rune) {
	u.mu.Lock()
	if len(u.buf) < maxBodyRunes {
		u.buf = append(u.buf, 0)
		copy(u.buf[u.cur+1:], u.buf[u.cur:])
		u.buf[u.cur] = r
		u.cur++
	}
	u.drawLocked()
	u.mu.Unlock()
}

func (u *UI) backspace() {
	u.mu.Lock()
	if u.cur > 0 {
		u.buf = append(u.buf[:u.cur-1], u.buf[u.cur:]...)
		u.cur--
	}
	u.drawLocked()
	u.mu.Unlock()
}

func (u *UI) deleteWord() {
	u.mu.Lock()
	i := u.cur
	for i > 0 && u.buf[i-1] == ' ' {
		i--
	}
	for i > 0 && u.buf[i-1] != ' ' {
		i--
	}
	u.buf = append(u.buf[:i], u.buf[u.cur:]...)
	u.cur = i
	u.drawLocked()
	u.mu.Unlock()
}

func (u *UI) setLine(rs []rune) {
	u.mu.Lock()
	u.buf = append(u.buf[:0], rs...)
	u.cur = len(u.buf)
	u.drawLocked()
	u.mu.Unlock()
}

func (u *UI) moveTo(pos int) {
	u.mu.Lock()
	if pos < 0 {
		pos = 0
	}
	if pos > len(u.buf) {
		pos = len(u.buf)
	}
	u.cur = pos
	u.drawLocked()
	u.mu.Unlock()
}

func (u *UI) clearScreen() {
	u.mu.Lock()
	fmt.Fprint(u.out, "\x1b[2J\x1b[H")
	u.drawLocked()
	u.mu.Unlock()
}

// handleEscape parses the common CSI sequences for arrow/Home/End/Delete.
func (u *UI) handleEscape() {
	b1, _, err := u.in.ReadRune()
	if err != nil {
		return
	}
	if b1 != '[' && b1 != 'O' {
		return
	}
	b2, _, err := u.in.ReadRune()
	if err != nil {
		return
	}
	switch b2 {
	case 'A': // up
		u.historyPrev()
	case 'B': // down
		u.historyNext()
	case 'C': // right
		u.moveTo(u.cur + 1)
	case 'D': // left
		u.moveTo(u.cur - 1)
	case 'H': // home
		u.moveTo(0)
	case 'F': // end
		u.moveTo(len(u.buf))
	case '1', '3', '4', '7', '8': // extended: consume up to the trailing '~'
		for {
			r, _, err := u.in.ReadRune()
			if err != nil || r == '~' {
				break
			}
		}
		switch b2 {
		case '1', '7':
			u.moveTo(0)
		case '4', '8':
			u.moveTo(len(u.buf))
		case '3': // delete key
			u.mu.Lock()
			if u.cur < len(u.buf) {
				u.buf = append(u.buf[:u.cur], u.buf[u.cur+1:]...)
			}
			u.drawLocked()
			u.mu.Unlock()
		}
	}
}

func (u *UI) historyPrev() {
	u.mu.Lock()
	if u.histPos > 0 {
		u.histPos--
		u.buf = append(u.buf[:0], u.history[u.histPos]...)
		u.cur = len(u.buf)
	}
	u.drawLocked()
	u.mu.Unlock()
}

func (u *UI) historyNext() {
	u.mu.Lock()
	if u.histPos < len(u.history) {
		u.histPos++
		if u.histPos == len(u.history) {
			u.buf = u.buf[:0]
		} else {
			u.buf = append(u.buf[:0], u.history[u.histPos]...)
		}
		u.cur = len(u.buf)
	}
	u.drawLocked()
	u.mu.Unlock()
}

func lineEnd(raw bool) string {
	if raw {
		return "\r\n"
	}
	return "\n"
}
