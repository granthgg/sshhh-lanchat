// Package update checks GitHub Releases for a newer lanchat build and
// suggests the right one-line installer for the user's platform. It is the
// only part of the program that talks to the internet — chat itself never
// leaves the LAN — so the check is deliberately small, timeout-bounded, and
// fail-silent: any error simply means no notice is shown.
package update

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptrace"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	// LatestURL is the GitHub Releases API endpoint polled for the newest
	// published release.
	LatestURL = "https://api.github.com/repos/granthgg/sshhh-lanchat/releases/latest"

	// maxTagLen bounds the accepted tag: anything longer is nonsense from a
	// broken or hostile endpoint and gets rejected before parsing.
	maxTagLen = 64

	// dialTimeout bounds the TCP+TLS setup; totalTimeout bounds the whole
	// request. The check runs on a background goroutine, but a hanging
	// connection must not outlive the session by minutes.
	dialTimeout  = 2 * time.Second
	totalTimeout = 4 * time.Second

	// maxBody caps how much of the response is read. The full release JSON
	// can be tens of kilobytes (notes, asset lists) but tag_name sits at
	// the very top — a small cap makes a hostile or broken endpoint cheap.
	maxBody = 16 << 10
)

// client is a private transport: HTTPS only, no redirects followed (the API
// answers 200 directly; a redirect here would mean a proxy or MITM, and
// silently following it would leak the request somewhere unexpected).
var client = &http.Client{
	Timeout: totalTimeout,
	Transport: &http.Transport{
		// Force IPv4: GitHub's API has AAAA records, and on the LANs this
		// tool lives on (campus, office, hotspot) broken IPv6 is common —
		// the dial would stall on v6 before ever trying v4.
		DialContext: (&net.Dialer{
			Timeout:   dialTimeout,
			KeepAlive: -1, // one shot; keep-alive only helps repeat callers
		}).DialContext,
		ForceAttemptHTTP2:   true,
		TLSHandshakeTimeout: dialTimeout,
	},
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

// Latest returns the version of the newest GitHub release, without the
// leading "v" (e.g. "2.6.0"). baseURL is normally LatestURL; tests point it
// at an httptest server. Any failure — offline, DNS, timeout, bad payload —
// is an error, and callers are expected to ignore it silently.
func Latest(ctx context.Context, baseURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "lanchat-update-check")

	// Abort the in-flight request the moment the session ends: waiting on a
	// connection that can no longer report anything just delays goroutine
	// teardown.
	var conn net.Conn
	trace := &httptrace.ClientTrace{
		GotConn: func(info httptrace.GotConnInfo) { conn = info.Conn },
	}
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))
	go func() {
		<-ctx.Done()
		if conn != nil {
			conn.Close()
		}
	}()

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status %s", resp.Status)
	}

	// tag_name is the first field in GitHub's release payload, so decoding
	// from a bounded reader costs almost nothing and stops early.
	var payload struct {
		TagName string `json:"tag_name"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(nil, resp.Body, maxBody))
	if err := dec.Decode(&payload); err != nil {
		return "", fmt.Errorf("bad release payload: %w", err)
	}
	return normalizeTag(payload.TagName)
}

// normalizeTag validates and strips a release tag ("v2.6.0" → "2.6.0"). It
// is deliberately strict: the value came off the network, and everything
// downstream (banner lines, the update hint) prints it verbatim into the
// user's terminal — so control characters and ANSI escapes are refused
// outright rather than sanitized after the fact.
func normalizeTag(tag string) (string, error) {
	if len(tag) == 0 || len(tag) > maxTagLen {
		return "", fmt.Errorf("implausible tag length %d", len(tag))
	}
	for _, r := range tag {
		if r < 0x20 || r == 0x7f {
			return "", fmt.Errorf("tag contains control characters")
		}
	}
	tag = strings.TrimPrefix(tag, "v")
	if _, _, _, ok := parseSemver(tag); !ok {
		return "", fmt.Errorf("tag %q is not a release version", tag)
	}
	return tag, nil
}

// Newer reports whether latest is a strictly higher stable release than
// current, comparing semantic versions numerically ("2.10.0" > "2.9.0",
// unlike a string sort). Anything unparseable — dev builds, snapshots —
// reports false: never nag someone running a local build. Pre-release tags
// (2.6.0-rc1) are never suggested over a stable current version.
func Newer(current, latest string) bool {
	cM, cm, cp, cok := parseSemver(current)
	lM, lm, lp, lok := parseSemver(latest)
	if !cok || !lok {
		return false
	}
	switch {
	case lM != cM:
		return lM > cM
	case lm != cm:
		return lm > cm
	default:
		return lp > cp
	}
}

// parseSemver splits "major.minor.patch" into its numeric parts. A leading
// "v" is tolerated; any pre-release/build suffix ("-rc1", "+meta") fails —
// we only reason about stable releases.
func parseSemver(v string) (major, minor, patch int, ok bool) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return 0, 0, 0, false
	}
	nums := [3]int{}
	for i, p := range parts {
		if p == "" || len(p) > 6 {
			return 0, 0, 0, false
		}
		for _, r := range p {
			if r < '0' || r > '9' {
				return 0, 0, 0, false
			}
		}
		n, err := strconv.Atoi(p)
		if err != nil {
			return 0, 0, 0, false
		}
		nums[i] = n
	}
	return nums[0], nums[1], nums[2], true
}

// Command is the update one-liner to suggest for the current platform. It
// points at the same checksum-verifying installers documented in the README,
// served from the project's own domain (which redirects to the raw scripts).
// Building from source? The git path keeps you honest instead.
func Command() string {
	if runtime.GOOS == "windows" {
		return "irm https://sshhh-lanchat.tech/install.ps1 | iex"
	}
	return "curl -fsSL https://sshhh-lanchat.tech/install.sh | sh"
}
