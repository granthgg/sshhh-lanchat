// Package roster tracks who is currently present in a room.
//
// Presence is soft state kept alive by periodic heartbeats: a peer that stops
// sending is dropped after presenceTTL, which also covers ungraceful exits
// (laptop lid closed, Wi-Fi dropped) where no "leave" was ever sent.
package roster

import (
	"sort"
	"sync"
	"time"
)

// presenceTTL is how long a peer may go unheard-from before it is dropped.
const presenceTTL = 13 * time.Second

type peer struct {
	nick string
	last time.Time
}

// Roster is the set of currently present peers, keyed by instance id. It is
// safe for concurrent use.
type Roster struct {
	mu    sync.Mutex
	peers map[string]*peer
}

// New returns an empty Roster ready for use.
func New() *Roster { return &Roster{peers: make(map[string]*peer)} }

// Seen records activity from a peer and reports whether this is the first time
// we have seen them (so the caller can announce a join exactly once).
func (r *Roster) Seen(id, nick string) (isNew bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.peers[id]
	if !ok {
		r.peers[id] = &peer{nick: nick, last: time.Now()}
		return true
	}
	p.nick = nick
	p.last = time.Now()
	return false
}

// Leave removes a peer that announced departure, returning its last nickname.
func (r *Roster) Leave(id string) (nick string, existed bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if p, ok := r.peers[id]; ok {
		delete(r.peers, id)
		return p.nick, true
	}
	return "", false
}

// Expire drops peers not heard from within presenceTTL, returning their nicks
// so the caller can announce the departures.
func (r *Roster) Expire() (left []string) {
	cutoff := time.Now().Add(-presenceTTL)
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, p := range r.peers {
		if p.last.Before(cutoff) {
			left = append(left, p.nick)
			delete(r.peers, id)
		}
	}
	return left
}

// List returns the current nicknames, sorted for stable display.
func (r *Roster) List() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.peers))
	for _, p := range r.peers {
		out = append(out, p.nick)
	}
	sort.Strings(out)
	return out
}
