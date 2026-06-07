package daemon

import (
	"reflect"
	"testing"
	"time"

	evdev "github.com/mikegio27/go-evdev"
)

func TestBuildCaptureEventChord(t *testing.T) {
	held := map[evdev.EvCode]bool{}
	last := time.Now()

	// Press Ctrl, then J: chord should report both held, sorted by name.
	ce := buildCaptureEvent(evdev.InputEvent{Type: evdev.EV_KEY, Code: evdev.KEY_LEFTCTRL, Value: 1}, held, &last, "chord")
	if ce.KeyName != "KEY_LEFTCTRL" || ce.Value != 1 {
		t.Errorf("unexpected event: %+v", ce)
	}
	ce = buildCaptureEvent(evdev.InputEvent{Type: evdev.EV_KEY, Code: evdev.KEY_J, Value: 1}, held, &last, "chord")
	want := []string{"KEY_J", "KEY_LEFTCTRL"}
	if !reflect.DeepEqual(ce.Chord, want) {
		t.Errorf("chord = %v, want %v", ce.Chord, want)
	}

	// Releasing Ctrl removes it from the held set.
	ce = buildCaptureEvent(evdev.InputEvent{Type: evdev.EV_KEY, Code: evdev.KEY_LEFTCTRL, Value: 0}, held, &last, "chord")
	if !reflect.DeepEqual(ce.Chord, []string{"KEY_J"}) {
		t.Errorf("after release, chord = %v, want [KEY_J]", ce.Chord)
	}
}

func TestBuildCaptureEventKeyModeHasNoChord(t *testing.T) {
	held := map[evdev.EvCode]bool{}
	last := time.Now()
	ce := buildCaptureEvent(evdev.InputEvent{Type: evdev.EV_KEY, Code: evdev.KEY_A, Value: 1}, held, &last, "key")
	if ce.Chord != nil {
		t.Errorf("key mode should not populate Chord, got %v", ce.Chord)
	}
}
