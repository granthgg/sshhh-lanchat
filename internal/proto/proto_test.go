package proto

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// Dedup shows each (id, seq) exactly once regardless of how many copies arrive.
func TestDedup(t *testing.T) {
	d := NewDedup()
	if !d.FirstSeen("id1", 1) {
		t.Fatal("first sighting should be new")
	}
	if d.FirstSeen("id1", 1) {
		t.Fatal("duplicate should be suppressed")
	}
	if !d.FirstSeen("id1", 2) {
		t.Fatal("new seq should be new")
	}
	if !d.FirstSeen("id2", 1) {
		t.Fatal("different sender should be new")
	}
}

// Sanitize strips control characters, including the ESC that would let a peer
// inject terminal escape sequences.
func TestSanitizeStripsControl(t *testing.T) {
	in := "hi\x1b[31mred\x1b[0m\x07\r\nthere\ttab"
	got := Sanitize(in)
	want := "hi[31mred[0mthere tab"
	if got != want {
		t.Fatalf("Sanitize = %q, want %q", got, want)
	}
}

// ClampBytes must cut at a rune boundary, never mid-encoding.
func TestClampBytes(t *testing.T) {
	cases := []struct {
		in   string
		max  int
		want string
	}{
		{"hello", 10, "hello"}, // under the cap: untouched
		{"hello", 3, "hel"},    // ASCII cut
		{"héllo", 2, "h"},      // é is 2 bytes; cutting mid-rune backs up
		{"héllo", 3, "hé"},     // exact rune boundary is kept
		{"🙂🙂", 5, "🙂"},         // emoji are 4 bytes each
		{"🙂🙂", 4, "🙂"},         // exact boundary
		{"🙂", 3, ""},           // can't fit even one rune
		{"", 5, ""},            // empty in, empty out
		{"abc", 0, ""},         // zero budget
		{"abc", -1, ""},        // negative treated as zero
	}
	for _, tc := range cases {
		if got := ClampBytes(tc.in, tc.max); got != tc.want {
			t.Errorf("ClampBytes(%q, %d) = %q, want %q", tc.in, tc.max, got, tc.want)
		}
	}
}

// A pathological body (every byte needs JSON escaping) must still encode
// within budget, stay valid JSON, and keep a usable prefix of the message.
func TestEncodeBoundedFitsBudget(t *testing.T) {
	m := Msg{T: TypeMsg, ID: "abcdef123456", N: "alice", S: 42, B: strings.Repeat(`"`, 3*MaxBodyBytes)}
	raw, err := m.EncodeBounded(MaxRawBytes)
	if err != nil {
		t.Fatalf("EncodeBounded: %v", err)
	}
	if len(raw) > MaxRawBytes {
		t.Fatalf("encoded %d bytes, budget is %d", len(raw), MaxRawBytes)
	}
	var out Msg
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}
	if out.B == "" || !strings.HasPrefix(m.B, out.B) {
		t.Fatalf("body should be a non-empty prefix of the original, got %d bytes", len(out.B))
	}
	if out.ID != m.ID || out.N != m.N || out.S != m.S || out.T != m.T {
		t.Fatal("only the body may be trimmed")
	}
}

// A normal-size message passes through unmodified.
func TestEncodeBoundedRoundTrip(t *testing.T) {
	m := Msg{T: TypeMsg, ID: "abc123", N: "alice", S: 7, B: "hello, world — ça va? 🙂"}
	raw, err := m.EncodeBounded(MaxRawBytes)
	if err != nil {
		t.Fatalf("EncodeBounded: %v", err)
	}
	var out Msg
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out != m {
		t.Fatalf("round-trip mismatch: got %+v want %+v", out, m)
	}
}

// HTML escaping must be off: '<' as 1 byte, not the 6-byte <, so common
// punctuation can't inflate a frame past the MTU.
func TestEncodeBoundedNoHTMLEscape(t *testing.T) {
	m := Msg{T: TypeMsg, ID: "x", N: "n", S: 1, B: "a <b> & c"}
	raw, err := m.EncodeBounded(MaxRawBytes)
	if err != nil {
		t.Fatalf("EncodeBounded: %v", err)
	}
	if !bytes.Contains(raw, []byte("a <b> & c")) {
		t.Fatalf("body was HTML-escaped: %s", raw)
	}
}
