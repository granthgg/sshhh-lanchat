package update

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewer(t *testing.T) {
	cases := []struct {
		current, latest string
		want            bool
	}{
		{"2.5.0", "2.6.0", true},
		{"2.5.0", "2.10.0", true},     // numeric, not lexicographic
		{"2.9.0", "2.10.0", true},     // would be false under string compare
		{"2.5.0", "3.0.0", true},      // major bump
		{"2.5.0", "2.5.1", true},      // patch bump
		{"2.5.0", "2.5.0", false},     // same
		{"2.6.0", "2.5.0", false},     // current ahead (dev build of next)
		{"v2.5.0", "v2.6.0", true},    // v prefixes tolerated
		{"2.5.0", "2.6.0-rc1", false}, // pre-releases never suggested
		{"2.6.0-rc1", "2.6.0", false}, // unparseable current: never nag
		{"dev", "2.6.0", false},       // local build
		{"", "2.6.0", false},
		{"2.5.0", "", false},
		{"2.5.0", "garbage", false},
		{"2.5", "2.6", false}, // two-part versions are not semver
		{"2.5.0.1", "2.6.0", false},
	}
	for _, c := range cases {
		if got := Newer(c.current, c.latest); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.current, c.latest, got, c.want)
		}
	}
}

func TestNormalizeTag(t *testing.T) {
	good := map[string]string{
		"v2.6.0": "2.6.0",
		"2.6.0":  "2.6.0",
		"v0.0.1": "0.0.1",
	}
	for in, want := range good {
		got, err := normalizeTag(in)
		if err != nil || got != want {
			t.Errorf("normalizeTag(%q) = %q, %v; want %q, nil", in, got, err, want)
		}
	}

	bad := []string{
		"",                       // empty
		"v2.6",                   // not semver
		"latest",                 // not a version
		"v2.6.0-rc1",             // pre-release
		"v2.6.0\x1b[31m",         // ANSI escape — terminal injection
		"v2.6.0\r\nspoofed-line", // CRLF injection
		strings.Repeat("9", 100), // absurd length
	}
	for _, in := range bad {
		if got, err := normalizeTag(in); err == nil {
			t.Errorf("normalizeTag(%q) = %q, nil; want error", in, got)
		}
	}
}

func TestLatest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "application/vnd.github+json" {
			t.Error("missing GitHub API Accept header")
		}
		fmt.Fprint(w, `{"tag_name":"v9.9.9","name":"other stuff"}`)
	}))
	defer srv.Close()

	got, err := Latest(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if got != "9.9.9" {
		t.Fatalf("Latest = %q, want 9.9.9", got)
	}
}

func TestLatestFailures(t *testing.T) {
	// HTTP error status.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rate limited", http.StatusForbidden)
	}))
	defer srv.Close()
	if _, err := Latest(context.Background(), srv.URL); err == nil {
		t.Error("Latest: expected error on 403, got nil")
	}

	// Malformed JSON.
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `not json`)
	}))
	defer bad.Close()
	if _, err := Latest(context.Background(), bad.URL); err == nil {
		t.Error("Latest: expected error on malformed JSON, got nil")
	}

	// Already-cancelled context must fail fast, not hang.
	done := make(chan struct{})
	go func() {
		defer close(done)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, _ = Latest(ctx, srv.URL)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Error("Latest with cancelled context did not return promptly")
	}
}

func TestCommand(t *testing.T) {
	cmd := Command()
	if !strings.Contains(cmd, "sshhh-lanchat.tech/install.") {
		t.Fatalf("Command %q does not point at the documented installer", cmd)
	}
	if strings.Contains(cmd, "\n") {
		t.Fatalf("Command %q must stay a one-liner", cmd)
	}
}
