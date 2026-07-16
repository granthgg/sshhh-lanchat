// Package ui owns the terminal: a small raw-mode line editor and a thread-safe
// printer, plus the boss-key decoy screen.
package ui

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"

	"github.com/granthgg/sshhh-lanchat/internal/proto"
)

// UI runs a tiny line editor in raw mode so that messages arriving
// asynchronously never land in the middle of what you are typing: incoming
// lines are printed above a fixed input line, which is then redrawn. All writes
// to the terminal go through one mutex, so the reader goroutine (key input) and
// the network goroutine (incoming messages) can never interleave their output.
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
	logStyle bool // stealth: flat unstyled lines that read as log output
	prompt   string

	buf     []rune // current input line
	cur     int    // cursor position within buf
	history [][]rune
	histPos int // index into history while browsing; == len(history) when not

	hidden bool // boss-mode: incoming messages are suppressed
	missed int  // messages dropped while hidden

	// Lines delivers completed input lines to the main loop. Run closes it
	// when input ends.
	Lines chan string
}

// New returns a UI bound to stdin/stdout. If stdin is a terminal it switches
// to raw mode; otherwise it stays in line-buffered mode. logStyle selects the
// flat, unstyled "log output" line format used by stealth mode, instead of the
// decorated one (bold right-aligned nicks, dim timestamps, │ gutter).
func New(prompt string, color, logStyle bool) *UI {
	u := &UI{
		out:      os.Stdout,
		in:       bufio.NewReader(os.Stdin),
		fd:       int(os.Stdin.Fd()),
		color:    color,
		logStyle: logStyle,
		prompt:   prompt,
		Lines:    make(chan string, 16),
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

// Restore puts the terminal back the way we found it. Safe to call twice and
// from any goroutine.
func (u *UI) Restore() {
	u.mu.Lock()
	defer u.mu.Unlock()
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
//
// The input is windowed to the terminal width so it can never wrap: a wrapped
// input line would break the redraw ("\r" only returns to the start of the
// last physical row) and garble the screen on every keystroke. When the line
// outgrows the window, the view scrolls so the cursor stays visible — the
// same behavior as readline.
func (u *UI) drawLocked() {
	if !u.raw {
		return
	}
	avail := u.termWidth() - len([]rune(u.prompt)) - 1
	if avail < 8 {
		avail = 8
	}
	start := 0
	if u.cur > avail {
		start = u.cur - avail
	}
	end := start + avail
	if end > len(u.buf) {
		end = len(u.buf)
	}
	fmt.Fprint(u.out, "\r\x1b[K", u.prompt, string(u.buf[start:end]))
	if back := end - u.cur; back > 0 {
		fmt.Fprintf(u.out, "\x1b[%dD", back)
	}
}

// termWidth returns the current terminal width, defaulting to 80 when it
// cannot be determined. Queried per redraw, so window resizes self-correct on
// the next keystroke or message.
func (u *UI) termWidth() int {
	if w, _, err := term.GetSize(u.fd); err == nil && w > 0 {
		return w
	}
	return 80
}

func (u *UI) draw() {
	u.mu.Lock()
	u.drawLocked()
	u.mu.Unlock()
}

// ---- display lines ---------------------------------------------------------

// Plain prints one line verbatim, with no timestamp or name column. The
// startup banner uses it; chat traffic never does.
func (u *UI) Plain(line string) {
	u.emit(line)
}

// Interactive reports whether the UI drives a real terminal in raw mode
// (false when input is piped, in CI, or under tests). Callers use it to skip
// ANSI styling when the output may be captured as plain text.
func (u *UI) Interactive() bool { return u.raw }

// Width returns the terminal width, or 80 when it cannot be determined.
func (u *UI) Width() int {
	if u.raw {
		return u.termWidth()
	}
	return 80
}

// Chat prints a chat line from nick.
func (u *UI) Chat(nick, text string) {
	u.emit(u.format(nick, text, false))
}

// Action prints an emote, e.g. "* alice waves".
func (u *UI) Action(nick, text string) {
	u.emit(u.format("*", nick+" "+text, false))
}

// System prints a system notice.
func (u *UI) System(text string) {
	u.emit(u.format("·", text, true))
}

// nickCol is the fixed display width of the name column.
const nickCol = 12

// format lays out one display line.
//
// Decorated mode (the default) separates metadata from content so names read
// at a glance even without -color: the timestamp is dimmed, the nick is
// right-aligned into a fixed column and emphasized (bold, or its hash color
// with -color), and a dim │ gutter runs between names and messages, forming a
// clean vertical seam down the screen. System notices are dimmed whole so real
// chat stands out. Log style (stealth) keeps the flat, unstyled layout that
// passes for logger output.
func (u *UI) format(nick, text string, system bool) string {
	ts := time.Now().Format("15:04:05")
	name := proto.ClampRunes(nick, nickCol)
	if u.logStyle {
		pad := strings.Repeat(" ", nickCol-len([]rune(name)))
		return ts + " " + name + pad + " " + text
	}
	pad := strings.Repeat(" ", nickCol-len([]rune(name)))
	if system {
		return u.sgr("2", ts+" "+pad+name+" │ "+text)
	}
	styled := u.sgr("1", name)
	if u.color {
		styled = u.sgr(strconv.Itoa(colorFor(nick)), name)
	}
	return u.sgr("2", ts) + " " + pad + styled + " " + u.sgr("2", "│") + " " + text
}

// sgr wraps text in an ANSI SGR attribute when driving a real terminal, and
// leaves it untouched when output is piped, so captured text stays plain.
func (u *UI) sgr(code, t string) string {
	if !u.raw {
		return t
	}
	return "\x1b[" + code + "m" + t + "\x1b[0m"
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

// ToggleBoss hides the chat behind the decoy build output, or restores it. When
// restoring, it reports how many messages were suppressed while hidden.
func (u *UI) ToggleBoss() {
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

// Run reads input until EOF/quit and delivers completed lines on u.Lines, which
// it closes when input ends.
func (u *UI) Run() {
	defer close(u.Lines)
	if !u.raw {
		sc := bufio.NewScanner(u.in)
		sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
		for sc.Scan() {
			u.Lines <- strings.TrimRight(sc.Text(), "\r\n")
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
			u.ToggleBoss()
			continue
		}

		switch r {
		case '\r', '\n':
			u.commit()
		case 3: // Ctrl-C
			u.Lines <- "/quit"
			return
		case 4: // Ctrl-D on an empty line quits
			if len(u.buf) == 0 {
				u.Lines <- "/quit"
				return
			}
		case 127, 8: // Backspace
			u.backspace()
		case 21: // Ctrl-U — clear line
			u.setLine(nil)
		case 23: // Ctrl-W — delete previous word
			u.deleteWord()
		case 12: // Ctrl-L — clear screen
			u.ClearScreen()
		case 2: // Ctrl-B — instant boss key
			u.ToggleBoss()
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

// maxHistory bounds the input history so a long-lived session can't grow
// memory without limit.
const maxHistory = 200

func (u *UI) commit() {
	u.mu.Lock()
	line := string(u.buf)
	if t := strings.TrimSpace(line); t != "" {
		u.history = append(u.history, append([]rune(nil), u.buf...))
		if len(u.history) > maxHistory {
			u.history = u.history[len(u.history)-maxHistory:]
		}
	}
	u.histPos = len(u.history)
	u.buf = u.buf[:0]
	u.cur = 0
	u.drawLocked()
	u.mu.Unlock()
	u.Lines <- line
}

func (u *UI) insert(r rune) {
	u.mu.Lock()
	if len(u.buf) < proto.MaxBodyRunes {
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

// ClearScreen wipes the terminal and redraws the input line.
func (u *UI) ClearScreen() {
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
