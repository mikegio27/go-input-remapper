package engine

import (
	"testing"

	evdev "github.com/mikegio27/go-evdev"
	"github.com/mikegio27/nereus/internal/config"
)

func keyEv(code evdev.EvCode, value int32) evdev.InputEvent {
	return evdev.InputEvent{Type: evdev.EV_KEY, Code: code, Value: value}
}

func TestCompileRemapsErrors(t *testing.T) {
	if _, _, err := CompileRemaps([]config.Remap{{From: "KEY_NOPE", To: "KEY_A"}}); err == nil {
		t.Error("expected error for unknown 'from' key")
	}
	if _, _, err := CompileRemaps([]config.Remap{{From: "KEY_A", To: "KEY_NOPE"}}); err == nil {
		t.Error("expected error for unknown 'to' key")
	}
	_, extra, err := CompileRemaps([]config.Remap{{From: "KEY_CAPSLOCK", To: "KEY_ESC"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(extra) != 1 || extra[0] != evdev.KEY_ESC {
		t.Errorf("extra target keys = %v, want [KEY_ESC]", extra)
	}
}

func TestApply(t *testing.T) {
	rs, _, err := CompileRemaps([]config.Remap{
		{From: "KEY_CAPSLOCK", To: "KEY_ESC"},
		{From: "KEY_SCROLLLOCK", To: ""}, // suppress
	})
	if err != nil {
		t.Fatal(err)
	}

	t.Run("remap rewrites code and preserves value", func(t *testing.T) {
		for _, value := range []int32{1, 0, 2} { // down, up, repeat
			out := rs.Apply(keyEv(evdev.KEY_CAPSLOCK, value))
			if len(out) != 1 {
				t.Fatalf("value %d: got %d events, want 1", value, len(out))
			}
			if out[0].Code != evdev.KEY_ESC || out[0].Value != value {
				t.Errorf("value %d: got %v/%d, want KEY_ESC/%d", value, out[0].Code, out[0].Value, value)
			}
		}
	})

	t.Run("suppressed key yields nothing", func(t *testing.T) {
		if out := rs.Apply(keyEv(evdev.KEY_SCROLLLOCK, 1)); out != nil {
			t.Errorf("expected nil, got %v", out)
		}
	})

	t.Run("unmapped key passes through", func(t *testing.T) {
		out := rs.Apply(keyEv(evdev.KEY_A, 1))
		if len(out) != 1 || out[0].Code != evdev.KEY_A {
			t.Errorf("expected passthrough of KEY_A, got %v", out)
		}
	})

	t.Run("non-key event passes through", func(t *testing.T) {
		rel := evdev.InputEvent{Type: evdev.EV_REL, Code: evdev.REL_X, Value: 5}
		out := rs.Apply(rel)
		if len(out) != 1 || out[0] != rel {
			t.Errorf("expected passthrough of REL_X, got %v", out)
		}
	})
}
