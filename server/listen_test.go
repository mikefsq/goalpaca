package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

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

func TestPortScan(t *testing.T) {
	base := 40000 + int(time.Now().UnixNano()%1000)*10 // avoid collisions across runs
	mk := func() *Server {
		s := New(Config{AlpacaPort: 0, PortScanBase: base, PortScanLimit: 10,
			Discovery: DiscoveryConfig{Mode: DiscoveryOff}, ServerName: "scan"})
		if err := s.Register(customType, 0, newTestFocuser()); err != nil {
			t.Fatal(err)
		}
		return s
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a, b := mk(), mk()
	go a.Run(ctx)
	waitPort(t, a)
	go b.Run(ctx)
	waitPort(t, b)
	if a.Port() < base || a.Port() >= base+10 || b.Port() < base || b.Port() >= base+10 {
		t.Errorf("ports %d %d outside %d..%d", a.Port(), b.Port(), base, base+9)
	}
	if a.Port() == b.Port() {
		t.Errorf("both servers bound %d", a.Port())
	}
	if a.Port() != base {
		t.Errorf("first server should take the base %d, got %d", base, a.Port())
	}
	// A server with no base and port 0 is OS-assigned, outside the scan range.
	c := New(Config{AlpacaPort: 0, Discovery: DiscoveryConfig{Mode: DiscoveryOff}, ServerName: "os"})
	if err := c.Register(customType, 0, newTestFocuser()); err != nil {
		t.Fatal(err)
	}
	go c.Run(ctx)
	waitPort(t, c)
	if c.Port() == 0 {
		t.Error("OS-assigned port not reported")
	}
	// A full range is an error, not a hang.
	full := New(Config{AlpacaPort: 0, PortScanBase: a.Port(), PortScanLimit: 2,
		Discovery: DiscoveryConfig{Mode: DiscoveryOff}, ServerName: "full"})
	if err := full.Register(customType, 0, newTestFocuser()); err != nil {
		t.Fatal(err)
	}
	if err := full.Run(ctx); err == nil || !strings.Contains(err.Error(), "no free port") {
		t.Errorf("full range: err = %v", err)
	}
}

func waitPort(t *testing.T, s *Server) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if s.Port() != 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("server did not bind")
}
