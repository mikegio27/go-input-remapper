// Package paths resolves the on-disk and runtime locations the daemon, TUI, and
// CLI share: the config directory (source of truth), the control socket, and the
// log file. Resolution honors an explicit override first, then XDG, then a
// system-wide fallback, so the same binary works as a user tool or a system
// service.
package paths

import (
	"os"
	"path/filepath"
)

// appName is the directory/socket basename used across all locations.
const appName = "go-input-remapper"

// ConfigDir returns the directory holding config.toml and profiles/. Resolution
// order: the explicit override (e.g. a --config-dir flag), then
// $XDG_CONFIG_HOME/go-input-remapper, then ~/.config/go-input-remapper, then the
// system-wide /etc/go-input-remapper. The directory is not created here.
func ConfigDir(override string) string {
	if override != "" {
		return override
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, appName)
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".config", appName)
	}
	return filepath.Join("/etc", appName)
}

// ProfilesDir returns the profiles subdirectory of the given config directory.
func ProfilesDir(configDir string) string {
	return filepath.Join(configDir, "profiles")
}

// ConfigFile returns the path to the global config.toml within configDir.
func ConfigFile(configDir string) string {
	return filepath.Join(configDir, "config.toml")
}

// SocketPath returns the control socket path. Resolution order: the explicit
// override (e.g. a --socket flag), then $XDG_RUNTIME_DIR/go-input-remapper.sock
// for a per-user daemon, then the system-wide /run/go-input-remapper.sock.
func SocketPath(override string) string {
	if override != "" {
		return override
	}
	if run := os.Getenv("XDG_RUNTIME_DIR"); run != "" {
		return filepath.Join(run, appName+".sock")
	}
	return filepath.Join("/run", appName+".sock")
}
