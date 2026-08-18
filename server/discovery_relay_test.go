package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// registerModeServer runs a Register-mode server whose discovery server is a
// UDP socket the test holds, and returns both plus the first Heartbeat. The
// server binds an OS-assigned HTTP port and one simulated device.
func registerModeServer(t *testing.T, hosts []string) (*Server, *net.UDPConn, Heartbeat, *bytes.Buffer) {
	t.Helper()
	orch, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("orchestrator socket: %v", err)
	}
	t.Cleanup(func() { orch.Close() })

	var logBuf bytes.Buffer
	s := New(Config{
		AlpacaPort: 0,
		Hosts:      hosts,
		Discovery: DiscoveryConfig{
			Mode:       DiscoveryRegister,
			ServerAddr: orch.LocalAddr().String(),
			Interval:   50 * time.Millisecond,
			Instance:   "bench-camera",
		},
		ServerName: "relay-test",
		Logger:     log.New(&logBuf, "", 0),
	})
	if err := s.Register(CameraType, 0, newFakeCamera()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = s.Run(ctx) }()

	_ = orch.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, 2048)
	n, from, err := orch.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("no heartbeat: %v", err)
	}
	var hb Heartbeat
	if err := json.Unmarshal(buf[:n], &hb); err != nil {
		t.Fatalf("heartbeat decode: %v (%q)", err, buf[:n])
	}
	if from.Port == alpacaDiscoveryPort {
		t.Fatalf("heartbeat came from port %d; register mode must not bind the discovery port", from.Port)
	}
	return s, orch, hb, &logBuf
}

// TestRegisterModeBindsNoDiscoverySocket: a device launched in Register mode
// registers over an ephemeral socket and never binds UDP 32227, which stays
// free for the orchestrator on the same host. The test holds 32227 exclusively
// where it can, so any bind attempt by the server would fail and be logged.
func TestRegisterModeBindsNoDiscoverySocket(t *testing.T) {
	hold, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: alpacaDiscoveryPort})
	if err != nil {
		t.Logf("32227 is in use on this host (%v); the source-port check still applies", err)
	} else {
		defer hold.Close()
	}
	s, _, hb, logBuf := registerModeServer(t, nil)
	if hb.AlpacaPort != s.Port() || hb.AlpacaPort == 0 {
		t.Fatalf("heartbeat AlpacaPort %d, bound %d", hb.AlpacaPort, s.Port())
	}
	if hb.Instance != "bench-camera" {
		t.Fatalf("heartbeat Instance %q", hb.Instance)
	}
	if hb.Address != "" {
		t.Fatalf("heartbeat Address %q with no Hosts; want empty", hb.Address)
	}
	if hb.DeviceType != string(CameraType) || hb.UniqueID == "" {
		t.Fatalf("heartbeat %+v", hb)
	}
	if strings.Contains(logBuf.String(), "listen") {
		t.Fatalf("register mode logged a listen: %s", logBuf.String())
	}
}

// TestHeartbeatCarriesSingleHost: with one specific Host the registration
// names it as the reachable address.
func TestHeartbeatCarriesSingleHost(t *testing.T) {
	_, _, hb, _ := registerModeServer(t, []string{"127.0.0.1"})
	if hb.Address != "127.0.0.1" {
		t.Fatalf("heartbeat Address %q, want 127.0.0.1", hb.Address)
	}
}

func postReply(t *testing.T, s *Server, body string) *http.Response {
	t.Helper()
	url := fmt.Sprintf("http://127.0.0.1:%d%s", s.Port(), DiscoveryReplyPath)
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp
}

// TestDiscoveryReplyEndpoint: a POST from the discovery server's address makes
// the device send its standard reply to the given target, and the checks on
// method, body, and target hold.
func TestDiscoveryReplyEndpoint(t *testing.T) {
	s, _, _, _ := registerModeServer(t, nil)

	client, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	cport := client.LocalAddr().(*net.UDPAddr).Port

	body := fmt.Sprintf(`{"ip":"127.0.0.1","port":%d}`, cport)
	if resp := postReply(t, s, body); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("POST %s: %d", body, resp.StatusCode)
	}
	_ = client.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 256)
	n, _, err := client.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("no relayed reply: %v", err)
	}
	want := `{"AlpacaPort":` + strconv.Itoa(s.Port()) + `}`
	if string(buf[:n]) != want {
		t.Fatalf("relayed reply %q, want %q", buf[:n], want)
	}

	// Method, body, and target checks.
	url := fmt.Sprintf("http://127.0.0.1:%d%s", s.Port(), DiscoveryReplyPath)
	if resp, _ := http.Get(url); resp == nil || resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET: %v", resp)
	}
	for _, bad := range []string{
		`not json`,
		`{"ip":"224.0.0.1","port":5000}`,
		`{"ip":"255.255.255.255","port":5000}`,
		`{"ip":"0.0.0.0","port":5000}`,
		`{"ip":"127.0.0.1","port":0}`,
		`{"ip":"127.0.0.1","port":70000}`,
	} {
		if resp := postReply(t, s, bad); resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("POST %s: %d, want 400", bad, resp.StatusCode)
		}
	}
}

// TestDiscoveryReplyRefusesOtherCallers: a device registered with a remote
// discovery server refuses a relay request from anyone else, here the test's
// own loopback client against a TEST-NET peer.
func TestDiscoveryReplyRefusesOtherCallers(t *testing.T) {
	s := New(Config{
		AlpacaPort: 0,
		Discovery:  DiscoveryConfig{Mode: DiscoveryRegister, ServerAddr: "192.0.2.1:32227", Interval: time.Hour},
		ServerName: "relay-test",
	})
	if err := s.Register(CameraType, 0, newFakeCamera()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = s.Run(ctx) }()
	deadline := time.Now().Add(5 * time.Second)
	for (s.Port() == 0 || s.relayPeer.Load() == nil) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if s.Port() == 0 || s.relayPeer.Load() == nil {
		t.Fatal("server did not start")
	}
	if resp := postReply(t, s, `{"ip":"127.0.0.1","port":5000}`); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("POST from a non-peer: %d, want 403", resp.StatusCode)
	}
}

// TestDiscoveryReplyAbsentOutsideRegisterMode: Direct and Off servers have no
// relay endpoint at all.
func TestDiscoveryReplyAbsentOutsideRegisterMode(t *testing.T) {
	for _, mode := range []DiscoveryMode{DiscoveryDirect, DiscoveryOff} {
		s := New(Config{AlpacaPort: 0, Discovery: DiscoveryConfig{Mode: mode}, ServerName: "relay-test"})
		if err := s.Register(CameraType, 0, newFakeCamera()); err != nil {
			t.Fatal(err)
		}
		// Route the request without running the server: the mode check comes
		// first, so no socket is needed to see the 404.
		req, _ := http.NewRequest(http.MethodPost, DiscoveryReplyPath, strings.NewReader(`{"ip":"127.0.0.1","port":1}`))
		req.RemoteAddr = "127.0.0.1:1"
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("mode %v: %d, want 404", mode, rec.Code)
		}
	}
}

// TestDiscoveryReplyRateLimit: a burst beyond the bucket draws 429s.
func TestDiscoveryReplyRateLimit(t *testing.T) {
	var g rateGate
	now := time.Unix(1_700_000_000, 0)
	for i := 0; i < relayBurst; i++ {
		if !g.take(now) {
			t.Fatalf("token %d refused within the burst", i)
		}
	}
	if g.take(now) {
		t.Fatal("token beyond the burst granted")
	}
	if !g.take(now.Add(time.Second)) {
		t.Fatal("no refill after a second")
	}
}
