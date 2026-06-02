package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"time"
)

const (
	discoveryPort  = 32227
	discoveryProbe = "alpacadiscovery1"
	// alpacaV6Group is the Alpaca IPv6 discovery multicast group (link-local
	// scope); matches the server's group so the two interoperate.
	alpacaV6Group = "ff12::00a1:9aca"
)

// DiscoveredServer is an Alpaca server found via UDP discovery.
type DiscoveredServer struct {
	IP         net.IP // responder's source address
	AlpacaPort int    // the server's Alpaca HTTP port
	Address    string // host:port for the HTTP API (pass to New<Type>)
}

// Discover finds Alpaca servers via UDP discovery on BOTH IPv4 (broadcast) and
// IPv6 (per-interface multicast), running the two concurrently and merging the
// results deduplicated by address. If one stack is unavailable (e.g. no IPv6),
// its results are simply omitted; an error is returned only if both legs fail.
//
// IPv6 discovery only finds servers that have joined the multicast group, i.e.
// servers configured with DiscoveryConfig.EnableIPv6.
func Discover(timeout time.Duration) ([]DiscoveredServer, error) {
	type leg struct {
		servers []DiscoveredServer
		err     error
	}
	ch := make(chan leg, 2)

	go func() {
		dests := []*net.UDPAddr{
			{IP: net.IPv4bcast, Port: discoveryPort},
			// Same-host probe: macOS does not loop a broadcast back to a listener
			// on the same machine, so a sim + client on one Mac would never see
			// each other. The IPv4 responder binds 0.0.0.0, which does receive a
			// unicast to loopback, making the local dev loop work.
			{IP: net.IPv4(127, 0, 0, 1), Port: discoveryPort},
		}
		for _, b := range interfaceBroadcasts() {
			dests = append(dests, &net.UDPAddr{IP: b, Port: discoveryPort})
		}
		s, err := discover(timeout, dests)
		ch <- leg{s, err}
	}()
	go func() {
		s, err := discoverIPv6(timeout)
		ch <- leg{s, err}
	}()

	seen := map[string]bool{}
	var out []DiscoveredServer
	var firstErr error
	ok := 0
	for i := 0; i < 2; i++ {
		l := <-ch
		if l.err != nil {
			if firstErr == nil {
				firstErr = l.err
			}
			continue
		}
		ok++
		for _, s := range l.servers {
			if !seen[s.Address] {
				seen[s.Address] = true
				out = append(out, s)
			}
		}
	}
	if ok == 0 {
		return nil, firstErr
	}
	return out, nil
}

// discover sends the probe over IPv4 to each destination (broadcast addresses)
// and collects the unicast replies.
func discover(timeout time.Duration, dests []*net.UDPAddr) ([]DiscoveredServer, error) {
	lc := net.ListenConfig{Control: setBroadcast}
	pc, err := lc.ListenPacket(context.Background(), "udp4", ":0")
	if err != nil {
		return nil, err
	}
	conn := pc.(*net.UDPConn)
	defer conn.Close()
	for _, d := range dests {
		_, _ = conn.WriteToUDP([]byte(discoveryProbe), d)
	}
	return readResponses(conn, timeout), nil
}

// discoverIPv6 sends the probe to the Alpaca IPv6 multicast group on every up,
// multicast-capable interface (the group is link-local scoped, so each send
// carries the interface zone) and collects the unicast replies. It returns an
// error only if no IPv6 socket can be opened (no IPv6 stack).
func discoverIPv6(timeout time.Duration) ([]DiscoveredServer, error) {
	pc, err := net.ListenUDP("udp6", &net.UDPAddr{IP: net.IPv6unspecified, Port: 0})
	if err != nil {
		return nil, err // no usable IPv6 stack
	}
	defer pc.Close()

	group := net.ParseIP(alpacaV6Group)
	sent := 0
	ifaces, _ := net.Interfaces()
	for _, ifi := range ifaces {
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagMulticast == 0 {
			continue
		}
		dst := &net.UDPAddr{IP: group, Port: discoveryPort, Zone: ifi.Name}
		if _, err := pc.WriteToUDP([]byte(discoveryProbe), dst); err == nil {
			sent++
		}
	}
	if sent == 0 {
		return nil, nil // no multicast-capable interface; nothing to discover
	}
	return readResponses(pc, timeout), nil
}

// readResponses collects {AlpacaPort} replies on conn until the timeout,
// deduplicated by address. An IPv6 zone (for link-local replies) is preserved in
// the address so the result stays dialable.
func readResponses(conn *net.UDPConn, timeout time.Duration) []DiscoveredServer {
	_ = conn.SetReadDeadline(time.Now().Add(timeout))
	seen := map[string]bool{}
	var out []DiscoveredServer
	buf := make([]byte, 1024)
	for {
		n, from, err := conn.ReadFromUDP(buf)
		if err != nil {
			break // read deadline reached or socket closed
		}
		var r struct {
			AlpacaPort int `json:"AlpacaPort"`
		}
		if json.Unmarshal(buf[:n], &r) != nil || r.AlpacaPort <= 0 {
			continue
		}
		host := from.IP.String()
		if from.Zone != "" {
			host += "%" + from.Zone
		}
		addr := net.JoinHostPort(host, strconv.Itoa(r.AlpacaPort))
		if seen[addr] {
			continue
		}
		seen[addr] = true
		out = append(out, DiscoveredServer{IP: from.IP, AlpacaPort: r.AlpacaPort, Address: addr})
	}
	return out
}

// interfaceBroadcasts returns the directed IPv4 broadcast address of each up,
// broadcast-capable interface (for multi-homed hosts where the limited
// 255.255.255.255 broadcast may not reach every subnet).
func interfaceBroadcasts() []net.IP {
	var out []net.IP
	ifaces, _ := net.Interfaces()
	for _, ifi := range ifaces {
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagBroadcast == 0 {
			continue
		}
		addrs, _ := ifi.Addrs()
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip4 := ipnet.IP.To4()
			if ip4 == nil {
				continue
			}
			mask := ipnet.Mask
			if len(mask) == net.IPv6len {
				mask = mask[12:]
			}
			bc := make(net.IP, net.IPv4len)
			for i := 0; i < net.IPv4len; i++ {
				bc[i] = ip4[i] | ^mask[i]
			}
			out = append(out, bc)
		}
	}
	return out
}

// ConfiguredDevice is one entry from a server's management API.
type ConfiguredDevice struct {
	DeviceName   string `json:"DeviceName"`
	DeviceType   string `json:"DeviceType"`
	DeviceNumber int    `json:"DeviceNumber"`
	UniqueID     string `json:"UniqueID"`
}

// ConfiguredDevices queries the server's /management/v1/configureddevices and
// returns the devices it hosts.
func (s DiscoveredServer) ConfiguredDevices() ([]ConfiguredDevice, error) {
	resp, err := http.Get(fmt.Sprintf("http://%s/management/v1/configureddevices", s.Address))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var env struct {
		Value []ConfiguredDevice `json:"Value"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, err
	}
	return env.Value, nil
}
