package server

import (
	"encoding/json"
	"fmt"
	"sync"
)

// Reconfigurable receives a fresh config struct containing current values and
// submitted changes. Return an error to reject the submission.
// Without this interface, a generated setup form is read-only.
type Reconfigurable interface {
	Reconfigure(cfg any) error
}

// StructConfig implements Configurable using a tagged config struct.
// Construct it with NewStructConfig and attach it with RegisterConfigurable.
// A device's own Configurable implementation takes precedence.
type StructConfig struct {
	newCfg  func() any      // returns a pointer to a zero config struct
	dev     Device          // the device changes are delivered to
	pinned  map[string]bool // host-owned keys, rendered locked
	source  string          // Source note for pinned fields
	mu      sync.Mutex
	current map[string]string // form key -> current value
}

// NewStructConfig returns an adapter for dev over the config struct newCfg
// produces. raw is the device's effective config entry as JSON (the same bytes
// the driver decoded); it seeds the current values, and keys absent from it
// take the struct's zero values. pinned names host-owned keys to render locked,
// with source as the note beside them.
func NewStructConfig(dev Device, newCfg func() any, raw json.RawMessage, pinned map[string]bool, source string) (*StructConfig, error) {
	if newCfg == nil {
		return nil, fmt.Errorf("config adapter: nil Config func")
	}
	cfg := newCfg()
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, cfg); err != nil {
			return nil, fmt.Errorf("config adapter: decode config: %w", err)
		}
	}
	fields, err := FieldsFromStruct(cfg, nil, "")
	if err != nil {
		return nil, err
	}
	cur := make(map[string]string, len(fields))
	for _, f := range fields {
		cur[f.Name] = f.Value
	}
	// FieldsFromStruct blanks a secret's Value; keep the real one internally so
	// a submission that leaves it untouched does not clear it.
	if err := fillSecrets(cfg, cur); err != nil {
		return nil, err
	}
	return &StructConfig{newCfg: newCfg, dev: dev, pinned: pinned, source: source, current: cur}, nil
}

// fillSecrets copies secret fields' true values into cur, which
// FieldsFromStruct left blank for display.
func fillSecrets(cfg any, cur map[string]string) error {
	fields, err := walkConfigStruct(cfg)
	if err != nil {
		return err
	}
	for _, f := range fields {
		if f.tag.Secret {
			cur[f.name] = fieldValueString(f.value)
		}
	}
	return nil
}

// struct_ builds a config struct holding the adapter's current values. The
// values are not validated: they came from the config file, not a submission,
// and a stored value outside a tag's range (a zero that means "unset") must
// still render.
func (a *StructConfig) struct_() (any, error) {
	cfg := a.newCfg()
	if err := applyToStruct(cfg, a.current, false); err != nil {
		return nil, err
	}
	return cfg, nil
}

// SettingsForm implements Configurable.
func (a *StructConfig) SettingsForm() []SettingField {
	a.mu.Lock()
	defer a.mu.Unlock()
	cfg, err := a.struct_()
	if err != nil {
		return nil
	}
	fields, err := FieldsFromStruct(cfg, a.pinned, a.source)
	if err != nil {
		return nil
	}
	if _, ok := a.dev.(Reconfigurable); !ok {
		// Nothing can act on a change, so show the values read-only.
		for i := range fields {
			if !fields[i].Locked {
				fields[i].Locked = true
				fields[i].Source = "read-only: this device does not accept live changes"
			}
		}
	}
	return fields
}

// ApplySettings implements Configurable. It validates and writes the submitted
// values into a fresh struct alongside the current ones, hands that struct to
// the device's Reconfigure, and commits the values only when Reconfigure
// accepts them.
func (a *StructConfig) ApplySettings(values map[string]string) error {
	rc, ok := a.dev.(Reconfigurable)
	if !ok {
		return fmt.Errorf("this device does not accept live changes")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	next := make(map[string]string, len(a.current)+len(values))
	for k, v := range a.current {
		next[k] = v
	}
	for k, v := range values {
		if a.pinned[k] {
			continue // collectValues drops pinned keys before this point; a caller that bypasses it gets the same refusal
		}
		next[k] = v
	}
	cfg := a.newCfg()
	if err := ApplyToStruct(cfg, next); err != nil {
		return err
	}
	// Refuse changes to start-time fields: they belong to the config file.
	fields, err := walkConfigStruct(cfg)
	if err != nil {
		return err
	}
	for _, f := range fields {
		if f.tag.When == WhenStart {
			if v, sent := values[f.name]; sent && v != a.current[f.name] {
				return fmt.Errorf("%s applies at the next start; set it in the config file", f.name)
			}
		}
	}
	if err := rc.Reconfigure(cfg); err != nil {
		return err
	}
	a.current = next
	return nil
}

// PersistValues returns the adapter's current editable values as typed JSON
// values (bool, number, string) rather than form strings, so a persisted file
// decodes under the driver's typed config struct at the next start. Pinned
// keys are left out: they belong to the host's own file.
func (a *StructConfig) PersistValues() map[string]any {
	a.mu.Lock()
	defer a.mu.Unlock()
	cfg, err := a.struct_()
	if err != nil {
		return nil
	}
	fields, err := walkConfigStruct(cfg)
	if err != nil {
		return nil
	}
	out := make(map[string]any, len(fields))
	for _, f := range fields {
		if a.pinned[f.name] {
			continue
		}
		out[f.name] = f.value.Interface()
	}
	return out
}

// Values returns a copy of the adapter's current values, for persistence.
func (a *StructConfig) Values() map[string]string {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make(map[string]string, len(a.current))
	for k, v := range a.current {
		out[k] = v
	}
	return out
}

var _ Configurable = (*StructConfig)(nil)

// RegisterConfigurable attaches a setup form to a registered device.
// If the server is running, it applies persisted settings immediately.
func (s *Server) RegisterConfigurable(devType DeviceType, number int, c Configurable) error {
	s.mu.Lock()
	rd, ok := s.devices[regKey{devType, number}]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("goalpaca: %s device %d is not registered", devType, number)
	}
	rd.cfg = c
	running := s.runCtx != nil
	s.mu.Unlock()
	if running {
		// Added to a running server: the persisted settings were not applied
		// at Register, since nothing could take them; apply them now.
		s.loadSettingsFor(rd)
	}
	return nil
}

// configurableFor returns the Configurable for a registered device: the
// device's own implementation when it has one, else the adapter attached by
// RegisterConfigurable, else nil.
func configurableFor(rd *registeredDevice) (Configurable, bool) {
	if c, ok := rd.dev.(Configurable); ok {
		return c, true
	}
	if rd.cfg != nil {
		return rd.cfg, true
	}
	return nil, false
}
