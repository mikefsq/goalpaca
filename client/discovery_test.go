package client

import (
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"

	alpaca "github.com/mikefsq/goalpaca/server"
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
	servers, err := discover(300*time.Millisecond, []*net.UDPAddr{{IP: net.ParseIP("127.0.0.1"), Port: port}})
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

// TestReadResponsesIPv6 exercises the IPv6 read/parse path (the dual-stack
// addition) over loopback, without relying on multicast routing.
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
	ts := serve(t, alpaca.FocuserType, dev)
	s := DiscoveredServer{Address: strings.TrimPrefix(ts.URL, "http://")}
	devs, err := s.ConfiguredDevices()
	if err != nil {
		t.Fatalf("ConfiguredDevices: %v", err)
	}
	if len(devs) != 1 || devs[0].DeviceType != "focuser" || devs[0].DeviceName != "F" {
		t.Fatalf("ConfiguredDevices = %+v", devs)
	}
}
