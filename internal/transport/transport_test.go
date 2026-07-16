package transport

import (
	"net"
	"testing"
)

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

// Rooms must never land on addresses owned by other protocols (SSDP, SLP):
// sharing a group with every smart TV's discovery chatter would flood the
// room, and our frames would be delivered to their listeners.
func TestGroupOctetsSkipsReserved(t *testing.T) {
	h := make([]byte, 32)
	h[0], h[1] = 255, 250 // SSDP — must be skipped
	h[2], h[3] = 255, 253 // SLPv2 — must be skipped
	h[4], h[5] = 255, 255 // scope top — must be skipped
	h[6], h[7] = 9, 42    // first acceptable pair
	if a, b := groupOctets(h); a != 9 || b != 42 {
		t.Fatalf("groupOctets = %d.%d, want 9.42", a, b)
	}

	// A non-reserved first pair is used as-is (compatibility with old builds).
	h2 := []byte{17, 34, 1, 2}
	if a, b := groupOctets(h2); a != 17 || b != 34 {
		t.Fatalf("groupOctets = %d.%d, want 17.34", a, b)
	}
}

func TestReservedPair(t *testing.T) {
	for _, tc := range []struct {
		a, b byte
		want bool
	}{
		{255, 250, true},  // SSDP
		{255, 253, true},  // SLPv2
		{255, 255, true},  // scope top
		{255, 249, false}, // neighbors are fine
		{254, 250, false},
		{0, 0, false},
	} {
		if got := reservedPair(tc.a, tc.b); got != tc.want {
			t.Errorf("reservedPair(%d, %d) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

// Directed broadcast: address | ^mask, and no usable broadcast for /31, /32,
// or IPv6 networks.
func TestSubnetBroadcast(t *testing.T) {
	cases := []struct {
		cidr string
		want string // "" = nil expected
	}{
		{"192.168.1.42/24", "192.168.1.255"},
		{"10.0.0.5/8", "10.255.255.255"},
		{"172.16.16.7/20", "172.16.31.255"},
		{"169.254.7.9/16", "169.254.255.255"}, // link-local still broadcasts
		{"192.168.1.0/31", ""},                // point-to-point: no broadcast
		{"192.168.1.1/32", ""},                // host route: no broadcast
		{"2001:db8::1/64", ""},                // IPv6: not applicable
	}
	for _, tc := range cases {
		ip, n, err := net.ParseCIDR(tc.cidr)
		if err != nil {
			t.Fatalf("ParseCIDR(%q): %v", tc.cidr, err)
		}
		n.IP = ip // keep the host address, as Interface.Addrs reports it
		got := subnetBroadcast(n)
		if tc.want == "" {
			if got != nil {
				t.Errorf("subnetBroadcast(%s) = %v, want nil", tc.cidr, got)
			}
			continue
		}
		if got == nil || got.String() != tc.want {
			t.Errorf("subnetBroadcast(%s) = %v, want %s", tc.cidr, got, tc.want)
		}
	}
}
