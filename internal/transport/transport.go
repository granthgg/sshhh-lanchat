// Package transport is the peer-to-peer datagram layer.
//
// There is no server: every instance sends to, and listens on, the same UDP
// multicast group, derived from the room name. Real-world LANs — office Wi-Fi
// with IGMP snooping, mesh networks with several access points, machines with
// both Ethernet and Wi-Fi, active VPNs — all get in the way of naive
// multicast, so delivery is layered:
//
//   - Multicast is sent on EVERY usable interface, not just one, so a machine
//     that straddles two segments reaches peers on both.
//   - A directed-broadcast copy (e.g. 192.168.1.255) is sent per interface as
//     a fallback for networks that filter multicast. Directed broadcast also
//     always egresses the right interface — unlike 255.255.255.255, which
//     follows the default route (straight into a VPN tunnel, if one is up).
//   - Interfaces are re-scanned periodically: joining a new network, waking
//     from sleep, or roaming between access points re-establishes multicast
//     membership without a restart.
//   - VPN-style point-to-point tunnels are skipped during auto-detection so
//     chat traffic stays on the LAN (pin one explicitly with -iface to
//     override).
//
// The redundant copies are expected and are removed by the (id, seq) dedup in
// package proto.
package transport

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net"
	"sync"
	"time"

	"golang.org/x/net/ipv4"
)

// chatPort is the single UDP port every room uses. Rooms are separated by their
// multicast group address (below) and, decisively, by encryption — so a fixed
// port keeps firewall rules simple ("allow UDP 48710") without letting rooms
// bleed into each other.
const chatPort = 48710

// rescanEvery is how often the interface list is refreshed. Short enough that
// an AP roam or sleep/wake recovers quickly; long enough to cost nothing.
const rescanEvery = 20 * time.Second

// Options configures Open.
type Options struct {
	Room      string // room name; determines the multicast group
	Iface     string // pin one interface by name; "" = auto-detect
	Broadcast bool   // also send directed-broadcast copies of every frame
	TTL       int    // multicast TTL; 1 = never routed off the local segment
}

// Transport carries encrypted frames to and from peers on the local segment.
type Transport struct {
	pc    *ipv4.PacketConn
	group *net.UDPAddr
	opts  Options

	mu     sync.Mutex
	send   []net.Interface // interfaces to send multicast on
	bcasts []*net.UDPAddr  // directed-broadcast destinations, one per subnet
	joined map[int]bool    // interface index → multicast group joined

	closeOnce sync.Once
	done      chan struct{}
}

// groupFor maps a room name to a deterministic address in the 239.255.0.0/16
// organization-local multicast scope.
func groupFor(room string) net.IP {
	h := sha256.Sum256([]byte("tchat-v2|group|" + room))
	a, b := groupOctets(h[:])
	return net.IPv4(239, 255, a, b)
}

// groupOctets picks the first hash pair that doesn't collide with a well-known
// 239.255.x.y address, so a room never shares its group with chatty discovery
// protocols (SSDP floods would waste CPU on decrypt failures, and our frames
// would land on every smart TV on the network).
func groupOctets(h []byte) (byte, byte) {
	for i := 0; i+1 < len(h); i += 2 {
		if !reservedPair(h[i], h[i+1]) {
			return h[i], h[i+1]
		}
	}
	return 7, 7 // 16 reserved pairs in a row: not reachable in practice
}

// reservedPair reports whether 239.255.a.b is assigned to another protocol:
// SSDP (239.255.255.250), SLPv2 (239.255.255.253), or the scope's top address.
func reservedPair(a, b byte) bool {
	return a == 255 && (b == 250 || b == 253 || b == 255)
}

// Open binds the chat port, joins the room's multicast group on every usable
// interface, and starts a background re-scan that keeps membership fresh as
// interfaces come, go, or change networks.
func Open(o Options) (*Transport, error) {
	if o.TTL < 1 {
		o.TTL = 1
	}
	if o.TTL > 255 {
		o.TTL = 255
	}

	lc := net.ListenConfig{Control: reuseControl}
	uc, err := lc.ListenPacket(context.Background(), "udp4", fmt.Sprintf("0.0.0.0:%d", chatPort))
	if err != nil {
		return nil, fmt.Errorf("bind udp/%d: %w", chatPort, err)
	}
	if u, ok := uc.(*net.UDPConn); ok {
		_ = u.SetReadBuffer(1 << 20) // ride out bursts without kernel drops
	}

	pc := ipv4.NewPacketConn(uc)
	// Loopback on: two instances on one machine can see each other; the LAN
	// copy is unaffected and duplicates are deduped.
	_ = pc.SetMulticastLoopback(true)
	// TTL 1 (the default) means datagrams cannot be routed off the local
	// segment — both the intent and a privacy safeguard. Higher values only
	// help on networks that actually route multicast between subnets.
	_ = pc.SetMulticastTTL(o.TTL)

	t := &Transport{
		pc:     pc,
		group:  &net.UDPAddr{IP: groupFor(o.Room), Port: chatPort},
		opts:   o,
		joined: make(map[int]bool),
		done:   make(chan struct{}),
	}
	t.rescan()
	go t.rescanLoop()
	return t, nil
}

