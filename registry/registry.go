// Package registry catalogs drivers that hosts construct from configuration.
// Drivers register from init; hosts select available drivers through imports.
// Spec.Decode strips host-owned CommonKeys and rejects unknown driver fields.
package registry

import (
	"bytes"
	"context"
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

	// ConfigExample is a valid JSON device entry including the driver key and
	// driver-specific fields. Omit port so the host can assign one.
	ConfigExample string

	// New constructs a device without accessing hardware. Acquire hardware in
	// the device's Hardware lifecycle.
	New func(Spec) (server.Device, error)

	// MultiKey names an optional array of devices in an entry. Supporting hosts
	// expand each block into a Spec, using its array index as Device. Hosts that
	// do not expand blocks ignore this field.
	MultiKey string

	// FrontEnd starts an optional protocol front-end after registration. Errors
	// leave the Alpaca device serving without the front-end.
	//
	// Return promptly; start serving in goroutines that stop when ctx ends.
	// The host cancels ctx on unregistration and calls FrontEnd on re-registration.
	// Resolve dev on each use and handle nil during reload or unregistration.
	// entry is the flat config entry or the individual MultiKey block.
	// Bind the supplied hosts; an empty slice means all interfaces.
	FrontEnd func(ctx context.Context, dev func() server.Device, entry json.RawMessage, hosts []string) error

	// Config returns a pointer to a zero-valued config struct used by New.
	// Its json and alpaca tags define the generated setup form. A device's own
	// server.Configurable implementation takes precedence.
	Config func() any
}

// Spec is the driver-facing view of one device config entry.
type Spec struct {
	Driver string // the entry's "driver" key (canonical registered casing)
	Name   string // the entry's "name" display-name override; "" when unset

	// Instance is the host's entry name, usually a device file's stem.
	// Copy it to server.BaseDevice.Instance for consistent log labels.
	Instance string

	// Raw is the entire JSON config entry, common keys included. Drivers decode
	// their own fields from it with Decode.
	Raw json.RawMessage

	// Device is the host-assigned device number, or zero when not yet known.
	// A MultiKey expansion uses the block's array index.
	Device int
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
