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

func TestCompileMacroRepeatFields(t *testing.T) {
	macros, _ := mustCompile(t, []config.Macro{{
		Name: "r", Trigger: []string{"KEY_A"}, Steps: []config.MacroStep{{Key: "KEY_B"}},
		Repeat: config.RepeatModeCount, RepeatMs: 40, RepeatCount: 5,
	}})
	cm := macros[0]
	if cm.repeatMode != config.RepeatModeCount || cm.repeatInterval != 40*time.Millisecond || cm.repeatCount != 5 {
		t.Fatalf("repeat fields not compiled: mode=%q interval=%v count=%d", cm.repeatMode, cm.repeatInterval, cm.repeatCount)
	}
}

func TestProcessorTriggerHeld(t *testing.T) {
	macros, _ := mustCompile(t, []config.Macro{{
		Name: "cp", Trigger: []string{"KEY_LEFTCTRL", "KEY_J"}, Steps: []config.MacroStep{{Key: "KEY_C"}},
	}})
	p := newProcessor(Ruleset{keys: map[evdev.EvCode]keyAction{}}, macros)
	m := &macros[0]
	if p.triggerHeld(m) {
		t.Fatal("trigger should not be held initially")
	}
	p.process(keyEvent(evdev.KEY_LEFTCTRL, keyDown))
	p.process(keyEvent(evdev.KEY_J, keyDown))
	if !p.triggerHeld(m) {
		t.Fatal("trigger should be held after both keys down")
	}
	p.process(keyEvent(evdev.KEY_J, keyUp))
	if p.triggerHeld(m) {
		t.Fatal("trigger should not be held after J released")
	}
}

// TestSchedulerFireRepeatCount checks "count" mode runs the macro exactly N times
// and invokes onDone once when it finishes on its own.
func TestSchedulerFireRepeatCount(t *testing.T) {
	macros, _ := mustCompile(t, []config.Macro{{
		Name: "c", Trigger: []string{"KEY_A"}, Steps: []config.MacroStep{{Key: "KEY_B"}},
	}})
	sink := &recordSink{}
	var mu sync.Mutex
	s := newScheduler(sink, &mu)
	s.sleep = func(time.Duration) {}
	s.after = func(time.Duration) <-chan time.Time { // fire immediately so the interval is instant
		ch := make(chan time.Time, 1)
		ch <- time.Time{}
		return ch
	}
	doneCalls := 0
	s.fireRepeat(&macros[0], time.Millisecond, 3, make(chan struct{}), func() { doneCalls++ })
	s.wait()

	// tap KEY_B => 2 events per run, 3 runs => 6 events. Safe to read after wait().
	if len(sink.events) != 6 {
		t.Fatalf("got %d events, want 6 (3 runs of a 2-event tap)", len(sink.events))
	}
	if doneCalls != 1 {
		t.Fatalf("onDone calls = %d, want 1", doneCalls)
	}
}

// TestSchedulerFireRepeatStop checks an unbounded repeat stops promptly when its
// stop channel closes, interrupting the wait between runs.
func TestSchedulerFireRepeatStop(t *testing.T) {
	macros, _ := mustCompile(t, []config.Macro{{
		Name: "h", Trigger: []string{"KEY_A"}, Steps: []config.MacroStep{{Key: "KEY_B"}},
	}})
	sink := &recordSink{}
	var mu sync.Mutex
	s := newScheduler(sink, &mu)
	s.sleep = func(time.Duration) {}
	ran := make(chan struct{}, 1)
	block := make(chan time.Time) // never fires: the wait blocks until stop closes
	s.after = func(time.Duration) <-chan time.Time {
		select { // signal that a run finished and we're now waiting
		case ran <- struct{}{}:
		default:
		}
		return block
	}
	stop := make(chan struct{})
	s.fireRepeat(&macros[0], time.Hour, 0, stop, nil)
	<-ran       // one run done; goroutine is now blocked on the interval wait
	close(stop) // release it
	s.wait()

	if len(sink.events) != 2 {
		t.Fatalf("got %d events, want 2 (exactly one run before stop)", len(sink.events))
	}
}

