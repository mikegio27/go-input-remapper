package config

import (
	"fmt"
	"sort"
	"strings"
)

// Validate checks a loaded config for problems that would make the daemon
// misbehave: an active profile that doesn't exist, empty matchers, unknown key
// names, conflicting remaps, and malformed macros. It returns all problems found
// (not just the first) so the TUI/CLI can show a complete list. A nil/empty
// result means the config is valid.
func Validate(cfg *Config) []error {
	var errs []error
	add := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf(format, args...))
	}

	if cfg.ActiveProfile != "" {
		if _, ok := cfg.Profiles[cfg.ActiveProfile]; !ok {
			add("active_profile %q does not exist", cfg.ActiveProfile)
		}
	}

	// Iterate profiles in a stable order so error output is deterministic.
	keys := make([]string, 0, len(cfg.Profiles))
	for k := range cfg.Profiles {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, key := range keys {
		p := cfg.Profiles[key]
		for i, b := range p.Devices {
			where := fmt.Sprintf("profile %q device[%d]", key, i)
			validateBinding(where, b, add)
		}
	}
	return errs
}

func validateBinding(where string, b DeviceBinding, add func(string, ...any)) {
	if b.Match.IsEmpty() {
		add("%s: match has no criteria (it would never match a device)", where)
	}

	seenFrom := map[string]bool{}
	for j, r := range b.Remaps {
		if !IsKeyName(r.From) {
			add("%s: remap[%d]: unknown key name %q in 'from'", where, j, r.From)
		} else if seenFrom[r.From] {
			add("%s: remap[%d]: duplicate 'from' key %q", where, j, r.From)
		}
		seenFrom[r.From] = true
		if r.To != "" && !IsKeyName(r.To) {
			add("%s: remap[%d]: unknown key name %q in 'to'", where, j, r.To)
		}
	}

	seenTrigger := map[string]bool{}
	for j, m := range b.Macros {
		mwhere := fmt.Sprintf("%s: macro[%d]", where, j)
		if m.Name == "" {
			add("%s: missing name", mwhere)
		}
		if len(m.Trigger) == 0 {
			add("%s: trigger is empty", mwhere)
		}
		for _, key := range m.Trigger {
			if !IsKeyName(key) {
				add("%s: unknown key name %q in trigger", mwhere, key)
			}
		}
		if sig := triggerSignature(m.Trigger); sig != "" {
			if seenTrigger[sig] {
				add("%s: duplicate trigger %v", mwhere, m.Trigger)
			}
			seenTrigger[sig] = true
		}
		for k, s := range m.Steps {
			validateStep(fmt.Sprintf("%s step[%d]", mwhere, k), s, add)
		}
	}
}

func validateStep(where string, s MacroStep, add func(string, ...any)) {
	switch {
	case s.Key != "" && s.Text != "":
		add("%s: set either 'key' or 'text', not both", where)
	case s.Key != "":
		if !IsKeyName(s.Key) {
			add("%s: unknown key name %q", where, s.Key)
		}
		if s.Hold && s.Release {
			add("%s: 'hold' and 'release' are mutually exclusive", where)
		}
	case s.Text != "":
		if s.Hold || s.Release {
			add("%s: 'hold'/'release' apply to 'key' steps, not 'text'", where)
		}
	default:
		// Neither key nor text: only valid as a pure delay.
		if s.DelayMs <= 0 {
			add("%s: empty step (set 'key', 'text', or a positive 'delay_ms')", where)
		}
		if s.Hold || s.Release {
			add("%s: 'hold'/'release' require a 'key'", where)
		}
	}
	if s.DelayMs < 0 {
		add("%s: negative delay_ms", where)
	}
}

// triggerSignature builds an order-independent key for a trigger chord so that
// ["KEY_LEFTCTRL","KEY_J"] and ["KEY_J","KEY_LEFTCTRL"] count as the same chord.
func triggerSignature(trigger []string) string {
	if len(trigger) == 0 {
		return ""
	}
	keys := append([]string(nil), trigger...)
	sort.Strings(keys)
	return strings.Join(keys, "+")
}
