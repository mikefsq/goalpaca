package server

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	focus := filepath.Join(dir, "sub", "focus-1.json") // sub/ does not exist yet
	cam := filepath.Join(dir, "cam-1.json")
	s := NewFileStore()

	// A missing file loads as a nil map with no error.
	got, err := s.Load(filepath.Join(dir, "nope.json"))
	if err != nil || got != nil {
		t.Fatalf("Load(missing) = %v, %v; want nil, nil", got, err)
	}

	if err := s.Save(focus, map[string]any{"stepsize": "12", "reversed": "true"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := s.Save(cam, map[string]any{"setpoint": "-10"}); err != nil {
		t.Fatalf("Save cam: %v", err)
	}

	// A fresh store reads each device's own file.
	s2 := NewFileStore()
	got, err = s2.Load(focus)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got["stepsize"] != "12" || got["reversed"] != "true" {
		t.Errorf("focus-1 = %v", got)
	}
	if c, _ := s2.Load(cam); c["setpoint"] != "-10" {
		t.Errorf("cam-1 = %v", c)
	}

	// The file is 0600 and a re-save replaces its whole content.
	if fi, err := os.Stat(focus); err != nil {
		t.Fatalf("stat: %v", err)
	} else if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("mode = %o, want 600", perm)
	}
	if err := s.Save(focus, map[string]any{"stepsize": "20"}); err != nil {
		t.Fatalf("re-save: %v", err)
	}
	if got, _ := NewFileStore().Load(focus); got["stepsize"] != "20" || got["reversed"] != nil {
		t.Errorf("after re-save focus-1 = %v, want only stepsize=20", got)
	}
}

func TestSettingsPathDefaultAndOverride(t *testing.T) {
	t.Setenv("ALPACA_STATE_DIR", "/state")
	s := New(Config{ServerName: "rig", Settings: NewFileStore()})
	if err := s.Register(customType, 0, newTestFocuser()); err != nil {
		t.Fatal(err)
	}
	rd := s.devices[regKey{customType, 0}]
	if got, want := s.settingsKey(rd), filepath.Join("/state", "customfocuser-0.json"); got != want {
		t.Errorf("default key = %q, want %q", got, want)
	}
	if err := s.SettingsPath(customType, 0, "/var/lib/hurd/devices/main.json"); err != nil {
		t.Fatal(err)
	}
	if got := s.settingsKey(rd); got != "/var/lib/hurd/devices/main.json" {
		t.Errorf("override key = %q", got)
	}
	if err := s.SettingsPath(customType, 9, "/x"); err == nil {
		t.Error("SettingsPath on an unregistered device should fail")
	}
}