// Send transmits one frame to the room: over multicast on every send
// interface, plus (if enabled) a directed-broadcast copy per subnet.
func (t *Transport) Send(frame []byte) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.send) == 0 {
		// No usable interface right now; try the default route anyway.
		_, _ = t.pc.WriteTo(frame, nil, t.group)
	}
	for i := range t.send {
		_ = t.pc.SetMulticastInterface(&t.send[i])
		_, _ = t.pc.WriteTo(frame, nil, t.group)
	}
	if t.opts.Broadcast {
		for _, b := range t.bcasts {
			_, _ = t.pc.WriteTo(frame, nil, b)
		}
		if len(t.bcasts) == 0 {
			_, _ = t.pc.WriteTo(frame, nil, &net.UDPAddr{IP: net.IPv4bcast, Port: chatPort})
		}
	}
}

// Read blocks for the next datagram, copying it into buf.
func (t *Transport) Read(buf []byte) (int, net.Addr, error) {
	n, _, src, err := t.pc.ReadFrom(buf)
	return n, src, err
}

// Close stops the re-scan loop and releases the underlying socket.
func (t *Transport) Close() error {
	t.closeOnce.Do(func() { close(t.done) })
	return t.pc.Close()
}

// Joined reports how many interfaces currently have multicast membership.
// Zero means multicast is unavailable and only the broadcast fallback is live.
func (t *Transport) Joined() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.joined)
}

// rescanLoop refreshes interface state until Close.
func (t *Transport) rescanLoop() {
	tick := time.NewTicker(rescanEvery)
	defer tick.Stop()
	for {
		select {
		case <-t.done:
			return
		case <-tick.C:
			t.rescan()
		}
	}
}

// rescan re-enumerates interfaces, joins the multicast group on any new ones,
// forgets vanished ones, and rebuilds the send and broadcast target lists.
func (t *Transport) rescan() {
	candidates, send := usableInterfaces(t.opts.Iface)

	t.mu.Lock()
	defer t.mu.Unlock()

	live := make(map[int]bool, len(candidates))
	for i := range candidates {
		idx := candidates[i].Index
		live[idx] = true
		if !t.joined[idx] {
			if err := t.pc.JoinGroup(&candidates[i], &net.UDPAddr{IP: t.group.IP}); err == nil {
				t.joined[idx] = true
			}
		}
	}
	// The kernel already dropped membership for interfaces that went away;
	// forget them so they re-join cleanly if they come back.
	for idx := range t.joined {
		if !live[idx] {
			delete(t.joined, idx)
		}
	}

	t.send = send
	t.bcasts = broadcastAddrs(send)
}

// usableInterfaces returns the interfaces to join the multicast group on
// (candidates) and the subset to send on. Candidates are up, multicast-capable
// and have an IPv4 address. Sending skips loopback and point-to-point tunnels
// (VPNs) so traffic reaches — and stays on — the LAN; loopback is still joined
// for receiving, which keeps same-host instances working. If a name is pinned,
// only that interface is used for everything, whatever its flags.
func usableInterfaces(pinned string) (candidates, send []net.Interface) {
	ifs, err := net.Interfaces()
	if err != nil {
		return nil, nil
	}
	for _, ifi := range ifs {
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagMulticast == 0 {
			continue
		}
		if pinned != "" && ifi.Name != pinned {
			continue
		}
		if !hasIPv4(ifi) {
			continue
		}
		candidates = append(candidates, ifi)
		if pinned == "" && ifi.Flags&(net.FlagLoopback|net.FlagPointToPoint) != 0 {
			continue
		}
		send = append(send, ifi)
	}
	if len(send) == 0 {
		send = candidates // loopback-only host (offline): still works locally
	}
	return candidates, send
}

func hasIPv4(ifi net.Interface) bool {
	addrs, _ := ifi.Addrs()
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil {
			return true
		}
	}
	return false
}

// broadcastAddrs computes the directed-broadcast destination for every subnet
// on the given interfaces, deduplicated (two interfaces on one subnet share an
// address).
func broadcastAddrs(ifs []net.Interface) []*net.UDPAddr {
	var out []*net.UDPAddr
	seen := make(map[string]bool)
	for i := range ifs {
		addrs, _ := ifs[i].Addrs()
		for _, a := range addrs {
			n, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			b := subnetBroadcast(n)
			if b == nil || seen[b.String()] {
				continue
			}
			seen[b.String()] = true
			out = append(out, &net.UDPAddr{IP: b, Port: chatPort})
		}
	}
	return out
}

// subnetBroadcast returns the directed-broadcast address of an IPv4 network
// (address | ^mask), or nil when the network has no usable broadcast (IPv6,
// /31, /32).
func subnetBroadcast(n *net.IPNet) net.IP {
	ip4 := n.IP.To4()
	if ip4 == nil {
		return nil
	}
	mask := n.Mask
	if len(mask) == 16 {
		mask = mask[12:]
	}
	if len(mask) != 4 {
		return nil
	}
	if ones, _ := mask.Size(); ones >= 31 {
		return nil
	}
	b := make(net.IP, 4)
	for i := 0; i < 4; i++ {
		b[i] = ip4[i] | ^mask[i]
	}
	return b
}
