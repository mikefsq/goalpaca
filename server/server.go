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
	// server on the same host or a routed network. Default.
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
	// Instance is the host's name for this device binary, carried in every
	// registration (Heartbeat.Instance) so an orchestrator can join it to its
	// own entry for the device. A device binary sets it from its device file's
	// stem; empty when there is none.
	Instance string
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

	// PortScanBase, with AlpacaPort 0, makes Run bind the first free port at or
	// above the base rather than an OS-assigned one, and Port reports which.
	// Binding is the arbiter: two servers starting together cannot select the
	// same port, since the second's bind fails and it moves on. A host persists
	// the result so the port is stable from the next start. AlpacaPort 0 with a
	// zero base keeps the OS-assigned behaviour. PortScanLimit bounds the scan;
	// zero means 100 ports.
	PortScanBase  int
	PortScanLimit int

	// Timeouts bounds per-connection HTTP I/O; the zero value selects
	// defaults suited to LAN Alpaca traffic (see HTTPTimeouts).
	Timeouts HTTPTimeouts

	// Hosts restricts the local addresses the HTTP server binds to, one listener
	// per address (e.g. []string{"127.0.0.1", "10.0.1.20"}). Empty (the default)
	// binds the wildcard ":port", i.e. every interface on both IP stacks. Use it to
	// keep the device off interfaces like a VM bridge. An address that fails to bind
	// is logged and skipped; if none bind, Run returns an error.
	Hosts []string

	// Settings, if non-nil, persists Configurable devices' settings across
	// restarts: the server loads and applies each device's stored values at
	// startup (before hardware opens) and saves them after every successful
	// /setup form submission. Use NewFileStore for the default JSON-file store;
	// the server does no persistence when this is nil (settings are in-memory
	// only, as before). Load/apply errors are logged, never fatal.
	Settings SettingsStore

	// ConfigPath names the configuration file this server was started from, for
	// display on the /setup page so a user knows where the values came from and
	// which file to edit. Empty when the host has no config file (a flags-only
	// binary), in which case the page says so.
	ConfigPath string

	// Setup, when set, adds a server-level form to the /setup page, rendered by
	// the same form code as a device's setup page. A host puts server-wide
	// values here (listen address, discovery mode). Nil leaves /setup read-only.
	Setup Configurable

	// SetupTemplates overrides the setup page templates and stylesheet, so a
	// host can brand or extend the pages while every device page keeps the same
	// structure. Zero-value fields keep the defaults.
	SetupTemplates SetupTemplates

	// SetupPages adds host pages under /setup/<name>, linked from /setup. A
	// host uses it for pages whose shape is not a device form (an orchestrator's
	// device table with per-row actions). The name is one path segment; "v1"
	// is reserved for the spec's device pages. The handler receives the request
	// with its path intact and renders its own HTML; SetupTemplates.CSS is
	// available through Server.SetupCSS so the page can match.
	SetupPages map[string]http.Handler

	// SetupHome, when set, is served at /setup in place of the server page, and
	// GET / redirects to /setup as always. A host whose server exists to carry
	// one page (an orchestrator's) puts it here, so its address is the same
	// /setup every Alpaca server has. The server page stays reachable at
	// /setup/server while the server hosts devices; a server with none has
	// nothing to show there, and /setup/server redirects to /setup. Nil keeps
	// the server page at /setup.
	SetupHome http.Handler

	// SetupLinks adds links to pages served elsewhere, listed on /setup beside
	// SetupPages: an orchestrator's page on its own port, say. Keys are the link
	// text; values are absolute URLs.
	SetupLinks map[string]string

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
	// dev and cfg are read and written under Server.mu: Reload swaps them
	// while requests are in flight.
	dev Device
	cfg Configurable // attached by RegisterConfigurable when dev has no own implementation

	// hw is dev's Hardware once Open succeeded, nil otherwise; closed at
	// shutdown and before a reload swaps the device out. hwCancel ends the
	// context Open ran under, called before Close so a loop Open started
	// stops. Both under Server.mu.
	hw       Hardware
	hwCancel context.CancelFunc

	// reloader rebuilds the device from its configuration; nil means the
	// device cannot be reloaded in place. reloadMu serializes reloads of
	// this device.
	reloader Reloader
	reloadMu sync.Mutex

	// settingsKey is the SettingsStore key (a file path for FileStore), set by
	// SettingsPath; empty means the default under StateDir.
	settingsKey string
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

	// runCtx is Run's context while the server runs: the parent of every
	// device's hardware context, so a device opened by a Reload lives as long
	// as one opened at start rather than as long as the request that reloaded
	// it. Under mu.
	runCtx context.Context

	// relayPeer is the discovery server's IP in Register mode, the one caller
	// the relay endpoint accepts; nil until the heartbeat sender resolves it.
	relayPeer atomic.Pointer[net.IP]
	relayGate rateGate

	// Setup page templates, resolved from Config.SetupTemplates on first use.
	tmplOnce  sync.Once
	tmplCache *resolvedTemplates
}

