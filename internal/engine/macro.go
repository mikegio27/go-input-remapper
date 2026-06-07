package engine

import (
	"fmt"
	"time"

	evdev "github.com/mikegio27/go-evdev"
	"github.com/mikegio27/go-input-remapper/internal/config"
)

// Event values for EV_KEY.
const (
	keyUp   int32 = 0
	keyDown int32 = 1
)

// compiledMacro is the runtime form of a config.Macro: a trigger key set and an
// ordered list of timed steps. Each step's events are pre-expanded (a tap is a
// down+up pair, text is a full keystroke sequence) so the scheduler only has to
// sleep and write.
type compiledMacro struct {
	name    string
	trigger map[evdev.EvCode]bool
	steps   []macroStep

	// repeat configuration (empty repeatMode = run the steps once per trigger).
	repeatMode     string        // config.RepeatMode* ("hold" | "toggle" | "count")
	repeatInterval time.Duration // wait between runs while repeating
	repeatCount    int           // total runs for "count" mode
}

// macroStep is one timed unit of a macro: pause for delay, then emit events (each
// written as its own synced frame by the scheduler).
type macroStep struct {
	delay  time.Duration
	events []evdev.InputEvent
}

// CompileMacros turns config macros into runtime form and reports every EV_KEY
// code they can emit, so the caller can enable those codes on the virtual device
// (a macro may type keys the source device lacks). It errors on unknown key names
// or unsupported text so a bad config is rejected before any device is grabbed.
func CompileMacros(macros []config.Macro) ([]compiledMacro, []evdev.EvCode, error) {
	var out []compiledMacro
	emitted := map[evdev.EvCode]bool{}
	note := func(code evdev.EvCode) { emitted[code] = true }

	for _, m := range macros {
		cm := compiledMacro{name: m.Name, trigger: map[evdev.EvCode]bool{}}
		for _, name := range m.Trigger {
			code, ok := config.LookupKey(name)
			if !ok {
				return nil, nil, fmt.Errorf("macro %q: unknown trigger key %q", m.Name, name)
			}
			cm.trigger[code] = true
		}
		if len(cm.trigger) == 0 {
			return nil, nil, fmt.Errorf("macro %q: empty trigger", m.Name)
		}
		for i, s := range m.Steps {
			step, err := compileStep(s, note)
			if err != nil {
				return nil, nil, fmt.Errorf("macro %q step %d: %w", m.Name, i, err)
			}
			cm.steps = append(cm.steps, step)
		}
		cm.repeatMode = m.Repeat
		cm.repeatInterval = time.Duration(m.RepeatMs) * time.Millisecond
		cm.repeatCount = m.RepeatCount
		out = append(out, cm)
	}

	extra := make([]evdev.EvCode, 0, len(emitted))
	for code := range emitted {
		extra = append(extra, code)
	}
	return out, extra, nil
}

// compileStep expands one config step into a timed list of events. note is
// called for every code the step emits so the caller can collect capabilities.
func compileStep(s config.MacroStep, note func(evdev.EvCode)) (macroStep, error) {
	step := macroStep{delay: time.Duration(s.DelayMs) * time.Millisecond}

	switch {
	case s.Key != "" || len(s.Keys) > 0:
		names := s.KeyNames()
		codes := make([]evdev.EvCode, 0, len(names))
		for _, name := range names {
			code, ok := config.LookupKey(name)
			if !ok {
				return macroStep{}, fmt.Errorf("unknown key %q", name)
			}
			note(code)
			codes = append(codes, code)
		}
		switch {
		case s.Hold:
			step.events = chordDown(codes)
		case s.Release:
			step.events = chordUp(codes)
		default:
			step.events = chordTap(codes)
		}
	case s.Text != "":
		events, err := compileText(s.Text, note)
		if err != nil {
			return macroStep{}, err
		}
		step.events = events
	default:
		// Pure delay: no events, just the pause already set above.
	}
	return step, nil
}

// compileText turns a literal string into a keystroke sequence on a US layout,
// pressing Shift for characters that need it. Unsupported runes are an error.
func compileText(text string, note func(evdev.EvCode)) ([]evdev.InputEvent, error) {
	var events []evdev.InputEvent
	for _, r := range text {
		name, shifted, ok := asciiToKey(r)
		if !ok {
			return nil, fmt.Errorf("unsupported character %q in text (US layout only)", r)
		}
		code, ok := config.LookupKey(name)
		if !ok {
			return nil, fmt.Errorf("internal: key %q not found for %q", name, r)
		}
		note(code)
		if shifted {
			note(evdev.KEY_LEFTSHIFT)
			events = append(events, keyEvent(evdev.KEY_LEFTSHIFT, keyDown))
			events = append(events, tap(code)...)
			events = append(events, keyEvent(evdev.KEY_LEFTSHIFT, keyUp))
		} else {
			events = append(events, tap(code)...)
		}
	}
	return events, nil
}

// tap is a press-and-release pair for one key.
func tap(code evdev.EvCode) []evdev.InputEvent {
	return []evdev.InputEvent{keyEvent(code, keyDown), keyEvent(code, keyUp)}
}

// chordDown presses every key down in order.
func chordDown(codes []evdev.EvCode) []evdev.InputEvent {
	events := make([]evdev.InputEvent, 0, len(codes))
	for _, code := range codes {
		events = append(events, keyEvent(code, keyDown))
	}
	return events
}

// chordUp releases every key in reverse order (mirror of chordDown).
func chordUp(codes []evdev.EvCode) []evdev.InputEvent {
	events := make([]evdev.InputEvent, 0, len(codes))
	for i := len(codes) - 1; i >= 0; i-- {
		events = append(events, keyEvent(codes[i], keyUp))
	}
	return events
}

// chordTap presses all keys down in order then releases them in reverse, so a
// modifier+key chord like Shift+3 produces the shifted character.
func chordTap(codes []evdev.EvCode) []evdev.InputEvent {
	return append(chordDown(codes), chordUp(codes)...)
}

func keyEvent(code evdev.EvCode, value int32) evdev.InputEvent {
	return evdev.InputEvent{Type: evdev.EV_KEY, Code: code, Value: value}
}
