// Package tui is the terminal front-end. It edits the TOML config files (the
// source of truth) and drives the daemon over the control socket for live state
// and learn-a-key capture. It never touches devices directly.
package tui

import (
	"strings"
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
	screenMappings
	screenProfiles
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
	// bodyW/bodyH are the frame region available to the active screen body,
	// computed in View between the header and footer so panels fill the window.
	bodyW, bodyH int

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

	// add-mapping wizard (nil unless active)
	addFlow *addFlowState

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
	if m.addFlow != nil {
		return m.addFlowKey(msg)
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

// View renders header, the active body filling the frame, and the footer.
func (m *Model) View() string {
	if m.quitting {
		return ""
	}
	// Overlays own the whole frame.
	if m.addFlow != nil {
		return m.addFlowOverlay()
	}
	if m.capture != nil {
		return m.captureOverlay()
	}

	header, footer := m.header(), m.footer()
	// Hand the body the space between header and footer so its panels can stretch
	// to the window edges instead of hugging their content at the top-left.
	m.bodyW = m.width
	if m.height > 0 {
		m.bodyH = max(m.height-lipgloss.Height(header)-lipgloss.Height(footer), panelChromeH+1)
	} else {
		m.bodyH = 0
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

	return lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
}

// paneWidths splits the body into a master/detail pair when a detail pane is
// wanted and there's room. right==0 means render a single full-width pane (no
// detail requested, the terminal is too narrow to split, or size isn't known).
func (m *Model) paneWidths(hasDetail bool) (left, right int) {
	if !hasDetail || m.bodyW <= 0 {
		return m.bodyW, 0
	}
	right = m.bodyW * 2 / 5
	switch {
	case right < 26:
		right = 26
	case right > 46:
		right = 46
	}
	left = m.bodyW - right - 1 // 1-col gap between the panes
	if left < 40 {
		return m.bodyW, 0 // too tight to split cleanly; give it all to the list
	}
	return left, right
}

// renderPanes lays out a master (and optional detail) panel filling the body. The
// master is always focused; the detail pane is informational. With no detail it
// fills the whole width; when the split would be too tight it stacks vertically
// rather than dropping the detail.
func (m *Model) renderPanes(lw, rw int, lTitle, lBody string, rTitle, rBody string) string {
	if rBody == "" {
		return fillPanel(lTitle, lBody, true, m.bodyW, m.bodyH)
	}
	if rw == 0 {
		// Stack: size the detail pane to its content and give the list the rest, so
		// the two together fill the body exactly. If the detail leaves no room for a
		// usable list, drop it and let the list fill the whole frame.
		bot := fillPanel(rTitle, rBody, false, m.bodyW, 0)
		topH := m.bodyH - lipgloss.Height(bot)
		if topH < panelChromeH+1 {
			return fillPanel(lTitle, lBody, true, m.bodyW, m.bodyH)
		}
		top := fillPanel(lTitle, lBody, true, m.bodyW, topH)
		return lipgloss.JoinVertical(lipgloss.Left, top, bot)
	}
	left := fillPanel(lTitle, lBody, true, lw, m.bodyH)
	right := fillPanel(rTitle, rBody, false, rw, m.bodyH)
	return lipgloss.JoinHorizontal(lipgloss.Top, left, " ", right)
}

// singleCentered renders one full-frame panel with its body centered — for empty
// states that would otherwise float in the top-left of a large panel.
func (m *Model) singleCentered(title, body string) string {
	placed := centerBody(body, panelInnerWidth(m.bodyW), panelInnerHeight(m.bodyH))
	return fillPanel(title, placed, true, m.bodyW, m.bodyH)
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
	case m.addFlow != nil:
		hints = "↑/↓ move · enter select · esc cancel"
	case m.addingProfile:
		hints = "type a profile name · enter create · esc cancel"
	default:
		switch m.screen {
		case screenDevices:
			toggle, toggleShort := "a show all", "a all"
			if m.showAll {
				toggle, toggleShort = "a remappable only", "a remap-only"
			}
			hints = m.hint(
				"↑/↓ move · enter remaps · m macros · "+toggle+" · tab switch · r refresh · q quit",
				"↑↓ · enter remap · m macro · "+toggleShort+" · ↹ tab · r · q")
		case screenProfiles:
			hints = m.hint(
				"↑/↓ move · enter activate · n new · d delete · tab switch · q quit",
				"↑↓ · enter activate · n new · d del · ↹ tab · q")
		case screenMappings:
			hints = m.hint(
				"↑/↓ move · enter edit · a add new · tab switch · r refresh · q quit",
				"↑↓ · enter edit · a add · ↹ tab · r · q")
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
	return lipgloss.JoinVertical(lipgloss.Left, m.statusStrip(), line)
}

// statusStrip is the always-on at-a-glance line above the footer hints: active
// profile, how many devices are bound, and the daemon's state. It grounds the
// layout and fills space with information rather than emptiness.
func (m *Model) statusStrip() string {
	profile := m.activeProfile
	if profile == "" {
		profile = "(none)"
	}
	parts := []string{
		"profile " + tabActiveStyle.Render(profile),
		itoa(len(m.engines)) + " bound",
		dot(m.daemonUp) + " daemon",
	}
	if m.daemonPID != 0 {
		parts = append(parts, dimStyle.Render("pid "+itoa(m.daemonPID)))
	}
	return stripStyle.Render(strings.Join(parts, dimStyle.Render(" · ")))
}

// hint returns full when it fits the terminal width (with a little slack for the
// footer's padding), else a compact variant, so the key-hint line never overflows
// a narrow window.
func (m *Model) hint(full, short string) string {
	if m.width > 0 && m.width < lipgloss.Width(full)+2 {
		return short
	}
	return full
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
