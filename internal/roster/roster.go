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

// Seen records activity from a peer. It reports whether this is the first
// sighting (so the caller can announce a join exactly once) and, when it is
// not, the previously known nickname — so the caller can announce a rename
// whenever prev differs from nick.
func (r *Roster) Seen(id, nick string) (prev string, isNew bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.peers[id]
	if !ok {
		r.peers[id] = &peer{nick: nick, last: time.Now()}
		return "", true
	}
	prev = p.nick
	p.nick = nick
	p.last = time.Now()
	return prev, false
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

// Departure identifies one peer dropped from the roster: the nick for the
// on-screen announcement, the id for per-peer cleanup (color release).
type Departure struct {
	ID   string
	Nick string
}

// Expire drops peers not heard from within presenceTTL, returning who left so
// the caller can announce the departures and release per-peer state.
func (r *Roster) Expire() (left []Departure) {
	cutoff := time.Now().Add(-presenceTTL)
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, p := range r.peers {
		if p.last.Before(cutoff) {
			left = append(left, Departure{ID: id, Nick: p.nick})
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
