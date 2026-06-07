package device

import (
	"testing"

	evdev "github.com/mikegio27/go-evdev"
)

// caps is a small builder for capability snapshots in tests.
func caps(keys []evdev.EvCode, rels []evdev.EvCode, hasAbs bool, props ...evdev.InputProp) Caps {
	c := Caps{
		Keys:   map[evdev.EvCode]bool{},
		Rels:   map[evdev.EvCode]bool{},
		Props:  map[evdev.InputProp]bool{},
		HasAbs: hasAbs,
	}
	for _, k := range keys {
		c.Keys[k] = true
	}
	for _, r := range rels {
		c.Rels[r] = true
	}
	for _, p := range props {
		c.Props[p] = true
	}
	return c
}

func TestClassifyCaps(t *testing.T) {
	keyboardKeys := []evdev.EvCode{evdev.KEY_A, evdev.KEY_Z, evdev.KEY_SPACE, evdev.KEY_CAPSLOCK}

	tests := []struct {
		name string
		caps Caps
		want Kind
	}{
		{
			name: "keyboard has alpha keys + space",
			caps: caps(keyboardKeys, nil, false),
			want: KindKeyboard,
		},
		{
			name: "mouse has BTN_LEFT and REL_X",
			caps: caps([]evdev.EvCode{evdev.BTN_LEFT, evdev.BTN_RIGHT}, []evdev.EvCode{evdev.REL_X, evdev.REL_Y}, false),
			want: KindMouse,
		},
		{
			name: "gamepad via BTN_GAMEPAD wins over its buttons",
			caps: caps([]evdev.EvCode{evdev.BTN_GAMEPAD, evdev.BTN_SOUTH}, nil, true),
			want: KindGamepad,
		},
		{
			name: "joystick via BTN_JOYSTICK",
			caps: caps([]evdev.EvCode{evdev.BTN_JOYSTICK}, nil, true),
			want: KindGamepad,
		},
		{
			name: "touchpad via BTN_TOOL_FINGER beats mouse heuristic",
			caps: caps([]evdev.EvCode{evdev.BTN_LEFT, evdev.BTN_TOOL_FINGER}, []evdev.EvCode{evdev.REL_X}, true, evdev.INPUT_PROP_POINTER),
			want: KindTouchpad,
		},
		{
			name: "touchpad via INPUT_PROP_BUTTONPAD",
			caps: caps([]evdev.EvCode{evdev.BTN_LEFT}, nil, true, evdev.INPUT_PROP_BUTTONPAD),
			want: KindTouchpad,
		},
		{
			name: "mouse without REL_X is not a mouse",
			caps: caps([]evdev.EvCode{evdev.BTN_LEFT}, nil, false),
			want: KindUnknown,
		},
		{
			name: "empty device is unknown",
			caps: caps(nil, nil, false),
			want: KindUnknown,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyCaps(tc.caps); got != tc.want {
				t.Errorf("ClassifyCaps() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestKindString(t *testing.T) {
	for k, want := range map[Kind]string{
		KindKeyboard: "keyboard",
		KindMouse:    "mouse",
		KindGamepad:  "gamepad",
		KindTouchpad: "touchpad",
		KindUnknown:  "unknown",
	} {
		if got := k.String(); got != want {
			t.Errorf("Kind(%d).String() = %q, want %q", k, got, want)
		}
	}
}
