package transport

import "testing"

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