// TestEmitCaptureReportsActive covers the signal the run loop uses to suppress a
// key from passthrough while it is being learned (the fix for capture leaking the
// pressed key into the TUI, doubling the value).
func TestEmitCaptureReportsActive(t *testing.T) {
	e := &Engine{sinks: map[chan<- evdev.InputEvent]struct{}{}}

	if e.emitCapture(keyEvent(evdev.KEY_A, keyDown)) {
		t.Error("no sinks: emitCapture should report not capturing")
	}

	ch := make(chan evdev.InputEvent, 1)
	remove := e.AddCaptureSink(ch)
	if !e.emitCapture(keyEvent(evdev.KEY_A, keyDown)) {
		t.Error("with a sink: emitCapture should report capturing")
	}
	select {
	case <-ch:
	default:
		t.Error("event should have been delivered to the capture sink")
	}

	remove()
	if e.emitCapture(keyEvent(evdev.KEY_A, keyDown)) {
		t.Error("after remove: emitCapture should report not capturing")
	}
}

// TestCompileChordStep checks a multi-key tap step presses all keys down in
// order then releases them in reverse (so Shift+3 yields the shifted char), and
// that Hold/Release variants emit only the downs/ups.
func TestCompileChordStep(t *testing.T) {
	macros, extra := mustCompile(t, []config.Macro{{
		Name:    "hash",
		Trigger: []string{"BTN_SIDE"},
		Steps: []config.MacroStep{
			{Keys: []string{"KEY_LEFTSHIFT", "KEY_3"}},                  // tap chord
			{Keys: []string{"KEY_LEFTCTRL", "KEY_LEFTALT"}, Hold: true}, // hold chord
			{Keys: []string{"KEY_LEFTCTRL", "KEY_LEFTALT"}, Release: true},
		},
	}})

	// Extras must include every key the chords touch.
	want := map[evdev.EvCode]bool{evdev.KEY_LEFTSHIFT: false, evdev.KEY_3: false, evdev.KEY_LEFTCTRL: false, evdev.KEY_LEFTALT: false}
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

	// Tap chord: Shift down, 3 down, 3 up, Shift up (reverse release order).
	wantTap := []evdev.InputEvent{
		keyEvent(evdev.KEY_LEFTSHIFT, keyDown),
		keyEvent(evdev.KEY_3, keyDown),
		keyEvent(evdev.KEY_3, keyUp),
		keyEvent(evdev.KEY_LEFTSHIFT, keyUp),
	}
	if got := macros[0].steps[0].events; !eventsEqual(got, wantTap) {
		t.Errorf("tap chord events = %+v, want %+v", got, wantTap)
	}
	// Hold chord: both downs, in order.
	wantHold := []evdev.InputEvent{keyEvent(evdev.KEY_LEFTCTRL, keyDown), keyEvent(evdev.KEY_LEFTALT, keyDown)}
	if got := macros[0].steps[1].events; !eventsEqual(got, wantHold) {
		t.Errorf("hold chord events = %+v, want %+v", got, wantHold)
	}
	// Release chord: both ups, reverse order.
	wantRel := []evdev.InputEvent{keyEvent(evdev.KEY_LEFTALT, keyUp), keyEvent(evdev.KEY_LEFTCTRL, keyUp)}
	if got := macros[0].steps[2].events; !eventsEqual(got, wantRel) {
		t.Errorf("release chord events = %+v, want %+v", got, wantRel)
	}
}

func eventsEqual(a, b []evdev.InputEvent) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Code != b[i].Code || a[i].Value != b[i].Value || a[i].Type != b[i].Type {
			return false
		}
	}
	return true
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
