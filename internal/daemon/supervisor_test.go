package daemon

import (
	"testing"

	"github.com/mikegio27/nereus/internal/config"
)

// TestReloadSuppressedAfterApply verifies that applying a config arms the
// short window during which the file watcher's echo of that same change is
// ignored — preventing the redundant second apply that tore down every engine.
func TestReloadSuppressedAfterApply(t *testing.T) {
	sup := NewSupervisor()
	if sup.ReloadSuppressed() {
		t.Fatal("a fresh supervisor should not suppress reloads")
	}

	// Apply with no devices/profile still arms the suppression window (the config
	// write that triggered it will echo through the watcher regardless).
	sup.Apply(&config.Config{}, &config.Profile{Name: "p"})
	if !sup.ReloadSuppressed() {
		t.Fatal("expected reloads to be suppressed immediately after Apply")
	}
}
