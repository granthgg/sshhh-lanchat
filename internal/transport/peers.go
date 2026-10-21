package transport

import (
	"context"
	"encoding/json"
	"net"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// Overlay networks such as Tailscale/WireGuard carry neither multicast nor
// broadcast, so peers there are reached with plain unicast copies of every
// frame. Targets come from -peer (explicit hosts/IPs) and, with -tailscale,
// from the online peers listed by `tailscale status --json`. Both are
// re-resolved on every rescan, so peers coming online are picked up without a
// restart. Duplicates (e.g. a peer reached by both LAN multicast and unicast)
// are removed by the (id, seq) dedup in package proto.

// resolvePeers turns host names / IPs into unicast destinations, adding the
// Tailscale peers when enabled. Our own addresses are skipped.
func resolvePeers(hosts []string, tailscale bool) []*net.UDPAddr {
	var ips []net.IP
	for _, h := range hosts {
		h = strings.TrimSpace(h)
		if h == "" {
			continue
		}
		if ip := net.ParseIP(h); ip != nil {
			ips = append(ips, ip)
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		addrs, err := net.DefaultResolver.LookupIP(ctx, "ip4", h)
		cancel()
		if err == nil {
			ips = append(ips, addrs...)
		}
	}
	if tailscale {
		ips = append(ips, tailscalePeers()...)
	}

	self := localIPs()
	seen := make(map[string]bool)
	var out []*net.UDPAddr
	for _, ip := range ips {
		ip4 := ip.To4()
		if ip4 == nil || self[ip4.String()] || seen[ip4.String()] {
			continue
		}
		seen[ip4.String()] = true
		out = append(out, &net.UDPAddr{IP: ip4, Port: chatPort})
	}
	return out
}

// tailscaleStatus is the subset of `tailscale status --json` we read.
type tailscaleStatus struct {
	Peer map[string]struct {
		TailscaleIPs []string
		Online       bool
	}
}

// tailscalePeers returns the IPv4 addresses of every online Tailscale peer, or
// nil when the CLI is missing or Tailscale isn't running.
func tailscalePeers() []net.IP {
	bin := tailscaleBinary()
	if bin == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	b, err := exec.CommandContext(ctx, bin, "status", "--json").Output()
	if err != nil {
		return nil
	}
	var st tailscaleStatus
	if json.Unmarshal(b, &st) != nil {
		return nil
	}
	var out []net.IP
	for _, p := range st.Peer {
		if !p.Online {
			continue
		}
		for _, s := range p.TailscaleIPs {
			if ip := net.ParseIP(s); ip != nil && ip.To4() != nil {
				out = append(out, ip)
			}
		}
	}
	return out
}

// tailscaleBinary finds the tailscale CLI on PATH or in its usual install
// locations (the macOS app bundle doesn't put it on PATH).
func tailscaleBinary() string {
	if p, err := exec.LookPath("tailscale"); err == nil {
		return p
	}
	var candidates []string
	switch runtime.GOOS {
	case "darwin":
		candidates = []string{"/Applications/Tailscale.app/Contents/MacOS/Tailscale", "/usr/local/bin/tailscale", "/opt/homebrew/bin/tailscale"}
	case "windows":
		candidates = []string{`C:\Program Files\Tailscale\tailscale.exe`}
	default:
		candidates = []string{"/usr/bin/tailscale", "/usr/local/bin/tailscale"}
	}
	for _, c := range candidates {
		if p, err := exec.LookPath(c); err == nil {
			return p
		}
	}
	return ""
}

// localIPs is the set of this host's own IPv4 addresses, so we never unicast
// to ourselves.
func localIPs() map[string]bool {
	m := make(map[string]bool)
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil {
			m[n.IP.To4().String()] = true
		}
	}
	return m
}
