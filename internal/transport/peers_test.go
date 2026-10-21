package transport

import "testing"

func TestResolvePeersDedupAndSkipSelf(t *testing.T) {
	got := resolvePeers([]string{"100.64.0.2", " 100.64.0.2 ", "", "127.0.0.1", "fd7a::1"}, false)
	if len(got) != 1 || got[0].IP.String() != "100.64.0.2" || got[0].Port != chatPort {
		t.Fatalf("resolvePeers = %v, want [100.64.0.2:%d]", got, chatPort)
	}
}
