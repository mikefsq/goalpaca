package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// adapterCfg is the kind of tagged config struct a registry driver's Config
// func returns.
type adapterCfg struct {
	Serial     string `json:"serial"     alpaca:"label=Serial,when=start"`
	FixDefects bool   `json:"fixdefects" alpaca:"label=Hot-pixel correction"`
	FpsPercent int    `json:"fpsPercent" alpaca:"label=FPS percent,min=40,max=100"`
	Token      string `json:"token"      alpaca:"secret"`
}

// reconfigurableDevice implements Reconfigurable but NOT Configurable, so it
// only ever gets a form through the StructConfig adapter.
type reconfigurableDevice struct {
	BaseDevice
	mu     sync.Mutex
	got    []*adapterCfg
	reject error
}

func (d *reconfigurableDevice) Reconfigure(cfg any) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.reject != nil {
		return d.reject
	}
	d.got = append(d.got, cfg.(*adapterCfg))
	return nil
}

func (d *reconfigurableDevice) last() *adapterCfg {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.got) == 0 {
		return nil
	}
	return d.got[len(d.got)-1]
}

// plainDevice implements neither interface.
type plainDevice struct{ BaseDevice }

func newAdapterServer(t *testing.T, dev Device, raw string, pinned map[string]bool) (*Server, *StructConfig) {
	t.Helper()
	s := New(Config{ServerName: "t"})
	if err := s.Register(customType, 0, dev); err != nil {
		t.Fatal(err)
	}
	sc, err := NewStructConfig(dev, func() any { return &adapterCfg{} }, json.RawMessage(raw), pinned, "set in hurd.conf")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RegisterConfigurable(customType, 0, sc); err != nil {
		t.Fatal(err)
	}
	return s, sc
}

