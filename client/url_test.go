package client

import (
	"net/url"
	"testing"
)

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
