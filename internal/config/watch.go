package config

import (
	"os"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/mikegio27/go-input-remapper/internal/paths"
)

// debounceInterval coalesces the burst of filesystem events a single save
// produces (editors and our own atomic writes do write+rename+chmod) into one
// reload signal.
const debounceInterval = 200 * time.Millisecond

// Watcher signals when the config directory changes, debounced so a flurry of
// filesystem events becomes a single notification. It watches the config
// directory and the profiles/ subdirectory (watching directories rather than
// files so renames and new profile files are caught).
type Watcher struct {
	fsw    *fsnotify.Watcher
	events chan struct{}
	quit   chan struct{}
	done   chan struct{}
}

// NewWatcher starts watching dir (and dir/profiles, if it exists) for changes.
// The caller must Close it.
func NewWatcher(dir string) (*Watcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	// The config dir must exist to watch it; create it so a first run can pick
	// up files the TUI writes later.
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fsw.Close()
		return nil, err
	}
	if err := fsw.Add(dir); err != nil {
		fsw.Close()
		return nil, err
	}
	// profiles/ may not exist yet; watch it when present. If it is created later,
	// events in the parent dir still trigger a reload, which re-reads everything.
	if profDir := paths.ProfilesDir(dir); dirExists(profDir) {
		_ = fsw.Add(profDir)
	}

	w := &Watcher{
		fsw:    fsw,
		events: make(chan struct{}, 1),
		quit:   make(chan struct{}),
		done:   make(chan struct{}),
	}
	go w.loop(dir)
	return w, nil
}

// Events returns a channel that receives one value per debounced change.
func (w *Watcher) Events() <-chan struct{} { return w.events }

// Close stops watching and releases resources.
func (w *Watcher) Close() error {
	close(w.quit)
	<-w.done
	return w.fsw.Close()
}

func (w *Watcher) loop(dir string) {
	defer close(w.done)

	var timer *time.Timer
	var fire <-chan time.Time
	reset := func() {
		if timer == nil {
			timer = time.NewTimer(debounceInterval)
		} else {
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(debounceInterval)
		}
		fire = timer.C
	}

	profDir := paths.ProfilesDir(dir)
	for {
		select {
		case ev, ok := <-w.fsw.Events:
			if !ok {
				return
			}
			// If profiles/ is created after startup, start watching it too.
			if ev.Op&fsnotify.Create != 0 && ev.Name == profDir {
				_ = w.fsw.Add(profDir)
			}
			reset()
		case _, ok := <-w.fsw.Errors:
			if !ok {
				return
			}
			// Watch errors are non-fatal here; the next event still triggers a
			// reload, and the daemon keeps its current config meanwhile.
		case <-fire:
			fire = nil
			select {
			case w.events <- struct{}{}:
			default: // a reload is already pending; coalesce
			}
		case <-w.quit:
			if timer != nil {
				timer.Stop()
			}
			return
		}
	}
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