// SetIdentity sets what the server reports as itself: the ServerName,
// Manufacturer, and ManufacturerVersion of /management/v1/description and the
// server setup page. A host that gives each device its own port names the
// server after the device it hosts. Call before Run.
func (s *Server) SetIdentity(name, manufacturer, version string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if name != "" {
		s.cfg.ServerName = name
	}
	if manufacturer != "" {
		s.cfg.Manufacturer = manufacturer
	}
	if version != "" {
		s.cfg.ManufacturerVersion = version
	}
}

// Port returns the actual bound HTTP port once Run has bound its listener(s),
// or 0 before that. With Config.AlpacaPort 0 (OS-assigned) this is how a
// caller — and discovery — learns the real port; with multiple Hosts it is
// the first listener's port.
func (s *Server) Port() int { return int(s.boundPort.Load()) }

// scanListen binds the first free port at or above Config.PortScanBase on the
// given host ("" for the wildcard) and returns the held listener. The bind is
// the probe, so there is no window in which another process can take the port
// between finding it and using it: two servers scanning together get distinct
// ports because the second's bind of the first's port fails and it moves on.
func (s *Server) scanListen(host string) (net.Listener, error) {
	limit := s.cfg.PortScanLimit
	if limit <= 0 {
		limit = 100
	}
	for p := s.cfg.PortScanBase; p < s.cfg.PortScanBase+limit; p++ {
		ln, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(p)))
		if err == nil {
			return ln, nil
		}
	}
	return nil, fmt.Errorf("goalpaca: no free port in %d..%d", s.cfg.PortScanBase, s.cfg.PortScanBase+limit-1)
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

// Register adds a device under a unique type and number. The device must
// implement the interface matching devType.
// Before Run, settings and hardware wait for startup. On a running server,
// settings load and hardware opens immediately; Open failures are logged and
// the device remains registered.
func (s *Server) Register(devType DeviceType, number int, d Device) error {
	if d == nil {
		return errors.New("goalpaca: nil device")
	}
	if !implementsType(devType, d) {
		return fmt.Errorf("goalpaca: device %T does not implement the %s interface", d, devType)
	}
	s.mu.Lock()
	k := regKey{devType, number}
	if _, exists := s.devices[k]; exists {
		s.mu.Unlock()
		return fmt.Errorf("goalpaca: %s device %d already registered", devType, number)
	}
	rd := &registeredDevice{typ: devType, num: number, dev: d}
	s.devices[k] = rd
	s.order = append(s.order, rd)
	running := s.runCtx != nil
	s.mu.Unlock()
	if running {
		s.loadSettingsFor(rd)
		_ = s.openHardwareFor(context.Background(), rd)
	}
	return nil
}

