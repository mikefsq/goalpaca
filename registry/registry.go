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
// (CommonKeys: "driver", "name", "enable", "port", the INDI/LX200 front-end
// keys, and the optics block); everything else in the entry belongs to the
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
	// (acquire → monitor → re-acquire), so a device can be configured before its
	// hardware is attached.
	New func(Spec) (server.Device, error)
}

// Spec is the driver-facing view of one device config entry.
type Spec struct {
	Driver string // the entry's "driver" key (canonical registered casing)
	Name   string // the entry's "name" display-name override; "" when unset

	// Raw is the entire JSON config entry, common keys included. Drivers decode
	// their own fields from it with Decode.
	Raw json.RawMessage
}

// commonKeys are the host-owned config entry keys, stripped by Decode before a
// driver's strict decode. Matched case-insensitively.
var commonKeys = []string{
	"driver", "name", "enable", "port",
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
