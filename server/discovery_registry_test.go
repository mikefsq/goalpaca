package server

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func TestRegistrationsLocalAndRemote(t *testing.T) {
	// The "remote" device is an HTTP server on loopback; the table is told the
	// host's addresses are 10.0.0.5 alone, so loopback would count as local by
	// the loopback rule. Give the remote entry a TEST-NET reachable address in
	// its heartbeat, and point the relay at the httptest server by rewriting
	// the client transport's dial.
	var got atomic.Pointer[ReplyTarget]
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != DiscoveryReplyPath || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var tgt ReplyTarget
		_ = json.Unmarshal(body, &tgt)
		got.Store(&tgt)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer remote.Close()
	_, rport, _ := net.SplitHostPort(remote.Listener.Addr().String())
	remotePort, _ := strconv.Atoi(rport)

	reg := NewRegistrations(time.Minute)
	reg.localIPs = func() []net.IP { return []net.IP{net.ParseIP("10.0.0.5")} }
	reg.Client = &http.Client{Timeout: time.Second, Transport: &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			// Every relay request lands on the httptest server.
			return (&net.Dialer{}).DialContext(ctx, network, remote.Listener.Addr().String())
		},
	}}

	// Local device: heartbeat from the host's own address, no Address field.
	kind, rec := reg.Datagram([]byte(`{"AlpacaPort":11111,"UniqueID":"local-1","DeviceType":"Camera","DeviceName":"L"}`),
		&net.UDPAddr{IP: net.ParseIP("10.0.0.5"), Port: 50000})
	if kind != DatagramHeartbeat || rec == nil || !rec.Local || rec.AlpacaPort != 11111 {
		t.Fatalf("local datagram kind %v, %+v", kind, rec)
	}
	// Remote device: heartbeat naming its address.
	reg.Record(Heartbeat{AlpacaPort: remotePort, UniqueID: "remote-1", DeviceType: "Focuser", Address: "192.0.2.9"}, net.ParseIP("192.0.2.9"))
	// A probe is a probe.
	if k, _ := reg.Datagram([]byte("alpacadiscovery1"), &net.UDPAddr{}); k != DatagramProbe {
		t.Fatalf("probe kind %v", k)
	}
	if k, _ := reg.Datagram([]byte("hello"), &net.UDPAddr{}); k != DatagramOther {
		t.Fatalf("junk kind %v", k)
	}

	if ports := reg.LocalPorts(); len(ports) != 1 || ports[0] != 11111 {
		t.Fatalf("LocalPorts %v, want [11111]", ports)
	}
	live := reg.Live()
	if len(live) != 2 {
		t.Fatalf("live %d, want 2", len(live))
	}
	for _, e := range live {
		switch e.UniqueID {
		case "local-1":
			if !e.Local || !e.Addr.Equal(net.ParseIP("10.0.0.5")) {
				t.Fatalf("local entry %+v", e)
			}
		case "remote-1":
			if e.Local || !e.Addr.Equal(net.ParseIP("192.0.2.9")) {
				t.Fatalf("remote entry %+v", e)
			}
		}
	}

	client := &net.UDPAddr{IP: net.ParseIP("10.0.0.77"), Port: 56079}
	var relayErr error
	reg.Relay(context.Background(), client, func(_ Registration, err error) { relayErr = err })
	if relayErr != nil {
		t.Fatalf("relay: %v", relayErr)
	}
	tgt := got.Load()
	if tgt == nil || tgt.IP != "10.0.0.77" || tgt.Port != 56079 {
		t.Fatalf("relayed target %+v, want 10.0.0.77:56079", tgt)
	}
}

func TestRegistrationsExpireAndReplace(t *testing.T) {
	reg := NewRegistrations(50 * time.Millisecond)
	src := net.ParseIP("127.0.0.1")
	reg.Record(Heartbeat{AlpacaPort: 11111, UniqueID: "a"}, src)
	reg.Record(Heartbeat{AlpacaPort: 11112, UniqueID: "a"}, src)
	if ports := reg.LocalPorts(); len(ports) != 1 || ports[0] != 11112 {
		t.Fatalf("after replace: %v", ports)
	}
	time.Sleep(80 * time.Millisecond)
	if ports := reg.LocalPorts(); len(ports) != 0 {
		t.Fatalf("after expiry: %v", ports)
	}
}

func TestRelayReportsFailure(t *testing.T) {
	reg := NewRegistrations(time.Minute)
	reg.localIPs = func() []net.IP { return nil }
	reg.Client = &http.Client{Timeout: 200 * time.Millisecond}
	// A closed loopback port refuses at once; the address is not loopback in
	// the table's view only if the local rule says so, so mark it remote via a
	// non-loopback Address that still dials loopback through the transport.
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	reg.Client.Transport = &http.Transport{DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	}}
	reg.Record(Heartbeat{AlpacaPort: port, UniqueID: "dead", Address: "192.0.2.10"}, net.ParseIP("192.0.2.10"))
	var failed int
	reg.Relay(context.Background(), &net.UDPAddr{IP: net.ParseIP("10.0.0.1"), Port: 1}, func(Registration, error) { failed++ })
	if failed != 1 {
		t.Fatalf("failures %d, want 1", failed)
	}
}
