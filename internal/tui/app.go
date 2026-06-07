// Package tui is the terminal front-end. It edits the TOML config files (the
// source of truth) and drives the daemon over the control socket for live state
// and learn-a-key capture. It never touches devices directly.
package tui

import (
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mikegio27/go-input-remapper/internal/config"
	"github.com/mikegio27/go-input-remapper/internal/control"
)

// screen is the active top-level view.
type screen int

const (
	screenDevices screen = iota
	screenProfiles
	screenStatus
)

var screenNames = map[screen]string{
	screenDevices:  "Devices",
	screenProfiles: "Profiles",
	screenStatus:   "Status",
}

// Options configures the TUI.
type Options struct {
	ConfigDir  string
	SocketPath string
}

// Model is the root Bubble Tea model. It holds all screen state; per-screen logic
// lives in the other files as methods on *Model.
type Model struct {
	opts          Options
	screen        screen
	width, height int

	// daemon-derived state
	daemonUp      bool
	activeProfile string
	engines       []control.EngineInfo

	// devices screen
	devices       []control.DeviceInfo
	devCursor     int
	devFromDaemon bool
	showAll       bool // include non-remappable (unknown/touchpad/virtual) devices

	// profiles screen
	cfg           *config.Config
	profileNames  []string
	profCursor    int
	addingProfile bool
	profileInput  textinput.Model

	// sub-screens (nil unless active)
	editor *editorState
	macro  *macroState

	// capture overlay (nil unless capturing)
	capture *captureState

	daemonPID int // pid of a daemon we started, for stopping it (0 = none)

	refreshing bool
	flash      string // transient status line
	flashErr   bool
	quitting   bool
}

// Run starts the TUI event loop.
func Run(opts Options) error {
	m := &Model{opts: opts}
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

// Init kicks off the initial data fetches.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(
		fetchStatus(m.opts.SocketPath),
		fetchDevices(m.opts.SocketPath),
		loadConfig(m.opts.ConfigDir),
	)
}

// Update is the central dispatcher: window sizing, async results, the capture
// overlay (which intercepts input while active), sub-screens, then the active
// top-level screen.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case devicesMsg:
		m.devices = msg.devices
		m.devFromDaemon = msg.fromDaemon
		if m.devCursor >= len(m.visibleDevices()) {
			m.devCursor = max(0, len(m.visibleDevices())-1)
		}
		if m.refreshing {
			m.refreshing = false
			m.setFlash("refreshed", false)
		}
		return m, nil

	case statusMsg:
		m.daemonUp = msg.up
		if msg.up {
			m.activeProfile = msg.status.ActiveProfile
			m.engines = msg.status.Engines
		}
		return m, nil

	case configMsg:
		if msg.err == nil && msg.cfg != nil {
			m.cfg = msg.cfg
			m.refreshProfileNames()
			if m.activeProfile == "" {
				m.activeProfile = msg.cfg.ActiveProfile
			}
		}
		return m, nil

	case actionMsg:
		if msg.err != nil {
			m.setFlash(msg.err.Error(), true)
		} else {
			m.setFlash(msg.text, false)
		}
		// Refresh derived state after any mutation.
		return m, tea.Batch(fetchStatus(m.opts.SocketPath), fetchDevices(m.opts.SocketPath), loadConfig(m.opts.ConfigDir))

	case daemonStartedMsg:
		if msg.err != nil {
			m.setFlash("could not start daemon: "+msg.err.Error(), true)
			return m, nil
		}
		m.daemonPID = msg.pid
		m.setFlash("daemon started (pid "+itoa(msg.pid)+")", false)
		return m, tea.Batch(fetchStatus(m.opts.SocketPath), fetchDevices(m.opts.SocketPath))

	case daemonStoppedMsg:
		m.daemonPID = 0
		if msg.err != nil {
			m.setFlash("stop: "+msg.err.Error(), true)
		} else {
			m.setFlash("daemon stopped", false)
		}
		return m, tea.Batch(fetchStatus(m.opts.SocketPath), fetchDevices(m.opts.SocketPath))

	case captureStartedMsg:
		return m.onCaptureStarted(msg)
	case captureEventMsg:
		return m.onCaptureEvent(msg)
	case captureClosedMsg:
		// The session ended (peer/daemon side). Drop the overlay if still up.
		if m.capture != nil {
			m.capture = nil
		}
		return m, nil

	case tea.KeyMsg:
		return m.onKey(msg)
	}
	return m, nil
}

