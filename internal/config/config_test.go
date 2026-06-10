package config

import (
	"os"
	"path/filepath"
	"testing"
)

// sampleConfig builds a config exercising matchers, remaps, and a timed macro.
func sampleConfig() *Config {
	return &Config{
		ActiveProfile: "default",
		VirtualPrefix: "nereus",
		Profiles: map[string]*Profile{
			"default": {
				Name: "default",
				Devices: []DeviceBinding{
					{
						Match: DeviceMatcher{Name: "AT Translated Set 2 keyboard", Vendor: 0x0001, Product: 0x0001},
						Remaps: []Remap{
							{From: "KEY_CAPSLOCK", To: "KEY_ESC"},
							{From: "KEY_SCROLLLOCK", To: ""}, // suppress
						},
						Macros: []Macro{
							{
								Name:    "copy-paste",
								Trigger: []string{"KEY_LEFTCTRL", "KEY_J"},
								Steps: []MacroStep{
									{Key: "KEY_LEFTCTRL", Hold: true},
									{Key: "KEY_C"},
									{DelayMs: 100, Key: "KEY_V"},
									{Key: "KEY_LEFTCTRL", Release: true},
								},
							},
						},
					},
				},
			},
		},
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	orig := sampleConfig()
	if err := Save(dir, orig); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.ActiveProfile != orig.ActiveProfile {
		t.Errorf("ActiveProfile = %q, want %q", got.ActiveProfile, orig.ActiveProfile)
	}
	if got.VirtualPrefix != orig.VirtualPrefix {
		t.Errorf("VirtualPrefix = %q, want %q", got.VirtualPrefix, orig.VirtualPrefix)
	}
	p, ok := got.Profiles["default"]
	if !ok {
		t.Fatalf("profile %q missing after round-trip", "default")
	}
	if len(p.Devices) != 1 {
		t.Fatalf("got %d device bindings, want 1", len(p.Devices))
	}
	b := p.Devices[0]
	if b.Match.Vendor != 0x0001 || b.Match.Product != 0x0001 {
		t.Errorf("matcher vendor/product = %04x:%04x, want 0001:0001", b.Match.Vendor, b.Match.Product)
	}
	if len(b.Remaps) != 2 || b.Remaps[0].To != "KEY_ESC" || !b.Remaps[1].Suppresses() {
		t.Errorf("remaps not preserved: %+v", b.Remaps)
	}
	if len(b.Macros) != 1 || len(b.Macros[0].Steps) != 4 {
		t.Fatalf("macro not preserved: %+v", b.Macros)
	}
	if b.Macros[0].Steps[2].DelayMs != 100 {
		t.Errorf("macro step delay = %d, want 100", b.Macros[0].Steps[2].DelayMs)
	}
}

// TestSavedFilesAreGroupReadable guards the fix for a root daemon and a user TUI
// sharing one config dir: atomic writes must land at 0644, not CreateTemp's 0600,
// or the other party gets permission denied reading config.toml.
func TestSavedFilesAreGroupReadable(t *testing.T) {
	dir := t.TempDir()
	if err := Save(dir, sampleConfig()); err != nil {
		t.Fatalf("Save: %v", err)
	}
	for _, rel := range []string{"config.toml", filepath.Join("profiles", "default.toml")} {
		fi, err := os.Stat(filepath.Join(dir, rel))
		if err != nil {
			t.Fatal(err)
		}
		if got := fi.Mode().Perm(); got != 0o644 {
			t.Errorf("%s mode = %o, want 644", rel, got)
		}
	}
}

func TestHexU16InFile(t *testing.T) {
	dir := t.TempDir()
	if err := Save(dir, sampleConfig()); err != nil {
		t.Fatalf("Save: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "profiles", "default.toml"))
	if err != nil {
		t.Fatal(err)
	}
	// Vendor/product must render as lsusb-style hex ("0001"), not decimal (1).
	got := string(data)
	if !contains(got, `'0001'`) && !contains(got, `"0001"`) {
		t.Errorf("vendor not rendered as hex string; file:\n%s", got)
	}
}

func TestLoadMissingDirIsDefault(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatalf("Load of missing dir should not error: %v", err)
	}
	if cfg.VirtualPrefix == "" {
		t.Error("expected a default VirtualPrefix")
	}
	if len(cfg.Profiles) != 0 {
		t.Errorf("expected no profiles, got %d", len(cfg.Profiles))
	}
}

func TestValidate(t *testing.T) {
	t.Run("valid config has no errors", func(t *testing.T) {
		if errs := Validate(sampleConfig()); len(errs) != 0 {
			t.Errorf("expected no errors, got %v", errs)
		}
	})

	t.Run("catches problems", func(t *testing.T) {
		cfg := &Config{
			ActiveProfile: "ghost", // does not exist
			Profiles: map[string]*Profile{
				"p": {
					Name: "p",
					Devices: []DeviceBinding{
						{Match: DeviceMatcher{}}, // empty matcher
						{
							Match: DeviceMatcher{Name: "kbd"},
							Remaps: []Remap{
								{From: "KEY_BOGUS", To: "KEY_ESC"},    // bad from
								{From: "KEY_A", To: "KEY_ALSO_BOGUS"}, // bad to
								{From: "KEY_A", To: "KEY_B"},          // duplicate from
							},
							Macros: []Macro{
								{Name: "", Trigger: nil}, // no name, empty trigger
								{Name: "m", Trigger: []string{"KEY_X"}, Steps: []MacroStep{{}}}, // empty step
							},
						},
					},
				},
			},
		}
		errs := Validate(cfg)
		if len(errs) < 6 {
			t.Errorf("expected several validation errors, got %d: %v", len(errs), errs)
		}
	})

	t.Run("multi-key steps", func(t *testing.T) {
		mk := func(s MacroStep) *Config {
			return &Config{Profiles: map[string]*Profile{"p": {Name: "p", Devices: []DeviceBinding{{
				Match:  DeviceMatcher{Name: "kbd"},
				Macros: []Macro{{Name: "m", Trigger: []string{"KEY_A"}, Steps: []MacroStep{s}}},
			}}}}}
		}
		if errs := Validate(mk(MacroStep{Keys: []string{"KEY_LEFTSHIFT", "KEY_3"}})); len(errs) != 0 {
			t.Errorf("valid chord step should pass, got %v", errs)
		}
		if errs := Validate(mk(MacroStep{Keys: []string{"KEY_LEFTSHIFT", "KEY_BOGUS"}})); len(errs) == 0 {
			t.Error("chord step with a bad key should fail")
		}
		if errs := Validate(mk(MacroStep{Keys: []string{"KEY_A"}, Text: "x"})); len(errs) == 0 {
			t.Error("keys + text in one step should fail")
		}
	})

	t.Run("repeat settings", func(t *testing.T) {
		mk := func(m Macro) *Config {
			return &Config{Profiles: map[string]*Profile{"p": {Name: "p", Devices: []DeviceBinding{{
				Match: DeviceMatcher{Name: "kbd"}, Macros: []Macro{m},
			}}}}}
		}
		base := Macro{Name: "m", Trigger: []string{"KEY_A"}, Steps: []MacroStep{{Key: "KEY_B"}}}

		valid := []Macro{
			base, // RepeatNone
			{Name: "h", Trigger: []string{"KEY_A"}, Steps: base.Steps, Repeat: RepeatModeHold, RepeatMs: 50},
			{Name: "t", Trigger: []string{"KEY_A"}, Steps: base.Steps, Repeat: RepeatModeToggle, RepeatMs: 50},
			{Name: "c", Trigger: []string{"KEY_A"}, Steps: base.Steps, Repeat: RepeatModeCount, RepeatMs: 50, RepeatCount: 3},
		}
		for _, m := range valid {
			if errs := Validate(mk(m)); len(errs) != 0 {
				t.Errorf("repeat %q should be valid, got %v", m.Repeat, errs)
			}
		}

		invalid := []Macro{
			{Name: "x", Trigger: []string{"KEY_A"}, Steps: base.Steps, Repeat: "spin", RepeatMs: 50},          // unknown mode
			{Name: "x", Trigger: []string{"KEY_A"}, Steps: base.Steps, Repeat: RepeatModeHold},                // no interval
			{Name: "x", Trigger: []string{"KEY_A"}, Steps: base.Steps, Repeat: RepeatModeCount, RepeatMs: 50}, // no count
		}
		for _, m := range invalid {
			if errs := Validate(mk(m)); len(errs) == 0 {
				t.Errorf("repeat config %+v should be invalid", m)
			}
		}
	})
}

func TestBindingHasRules(t *testing.T) {
	cases := []struct {
		name string
		b    DeviceBinding
		want bool
	}{
		{"empty placeholder", DeviceBinding{Match: DeviceMatcher{Name: "kbd"}}, false},
		{"has a remap", DeviceBinding{Remaps: []Remap{{From: "KEY_A", To: "KEY_B"}}}, true},
		{"has a macro", DeviceBinding{Macros: []Macro{{Name: "m", Trigger: []string{"KEY_A"}}}}, true},
	}
	for _, c := range cases {
		if got := c.b.HasRules(); got != c.want {
			t.Errorf("%s: HasRules() = %v, want %v", c.name, got, c.want)
		}
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
