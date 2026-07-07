// Command discover_proxy is an Alpaca discovery + registration server. It
// answers the Alpaca UDP discovery protocol on :32227 on behalf of per-device
// drivers that register via periodic unicast heartbeat.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

const token = "alpacadiscovery" // discovery request prefix; version digit follows

// registration doubles as the discovery response schema (extra fields ignored by stock clients).
type registration struct {
	AlpacaPort int    `json:"AlpacaPort"`
	UniqueID   string `json:"UniqueID,omitempty"`
	DeviceType string `json:"DeviceType,omitempty"`
	DeviceName string `json:"DeviceName,omitempty"`
}

type entry struct {
	port int
	seen time.Time
}

type server struct {
	mu  sync.Mutex
	tab map[string]*entry
	ttl time.Duration
}

func (s *server) upsert(r registration) {
	key := r.UniqueID
	if key == "" {
		key = fmt.Sprintf("p%d", r.AlpacaPort)
	}
	s.mu.Lock()
	s.tab[key] = &entry{port: r.AlpacaPort, seen: time.Now()}
	s.mu.Unlock()
}

// livePorts returns distinct ports of non-expired devices, pruning stale entries.
func (s *server) livePorts() []int {
	s.mu.Lock()
	defer s.mu.Unlock()
	now, seen := time.Now(), map[int]bool{}
	var ports []int
	for k, e := range s.tab {
		if now.Sub(e.seen) > s.ttl {
			delete(s.tab, k)
			continue
		}
		if !seen[e.port] {
			seen[e.port] = true
			ports = append(ports, e.port)
		}
	}
	return ports
}

func (s *server) handle(c *net.UDPConn, src *net.UDPAddr, p []byte) {
	if bytes.HasPrefix(bytes.TrimSpace(p), []byte(token)) { // client discovery request
		ports := s.livePorts()
		log.Printf("discovery from %s -> %d device(s): %v", src, len(ports), ports)
		for _, port := range ports {
			b, _ := json.Marshal(registration{AlpacaPort: port})
			_, _ = c.WriteToUDP(b, src) // unicast back to requester
		}
		return
	}
	var r registration // device registration / heartbeat
	if json.Unmarshal(p, &r) == nil && r.AlpacaPort != 0 {
		s.upsert(r)
		log.Printf("register %s port %d (%s)", r.UniqueID, r.AlpacaPort, r.DeviceType)
	}
}

func (s *server) serve(ctx context.Context, c *net.UDPConn) {
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
		s.handle(c, src, append([]byte(nil), buf[:n]...))
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

	s := &server{tab: map[string]*entry{}, ttl: *ttl}
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