// Unregister removes a device from a running or a not yet running server:
// its hardware is closed (the context its Open ran under is cancelled first,
// see Hardware) and its type and number are free again. Requests to it answer
// as to any unknown device from then on. It is what a host uses to disable a
// device without restarting the server.
func (s *Server) Unregister(devType DeviceType, number int) error {
	s.mu.Lock()
	k := regKey{devType, number}
	rd, ok := s.devices[k]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("goalpaca: %s device %d is not registered", devType, number)
	}
	delete(s.devices, k)
	for i, o := range s.order {
		if o == rd {
			s.order = append(s.order[:i], s.order[i+1:]...)
			break
		}
	}
	s.mu.Unlock()
	s.closeHardwareFor(context.Background(), rd)
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
	rd, ok := s.lookupRegistered(devType, number)
	if !ok {
		return nil, false
	}
	return rd.dev, true
}

// Device returns the device currently registered at the address. A reload
// swaps the registered device, so a front-end that resolves it through here
// per call follows the swap instead of holding the replaced object.
func (s *Server) Device(devType DeviceType, number int) (Device, bool) {
	return s.lookup(devType, number)
}

// lookupRegistered returns the registration record for a device, which carries
// the attached Configurable alongside the device itself.
func (s *Server) lookupRegistered(devType DeviceType, number int) (*registeredDevice, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rd, ok := s.devices[regKey{devType, number}]
	return rd, ok
}

