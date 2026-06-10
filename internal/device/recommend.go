package device

import (
	"fmt"
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
	Primary    bool     // the likely-correct node when one device exposes several
	Reasons    []string // why it is (or isn't) a good target, and caveats
}

// Recommend turns inspection results into per-device advice. It steers users
// toward keyboards/mice/gamepads, warns hard against remapping our own virtual
// devices (feedback loop), and flags the EV_ABS limitation (gamepad analog
// sticks and touch surfaces can't be mirrored by go-evdev yet). One physical
// device often exposes several /dev/input/eventX nodes; markPrimary picks the
// likeliest one to edit within each group so the TUI can highlight it.
func Recommend(infos []Info) []Recommendation {
	out := make([]Recommendation, 0, len(infos))
	for _, in := range infos {
		out = append(out, recommendOne(in))
	}
	markPrimary(out)
	return out
}

// markPrimary groups remappable nodes by stable identity and flags the
// highest-scoring node in each group as the primary (likely-correct) one. When a
// group has more than one node it annotates the reasons so the TUI/CLI can point
// users away from secondary nodes (e.g. a keyboard's consumer-control node).
func markPrimary(recs []Recommendation) {
	type agg struct{ bestIdx, bestScore, count int }
	groups := map[string]*agg{}
	for i := range recs {
		r := &recs[i]
		if !r.Remappable {
			continue
		}
		key := groupKey(r.Info.Identity)
		g := groups[key]
		if g == nil {
			g = &agg{bestIdx: i, bestScore: nodeScore(r.Info)}
			groups[key] = g
			g.count++
			continue
		}
		g.count++
		if sc := nodeScore(r.Info); sc > g.bestScore { // strict: ties keep the first (lowest path)
			g.bestScore, g.bestIdx = sc, i
		}
	}
	// Only flag a primary when there's actually a choice to make: a single-node
	// device has no ambiguity, so starring it would just add noise.
	for _, g := range groups {
		if g.count >= 2 {
			recs[g.bestIdx].Primary = true
		}
	}
	for i := range recs {
		r := &recs[i]
		if !r.Remappable {
			continue
		}
		g := groups[groupKey(r.Info.Identity)]
		if g == nil || g.count < 2 {
			continue // single-node device: no ambiguity to call out
		}
		if r.Primary {
			r.Reasons = append(r.Reasons, "likely the right node to remap (this device exposes multiple event nodes)")
		} else {
			r.Reasons = append(r.Reasons, "secondary node — the likely one to edit is "+recs[g.bestIdx].Info.Identity.Path)
		}
	}
}

// groupKey identifies the physical device a node belongs to. Uniq is unique per
// unit when the driver reports it; otherwise nodes are grouped by model+name,
// which collapses identical models but still correctly groups one device's nodes.
func groupKey(id Identity) string {
	if id.Uniq != "" {
		return "u:" + id.Uniq
	}
	return fmt.Sprintf("m:%04x:%04x:%s", id.Vendor, id.Product, id.Name)
}

// nodeScore ranks a node as a remap target: prefer the richer device kind, and
// within a kind the node exposing the most relevant capability codes (the real
// keyboard node carries the full KEY_* range; a power/consumer node carries few).
func nodeScore(in Info) int {
	switch in.Kind {
	case KindKeyboard:
		return 3000 + len(in.Caps.Keys)
	case KindMouse:
		return 2000 + len(in.Caps.Keys) + len(in.Caps.Rels)
	case KindGamepad:
		return 1000 + len(in.Caps.Keys)
	default:
		return len(in.Caps.Keys)
	}
}

func recommendOne(in Info) Recommendation {
	r := Recommendation{Info: in}

	if in.IsVirtual {
		r.Remappable = false
		r.Reasons = append(r.Reasons,
			"this is a nereus virtual device — remapping it would create a feedback loop")
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
