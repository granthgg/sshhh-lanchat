// Package proto defines the message record exchanged between peers, along with
// the small helpers that guard the wire: sequence numbering, duplicate
// suppression, text sanitizing, and size-bounded encoding.
package proto

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

// Message types carried inside the encrypted payload.
const (
	TypeMsg   = "m" // a chat line
	TypeMe    = "a" // an action / emote (/me)
	TypeJoin  = "j" // announce arrival
	TypeLeave = "l" // announce departure
	TypePing  = "p" // periodic presence heartbeat
)

// Msg is the decrypted, on-wire record. Kept short: it is JSON-encoded, then
// encrypted, then sent as a single UDP datagram, so it must fit comfortably
// inside one packet (see EncodeBounded).
type Msg struct {
	T  string `json:"t"`           // type (see constants above)
	ID string `json:"id"`          // sender instance id (random per run)
	N  string `json:"n"`           // sender nickname
	S  uint64 `json:"s"`           // per-sender sequence number
	B  string `json:"b,omitempty"` // body text
}

// Sizing. A standard Ethernet MTU leaves 1472 bytes of UDP payload
// (1500 − 20 IP − 8 UDP); the encryption framing adds 32 bytes (magic, nonce,
// GCM tag). Staying under these numbers means a message is never IP-fragmented
// — fragments are the first thing flaky Wi-Fi gear and office networks drop.
const (
	// MaxBodyRunes caps a chat line by character count (what the user sees
	// while typing).
	MaxBodyRunes = 900
	// MaxBodyBytes caps the UTF-8 encoding of a body. A rune can be up to 4
	// bytes, so a rune cap alone is not enough: 900 runes of emoji is 3.6 KB.
	MaxBodyBytes = 1000
	// MaxRawBytes bounds the JSON payload handed to the cipher, with margin
	// for tunnels/VLANs with a smaller-than-1500 MTU.
	MaxRawBytes = 1200
)

// NewInstanceID returns a short random id identifying one running process. It
// lets peers ignore their own multicast echoes and track presence even if two
// people pick the same nickname.
func NewInstanceID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// EncodeBounded JSON-encodes m, trimming the body as needed so the result is
// at most max bytes. HTML escaping is disabled ('<' stays 1 byte instead of
// the 6-byte "<"), which keeps common punctuation from inflating a frame
// past the MTU. If the message is over budget even with an empty body (huge
// nick — cannot happen with upstream clamps), the encoding is returned as-is.
func (m Msg) EncodeBounded(max int) ([]byte, error) {
	for {
		raw, err := marshalCompact(m)
		if err != nil {
			return nil, err
		}
		if len(raw) <= max || m.B == "" {
			return raw, nil
		}
		// Each body byte accounts for 1–2 encoded bytes (quotes and
		// backslashes escape to two), so cutting half the overshoot in body
		// bytes converges in a few passes without overshooting to empty.
		cut := (len(raw) - max + 1) / 2
		if cut < 1 {
			cut = 1
		}
		keep := len(m.B) - cut
		if keep < 0 {
			keep = 0
		}
		m.B = ClampBytes(m.B, keep)
	}
}

func marshalCompact(m Msg) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(m); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// SeqGen hands out monotonically increasing sequence numbers for one sender.
type SeqGen struct{ n atomic.Uint64 }

// Next returns the next sequence number. It is safe for concurrent use.
func (s *SeqGen) Next() uint64 { return s.n.Add(1) }

// Dedup remembers recently seen (id, seq) pairs so the same logical message —
// which may arrive several times because we send over multicast *and* broadcast
// and may listen on several interfaces — is shown only once.
type Dedup struct {
	mu        sync.Mutex
	seen      map[string]time.Time
	lastPrune time.Time
}

const (
	dedupTTL     = 2 * time.Minute
	dedupSoftCap = 8192  // start pruning expired entries above this
	dedupHardCap = 32768 // flood guard: keep only the last few seconds
)

// NewDedup returns an empty Dedup ready for use.
func NewDedup() *Dedup { return &Dedup{seen: make(map[string]time.Time)} }

// FirstSeen reports whether this (id, seq) is new, recording it if so.
func (d *Dedup) FirstSeen(id string, seq uint64) bool {
	key := id + ":" + strconv.FormatUint(seq, 10)
	now := time.Now()

	d.mu.Lock()
	defer d.mu.Unlock()

	if _, ok := d.seen[key]; ok {
		return false
	}
	d.seen[key] = now

	// Prune at most once per second so a sustained burst can't turn every
	// insert into an O(n) sweep.
	if len(d.seen) > dedupSoftCap && now.Sub(d.lastPrune) > time.Second {
		d.lastPrune = now
		for k, t := range d.seen {
			if now.Sub(t) > dedupTTL {
				delete(d.seen, k)
			}
		}
		if len(d.seen) > dedupHardCap { // someone with the key is flooding
			for k, t := range d.seen {
				if now.Sub(t) > 5*time.Second {
					delete(d.seen, k)
				}
			}
		}
	}
	return true
}

// Sanitize strips control characters from text arriving off the network (and
// from our own input) so a peer cannot inject ANSI escape sequences to spoof
// lines, move the cursor, or otherwise scramble another person's terminal.
// Tabs become spaces; everything else below 0x20 (including ESC) is dropped.
func Sanitize(s string) string {
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

// ClampRunes truncates s to at most n runes.
func ClampRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// ClampBytes truncates s to at most max bytes without splitting a UTF-8 rune.
func ClampBytes(s string, max int) string {
	if max < 0 {
		max = 0
	}
	if len(s) <= max {
		return s
	}
	for max > 0 && !utf8.RuneStart(s[max]) {
		max--
	}
	return s[:max]
}
