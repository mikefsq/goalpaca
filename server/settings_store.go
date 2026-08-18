package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
)

// SettingsStore persists a Configurable device's settings across restarts.
// Each device has a settings key, a file path by default (see
// Server.SettingsPath), and the server loads that key at startup, applying the
// values before hardware opens, and saves it after every successful /setup
// submission.
type SettingsStore interface {
	// Load returns the stored values for key, or a nil map (and nil error) if
	// nothing has been stored for it yet. Values are what Save was given: form
	// strings from a hand-written Configurable, or typed JSON values (bool,
	// number, string) from a generated form.
	Load(key string) (map[string]any, error)
	// Save replaces the stored values for key.
	Save(key string, values map[string]any) error
}

// FileStore is a SettingsStore that keeps one JSON file per device: the key is
// the file path. Writes are atomic (temp file + rename), the file is created
// mode 0600, and its directory is created on first save. Safe for concurrent
// use.
//
// One file per device lets an orchestrator edit the same file, since there is
// no shared index and the only coordination needed is an atomic write. The
// device's UniqueID plays no part in the path. A camera bound by enumeration
// index rewrites its UniqueID once its hardware is opened, and with a path key
// it still loads and saves under one stable name.
type FileStore struct {
	mu sync.Mutex
}

// NewFileStore returns a FileStore.
func NewFileStore() *FileStore { return &FileStore{} }

// Load implements SettingsStore.
func (f *FileStore) Load(path string) (map[string]any, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, nil
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// Save implements SettingsStore, rewriting the file atomically so a crash
// mid-write cannot corrupt the existing settings.
func (f *FileStore) Save(path string, values map[string]any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	data, err := json.MarshalIndent(values, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil { // atomic replace on POSIX
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// SettingsPath sets the settings key for a registered device: with the default
// FileStore that is the file its settings are read from and written to. A host
// that owns per-device config files points each device at its own file here.
// Unset, the key defaults to <StateDir(ServerName)>/<type>-<number>.json. Call
// before Run.
func (s *Server) SettingsPath(devType DeviceType, number int, path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rd, ok := s.devices[regKey{devType, number}]
	if !ok {
		return fmt.Errorf("goalpaca: %s device %d is not registered", devType, number)
	}
	rd.settingsKey = path
	return nil
}

// settingsKey returns the settings key for a registration: the host-supplied
// path, else the default under the server's state directory.
func (s *Server) settingsKey(rd *registeredDevice) string {
	if rd.settingsKey != "" {
		return rd.settingsKey
	}
	return filepath.Join(StateDir(s.cfg.ServerName), string(rd.typ)+"-"+strconv.Itoa(rd.num)+".json")
}
