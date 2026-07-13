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

		// A Windows zone is the interface NAME, which routinely contains spaces. RFC 6874
		// allows only unreserved characters literally in a ZoneID, and url.Parse rejects a
		// raw space outright ("invalid character \" \" in host name"), so the zone body must
		// be percent-encoded — not just the '%' delimiter.
		{`[fe80::ecac:ca83:5438:3c7b%Ethernet Instance 0]:11110`, `[fe80::ecac:ca83:5438:3c7b%25Ethernet%20Instance%200]:11110`},
		{`[fe80::1%Wi-Fi]:11110`, `[fe80::1%25Wi-Fi]:11110`},                                         // '-' is unreserved: literal
		{`[fe80::1%Local Area Connection* 2]:80`, `[fe80::1%25Local%20Area%20Connection%2A%202]:80`}, // '*' is reserved
		{"[fe80::1%3]:11110", "[fe80::1%253]:11110"},                                                 // numeric zone index
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

// A Windows zone (an interface name with spaces) must parse and round-trip too. This is the
// regression: the zone body used to be embedded raw, so every management/configureddevices
// probe against a link-local Windows address failed with "invalid character \" \" in host
// name" and discovery silently skipped the host.
func TestURLAuthorityWindowsZoneParsesAndRoundTrips(t *testing.T) {
	const raw = `[fe80::ecac:ca83:5438:3c7b%Ethernet Instance 0]:11110`
	u, err := url.Parse("http://" + urlAuthority(raw) + "/management/v1/configureddevices")
	if err != nil {
		t.Fatalf("URL with an encoded Windows zone must parse, got %v", err)
	}
	// Hostname() decodes the pct-encoding, handing the dialer back the exact zone the OS gave us.
	if want := "fe80::ecac:ca83:5438:3c7b%Ethernet Instance 0"; u.Hostname() != want {
		t.Errorf("Hostname() = %q, want %q", u.Hostname(), want)
	}
	if u.Port() != "11110" {
		t.Errorf("Port() = %q, want 11110", u.Port())
	}
}

// normalizeBaseURL must produce a parseable base URL for a link-local address (it previously
// embedded the raw zone, which url.Parse rejected as an invalid escape).
func TestNormalizeBaseURLZone(t *testing.T) {
	for _, addr := range []string{
		"[fe80::1%eth0]:11112",                                  // unix-style zone
		`[fe80::ecac:ca83:5438:3c7b%Ethernet Instance 0]:11110`, // windows-style zone
	} {
		base := normalizeBaseURL(addr)
		if _, err := url.Parse(base + "/api/v1/camera/0/connected"); err != nil {
			t.Fatalf("normalizeBaseURL(%q) produced an unparseable URL %q: %v", addr, base, err)
		}
	}
}
