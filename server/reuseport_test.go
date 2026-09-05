//go:build linux || darwin || windows

package server

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"
)

func TestReusePortCoBind(t *testing.T) {
	lc := net.ListenConfig{Control: reuseControl}
	const addr = "0.0.0.0:45227"

	a, err := lc.ListenPacket(context.Background(), "udp4", addr)
	if err != nil {
		t.Fatalf("first bind failed: %v", err)
	}
	defer a.Close()

	b, err := lc.ListenPacket(context.Background(), "udp4", addr)
	if err != nil {
		t.Fatalf("co-bind failed (SO_REUSEADDR/SO_REUSEPORT not effective): %v", err)
	}
	defer b.Close()
}

func TestReusePortBroadcastReachesEveryCoBoundSocket(t *testing.T) {
	lc := net.ListenConfig{Control: reuseControl}
	const port = 45228
	addr := net.JoinHostPort("0.0.0.0", strconv.Itoa(port))

	socks := make([]net.PacketConn, 2)
	for i := range socks {
		c, err := lc.ListenPacket(context.Background(), "udp4", addr)
		if err != nil {
			t.Fatalf("bind %d failed: %v", i, err)
		}
		defer c.Close()
		socks[i] = c
	}

	s, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: net.IPv4bcast, Port: port})
	if err != nil {
		t.Skipf("cannot send broadcast in this environment: %v", err)
	}
	defer s.Close()
	if _, err := s.Write([]byte("alpacadiscovery1")); err != nil {
		t.Skipf("broadcast send blocked in this environment (firewall?): %v", err)
	}

	// Count how many co-bound sockets saw it. Zero means this environment cannot deliver a
	// broadcast at all (a sandboxed CI runner with no broadcast-capable interface) — that is
	// not a bug in reuseControl, so skip rather than fail. One-of-two IS the real failure:
	// the port is shared but the datagram is delivered to a single socket, which would make
	// all but one co-bound responder invisible to a Discover.
	got := 0
	for _, c := range socks {
		buf := make([]byte, 64)
		_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
		n, _, err := c.ReadFrom(buf)
		if err != nil {
			continue // deadline: this socket saw nothing
		}
		if string(buf[:n]) != "alpacadiscovery1" {
			t.Errorf("got %q, want the probe", buf[:n])
		}
		got++
	}
	if got == 0 {
		t.Skip("no broadcast delivery in this environment; cannot exercise co-bound fan-out")
	}
	if got != len(socks) {
		t.Fatalf("broadcast reached %d/%d co-bound sockets — every responder must see the probe, "+
			"or only one device shows up in a Discover list", got, len(socks))
	}
}
