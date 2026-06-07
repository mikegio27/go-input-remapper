package tui

import (
	"errors"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mikegio27/go-input-remapper/internal/control"
)

// captureSession holds the dedicated client and stream for an active capture.
type captureSession struct {
	client *control.Client
	events <-chan control.CaptureEvent
	stop   func()
}

// close ends the capture and releases the dedicated connection.
func (s *captureSession) close() {
	if s == nil {
		return
	}
	s.stop()
	s.client.Close()
}

type (
	captureStartedMsg struct {
		session *captureSession
		err     error
	}
	captureEventMsg  struct{ ev control.CaptureEvent }
	captureClosedMsg struct{}
)

// startCapture opens a dedicated connection and begins a capture stream. Capture
// needs its own connection because the stream holds the socket for its duration.
func startCapture(sock, devicePath, mode string) tea.Cmd {
	return func() tea.Msg {
		c, err := control.Dial(sock)
		if err != nil {
			return captureStartedMsg{err: err}
		}
		events, stop, err := c.Capture(control.CaptureParams{DevicePath: devicePath, Mode: mode})
		if err != nil {
			c.Close()
			return captureStartedMsg{err: err}
		}
		return captureStartedMsg{session: &captureSession{client: c, events: events, stop: stop}}
	}
}

// waitCapture blocks for the next captured event (or stream close) and reports it
// back into the update loop. Re-issue it after each event to keep reading.
func waitCapture(s *captureSession) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-s.events
		if !ok {
			return captureClosedMsg{}
		}
		return captureEventMsg{ev: ev}
	}
}

// errFromStrings joins server-reported error strings into one error.
func errFromStrings(msgs []string) error {
	if len(msgs) == 0 {
		return errors.New("unknown error")
	}
	return errors.New(strings.Join(msgs, "; "))
}
