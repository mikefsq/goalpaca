package client

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mikefsq/goalpaca/server"
)

// startFakeResponder runs a loopback UDP discovery responder and returns its port.
func startFakeResponder(t *testing.T, alpacaPort int) int {
	t.Helper()
	pc, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("responder listen: %v", err)
	}
	t.Cleanup(func() { _ = pc.Close() })
	conn := pc.(*net.UDPConn)
	resp, _ := json.Marshal(map[string]int{"AlpacaPort": alpacaPort})
	go func() {
		buf := make([]byte, 1024)
		for {
			n, from, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			if strings.HasPrefix(strings.ToLower(string(buf[:n])), "alpacadiscovery") {
				_, _ = conn.WriteToUDP(resp, from)
			}
		}
	}()
	return conn.LocalAddr().(*net.UDPAddr).Port
}

func TestDiscover(t *testing.T) {
	port := startFakeResponder(t, 11111)
	servers, err := discover(context.Background(), 300*time.Millisecond, []*net.UDPAddr{{IP: net.ParseIP("127.0.0.1"), Port: port}})
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	found := false
	for _, s := range servers {
		if s.AlpacaPort == 11111 {
			found = true
		}
	}
	if !found {
		t.Fatalf("discover did not find the responder; got %+v", servers)
	}
}

func TestReadResponsesIPv6(t *testing.T) {
	cli, err := net.ListenUDP("udp6", &net.UDPAddr{IP: net.IPv6loopback, Port: 0})
	if err != nil {
		t.Skipf("IPv6 not available: %v", err)
	}
	defer cli.Close()
	srv, err := net.ListenUDP("udp6", &net.UDPAddr{IP: net.IPv6loopback, Port: 0})
	if err != nil {
		t.Skipf("IPv6 not available: %v", err)
	}
	defer srv.Close()

	resp, _ := json.Marshal(map[string]int{"AlpacaPort": 11111})
	if _, err := srv.WriteToUDP(resp, cli.LocalAddr().(*net.UDPAddr)); err != nil {
		t.Fatalf("send reply: %v", err)
	}
	servers := readResponses(cli, 500*time.Millisecond)
	found := false
	for _, s := range servers {
		if s.AlpacaPort == 11111 {
			found = true
		}
	}
	if !found {
		t.Fatalf("readResponses (IPv6) did not parse the reply; got %+v", servers)
	}
}

func TestConfiguredDevices(t *testing.T) {
	dev := &fakeFocuser{}
	dev.DevName = "F"
	dev.IfaceVer = 4
	ts := serve(t, server.FocuserType, dev)
	s := DiscoveredServer{Address: strings.TrimPrefix(ts.URL, "http://")}
	devs, err := s.ConfiguredDevices()
	if err != nil {
		t.Fatalf("ConfiguredDevices: %v", err)
	}
	if len(devs) != 1 || devs[0].DeviceType != "focuser" || devs[0].DeviceName != "F" {
		t.Fatalf("ConfiguredDevices = %+v", devs)
	}
}

func TestDiscoverContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	_, err := DiscoverContext(ctx, 10*time.Second)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("DiscoverContext after cancel: err = %v, want context.Canceled", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("DiscoverContext took %v after cancel; want prompt return", elapsed)
	}
}

func TestConfiguredDevicesContextCancel(t *testing.T) {
	hung := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done() // never answer
	}))
	defer hung.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := DiscoveredServer{Address: hung.Listener.Addr().String()}.ConfiguredDevicesContext(ctx)
	if err == nil {
		t.Fatal("ConfiguredDevicesContext on hung server: want error, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want context.DeadlineExceeded", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("took %v; want prompt return on cancel", elapsed)
	}
}
