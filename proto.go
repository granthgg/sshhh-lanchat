package main

import (
	"crypto/rand"
	"encoding/hex"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Message types carried inside the encrypted payload.
const (
	typeMsg   = "m" // a chat line
	typeMe    = "a" // an action / emote (/me)
	typeJoin  = "j" // announce arrival
	typeLeave = "l" // announce departure
	typePing  = "p" // periodic presence heartbeat
)

// Msg is the decrypted, on-wire record. Kept short: it is JSON-encoded, then
// encrypted, then sent as a single UDP datagram, so it must fit comfortably
// inside one packet (see maxBodyRunes).
type Msg struct {
	T  string `json:"t"`           // type (see constants above)
	ID string `json:"id"`          // sender instance id (random per run)
	N  string `json:"n"`           // sender nickname
	S  uint64 `json:"s"`           // per-sender sequence number
	B  string `json:"b,omitempty"` // body text
}

// maxBodyRunes caps a chat line so the encrypted datagram stays well under a
// typical 1500-byte MTU and never fragments.
const maxBodyRunes = 900

// newInstanceID returns a short random id identifying one running process.
// It lets peers ignore their own multicast echoes and track presence even if
// two people pick the same nickname.
func newInstanceID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// seqGen hands out monotonically increasing sequence numbers for one sender.
type seqGen struct{ n atomic.Uint64 }

func (s *seqGen) next() uint64 { return s.n.Add(1) }

// dedup remembers recently seen (id, seq) pairs so the same logical message —
// which may arrive several times because we send over multicast *and*
// broadcast and may listen on several interfaces — is shown only once.
type dedup struct {
	mu   sync.Mutex
	seen map[string]time.Time
}

func newDedup() *dedup { return &dedup{seen: make(map[string]time.Time)} }

// firstSeen reports whether this (id, seq) is new, recording it if so.
func (d *dedup) firstSeen(id string, seq uint64) bool {
	key := id + ":" + strconv.FormatUint(seq, 10)
	now := time.Now()

	d.mu.Lock()
	defer d.mu.Unlock()

	if _, ok := d.seen[key]; ok {
		return false
	}
	d.seen[key] = now

	if len(d.seen) > 8192 { // opportunistic prune of stale entries
		for k, t := range d.seen {
			if now.Sub(t) > 2*time.Minute {
				delete(d.seen, k)
			}
		}
	}
	return true
}

// sanitize strips control characters from text arriving off the network (and
// from our own input) so a peer cannot inject ANSI escape sequences to spoof
// lines, move the cursor, or otherwise scramble another person's terminal.
// Tabs become spaces; everything else below 0x20 (including ESC) is dropped.
func sanitize(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\t':
			b.WriteByte(' ')
		case r < 0x20 || r == 0x7f:
			// drop control characters, including ESC (0x1b)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// clampRunes truncates s to at most n runes.
func clampRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
