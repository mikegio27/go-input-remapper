package config

import (
	"testing"
	"time"
)

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
