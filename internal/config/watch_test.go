package config

import (
	"path/filepath"
	"testing"
	"time"
)

func TestRelevantConfigChange(t *testing.T) {
	dir := "/etc/go-input-remapper"
	profDir := filepath.Join(dir, "profiles")
	cases := []struct {
		name string
		want bool
	}{
		{filepath.Join(dir, "config.toml"), true},
		{filepath.Join(profDir, "default.toml"), true},
		{filepath.Join(profDir, "wow.toml"), true},
		{filepath.Join(dir, "daemon.log"), false},         // the feedback-loop culprit
		{filepath.Join(dir, "config.toml.tmp123"), false}, // atomic-write temp
		{filepath.Join(profDir, "wow.toml.swp"), false},   // editor swap
		{filepath.Join(dir, "profiles"), false},           // the dir itself
	}
	for _, c := range cases {
		if got := relevantConfigChange(dir, profDir, c.name); got != c.want {
			t.Errorf("relevantConfigChange(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestWatcherFiresOnChange(t *testing.T) {
	dir := t.TempDir()
	w, err := NewWatcher(dir)
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	defer w.Close()

	// Writing a profile should produce exactly one debounced event.
	if err := SaveProfile(dir, "default", &Profile{Name: "default"}); err != nil {
		t.Fatalf("SaveProfile: %v", err)
	}

	select {
	case <-w.Events():
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for change event")
	}
}

func TestWatcherDebouncesBurst(t *testing.T) {
	dir := t.TempDir()
	w, err := NewWatcher(dir)
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	defer w.Close()

	// A burst of writes within the debounce window should coalesce.
	for i := 0; i < 5; i++ {
		if err := SaveGlobal(dir, &Config{ActiveProfile: "x"}); err != nil {
			t.Fatalf("SaveGlobal: %v", err)
		}
	}

	select {
	case <-w.Events():
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for coalesced event")
	}

	// No second event should be pending shortly after.
	select {
	case <-w.Events():
		t.Error("expected burst to coalesce into a single event")
	case <-time.After(debounceInterval * 2):
	}
}
