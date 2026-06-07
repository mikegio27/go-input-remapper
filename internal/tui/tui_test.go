package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mikegio27/go-input-remapper/internal/config"
	"github.com/mikegio27/go-input-remapper/internal/control"
)

// TestConfigErrorKeepsProfiles checks a config-load failure never blanks the
// Profiles list (the bug where a transient read error left it empty), and that a
// background refresh stays silent while an explicit load surfaces the error.
func TestConfigErrorKeepsProfiles(t *testing.T) {
	m, _ := newTestModel(t)
	if len(m.profileNames) != 1 {
		t.Fatalf("setup: expected 1 profile, got %d", len(m.profileNames))
	}

	m.Update(configMsg{err: errors.New("boom"), quiet: true})
	if len(m.profileNames) != 1 {
		t.Errorf("quiet config error should keep profiles; got %d", len(m.profileNames))
	}
	if m.flash != "" {
		t.Errorf("quiet config error should not flash; got %q", m.flash)
	}

	m.Update(configMsg{err: errors.New("boom2")})
	if len(m.profileNames) != 1 {
		t.Errorf("config error should keep profiles; got %d", len(m.profileNames))
	}
	if m.flash == "" || !m.flashErr {
		t.Errorf("explicit config error should flash an error; flash=%q err=%v", m.flash, m.flashErr)
	}
}

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

// newTestModel returns a model seeded with a config dir containing one empty
// profile plus a single device, as if the async fetches had completed.
func newTestModel(t *testing.T) (*Model, string) {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{
		ActiveProfile: "default",
		VirtualPrefix: "go-input-remapper",
		Profiles:      map[string]*config.Profile{"default": {Name: "default"}},
	}
	if err := config.Save(dir, cfg); err != nil {
		t.Fatal(err)
	}

	m := &Model{opts: Options{ConfigDir: dir, SocketPath: dir + "/nosock"}}
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.Update(configMsg{cfg: cfg})
	m.Update(statusMsg{up: false})
	m.Update(devicesMsg{devices: []control.DeviceInfo{
		{Path: "/dev/input/event3", Name: "Test Keyboard", Kind: "keyboard", Vendor: 0x1234, Product: 0x5678, Recommended: true},
	}})
	return m, dir
}

func TestModelRendersScreens(t *testing.T) {
	m, _ := newTestModel(t)

	if !strings.Contains(m.View(), "Test Keyboard") {
		t.Error("devices view should list the device")
	}
	m.Update(key("tab")) // -> profiles
	if !strings.Contains(m.View(), "default") {
		t.Error("profiles view should list the default profile")
	}
	m.Update(key("tab")) // -> status
	if !strings.Contains(m.View(), "daemon") {
		t.Error("status view should mention the daemon")
	}
}

// TestCaptureReadyHandshake checks the overlay waits for the daemon's "ready"
// message before treating a key as captured, and then completes on a single key
// press (the fix for capture needing two presses).
func TestCaptureReadyHandshake(t *testing.T) {
	m, _ := newTestModel(t)
	m.Update(key("enter")) // open editor
	m.Update(key("a"))     // adding mode
	if m.editor == nil || !m.editor.adding {
		t.Fatal("expected editor in adding mode")
	}
	// Simulate beginCapture having opened the overlay (no real socket/session).
	m.capture = &captureState{mode: "key", purpose: purposeRemapFrom, prompt: "press"}

	if m.capture.ready {
		t.Fatal("capture should not be ready before the daemon attaches")
	}
	m.Update(captureEventMsg{ev: control.CaptureEvent{Ready: true}})
	if m.capture == nil || !m.capture.ready {
		t.Fatal("expected capture marked ready after the ready message")
	}
	// One key-down after ready must complete the capture and fill FROM.
	m.Update(captureEventMsg{ev: control.CaptureEvent{KeyName: "KEY_RIGHTBRACE", Value: 1}})
	if m.capture != nil {
		t.Fatal("capture should finish on the first key after ready")
	}
	if got := m.editor.fromInput.Value(); got != "KEY_RIGHTBRACE" {
		t.Fatalf("FROM = %q, want KEY_RIGHTBRACE", got)
	}
}

