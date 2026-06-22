package client

import (
	"net/url"
	"testing"
)

// urlAuthority must percent-encode an IPv6 zone (RFC 6874) so link-local discovery
// results — the form Alpaca IPv6 discovery returns — are usable in a URL. Everything
// else passes through untouched.
func TestURLAuthority(t *testing.T) {
	cases := []struct{ in, want string }{
		{"10.0.1.176:11112", "10.0.1.176:11112"},                                                   // IPv4
		{"[2001:db8::1]:80", "[2001:db8::1]:80"},                                                   // global IPv6, no zone
		{"[fe80::1%eth0]:11112", "[fe80::1%25eth0]:11112"},                                         // link-local + zone
		{"[fe80::4a21:bff:fe51:daa1%enp86s0]:11110", "[fe80::4a21:bff:fe51:daa1%25enp86s0]:11110"}, // real interface name
		{"host.local:11111", "host.local:11111"},                                                   // hostname
		{"noport", "noport"},                                                                       // not host:port — passthrough
	}
	for _, c := range cases {
		if got := urlAuthority(c.in); got != c.want {
			t.Errorf("urlAuthority(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// The encoded authority must parse as a URL and round-trip the zone back to its raw form
// (Go decodes %25 → % in Hostname), which is what makes the dialer reach the link-local host.
func TestURLAuthorityParsesAndRoundTrips(t *testing.T) {
	u, err := url.Parse("http://" + urlAuthority("[fe80::1%eth0]:11112"))
	if err != nil {
		t.Fatalf("URL with encoded zone must parse, got %v", err)
	}
	if u.Hostname() != "fe80::1%eth0" {
		t.Errorf("Hostname() = %q, want fe80::1%%eth0", u.Hostname())
	}
	if u.Port() != "11112" {
		t.Errorf("Port() = %q, want 11112", u.Port())
	}
}

// normalizeBaseURL must produce a parseable base URL for a link-local address (it previously
// embedded the raw zone, which url.Parse rejected as an invalid escape).
func TestNormalizeBaseURLZone(t *testing.T) {
	base := normalizeBaseURL("[fe80::1%eth0]:11112")
	if _, err := url.Parse(base + "/api/v1/camera/0/connected"); err != nil {
		t.Fatalf("normalizeBaseURL produced an unparseable URL %q: %v", base, err)
	}
}
