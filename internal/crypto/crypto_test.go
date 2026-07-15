package crypto

import "testing"

// A payload sealed with a key round-trips through Open with the same key.
func TestSealOpenRoundTrip(t *testing.T) {
	a, err := New("team", "hunter2")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	want := []byte(`{"t":"m","id":"abc123","n":"alice","s":7,"b":"hello world"}`)

	frame, err := Seal(a, want)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	got, err := Open(a, frame)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("round-trip mismatch: got %q want %q", got, want)
	}
}

// A frame sealed for one room/passphrase must NOT open under another. This is
// the property that keeps rooms isolated and private on a shared LAN.
func TestWrongKeyRejected(t *testing.T) {
	sender, err := New("team", "hunter2")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	frame, _ := Seal(sender, []byte(`{"t":"m","b":"secret"}`))

	for _, tc := range []struct{ room, pass string }{
		{"team", "wrong-pass"},
		{"other-room", "hunter2"},
		{"lobby", ""},
	} {
		outsider, err := New(tc.room, tc.pass)
		if err != nil {
			t.Fatalf("New(%q,%q): %v", tc.room, tc.pass, err)
		}
		if _, err := Open(outsider, frame); err == nil {
			t.Fatalf("room=%q pass=%q was able to decrypt another room's frame", tc.room, tc.pass)
		}
	}
}

// Garbage and truncated data are rejected cleanly rather than panicking.
func TestOpenRejectsJunk(t *testing.T) {
	a, err := New("lobby", "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for _, junk := range [][]byte{nil, []byte("x"), []byte("TC02short"), []byte("NOPEnonce........ciphertext")} {
		if _, err := Open(a, junk); err == nil {
			t.Fatalf("expected error for junk input %q", junk)
		}
	}
}
