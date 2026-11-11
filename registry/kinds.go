package registry

import (
	"encoding/json"
	"fmt"
	"strings"

	server "github.com/mikefsq/goalpaca/server"
)

// Kind is one device type a driver serves from a config entry.
//
// A driver's primary kind is its Type, New, MultiKey and Config fields; Driver.Kinds
// returns it first. Driver.Extra adds more types served from the same entry on the
// same port, for hardware that is several ASCOM devices at once (a power hub that is
// also a weather station). A host serves every kind of one entry in one process, so
// the kinds of a driver may share an open hardware handle; how they share it is the
// driver's business.
type Kind struct {
	// Type is the ASCOM device type the kind's devices register as.
	Type server.DeviceType

	// MultiKey names the entry's array of this kind's blocks, one device per block.
	// Required for an Extra kind: it is how a file turns the kind on or off. A block
	// also names the kind's state file (see Kind.StateSuffix).
	MultiKey string

	// New constructs one device of the kind without accessing hardware.
	New func(Spec) (server.Device, error)

	// Config returns a pointer to the kind's zero-valued config struct, as Driver.Config.
	Config func() any

	// Default serves one device of an Extra kind when the entry has no MultiKey array
	// for it. False makes the kind opt-in: the file names the key to get a device.
	// The primary kind is always served.
	Default bool
}

// Kinds returns the kinds a driver serves: its primary kind first, then Extra in order.
// A driver without Extra returns the primary alone, so a host that iterates Kinds
// serves an old driver exactly as before.
func (d Driver) Kinds() []Kind {
	out := []Kind{{Type: d.Type, MultiKey: d.MultiKey, New: d.New, Config: d.Config, Default: true}}
	return append(out, d.Extra...)
}

// StateSuffix is the part a host adds to an instance's state-file stem for this kind:
// "" for the primary kind, so existing state files keep their names, and "." plus the
// lowercase type for an Extra kind.
func (d Driver) StateSuffix(k Kind) string {
	if k.Type == d.Type {
		return ""
	}
	return "." + strings.ToLower(string(k.Type))
}

// extraKey reports whether key is the MultiKey of one of d's Extra kinds.
func (d Driver) extraKey(key string) bool {
	for _, k := range d.Extra {
		if strings.EqualFold(k.MultiKey, key) {
			return true
		}
	}
	return false
}

// ExtraBlocks splits an entry into the blocks of one Extra kind.
//
// With the kind's MultiKey array present, each element is one block, and its Raw is
// the entry's driver keys with the block's keys over them: a block inherits the
// hardware binding (a serial, say) from the entry and overrides what it names. Other
// Extra kinds' arrays are left out, so a kind's strict Decode sees only its own keys.
// Without the array, a Default kind gets one block holding the entry's driver keys and
// no name; a kind that is not Default gets none.
//
// The entry's "name", "enable", "port" and "device" stay with the primary kind: a
// block names its own device and switches itself off with its own "enable".
func ExtraBlocks(d Driver, k Kind, raw json.RawMessage) ([]MultiBlock, error) {
	var m map[string]json.RawMessage
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil, fmt.Errorf("registry: %s entry: %w", d.Name, err)
		}
	}
	base := map[string]json.RawMessage{}
	for key, v := range m {
		// Kind arrays, the primary's included, belong to their own kinds.
		if d.extraKey(key) || (d.MultiKey != "" && strings.EqualFold(key, d.MultiKey)) {
			continue
		}
		switch strings.ToLower(key) {
		case "name", "enable", "device":
			continue
		}
		base[key] = v
	}
	var blocksRaw json.RawMessage
	for key, v := range m {
		if strings.EqualFold(key, k.MultiKey) {
			blocksRaw = v
		}
	}
	if blocksRaw == nil {
		if !k.Default {
			return nil, nil
		}
		b, err := json.Marshal(base)
		if err != nil {
			return nil, err
		}
		return []MultiBlock{{Index: 0, Raw: b}}, nil
	}
	var raws []json.RawMessage
	if err := json.Unmarshal(blocksRaw, &raws); err != nil {
		return nil, fmt.Errorf("registry: %s entry: %q: %w", d.Name, k.MultiKey, err)
	}
	out := make([]MultiBlock, 0, len(raws))
	for i, r := range raws {
		var over map[string]json.RawMessage
		if err := json.Unmarshal(r, &over); err != nil {
			return nil, fmt.Errorf("registry: %s entry: %s[%d]: %w", d.Name, k.MultiKey, i, err)
		}
		merged := make(map[string]json.RawMessage, len(base)+len(over))
		for key, v := range base {
			merged[key] = v
		}
		for key, v := range over {
			merged[key] = v
		}
		b, err := json.Marshal(merged)
		if err != nil {
			return nil, err
		}
		blk := MultiBlock{Index: i, Raw: b}
		_ = json.Unmarshal(over["name"], &blk.Name)
		if e, ok := over["enable"]; ok {
			var on bool
			if err := json.Unmarshal(e, &on); err != nil {
				return nil, fmt.Errorf("registry: %s entry: %s[%d]: enable: %w", d.Name, k.MultiKey, i, err)
			}
			blk.Enable = &on
		}
		out = append(out, blk)
	}
	return out, nil
}