// TestMappingsScreen checks the profile-wide Mappings tab lists remaps and
// macros, and that enter on a row opens the right editor.
func TestMappingsScreen(t *testing.T) {
	m, _ := newTestModel(t)
	m.cfg.Profiles["default"] = &config.Profile{
		Name: "default",
		Devices: []config.DeviceBinding{{
			Match:  config.DeviceMatcher{Name: "Test Keyboard", Vendor: 0x1234, Product: 0x5678},
			Remaps: []config.Remap{{From: "KEY_CAPSLOCK", To: "KEY_ESC"}},
			Macros: []config.Macro{{Name: "greet", Trigger: []string{"KEY_LEFTCTRL", "KEY_G"}, Steps: []config.MacroStep{{Key: "KEY_H"}}}},
		}},
	}
	m.activeProfile = "default"

	if rows := m.mappingRows(); len(rows) != 2 {
		t.Fatalf("expected 2 mapping rows (remap + macro), got %d", len(rows))
	}

	m.screen = screenMappings
	v := m.View()
	for _, want := range []string{"KEY_CAPSLOCK", "KEY_ESC", "greet"} {
		if !strings.Contains(v, want) {
			t.Errorf("mappings view missing %q", want)
		}
	}

	// enter on the remap row opens the remap editor for that device.
	m.mapCursor = 0
	m.Update(key("enter"))
	if m.editor == nil {
		t.Fatal("enter on a remap row should open the remap editor")
	}

	// enter on the macro row opens the macro recorder.
	m.editor = nil
	m.mapCursor = 1
	m.Update(key("enter"))
	if m.macro == nil {
		t.Fatal("enter on a macro row should open the macro recorder")
	}
}

func TestEditorAddRemapAndSave(t *testing.T) {
	m, dir := newTestModel(t)

	// Open the remap editor for the selected device.
	m.Update(key("enter"))
	if m.editor == nil {
		t.Fatal("expected editor to open")
	}

	// Begin adding; simulate captured FROM and TO keys, then commit.
	m.Update(key("a"))
	if !m.editor.adding {
		t.Fatal("expected adding mode")
	}
	m.editorCaptured(purposeRemapFrom, []string{"KEY_CAPSLOCK"})
	m.editorCaptured(purposeRemapTo, []string{"KEY_ESC"})
	m.Update(key("enter")) // commit row
	if len(m.editor.remaps) != 1 || m.editor.remaps[0].From != "KEY_CAPSLOCK" || m.editor.remaps[0].To != "KEY_ESC" {
		t.Fatalf("remap not committed: %+v", m.editor.remaps)
	}

	// Save writes the profile to disk (the reload cmd is returned but not run).
	m.Update(key("s"))
	if m.editor != nil {
		t.Error("editor should close after save")
	}

	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	prof := cfg.Profiles["default"]
	if prof == nil || len(prof.Devices) != 1 {
		t.Fatalf("expected one device binding persisted, got %+v", prof)
	}
	b := prof.Devices[0]
	if b.Match.Name != "Test Keyboard" || len(b.Remaps) != 1 || b.Remaps[0].To != "KEY_ESC" {
		t.Errorf("persisted binding wrong: %+v", b)
	}
}

func TestEditorRejectsInvalidKey(t *testing.T) {
	m, _ := newTestModel(t)
	m.Update(key("enter")) // open editor
	m.Update(key("a"))     // add
	m.editorCaptured(purposeRemapFrom, []string{"KEY_NOPE"})
	m.Update(key("enter")) // commit attempt
	if len(m.editor.remaps) != 0 {
		t.Error("invalid FROM key should not commit")
	}
	if !m.flashErr {
		t.Error("expected an error flash for invalid key")
	}
}

func TestDeviceFilterDefaultRemappableOnly(t *testing.T) {
	m, _ := newTestModel(t)
	// Add an unknown, non-remappable device alongside the keyboard.
	m.Update(devicesMsg{devices: []control.DeviceInfo{
		{Path: "/dev/input/event3", Name: "Test Keyboard", Kind: "keyboard", Recommended: true},
		{Path: "/dev/input/event9", Name: "Weird Sensor", Kind: "unknown", Recommended: false},
		{Path: "/dev/input/event10", Name: "go-input-remapper Test Keyboard", Kind: "keyboard", Recommended: false, IsVirtual: true},
	}})

	if got := len(m.visibleDevices()); got != 1 {
		t.Fatalf("default view should show 1 remappable device, got %d", got)
	}
	if m.visibleDevices()[0].Name != "Test Keyboard" {
		t.Errorf("expected the keyboard, got %s", m.visibleDevices()[0].Name)
	}

	// Toggle show-all.
	m.Update(key("a"))
	if got := len(m.visibleDevices()); got != 3 {
		t.Errorf("show-all should show all 3 devices, got %d", got)
	}
}

