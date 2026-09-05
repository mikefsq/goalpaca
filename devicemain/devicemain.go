// Package devicemain runs registered drivers as standalone Alpaca servers.
// It supplies config files, flags, setup forms, persistence, and discovery.
//
//	package main
//
//	import (
//		"github.com/mikefsq/goalpaca/devicemain"
//		_ "example.com/mywidget"
//	)
//
//	func main() { devicemain.Run("mywidget") }
package devicemain

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/mikefsq/goalpaca/registry"
	"github.com/mikefsq/goalpaca/server"
)

// Options configures RunWith. The zero value uses the command-line defaults.
type Options struct {
	// Args are the command-line arguments to parse. Nil means os.Args[1:].
	Args []string
	// Stdout and Stderr receive -check and -schema output and log lines. Nil
	// means os.Stdout and os.Stderr.
	Stdout, Stderr io.Writer
	// DefaultPort is the Alpaca port when neither the config file nor -port
	// names one. Zero means 11111.
	DefaultPort int
	// Version is reported as the server's ManufacturerVersion.
	Version string
	// Manufacturer is reported in the management description. Empty means the
	// driver's registered name.
	Manufacturer string
	// Context, when non-nil, ends the server when it is cancelled. Nil means
	// SIGINT and SIGTERM end it.
	Context context.Context

	// Flags, when non-nil, is called with the flag set before parsing so a
	// binary can add its own flags beside the library's (a -list that
	// enumerates attached hardware, say).
	Flags func(fs *flag.FlagSet)
	// BeforeRun, when non-nil, is called after parsing and before the device
	// is constructed. Returning done=true ends Run there with err, which is how
	// a utility flag such as -list exits without serving.
	BeforeRun func() (done bool, err error)
	// AfterRegister, when non-nil, runs once the device is registered and the
	// server is about to start: a binary wires an extra front-end onto its
	// device here (a mount's LX200 bridge, say). dev returns the current
	// device — a reload swaps it, so a front-end that calls dev() per use
	// follows the swap — entry is the assembled device entry (file plus
	// flags), and ctx ends with the server. An error stops the binary before
	// it serves.
	AfterRegister func(ctx context.Context, dev func() server.Device, entry map[string]json.RawMessage) error
}

// Run serves a registered driver until shutdown and exits with status 1 on error.
// Use RunWith to receive the error without exiting.
func Run(driverName string) {
	if err := RunWith(driverName, Options{}); err != nil {
		fmt.Fprintln(os.Stderr, driverName+": "+err.Error())
		os.Exit(1)
	}
}

