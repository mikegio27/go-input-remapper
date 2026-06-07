// Package tui is the terminal front-end. It edits the TOML config files (the
// source of truth) and drives the daemon over the control socket for live state
// and learn-a-key capture. It never touches devices directly.
package tui

import (
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mikegio27/go-input-remapper/internal/config"
	"github.com/mikegio27/go-input-remapper/internal/control"
)

// pollInterval is how often the TUI re-checks daemon status so the connection
// indicator and bound-device list track reality even when the daemon is started
// or stopped outside the TUI (e.g. systemctl, a crash).
const pollInterval = 2 * time.Second

// tickMsg drives the periodic status poll.
type tickMsg struct{}

// tickCmd schedules the next status poll.
func tickCmd() tea.Cmd {
	return tea.Tick(pollInterval, func(time.Time) tea.Msg { return tickMsg{} })
}

// screen is the active top-level view.
type screen int

const (
	screenDevices screen = iota
	screenProfiles
	screenMappings
	screenStatus
)

// screenCount is the number of top-level screens, for tab cycling.
const screenCount = 4

var screenNames = map[screen]string{
	screenDevices:  "Devices",
	screenProfiles: "Profiles",
	screenMappings: "Mappings",
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

	// mappings screen
	mapCursor int

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
		tickCmd(),
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

	case tickMsg:
		// Background poll: keep the daemon indicator honest and re-read config from
		// disk so the Profiles list self-heals after a transient read failure, then
		// reschedule. Devices refresh on r / after actions.
		return m, tea.Batch(fetchStatus(m.opts.SocketPath), loadConfigQuiet(m.opts.ConfigDir), tickCmd())

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
		if msg.err != nil {
			// Keep the last good config; surface the error unless this was a
			// background refresh (which would otherwise flash every poll).
			if !msg.quiet {
				m.setFlash("config load failed: "+msg.err.Error(), true)
			}
			return m, nil
		}
		if msg.cfg != nil {
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
		// Flip the indicator immediately; re-dialing now would race the socket
		// close and could momentarily report "connected" again. The periodic poll
		// confirms shortly. Refresh devices so the bound list clears.
		m.daemonUp = false
		m.engines = nil
		if msg.err != nil {
			m.setFlash("stop: "+msg.err.Error(), true)
		} else {
			m.setFlash("daemon stopped", false)
		}
		return m, fetchDevices(m.opts.SocketPath)

	case captureStartedMsg:
		return m.onCaptureStarted(msg)
	case captureEventMsg:
		return m.onCaptureEvent(msg)
	case captureClosedMsg:
		// The session ended (peer/daemon side). Drop the overlay if still up and,
		// when it ended on an error, surface why instead of vanishing silently.
		if m.capture != nil {
			m.capture = nil
			if msg.err != nil {
				m.setFlash("capture failed: "+msg.err.Error(), true)
			}
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
	// A flash is a transient status line: clear it on the next keypress so it
	// doesn't ride along across tab switches and screens. Handlers below set a
	// fresh one when they have something to report.
	m.flash, m.flashErr = "", false

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
		m.screen = (m.screen + 1) % screenCount
		return m, nil
	case "shift+tab":
		m.screen = (m.screen + screenCount - 1) % screenCount
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
	case screenMappings:
		return m.mappingsKey(msg)
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
		case screenMappings:
			body = m.mappingsView()
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
		// "c capture key" only applies while adding a remap row.
		if m.editor.adding {
			hints = "tab switch field · c capture key · enter add · esc cancel"
		} else {
			hints = "↑/↓ move · a add · d delete · s save · esc back"
		}
	case m.macro != nil:
		switch m.macro.stage {
		case macroStageName:
			hints = "type a name · enter continue · esc cancel"
		case macroStageTrigger:
			hints = "press the trigger chord… · esc cancel"
		case macroStageSteps:
			hints = "k add key · enter finish macro · esc cancel"
		case macroStageRepeat:
			hints = "m change repeat mode · enter finish macro · esc cancel"
		default:
			hints = "↑/↓ move · n new · x delete · s save · esc back"
		}
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
		case screenMappings:
			hints = "↑/↓ move · enter edit · tab switch · r refresh · q quit"
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
