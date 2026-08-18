package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"time"
)

// alpacaDiscoveryPort is the fixed Alpaca UDP discovery port.
const alpacaDiscoveryPort = 32227

// alpacaV6Group is the Alpaca discovery IPv6 multicast group (matches
// discover_proxy's group so the two interoperate).
const alpacaV6Group = "ff12::00a1:9aca"

// discoveryProbe is the prefix of the Alpaca discovery request datagram
// ("alpacadiscovery1" for protocol version 1).
const discoveryProbe = "alpacadiscovery"

// directResponse is the Direct-mode reply: just the Alpaca HTTP port.
type directResponse struct {
	AlpacaPort int `json:"AlpacaPort"`
}

// Heartbeat is the Register-mode registration datagram, one per device,
// unicast to the discovery server or orchestrator named by
// DiscoveryConfig.ServerAddr. The receiver answers discovery probes for the
// device: directly when the device is on its own host, and through the relay
// endpoint (DiscoveryReplyPath) when it is not.
//
// AlpacaPort is the bound port, so a device that scanned for a port reports the
// one it got. Address is the IP the device is reachable at when it knows one
// (Config.Hosts names a single address); when empty the receiver uses the
// datagram's source address, which is what a routed peer sees anyway. Instance
// is the host's name for the device (DiscoveryConfig.Instance), the join key
// an orchestrator uses to match the registration to its own entry.
type Heartbeat struct {
	AlpacaPort int    `json:"AlpacaPort"`
	UniqueID   string `json:"UniqueID"`
	DeviceType string `json:"DeviceType"`
	DeviceName string `json:"DeviceName"`
	Instance   string `json:"Instance,omitempty"`
	Address    string `json:"Address,omitempty"`
}

func (s *Server) startDiscovery(ctx context.Context) {
	switch s.cfg.Discovery.Mode {
	case DiscoveryOff:
		return
	case DiscoveryDirect:
		s.runDirectDiscovery(ctx)
	case DiscoveryRegister:
		s.runRegisterDiscovery(ctx)
	}
}

// runDirectDiscovery answers discovery probes with this server's Alpaca port,
// with no dependency on a discovery server. It serves IPv4 broadcast (always)
// and IPv6 multicast (when DiscoveryConfig.EnableIPv6 is set).
//
// The IPv4 socket is bound with SO_REUSEADDR/SO_REUSEPORT (see reuseControl) so
// several device processes can share 32227 on one host; each answers broadcast
// probes with its own port. (Directed unicast probes reach only one of them —
// use Register mode with discovery_proxy if that matters.)
func (s *Server) runDirectDiscovery(ctx context.Context) {
	// IPv4 broadcast responder.
	lc := net.ListenConfig{Control: reuseControl}
	if pc, err := lc.ListenPacket(ctx, "udp4", fmt.Sprintf("0.0.0.0:%d", alpacaDiscoveryPort)); err != nil {
		s.logf("goalpaca: direct discovery (IPv4) listen failed: %v", err)
	} else {
		go s.serveDiscovery(ctx, pc.(*net.UDPConn))
	}

	// IPv6 multicast responder (opt-in, best-effort).
	if s.cfg.Discovery.EnableIPv6 {
		gaddr := &net.UDPAddr{IP: net.ParseIP(alpacaV6Group), Port: alpacaDiscoveryPort}
		if c6, err := net.ListenMulticastUDP("udp6", nil, gaddr); err != nil {
			s.logf("goalpaca: direct discovery (IPv6) join failed: %v", err)
		} else {
			go s.serveDiscovery(ctx, c6)
		}
	}

	<-ctx.Done()
}

// serveDiscovery reads probes on conn and unicasts the AlpacaPort response back
// to each requester, until ctx is cancelled.
func (s *Server) serveDiscovery(ctx context.Context, conn *net.UDPConn) {
	defer conn.Close()
	go func() {
		<-ctx.Done()
		_ = conn.Close() // unblock ReadFromUDP on shutdown
	}()

	resp, _ := json.Marshal(directResponse{AlpacaPort: s.advertisedPort()})
	buf := make([]byte, 1024)
	for {
		n, from, err := conn.ReadFromUDP(buf)
		if err != nil {
			if ctx.Err() != nil {
				return // shutting down
			}
			// Back off briefly: a persistent read error (e.g. a closed or
			// broken socket that isn't the shutdown path) must not busy-spin.
			time.Sleep(100 * time.Millisecond)
			continue
		}
		if strings.HasPrefix(strings.ToLower(string(buf[:n])), discoveryProbe) {
			_, _ = conn.WriteToUDP(resp, from)
		}
	}
}

// runRegisterDiscovery sends a periodic unicast heartbeat for each device to the
// configured discovery server. Never broadcasts.
func (s *Server) runRegisterDiscovery(ctx context.Context) {
	if s.cfg.Discovery.ServerAddr == "" {
		s.logf("goalpaca: register discovery has no ServerAddr; disabled")
		return
	}
	raddr, err := net.ResolveUDPAddr("udp", s.cfg.Discovery.ServerAddr)
	if err != nil {
		s.logf("goalpaca: register discovery resolve failed: %v", err)
		return
	}
	// The relay endpoint accepts requests from this peer alone; record it
	// before the first datagram goes out so no request arrives ahead of it.
	s.relayPeer.Store(&raddr.IP)
	conn, err := net.DialUDP("udp", nil, raddr)
	if err != nil {
		s.logf("goalpaca: register discovery dial failed: %v", err)
		return
	}
	defer conn.Close()

	ticker := time.NewTicker(s.cfg.Discovery.Interval)
	defer ticker.Stop()

	s.sendHeartbeats(conn) // send once immediately
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.sendHeartbeats(conn)
		}
	}
}

func (s *Server) sendHeartbeats(conn *net.UDPConn) {
	addr := s.reachableAddress()
	s.mu.RLock()
	var hbs [][]byte
	for _, rd := range s.order {
		hb, _ := json.Marshal(Heartbeat{
			AlpacaPort: s.advertisedPort(),
			UniqueID:   rd.dev.UniqueID(),
			DeviceType: string(rd.typ),
			DeviceName: rd.dev.Name(),
			Instance:   s.cfg.Discovery.Instance,
			Address:    addr,
		})
		hbs = append(hbs, hb)
	}
	s.mu.RUnlock()
	for _, hb := range hbs {
		_, _ = conn.Write(hb)
	}
}

// reachableAddress is the address a registration carries: the one host in
// Config.Hosts when it is a single specific IP, since a device told to bind one
// address is reachable there and possibly nowhere else. Otherwise it is empty
// and the receiver falls back to the datagram's source address.
func (s *Server) reachableAddress() string {
	if len(s.cfg.Hosts) != 1 {
		return ""
	}
	ip := net.ParseIP(s.cfg.Hosts[0])
	if ip == nil || ip.IsUnspecified() {
		return ""
	}
	return ip.String()
}

// advertisedPort is the port discovery responses and heartbeats carry: the
// actual bound port once Run has bound (covers Config.AlpacaPort 0), else the
// configured one.
func (s *Server) advertisedPort() int {
	if p := s.Port(); p != 0 {
		return p
	}
	return s.cfg.AlpacaPort
}