// onKey routes key presses: capture overlay first (modal), then sub-screens,
// then global keys, then the active screen.
func (m *Model) onKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.capture != nil {
		return m.captureKey(msg)
	}
	if m.editor != nil {
		return m.editorKey(msg)
	}
	if m.macro != nil {
		return m.macroKey(msg)
	}
	if m.addingProfile {
		return m.profileInputKey(msg)
	}

	switch msg.String() {
	case "ctrl+c", "q":
		m.quitting = true
		return m, tea.Quit
	case "tab":
		m.screen = (m.screen + 1) % 3
		return m, nil
	case "shift+tab":
		m.screen = (m.screen + 2) % 3
		return m, nil
	case "r":
		m.refreshing = true
		m.setFlash("refreshing…", false)
		return m, tea.Batch(fetchStatus(m.opts.SocketPath), fetchDevices(m.opts.SocketPath), loadConfig(m.opts.ConfigDir))
	}

	switch m.screen {
	case screenDevices:
		return m.devicesKey(msg)
	case screenProfiles:
		return m.profilesKey(msg)
	case screenStatus:
		return m.statusKey(msg)
	}
	return m, nil
}

// View renders header, the active body, and the footer.
func (m *Model) View() string {
	if m.quitting {
		return ""
	}
	body := ""
	switch {
	case m.editor != nil:
		body = m.editorView()
	case m.macro != nil:
		body = m.macroView()
	default:
		switch m.screen {
		case screenDevices:
			body = m.devicesView()
		case screenProfiles:
			body = m.profilesView()
		case screenStatus:
			body = m.statusView()
		}
	}

	view := lipgloss.JoinVertical(lipgloss.Left, m.header(), body, m.footer())
	if m.capture != nil {
		view = m.captureOverlay() // overlay replaces the frame while capturing
	}
	return view
}

// header renders the title bar, active profile, daemon indicator, and tabs.
func (m *Model) header() string {
	profile := m.activeProfile
	if profile == "" {
		profile = "(none)"
	}
	left := titleStyle.Render("go-input-remapper")
	info := headerBarStyle.Render("profile: " + tabActiveStyle.Render(profile) + "   " + dot(m.daemonUp) + " daemon")

	var tabs []string
	for s := screenDevices; s <= screenStatus; s++ {
		name := screenNames[s]
		if s == m.screen && m.editor == nil && m.macro == nil {
			tabs = append(tabs, tabActiveStyle.Render("["+name+"]"))
		} else {
			tabs = append(tabs, tabInactive.Render(" "+name+" "))
		}
	}
	tabbar := headerBarStyle.Render(lipgloss.JoinHorizontal(lipgloss.Top, tabs...))
	return lipgloss.JoinVertical(lipgloss.Left, lipgloss.JoinHorizontal(lipgloss.Top, left, "  ", info), tabbar)
}

// footer renders the contextual key hints and any flash message.
func (m *Model) footer() string {
	var hints string
	switch {
	case m.editor != nil:
		hints = "a add · c capture key · d delete · s save · esc back"
	case m.macro != nil:
		hints = "t trigger · k add key · x delete · s save · esc back"
	case m.addingProfile:
		hints = "type a profile name · enter create · esc cancel"
	default:
		switch m.screen {
		case screenDevices:
			toggle := "a show all"
			if m.showAll {
				toggle = "a remappable only"
			}
			hints = "↑/↓ move · enter remaps · m macros · " + toggle + " · tab switch · r refresh · q quit"
		case screenProfiles:
			hints = "↑/↓ move · enter activate · n new · d delete · tab switch · q quit"
		case screenStatus:
			if m.daemonUp {
				hints = "k stop daemon · tab switch · r refresh · q quit"
			} else {
				hints = "d start daemon · tab switch · r refresh · q quit"
			}
		}
	}
	line := footerStyle.Render(hints)
	if m.flash != "" {
		style := goodStyle
		if m.flashErr {
			style = errStyle
		}
		line = lipgloss.JoinVertical(lipgloss.Left, footerStyle.Render(style.Render(m.flash)), line)
	}
	return line
}

func (m *Model) setFlash(text string, isErr bool) {
	m.flash = text
	m.flashErr = isErr
}

func (m *Model) refreshProfileNames() {
	m.profileNames = m.profileNames[:0]
	if m.cfg == nil {
		return
	}
	for name := range m.cfg.Profiles {
		m.profileNames = append(m.profileNames, name)
	}
	sortStrings(m.profileNames)
	if m.profCursor >= len(m.profileNames) {
		m.profCursor = max(0, len(m.profileNames)-1)
	}
}
