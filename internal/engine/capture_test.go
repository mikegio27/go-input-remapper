package engine

import (
	"testing"

	evdev "github.com/mikegio27/go-evdev"
)

// TestCaptureTee verifies the sink registry delivers events and that removal
// stops delivery. It constructs a bare Engine (no device) since emitCapture only
// touches the sink registry.
func TestCaptureTee(t *testing.T) {
	e := &Engine{sinks: map[chan<- evdev.InputEvent]struct{}{}}

	ch := make(chan evdev.InputEvent, 4)
	remove := e.AddCaptureSink(ch)

	want := keyEvent(evdev.KEY_A, keyDown)
	e.emitCapture(want)
	select {
	case got := <-ch:
		if got != want {
			t.Errorf("got %+v, want %+v", got, want)
		}
	default:
		t.Fatal("expected an event on the sink")
	}

	remove()
	e.emitCapture(keyEvent(evdev.KEY_B, keyDown))
	select {
	case got := <-ch:
		t.Errorf("expected no event after remove, got %+v", got)
	default:
	}
}

// TestCaptureNonBlocking ensures a full sink doesn't block emitCapture.
func TestCaptureNonBlocking(t *testing.T) {
	e := &Engine{sinks: map[chan<- evdev.InputEvent]struct{}{}}
	ch := make(chan evdev.InputEvent) // unbuffered, no reader
	e.AddCaptureSink(ch)
	done := make(chan struct{})
	go func() {
		e.emitCapture(keyEvent(evdev.KEY_A, keyDown)) // must not block
		close(done)
	}()
	<-done
}
