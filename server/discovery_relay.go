package server

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"sync"
	"time"
)

// DiscoveryReplyPath is the relay endpoint a Register-mode server exposes:
// POST with a JSON ReplyTarget makes the server send its standard discovery
// reply ({"AlpacaPort":n}) to that address. The discovery server or
// orchestrator the device registered with calls it when a probe arrives from
// a host the device is not on, so the client sees the reply come from the
// device's own address, which the protocol has no other way to convey. It is
// a goalpaca extension, not part of Alpaca.
//
// The endpoint makes the server emit UDP to a caller-supplied address, so it
// is constrained: it exists only in Register mode, it accepts requests only
// from the address of DiscoveryConfig.ServerAddr, the target has to be a
// unicast address, and it is rate limited. Any other request is refused.
const DiscoveryReplyPath = "/discovery/reply"

// ReplyTarget is the body of a DiscoveryReplyPath request: the client's probe
// source, where the discovery reply is to be sent.
type ReplyTarget struct {
	IP   string `json:"ip"`
	Port int    `json:"port"`
}

// relayBurst and relayRate bound the endpoint: a client discovering sends a
// handful of probes, so a burst well above that with a slow refill lets every
// real probe through and turns a flood into 429s.
const (
	relayBurst = 20
	relayRate  = 5 // tokens per second
)

// handleDiscoveryReply serves DiscoveryReplyPath.
func (s *Server) handleDiscoveryReply(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Discovery.Mode != DiscoveryRegister {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.relayCallerAllowed(r.RemoteAddr) {
		http.Error(w, "discovery reply requests are accepted from the registered discovery server only", http.StatusForbidden)
		return
	}
	if !s.relayGate.take(time.Now()) {
		http.Error(w, "too many discovery reply requests", http.StatusTooManyRequests)
		return
	}
	var t ReplyTarget
	if err := json.NewDecoder(io.LimitReader(r.Body, 1024)).Decode(&t); err != nil {
		http.Error(w, "body must be JSON {\"ip\":..., \"port\":...}", http.StatusBadRequest)
		return
	}
	ip := net.ParseIP(t.IP)
	if ip == nil || !isUnicastTarget(ip) || t.Port < 1 || t.Port > 65535 {
		http.Error(w, "target must be a unicast IP and a port in 1..65535", http.StatusBadRequest)
		return
	}
	if err := s.sendDiscoveryReply(&net.UDPAddr{IP: ip, Port: t.Port}); err != nil {
		http.Error(w, "send failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// relayCallerAllowed reports whether remoteAddr is the discovery server the
// device registered with. A loopback server admits any loopback caller, since
// "localhost" resolves to one loopback address while the request may arrive
// on the other.
func (s *Server) relayCallerAllowed(remoteAddr string) bool {
	peer := s.relayPeer.Load()
	if peer == nil {
		return false
	}
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return false
	}
	caller := net.ParseIP(host)
	if caller == nil {
		return false
	}
	if peer.IsLoopback() && caller.IsLoopback() {
		return true
	}
	return caller.Equal(*peer)
}

// isUnicastTarget rejects the addresses a reply must never go to: multicast,
// broadcast, and the unspecified address. Loopback stays allowed, since a
// client on the device's own host is a normal case.
func isUnicastTarget(ip net.IP) bool {
	if ip.IsUnspecified() || ip.IsMulticast() || ip.Equal(net.IPv4bcast) {
		return false
	}
	return true
}

// sendDiscoveryReply unicasts the standard discovery reply to target from a
// fresh socket. The source port is immaterial to a client, which reads the
// device's address from the datagram and the port from its body.
func (s *Server) sendDiscoveryReply(target *net.UDPAddr) error {
	conn, err := net.DialUDP("udp", nil, target)
	if err != nil {
		return err
	}
	defer conn.Close()
	resp, _ := json.Marshal(directResponse{AlpacaPort: s.advertisedPort()})
	_, err = conn.Write(resp)
	return err
}

// rateGate is a token bucket: relayBurst tokens, refilled at relayRate per
// second. The zero value starts full.
type rateGate struct {
	mu     sync.Mutex
	tokens float64
	last   time.Time
}

// take spends one token at time now and reports whether one was available.
func (g *rateGate) take(now time.Time) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.last.IsZero() {
		g.tokens = relayBurst
	} else {
		g.tokens += now.Sub(g.last).Seconds() * relayRate
		if g.tokens > relayBurst {
			g.tokens = relayBurst
		}
	}
	g.last = now
	if g.tokens < 1 {
		return false
	}
	g.tokens--
	return true
}
