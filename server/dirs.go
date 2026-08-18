package server

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Directory roles. A server has three kinds of files: admin-owned config that
// it reads, state it writes (values changed through the setup form), and logs.
// Each role resolves to a conventional location per platform, and every
// program on one machine (an orchestrator and the device binaries it launches)
// resolves the same paths from the same name.
//
//	role     Linux                    macOS                                        Windows
//	config   /etc/<name>              /Library/Application Support/<name>          %ProgramData%\<name>
//	state    /var/lib/<name>          /Library/Application Support/<name>/state    %ProgramData%\<name>\state
//	logs     /var/log/<name>          /Library/Logs/<name>                         %ProgramData%\<name>\logs
//
// Those are the system-wide locations, used by a service. An interactive run
// uses the per-user equivalents (~/.config, ~/.local/state, ~/Library, %AppData%).
//
// Resolution order for each role:
//
//  1. An explicit environment override: $ALPACA_CONFIG_DIR, $ALPACA_STATE_DIR,
//     $ALPACA_LOG_DIR.
//  2. systemd's own variables: $CONFIGURATION_DIRECTORY, $STATE_DIRECTORY,
//     $LOGS_DIRECTORY (first entry of a colon list). launchd and the Windows
//     SCM have no equivalent, so services there set the ALPACA_* variables.
//  3. The system-wide location when running as a service (see IsSystemService),
//     else the per-user location.
//  4. <os.TempDir>/<name> when no home directory is usable.

// ConfigDir resolves the directory a server reads its admin-owned config from.
func ConfigDir(serverName string) string {
	return resolveDir(serverName, "ALPACA_CONFIG_DIR", "CONFIGURATION_DIRECTORY", roleConfig)
}

// StateDir resolves the directory a server writes its state to: values changed
// through the setup form, per-device settings files, and anything else the
// program owns rather than the admin.
func StateDir(serverName string) string {
	return resolveDir(serverName, "ALPACA_STATE_DIR", "STATE_DIRECTORY", roleState)
}

// LogDir resolves the directory a server writes log files to. A service under
// systemd normally logs to the journal and needs no directory; this is for
// launchd and Windows services, and for programs that write their own files.
func LogDir(serverName string) string {
	return resolveDir(serverName, "ALPACA_LOG_DIR", "LOGS_DIRECTORY", roleLogs)
}

// SystemConfigDir, SystemStateDir, and SystemLogDir return the system-wide
// location for each role regardless of who is running, for a program that
// needs the service's paths while running interactively (an admin tool
// finding the service's config file). They ignore the environment overrides.
func SystemConfigDir(serverName string) string {
	return systemDir(sanitizeName(serverName), roleConfig)
}

// SystemStateDir is the system-wide state directory; see SystemConfigDir.
func SystemStateDir(serverName string) string {
	return systemDir(sanitizeName(serverName), roleState)
}

// SystemLogDir is the system-wide log directory; see SystemConfigDir.
func SystemLogDir(serverName string) string {
	return systemDir(sanitizeName(serverName), roleLogs)
}

type dirRole int

const (
	roleConfig dirRole = iota
	roleState
	roleLogs
)

// resolveDir applies the resolution order for one role.
func resolveDir(serverName, override, systemdVar string, role dirRole) string {
	name := sanitizeName(serverName)
	if d := os.Getenv(override); d != "" {
		return d
	}
	if d := os.Getenv(systemdVar); d != "" {
		return strings.SplitN(d, string(os.PathListSeparator), 2)[0]
	}
	if IsSystemService() {
		return systemDir(name, role)
	}
	return perUserDir(name, role)
}

// IsSystemService reports whether the process is running as a system service
// rather than an interactive user, which selects the system-wide directory
// locations. It is true when $ALPACA_SYSTEM_SERVICE is set to a true value, or
// on Linux and macOS when running as root. A host that knows better sets the
// variable either way.
func IsSystemService() bool {
	switch strings.ToLower(os.Getenv("ALPACA_SYSTEM_SERVICE")) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return runtime.GOOS != "windows" && os.Geteuid() == 0
}

// systemDir returns the system-wide location for a role.
func systemDir(name string, role dirRole) string {
	switch runtime.GOOS {
	case "darwin":
		switch role {
		case roleConfig:
			return filepath.Join("/Library/Application Support", name)
		case roleState:
			return filepath.Join("/Library/Application Support", name, "state")
		default:
			return filepath.Join("/Library/Logs", name)
		}
	case "windows":
		base := os.Getenv("ProgramData")
		if base == "" {
			base = `C:\ProgramData`
		}
		switch role {
		case roleConfig:
			return filepath.Join(base, name)
		case roleState:
			return filepath.Join(base, name, "state")
		default:
			return filepath.Join(base, name, "logs")
		}
	default: // linux and the other unixes
		switch role {
		case roleConfig:
			return filepath.Join("/etc", name)
		case roleState:
			return filepath.Join("/var/lib", name)
		default:
			return filepath.Join("/var/log", name)
		}
	}
}

// perUserDir returns the per-user location for a role.
func perUserDir(name string, role dirRole) string {
	if runtime.GOOS == "linux" {
		var envVar, sub string
		switch role {
		case roleConfig:
			envVar, sub = "XDG_CONFIG_HOME", ".config"
		case roleState:
			envVar, sub = "XDG_STATE_HOME", filepath.Join(".local", "state")
		default:
			envVar, sub = "XDG_STATE_HOME", filepath.Join(".local", "state") // XDG has no log dir; logs sit beside state
		}
		if d := os.Getenv(envVar); d != "" {
			return filepath.Join(d, name)
		}
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			return filepath.Join(home, sub, name)
		}
		return filepath.Join(os.TempDir(), name)
	}
	// darwin: ~/Library/Application Support/<name>; windows: %AppData%\<name>.
	base, err := os.UserConfigDir()
	if err != nil {
		return filepath.Join(os.TempDir(), name)
	}
	switch role {
	case roleConfig:
		return filepath.Join(base, name)
	case roleState:
		return filepath.Join(base, name, "state")
	default:
		if runtime.GOOS == "darwin" {
			if home, err := os.UserHomeDir(); err == nil && home != "" {
				return filepath.Join(home, "Library", "Logs", name)
			}
		}
		return filepath.Join(base, name, "logs")
	}
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
