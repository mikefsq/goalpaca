package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
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
	Mode       DiscoveryMode // participation mode (default DiscoveryRegister)
	ServerAddr string        // host:32227, for DiscoveryRegister
	Interval   time.Duration // heartbeat cadence (≈ discovery-server TTL/3)
	EnableIPv6 bool          // also answer IPv6 multicast probes (DiscoveryDirect)
}

// HTTPTimeouts bounds the HTTP server's per-connection I/O. The zero value of
// each field selects the default; a negative value disables that limit.
type HTTPTimeouts struct {
	// ReadHeader bounds reading a request's headers (slowloris guard).
	// Default 10s.
	ReadHeader time.Duration
	// Read bounds reading a whole request. Alpaca requests are tiny (query
	// params / small form bodies), so the default is a tight 30s.
	Read time.Duration
	// Write bounds writing a whole response. An ImageBytes response can be
	// >100 MB and each in-flight image write pins a frame-sized buffer, so an
	// overall cap matters; the 5m default passes a full frame at under
	// 500 KB/s. Raise it (or set -1) for slower links.
	Write time.Duration
	// Idle bounds how long a keep-alive connection may sit idle. Default 2m.
	Idle time.Duration
}

// value resolves one field: zero → default, negative → no limit.
func timeoutValue(v, def time.Duration) time.Duration {
	switch {
	case v == 0:
		return def
	case v < 0:
		return 0 // net/http: zero means no timeout
	}
	return v
}

// Config is the server configuration.
type Config struct {
	AlpacaPort int // device HTTP REST port (e.g. 11111)
	Discovery  DiscoveryConfig

	// Timeouts bounds per-connection HTTP I/O; the zero value selects
	// defaults suited to LAN Alpaca traffic (see HTTPTimeouts).
	Timeouts HTTPTimeouts

	// Hosts restricts the local addresses the HTTP server binds to, one listener
	// per address (e.g. []string{"127.0.0.1", "10.0.1.20"}). Empty (the default)
	// binds the wildcard ":port", i.e. every interface on both IP stacks. Use it to
	// keep the device off interfaces like a VM bridge. An address that fails to bind
	// is logged and skipped; if none bind, Run returns an error.
	Hosts []string

	// Management metadata (served at /management/v1/description).
	ServerName          string
	Manufacturer        string
	ManufacturerVersion string
	Location            string

	// Logger, if non-nil, receives one line per HTTP request (remote addr,
	// method, URI, status, duration; PUT form body included) plus server
	// events (bind failures, discovery errors, hardware Open/Close failures).
	// If nil, request logging is off and events go to the standard logger.
	// Set a log.New(io.Discard, "", 0) Logger for complete silence.
	Logger *log.Logger

	// StrictParamCasing, if true, matches request parameter names exactly
	// instead of case-insensitively. The default (false) follows the spec —
	// "Parameter names are not case sensitive, so clients and drivers should
	// be prepared for parameter names to be supplied and returned with any
	// casing" (specs/AlpacaDeviceAPI_v1.yaml) — and is what real-world clients
	// expect. Set true only to satisfy ConformU's "Check Alpaca Protocol" mode,
	// whose "Bad casing" tests invert a parameter name's casing and expect the
	// server to treat it as missing (400) rather than tolerate it; that is
	// stricter than the spec text and will reject real clients that send a
	// differently-cased parameter name.
	StrictParamCasing bool
}

// logf logs a server event: to cfg.Logger when set, else the standard logger.
func (s *Server) logf(format string, args ...any) {
	if s.cfg.Logger != nil {
		s.cfg.Logger.Printf(format, args...)
		return
	}
	log.Printf(format, args...)
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

	boundPort atomic.Int32 // actual bound TCP port, set by Run (0 until bound)
}

