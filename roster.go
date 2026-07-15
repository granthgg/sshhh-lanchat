package main

import (
	"sort"
	"sync"
	"time"
)

// roster tracks who is currently present, keyed by instance id. Presence is
// soft state kept alive by periodic heartbeats: a peer that stops sending is
// dropped after presenceTTL, which also covers ungraceful exits (laptop lid
// closed, Wi-Fi dropped) where no "leave" was ever sent.
const presenceTTL = 13 * time.Second

type peer struct {
	nick string
	last time.Time
}

type roster struct {
	mu    sync.Mutex
	peers map[string]*peer
}

func newRoster() *roster { return &roster{peers: make(map[string]*peer)} }

// seen records activity from a peer and reports whether this is the first time
// we have seen them (so the caller can announce a join exactly once).
func (r *roster) seen(id, nick string) (isNew bool) {
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

// leave removes a peer that announced departure, returning its last nickname.
func (r *roster) leave(id string) (nick string, existed bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if p, ok := r.peers[id]; ok {
		delete(r.peers, id)
		return p.nick, true
	}
	return "", false
}

// expire drops peers not heard from within presenceTTL, returning their nicks
// so the caller can announce the departures.
func (r *roster) expire() (left []string) {
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

// list returns the current nicknames, sorted for stable display.
func (r *roster) list() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.peers))
	for _, p := range r.peers {
		out = append(out, p.nick)
	}
	sort.Strings(out)
	return out
}