func TestServerLoadsPersistedSettingsAtStartup(t *testing.T) {
	dev := newTestFocuser() // stepSize starts at "5"
	path := filepath.Join(t.TempDir(), "focus.json")
	store := NewFileStore()
	if err := store.Save(path, map[string]any{"stepsize": "42", "reversed": "true"}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	s := New(Config{ServerName: "rig", Settings: store})
	if err := s.Register(customType, 0, dev); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := s.SettingsPath(customType, 0, path); err != nil {
		t.Fatal(err)
	}
	s.loadSettings() // what Run calls before opening hardware

	if dev.stepSize != "42" || !dev.reversed {
		t.Errorf("persisted settings not applied: stepSize=%q reversed=%v", dev.stepSize, dev.reversed)
	}
}

func TestPersistenceSkipsHostPinnedField(t *testing.T) {
	// Precedence: default < persisted < host override. A persisted value for a
	// host-pinned field must NOT override the host's value at startup.
	dev := newTestFocuser()
	dev.serial = "PINNED"
	dev.pinned = true

	path := filepath.Join(t.TempDir(), "focus.json")
	store := NewFileStore()
	if err := store.Save(path, map[string]any{
		"stepsize": "50",    // not pinned: applies
		"serial":   "STALE", // pinned: ignored
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	s := New(Config{ServerName: "rig", Settings: store})
	if err := s.Register(customType, 0, dev); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := s.SettingsPath(customType, 0, path); err != nil {
		t.Fatal(err)
	}
	s.loadSettings()

	if dev.stepSize != "50" {
		t.Errorf("unpinned persisted value not applied: stepSize=%q", dev.stepSize)
	}
	if dev.serial != "PINNED" {
		t.Errorf("persistence overrode host-pinned field: serial=%q, want PINNED", dev.serial)
	}
}

func TestSnapshotExcludesLockedField(t *testing.T) {
	// Saving through the web form must not persist a pinned field's value.
	dev := newTestFocuser()
	dev.serial = "PINNED"
	dev.pinned = true
	path := filepath.Join(t.TempDir(), "focus.json")
	s := New(Config{ServerName: "rig", Settings: NewFileStore()})
	if err := s.Register(customType, 0, dev); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := s.SettingsPath(customType, 0, path); err != nil {
		t.Fatal(err)
	}

	if w := do(t, s, http.MethodPost, "/setup/v1/customfocuser/0/setup", "stepsize=8"); w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	got, _ := NewFileStore().Load(path)
	if _, ok := got["serial"]; ok {
		t.Errorf("pinned field was persisted: %v", got)
	}
	if got["stepsize"] != "8" {
		t.Errorf("editable field not persisted: %v", got)
	}
}

func TestSetupSavesOnApply(t *testing.T) {
	dev := newTestFocuser()
	path := filepath.Join(t.TempDir(), "focus.json")
	s := New(Config{ServerName: "rig", Settings: NewFileStore()})
	if err := s.Register(customType, 0, dev); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := s.SettingsPath(customType, 0, path); err != nil {
		t.Fatal(err)
	}

	w := do(t, s, http.MethodPost, "/setup/v1/customfocuser/0/setup", "stepsize=99&reversed=true")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "applied and saved") {
		t.Errorf("expected saved banner:\n%s", w.Body.String())
	}

	// Persisted to disk: a fresh store reads back the applied values.
	got, err := NewFileStore().Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got["stepsize"] != "99" || got["reversed"] != "true" {
		t.Errorf("persisted = %v, want stepsize=99 reversed=true", got)
	}
}

// clampingDevice accepts any step size but stores it clamped to 10, and
// returns nil: a partial apply. What is persisted must be the submitted value,
// so the file reflects the user's request rather than the device's clamp.
type clampingDevice struct {
	configurableFocuser
}

func (d *clampingDevice) ApplySettings(v map[string]string) error {
	if err := d.configurableFocuser.ApplySettings(v); err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.stepSize > "10" && len(d.stepSize) >= 2 { // crude: "99" > "10"
		d.stepSize = "10"
	}
	return nil
}

func TestPersistedValuesAreTheSubmission(t *testing.T) {
	dev := &clampingDevice{}
	dev.stepSize = "5"
	dev.ID, dev.DevName, dev.Desc = "clamp-1", "Clamp", "clamps step size"
	path := filepath.Join(t.TempDir(), "clamp.json")
	s := New(Config{ServerName: "rig", Settings: NewFileStore()})
	if err := s.Register(customType, 0, dev); err != nil {
		t.Fatal(err)
	}
	if err := s.SettingsPath(customType, 0, path); err != nil {
		t.Fatal(err)
	}
	if w := do(t, s, http.MethodPost, "/setup/v1/customfocuser/0/setup", "stepsize=99"); w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if dev.stepSize != "10" {
		t.Fatalf("device should have clamped to 10, has %q", dev.stepSize)
	}
	got, _ := NewFileStore().Load(path)
	if got["stepsize"] != "99" {
		t.Errorf("persisted %q, want the submitted 99", got["stepsize"])
	}
	// Fields not in the submission keep their prior editable value.
	if got["reversed"] != "false" {
		t.Errorf("unsubmitted field: %v", got)
	}
}

func TestLoadSkipsUndeclaredKeys(t *testing.T) {
	dev := newTestFocuser()
	path := filepath.Join(t.TempDir(), "focus.json")
	store := NewFileStore()
	if err := store.Save(path, map[string]any{"stepsize": "42", "port": 11300, "dropped": "x"}); err != nil {
		t.Fatal(err)
	}
	s := New(Config{ServerName: "rig", Settings: store})
	if err := s.Register(customType, 0, dev); err != nil {
		t.Fatal(err)
	}
	if err := s.SettingsPath(customType, 0, path); err != nil {
		t.Fatal(err)
	}
	s.loadSettings()
	if dev.stepSize != "42" {
		t.Errorf("declared key not applied: stepSize=%q", dev.stepSize)
	}
}
