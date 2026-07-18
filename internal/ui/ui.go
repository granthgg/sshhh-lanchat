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
	bell     bool // ring the terminal bell when a line asks for it (mentions)
	prompt   string

	buf     []rune // current input line
	cur     int    // cursor position within buf
	history [][]rune
	histPos int // index into history while browsing; == len(history) when not

	hidden        bool     // boss-mode: incoming messages are held, not shown
	hiddenBuf     []string // lines held while hidden, replayed on restore
	hiddenDropped int      // lines beyond the buffer cap, oldest first

	// Speaker colors (-color): each speaker key (sender instance id) holds
	// one palette slot for as long as they're present, so two people never
	// share a color while free slots remain — even two people using the same
	// nickname. Guarded by mu.
	colorAssign map[string]int // key → ANSI color code
	colorUse    map[int]int    // ANSI color code → current holders

	// Tab-completion session state; owned by the Run goroutine.
	compActive bool
	compCands  []string
	compIdx    int
	compStart  int // rune index where the completed word begins
	compEnd    int // rune index just past the inserted completion
	compSuffix string

	// Completer, when set, returns completion candidates for the word before
	// the cursor. lineStart reports whether the word begins the line. Set it
	// before Run.
	Completer func(word string, lineStart bool) []string

	// Lines delivers completed input lines to the main loop. Run closes it
	// when input ends.
	Lines chan string
}

// Options configures New.
type Options struct {
	Prompt   string
	Color    bool // colorize nicknames
	LogStyle bool // stealth: flat unstyled lines that read as log output
	Bell     bool // ring the terminal bell on bell-flagged lines
}

