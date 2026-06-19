package alpacadev

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"
)

// TestRunBindsConfiguredHostsBothStacks verifies that Config.Hosts binds one
// listener per address and that the server answers over each — including IPv6.
// This is the server-side guard for the fleet regression where restricting the
// bind to a single IPv4 address silently dropped IPv6 reachability.
func TestRunBindsConfiguredHostsBothStacks(t *testing.T) {
	port := freePort(t)
	s := New(Config{
		AlpacaPort: port,
		Hosts:      []string{"127.0.0.1", "::1"},
		Discovery:  DiscoveryConfig{Mode: DiscoveryOff},
	})
	if err := s.Register(CameraType, 0, newFakeCamera()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = s.Run(ctx) }()

	for _, host := range []string{"127.0.0.1", "[::1]"} {
		url := fmt.Sprintf("http://%s:%d/management/apiversions", host, port)
		if !getOK(url) {
			t.Errorf("server not reachable at %s", url)
		}
	}
}

// getOK polls url briefly (the server binds asynchronously) and reports whether it
// answers 2xx.
func getOK(url string) bool {
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode < 300 {
				return true
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

// freePort returns a currently-free TCP port.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}
