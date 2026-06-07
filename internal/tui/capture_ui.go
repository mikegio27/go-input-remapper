package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// capturePurpose tells the update loop where to deliver a finished capture.
type capturePurpose int

const (
	purposeRemapFrom capturePurpose = iota
	purposeRemapTo
	purposeMacroTrigger
	purposeMacroStep
)

// captureState is the modal learn-a-key overlay. mode is "key" (finish on first
// key-down) or "chord" (finish when all keys release, taking the largest held set).
type captureState struct {
	session  *captureSession
	mode     string
	purpose  capturePurpose
	prompt   string
	live     []string // currently held (chord mode)
	maxChord []string // largest held set seen (chord result)
	result   []string
}

// beginCapture starts a capture session for a device, showing the overlay. If the
// daemon is unreachable, it flashes an error instead.
func (m *Model) beginCapture(devicePath, mode string, purpose capturePurpose, prompt string) (tea.Model, tea.Cmd) {
	m.capture = &captureState{mode: mode, purpose: purpose, prompt: prompt}
	return m, startCapture(m.opts.SocketPath, devicePath, mode)
}

func (m *Model) onCaptureStarted(msg captureStartedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.capture = nil
		m.setFlash("capture unavailable: "+msg.err.Error(), true)
		return m, nil
	}
	if m.capture == nil { // cancelled before it started
		msg.session.close()
		return m, nil
	}
	m.capture.session = msg.session
	return m, waitCapture(msg.session)
}

func (m *Model) onCaptureEvent(msg captureEventMsg) (tea.Model, tea.Cmd) {
	if m.capture == nil {
		return m, nil
	}
	ev := msg.ev
	switch m.capture.mode {
	case "key":
		if ev.Value == 1 { // key down completes a single-key capture
			m.capture.result = []string{ev.KeyName}
			return m.finishCapture()
		}
	case "chord":
		if len(ev.Chord) > len(m.capture.maxChord) {
			m.capture.maxChord = ev.Chord
		}
		m.capture.live = ev.Chord
		if len(ev.Chord) == 0 && len(m.capture.maxChord) > 0 {
			m.capture.result = m.capture.maxChord
			return m.finishCapture()
		}
	}
	return m, waitCapture(m.capture.session)
}

// captureKey lets the user cancel the overlay with Esc.
func (m *Model) captureKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "esc" {
		if m.capture.session != nil {
			m.capture.session.close()
		}
		m.capture = nil
		m.setFlash("capture cancelled", false)
		return m, nil
	}
	return m, nil
}

// finishCapture closes the session and routes the captured result to whatever
// requested it.
func (m *Model) finishCapture() (tea.Model, tea.Cmd) {
	cap := m.capture
	m.capture = nil
	if cap.session != nil {
		cap.session.close()
	}
	switch cap.purpose {
	case purposeRemapFrom, purposeRemapTo:
		return m.editorCaptured(cap.purpose, cap.result)
	case purposeMacroTrigger, purposeMacroStep:
		return m.macroCaptured(cap.purpose, cap.result)
	}
	return m, nil
}

func (m *Model) captureOverlay() string {
	c := m.capture
	lines := []string{warnStyle.Render(c.prompt), ""}
	switch c.mode {
	case "chord":
		live := strings.Join(c.live, " + ")
		if live == "" {
			live = dimStyle.Render("(press and hold the keys, then release)")
		}
		lines = append(lines, "chord: "+tabActiveStyle.Render(live))
	default:
		lines = append(lines, dimStyle.Render("waiting for a key…"))
	}
	lines = append(lines, "", dimStyle.Render("esc to cancel"))
	box := overlayStyle.Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
	if m.width == 0 || m.height == 0 {
		return box
	}
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}
