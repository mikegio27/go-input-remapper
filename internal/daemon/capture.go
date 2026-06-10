package daemon

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"sort"
	"time"

	evdev "github.com/mikegio27/go-evdev"
	"github.com/mikegio27/nereus/internal/control"
	"github.com/mikegio27/nereus/internal/device"
)

// captureSinkSize buffers a capture session's events so a brief stall doesn't
// drop keys from a human pressing them.
const captureSinkSize = 64

// handleCapture streams a device's raw key events to the client until it sends
// stop_capture or disconnects. It works on a grabbed device by teeing the
// engine's event stream; for an unbound device it opens and grabs the device for
// the session. Every streamed message has Stream=true; a final Stream=false
// response marks a clean end.
func (s *controlServer) handleCapture(conn net.Conn, r *bufio.Reader, req control.Request) {
	var p control.CaptureParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		writeResponse(conn, control.Response{ID: req.ID, OK: false, Error: "bad params: " + err.Error()})
		return
	}

	events, cleanup, err := s.captureSource(p.DevicePath)
	if err != nil {
		slog.Warn("capture: cannot open source", "path", p.DevicePath, "mode", p.Mode, "err", err)
		writeResponse(conn, control.Response{ID: req.ID, OK: false, Error: err.Error()})
		return
	}
	defer cleanup()
	slog.Info("capture: started", "path", p.DevicePath, "mode", p.Mode)

	// Tell the client we're attached so it prompts the user only now; any keypress
	// from here on is buffered by the source channel and won't be dropped.
	if raw, err := json.Marshal(control.CaptureEvent{Ready: true}); err == nil {
		writeResponse(conn, control.Response{ID: req.ID, OK: true, Stream: true, Result: raw})
	}

	// Watch the same connection for stop_capture (or disconnect) in the
	// background. It closes stop and returns, so when this function returns the
	// outer handle loop safely resumes reading.
	stop := make(chan struct{})
	go func() {
		for {
			line, err := r.ReadBytes('\n')
			if err != nil {
				close(stop)
				return
			}
			var rq control.Request
			if json.Unmarshal(line, &rq) == nil && rq.Method == control.MethodStopCapture {
				close(stop)
				return
			}
		}
	}()

	held := map[evdev.EvCode]bool{}
	last := time.Now()
	for {
		select {
		case ev := <-events:
			ce := buildCaptureEvent(ev, held, &last, p.Mode)
			raw, err := json.Marshal(ce)
			if err != nil {
				continue
			}
			writeResponse(conn, control.Response{ID: req.ID, OK: true, Stream: true, Result: raw})
		case <-stop:
			writeResponse(conn, control.Response{ID: req.ID, OK: true})
			return
		}
	}
}

// captureSource yields a channel of raw EV_KEY events for the device at path and
// a cleanup function. If an engine holds the device, it tees from the engine;
// otherwise it opens and grabs the device for the session (so the keys being
// recorded don't leak to the focused application).
func (s *controlServer) captureSource(path string) (<-chan evdev.InputEvent, func(), error) {
	if eng := s.sup.EngineFor(path); eng != nil {
		slog.Debug("capture: teeing from bound engine", "path", path)
		ch := make(chan evdev.InputEvent, captureSinkSize)
		remove := eng.AddCaptureSink(ch)
		return ch, remove, nil
	}
	slog.Debug("capture: temporary grab (device not bound)", "path", path)

	d, err := evdev.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("open %s: %w", path, err)
	}
	id, err := device.ReadIdentity(d)
	if err != nil {
		d.Close()
		return nil, nil, err
	}
	if device.IsVirtualName(id.Name, s.sup.VirtualPrefix()) {
		d.Close()
		return nil, nil, fmt.Errorf("refusing to capture from our own virtual device")
	}
	// Best-effort grab so recorded keys don't also reach applications; capture
	// still works (events just leak) if the grab fails.
	_ = d.Grab()

	ch := make(chan evdev.InputEvent, captureSinkSize)
	go func() {
		for {
			ev, err := d.ReadOne()
			if err != nil {
				return // device closed by cleanup
			}
			if ev.Type == evdev.EV_KEY {
				select {
				case ch <- ev:
				default:
				}
			}
		}
	}()
	cleanup := func() { d.Close() } // unblocks ReadOne and releases the grab
	return ch, cleanup, nil
}

// buildCaptureEvent converts a raw key event into a CaptureEvent, updating the
// held-key set and timing. For chord mode it includes the current held set; the
// duration since the previous event is included for macro timing.
func buildCaptureEvent(ev evdev.InputEvent, held map[evdev.EvCode]bool, last *time.Time, mode string) control.CaptureEvent {
	now := time.Now()
	dur := now.Sub(*last)
	*last = now

	switch ev.Value {
	case 1:
		held[ev.Code] = true
	case 0:
		delete(held, ev.Code)
	}

	ce := control.CaptureEvent{
		KeyName:    evdev.CodeName(evdev.EV_KEY, ev.Code),
		Value:      ev.Value,
		DurationMs: int(dur.Milliseconds()),
	}
	if mode == "chord" {
		ce.Chord = heldNames(held)
	}
	return ce
}

// heldNames returns the sorted names of the currently held keys.
func heldNames(held map[evdev.EvCode]bool) []string {
	names := make([]string, 0, len(held))
	for code := range held {
		names = append(names, evdev.CodeName(evdev.EV_KEY, code))
	}
	sort.Strings(names)
	return names
}
