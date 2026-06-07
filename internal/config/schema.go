// Package config is the on-disk source of truth: the TOML schema the daemon
// reads and the TUI writes, plus loading, saving, validation, and change
// watching. It models intent only — it does not open or grab devices.
//
// Layout on disk (under the config directory):
//
//	config.toml            global settings (active profile, virtual-device prefix)
//	profiles/<name>.toml   one profile per file (device bindings, remaps, macros)
//
// Splitting profiles into separate files lets the TUI rewrite a single profile
// without disturbing the others or the user's hand-edits elsewhere.
package config

import (
	"fmt"
	"strconv"
)

// Config is the global configuration from config.toml. Profiles is populated by
// Load from the profiles/ directory and is not itself serialized into
// config.toml (each profile lives in its own file).
type Config struct {
	ActiveProfile string `toml:"active_profile"`
	VirtualPrefix string `toml:"virtual_prefix"`

	Profiles map[string]*Profile `toml:"-"`
}

// Profile is a named set of per-device bindings, switched as a unit.
type Profile struct {
	Name    string          `toml:"name"`
	Devices []DeviceBinding `toml:"device"`
}

// DeviceBinding ties a device matcher to the remaps and macros applied to every
// device it resolves to.
type DeviceBinding struct {
	Match  DeviceMatcher `toml:"match"`
	Remaps []Remap       `toml:"remap"`
	Macros []Macro       `toml:"macro"`
}

// DeviceMatcher selects physical devices by stable attributes rather than the
// volatile /dev/input/eventX path. Empty fields are ignored. Match precedence
// (strongest first) is applied by the resolver: Uniq, then Vendor+Product+Name,
// then Phys.
type DeviceMatcher struct {
	Name    string `toml:"name,omitempty"`
	Vendor  HexU16 `toml:"vendor,omitempty"`
	Product HexU16 `toml:"product,omitempty"`
	Uniq    string `toml:"uniq,omitempty"`
	Phys    string `toml:"phys,omitempty"`
}

// IsEmpty reports whether the matcher has no criteria at all (which can never
// match a device and is treated as a configuration error).
func (m DeviceMatcher) IsEmpty() bool {
	return m.Name == "" && m.Vendor == 0 && m.Product == 0 && m.Uniq == "" && m.Phys == ""
}

// Remap rewrites one key/button code to another. To empty means suppress the
// input entirely (drop it).
type Remap struct {
	From string `toml:"from"`
	To   string `toml:"to"`
}

// Suppresses reports whether the remap drops its input rather than rewriting it.
func (r Remap) Suppresses() bool { return r.To == "" }

// Macro fires a sequence of steps when its trigger chord is pressed. Trigger is
// the set of keys that must be held together (length 1 is a single key).
type Macro struct {
	Name    string      `toml:"name"`
	Trigger []string    `toml:"trigger"`
	Steps   []MacroStep `toml:"step"`
}

// MacroStep is one action in a macro. Exactly one of Key or Text is the payload
// (a delay-only step sets neither and just pauses). For a Key step: by default
// it taps (down+up); Hold presses and keeps the key down; Release releases a
// previously held key. DelayMs pauses before the step runs.
type MacroStep struct {
	Key     string `toml:"key,omitempty"`
	Text    string `toml:"text,omitempty"`
	Hold    bool   `toml:"hold,omitempty"`
	Release bool   `toml:"release,omitempty"`
	DelayMs int    `toml:"delay_ms,omitempty"`
}

// HexU16 is a 16-bit USB vendor/product id rendered in TOML as a lowercase hex
// string (e.g. "046d") so config files line up with lsusb output. The zero value
// marshals as absent under omitempty and means "unset" in a matcher.
type HexU16 uint16

// MarshalText renders the id as 4-digit lowercase hex.
func (h HexU16) MarshalText() ([]byte, error) {
	return fmt.Appendf(nil, "%04x", uint16(h)), nil
}

// UnmarshalText parses a hex id, tolerating an optional "0x" prefix.
func (h *HexU16) UnmarshalText(text []byte) error {
	s := string(text)
	if len(s) >= 2 && (s[:2] == "0x" || s[:2] == "0X") {
		s = s[2:]
	}
	v, err := strconv.ParseUint(s, 16, 16)
	if err != nil {
		return fmt.Errorf("invalid hex id %q: %w", string(text), err)
	}
	*h = HexU16(v)
	return nil
}
