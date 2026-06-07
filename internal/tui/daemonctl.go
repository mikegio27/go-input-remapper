package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type (
	daemonStartedMsg struct {
		pid int
		err error
	}
	daemonStoppedMsg struct{ err error }
)

// startDaemon launches the daemon as a detached background process using the
// same binary and the TUI's config/socket paths. Its output goes to a log file
// so it never corrupts the TUI. This is convenient for desktop use; the systemd
// unit remains the recommended way to run it at boot.
func startDaemon(configDir, socketPath string) tea.Cmd {
	return func() tea.Msg {
		exe, err := os.Executable()
		if err != nil {
			return daemonStartedMsg{err: err}
		}
		logPath := filepath.Join(configDir, "daemon.log")
		_ = os.MkdirAll(configDir, 0o755)
		logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return daemonStartedMsg{err: err}
		}
		defer logFile.Close()

		cmd := exec.Command(exe, "daemon", "--config-dir", configDir, "--socket", socketPath)
		cmd.Stdout = logFile
		cmd.Stderr = logFile
		// New process group so it outlives the TUI and can be signalled as a group.
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := cmd.Start(); err != nil {
			return daemonStartedMsg{err: err}
		}
		// Give it a moment to bind the socket before we refresh status.
		time.Sleep(500 * time.Millisecond)
		return daemonStartedMsg{pid: cmd.Process.Pid}
	}
}

// stopDaemon signals a daemon previously started from the TUI to terminate.
func stopDaemon(pid int) tea.Cmd {
	return func() tea.Msg {
		if pid <= 0 {
			return daemonStoppedMsg{}
		}
		// Signal the whole process group (negative pid).
		err := syscall.Kill(-pid, syscall.SIGTERM)
		return daemonStoppedMsg{err: err}
	}
}
