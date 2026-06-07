package engine

import (
	"errors"
	"io"
	"os"
	"sync"
	"sync/atomic"

	evdev "github.com/mikegio27/go-evdev"
	"github.com/mikegio27/go-input-remapper/internal/config"
)

// Engine owns one source device and the virtual device its transformed events
// are injected through. Construct with New, drive with Run (blocks), and release
// with Close.
type Engine struct {
	src  *evdev.Device
	vdev *evdev.VirtualDevice
	name string

	proc  *processor
	sched *scheduler
	outMu sync.Mutex // serializes vdev writes between the run loop and macros

	captureMu sync.Mutex
	sinks     map[chan<- evdev.InputEvent]struct{} // capture subscribers (learn-a-key)

	repeatMu sync.Mutex
	repeats  map[*compiledMacro]chan struct{} // active repeating macros → their stop channel

	closed    atomic.Bool // set by Close so an orphaned read loop exits without writing to a destroyed vdev
	closeOnce sync.Once
	closeErr  error
}

// New builds an engine for an already-opened source device and a binding. It
// compiles the binding's remaps and macros, creates a virtual device mirroring
// the source's capabilities plus every key the remaps/macros emit, then grabs
// the source exclusively. The engine takes ownership of src and closes it on
// Close. A bad binding (unknown key name, unsupported macro text) is rejected
// here, before the grab.
//
// virtualName must start with the configured virtual prefix so the daemon's
// feedback-loop guard recognizes and skips it during device resolution.
func New(src *evdev.Device, binding config.DeviceBinding, virtualName string) (*Engine, error) {
	rules, remapKeys, err := CompileRemaps(binding.Remaps)
	if err != nil {
		return nil, err
	}
	macros, macroKeys, err := CompileMacros(binding.Macros)
	if err != nil {
		return nil, err
	}

	caps, err := evdev.CapabilitiesOf(src)
	if err != nil {
		return nil, err
	}
	caps.Keys = append(caps.Keys, remapKeys...)
	caps.Keys = append(caps.Keys, macroKeys...)

	id, err := src.ID()
	if err != nil {
		return nil, err
	}
	vdev, err := evdev.CreateVirtualDevice(virtualName, id, caps)
	if err != nil {
		return nil, err
	}
	if err := src.Grab(); err != nil {
		vdev.Close()
		return nil, err
	}

	e := &Engine{
		src:     src,
		vdev:    vdev,
		name:    virtualName,
		proc:    newProcessor(rules, macros),
		sinks:   map[chan<- evdev.InputEvent]struct{}{},
		repeats: map[*compiledMacro]chan struct{}{},
	}
	e.sched = newScheduler(vdev, &e.outMu)
	return e, nil
}

// Run reads, transforms, and re-emits events until the source returns io.EOF
// (returns nil) or another error. It blocks; run it in a goroutine. Close
// unblocks it by closing the source.
func (e *Engine) Run() error {
	for {
		ev, err := e.src.ReadOne()
		if err != nil {
			// EOF = device unplugged; ErrClosed / our closed flag = we tore the
			// engine down (Close interrupts the blocked read). All are clean exits.
			if errors.Is(err, io.EOF) || errors.Is(err, os.ErrClosed) || e.closed.Load() {
				return nil
			}
			return err
		}
		// If we were torn down while blocked in the read above, exit quietly rather
		// than transform and write to an already-destroyed virtual device.
		if e.closed.Load() {
			return nil
		}
		// Forward frame markers and non-key events verbatim; only EV_KEY drives
		// remaps and macro triggers.
		if ev.Type != evdev.EV_KEY {
			if err := e.write(ev); err != nil {
				return err
			}
			continue
		}
		// Tee the raw key event to any capture subscribers (learn-a-key) before
		// transforming, so the TUI records what was physically pressed. While a
		// capture is active, suppress the key from passthrough: otherwise the key
		// being learned would also reach the system (e.g. typed into the TUI's own
		// input field, producing a doubled value) and fire remaps/macros.
		if e.emitCapture(ev) {
			continue
		}
		out, macro := e.proc.process(ev)
		if err := e.writeFrame(out); err != nil {
			return err
		}
		if macro != nil {
			e.startMacro(macro)
		}
		// A "hold"-mode repeat stops as soon as its trigger chord is released.
		e.cancelReleasedRepeats()
	}
}