// RunWith is Run with options and an error return instead of an exit.
func RunWith(driverName string, opt Options) error {
	if opt.Stdout == nil {
		opt.Stdout = os.Stdout
	}
	if opt.Stderr == nil {
		opt.Stderr = os.Stderr
	}
	if opt.Args == nil {
		opt.Args = os.Args[1:]
	}
	if opt.DefaultPort == 0 {
		opt.DefaultPort = 11111
	}
	drv, ok := registry.Lookup(driverName)
	if !ok {
		return fmt.Errorf("driver %q is not registered; import its package for its init to register it", driverName)
	}
	logger := log.New(opt.Stderr, driverName+": ", log.LstdFlags|log.Lmsgprefix)

	fs := flag.NewFlagSet(driverName, flag.ContinueOnError)
	fs.SetOutput(opt.Stderr)
	configPath := fs.String("config", "", "device file (JSON with // comments); flags override its keys")
	port := fs.Int("port", 0, "Alpaca HTTP port (default: the file's, else "+strconv.Itoa(opt.DefaultPort)+")")
	name := fs.String("name", "", "device display name")
	discovery := fs.String("discovery", "direct",
		"discovery mode: direct (answer UDP 32227), register (heartbeat to a proxy or orchestrator, which answers for this device; binds no socket), off")
	discoveryServer := fs.String("discovery-server", "localhost:32227", "proxy or orchestrator address for register mode")
	ipv6 := fs.Bool("ipv6", false, "also answer IPv6 multicast discovery (direct mode)")
	check := fs.Bool("check", false, "load the config, construct the device without touching hardware, report, and exit")
	schema := fs.String("schema", "", "print the driver's config schema and exit: json, or commented (a device file with every key commented at its default)")
	quiet := fs.Bool("quiet", false, "no per-request log lines")

	// One flag per key of the driver's Config struct, so a value can be given
	// on the command line without a file. The values are collected as strings
	// and applied through the same path a form submission takes.
	cfgFlags := map[string]*string{}
	var cfgFields []server.SettingField
	if drv.Config != nil {
		fields, err := server.FieldsFromStruct(drv.Config(), nil, "")
		if err != nil {
			return fmt.Errorf("driver %s: config struct: %w", driverName, err)
		}
		cfgFields = fields
		for _, f := range fields {
			usage := f.Label
			if f.Constraints != "" {
				usage += " (" + f.Constraints + ")"
			}
			if f.Help != "" {
				usage += ": " + f.Help
			}
			cfgFlags[f.Name] = fs.String(f.Name, "", usage)
		}
	}
	if opt.Flags != nil {
		opt.Flags(fs)
	}
	if err := fs.Parse(opt.Args); err != nil {
		return err
	}
	if opt.BeforeRun != nil {
		if done, err := opt.BeforeRun(); done {
			return err
		}
	}

	switch *schema {
	case "":
	case "json":
		return writeSchemaJSON(opt.Stdout, drv, cfgFields)
	case "commented":
		return writeSchemaCommented(opt.Stdout, drv, cfgFields, opt.DefaultPort)
	default:
		return fmt.Errorf("-schema wants json or commented, got %q", *schema)
	}

	// The instance is the device file's stem, the same word an orchestrator
	// uses, so a standalone run and an orchestrated one log the same name.
	instance := ""
	if *configPath != "" {
		instance = strings.TrimSuffix(filepath.Base(*configPath), filepath.Ext(*configPath))
	}

	// assemble builds the device entry: file first, then flags over it. It
	// runs at start and again on every reload, so a reload sees the file as
	// it is now.
	assemble := func() (map[string]json.RawMessage, string, error) {
		entry := map[string]json.RawMessage{}
		source := "flags"
		if *configPath != "" {
			m, err := ReadDeviceFile(*configPath)
			if err != nil {
				return nil, "", err
			}
			entry = m
			source = *configPath
		}
		entry["driver"] = rawString(drv.Name)
		if *name != "" {
			entry["name"] = rawString(*name)
		}
		if *port != 0 {
			entry["port"] = rawInt(*port)
		}
		if _, has := entry["port"]; !has {
			entry["port"] = rawInt(opt.DefaultPort)
		}
		// Flag values are strings; type them through the config struct so the entry
		// decodes strictly in New.
		if drv.Config != nil {
			vals := map[string]string{}
			fs.Visit(func(f *flag.Flag) {
				if p, ok := cfgFlags[f.Name]; ok {
					vals[f.Name] = *p
				}
			})
			if len(vals) > 0 {
				cfg := drv.Config()
				if err := json.Unmarshal(mustJSON(entry), cfg); err != nil {
					return nil, "", fmt.Errorf("%s: %w", source, err)
				}
				if err := server.ApplyToStruct(cfg, vals); err != nil {
					return nil, "", err
				}
				typed, err := json.Marshal(cfg)
				if err != nil {
					return nil, "", err
				}
				var m map[string]json.RawMessage
				_ = json.Unmarshal(typed, &m)
				for k, v := range m {
					if _, sent := vals[k]; sent {
						entry[k] = v
					}
				}
			}
		}
		return entry, source, nil
	}

	// construct assembles the entry and builds the device from it, plus the
	// generated setup form when the driver has a Config struct and the device
	// no form of its own; keys the file or the flags supplied are the admin's
	// and render locked. This is the Reloader as well as the startup path.
	construct := func() (map[string]json.RawMessage, server.Device, server.Configurable, error) {
		entry, source, err := assemble()
		if err != nil {
			return nil, nil, nil, err
		}
		raw := mustJSON(entry)
		var displayName string
		_ = json.Unmarshal(entry["name"], &displayName)
		dev, err := drv.New(registry.Spec{Driver: drv.Name, Name: displayName, Instance: instance, Raw: raw})
		if err != nil {
			return nil, nil, nil, err
		}
		var sc server.Configurable
		if drv.Config != nil {
			if _, own := dev.(server.Configurable); !own {
				pinned := map[string]bool{}
				for k := range entry {
					if !isCommonKey(k) {
						pinned[k] = true
					}
				}
				adapter, err := server.NewStructConfig(dev, drv.Config, raw, pinned, "set in "+source)
				if err != nil {
					return nil, nil, nil, err
				}
				sc = adapter
			}
		}
		return entry, dev, sc, nil
	}

	entry, dev, sc, err := construct()
	if err != nil {
		return err
	}
	var portNum int
	_ = json.Unmarshal(entry["port"], &portNum)
	if *check {
		fmt.Fprintf(opt.Stdout, "ok     %-22s %s/0 on port %d  %q\n", drv.Name, drv.Type, portNum, dev.Name())
		return nil
	}

	var disc server.DiscoveryConfig
	switch strings.ToLower(*discovery) {
	case "direct":
		disc = server.DiscoveryConfig{Mode: server.DiscoveryDirect, EnableIPv6: *ipv6}
	case "register":
		// Register mode binds no socket: the device heartbeats to the
		// orchestrator or proxy, which answers probes for it, directly when
		// on the same host and through the relay endpoint when not. The
		// instance travels in the heartbeat so the orchestrator can join it
		// to the device's entry.
		disc = server.DiscoveryConfig{Mode: server.DiscoveryRegister, ServerAddr: *discoveryServer, Instance: instance}
	case "off":
		disc = server.DiscoveryConfig{Mode: server.DiscoveryOff}
	default:
		return fmt.Errorf("-discovery wants direct, register, or off, got %q", *discovery)
	}

	manufacturer := opt.Manufacturer
	if manufacturer == "" {
		manufacturer = drv.Name
	}
	var reqLog *log.Logger
	if !*quiet {
		reqLog = logger
	}
	srv := server.New(server.Config{
		AlpacaPort:          portNum,
		Discovery:           disc,
		ServerName:          drv.Name,
		Manufacturer:        manufacturer,
		ManufacturerVersion: opt.Version,
		Logger:              reqLog,
		ConfigPath:          *configPath,
		Settings:            server.NewFileStore(),
	})
	if err := srv.Register(drv.Type, 0, dev); err != nil {
		return err
	}
	if sc != nil {
		if err := srv.RegisterConfigurable(drv.Type, 0, sc); err != nil {
			return err
		}
	}
	// The current device, for AfterRegister front-ends: a reload swaps it.
	var devMu sync.RWMutex
	curDev := dev
	getDev := func() server.Device {
		devMu.RLock()
		defer devMu.RUnlock()
		return curDev
	}
	// A reload re-runs construct: the file and flags are read again, the
	// device rebuilt, its hardware closed and reopened, the port kept. It is
	// offered on the setup page and, on Unix, by SIGHUP.
	if err := srv.SetReloader(drv.Type, 0, func(context.Context) (server.Device, server.Configurable, error) {
		_, ndev, nsc, err := construct()
		if err == nil {
			devMu.Lock()
			curDev = ndev
			devMu.Unlock()
		}
		return ndev, nsc, err
	}); err != nil {
		return err
	}
	// Persist setup-page changes beside a device file when there is one, so a
	// hand-run binary and the same file under an orchestrator share state; else
	// under the per-user state directory.
	if p := deviceStatePath(drv.Name, instance); p != "" {
		_ = srv.SettingsPath(drv.Type, 0, p)
	}

	ctx := opt.Context
	if ctx == nil {
		var stop context.CancelFunc
		ctx, stop = signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()
	}
	// The driver's own front-end (a mount's LX200 bridge), wired from the same
	// entry in every layout; then the binary's extras. The device lives as
	// long as the process here, so its front-end context is the serve
	// context, and the server binds every interface, so hosts is empty.
	if drv.FrontEnd != nil {
		if err := drv.FrontEnd(ctx, getDev, mustJSON(entry), nil); err != nil {
			logger.Printf("front-end: %v", err)
		}
	}
	if opt.AfterRegister != nil {
		if err := opt.AfterRegister(ctx, getDev, entry); err != nil {
			return err
		}
	}
	stopReload := onReloadSignal(ctx, func() {
		if err := srv.ReloadAll(ctx); err != nil {
			logger.Printf("reload: %v", err)
		}
	})
	defer stopReload()
	logger.Printf("serving %s %q on :%d (discovery %s)", drv.Type, dev.Name(), portNum, strings.ToLower(*discovery))
	return srv.Run(ctx)
}

