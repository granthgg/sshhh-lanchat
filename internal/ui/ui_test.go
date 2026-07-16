package ui

import (
	"os"
	"strings"
	"testing"
)

// newTestUI returns a UI in non-raw mode (test stdin is not a terminal), which
// exercises the same buffer logic with drawing as a no-op.
func newTestUI(t *testing.T) *UI {
	t.Helper()
	u := New(Options{Prompt: "» "})
	if u.raw {
		t.Fatal("test environment unexpectedly has a raw terminal")
	}
	return u
}

func setLine(u *UI, s string, cur int) {
	u.buf = []rune(s)
	u.cur = cur
}

// A single command match completes in place with a trailing space.
func TestCompleteCommand(t *testing.T) {
	u := newTestUI(t)
	u.Completer = func(word string, lineStart bool) []string {
		if word == "/n" && lineStart {
			return []string{"/nick"}
		}
		return nil
	}
	setLine(u, "/n", 2)
	u.complete()
	if got := string(u.buf); got != "/nick " {
		t.Fatalf("buf = %q, want %q", got, "/nick ")
	}
	if u.cur != len(u.buf) {
		t.Fatalf("cursor = %d, want end (%d)", u.cur, len(u.buf))
	}
}

// A nick at the start of the line completes with the ": " addressing suffix,
// and repeated Tab cycles through all candidates, replacing cleanly.
func TestCompleteNickCycles(t *testing.T) {
	u := newTestUI(t)
	u.Completer = func(word string, lineStart bool) []string {
		return []string{"alice", "alexandra"}
	}
	setLine(u, "al", 2)

	u.complete()
	if got := string(u.buf); got != "alice: " {
		t.Fatalf("first Tab: buf = %q, want %q", got, "alice: ")
	}
	u.complete()
	if got := string(u.buf); got != "alexandra: " {
		t.Fatalf("second Tab: buf = %q, want %q", got, "alexandra: ")
	}
	u.complete()
	if got := string(u.buf); got != "alice: " {
		t.Fatalf("third Tab wraps: buf = %q, want %q", got, "alice: ")
	}
}

// Completing in the middle of a line inserts the bare candidate (no suffix)
// and preserves the text after the cursor.
func TestCompleteMidLinePreservesTail(t *testing.T) {
	u := newTestUI(t)
	u.Completer = func(word string, lineStart bool) []string {
		if lineStart {
			t.Fatal("mid-line word reported as line start")
		}
		return []string{"alice"}
	}
	setLine(u, "ping al now", 7) // cursor right after "al"
	u.complete()
	if got := string(u.buf); got != "ping alice now" {
		t.Fatalf("buf = %q, want %q", got, "ping alice now")
	}
	if got := string(u.buf[u.cur:]); got != " now" {
		t.Fatalf("tail after cursor = %q, want %q", got, " now")
	}
}

// No candidates, no completer, or an empty prefix leave the line untouched.
func TestCompleteNoOps(t *testing.T) {
	u := newTestUI(t)
	setLine(u, "xyz", 3)
	u.complete() // nil Completer
	if got := string(u.buf); got != "xyz" {
		t.Fatalf("nil completer changed the line: %q", got)
	}
	u.Completer = func(string, bool) []string { return nil }
	u.complete() // no candidates
	if got := string(u.buf); got != "xyz" {
		t.Fatalf("no candidates changed the line: %q", got)
	}
	setLine(u, "word ", 5)
	u.complete() // empty prefix (cursor after a space)
	if got := string(u.buf); got != "word " {
		t.Fatalf("empty prefix changed the line: %q", got)
	}
}

// Boss mode holds arriving lines and replays them on restore, with a count.
func TestBossReplay(t *testing.T) {
	u := newTestUI(t)
	f, err := os.CreateTemp(t.TempDir(), "out")
	if err != nil {
		t.Fatal(err)
	}
	u.out = f

	u.ToggleBoss() // hide
	u.Chat("alice", "first while hidden", false)
	u.Chat("bob", "second while hidden", true) // mention: still silent while hidden
	u.ToggleBoss()                             // restore → replay

	data, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)
	for _, want := range []string{
		"while hidden, 2 message(s) arrived:",
		"first while hidden",
		"second while hidden",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("restore output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "\a") {
		t.Fatal("bell must never ring for lines that arrived while hidden")
	}
}

// The replay buffer is bounded: oldest lines drop, the note says so.
func TestBossReplayCapped(t *testing.T) {
	u := newTestUI(t)
	f, err := os.CreateTemp(t.TempDir(), "out")
	if err != nil {
		t.Fatal(err)
	}
	u.out = f

	u.ToggleBoss()
	for i := 0; i < hiddenBufCap+7; i++ {
		u.System("line")
	}
	u.ToggleBoss()

	data, _ := os.ReadFile(f.Name())
	out := string(data)
	if !strings.Contains(out, "507 message(s)") || !strings.Contains(out, "showing the last 500") {
		t.Fatalf("cap note missing or wrong:\n%.200s", out)
	}
}