func TestStructConfigRendersAndApplies(t *testing.T) {
	dev := &reconfigurableDevice{}
	dev.ID, dev.DevName = "d1", "Adapter Cam"
	s, sc := newAdapterServer(t, dev,
		`{"driver":"x","serial":"abc","fixdefects":true,"fpsPercent":80,"token":"hunter2"}`,
		map[string]bool{"serial": true})

	// GET renders the generated form with the seeded values.
	w := do(t, s, http.MethodGet, "/setup/v1/customfocuser/0/setup", "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET: %d %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, want := range []string{
		`name="fixdefects"`, `checked`, // bool seeded true
		`name="fpsPercent"`, `value="80"`, // int seeded
		`name="serial"`, `disabled`, // pinned -> locked
		`type="password"`,  // secret
		`min 40 · max 100`, // constraints line
	} {
		if !strings.Contains(body, want) {
			t.Errorf("form missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "hunter2") {
		t.Error("secret value echoed to the browser")
	}

	// POST a live change; the device receives the full struct with the change.
	w = do(t, s, http.MethodPost, "/setup/v1/customfocuser/0/setup", "fpsPercent=60&fixdefects=true&token=hunter2")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Settings applied") {
		t.Fatalf("POST: %d %s", w.Code, w.Body.String())
	}
	got := dev.last()
	if got == nil {
		t.Fatal("Reconfigure not called")
	}
	if got.FpsPercent != 60 || !got.FixDefects || got.Serial != "abc" || got.Token != "hunter2" {
		t.Errorf("Reconfigure received %+v", got)
	}
	if sc.Values()["fpsPercent"] != "60" {
		t.Errorf("adapter did not commit: %v", sc.Values())
	}

	// A range violation is rejected before Reconfigure and nothing is committed.
	before := len(dev.got)
	w = do(t, s, http.MethodPost, "/setup/v1/customfocuser/0/setup", "fpsPercent=200")
	if !strings.Contains(w.Body.String(), "above the maximum") {
		t.Errorf("range error not shown: %s", w.Body.String())
	}
	if len(dev.got) != before || sc.Values()["fpsPercent"] != "60" {
		t.Error("rejected submission reached the device or was committed")
	}
}

func TestStructConfigStartFieldRefused(t *testing.T) {
	dev := &reconfigurableDevice{}
	s, _ := newAdapterServer(t, dev, `{"serial":"abc","fpsPercent":50}`, nil)
	// serial is when=start: the form renders it locked, so collectValues drops
	// it and a normal submit cannot change it. Drive the adapter directly to
	// prove the adapter refuses it even without the form's protection.
	sc, _ := configurableFor(s.devices[regKey{customType, 0}])
	err := sc.ApplySettings(map[string]string{"serial": "zzz"})
	if err == nil || !strings.Contains(err.Error(), "next start") {
		t.Errorf("start-time field change: err = %v", err)
	}
	if got := dev.last(); got != nil {
		t.Errorf("Reconfigure called for a start-time change: %+v", got)
	}
}

func TestStructConfigDeviceRejects(t *testing.T) {
	dev := &reconfigurableDevice{reject: errors.New("hardware says no")}
	s, sc := newAdapterServer(t, dev, `{"fpsPercent":50}`, nil)
	w := do(t, s, http.MethodPost, "/setup/v1/customfocuser/0/setup", "fpsPercent=70")
	if !strings.Contains(w.Body.String(), "hardware says no") {
		t.Errorf("device error not shown: %s", w.Body.String())
	}
	if sc.Values()["fpsPercent"] != "50" {
		t.Error("value committed despite device rejection")
	}
}

func TestStructConfigWithoutReconfigureIsReadOnly(t *testing.T) {
	dev := &plainDevice{}
	dev.ID, dev.DevName = "p", "Plain"
	s, _ := newAdapterServer(t, dev, `{"fpsPercent":50}`, nil)
	w := do(t, s, http.MethodGet, "/setup/v1/customfocuser/0/setup", "")
	body := w.Body.String()
	if !strings.Contains(body, `name="fpsPercent"`) || !strings.Contains(body, "does not accept live changes") {
		t.Errorf("expected a read-only generated form:\n%s", body)
	}
	if strings.Count(body, "disabled") < 3 { // serial, fixdefects, fpsPercent, token
		t.Errorf("fields should all be disabled:\n%s", body)
	}
	w = do(t, s, http.MethodPost, "/setup/v1/customfocuser/0/setup", "fpsPercent=70")
	if !strings.Contains(w.Body.String(), "does not accept live changes") {
		t.Errorf("POST should be refused: %s", w.Body.String())
	}
}

func TestOwnConfigurableBypassesAdapter(t *testing.T) {
	dev := newTestFocuser() // implements Configurable directly
	s := setupTestServer(t, dev)
	sc, err := NewStructConfig(dev, func() any { return &adapterCfg{} }, nil, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RegisterConfigurable(customType, 0, sc); err != nil {
		t.Fatal(err)
	}
	got, ok := configurableFor(s.devices[regKey{customType, 0}])
	if !ok || got != Configurable(dev) {
		t.Fatalf("configurableFor returned %T, want the device itself", got)
	}
	w := do(t, s, http.MethodGet, "/setup/v1/customfocuser/0/setup", "")
	if !strings.Contains(w.Body.String(), "Step size (microns)") || strings.Contains(w.Body.String(), "fpsPercent") {
		t.Errorf("device's own form should render, not the adapter's:\n%s", w.Body.String())
	}
}

func TestRegisterConfigurableUnknownDevice(t *testing.T) {
	s := New(Config{ServerName: "t"})
	sc, _ := NewStructConfig(&plainDevice{}, func() any { return &adapterCfg{} }, nil, nil, "")
	if err := s.RegisterConfigurable(customType, 9, sc); err == nil {
		t.Error("expected an error for an unregistered device")
	}
}

func TestNewStructConfigErrors(t *testing.T) {
	if _, err := NewStructConfig(&plainDevice{}, nil, nil, nil, ""); err == nil {
		t.Error("nil Config func accepted")
	}
	if _, err := NewStructConfig(&plainDevice{}, func() any { return &adapterCfg{} }, json.RawMessage(`{"fpsPercent":"notanint"}`), nil, ""); err == nil {
		t.Error("undecodable raw config accepted")
	}
}

func TestStructConfigStoredValueOutsideRangeRenders(t *testing.T) {
	dev := &reconfigurableDevice{}
	s, sc := newAdapterServer(t, dev, `{"fpsPercent":0}`, nil) // 0 < min 40
	w := do(t, s, http.MethodGet, "/setup/v1/customfocuser/0/setup", "")
	if !strings.Contains(w.Body.String(), `name="fpsPercent"`) || !strings.Contains(w.Body.String(), `value="0"`) {
		t.Fatalf("form should render the stored 0:\n%s", w.Body.String())
	}
	if len(sc.SettingsForm()) == 0 {
		t.Fatal("SettingsForm returned nothing for a stored out-of-range value")
	}
	// A submission of the same 0 is refused, since submissions are validated.
	if err := sc.ApplySettings(map[string]string{"fpsPercent": "0"}); err == nil || !strings.Contains(err.Error(), "below the minimum") {
		t.Errorf("submitting 0 should be refused: %v", err)
	}
}
