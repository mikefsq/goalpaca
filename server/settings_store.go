package server

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// SettingsStore persists a Configurable device's settings across restarts,
// keyed by the device's UniqueID. When Config.Settings is set, the server loads
// each Configurable device's stored values at startup (applying them before
// hardware opens) and saves them after every successful /setup submission.
type SettingsStore interface {
	// Load returns the stored values for uniqueID, or a nil map (and nil error)
	// if nothing has been stored for it yet.
	Load(uniqueID string) (map[string]string, error)
	// Save replaces the stored values for uniqueID.
	Save(uniqueID string, values map[string]string) error
}

// FileStore is a SettingsStore backed by a single JSON file that holds every
// device's settings keyed by UniqueID. Writes are atomic (temp file + rename)
// and the file is created mode 0600. Safe for concurrent use.
type FileStore struct {
	path string
	mu   sync.Mutex
}

// NewFileStore returns a FileStore writing to path. If path is "", it is
// resolved to <StateDir(serverName)>/settings.json — preferring $ALPACA_STATE_DIR,
// then systemd's $STATE_DIRECTORY, then the OS per-user location (see StateDir).
func NewFileStore(serverName, path string) *FileStore {
	if path == "" {
		path = filepath.Join(StateDir(serverName), "settings.json")
	}
	return &FileStore{path: path}
}

// Path reports the file the store reads and writes.
func (f *FileStore) Path() string { return f.path }

func (f *FileStore) readAll() (map[string]map[string]string, error) {
	data, err := os.ReadFile(f.path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	all := map[string]map[string]string{}
	if len(data) == 0 {
		return all, nil
	}
	if err := json.Unmarshal(data, &all); err != nil {
		return nil, err
	}
	return all, nil
}

// Load implements SettingsStore.
func (f *FileStore) Load(uniqueID string) (map[string]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	all, err := f.readAll()
	if err != nil {
		return nil, err
	}
	return all[uniqueID], nil
}

// Save implements SettingsStore, replacing uniqueID's values and rewriting the
// file atomically so a crash mid-write cannot corrupt existing settings.
func (f *FileStore) Save(uniqueID string, values map[string]string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	all, err := f.readAll()
	if err != nil {
		return err
	}
	all[uniqueID] = values
	data, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(f.path), 0o755); err != nil {
		return err
	}
	tmp := f.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, f.path); err != nil { // atomic replace on POSIX
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// StateDir resolves the directory for a server's writable state, in priority
// order:
//
//  1. $ALPACA_STATE_DIR              — explicit override (all platforms)
//  2. $STATE_DIRECTORY               — systemd StateDirectory= (first of a colon list)
//  3. OS per-user location:
//     - Linux:   $XDG_STATE_HOME/<name> else ~/.local/state/<name>
//     - macOS:   ~/Library/Application Support/<name>
//     - Windows: %AppData%\<name>
//  4. <os.TempDir>/<name>            — last resort (no usable HOME)
//
// It holds app-managed *state* (values changed through the setup form), which is
// distinct from admin config in /etc or ~/.config. launchd and the Windows SCM
// have no $STATE_DIRECTORY analog, so daemons/services on those platforms set
// $ALPACA_STATE_DIR (e.g. /Library/Application Support/... or C:\ProgramData\...);
// interactive runs everywhere fall through to the per-user location.
func StateDir(serverName string) string {
	name := sanitizeName(serverName)
	if d := os.Getenv("ALPACA_STATE_DIR"); d != "" {
		return d
	}
	if d := os.Getenv("STATE_DIRECTORY"); d != "" { // set by systemd
		return strings.SplitN(d, string(os.PathListSeparator), 2)[0]
	}
	return perUserStateDir(name)
}

func perUserStateDir(name string) string {
	if runtime.GOOS == "linux" { // XDG "state", not "config"
		if d := os.Getenv("XDG_STATE_HOME"); d != "" {
			return filepath.Join(d, name)
		}
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			return filepath.Join(home, ".local", "state", name)
		}
	} else if d, err := os.UserConfigDir(); err == nil {
		// darwin: ~/Library/Application Support/<name>; windows: %AppData%\<name>
		return filepath.Join(d, name)
	}
	return filepath.Join(os.TempDir(), name)
}

// sanitizeName keeps a server name safe to use as a path segment.
func sanitizeName(s string) string {
	if s == "" {
		return "goalpaca"
	}
	mapped := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '-', r == '_', r == '.':
			return r
		}
		return '_'
	}, s)
	if mapped == "" || mapped == "." || mapped == ".." {
		return "goalpaca"
	}
	return mapped
}
