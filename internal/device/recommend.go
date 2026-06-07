package device

import (
	"strings"

	evdev "github.com/mikegio27/go-evdev"
)

// Info is the full inspection result for one device: its identity, classified
// kind, and whether it is one of our own virtual output devices.
type Info struct {
	Identity  Identity
	Kind      Kind
	Caps      Caps
	IsVirtual bool
}

// Inspect reads identity and capabilities from an open device and flags whether
// it is one of our own virtual devices (its name starts with virtualPrefix).
// Recognizing our virtual devices is what prevents a remap feedback loop: the
// daemon must never grab and re-remap the device it just created.
func Inspect(d *evdev.Device, virtualPrefix string) (Info, error) {
	id, err := ReadIdentity(d)
	if err != nil {
		return Info{}, err
	}
	caps, err := ReadCaps(d)
	if err != nil {
		return Info{}, err
	}
	return Info{
		Identity:  id,
		Kind:      ClassifyCaps(caps),
		Caps:      caps,
		IsVirtual: IsVirtualName(id.Name, virtualPrefix),
	}, nil
}

// IsVirtualName reports whether a device name marks it as one of our virtual
// output devices. An empty prefix matches nothing (so the guard can be disabled
// only deliberately).
func IsVirtualName(name, virtualPrefix string) bool {
	return virtualPrefix != "" && strings.HasPrefix(name, virtualPrefix)
}

// Recommendation is advice about whether and how a device should be remapped.
type Recommendation struct {
	Info       Info
	Remappable bool     // safe and sensible to bind a remapper to
	Reasons    []string // why it is (or isn't) a good target, and caveats
}

// Recommend turns inspection results into per-device advice. It steers users
// toward keyboards/mice/gamepads, warns hard against remapping our own virtual
// devices (feedback loop), and flags the EV_ABS limitation (gamepad analog
// sticks and touch surfaces can't be mirrored by go-evdev yet).
func Recommend(infos []Info) []Recommendation {
	out := make([]Recommendation, 0, len(infos))
	for _, in := range infos {
		out = append(out, recommendOne(in))
	}
	return out
}

func recommendOne(in Info) Recommendation {
	r := Recommendation{Info: in}

	if in.IsVirtual {
		r.Remappable = false
		r.Reasons = append(r.Reasons,
			"this is a go-input-remapper virtual device — remapping it would create a feedback loop")
		return r
	}

	switch in.Kind {
	case KindKeyboard:
		r.Remappable = true
		r.Reasons = append(r.Reasons, "keyboard: keys and chords remap cleanly, macros fully supported")
	case KindMouse:
		r.Remappable = true
		r.Reasons = append(r.Reasons, "mouse: buttons and wheel remap; relative motion passes through")
	case KindGamepad:
		r.Remappable = true
		r.Reasons = append(r.Reasons, "gamepad: buttons remap")
		r.Reasons = append(r.Reasons, "caveat: analog sticks/triggers (EV_ABS) can't be remapped yet")
	case KindTouchpad:
		r.Remappable = false
		r.Reasons = append(r.Reasons, "touchpad: relies on EV_ABS, which can't be mirrored yet — not recommended")
	default:
		r.Remappable = false
		r.Reasons = append(r.Reasons, "unrecognized device type — remap only if you know its event codes")
	}

	if in.Caps.HasAbs && in.Kind != KindTouchpad {
		r.Reasons = append(r.Reasons, "note: EV_ABS axes on this device are passed through unchanged (not remappable)")
	}
	return r
}
