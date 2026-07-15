// Package transport is the peer-to-peer datagram layer.
//
// There is no server: every instance sends to, and listens on, the same UDP
// multicast group, derived from the room name. A copy of each datagram is also
// sent to the subnet broadcast address, which some networks deliver even when
// they silently drop multicast; duplicates are removed later by the (id, seq)
// dedup in package proto.
package transport

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net"

	"golang.org/x/net/ipv4"
)

// chatPort is the single UDP port every room uses. Rooms are separated by their
// multicast group address (below) and, decisively, by encryption — so a fixed
// port keeps firewall rules simple ("allow UDP 48710") without letting rooms
// bleed into each other.
const chatPort = 48710

// Transport carries encrypted frames to and from peers on the local segment.
type Transport struct {
	pc        *ipv4.PacketConn
	group     *net.UDPAddr
	bcast     *net.UDPAddr
	sendIface *net.Interface
	useBcast  bool
	joined    int
}

// groupFor maps a room name to a deterministic address in the 239.255.0.0/16
// organization-local multicast scope.
func groupFor(room string) net.IP {
	h := sha256.Sum256([]byte("tchat-v2|group|" + room))
	return net.IPv4(239, 255, h[0], h[1])
}

// Open binds the chat port, joins the room's multicast group on every usable
// interface, and returns a ready Transport. If ifaceName is non-empty only that
// interface is used. Set useBcast to also send a broadcast copy of each frame.
func Open(room, ifaceName string, useBcast bool) (*Transport, error) {
	group := &net.UDPAddr{IP: groupFor(room), Port: chatPort}

	lc := net.ListenConfig{Control: reuseControl}
	uc, err := lc.ListenPacket(context.Background(), "udp4", fmt.Sprintf("0.0.0.0:%d", chatPort))
	if err != nil {
		return nil, fmt.Errorf("bind udp/%d: %w", chatPort, err)
	}
	pc := ipv4.NewPacketConn(uc)

	all, send := usableInterfaces(ifaceName)
	joined := 0
	for i := range all {
		if err := pc.JoinGroup(&all[i], &net.UDPAddr{IP: group.IP}); err == nil {
			joined++
		}
	}

	// Loopback on: two instances on one machine (and the boss-key self-test)
	// can see each other; the LAN copy is unaffected and duplicates are deduped.
	_ = pc.SetMulticastLoopback(true)
	// TTL 1: datagrams never leave the local segment — they cannot be routed
	// off the LAN, which is both the intent and a privacy safeguard.
	_ = pc.SetMulticastTTL(1)
	if send != nil {
		_ = pc.SetMulticastInterface(send)
	}

	return &Transport{
		pc:        pc,
		group:     group,
		bcast:     &net.UDPAddr{IP: net.IPv4bcast, Port: chatPort},
		sendIface: send,
		useBcast:  useBcast,
		joined:    joined,
	}, nil
}

// Send transmits one frame to the room, over multicast and (if enabled) the
// subnet broadcast address.
func (t *Transport) Send(frame []byte) {
	_, _ = t.pc.WriteTo(frame, nil, t.group)
	if t.useBcast {
		_, _ = t.pc.WriteTo(frame, nil, t.bcast)
	}
}

// Read blocks for the next datagram, copying it into buf.
func (t *Transport) Read(buf []byte) (int, net.Addr, error) {
	n, _, src, err := t.pc.ReadFrom(buf)
	return n, src, err
}

// Close releases the underlying socket.
func (t *Transport) Close() error { return t.pc.Close() }

// Joined reports how many interfaces successfully joined the multicast group.
// Zero means multicast is unavailable and only the broadcast fallback is live.
func (t *Transport) Joined() int { return t.joined }

// usableInterfaces returns the interfaces to join the multicast group on and
// the one to send from. It keeps interfaces that are up, multicast-capable and
// have an IPv4 address. The send interface is the first non-loopback match (so
// traffic actually reaches the LAN); loopback is still joined for receiving so
// same-host instances work. If a name is given, only that interface is used.
func usableInterfaces(name string) (all []net.Interface, send *net.Interface) {
	ifs, err := net.Interfaces()
	if err != nil {
		return nil, nil
	}
	for _, ifi := range ifs {
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagMulticast == 0 {
			continue
		}
		if name != "" && ifi.Name != name {
			continue
		}
		addrs, _ := ifi.Addrs()
		has4 := false
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil {
				has4 = true
				break
			}
		}
		if !has4 {
			continue
		}
		ifi := ifi
		all = append(all, ifi)
		if send == nil && ifi.Flags&net.FlagLoopback == 0 {
			s := ifi
			send = &s
		}
	}
	if send == nil && len(all) > 0 {
		send = &all[0] // loopback-only host (offline): still works locally
	}
	return all, send
}
