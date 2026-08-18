// Package registry is the process-wide driver catalogue that lets a composed
// Alpaca server (the alpacahurd "herd of alpaca daemons", or any other host)
// construct devices from a config file without compile-time knowledge of each
// driver.
//
// A driver package registers itself from init():
//
//	func init() {
//		registry.Register(registry.Driver{
//			Name:          "tenmicron",
//			Type:          server.TelescopeType,
//			Description:   "10Micron GM-series mount over TCP",
//			ConfigExample: `{ "driver": "tenmicron", "addr": "10.0.1.51:3492" }`,
//			New:           newFromSpec,
//		})
//	}
//
// so a host binary selects drivers purely by importing their packages (typically
// via a generated file of blank imports), and the set of available drivers is
// exactly the set compiled in.
//
// # Config ownership
//
// One JSON config entry declares one device. The host owns the common keys
// (CommonKeys: "driver", "name", "enable", "port", "device", "exec", the
// INDI/LX200 front-end keys, and the optics block); everything else in the entry belongs to the
// driver, which decodes it from Spec.Raw via Spec.Decode into its own config
// struct. Decode is strict — unknown keys are errors — so a typo in a device
// entry is reported instead of silently ignored, without the host needing to
// know any driver's fields. A driver must not name its own fields after a
// common key: those are stripped before Decode sees them.
package registry

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"sync"

	server "github.com/mikefsq/goalpaca/server"
)

// Driver describes one registered device driver.
type Driver struct {
	// Name is the config "driver" key that selects this driver. Lowercase by
	// convention; lookup is case-insensitive.
	Name string

	// Type is the ASCOM device type the constructed device registers as.
	Type server.DeviceType

	// Description is a one-line human summary for driver listings.
	Description string

	// ConfigExample is a complete example config entry for this driver: one JSON
	// object including the "driver" key and this driver's own fields, WITHOUT
	// "port" (the host injects a free port when it assembles a full example
	// config). It must parse as JSON; hosts may print it verbatim or merge it
	// into a generated starter config.
	ConfigExample string

	// New constructs the device from its config entry. It must not touch
	// hardware: acquisition happens later, in the device's own lifecycle
	// (acquire, monitor, re-acquire), so a device can be configured before its
	// hardware is attached.
	New func(Spec) (server.Device, error)

	// Config, when set, returns a pointer to a zero value of the driver's config
	// struct: the same struct New decodes its entry into. The struct's fields
	// carry `json` tags naming the config keys and `alpaca` tags describing the
	// setup form (server.ParseSettingTag), so a host can render a browser
	// configuration form and validate submissions without per-driver form code.
	// Optional; a driver that leaves it nil gets the "no configurable settings"
	// setup page unless it implements server.Configurable itself.
	Config func() any
}

// Spec is the driver-facing view of one device config entry.
type Spec struct {
	Driver string // the entry's "driver" key (canonical registered casing)
	Name   string // the entry's "name" display-name override; "" when unset

	// Instance is the entry's identity as the host names it: a devices.d file's
	// stem, or "" for an inline entry. It is the word that joins a host's log
	// lines to a driver's, so a driver puts it in its own log lines through
	// server.BaseDevice.Label.
	Instance string

	// Raw is the entire JSON config entry, common keys included. Drivers decode
	// their own fields from it with Decode.
	Raw json.RawMessage
}

// commonKeys are the host-owned config entry keys, stripped by Decode before a
// driver's strict decode. Matched case-insensitively.
var commonKeys = []string{
	"driver", "name", "enable", "port", "device", "exec",
	"indi", "lx200Port",
	"aperture", "apertureArea", "focalLength",
	"guiderAperture", "guiderFocalLength", "guideRate",
}

// CommonKeys returns the host-owned config entry keys (see package doc). Hosts
// can test their config structs against it to keep the two in sync.
func CommonKeys() []string { return append([]string(nil), commonKeys...) }

// Decode unmarshals the driver-owned fields of the config entry into v,
// rejecting unknown keys. The host-owned common keys (CommonKeys) are stripped
// first, so v declares only the driver's own fields. A nil/empty Raw decodes as
// an empty object, for drivers with no fields of their own.
func (s Spec) Decode(v any) error {
	if len(s.Raw) == 0 {
		return nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(s.Raw, &m); err != nil {
		return fmt.Errorf("device entry: %w", err)
	}
	for k := range m {
		for _, c := range commonKeys {
			if strings.EqualFold(k, c) {
				delete(m, k)
				break
			}
		}
	}
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("%s config: %w", s.Driver, err)
	}
	return nil
}

var (
	mu      sync.RWMutex
	drivers = map[string]Driver{} // key: lowercase Name
)

// Register adds a driver to the catalogue. It is called from driver package
// init() functions; a duplicate name, empty name, or nil New is a programmer
// error and panics.
func Register(d Driver) {
	if d.Name == "" {
		panic("registry: Register with empty Name")
	}
	if d.New == nil {
		panic("registry: Register " + d.Name + " with nil New")
	}
	key := strings.ToLower(d.Name)
	mu.Lock()
	defer mu.Unlock()
	if _, dup := drivers[key]; dup {
		panic("registry: duplicate driver " + d.Name)
	}
	drivers[key] = d
}

// PackagePath is the import path of the package that registered d, read off
// its New function, so a host can name the driver's module and look its
// version up in the binary's build info (debug.ReadBuildInfo). "" when New is
// nil.
func (d Driver) PackagePath() string {
	if d.New == nil {
		return ""
	}
	fn := runtime.FuncForPC(reflect.ValueOf(d.New).Pointer())
	if fn == nil {
		return ""
	}
	name := fn.Name() // "github.com/x/y/pkg.init.func1" or "github.com/x/y/pkg.newDev"
	// The package path ends at the first dot after the last slash.
	slash := strings.LastIndex(name, "/")
	if dot := strings.Index(name[slash+1:], "."); dot >= 0 {
		return name[:slash+1+dot]
	}
	return name
}

// ModuleVersion is the version of the module that provides d, from the
// binary's build info: a tagged version, a pseudo-version, or "(devel)" for a
// workspace or replaced checkout. "" when the binary carries no build info or
// the module is not among its dependencies (the main module, say).
func (d Driver) ModuleVersion() string {
	pkg := d.PackagePath()
	if pkg == "" {
		return ""
	}
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	best := ""
	version := ""
	for _, dep := range bi.Deps {
		m := dep
		if m.Replace != nil {
			m = m.Replace
		}
		if (pkg == dep.Path || strings.HasPrefix(pkg, dep.Path+"/")) && len(dep.Path) > len(best) {
			best, version = dep.Path, m.Version
		}
	}
	if best == "" && (pkg == bi.Main.Path || strings.HasPrefix(pkg, bi.Main.Path+"/")) {
		return bi.Main.Version
	}
	return version
}

// Lookup returns the driver registered under name (case-insensitive).
func Lookup(name string) (Driver, bool) {
	mu.RLock()
	defer mu.RUnlock()
	d, ok := drivers[strings.ToLower(name)]
	return d, ok
}

// All returns every registered driver, sorted by name.
func All() []Driver {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]Driver, 0, len(drivers))
	for _, d := range drivers {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
