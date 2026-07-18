package chat

import (
	"reflect"
	"testing"
	"time"
)

// filterPrefixFold matches case-insensitively, rune-safely, preserving the
// candidate's own spelling.
func TestFilterPrefixFold(t *testing.T) {
	list := []string{"Alice", "alexandra", "bob", "Wörld"}
	cases := []struct {
		prefix string
		want   []string
	}{
		{"al", []string{"Alice", "alexandra"}},
		{"AL", []string{"Alice", "alexandra"}},
		{"b", []string{"bob"}},
		{"wö", []string{"Wörld"}}, // multibyte prefix, case-folded
		{"z", nil},
		{"alexandra", []string{"alexandra"}}, // full-length match
		{"alexandraa", nil},                  // longer than any candidate
	}
	for _, tc := range cases {
		if got := filterPrefixFold(list, tc.prefix); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("filterPrefixFold(%q) = %v, want %v", tc.prefix, got, tc.want)
		}
	}
}

// Command completion offers slash commands only at the start of the line.
func TestCompletionsCommands(t *testing.T) {
	c := &Client{}
	if got := c.completions("/q", true); !reflect.DeepEqual(got, []string{"/quit"}) {
		t.Fatalf("completions(/q, lineStart) = %v, want [/quit]", got)
	}
	if got := c.completions("/s", true); !reflect.DeepEqual(got, []string{"/snooze"}) {
		t.Fatalf("completions(/s, lineStart) = %v, want [/snooze]", got)
	}
	if got := c.completions("/q", false); got != nil {
		t.Fatalf("mid-line slash word must not complete, got %v", got)
	}
}

// parseSnooze: bare numbers are minutes, Go duration syntax works verbatim,
// nonsense and non-positive values are rejected, oversized ones clamp.
func TestParseSnooze(t *testing.T) {
	cases := []struct {
		arg  string
		want time.Duration
		ok   bool
	}{
		{"10", 10 * time.Minute, true},
		{"90s", 90 * time.Second, true},
		{"1h30m", 90 * time.Minute, true},
		{"48h", maxSnooze, true}, // clamped, not rejected
		{"0", 0, false},
		{"-5m", 0, false},
		{"soon", 0, false},
		{"", 0, false},
	}
	for _, tc := range cases {
		got, err := parseSnooze(tc.arg)
		if tc.ok != (err == nil) {
			t.Errorf("parseSnooze(%q) error = %v, want ok=%v", tc.arg, err, tc.ok)
			continue
		}
		if tc.ok && got != tc.want {
			t.Errorf("parseSnooze(%q) = %v, want %v", tc.arg, got, tc.want)
		}
	}
}

// The snoozer gates the bell: silenced before the deadline, ringing after; a
// new snooze replaces the old deadline rather than extending it, and clear
// lifts it immediately.
func TestSnoozer(t *testing.T) {
	s := newSnoozer()
	now := time.Unix(1_000_000, 0)
	s.now = func() time.Time { return now }

	if got := s.remaining(); got != 0 {
		t.Fatalf("fresh snoozer: remaining() = %v, want 0", got)
	}
	s.set(10 * time.Minute)
	if got := s.remaining(); got != 10*time.Minute {
		t.Fatalf("remaining() = %v, want 10m", got)
	}
	now = now.Add(9 * time.Minute)
	s.set(time.Minute) // replace: 1m from now, not 1m + the old remainder
	if got := s.remaining(); got != time.Minute {
		t.Fatalf("remaining() after re-snooze = %v, want 1m", got)
	}
	now = now.Add(time.Minute + time.Second)
	if got := s.remaining(); got != 0 {
		t.Fatalf("remaining() after expiry = %v, want 0", got)
	}
	s.set(time.Hour)
	s.clear()
	if got := s.remaining(); got != 0 {
		t.Fatalf("remaining() after clear = %v, want 0", got)
	}
}

// formatDur drops the zero units that time.Duration.String would print.
func TestFormatDur(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{15 * time.Minute, "15m"},
		{45 * time.Second, "45s"},
		{90 * time.Second, "1m30s"},
		{time.Hour, "1h"},
		{90 * time.Minute, "1h30m"},
		{24 * time.Hour, "24h"},
	}
	for _, tc := range cases {
		if got := formatDur(tc.d); got != tc.want {
			t.Errorf("formatDur(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}
