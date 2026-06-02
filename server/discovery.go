package alpacadev

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

// heartbeat is the Register-mode unicast schema (shared with the discovery
// server). One is sent per registered device.
type heartbeat struct {
	AlpacaPort int    `json:"AlpacaPort"`
	UniqueID   string `json:"UniqueID"`
	DeviceType string `json:"DeviceType"`
	DeviceName string `json:"DeviceName"`
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
		fmt.Printf("alpacadev: direct discovery (IPv4) listen failed: %v\n", err)
	} else {
		go s.serveDiscovery(ctx, pc.(*net.UDPConn))
	}

	// IPv6 multicast responder (opt-in, best-effort).
	if s.cfg.Discovery.EnableIPv6 {
		gaddr := &net.UDPAddr{IP: net.ParseIP(alpacaV6Group), Port: alpacaDiscoveryPort}
		if c6, err := net.ListenMulticastUDP("udp6", nil, gaddr); err != nil {
			fmt.Printf("alpacadev: direct discovery (IPv6) join failed: %v\n", err)
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

	resp, _ := json.Marshal(directResponse{AlpacaPort: s.cfg.AlpacaPort})
	buf := make([]byte, 1024)
	for {
		n, from, err := conn.ReadFromUDP(buf)
		if err != nil {
			if ctx.Err() != nil {
				return // shutting down
			}
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
		fmt.Printf("alpacadev: register discovery has no ServerAddr; disabled\n")
		return
	}
	raddr, err := net.ResolveUDPAddr("udp", s.cfg.Discovery.ServerAddr)
	if err != nil {
		fmt.Printf("alpacadev: register discovery resolve failed: %v\n", err)
		return
	}
	conn, err := net.DialUDP("udp", nil, raddr)
	if err != nil {
		fmt.Printf("alpacadev: register discovery dial failed: %v\n", err)
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
	s.mu.RLock()
	order := append([]*registeredDevice(nil), s.order...)
	s.mu.RUnlock()
	for _, rd := range order {
		hb, _ := json.Marshal(heartbeat{
			AlpacaPort: s.cfg.AlpacaPort,
			UniqueID:   rd.dev.UniqueID(),
			DeviceType: string(rd.typ),
			DeviceName: rd.dev.Name(),
		})
		_, _ = conn.Write(hb)
	}
}
