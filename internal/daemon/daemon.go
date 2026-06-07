// Package daemon is the persistent worker: it loads the config, resolves device
// matchers to real devices, and runs one engine per bound device. It hot-reloads
// the config on change and binds/unbinds devices as they are hotplugged.
package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	evdev "github.com/mikegio27/go-evdev"
	"github.com/mikegio27/go-input-remapper/internal/config"
)

// Options configures a daemon run.
type Options struct {
	ConfigDir  string
	SocketPath string
}

// Run loads the config, builds engines for the active profile, and serves until
// ctx is cancelled. It reacts to two event sources: config-file changes (reload)
// and device hotplug (bind/unbind). It returns an error only for fatal startup
// problems (a bad initial config); afterwards a bad reload is logged and the
// last good config is kept running.
func Run(ctx context.Context, opts Options) error {
	cfg, err := config.Load(opts.ConfigDir)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if problems := config.Validate(cfg); len(problems) > 0 {
		return fmt.Errorf("invalid config: %w", errors.Join(problems...))
	}

	sup := NewSupervisor()

	// Create the hotplug watcher BEFORE the initial Apply so a device appearing
	// during startup is not missed (its event queues until the loop reads it).
	hw, err := evdev.NewWatcher()
	if err != nil {
		slog.Warn("hotplug watcher unavailable; running without hotplug support", "err", err)
	}

	sup.Apply(cfg, cfg.ActiveProfileOrNil())

	cw, err := config.NewWatcher(opts.ConfigDir)
	if err != nil {
		slog.Warn("config watcher unavailable; running without hot-reload", "err", err)
	}

	cs, err := newControlServer(opts.SocketPath, sup, opts.ConfigDir)
	if err != nil {
		slog.Warn("control socket unavailable; running without TUI/CLI control", "err", err)
	} else {
		go cs.serve()
		slog.Info("control socket listening", "path", opts.SocketPath)
	}

	slog.Info("daemon running")
	defer sup.Shutdown()
	if cs != nil {
		defer cs.close()
	}
	if cw != nil {
		defer cw.Close()
	}
	if hw != nil {
		defer hw.Close()
	}

	// Nil channels block forever in select, so a missing watcher simply drops out
	// of the loop rather than needing special-casing.
	var reloadCh <-chan struct{}
	if cw != nil {
		reloadCh = cw.Events()
	}
	var hotplugCh <-chan evdev.DeviceEvent
	var hotplugErrCh <-chan error
	if hw != nil {
		hotplugCh = hw.Events()
		hotplugErrCh = hw.Errors()
	}

	for {
		select {
		case <-ctx.Done():
			slog.Info("shutting down")
			return nil
		case <-reloadCh:
			reload(opts.ConfigDir, sup)
		case ev := <-hotplugCh:
			sup.OnHotplug(ev)
		case err := <-hotplugErrCh:
			if err != nil {
				slog.Error("hotplug watch error", "err", err)
			}
		}
	}
}

// reload re-reads the config and applies it, logging the outcome. A read or
// validation failure leaves the currently running engines untouched, so a typo
// in the config never takes down a working remapper. A change the daemon caused
// itself (set_profile, or a just-applied control reload) is ignored to avoid a
// redundant second apply that would needlessly rebuild every engine.
func reload(dir string, sup *Supervisor) {
	if sup.ReloadSuppressed() {
		slog.Debug("ignoring self-induced config change")
		return
	}
	if problems := applyFromDisk(dir, sup); len(problems) > 0 {
		slog.Error("reload rejected; keeping current config", "problems", errors.Join(problems...))
		return
	}
	slog.Info("config changed; reapplied")
}

// applyFromDisk loads and validates the config, and on success applies it. It
// returns any load/validation problems without applying (the caller keeps the
// running config). Shared by the file-watch reload and the control-socket reload.
func applyFromDisk(dir string, sup *Supervisor) []error {
	cfg, err := config.Load(dir)
	if err != nil {
		return []error{err}
	}
	if problems := config.Validate(cfg); len(problems) > 0 {
		return problems
	}
	sup.Apply(cfg, cfg.ActiveProfileOrNil())
	return nil
}
