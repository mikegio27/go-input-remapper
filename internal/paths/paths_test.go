package paths

import (
	"os"
	"path/filepath"
	"testing"
)

func TestClientSocketPath(t *testing.T) {
	run := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", run)
	userSock := filepath.Join(run, appName+".sock")

	t.Run("explicit override wins", func(t *testing.T) {
		if got := ClientSocketPath("/tmp/x.sock"); got != "/tmp/x.sock" {
			t.Errorf("got %q, want the override", got)
		}
	})

	t.Run("prefers an existing per-user socket", func(t *testing.T) {
		f, err := os.Create(userSock)
		if err != nil {
			t.Fatal(err)
		}
		f.Close()
		defer os.Remove(userSock)
		if got := ClientSocketPath(""); got != userSock {
			t.Errorf("got %q, want the user socket %q", got, userSock)
		}
	})

	t.Run("falls back to the user default when nothing exists", func(t *testing.T) {
		// No per-user socket and (on a dev box) no /run system socket: returns the
		// per-user default rather than an empty string.
		if _, err := os.Stat(systemSocket()); err == nil {
			t.Skip("system socket exists on this host; fallback branch not exercised")
		}
		if got := ClientSocketPath(""); got != userSock {
			t.Errorf("got %q, want the user default %q", got, userSock)
		}
	})
}
