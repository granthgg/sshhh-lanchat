package roster

import (
	"reflect"
	"testing"
)

// First sighting announces a join; repeats don't; a changed nick is reported
// via the previous name so the caller can announce the rename.
func TestSeenJoinAndRename(t *testing.T) {
	r := New()

	prev, isNew := r.Seen("id1", "alice")
	if !isNew || prev != "" {
		t.Fatalf("first sighting: got (prev=%q, isNew=%v), want (\"\", true)", prev, isNew)
	}
	prev, isNew = r.Seen("id1", "alice")
	if isNew || prev != "alice" {
		t.Fatalf("repeat sighting: got (prev=%q, isNew=%v), want (\"alice\", false)", prev, isNew)
	}
	prev, isNew = r.Seen("id1", "alicia")
	if isNew || prev != "alice" {
		t.Fatalf("rename: got (prev=%q, isNew=%v), want (\"alice\", false)", prev, isNew)
	}
	if got := r.List(); !reflect.DeepEqual(got, []string{"alicia"}) {
		t.Fatalf("List = %v, want [alicia]", got)
	}
}

func TestLeave(t *testing.T) {
	r := New()
	r.Seen("id1", "alice")

	nick, existed := r.Leave("id1")
	if !existed || nick != "alice" {
		t.Fatalf("Leave: got (%q, %v), want (\"alice\", true)", nick, existed)
	}
	if nick, existed = r.Leave("id1"); existed || nick != "" {
		t.Fatalf("second Leave: got (%q, %v), want (\"\", false)", nick, existed)
	}
	if got := r.List(); len(got) != 0 {
		t.Fatalf("List after leave = %v, want empty", got)
	}
}

// List is sorted for stable display.
func TestListSorted(t *testing.T) {
	r := New()
	r.Seen("id-c", "carol")
	r.Seen("id-a", "alice")
	r.Seen("id-b", "bob")
	want := []string{"alice", "bob", "carol"}
	if got := r.List(); !reflect.DeepEqual(got, want) {
		t.Fatalf("List = %v, want %v", got, want)
	}
}

// Expire reports both id and nick for each dropped peer, so the caller can
// announce the departure and release per-peer state (like the color slot).
func TestExpireReturnsDepartures(t *testing.T) {
	r := New()
	r.Seen("id1", "alice")
	r.peers["id1"].last = r.peers["id1"].last.Add(-2 * presenceTTL)

	left := r.Expire()
	if len(left) != 1 || left[0] != (Departure{ID: "id1", Nick: "alice"}) {
		t.Fatalf("Expire = %v, want [{id1 alice}]", left)
	}
	if got := r.List(); len(got) != 0 {
		t.Fatalf("List after expire = %v, want empty", got)
	}
}