// Port returns the actual bound HTTP port once Run has bound its listener(s),
// or 0 before that. With Config.AlpacaPort 0 (OS-assigned) this is how a
// caller — and discovery — learns the real port; with multiple Hosts it is
// the first listener's port.
func (s *Server) Port() int { return int(s.boundPort.Load()) }

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
// must be unique. The device must implement the typed interface matching
// devType (e.g. Camera for CameraType) — registering a mismatch would
// otherwise surface only as confusing "unknown member" responses at runtime.
// Call before Run.
func (s *Server) Register(devType DeviceType, number int, d Device) error {
	if d == nil {
		return errors.New("goalpaca: nil device")
	}
	if !implementsType(devType, d) {
		return fmt.Errorf("goalpaca: device %T does not implement the %s interface", d, devType)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	k := regKey{devType, number}
	if _, exists := s.devices[k]; exists {
		return fmt.Errorf("goalpaca: %s device %d already registered", devType, number)
	}
	rd := &registeredDevice{typ: devType, num: number, dev: d}
	s.devices[k] = rd
	s.order = append(s.order, rd)
	return nil
}

// implementsType reports whether d satisfies the typed interface for devType.
// Unknown/custom types carry no static contract beyond Device.
func implementsType(devType DeviceType, d Device) bool {
	switch devType {
	case CameraType:
		_, ok := d.(Camera)
		return ok
	case CoverCalibratorType:
		_, ok := d.(CoverCalibrator)
		return ok
	case DomeType:
		_, ok := d.(Dome)
		return ok
	case FilterWheelType:
		_, ok := d.(FilterWheel)
		return ok
	case FocuserType:
		_, ok := d.(Focuser)
		return ok
	case ObservingConditionsType:
		_, ok := d.(ObservingConditions)
		return ok
	case RotatorType:
		_, ok := d.(Rotator)
		return ok
	case SafetyMonitorType:
		_, ok := d.(SafetyMonitor)
		return ok
	case SwitchType:
		_, ok := d.(Switch)
		return ok
	case TelescopeType:
		_, ok := d.(Telescope)
		return ok
	}
	return true
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

	// 2. HTTP server. Bind the wildcard (:port, every interface) by default, or one
	// listener per address when Config.Hosts restricts it.
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.ServeHTTP)
	s.http = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: timeoutValue(s.cfg.Timeouts.ReadHeader, 10*time.Second),
		ReadTimeout:       timeoutValue(s.cfg.Timeouts.Read, 30*time.Second),
		WriteTimeout:      timeoutValue(s.cfg.Timeouts.Write, 5*time.Minute),
		IdleTimeout:       timeoutValue(s.cfg.Timeouts.Idle, 2*time.Minute),
	}

	httpErr := make(chan error, 1)
	serve := func(ln net.Listener) {
		if err := s.http.Serve(ln); err != nil && err != http.ErrServerClosed {
			select {
			case httpErr <- err: // first fatal error wins
			default: // another listener already reported; don't block forever
			}
		}
	}
	recordPort := func(ln net.Listener) {
		if s.boundPort.Load() == 0 {
			if ta, ok := ln.Addr().(*net.TCPAddr); ok {
				s.boundPort.Store(int32(ta.Port))
			}
		}
	}
	if len(s.cfg.Hosts) == 0 {
		ln, err := net.Listen("tcp", fmt.Sprintf(":%d", s.cfg.AlpacaPort))
		if err != nil {
			s.closeHardware(context.Background(), opened)
			return err
		}
		recordPort(ln)
		go serve(ln)
	} else {
		bound := 0
		for _, h := range s.cfg.Hosts {
			addr := net.JoinHostPort(h, strconv.Itoa(s.cfg.AlpacaPort))
			ln, err := net.Listen("tcp", addr)
			if err != nil {
				s.logf("goalpaca: listen %s failed: %v", addr, err)
				continue
			}
			bound++
			recordPort(ln)
			go serve(ln)
		}
		if bound == 0 {
			s.closeHardware(context.Background(), opened)
			return fmt.Errorf("goalpaca: could not bind any of %v on port %d", s.cfg.Hosts, s.cfg.AlpacaPort)
		}
	}

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
				s.logf("goalpaca: %s %d Open failed: %v", rd.typ, rd.num, err)
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
			s.logf("goalpaca: hardware Close failed: %v", err)
		}
	}
}