// deviceStatePath returns the instance's settings path under StateDir.
// An unnamed instance has no persistent settings path.
func deviceStatePath(driverName, instance string) string {
	if instance == "" {
		return ""
	}
	return filepath.Join(server.StateDir(driverName), "devices", instance+".json")
}

// ReadDeviceFile reads a device entry as JSONC, accepting // and /* */ comments.
// Trailing commas are not supported.
func ReadDeviceFile(path string) (map[string]json.RawMessage, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(StripComments(b), &m); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if m == nil {
		m = map[string]json.RawMessage{}
	}
	return m, nil
}

// StripComments removes // line comments and /* */ block comments from JSON
// text, leaving string literals intact. Comment bytes are replaced by spaces
// (newlines kept), so decode errors still report the right line.
func StripComments(b []byte) []byte {
	out := make([]byte, len(b))
	copy(out, b)
	inStr, esc := false, false
	for i := 0; i < len(out); i++ {
		c := out[i]
		switch {
		case inStr:
			if esc {
				esc = false
			} else if c == '\\' {
				esc = true
			} else if c == '"' {
				inStr = false
			}
		case c == '"':
			inStr = true
		case c == '/' && i+1 < len(out) && out[i+1] == '/':
			for ; i < len(out) && out[i] != '\n'; i++ {
				out[i] = ' '
			}
		case c == '/' && i+1 < len(out) && out[i+1] == '*':
			out[i], out[i+1] = ' ', ' '
			i += 2
			for ; i+1 < len(out) && !(out[i] == '*' && out[i+1] == '/'); i++ {
				if out[i] != '\n' {
					out[i] = ' '
				}
			}
			if i+1 < len(out) {
				out[i], out[i+1] = ' ', ' '
				i++
			}
		}
	}
	return out
}