func TestProfileCreateAndDelete(t *testing.T) {
	m, dir := newTestModel(t)
	m.Update(key("tab")) // -> profiles

	// Create "gaming".
	m.Update(key("n"))
	if !m.addingProfile {
		t.Fatal("expected profile-name entry mode")
	}
	for _, r := range "gaming" {
		m.Update(key(string(r)))
	}
	_, cmd := m.Update(key("enter"))
	if cmd == nil {
		t.Fatal("expected a create command")
	}
	cmd() // perform the disk write

	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.Profiles["gaming"]; !ok {
		t.Fatalf("gaming profile not created on disk; have %v", cfg.Profiles)
	}

	// Reload the model's view of profiles, then delete the default.
	m.Update(configMsg{cfg: cfg})
	m.profCursor = indexOf(m.profileNames, "default")
	_, cmd = m.Update(key("d"))
	if cmd == nil {
		t.Fatal("expected a delete command")
	}
	cmd()
	cfg, _ = config.Load(dir)
	if _, ok := cfg.Profiles["default"]; ok {
		t.Error("default profile should have been deleted")
	}
}

func indexOf(ss []string, want string) int {
	for i, s := range ss {
		if s == want {
			return i
		}
	}
	return 0
}

func TestMacroRecorderBuildAndSave(t *testing.T) {
	m, dir := newTestModel(t)

	m.Update(key("m")) // open macro recorder
	if m.macro == nil {
		t.Fatal("expected macro recorder to open")
	}
	m.Update(key("n")) // new macro -> name stage
	// Type a name rune-by-rune.
	for _, r := range "greet" {
		m.Update(key(string(r)))
	}
	m.Update(key("enter")) // confirm name -> begins trigger capture (overlay opens)
	// Simulate the capture overlay resolving (the real flow finishes via the
	// daemon stream; here we deliver the result directly).
	m.capture = nil
	m.macroCaptured(purposeMacroTrigger, []string{"KEY_LEFTCTRL", "KEY_G"})
	if m.macro.stage != macroStageSteps {
		t.Fatalf("expected steps stage, got %v", m.macro.stage)
	}
	m.macroCaptured(purposeMacroStep, []string{"KEY_H"})
	m.Update(key("enter")) // finish steps -> repeat-config stage
	if m.macro.stage != macroStageRepeat {
		t.Fatalf("expected repeat stage, got %v", m.macro.stage)
	}
	m.Update(key("enter")) // accept default "none" repeat -> added to list
	if len(m.macro.macros) != 1 {
		t.Fatalf("expected one macro, got %d", len(m.macro.macros))
	}
	m.Update(key("s")) // save

	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	b := cfg.Profiles["default"].Devices[0]
	if len(b.Macros) != 1 || b.Macros[0].Name != "greet" || len(b.Macros[0].Steps) != 1 {
		t.Errorf("macro not persisted correctly: %+v", b.Macros)
	}
	if b.Macros[0].Repeat != config.RepeatModeNone {
		t.Errorf("expected non-repeating macro, got repeat=%q", b.Macros[0].Repeat)
	}
}

// TestMacroRecorderRepeatCount drives the repeat-config stage into "count" mode
// and checks the interval and run count are captured and persisted.
func TestMacroRecorderRepeatCount(t *testing.T) {
	m, dir := newTestModel(t)

	m.Update(key("m"))
	m.Update(key("n"))
	for _, r := range "spam" {
		m.Update(key(string(r)))
	}
	m.Update(key("enter"))
	m.capture = nil
	m.macroCaptured(purposeMacroTrigger, []string{"KEY_F5"})
	m.macroCaptured(purposeMacroStep, []string{"KEY_A"})
	m.Update(key("enter")) // -> repeat stage

	// none -> hold -> toggle -> count
	m.Update(key("m"))
	m.Update(key("m"))
	m.Update(key("m"))
	if m.macro.repeatMode != config.RepeatModeCount {
		t.Fatalf("expected count mode, got %q", m.macro.repeatMode)
	}
	for _, r := range "25" { // interval ms (interval field focused by default)
		m.Update(key(string(r)))
	}
	m.Update(key("tab")) // switch to runs field
	for _, r := range "3" {
		m.Update(key(string(r)))
	}
	m.Update(key("enter")) // finish
	if len(m.macro.macros) != 1 {
		t.Fatalf("expected one macro, got %d", len(m.macro.macros))
	}
	m.Update(key("s"))

	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	mac := cfg.Profiles["default"].Devices[0].Macros[0]
	if mac.Repeat != config.RepeatModeCount || mac.RepeatMs != 25 || mac.RepeatCount != 3 {
		t.Errorf("repeat config not persisted: repeat=%q ms=%d count=%d", mac.Repeat, mac.RepeatMs, mac.RepeatCount)
	}
}
