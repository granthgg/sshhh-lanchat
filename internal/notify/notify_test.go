package notify

import (
	"strings"
	"testing"
	"time"
)

// testNotifier returns a Notifier on a fake clock. Gating is asserted through
// allow directly — Notify hands off to a goroutine, so the deterministic
// decision point is what the tests pin down.
func testNotifier(enabled bool) (*Notifier, *time.Time) {
	n := New(enabled)
	now := time.Unix(1_000_000, 0)
	n.now = func() time.Time { return now }
	return n, &now
}

// A disabled notifier never fires, and reports itself disabled.
func TestDisabled(t *testing.T) {
	n, _ := testNotifier(false)
	if n.Enabled() {
		t.Fatal("Enabled() = true for a disabled notifier")
	}
	if n.allow() {
		t.Fatal("disabled notifier allowed a send")
	}
}

// Bursts collapse: after one send, nothing fires until minGap has passed.
func TestRateLimit(t *testing.T) {
	n, now := testNotifier(true)
	if !n.allow() {
		t.Fatal("first send must be allowed")
	}
	if n.allow() {
		t.Fatal("send allowed immediately after another")
	}
	*now = now.Add(minGap - time.Millisecond)
	if n.allow() {
		t.Fatal("send allowed just inside the rate-limit gap")
	}
	*now = now.Add(2 * time.Millisecond)
	if !n.allow() {
		t.Fatal("send blocked after the gap elapsed")
	}
}

// Snooze silences everything until it expires, then delivery resumes; a new
// snooze replaces the old deadline rather than stacking onto it.
func TestSnooze(t *testing.T) {
	n, now := testNotifier(true)
	n.Snooze(10 * time.Minute)
	if n.allow() {
		t.Fatal("send allowed while snoozed")
	}
	if got := n.SnoozedFor(); got != 10*time.Minute {
		t.Fatalf("SnoozedFor() = %v, want 10m", got)
	}
	*now = now.Add(9 * time.Minute)
	n.Snooze(time.Minute) // replace: 1m from now, not 1m + remainder
	if got := n.SnoozedFor(); got != time.Minute {
		t.Fatalf("SnoozedFor() after re-snooze = %v, want 1m", got)
	}
	*now = now.Add(time.Minute + time.Second)
	if got := n.SnoozedFor(); got != 0 {
		t.Fatalf("SnoozedFor() after expiry = %v, want 0", got)
	}
	if !n.allow() {
		t.Fatal("send blocked after the snooze expired")
	}
}

// Unsnooze lifts the silence immediately; the notifier stays Enabled the
// whole time (snooze is a pause, not an off switch).
func TestUnsnooze(t *testing.T) {
	n, _ := testNotifier(true)
	n.Snooze(time.Hour)
	if !n.Enabled() {
		t.Fatal("Enabled() = false while snoozed")
	}
	n.Unsnooze()
	if got := n.SnoozedFor(); got != 0 {
		t.Fatalf("SnoozedFor() after Unsnooze = %v, want 0", got)
	}
	if !n.allow() {
		t.Fatal("send blocked after Unsnooze")
	}
}

// Notify delivers through the injected sender (asynchronously) when the gates
// are open, and skips it when disabled.
func TestNotifyDelivers(t *testing.T) {
	n := New(true)
	got := make(chan [2]string, 1)
	n.send = func(title, body string) { got <- [2]string{title, body} }
	n.Notify("lanchat", "alice: hi")
	select {
	case pair := <-got:
		if pair[0] != "lanchat" || pair[1] != "alice: hi" {
			t.Fatalf("delivered %q, want [lanchat, alice: hi]", pair)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("notification never delivered")
	}

	off := New(false)
	off.send = func(string, string) { t.Error("disabled notifier delivered") }
	off.Notify("lanchat", "nope")
	time.Sleep(20 * time.Millisecond) // give a buggy goroutine a chance to run
}

// tidyBody: empty bodies get a placeholder, long ones clamp at a rune
// boundary with an ellipsis, short ones pass through untouched.
func TestTidyBody(t *testing.T) {
	if got := tidyBody("  \t "); got != "new message" {
		t.Fatalf("tidyBody(blank) = %q", got)
	}
	if got := tidyBody("alice: hi"); got != "alice: hi" {
		t.Fatalf("tidyBody(short) = %q, want unchanged", got)
	}
	long := strings.Repeat("é", maxBodyRunes+50)
	got := tidyBody(long)
	if r := []rune(got); len(r) != maxBodyRunes || r[len(r)-1] != '…' {
		t.Fatalf("tidyBody(long) = %d runes ending %q, want %d ending …", len(r), r[len(r)-1], maxBodyRunes)
	}
}
