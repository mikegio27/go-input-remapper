package daemon

import (
	"errors"
	"io/fs"
	"log/slog"
	"sync"
	"time"

	evdev "github.com/mikegio27/go-evdev"
	"github.com/mikegio27/go-input-remapper/internal/config"
	"github.com/mikegio27/go-input-remapper/internal/device"
	"github.com/mikegio27/go-input-remapper/internal/engine"
)

// addRetries / addBackoff bound the retry when a freshly hotplugged device isn't
// yet readable because udev is still applying access rules.
const (
	addRetries = 4
	addBackoff = 40 * time.Millisecond
)

// reloadSuppressWindow is how long after an Apply the file watcher should ignore
// config changes: when the daemon writes config itself (e.g. set_profile) or a
// control reload applies it, the watcher would otherwise fire a redundant second
// Apply that needlessly tears down and rebuilds every engine. Comfortably longer
// than the watcher's debounce.
const reloadSuppressWindow = 750 * time.Millisecond

// shutdownGrace bounds how long Shutdown waits for engine run loops to exit. A
// loop blocked in a kernel read isn't reliably interrupted by closing the fd, so
// past this we proceed (grabs are already released and virtual devices destroyed
// in teardown) rather than hang process exit.
const shutdownGrace = 2 * time.Second

// Supervisor owns the live set of engines, one per bound source device (keyed by
// device node path). It applies a config/profile by tearing down and rebuilding
// engines, and reacts to hotplug add/remove. Its methods are safe for concurrent
// use so the control plane (M6) can query and mutate it alongside the daemon
// loop.
type Supervisor struct {
	mu            sync.Mutex
	cfg           *config.Config
	profile       *config.Profile
	engines       map[string]*engine.Engine
	wg            sync.WaitGroup
	suppressUntil time.Time // ignore file-watch reloads until this time (self-write echo)
}

// NewSupervisor returns an empty supervisor; call Apply to populate it.
func NewSupervisor() *Supervisor {
	return &Supervisor{engines: map[string]*engine.Engine{}}
}

// Apply makes profile the active set of bindings: it tears down every running
// engine and rebuilds from the devices currently present. A nil profile leaves
// the supervisor idle (all engines torn down). Tearing down first releases each
// exclusive grab before any device is re-grabbed, avoiding EBUSY on rebind.
func (s *Supervisor) Apply(cfg *config.Config, profile *config.Profile) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.cfg = cfg
	s.profile = profile
	// This Apply (and the config write that usually precedes it) will make the
	// file watcher fire; suppress that echo so it doesn't re-tear-down everything.
	s.suppressUntil = time.Now().Add(reloadSuppressWindow)
	s.teardownAllLocked()

	if profile == nil {
		return
	}
	paths, err := evdev.ListDevicePaths()
	if err != nil {
		slog.Error("enumerate devices", "err", err)
		return
	}
	for _, path := range paths {
		s.tryBindLocked(path, 1)
	}
	slog.Info("profile applied", "profile", profile.Name, "engines", len(s.engines))
}

// ReloadSuppressed reports whether a file-watch reload should be ignored because
// the daemon just applied a config itself (so the change on disk is its own echo).
func (s *Supervisor) ReloadSuppressed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return time.Now().Before(s.suppressUntil)
}

// OnHotplug binds a newly appeared device or tears down a removed one.
func (s *Supervisor) OnHotplug(ev evdev.DeviceEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch ev.Action {
	case evdev.DeviceAdded:
		if s.profile == nil {
			return
		}
		// Retry: udev may not have applied read permission the instant the node
		// appears. Our own virtual devices also appear here and are skipped by
		// the virtual-name guard inside tryBindLocked.
		s.tryBindLocked(ev.Path, addRetries)
	case evdev.DeviceRemoved:
		if e, ok := s.engines[ev.Path]; ok {
			e.Close()
			delete(s.engines, ev.Path)
			slog.Info("device removed", "path", ev.Path, "name", e.Name())
		}
	}
}

