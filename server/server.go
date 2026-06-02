package alpacadev

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

// DiscoveryMode selects how the device participates in Alpaca UDP discovery.
type DiscoveryMode int

const (
	// DiscoveryRegister sends a periodic unicast heartbeat to a discovery
	// server (device shares the server's host). Default.
	DiscoveryRegister DiscoveryMode = iota
	// DiscoveryDirect binds UDP 32227 (with SO_REUSEADDR/SO_REUSEPORT, so
	// multiple device processes can share the port on one host) and self-answers
	// broadcast discovery probes. No discovery server needed. Note: directed
	// unicast probes to a multi-device host reach only one responder.
	DiscoveryDirect
	// DiscoveryOff disables discovery; the device is reached by manual IP:port.
	DiscoveryOff
)

// DiscoveryConfig configures discovery participation.
type DiscoveryConfig struct {
	Mode       DiscoveryMode
	ServerAddr string        // host:32227, for DiscoveryRegister
	Interval   time.Duration // heartbeat cadence (≈ discovery-server TTL/3)
	EnableIPv6 bool          // also answer IPv6 multicast probes (DiscoveryDirect)
}

// Config is the server configuration.
type Config struct {
	AlpacaPort int // device HTTP REST port (e.g. 11111)
	Discovery  DiscoveryConfig

	// Management metadata (served at /management/v1/description).
	ServerName          string
	Manufacturer        string
	ManufacturerVersion string
	Location            string

	// Logger, if non-nil, logs one line per HTTP request (remote addr, method,
	// URI, status, duration; PUT form body included). Handy for debugging; leave
	// nil for silence.
	Logger *log.Logger
}

type regKey struct {
	typ DeviceType
	num int
}

type registeredDevice struct {
	typ DeviceType
	num int
	dev Device
}

// Server hosts one or more devices behind one Alpaca HTTP port and participates
// in discovery. It is the persistent owner of any Hardware-implementing devices.
type Server struct {
	cfg Config

	mu      sync.RWMutex
	devices map[regKey]*registeredDevice
	order   []*registeredDevice // registration order, for configureddevices

	tx   serverTxCounter
	http *http.Server
}

// New creates a Server. Defaults: Discovery.Interval 10s.
func New(cfg Config) *Server {
	if cfg.Discovery.Interval == 0 {
		cfg.Discovery.Interval = 10 * time.Second
	}
	return &Server{
		cfg:     cfg,
		devices: map[regKey]*registeredDevice{},
	}
}

// Register adds a device at the given type/number. Numbers are per type and
// must be unique. Call before Run.
func (s *Server) Register(devType DeviceType, number int, d Device) error {
	if d == nil {
		return errors.New("alpacadev: nil device")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	k := regKey{devType, number}
	if _, exists := s.devices[k]; exists {
		return fmt.Errorf("alpacadev: %s device %d already registered", devType, number)
	}
	rd := &registeredDevice{typ: devType, num: number, dev: d}
	s.devices[k] = rd
	s.order = append(s.order, rd)
	return nil
}

func (s *Server) lookup(devType DeviceType, number int) (Device, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rd, ok := s.devices[regKey{devType, number}]
	if !ok {
		return nil, false
	}
	return rd.dev, true
}

// Run opens hardware (once), starts the HTTP server and discovery, and blocks
// until ctx is cancelled, then shuts down gracefully and closes hardware last
// (so cooling/regulation persists until the very end).
func (s *Server) Run(ctx context.Context) error {
	// 1. Open hardware once for every Hardware-implementing device.
	opened := s.openHardware(ctx)

	// 2. HTTP server.
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.ServeHTTP)
	s.http = &http.Server{
		Addr:    fmt.Sprintf(":%d", s.cfg.AlpacaPort),
		Handler: mux,
	}

	httpErr := make(chan error, 1)
	go func() {
		if err := s.http.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			httpErr <- err
		}
	}()

	// 3. Discovery (responder or heartbeat ticker).
	discoveryCtx, stopDiscovery := context.WithCancel(ctx)
	go s.startDiscovery(discoveryCtx)

	// 4. Wait for shutdown or fatal HTTP error.
	var runErr error
	select {
	case <-ctx.Done():
	case runErr = <-httpErr:
	}

	// 5. Graceful shutdown: drain HTTP, stop heartbeat, then close hardware last.
	stopDiscovery()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = s.http.Shutdown(shutdownCtx)
	s.closeHardware(context.Background(), opened)

	return runErr
}

// openHardware calls Open on every Hardware device, returning those opened (in
// registration order) so they can be closed in reverse at shutdown.
func (s *Server) openHardware(ctx context.Context) []Hardware {
	var opened []Hardware
	s.mu.RLock()
	order := append([]*registeredDevice(nil), s.order...)
	s.mu.RUnlock()
	for _, rd := range order {
		if hw, ok := rd.dev.(Hardware); ok {
			if err := hw.Open(ctx); err != nil {
				// Surface but continue; a device that fails Open will report
				// NotConnected/errors per member. Supervised restart is the
				// recovery model (spec §8).
				fmt.Printf("alpacadev: %s %d Open failed: %v\n", rd.typ, rd.num, err)
				continue
			}
			opened = append(opened, hw)
		}
	}
	return opened
}

func (s *Server) closeHardware(ctx context.Context, opened []Hardware) {
	for i := len(opened) - 1; i >= 0; i-- {
		if err := opened[i].Close(ctx); err != nil {
			fmt.Printf("alpacadev: hardware Close failed: %v\n", err)
		}
	}
}
