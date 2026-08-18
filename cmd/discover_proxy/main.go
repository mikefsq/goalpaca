// Command discover_proxy is an Alpaca discovery + registration server. It
// answers the Alpaca UDP discovery protocol on :32227 on behalf of per-device
// drivers that register via periodic unicast heartbeat (server.Heartbeat).
//
// A device on this host is answered for directly. A device on another host is
// relayed to: the proxy posts the client's address to the device's relay
// endpoint (server.DiscoveryReplyPath) and the device sends its own reply, so
// the client sees the device's real address. See DISCOVERY_RELAY.md.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"net"
	"os/signal"
	"syscall"
	"time"

	"github.com/mikefsq/goalpaca/server"
)

type proxy struct {
	reg *server.Registrations
}

func (s *proxy) handle(ctx context.Context, c *net.UDPConn, src *net.UDPAddr, p []byte) {
	kind, _ := s.reg.Datagram(p, src)
	switch kind {
	case server.DatagramProbe:
		ports := s.reg.LocalPorts()
		var remote int
		for _, e := range s.reg.Live() {
			if !e.Local {
				remote++
			}
		}
		log.Printf("discovery from %s -> %d local port(s) %v, %d remote device(s)", src, len(ports), ports, remote)
		for _, port := range ports {
			b, _ := json.Marshal(struct {
				AlpacaPort int `json:"AlpacaPort"`
			}{port})
			_, _ = c.WriteToUDP(b, src) // unicast back to requester
		}
		s.reg.Relay(ctx, src, func(e server.Registration, err error) {
			log.Printf("relay to %s %s:%d: %v", e.UniqueID, e.Addr, e.AlpacaPort, err)
		})
	case server.DatagramHeartbeat:
		// Logged at the table's rate would be every heartbeat; keep quiet.
	}
}

func (s *proxy) serve(ctx context.Context, c *net.UDPConn) {
	buf := make([]byte, 2048)
	for ctx.Err() == nil {
		_ = c.SetReadDeadline(time.Now().Add(time.Second))
		n, src, err := c.ReadFromUDP(buf)
		if err != nil {
			// The 1s read deadline paces the loop; any other persistent
			// error (e.g. a closed socket) returns immediately and must
			// not busy-spin.
			if ne, ok := err.(net.Error); !ok || !ne.Timeout() {
				time.Sleep(100 * time.Millisecond)
			}
			continue
		}
		go s.handle(ctx, c, src, append([]byte(nil), buf[:n]...))
	}
}

func main() {
	port := flag.Int("port", 32227, "discovery UDP port")
	bind := flag.String("bind", "0.0.0.0", "IPv4 bind address")
	ttl := flag.Duration("ttl", 30*time.Second, "device liveness TTL")
	v6 := flag.Bool("v6", false, "also serve IPv6 multicast discovery")
	group := flag.String("group", "ff12::00a1:9aca", "IPv6 discovery multicast group (spec: ff12::a1:9aca)")
	flag.Parse()

	bindIP := net.ParseIP(*bind)
	if bindIP == nil {
		log.Fatalf("invalid -bind address %q", *bind)
	}
	groupIP := net.ParseIP(*group)
	if groupIP == nil {
		log.Fatalf("invalid -group address %q", *group)
	}

	s := &proxy{reg: server.NewRegistrations(*ttl)}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	v4, err := net.ListenUDP("udp4", &net.UDPAddr{IP: bindIP, Port: *port})
	if err != nil {
		log.Fatalf("ipv4 bind: %v", err)
	}
	defer v4.Close()
	go s.serve(ctx, v4)
	log.Printf("alpaca discovery server on %s:%d (ttl %s)", *bind, *port, *ttl)

	if *v6 {
		// Join the (link-local scoped) group on every multicast-capable
		// interface — a nil interface joins only the system default, so a
		// multi-NIC host would miss IPv6 discovery on its other links.
		gaddr := &net.UDPAddr{IP: groupIP, Port: *port}
		joined := 0
		ifaces, _ := net.Interfaces()
		for _, ifi := range ifaces {
			if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagMulticast == 0 {
				continue
			}
			ifi := ifi
			c6, err := net.ListenMulticastUDP("udp6", &ifi, gaddr)
			if err != nil {
				log.Printf("ipv6 join on %s: %v (skipped)", ifi.Name, err)
				continue
			}
			defer c6.Close()
			go s.serve(ctx, c6)
			joined++
		}
		if joined == 0 {
			log.Fatalf("ipv6 join: no multicast-capable interface joined %s", *group)
		}
		log.Printf("ipv6 multicast on [%s]:%d (%d interfaces)", *group, *port, joined)
	}

	<-ctx.Done()
	log.Println("shutting down")
}
