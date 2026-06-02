//go:build linux || darwin

package alpacadev

import (
	"context"
	"net"
	"testing"
)

// TestReusePortCoBind proves two sockets can co-bind the same UDP port via
// reuseControl — the property Direct discovery relies on for multiple device
// processes on one host.
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
