// Package engine runs the remap loop for a single device: it grabs the source
// exclusively, reads its events, transforms them per a compiled rule set, and
// re-emits them through a uinput virtual device. It uses go-evdev's low-level
// primitives directly (rather than the high-level Remapper) so the loop can be
// stateful — needed for chord/combo triggers and timed macros in later
// milestones.
package engine

import (
	"fmt"

	evdev "github.com/mikegio27/go-evdev"
	"github.com/mikegio27/nereus/internal/config"
)

// keyAction is what a rule set does with a given key code: rewrite it to another
// code, or suppress it entirely.
type keyAction struct {
	to       evdev.EvCode
	suppress bool
}

// Ruleset is the compiled form of a binding's remaps: a code-to-action table
// over the EV_KEY namespace. It is the pure, hardware-independent core of the
// engine, applied event-by-event by Apply.
type Ruleset struct {
	keys map[evdev.EvCode]keyAction
}

// CompileRemaps turns config remaps into a Ruleset and reports the extra target
// codes that must be enabled on the virtual device (a remap may emit a key the
// source device itself lacks). It errors on unknown key names so a bad config is
// rejected before any device is grabbed.
func CompileRemaps(remaps []config.Remap) (Ruleset, []evdev.EvCode, error) {
	rs := Ruleset{keys: map[evdev.EvCode]keyAction{}}
	var extra []evdev.EvCode
	for _, r := range remaps {
		from, ok := config.LookupKey(r.From)
		if !ok {
			return Ruleset{}, nil, fmt.Errorf("unknown 'from' key %q", r.From)
		}
		if r.Suppresses() {
			rs.keys[from] = keyAction{suppress: true}
			continue
		}
		to, ok := config.LookupKey(r.To)
		if !ok {
			return Ruleset{}, nil, fmt.Errorf("unknown 'to' key %q", r.To)
		}
		rs.keys[from] = keyAction{to: to}
		extra = append(extra, to)
	}
	return rs, extra, nil
}

// Apply returns the events to emit in place of ev. A remapped key yields one
// event with its code rewritten (preserving the press/release/repeat value, so
// up/down stay paired); a suppressed key yields nothing; everything else passes
// through unchanged. EV_SYN frame markers are handled by the run loop, not here.
func (rs Ruleset) Apply(ev evdev.InputEvent) []evdev.InputEvent {
	if ev.Type != evdev.EV_KEY {
		return []evdev.InputEvent{ev}
	}
	action, ok := rs.keys[ev.Code]
	if !ok {
		return []evdev.InputEvent{ev}
	}
	if action.suppress {
		return nil
	}
	ev.Code = action.to
	return []evdev.InputEvent{ev}
}