// Shutdown tears down all engines and waits (briefly) for their run goroutines to
// exit. Teardown already ungrabs each device and destroys its virtual device, so
// if a run loop is still blocked in a kernel read that closing the fd didn't
// interrupt, we proceed after shutdownGrace instead of hanging process exit (which
// would otherwise let systemd escalate to SIGKILL).
func (s *Supervisor) Shutdown() {
	s.mu.Lock()
	s.teardownAllLocked()
	s.mu.Unlock()

	done := make(chan struct{})
	go func() { s.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(shutdownGrace):
		slog.Warn("shutdown: engine read loop(s) did not exit in time; exiting anyway")
	}
}

// BoundDevice describes one running engine, for status reporting.
type BoundDevice struct {
	Path string
	Name string
}

// EngineFor returns the engine bound to the given source device path, or nil if
// none. Used by capture to tee a grabbed device's events.
func (s *Supervisor) EngineFor(path string) *engine.Engine {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.engines[path]
}

// VirtualPrefix returns the configured virtual-device name prefix, or empty if
// no config has been applied yet.
func (s *Supervisor) VirtualPrefix() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cfg == nil {
		return ""
	}
	return s.cfg.VirtualPrefix
}

// Snapshot returns the active profile name and the currently bound devices.
func (s *Supervisor) Snapshot() (profile string, bound []BoundDevice) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.profile != nil {
		profile = s.profile.Name
	}
	for path, e := range s.engines {
		bound = append(bound, BoundDevice{Path: path, Name: e.Name()})
	}
	return profile, bound
}

// teardownAllLocked closes every engine. Callers must hold s.mu. It does not wait
// for run goroutines (Shutdown does); for a rebuild that's unnecessary because
// Close closes each source fd, releasing its grab synchronously.
func (s *Supervisor) teardownAllLocked() {
	for path, e := range s.engines {
		if err := e.Close(); err != nil {
			slog.Warn("engine close error", "path", path, "err", err)
		}
		delete(s.engines, path)
	}
}

// tryBindLocked attempts to bind the device at path to the first matching binding
// in the active profile, building and starting an engine on success. It is a
// no-op if the path is already bound, is one of our virtual devices, doesn't
// match any binding, or can't be opened/grabbed. Callers must hold s.mu.
func (s *Supervisor) tryBindLocked(path string, attempts int) {
	if _, exists := s.engines[path]; exists {
		return
	}
	d, id, ok := s.openCandidate(path, attempts)
	if !ok {
		return
	}
	binding, matched := matchBinding(s.profile, id)
	if !matched {
		d.Close()
		return
	}
	name := s.cfg.VirtualPrefix + " " + id.Name
	e, err := engine.New(d, binding, name)
	if err != nil {
		slog.Error("build engine", "path", path, "device", id.Name, "err", err)
		d.Close()
		return
	}
	s.engines[path] = e
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		if err := e.Run(); err != nil {
			slog.Error("engine stopped", "path", path, "name", e.Name(), "err", err)
		}
	}()
	slog.Info("bound device", "path", path, "device", id.Name)
}

// openCandidate opens and identifies a device, retrying on permission errors (up
// to attempts times) to ride out udev settling. It returns ok=false — closing
// any opened handle — for our own virtual devices and for devices it can't read.
func (s *Supervisor) openCandidate(path string, attempts int) (*evdev.Device, device.Identity, bool) {
	var lastErr error
	for i := 0; i < attempts; i++ {
		if i > 0 {
			time.Sleep(addBackoff)
		}
		d, err := evdev.Open(path)
		if err != nil {
			lastErr = err
			if errors.Is(err, fs.ErrPermission) {
				continue // udev may still be applying rules
			}
			break
		}
		id, err := device.ReadIdentity(d)
		if err != nil {
			d.Close()
			lastErr = err
			continue
		}
		if device.IsVirtualName(id.Name, s.cfg.VirtualPrefix) {
			d.Close() // never remap our own output (would loop)
			return nil, device.Identity{}, false
		}
		return d, id, true
	}
	if lastErr != nil && !errors.Is(lastErr, fs.ErrPermission) {
		slog.Warn("open device", "path", path, "err", lastErr)
	}
	return nil, device.Identity{}, false
}

// matchBinding returns the first binding whose matcher accepts id.
func matchBinding(profile *config.Profile, id device.Identity) (config.DeviceBinding, bool) {
	for _, b := range profile.Devices {
		if b.Match.Matches(id) {
			return b, true
		}
	}
	return config.DeviceBinding{}, false
}
