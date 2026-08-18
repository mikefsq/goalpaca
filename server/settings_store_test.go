package server

import (
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestFileStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "settings.json") // sub/ does not exist yet
	s := NewFileStore("rig", path)

	// Unknown id → nil map, no error.
	got, err := s.Load("nope")
	if err != nil || got != nil {
		t.Fatalf("Load(unknown) = %v, %v; want nil, nil", got, err)
	}

	if err := s.Save("focus-1", map[string]string{"stepsize": "12", "reversed": "true"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := s.Save("cam-1", map[string]string{"setpoint": "-10"}); err != nil {
		t.Fatalf("Save cam: %v", err)
	}

	// A fresh store reading the same file sees both devices independently.
	s2 := NewFileStore("rig", path)
	got, err = s2.Load("focus-1")
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got["stepsize"] != "12" || got["reversed"] != "true" {
		t.Errorf("focus-1 = %v", got)
	}
	if cam, _ := s2.Load("cam-1"); cam["setpoint"] != "-10" {
		t.Errorf("cam-1 = %v", cam)
	}

	// File is 0600 and re-save overwrites the same key.
	if fi, err := os.Stat(path); err != nil {
		t.Fatalf("stat: %v", err)
	} else if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("mode = %o, want 600", perm)
	}
	if err := s.Save("focus-1", map[string]string{"stepsize": "20"}); err != nil {
		t.Fatalf("re-save: %v", err)
	}
	if got, _ := NewFileStore("rig", path).Load("focus-1"); got["stepsize"] != "20" || got["reversed"] != "" {
		t.Errorf("after re-save focus-1 = %v, want only stepsize=20", got)
	}
}

func TestStateDirPriority(t *testing.T) {
	// 1. ALPACA_STATE_DIR wins over everything.
	t.Setenv("ALPACA_STATE_DIR", "/explicit/dir")
	t.Setenv("STATE_DIRECTORY", "/var/lib/alpaca/asiccd")
	if d := StateDir("asiccd"); d != "/explicit/dir" {
		t.Errorf("ALPACA_STATE_DIR: got %q", d)
	}

	// 2. STATE_DIRECTORY (systemd) next; first of a list separator.
	t.Setenv("ALPACA_STATE_DIR", "")
	list := "/var/lib/alpaca/asiccd" + string(os.PathListSeparator) + "/var/lib/other"
	t.Setenv("STATE_DIRECTORY", list)
	if d := StateDir("asiccd"); d != "/var/lib/alpaca/asiccd" {
		t.Errorf("STATE_DIRECTORY: got %q", d)
	}

	// 3. Per-user fallback when neither is set (Linux → XDG_STATE_HOME).
	t.Setenv("STATE_DIRECTORY", "")
	if runtime.GOOS == "linux" {
		t.Setenv("XDG_STATE_HOME", "/home/u/.local/state")
		if d := StateDir("asiccd"); d != "/home/u/.local/state/asiccd" {
			t.Errorf("XDG_STATE_HOME: got %q", d)
		}
	} else {
		// darwin/windows: per-user config base + name.
		if d := StateDir("asiccd"); !strings.HasSuffix(d, "asiccd") {
			t.Errorf("per-user: got %q", d)
		}
	}
}

func TestStateDirSanitizesName(t *testing.T) {
	t.Setenv("ALPACA_STATE_DIR", "")
	t.Setenv("STATE_DIRECTORY", "")
	if runtime.GOOS == "linux" {
		// Separators become '_' so the name is always a single, safe path
		// segment; dots are kept (harmless inside one segment).
		t.Setenv("XDG_STATE_HOME", "/base")
		if d := StateDir("my server/../x"); d != "/base/my_server_.._x" {
			t.Errorf("sanitize: got %q", d)
		}
	}
}

func TestServerLoadsPersistedSettingsAtStartup(t *testing.T) {
	dev := newTestFocuser() // stepSize starts at "5"
	store := NewFileStore("rig", filepath.Join(t.TempDir(), "settings.json"))
	if err := store.Save(dev.UniqueID(), map[string]string{"stepsize": "42", "reversed": "true"}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	s := New(Config{ServerName: "rig", Settings: store})
	if err := s.Register(customType, 0, dev); err != nil {
		t.Fatalf("register: %v", err)
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

	store := NewFileStore("rig", filepath.Join(t.TempDir(), "settings.json"))
	if err := store.Save(dev.UniqueID(), map[string]string{
		"stepsize": "50",     // not pinned → should apply
		"serial":   "STALE",  // pinned → must be ignored
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	s := New(Config{ServerName: "rig", Settings: store})
	if err := s.Register(customType, 0, dev); err != nil {
		t.Fatalf("register: %v", err)
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
	path := filepath.Join(t.TempDir(), "settings.json")
	store := NewFileStore("rig", path)
	s := New(Config{ServerName: "rig", Settings: store})
	if err := s.Register(customType, 0, dev); err != nil {
		t.Fatalf("register: %v", err)
	}

	if w := do(t, s, http.MethodPost, "/setup/v1/customfocuser/0/setup", "stepsize=8"); w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	got, _ := NewFileStore("rig", path).Load(dev.UniqueID())
	if _, ok := got["serial"]; ok {
		t.Errorf("pinned field was persisted: %v", got)
	}
	if got["stepsize"] != "8" {
		t.Errorf("editable field not persisted: %v", got)
	}
}

func TestSetupSavesOnApply(t *testing.T) {
	dev := newTestFocuser()
	path := filepath.Join(t.TempDir(), "settings.json")
	store := NewFileStore("rig", path)
	s := New(Config{ServerName: "rig", Settings: store})
	if err := s.Register(customType, 0, dev); err != nil {
		t.Fatalf("register: %v", err)
	}

	w := do(t, s, http.MethodPost, "/setup/v1/customfocuser/0/setup", "stepsize=99&reversed=true")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "applied and saved") {
		t.Errorf("expected saved banner:\n%s", w.Body.String())
	}

	// Persisted to disk: a fresh store reads back the applied values.
	got, err := NewFileStore("rig", path).Load(dev.UniqueID())
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got["stepsize"] != "99" || got["reversed"] != "true" {
		t.Errorf("persisted = %v, want stepsize=99 reversed=true", got)
	}
}
