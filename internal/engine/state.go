package engine

import evdev "github.com/mikegio27/go-evdev"

// processor is the stateful core of the engine: it tracks which keys are held,
// fires macros when a trigger chord completes, suppresses trigger keys, and
// applies remaps to everything else. It is pure (no I/O), so the run loop and
// tests drive it the same way.
type processor struct {
	rules  Ruleset
	macros []compiledMacro

	held       map[evdev.EvCode]bool // keys currently down on the source
	suppressed map[evdev.EvCode]bool // trigger keys to drop until they release
}

func newProcessor(rules Ruleset, macros []compiledMacro) *processor {
	return &processor{
		rules:      rules,
		macros:     macros,
		held:       map[evdev.EvCode]bool{},
		suppressed: map[evdev.EvCode]bool{},
	}
}

// process handles one EV_KEY event and returns the events to emit in its place
// plus a macro to fire, if the event completed a trigger chord. A fired macro's
// own completing key is suppressed (and so are its repeats and release), so it
// does not also pass through. Non-EV_KEY events should not be passed here — the
// run loop forwards them directly.
func (p *processor) process(ev evdev.InputEvent) ([]evdev.InputEvent, *compiledMacro) {
	code := ev.Code
	switch ev.Value {
	case keyDown:
		p.held[code] = true
		if m := p.firedBy(code); m != nil {
			p.suppressed[code] = true
			return nil, m
		}
		return p.rules.Apply(ev), nil
	case keyUp:
		delete(p.held, code)
		if p.suppressed[code] {
			delete(p.suppressed, code)
			return nil, nil
		}
		return p.rules.Apply(ev), nil
	default: // key repeat (value 2) and anything else
		if p.suppressed[code] {
			return nil, nil
		}
		return p.rules.Apply(ev), nil
	}
}

// firedBy returns the first macro whose trigger chord is now fully held and
// includes the just-pressed code (so the chord completes on this key, not on a
// modifier pressed earlier). An already-suppressed key cannot re-fire.
func (p *processor) firedBy(code evdev.EvCode) *compiledMacro {
	if p.suppressed[code] {
		return nil
	}
	for i := range p.macros {
		m := &p.macros[i]
		if !m.trigger[code] {
			continue
		}
		if p.allHeld(m.trigger) {
			return m
		}
	}
	return nil
}

// allHeld reports whether every key in the set is currently down.
func (p *processor) allHeld(set map[evdev.EvCode]bool) bool {
	for code := range set {
		if !p.held[code] {
			return false
		}
	}
	return true
}