// New returns a UI bound to stdin/stdout. If stdin is a terminal it switches
// to raw mode; otherwise it stays in line-buffered mode.
func New(o Options) *UI {
	u := &UI{
		out:         os.Stdout,
		in:          bufio.NewReader(os.Stdin),
		fd:          int(os.Stdin.Fd()),
		color:       o.Color,
		logStyle:    o.LogStyle,
		bell:        o.Bell,
		prompt:      o.Prompt,
		colorAssign: make(map[string]int),
		colorUse:    make(map[int]int),
		Lines:       make(chan string, 16),
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

// hiddenBufCap bounds how many lines boss mode holds for replay. Beyond it the
// oldest lines are dropped (and counted), so an hours-long hide can't grow
// memory without limit. Everything stays in memory only — never on disk.
const hiddenBufCap = 500

// emit prints one finished display line above the input line. In boss mode
// nothing may appear over the decoy screen, so the line is held (bounded) and
// replayed when the user comes back. bell asks for a terminal bell with the
// line — suppressed while hidden, since a beep would give the user away.
func (u *UI) emit(line string, bell bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.hidden {
		if len(u.hiddenBuf) >= hiddenBufCap {
			u.hiddenBuf = u.hiddenBuf[1:]
			u.hiddenDropped++
		}
		u.hiddenBuf = append(u.hiddenBuf, line)
		return
	}
	if bell && u.bell && u.raw {
		line += "\a"
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
	u.emit(line, false)
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

// Chat prints a chat line from nick. key identifies the speaker for color
// assignment — the sender's instance id, not the nick, since nicknames are
// neither unique nor stable. mention bolds the message (it names the user);
// bell asks for the terminal bell, which is what a backgrounded terminal
// surfaces as a Dock badge / bounce (macOS) or taskbar flash (Windows) — the
// "you have a message" signal when the window isn't visible.
func (u *UI) Chat(key, nick, text string, mention, bell bool) {
	u.emit(u.format(key, nick, text, false, mention), bell)
}

// Action prints an emote, e.g. "* alice waves". key, mention and bell behave
// as in Chat; the * marker takes the speaker's color.
func (u *UI) Action(key, nick, text string, mention, bell bool) {
	u.emit(u.format(key, "*", nick+" "+text, false, mention), bell)
}

// System prints a system notice.
func (u *UI) System(text string) {
	u.emit(u.format("", "·", text, true, false), false)
}

// nickCol is the fixed display width of the name column.
const nickCol = 12

// format lays out one display line.
//
// Decorated mode (the default) separates metadata from content so names read
// at a glance even without -color: the timestamp is dimmed, the nick is
// right-aligned into a fixed column and emphasized (bold, or the speaker's
// assigned color with -color), and a dim │ gutter runs between names and
// messages, forming a clean vertical seam down the screen. System notices are
// dimmed whole so real chat stands out; mention-flagged messages are bolded
// so lines that name the user stand out further. Log style (stealth) keeps
// the flat, unstyled layout that passes for logger output.
//
// Callers must not hold mu for non-system lines: color assignment locks it.
func (u *UI) format(key, nick, text string, system, mention bool) string {
	ts := time.Now().Format("15:04:05")
	name := proto.ClampRunes(nick, nickCol)
	pad := strings.Repeat(" ", nickCol-len([]rune(name)))
	if u.logStyle {
		return ts + " " + name + pad + " " + text
	}
	if system {
		return u.sgr("2", ts+" "+pad+name+" │ "+text)
	}
	styled := u.sgr("1", name)
	if u.color {
		styled = u.sgr(strconv.Itoa(u.colorFor(key, nick)), name)
	}
	if mention {
		text = u.sgr("1", text)
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

// palette is the set of ANSI colors handed out to speakers in -color mode:
// the six standard colors and their bright variants (dim codes are reserved
// for the UI's own metadata styling).
var palette = [...]int{31, 32, 33, 34, 35, 36, 91, 92, 93, 94, 95, 96}

// colorFor returns the color for one speaker. A speaker keeps their first
// assignment for as long as they hold a slot — keyed by instance id, so a
// /nick rename keeps a person's color and two people sharing a nickname still
// look different. A new speaker prefers the hash of their nick (colors agree
// across machines when there's no contention, and match pre-registry builds),
// takes the nearest free slot after it when the preferred one is held, and
// only when every slot is occupied shares the least-crowded one.
func (u *UI) colorFor(key, nick string) int {
	u.mu.Lock()
	defer u.mu.Unlock()
	if c, ok := u.colorAssign[key]; ok {
		return c
	}
	start := hashIdx(nick)
	best, bestUse := start, int(^uint(0)>>1)
	for i := range palette {
		idx := (start + i) % len(palette)
		use := u.colorUse[palette[idx]]
		if use == 0 {
			best = idx
			break
		}
		if use < bestUse {
			best, bestUse = idx, use
		}
	}
	c := palette[best]
	u.colorAssign[key] = c
	u.colorUse[c]++
	return c
}

// ForgetColor releases the color slot held by key (the peer left), freeing it
// for the next joiner. Lines already on screen keep the color they were
// printed with; if the same peer somehow speaks again they are simply
// re-assigned.
func (u *UI) ForgetColor(key string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if c, ok := u.colorAssign[key]; ok {
		delete(u.colorAssign, key)
		if u.colorUse[c] > 1 {
			u.colorUse[c]--
		} else {
			delete(u.colorUse, c)
		}
	}
}

// hashIdx maps a nickname to its preferred palette slot (FNV-1a — the same
// mapping colors used before the per-speaker registry existed).
func hashIdx(s string) int {
	var h uint32 = 2166136261
	for _, c := range s {
		h = (h ^ uint32(c)) * 16777619
	}
	return int(h % uint32(len(palette)))
}

// ---- boss mode -------------------------------------------------------------

// ToggleBoss hides the chat behind the decoy build output, or restores it.
// Restoring replays every line that arrived while hidden (from the bounded
// in-memory buffer), so a quick Ctrl-B no longer costs you the conversation.
func (u *UI) ToggleBoss() {
	u.mu.Lock()
	defer u.mu.Unlock()
	if !u.hidden {
		u.hidden = true
		u.hiddenBuf = nil
		u.hiddenDropped = 0
		fmt.Fprint(u.out, "\x1b[2J\x1b[H")
		printDecoy(u.out, u.raw)
		return
	}
	u.hidden = false
	fmt.Fprint(u.out, "\x1b[2J\x1b[H")
	end := lineEnd(u.raw)
	if n := len(u.hiddenBuf); n > 0 {
		note := fmt.Sprintf("while hidden, %d message(s) arrived:", n+u.hiddenDropped)
		if u.hiddenDropped > 0 {
			note += fmt.Sprintf(" (showing the last %d)", n)
		}
		// System line: safe to format while holding mu (no color assignment).
		fmt.Fprint(u.out, u.format("", "·", note, true, false), end)
		for _, line := range u.hiddenBuf {
			fmt.Fprint(u.out, line, end)
		}
	}
	u.hiddenBuf = nil
	u.hiddenDropped = 0
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

		// Anything but another Tab ends a completion-cycling session.
		if r != '\t' {
			u.compActive = false
		}

		switch r {
		case '\t':
			u.complete()
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

// ---- tab completion ----------------------------------------------------------

// complete implements Tab: it completes the word before the cursor via the
// Completer callback, and cycles through the candidates on repeated presses.
// Commands complete with a trailing space, a nick at the start of the line
// with ": " (the IRC addressing convention); suffixes are only added when the
// cursor is at the end of the line, so completing mid-sentence stays clean.
// All completion state lives in the Run goroutine; only buf edits take the
// lock.
func (u *UI) complete() {
	if u.Completer == nil {
		return
	}
	u.mu.Lock()
	defer u.mu.Unlock()

	if u.compActive && len(u.compCands) > 0 {
		u.compIdx = (u.compIdx + 1) % len(u.compCands)
		u.applyCompletionLocked()
		return
	}

	start := u.cur
	for start > 0 && u.buf[start-1] != ' ' {
		start--
	}
	if start == u.cur {
		return // nothing before the cursor to complete
	}
	prefix := string(u.buf[start:u.cur])
	cands := u.Completer(prefix, start == 0)
	if len(cands) == 0 {
		return
	}

	suffix := ""
	if u.cur == len(u.buf) { // only pad when completing at the end of the line
		if strings.HasPrefix(prefix, "/") {
			suffix = " "
		} else if start == 0 {
			suffix = ": "
		} else {
			suffix = " "
		}
	}
	u.compActive = true
	u.compCands = cands
	u.compIdx = 0
	u.compStart = start
	u.compEnd = u.cur
	u.compSuffix = suffix
	u.applyCompletionLocked()
}

// applyCompletionLocked replaces the current completion region with the
// selected candidate (plus suffix) and repaints. Caller holds mu.
func (u *UI) applyCompletionLocked() {
	repl := []rune(u.compCands[u.compIdx] + u.compSuffix)
	tail := append([]rune(nil), u.buf[u.compEnd:]...)
	u.buf = append(u.buf[:u.compStart], repl...)
	u.buf = append(u.buf, tail...)
	u.compEnd = u.compStart + len(repl)
	u.cur = u.compEnd
	u.drawLocked()
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
