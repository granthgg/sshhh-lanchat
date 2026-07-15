package proto

import "testing"

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
