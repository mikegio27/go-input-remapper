package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mikegio27/go-input-remapper/internal/config"
	"github.com/mikegio27/go-input-remapper/internal/control"
	"github.com/mikegio27/go-input-remapper/internal/device"
)

// Messages flowing back into the Bubble Tea update loop.
type (
	devicesMsg struct {
		devices    []control.DeviceInfo
		fromDaemon bool
	}
	statusMsg struct {
		status control.StatusResult
		up     bool
	}
	configMsg struct {
		cfg   *config.Config
		err   error
		quiet bool // background refresh: don't flash an error if it fails
	}
	actionMsg struct { // result of a mutating action (reload/set_profile/save)
		text string
		err  error
	}
)

// The simple control calls use a fresh connection each time so concurrent
// Bubble Tea commands never share one (control.Client is single-goroutine). A
// dial failure means the daemon is down, which the UI shows rather than erroring.

func fetchDevices(sock string) tea.Cmd {
	return func() tea.Msg {
		if c, err := control.Dial(sock); err == nil {
			defer c.Close()
			var res control.ListDevicesResult
			if err := c.Call(control.MethodListDevices, nil, &res); err == nil {
				return devicesMsg{devices: res.Devices, fromDaemon: true}
			}
		}
		// Fallback: enumerate directly (no bound/daemon info).
		return devicesMsg{devices: enumerateDirect(), fromDaemon: false}
	}
}

func fetchStatus(sock string) tea.Cmd {
	return func() tea.Msg {
		c, err := control.Dial(sock)
		if err != nil {
			return statusMsg{up: false}
		}
		defer c.Close()
		var res control.StatusResult
		if err := c.Call(control.MethodStatus, nil, &res); err != nil {
			return statusMsg{up: false}
		}
		return statusMsg{status: res, up: true}
	}
}

func loadConfig(dir string) tea.Cmd {
	return func() tea.Msg {
		cfg, err := config.Load(dir)
		return configMsg{cfg: cfg, err: err}
	}
}

// loadConfigQuiet is loadConfig for the background poll: it refreshes config from
// disk (so the Profiles list self-heals after a transient read failure) without
// flashing an error on every tick if the config is momentarily unreadable.
func loadConfigQuiet(dir string) tea.Cmd {
	return func() tea.Msg {
		cfg, err := config.Load(dir)
		return configMsg{cfg: cfg, err: err, quiet: true}
	}
}

func setProfile(sock, dir, name string) tea.Cmd {
	return func() tea.Msg {
		// Prefer the daemon (persists + applies atomically). Fall back to writing
		// config.toml directly so it still takes effect on next daemon start.
		if c, err := control.Dial(sock); err == nil {
			defer c.Close()
			var res control.StatusResult
			if err := c.Call(control.MethodSetProfile, control.SetProfileParams{Profile: name}, &res); err == nil {
				return actionMsg{text: "switched to profile " + name}
			} else {
				return actionMsg{err: err}
			}
		}
		cfg, err := config.Load(dir)
		if err != nil {
			return actionMsg{err: err}
		}
		cfg.ActiveProfile = name
		if err := config.SaveGlobal(dir, cfg); err != nil {
			return actionMsg{err: err}
		}
		return actionMsg{text: "set profile " + name + " (daemon down; applies on next start)"}
	}
}

// createProfile writes a new empty profile file. If activate is set (e.g. it's
// the first profile), it also makes it the active profile so the user can start
// editing immediately.
func createProfile(sock, dir, name string, activate bool) tea.Cmd {
	return func() tea.Msg {
		cfg, err := config.Load(dir)
		if err != nil {
			return actionMsg{err: err}
		}
		if _, exists := cfg.Profiles[name]; exists {
			return actionMsg{err: errFromStrings([]string{"profile " + name + " already exists"})}
		}
		if err := config.SaveProfile(dir, name, &config.Profile{Name: name}); err != nil {
			return actionMsg{err: err}
		}
		if activate {
			cfg.ActiveProfile = name
			if err := config.SaveGlobal(dir, cfg); err != nil {
				return actionMsg{err: err}
			}
			// Best-effort: tell the daemon to pick it up.
			if c, derr := control.Dial(sock); derr == nil {
				c.Call(control.MethodReload, nil, nil)
				c.Close()
			}
			return actionMsg{text: "created and activated profile " + name}
		}
		return actionMsg{text: "created profile " + name}
	}
}

// deleteProfile removes a profile file and, if it was active, clears the active
// profile (the daemon then runs idle until another is activated).
func deleteProfile(sock, dir, name string) tea.Cmd {
	return func() tea.Msg {
		cfg, err := config.Load(dir)
		if err != nil {
			return actionMsg{err: err}
		}
		if err := config.DeleteProfile(dir, name); err != nil {
			return actionMsg{err: err}
		}
		if cfg.ActiveProfile == name {
			cfg.ActiveProfile = ""
			if err := config.SaveGlobal(dir, cfg); err != nil {
				return actionMsg{err: err}
			}
			if c, derr := control.Dial(sock); derr == nil {
				c.Call(control.MethodReload, nil, nil)
				c.Close()
			}
		}
		return actionMsg{text: "deleted profile " + name}
	}
}

func reloadDaemon(sock string) tea.Cmd {
	return func() tea.Msg {
		c, err := control.Dial(sock)
		if err != nil {
			return actionMsg{text: "saved (daemon down; will apply on next start)"}
		}
		defer c.Close()
		var res control.ReloadResult
		if err := c.Call(control.MethodReload, nil, &res); err != nil {
			return actionMsg{err: err}
		}
		if !res.OK {
			return actionMsg{err: errFromStrings(res.Errors)}
		}
		return actionMsg{text: "saved and reloaded"}
	}
}

// enumerateDirect mirrors the CLI fallback: inspect devices without the daemon.
func enumerateDirect() []control.DeviceInfo {
	infos, err := device.InspectAll(device.DefaultVirtualPrefix)
	if err != nil {
		return nil
	}
	recs := device.Recommend(infos)
	out := make([]control.DeviceInfo, 0, len(recs))
	for _, r := range recs {
		out = append(out, control.DeviceInfo{
			Path:        r.Info.Identity.Path,
			Name:        r.Info.Identity.Name,
			Kind:        r.Info.Kind.String(),
			Vendor:      r.Info.Identity.Vendor,
			Product:     r.Info.Identity.Product,
			Recommended: r.Remappable,
			Primary:     r.Primary,
			IsVirtual:   r.Info.IsVirtual,
			Reasons:     r.Reasons,
		})
	}
	return out
}