// Run opens hardware (once), starts the HTTP server and discovery, and blocks
// until ctx is cancelled, then shuts down gracefully and closes hardware last
// (so cooling/regulation persists until the very end).
func (s *Server) Run(ctx context.Context) error {
	s.mu.Lock()
	s.runCtx = ctx
	s.mu.Unlock()
	// 0. Apply persisted setup settings before hardware opens, so a device opens
	// with its saved configuration (e.g. a cooler setpoint) already in effect.
	s.loadSettings()

	// 1. Open hardware once for every Hardware-implementing device.
	s.openHardware(ctx)

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
	// listen binds host on the configured port, or scans up from PortScanBase
	// when AlpacaPort is 0 and a base is set. Once one listener has fixed the
	// port, every further host binds that same port so a multi-host server
	// answers on one number.
	scanning := s.cfg.AlpacaPort == 0 && s.cfg.PortScanBase > 0
	listen := func(host string) (net.Listener, error) {
		if p := int(s.boundPort.Load()); p != 0 {
			return net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(p)))
		}
		if scanning {
			return s.scanListen(host)
		}
		return net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(s.cfg.AlpacaPort)))
	}
	if len(s.cfg.Hosts) == 0 {
		ln, err := listen("")
		if err != nil {
			s.closeHardware(context.Background())
			return err
		}
		recordPort(ln)
		go serve(ln)
	} else {
		bound := 0
		for _, h := range s.cfg.Hosts {
			ln, err := listen(h)
			if err != nil {
				s.logf("goalpaca: listen %s failed: %v", h, err)
				continue
			}
			bound++
			recordPort(ln)
			go serve(ln)
		}
		if bound == 0 {
			s.closeHardware(context.Background())
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
	s.closeHardware(context.Background())

	return runErr
}

// loadSettings applies each Configurable device's persisted settings (if a
// SettingsStore is configured), establishing the precedence
// code default < persisted < host config: the host's own values are pinned in
// the form (see StructConfig), so
// persistence can never clobber a host config value. Errors are logged and
// skipped — a bad or missing store must never stop the server.
func (s *Server) loadSettings() {
	if s.cfg.Settings == nil {
		return
	}
	s.mu.RLock()
	order := append([]*registeredDevice(nil), s.order...)
	s.mu.RUnlock()
	for _, rd := range order {
		s.loadSettingsFor(rd)
	}
}

// loadSettingsFor applies one device's persisted settings; see loadSettings.
func (s *Server) loadSettingsFor(rd *registeredDevice) {
	if s.cfg.Settings == nil {
		return
	}
	_, cfg, ok := s.current(rd)
	if !ok {
		return
	}
	vals, err := s.cfg.Settings.Load(s.settingsKey(rd))
	if err != nil {
		s.logf("goalpaca: load settings for %s %d: %v", rd.typ, rd.num, err)
		return
	}
	if len(vals) == 0 {
		return
	}
	// Keep only keys the form declares and does not lock. A persisted file
	// may hold keys that are not the device's to apply: a host-owned "port"
	// the orchestrator recorded beside the driver's own settings, or a key
	// from a field the driver has since dropped. Those are left alone; a
	// locked field is host-pinned and persistence never overrides it.
	known := map[string]bool{}
	for _, f := range cfg.SettingsForm() {
		if !f.Locked {
			known[f.Name] = true
		}
	}
	for k := range vals {
		if !known[k] {
			delete(vals, k)
		}
	}
	if len(vals) == 0 {
		return
	}
	if err := cfg.ApplySettings(formStrings(vals)); err != nil {
		s.logf("goalpaca: apply persisted settings for %s %d: %v", rd.typ, rd.num, err)
	}
}

// formStrings renders persisted values as the form strings ApplySettings takes.
// A JSON number is formatted with %v so an integer stays "55", not "55.0".
func formStrings(vals map[string]any) map[string]string {
	out := make(map[string]string, len(vals))
	for k, v := range vals {
		switch x := v.(type) {
		case string:
			out[k] = x
		case bool:
			out[k] = strconv.FormatBool(x)
		case float64:
			out[k] = strconv.FormatFloat(x, 'f', -1, 64)
		default:
			out[k] = fmt.Sprint(x)
		}
	}
	return out
}

// openHardware calls Open on every Hardware device, recording each success on
// its registration so closeHardware and Reload can close it.
func (s *Server) openHardware(ctx context.Context) {
	s.mu.RLock()
	order := append([]*registeredDevice(nil), s.order...)
	s.mu.RUnlock()
	for _, rd := range order {
		s.openHardwareFor(ctx, rd)
	}
}

// openHardwareFor opens hardware under a context lasting until device closure.
// Failures are logged and returned; the device remains registered.
func (s *Server) openHardwareFor(ctx context.Context, rd *registeredDevice) error {
	s.mu.RLock()
	dev, open, base := rd.dev, rd.hw != nil, s.runCtx
	s.mu.RUnlock()
	if open {
		return nil
	}
	hw, ok := dev.(Hardware)
	if !ok {
		return nil
	}
	if base == nil {
		base = ctx
	}
	hwCtx, cancel := context.WithCancel(base)
	if err := hw.Open(hwCtx); err != nil {
		cancel()
		s.logf("goalpaca: %s %d Open failed: %v", rd.typ, rd.num, err)
		return err
	}
	s.mu.Lock()
	rd.hw, rd.hwCancel = hw, cancel
	s.mu.Unlock()
	return nil
}

// closeHardware closes every open device in reverse registration order.
func (s *Server) closeHardware(ctx context.Context) {
	s.mu.RLock()
	order := append([]*registeredDevice(nil), s.order...)
	s.mu.RUnlock()
	for i := len(order) - 1; i >= 0; i-- {
		s.closeHardwareFor(ctx, order[i])
	}
}

// closeHardwareFor closes one device's hardware if it is open: it cancels the
// context Open ran under first, so a loop the device started sees the end and
// stops re-acquiring, then calls Close.
func (s *Server) closeHardwareFor(ctx context.Context, rd *registeredDevice) {
	s.mu.Lock()
	hw, cancel := rd.hw, rd.hwCancel
	rd.hw, rd.hwCancel = nil, nil
	s.mu.Unlock()
	if hw == nil {
		return
	}
	if cancel != nil {
		cancel()
	}
	if err := hw.Close(ctx); err != nil {
		s.logf("goalpaca: hardware Close failed: %v", err)
	}
}

// current reads a registration's device and Configurable under the lock, so a
// caller outside it sees a consistent pair across a Reload.
func (s *Server) current(rd *registeredDevice) (Device, Configurable, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cfg, ok := configurableFor(rd)
	return rd.dev, cfg, ok
}
