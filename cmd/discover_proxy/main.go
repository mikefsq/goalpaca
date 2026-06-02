// Alpaca discovery + registration server.
// Answers the Alpaca UDP discovery protocol on :32227 on behalf of
// per-device drivers that register via periodic unicast heartbeat.
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
	group := flag.String("group", "ff12::00a1:9aca", "IPv6 discovery multicast group (verify against spec)")
	flag.Parse()

	s := &server{tab: map[string]*entry{}, ttl: *ttl}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	v4, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP(*bind), Port: *port})
	if err != nil {
		log.Fatalf("ipv4 bind: %v", err)
	}
	defer v4.Close()
	go s.serve(ctx, v4)
	log.Printf("alpaca discovery server on %s:%d (ttl %s)", *bind, *port, *ttl)

	if *v6 {
		c6, err := net.ListenMulticastUDP("udp6", nil, &net.UDPAddr{IP: net.ParseIP(*group), Port: *port})
		if err != nil {
			log.Fatalf("ipv6 join: %v", err)
		}
		defer c6.Close()
		go s.serve(ctx, c6)
		log.Printf("ipv6 multicast on [%s]:%d", *group, *port)
	}

	<-ctx.Done()
	log.Println("shutting down")
}