// startMacro runs a fired macro according to its repeat mode: one-shot for the
// default, or a managed repeating goroutine for hold/toggle/count. Re-firing a
// "toggle" macro that is already running stops it; re-firing a hold/count macro
// that is already running is ignored.
func (e *Engine) startMacro(m *compiledMacro) {
	if m.repeatMode == config.RepeatModeNone {
		e.sched.fire(m)
		return
	}
	e.repeatMu.Lock()
	defer e.repeatMu.Unlock()
	if stop, running := e.repeats[m]; running {
		if m.repeatMode == config.RepeatModeToggle {
			close(stop)
			delete(e.repeats, m)
		}
		return // hold/count already running: ignore the re-trigger
	}
	stop := make(chan struct{})
	e.repeats[m] = stop
	maxCount := 0
	if m.repeatMode == config.RepeatModeCount {
		maxCount = m.repeatCount
	}
	// Self-clean when the goroutine ends on its own (e.g. count exhausted), but
	// only if this exact session is still the registered one.
	e.sched.fireRepeat(m, m.repeatInterval, maxCount, stop, func() {
		e.repeatMu.Lock()
		if e.repeats[m] == stop {
			delete(e.repeats, m)
		}
		e.repeatMu.Unlock()
	})
}

// cancelReleasedRepeats stops every "hold"-mode repeat whose trigger chord is no
// longer fully held.
func (e *Engine) cancelReleasedRepeats() {
	e.repeatMu.Lock()
	for m, stop := range e.repeats {
		if m.repeatMode == config.RepeatModeHold && !e.proc.triggerHeld(m) {
			close(stop)
			delete(e.repeats, m)
		}
	}
	e.repeatMu.Unlock()
}

// write emits a single event under the output lock (a frame of one).
func (e *Engine) write(ev evdev.InputEvent) error {
	e.outMu.Lock()
	defer e.outMu.Unlock()
	return e.vdev.Write(ev)
}

// writeFrame emits a batch of events atomically with respect to macro injection.
// An empty batch (a suppressed key) writes nothing.
func (e *Engine) writeFrame(evs []evdev.InputEvent) error {
	if len(evs) == 0 {
		return nil
	}
	e.outMu.Lock()
	defer e.outMu.Unlock()
	for _, ev := range evs {
		if err := e.vdev.Write(ev); err != nil {
			return err
		}
	}
	return nil
}

// Name returns the engine's virtual device name.
func (e *Engine) Name() string { return e.name }

// SourcePath returns the source device node the engine is bound to.
func (e *Engine) SourcePath() string { return e.src.Path() }

// AddCaptureSink registers a channel to receive the engine's raw EV_KEY events,
// for learn-a-key capture even while the device is exclusively grabbed. It
// returns a function that unregisters the sink. Sends are non-blocking, so a
// slow consumer drops events rather than stalling the run loop; size the channel
// to taste.
func (e *Engine) AddCaptureSink(ch chan<- evdev.InputEvent) (remove func()) {
	e.captureMu.Lock()
	e.sinks[ch] = struct{}{}
	e.captureMu.Unlock()
	return func() {
		e.captureMu.Lock()
		delete(e.sinks, ch)
		e.captureMu.Unlock()
	}
}

// emitCapture forwards an event to all capture sinks without blocking, and
// reports whether any capture is active — the run loop uses that to suppress the
// event from passthrough so a key being learned doesn't also act on the system.
func (e *Engine) emitCapture(ev evdev.InputEvent) (capturing bool) {
	e.captureMu.Lock()
	defer e.captureMu.Unlock()
	for ch := range e.sinks {
		select {
		case ch <- ev:
		default: // drop for a slow consumer
		}
	}
	return len(e.sinks) > 0
}

// Close releases the grab, destroys the virtual device, and closes the source.
// Closing the source unblocks a running Run. It waits for in-flight macros to
// finish first so they don't write to a destroyed device. Safe to call repeatedly.
func (e *Engine) Close() error {
	e.closeOnce.Do(func() {
		e.closed.Store(true) // make any orphaned read loop exit instead of writing to the dead vdev
		// Release the exclusive grab explicitly first: a ReadOne blocked in the
		// kernel keeps the file description alive, so closing the fd alone does not
		// free the grab promptly — and a re-apply that re-grabs the same device
		// would hit EBUSY. Ungrab is a separate ioctl and takes effect immediately.
		_ = e.src.Ungrab()
		// Then close the source (best-effort unblock of Run), cancel any repeating
		// macros so they don't block on a long interval, let running macros drain,
		// and destroy the virtual device.
		serr := e.src.Close()
		e.repeatMu.Lock()
		for m, stop := range e.repeats {
			close(stop)
			delete(e.repeats, m)
		}
		e.repeatMu.Unlock()
		e.sched.wait()
		verr := e.vdev.Close()
		e.closeErr = errors.Join(serr, verr)
	})
	return e.closeErr
}
