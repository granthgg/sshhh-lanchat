// Package notify posts best-effort desktop notifications (macOS Notification
// Center, a Windows toast, libnotify on Linux) so that a chat sitting in a
// background window can still get the user's attention. Nothing here is
// load-bearing: every failure path — no helper installed, no permission, a
// hung helper — degrades to silence rather than an error, because the chat
// loops must never block or break over a banner that couldn't be shown.
package notify

import (
	"strings"
	"sync"
	"time"
)

// minGap is the minimum spacing between two delivered notifications. A burst
// of chat collapses into one banner: the job is "check your chat", and a
// banner per line is exactly the noise a quiet heads-up exists to avoid.
const minGap = 3 * time.Second

// maxBodyRunes bounds the text handed to the OS so a maximum-length chat
// message doesn't become a screen-filling toast. The OS truncates anyway;
// this keeps the helper's argv/env small and the banner readable.
const maxBodyRunes = 140

// Notifier gates and delivers desktop notifications. Construct with New; the
// zero value is a permanently disabled notifier.
//
// Delivery is fire-and-forget on a short-lived goroutine, so the caller (the
// receive loop) never waits on an OS helper process.
type Notifier struct {
	mu          sync.Mutex
	enabled     bool
	snoozeUntil time.Time
	lastSent    time.Time

	send func(title, body string) // platform delivery; swapped in tests
	now  func() time.Time         // clock; swapped in tests
}

// New returns a Notifier. With enabled=false it is inert: Notify becomes a
// no-op, and Enabled lets callers explain why (/snooze reports it).
func New(enabled bool) *Notifier {
	return &Notifier{enabled: enabled, send: send, now: time.Now}
}

// Enabled reports whether this notifier can deliver at all. It stays true
// while snoozed — snooze is a pause, not an off switch.
func (n *Notifier) Enabled() bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.enabled
}

// Notify shows one notification unless disabled, snoozed, or within minGap of
// the previous one. Burst-collapsing and snooze both live here, so callers
// just report every message and let the gate decide.
func (n *Notifier) Notify(title, body string) {
	if !n.allow() {
		return
	}
	go n.send(title, tidyBody(body))
}

// allow applies the gates (enabled, snooze, rate limit) and, when it returns
// true, records the send it just approved.
func (n *Notifier) allow() bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	now := n.now()
	if !n.enabled || now.Before(n.snoozeUntil) || now.Sub(n.lastSent) < minGap {
		return false
	}
	n.lastSent = now
	return true
}

// Snooze silences notifications for d from now, replacing (not extending) any
// snooze already running.
func (n *Notifier) Snooze(d time.Duration) {
	n.mu.Lock()
	n.snoozeUntil = n.now().Add(d)
	n.mu.Unlock()
}

// Unsnooze lifts an active snooze immediately.
func (n *Notifier) Unsnooze() {
	n.mu.Lock()
	n.snoozeUntil = time.Time{}
	n.mu.Unlock()
}

// SnoozedFor reports how much of an active snooze remains, or zero when
// notifications are not snoozed.
func (n *Notifier) SnoozedFor() time.Duration {
	n.mu.Lock()
	defer n.mu.Unlock()
	if r := n.snoozeUntil.Sub(n.now()); r > 0 {
		return r
	}
	return 0
}

// tidyBody clamps the body for display and guarantees it is never empty —
// Windows' balloon API rejects empty text, and a blank banner says nothing.
func tidyBody(body string) string {
	if strings.TrimSpace(body) == "" {
		return "new message"
	}
	r := []rune(body)
	if len(r) > maxBodyRunes {
		return string(r[:maxBodyRunes-1]) + "…"
	}
	return body
}
