package main

import (
	"encoding/json"
	"testing"
)

func mustAEAD(t *testing.T, room, pass string) interface {
	Seal(dst, nonce, plaintext, ad []byte) []byte
	Open(dst, nonce, ciphertext, ad []byte) ([]byte, error)
	NonceSize() int
	Overhead() int
} {
	t.Helper()
	a, err := buildAEAD(room, pass)
	if err != nil {
		t.Fatalf("buildAEAD: %v", err)
	}
	return a
}

// A message sealed with a key round-trips through open with the same key.
func TestSealOpenRoundTrip(t *testing.T) {
	a := mustAEAD(t, "team", "hunter2")
	in := Msg{T: typeMsg, ID: "abc123", N: "alice", S: 7, B: "hello world"}
	raw, _ := json.Marshal(in)

	frame, err := seal(a, raw)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	pt, err := open(a, frame)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	var out Msg
	if err := json.Unmarshal(pt, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out != in {
		t.Fatalf("round-trip mismatch: got %+v want %+v", out, in)
	}
}

// A frame sealed for one room/passphrase must NOT open under another. This is
// the property that keeps rooms isolated and private on a shared LAN.
func TestWrongKeyRejected(t *testing.T) {
	sender := mustAEAD(t, "team", "hunter2")
	frame, _ := seal(sender, []byte(`{"t":"m","b":"secret"}`))

	for _, tc := range []struct{ room, pass string }{
		{"team", "wrong-pass"},
		{"other-room", "hunter2"},
		{"lobby", ""},
	} {
		outsider := mustAEAD(t, tc.room, tc.pass)
		if _, err := open(outsider, frame); err == nil {
			t.Fatalf("room=%q pass=%q was able to decrypt another room's frame", tc.room, tc.pass)
		}
	}
}

// Garbage and truncated data are rejected cleanly rather than panicking.
func TestOpenRejectsJunk(t *testing.T) {
	a := mustAEAD(t, "lobby", "")
	for _, junk := range [][]byte{nil, []byte("x"), []byte("TC02short"), []byte("NOPEnonce........ciphertext")} {
		if _, err := open(a, junk); err == nil {
			t.Fatalf("expected error for junk input %q", junk)
		}
	}
}

// Dedup shows each (id, seq) exactly once regardless of how many copies arrive.
func TestDedup(t *testing.T) {
	d := newDedup()
	if !d.firstSeen("id1", 1) {
		t.Fatal("first sighting should be new")
	}
	if d.firstSeen("id1", 1) {
		t.Fatal("duplicate should be suppressed")
	}
	if !d.firstSeen("id1", 2) {
		t.Fatal("new seq should be new")
	}
	if !d.firstSeen("id2", 1) {
		t.Fatal("different sender should be new")
	}
}

// sanitize strips control characters, including the ESC that would let a peer
// inject terminal escape sequences.
func TestSanitizeStripsControl(t *testing.T) {
	in := "hi\x1b[31mred\x1b[0m\x07\r\nthere\ttab"
	got := sanitize(in)
	want := "hi[31mred[0mthere tab"
	if got != want {
		t.Fatalf("sanitize = %q, want %q", got, want)
	}
}

// The same room name always maps to the same multicast group, and different
// rooms generally differ.
func TestGroupForDeterministic(t *testing.T) {
	if !groupFor("team").Equal(groupFor("team")) {
		t.Fatal("group must be stable for a given room")
	}
	g := groupFor("team").To4()
	if g == nil || g[0] != 239 || g[1] != 255 {
		t.Fatalf("group %v not in 239.255.0.0/16", groupFor("team"))
	}
	if groupFor("team").Equal(groupFor("other")) {
		t.Fatal("different rooms should usually differ (hash collision is unlucky)")
	}
}
