package config

import (
	"strings"

	evdev "github.com/mikegio27/go-evdev"
)

// LookupKey resolves a key/button name (e.g. "KEY_ESC", "BTN_LEFT") to its
// EvCode. It only accepts names in the EV_KEY namespace — the codes a remap or
// macro can press — so relative/absolute axis names are rejected even though
// evdev knows them.
func LookupKey(name string) (evdev.EvCode, bool) {
	if !IsKeyName(name) {
		return 0, false
	}
	return evdev.EvCodeByName(name)
}

// IsKeyName reports whether name is a syntactically valid EV_KEY code name that
// evdev recognizes. KEY_* are keyboard keys; BTN_* are buttons (mouse, gamepad).
func IsKeyName(name string) bool {
	if !strings.HasPrefix(name, "KEY_") && !strings.HasPrefix(name, "BTN_") {
		return false
	}
	_, ok := evdev.EvCodeByName(name)
	return ok
}
