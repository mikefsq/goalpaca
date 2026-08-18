package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// Registrations is the table a discovery server or orchestrator keeps of the
// Register-mode devices that send it heartbeats. It answers the two questions
// a probe raises: which ports to reply for directly, and which devices to
// notify through the relay endpoint.
//
// A device is local when its reachable address is one of this host's own, and
// the receiver replies for it directly, since the reply then carries the right
// source address. A device anywhere else is remote, and the receiver relays the
// probe to it (see Relay), so the device's own reply carries its own address.
//
// Entries expire when no heartbeat arrives within the TTL, three heartbeat
// intervals by default, so a device that stops is forgotten without any
// unregister message.
type Registrations struct {
	// Client sends the relay requests. NewRegistrations sets one with a
	// timeout under a client's discovery wait; a host may replace it before
	// use, a test to redirect the requests.
	Client *http.Client

	ttl time.Duration

	mu      sync.Mutex
	entries map[string]*Registration

	// localIPs lists this host's addresses; a test substitutes its own.
	localIPs func() []net.IP
}

// Registration is one live device.
type Registration struct {
	Heartbeat
	// Addr is the address the device is reachable at: Heartbeat.Address when
	// the device supplied one, else the source of its heartbeat datagram.
	Addr net.IP
	// Local reports that Addr belongs to this host.
	Local bool
	Seen  time.Time
}

// DefaultRegistrationTTL is three times the default heartbeat interval.
const DefaultRegistrationTTL = 30 * time.Second

// relayTimeout bounds one relay request. A client waits a second or two for
// discovery replies, so a device that has not answered within this is not
// going to answer in time.
const relayTimeout = 800 * time.Millisecond

// NewRegistrations returns an empty table with the given TTL (zero selects
// DefaultRegistrationTTL).
func NewRegistrations(ttl time.Duration) *Registrations {
	if ttl <= 0 {
		ttl = DefaultRegistrationTTL
	}
	return &Registrations{
		ttl:      ttl,
		Client:   &http.Client{Timeout: relayTimeout},
		entries:  map[string]*Registration{},
		localIPs: hostIPs,
	}
}

// Datagram decodes one packet received on the discovery socket. It reports a
// probe, a heartbeat (recorded, with the sender's address as its reachable
// address unless the heartbeat names one, and returned as its Registration),
// or neither.
func (r *Registrations) Datagram(pkt []byte, from *net.UDPAddr) (DatagramKind, *Registration) {
	if bytes.HasPrefix(bytes.TrimSpace(pkt), []byte(discoveryProbe)) {
		return DatagramProbe, nil
	}
	var hb Heartbeat
	if json.Unmarshal(pkt, &hb) != nil || hb.AlpacaPort == 0 {
		return DatagramOther, nil
	}
	return DatagramHeartbeat, r.Record(hb, from.IP)
}

// DatagramKind classifies a packet on the discovery socket.
type DatagramKind int

const (
	DatagramOther DatagramKind = iota
	DatagramProbe
	DatagramHeartbeat
)

// Record upserts a heartbeat received from source. The key is the device's
// UniqueID, else its instance, else its address and port, so a device that
// restarts on a new port replaces its old entry rather than adding one.
func (r *Registrations) Record(hb Heartbeat, source net.IP) *Registration {
	addr := source
	if ip := net.ParseIP(hb.Address); ip != nil {
		addr = ip
	}
	key := hb.UniqueID
	if key == "" {
		key = hb.Instance
	}
	if key == "" {
		key = net.JoinHostPort(addr.String(), strconv.Itoa(hb.AlpacaPort))
	}
	entry := &Registration{Heartbeat: hb, Addr: addr, Local: r.isLocal(addr), Seen: time.Now()}
	r.mu.Lock()
	r.entries[key] = entry
	r.mu.Unlock()
	cp := *entry
	return &cp
}

// Live returns the unexpired registrations, pruning the rest.
func (r *Registrations) Live() []Registration {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	var out []Registration
	for k, e := range r.entries {
		if now.Sub(e.Seen) > r.ttl {
			delete(r.entries, k)
			continue
		}
		out = append(out, *e)
	}
	return out
}

// LocalPorts returns the distinct Alpaca ports of the live local devices, the
// ports the receiver answers a probe for directly.
func (r *Registrations) LocalPorts() []int {
	seen := map[int]bool{}
	var ports []int
	for _, e := range r.Live() {
		if e.Local && !seen[e.AlpacaPort] {
			seen[e.AlpacaPort] = true
			ports = append(ports, e.AlpacaPort)
		}
	}
	return ports
}

// Relay asks every live remote device to send its own discovery reply to
// client, the probe's source. The requests go out in parallel and Relay
// returns when all have completed or timed out; the client's wait is short and
// outside our control, so nothing here is sequential. Each failure is passed
// to onErr when it is non-nil.
func (r *Registrations) Relay(ctx context.Context, client *net.UDPAddr, onErr func(reg Registration, err error)) {
	target, _ := json.Marshal(ReplyTarget{IP: client.IP.String(), Port: client.Port})
	var wg sync.WaitGroup
	for _, e := range r.Live() {
		if e.Local {
			continue
		}
		wg.Add(1)
		go func(e Registration) {
			defer wg.Done()
			if err := r.relayOne(ctx, e, target); err != nil && onErr != nil {
				onErr(e, err)
			}
		}(e)
	}
	wg.Wait()
}

func (r *Registrations) relayOne(ctx context.Context, e Registration, body []byte) error {
	url := "http://" + net.JoinHostPort(e.Addr.String(), strconv.Itoa(e.AlpacaPort)) + DiscoveryReplyPath
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.Client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return &relayStatusError{url: url, status: resp.Status}
	}
	return nil
}

type relayStatusError struct{ url, status string }

func (e *relayStatusError) Error() string { return "POST " + e.url + ": " + e.status }

// isLocal reports whether ip is one of this host's own addresses.
func (r *Registrations) isLocal(ip net.IP) bool {
	if ip.IsLoopback() {
		return true
	}
	for _, own := range r.localIPs() {
		if own.Equal(ip) {
			return true
		}
	}
	return false
}

// hostIPs lists the addresses of every interface on this host.
func hostIPs() []net.IP {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	var ips []net.IP
	for _, a := range addrs {
		switch v := a.(type) {
		case *net.IPNet:
			ips = append(ips, v.IP)
		case *net.IPAddr:
			ips = append(ips, v.IP)
		}
	}
	return ips
}
