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
	"slices"
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

	// Scan lists the devices this driver can see attached to the machine now, so a host can
	// offer them to configure instead of asking an operator for a serial number they have no
	// way to know. Nil for a driver that cannot enumerate — a mount reached by address is
	// typed in — and a host offers the Config form instead of a scan.
	//
	// A scan touches the bus, so what it may do is part of the contract:
	//
	//   - It must not retain anything. Open only what reading the identity requires, release
	//     it before returning, and leave nothing claimed. A stuck USB claim outlives the
	//     process on some platforms.
	//   - It must not disturb a device already in use. A camera another process (or a
	//     connected instance of this same driver) holds is reported with the values that were
	//     readable and without the ones that would require taking it — not omitted, because an
	//     operator looking for the camera they just connected needs to see it listed.
	//   - It returns promptly and honours ctx. Hosts call this from a person's click, not from
	//     a polling loop; the results are pinned into a config entry so the bus is walked once.
	Scan func(ctx context.Context) ([]Found, error)

	// Extra lists further device types served from the same entry, beside the primary
	// Type (see Kind). Nil for a driver of one type. Each Extra kind needs its own Type,
	// different from the primary's and every other kind's, a New and a MultiKey.
	Extra []Kind

	// Identity names the config keys that address one device uniquely, most stable first.
	//
	// A driver knows which of its fields are identity and which are preference; nothing else
	// does. astrocam declares {"serial", "index"} — the factory serial survives a replug and
	// the enumeration index does not — where a networked mount declares {"addr"}. A host uses
	// the first key a scan filled to pin a device, and the order is what makes that choice
	// right without the host knowing anything about the driver.
	Identity []string
}

// Found is one device a Driver.Scan saw attached.
//
// Values are config-entry keys, not a fixed struct, because the identity of a device is
// driver-specific: a serial for a USB camera, a port path for a serial focuser, a host:port for
// a mount. Keying by the driver's own field names is what lets a host write the entry, and render
// it in the Config form, without knowing which driver it is holding.
type Found struct {
	// Label names the device in a picker row: the product name and whatever distinguishes one
	// unit from the next, e.g. "ASI6200MC Pro (serial 1a2b3c4d)".
	Label string

	// Values are the config keys this scan determined, ready to be written into a device entry.
	// A key the scan could not read — a serial on a camera that is currently in use — is absent
	// rather than empty, so a host can tell "not known" from "known to be blank".
	Values map[string]any
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

// CheckIdentity reports whether a driver's Identity names keys its Config struct actually
// declares.
//
// It exists because the two are wired together by STRING: Identity says "serial" and the struct
// says `json:"serial,omitempty"`, and a rename on one side leaves a host pinning a key no driver
// reads — a device that binds to nothing, discovered at connect time by an operator rather than at
// build time by a test. Drivers call it from their own tests; a driver with no Identity or no
// Config is not an error, only unchecked.
func CheckIdentity(d Driver) error {
	if len(d.Identity) == 0 || d.Config == nil {
		return nil
	}
	cfg := d.Config()
	if cfg == nil {
		return fmt.Errorf("driver %s: Config() returned nil", d.Name)
	}
	t := reflect.TypeOf(cfg)
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return fmt.Errorf("driver %s: Config() is %s, not a struct", d.Name, t.Kind())
	}
	have := map[string]bool{}
	for i := 0; i < t.NumField(); i++ {
		name := strings.Split(t.Field(i).Tag.Get("json"), ",")[0]
		if name == "" || name == "-" {
			name = t.Field(i).Name
		}
		have[strings.ToLower(name)] = true
	}
	for _, key := range d.Identity {
		if !have[strings.ToLower(key)] {
			return fmt.Errorf("driver %s: Identity names %q, which its config struct does not declare",
				d.Name, key)
		}
	}
	return nil
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
	// An entry's Extra kind arrays belong to those kinds, not to this one's strict decode.
	drv, _ := Lookup(s.Driver)
	for k := range m {
		if drv.extraKey(k) {
			delete(m, k)
			continue
		}
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
	seen := map[server.DeviceType]bool{d.Type: true}
	for _, k := range d.Extra {
		if k.New == nil || k.MultiKey == "" {
			panic("registry: Register " + d.Name + ": extra kind " + string(k.Type) + " needs New and MultiKey")
		}
		if seen[k.Type] {
			panic("registry: Register " + d.Name + ": device type " + string(k.Type) + " served twice")
		}
		seen[k.Type] = true
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

// MultiBlock is one per-device block of a MultiKey entry, e.g. astrocam's "cameras".
type MultiBlock struct {
	// Index is the block's position: the device number a host registers it under,
	// and the enumeration index a driver defaults its hardware binding to (Spec.Device).
	Index int
	// Raw is the block itself, host-owned keys included.
	Raw json.RawMessage
	// Name is the block's display-name override; "" when unset.
	Name string
	// Enable is the block's own flag; nil means unset, not false — the entry's flag still applies.
	Enable *bool
}

// MultiBlocks splits a multi-device config entry into its per-device blocks.
//
// The split belongs to the driver contract — which key holds the blocks, and what a
// block's position means — so two hosts reading one devices.d file agree about which
// hardware it describes.
//
// What an absent or empty block list MEANS is left to the caller: a serving host may
// default to empty slots for a setup page, while a discovery list must not invent
// devices nobody declared.
//
// Returns ok=false when the entry is not multi-device: the driver is unregistered, has
// no MultiKey, or carries flat driver keys instead of blocks.
func MultiBlocks(driver string, raw json.RawMessage) (blocks []MultiBlock, ok bool, err error) {
	drv, found := Lookup(driver)
	if !found || drv.MultiKey == "" {
		return nil, false, nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, false, fmt.Errorf("registry: %s entry: %w", driver, err)
	}
	blocksRaw, has := m[drv.MultiKey]
	if !has {
		// Flat driver keys mean one device pinned without the array; nothing at all
		// is an empty list and the caller's default.
		for k := range m {
			if drv.extraKey(k) {
				continue
			}
			if !slices.ContainsFunc(commonKeys, func(c string) bool { return strings.EqualFold(c, k) }) {
				return nil, false, nil
			}
		}
		return nil, true, nil
	}
	var raws []json.RawMessage
	if err := json.Unmarshal(blocksRaw, &raws); err != nil {
		return nil, false, fmt.Errorf("registry: %s entry: %q: %w", driver, drv.MultiKey, err)
	}
	for i, r := range raws {
		var b struct {
			Name   string `json:"name"`
			Enable *bool  `json:"enable"`
		}
		if err := json.Unmarshal(r, &b); err != nil {
			return nil, false, fmt.Errorf("registry: %s entry: %s[%d]: %w", driver, drv.MultiKey, i, err)
		}
		blocks = append(blocks, MultiBlock{
			Index: i, Raw: append(json.RawMessage(nil), r...), Name: b.Name, Enable: b.Enable,
		})
	}
	return blocks, true, nil
}
