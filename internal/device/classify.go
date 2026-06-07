package device

import (
	evdev "github.com/mikegio27/go-evdev"
)

// Kind is a coarse device classification used for display and for deciding
// whether a device is a sensible remap target.
type Kind int

const (
	KindUnknown Kind = iota
	KindKeyboard
	KindMouse
	KindGamepad
	KindTouchpad
)

// String returns a short human label for the kind.
func (k Kind) String() string {
	switch k {
	case KindKeyboard:
		return "keyboard"
	case KindMouse:
		return "mouse"
	case KindGamepad:
		return "gamepad"
	case KindTouchpad:
		return "touchpad"
	default:
		return "unknown"
	}
}

// Caps is a snapshot of the capability bits classification cares about. Pulling
// it into a plain struct keeps the classification logic a pure function that is
// trivial to table-test without real hardware.
type Caps struct {
	Keys   map[evdev.EvCode]bool // EV_KEY codes present (keys and BTN_* buttons)
	Rels   map[evdev.EvCode]bool // EV_REL codes present (relative axes)
	HasAbs bool                  // device emits EV_ABS at all
	Props  map[evdev.InputProp]bool
}

func (c Caps) hasKey(code evdev.EvCode) bool  { return c.Keys[code] }
func (c Caps) hasRel(code evdev.EvCode) bool  { return c.Rels[code] }
func (c Caps) hasProp(p evdev.InputProp) bool { return c.Props[p] }

// ClassifyCaps decides a device kind from a capability snapshot. Order matters:
// gamepads and touchpads are checked before mouse/keyboard because they share
// EV_KEY/EV_REL/EV_ABS bits with those simpler classes.
func ClassifyCaps(c Caps) Kind {
	switch {
	case c.hasKey(evdev.BTN_GAMEPAD) || c.hasKey(evdev.BTN_JOYSTICK):
		return KindGamepad
	case c.hasKey(evdev.BTN_TOOL_FINGER) || c.hasProp(evdev.INPUT_PROP_BUTTONPAD):
		// Touchpads report finger tooling or advertise themselves as a button
		// pad; both distinguish them from a plain mouse.
		return KindTouchpad
	case isKeyboard(c):
		return KindKeyboard
	case c.hasKey(evdev.BTN_LEFT) && c.hasRel(evdev.REL_X):
		return KindMouse
	default:
		return KindUnknown
	}
}

// isKeyboard mirrors evdev.Device.IsKeyboard on a capability snapshot: real
// keyboards carry the alphabetic keys plus space, which mice (BTN_* only) lack.
func isKeyboard(c Caps) bool {
	return c.hasKey(evdev.KEY_A) && c.hasKey(evdev.KEY_Z) && c.hasKey(evdev.KEY_SPACE)
}

// ReadCaps reads the capability snapshot from an open device.
func ReadCaps(d *evdev.Device) (Caps, error) {
	caps := Caps{
		Keys:  map[evdev.EvCode]bool{},
		Rels:  map[evdev.EvCode]bool{},
		Props: map[evdev.InputProp]bool{},
	}
	keys, err := d.CapableCodes(evdev.EV_KEY)
	if err != nil {
		return Caps{}, err
	}
	for _, c := range keys {
		caps.Keys[c] = true
	}
	rels, err := d.CapableCodes(evdev.EV_REL)
	if err != nil {
		return Caps{}, err
	}
	for _, c := range rels {
		caps.Rels[c] = true
	}
	types, err := d.CapableTypes()
	if err != nil {
		return Caps{}, err
	}
	for _, t := range types {
		if t == evdev.EV_ABS {
			caps.HasAbs = true
		}
	}
	props, err := d.CapableProps()
	if err != nil {
		return Caps{}, err
	}
	for _, p := range props {
		caps.Props[p] = true
	}
	return caps, nil
}

// Classify reads an open device's capabilities and returns its kind.
func Classify(d *evdev.Device) (Kind, error) {
	caps, err := ReadCaps(d)
	if err != nil {
		return KindUnknown, err
	}
	return ClassifyCaps(caps), nil
}
