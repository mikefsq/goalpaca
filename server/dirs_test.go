package server

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// clearDirEnv unsets every variable the directory resolvers consult, so a test
// controls each level of the resolution order explicitly.
func clearDirEnv(t *testing.T) {
	t.Helper()
	for _, v := range []string{
		"ALPACA_CONFIG_DIR", "ALPACA_STATE_DIR", "ALPACA_LOG_DIR",
		"CONFIGURATION_DIRECTORY", "STATE_DIRECTORY", "LOGS_DIRECTORY",
		"XDG_CONFIG_HOME", "XDG_STATE_HOME",
	} {
		t.Setenv(v, "")
	}
	t.Setenv("ALPACA_SYSTEM_SERVICE", "false")
}

func TestDirResolutionOrder(t *testing.T) {
	cases := []struct {
		name     string
		fn       func(string) string
		override string
		systemd  string
	}{
		{"config", ConfigDir, "ALPACA_CONFIG_DIR", "CONFIGURATION_DIRECTORY"},
		{"state", StateDir, "ALPACA_STATE_DIR", "STATE_DIRECTORY"},
		{"logs", LogDir, "ALPACA_LOG_DIR", "LOGS_DIRECTORY"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clearDirEnv(t)

			// 1. The explicit override wins over everything.
			t.Setenv(c.override, "/explicit/dir")
			t.Setenv(c.systemd, "/var/lib/systemd/dir")
			if d := c.fn("rig"); d != "/explicit/dir" {
				t.Errorf("override: got %q", d)
			}

			// 2. The systemd variable next; first entry of a list.
			t.Setenv(c.override, "")
			t.Setenv(c.systemd, "/var/lib/systemd/dir"+string(os.PathListSeparator)+"/other")
			if d := c.fn("rig"); d != "/var/lib/systemd/dir" {
				t.Errorf("systemd var: got %q", d)
			}

			// 3. Per-user fallback ends in the sanitized name.
			t.Setenv(c.systemd, "")
			if d := c.fn("rig"); !strings.HasSuffix(d, "rig") && !strings.HasSuffix(d, filepath.Join("rig", "state")) && !strings.HasSuffix(d, filepath.Join("rig", "logs")) {
				t.Errorf("per-user: got %q", d)
			}
		})
	}
}

func TestDirSystemService(t *testing.T) {
	clearDirEnv(t)
	t.Setenv("ALPACA_SYSTEM_SERVICE", "true")
	cfg, st, lg := ConfigDir("rig"), StateDir("rig"), LogDir("rig")
	switch runtime.GOOS {
	case "linux":
		if cfg != "/etc/rig" || st != "/var/lib/rig" || lg != "/var/log/rig" {
			t.Errorf("linux system dirs = %q %q %q", cfg, st, lg)
		}
	case "darwin":
		if cfg != "/Library/Application Support/rig" || st != "/Library/Application Support/rig/state" || lg != "/Library/Logs/rig" {
			t.Errorf("darwin system dirs = %q %q %q", cfg, st, lg)
		}
	case "windows":
		if !strings.HasSuffix(cfg, `\rig`) || !strings.HasSuffix(st, `\rig\state`) || !strings.HasSuffix(lg, `\rig\logs`) {
			t.Errorf("windows system dirs = %q %q %q", cfg, st, lg)
		}
	}
}

func TestDirPerUser(t *testing.T) {
	clearDirEnv(t)
	if runtime.GOOS == "linux" {
		t.Setenv("XDG_CONFIG_HOME", "/home/u/.config")
		t.Setenv("XDG_STATE_HOME", "/home/u/.local/state")
		if d := ConfigDir("rig"); d != "/home/u/.config/rig" {
			t.Errorf("XDG config: got %q", d)
		}
		if d := StateDir("rig"); d != "/home/u/.local/state/rig" {
			t.Errorf("XDG state: got %q", d)
		}
		return
	}
	base, err := os.UserConfigDir()
	if err != nil {
		t.Skip("no user config dir")
	}
	if d := ConfigDir("rig"); d != filepath.Join(base, "rig") {
		t.Errorf("per-user config: got %q", d)
	}
	if d := StateDir("rig"); d != filepath.Join(base, "rig", "state") {
		t.Errorf("per-user state: got %q", d)
	}
}

func TestIsSystemServiceEnv(t *testing.T) {
	t.Setenv("ALPACA_SYSTEM_SERVICE", "yes")
	if !IsSystemService() {
		t.Error("yes should force true")
	}
	t.Setenv("ALPACA_SYSTEM_SERVICE", "off")
	if IsSystemService() {
		t.Error("off should force false")
	}
}

func TestDirSanitizesName(t *testing.T) {
	clearDirEnv(t)
	if runtime.GOOS != "linux" {
		t.Skip("path assertion is Linux-specific")
	}
	// Separators become '_' so the name is always a single, safe path
	// segment; dots are kept (harmless inside one segment).
	t.Setenv("XDG_STATE_HOME", "/base")
	if d := StateDir("my server/../x"); d != "/base/my_server_.._x" {
		t.Errorf("sanitize: got %q", d)
	}
	for _, bad := range []string{"", ".", ".."} {
		if d := StateDir(bad); d != "/base/goalpaca" {
			t.Errorf("StateDir(%q) = %q, want /base/goalpaca", bad, d)
		}
	}
}

func TestSystemDirsIgnoreEnv(t *testing.T) {
	clearDirEnv(t)
	t.Setenv("ALPACA_CONFIG_DIR", "/override")
	t.Setenv("ALPACA_SYSTEM_SERVICE", "false")
	if runtime.GOOS == "linux" {
		if d := SystemConfigDir("rig"); d != "/etc/rig" {
			t.Errorf("SystemConfigDir = %q", d)
		}
		if d := SystemStateDir("rig"); d != "/var/lib/rig" {
			t.Errorf("SystemStateDir = %q", d)
		}
	}
	if d := SystemConfigDir("rig"); d == "/override" {
		t.Error("SystemConfigDir honoured the override")
	}
}
