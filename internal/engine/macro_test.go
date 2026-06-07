package engine

import (
	"sync"
	"testing"
	"time"

	evdev "github.com/mikegio27/go-evdev"
	"github.com/mikegio27/go-input-remapper/internal/config"
)

// recordSink records the events and syncs written to it, in order.
type recordSink struct {
	events []evdev.InputEvent
	syncs  int
}

func (r *recordSink) Write(ev evdev.InputEvent) error { r.events = append(r.events, ev); return nil }
func (r *recordSink) Sync() error                     { r.syncs++; return nil }

func mustCompile(t *testing.T, macros []config.Macro) ([]compiledMacro, []evdev.EvCode) {
	t.Helper()
	cm, extra, err := CompileMacros(macros)
	if err != nil {
		t.Fatalf("CompileMacros: %v", err)
	}
	return cm, extra
}

func TestCompileMacroErrors(t *testing.T) {
	cases := []config.Macro{
		{Name: "bad trigger", Trigger: []string{"KEY_NOPE"}},
		{Name: "bad step key", Trigger: []string{"KEY_A"}, Steps: []config.MacroStep{{Key: "KEY_NOPE"}}},
		{Name: "bad text", Trigger: []string{"KEY_A"}, Steps: []config.MacroStep{{Text: "é"}}}, // é, not US-typeable
	}
	for _, m := range cases {
		if _, _, err := CompileMacros([]config.Macro{m}); err == nil {
			t.Errorf("macro %q: expected compile error", m.Name)
		}
	}
}

func TestCompileTextAndExtras(t *testing.T) {
	macros, extra := mustCompile(t, []config.Macro{{
		Name:    "greet",
		Trigger: []string{"BTN_SIDE"},
		Steps:   []config.MacroStep{{Text: "Hi!"}},
	}})

	// Extras must include the letter keys, shift, and the '1' used by '!'.
	want := map[evdev.EvCode]bool{evdev.KEY_H: false, evdev.KEY_I: false, evdev.KEY_1: false, evdev.KEY_LEFTSHIFT: false}
	for _, c := range extra {
		if _, ok := want[c]; ok {
			want[c] = true
		}
	}
	for code, seen := range want {
		if !seen {
			t.Errorf("expected extra capability for %s", evdev.CodeName(evdev.EV_KEY, code))
		}
	}

	// "Hi!" => H (shift+h), i (tap), ! (shift+1). Check the shift wraps H and !.
	evs := macros[0].steps[0].events
	if len(evs) == 0 || evs[0] != keyEvent(evdev.KEY_LEFTSHIFT, keyDown) {
		t.Errorf("expected text to start with Shift down for 'H'; got %+v", evs)
	}
}

func TestProcessorRemapPassthrough(t *testing.T) {
	rules, _, _ := CompileRemaps([]config.Remap{{From: "KEY_CAPSLOCK", To: "KEY_ESC"}})
	p := newProcessor(rules, nil)

	out, m := p.process(keyEvent(evdev.KEY_CAPSLOCK, keyDown))
	if m != nil || len(out) != 1 || out[0].Code != evdev.KEY_ESC {
		t.Fatalf("expected remap to KEY_ESC, got out=%+v macro=%v", out, m)
	}
}

func TestProcessorSingleKeyTrigger(t *testing.T) {
	macros, _ := mustCompile(t, []config.Macro{{
		Name: "m", Trigger: []string{"BTN_SIDE"}, Steps: []config.MacroStep{{Key: "KEY_A"}},
	}})
	p := newProcessor(Ruleset{keys: map[evdev.EvCode]keyAction{}}, macros)

	// Down completes the (single-key) trigger: macro fires, key suppressed.
	out, fired := p.process(keyEvent(evdev.BTN_SIDE, keyDown))
	if fired == nil || len(out) != 0 {
		t.Fatalf("expected fire+suppress on down, got out=%+v fired=%v", out, fired)
	}
	// Repeat while held is dropped and does not re-fire.
	out, fired = p.process(keyEvent(evdev.BTN_SIDE, 2))
	if fired != nil || len(out) != 0 {
		t.Fatalf("expected repeat suppressed, got out=%+v fired=%v", out, fired)
	}
	// Release is suppressed too.
	out, fired = p.process(keyEvent(evdev.BTN_SIDE, keyUp))
	if fired != nil || len(out) != 0 {
		t.Fatalf("expected release suppressed, got out=%+v fired=%v", out, fired)
	}
}

func TestProcessorChordTrigger(t *testing.T) {
	macros, _ := mustCompile(t, []config.Macro{{
		Name: "cp", Trigger: []string{"KEY_LEFTCTRL", "KEY_J"}, Steps: []config.MacroStep{{Key: "KEY_C"}},
	}})
	p := newProcessor(Ruleset{keys: map[evdev.EvCode]keyAction{}}, macros)

	// Ctrl down: not a completion, passes through.
	out, fired := p.process(keyEvent(evdev.KEY_LEFTCTRL, keyDown))
	if fired != nil || len(out) != 1 || out[0].Code != evdev.KEY_LEFTCTRL {
		t.Fatalf("expected Ctrl passthrough, got out=%+v fired=%v", out, fired)
	}
	// J down completes the chord: fires, J suppressed.
	out, fired = p.process(keyEvent(evdev.KEY_J, keyDown))
	if fired == nil || len(out) != 0 {
		t.Fatalf("expected chord fire on J, got out=%+v fired=%v", out, fired)
	}
	// Ctrl release still passes through (it was never suppressed).
	out, fired = p.process(keyEvent(evdev.KEY_LEFTCTRL, keyUp))
	if fired != nil || len(out) != 1 {
		t.Fatalf("expected Ctrl release passthrough, got out=%+v fired=%v", out, fired)
	}
}

func TestSchedulerRunsStepsInOrder(t *testing.T) {
	macros, _ := mustCompile(t, []config.Macro{{
		Name:    "seq",
		Trigger: []string{"KEY_A"},
		Steps: []config.MacroStep{
			{Key: "KEY_LEFTCTRL", Hold: true},
			{Key: "KEY_C"},
			{DelayMs: 50, Key: "KEY_LEFTCTRL", Release: true},
		},
	}})

	sink := &recordSink{}
	var mu sync.Mutex
	var slept time.Duration
	s := newScheduler(sink, &mu)
	s.sleep = func(d time.Duration) { slept += d }

	s.run(&macros[0])

	// Hold(Ctrl down), tap C(down,up), release Ctrl(up) => 4 events, 4 syncs.
	wantCodes := []struct {
		code evdev.EvCode
		val  int32
	}{
		{evdev.KEY_LEFTCTRL, keyDown},
		{evdev.KEY_C, keyDown},
		{evdev.KEY_C, keyUp},
		{evdev.KEY_LEFTCTRL, keyUp},
	}
	if len(sink.events) != len(wantCodes) {
		t.Fatalf("got %d events, want %d: %+v", len(sink.events), len(wantCodes), sink.events)
	}
	for i, w := range wantCodes {
		if sink.events[i].Code != w.code || sink.events[i].Value != w.val {
			t.Errorf("event[%d] = %s/%d, want %s/%d", i,
				evdev.CodeName(evdev.EV_KEY, sink.events[i].Code), sink.events[i].Value,
				evdev.CodeName(evdev.EV_KEY, w.code), w.val)
		}
	}
	if sink.syncs != len(wantCodes) {
		t.Errorf("syncs = %d, want %d", sink.syncs, len(wantCodes))
	}
	if slept != 50*time.Millisecond {
		t.Errorf("slept = %v, want 50ms", slept)
	}
}