// writeSchemaJSON prints the driver's config fields as JSON: name, label,
// type, constraints, help, and the zero value.
func writeSchemaJSON(w io.Writer, drv registry.Driver, fields []server.SettingField) error {
	type field struct {
		Name        string   `json:"name"`
		Label       string   `json:"label"`
		Type        string   `json:"type"`
		Default     string   `json:"default"`
		Constraints string   `json:"constraints,omitempty"`
		Options     []string `json:"options,omitempty"`
		Help        string   `json:"help,omitempty"`
		Locked      bool     `json:"startTime,omitempty"`
	}
	out := struct {
		Driver      string  `json:"driver"`
		Type        string  `json:"type"`
		Description string  `json:"description"`
		Fields      []field `json:"fields"`
	}{Driver: drv.Name, Type: string(drv.Type), Description: drv.Description}
	for _, f := range fields {
		out.Fields = append(out.Fields, field{f.Name, f.Label, f.Type, f.Value, f.Constraints, f.Options, f.Help, f.Locked})
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

// WriteCommentedDeviceFile writes a device file for the driver with every key
// commented at its default, so it changes nothing until a line is uncommented.
// driver stays live so the file names its driver, and enable is false so a seed
// does not start on install. It is what -schema commented prints and what an
// installer seeds a devices.d directory with.
func WriteCommentedDeviceFile(w io.Writer, drv registry.Driver, defaultPort int) error {
	var fields []server.SettingField
	if drv.Config != nil {
		var err error
		if fields, err = server.FieldsFromStruct(drv.Config(), nil, ""); err != nil {
			return err
		}
	}
	return writeSchemaCommented(w, drv, fields, defaultPort)
}

// writeSchemaCommented is WriteCommentedDeviceFile with the fields already
// rendered.
func writeSchemaCommented(w io.Writer, drv registry.Driver, fields []server.SettingField, defaultPort int) error {
	// Layout: driver first, the commented keys in the middle, enable last and
	// without a trailing comma. Every commented line carries its own trailing
	// comma, so uncommenting any of them yields valid JSON with no further
	// editing: the comma it carries separates it from enable below.
	var b strings.Builder
	fmt.Fprintf(&b, "{\n")
	fmt.Fprintf(&b, "  // %s\n", drv.Description)
	fmt.Fprintf(&b, "  // Uncomment a line to override its default.\n")
	fmt.Fprintf(&b, "  \"driver\": %q,\n", drv.Name)
	fmt.Fprintf(&b, "  // \"name\": \"\",              // display name\n")
	fmt.Fprintf(&b, "  // \"port\": %d,             // Alpaca HTTP port\n", defaultPort)
	fmt.Fprintf(&b, "  // \"device\": 0,             // device number within the port\n")
	for _, f := range fields {
		val := jsonValueForField(f)
		note := f.Label
		if f.Constraints != "" {
			note += " (" + f.Constraints + ")"
		}
		if f.Help != "" {
			note += ": " + f.Help
		}
		fmt.Fprintf(&b, "  // %-24s // %s\n", fmt.Sprintf("%q: %s,", f.Name, val), note)
	}
	fmt.Fprintf(&b, "  \"enable\": false           // set true to serve this device\n")
	fmt.Fprintf(&b, "}\n")
	_, err := io.WriteString(w, b.String())
	return err
}

// jsonValueForField renders a field's zero/default as a JSON literal.
func jsonValueForField(f server.SettingField) string {
	switch f.Type {
	case "checkbox":
		if f.Value == "" {
			return "false"
		}
		return f.Value
	case "number":
		if f.Value == "" {
			return "0"
		}
		return f.Value
	default:
		return strconv.Quote(f.Value)
	}
}

func rawString(s string) json.RawMessage { b, _ := json.Marshal(s); return b }
func rawInt(n int) json.RawMessage       { return json.RawMessage(strconv.Itoa(n)) }

func mustJSON(m map[string]json.RawMessage) json.RawMessage {
	b, _ := json.Marshal(m)
	return b
}

var commonKeys = func() map[string]bool {
	m := map[string]bool{}
	for _, k := range registry.CommonKeys() {
		m[strings.ToLower(k)] = true
	}
	return m
}()

func isCommonKey(k string) bool { return commonKeys[strings.ToLower(k)] }
