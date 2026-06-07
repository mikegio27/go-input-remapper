package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func (m *Model) statusKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "d":
		if m.daemonUp {
			m.setFlash("daemon already running", false)
			return m, nil
		}
		m.setFlash("starting daemon…", false)
		return m, startDaemon(m.opts.ConfigDir, m.opts.SocketPath)
	case "k":
		if m.daemonPID == 0 {
			m.setFlash("no TUI-started daemon to stop (use systemctl for the service)", true)
			return m, nil
		}
		return m, stopDaemon(m.daemonPID)
	}
	return m, nil
}

func (m *Model) statusView() string {
	conn := errStyle.Render("not connected")
	if m.daemonUp {
		conn = goodStyle.Render("connected")
	}
	profile := m.activeProfile
	if profile == "" {
		profile = "(none)"
	}
	info := lipgloss.JoinVertical(lipgloss.Left,
		"daemon:  "+conn,
		"socket:  "+dimStyle.Render(m.opts.SocketPath),
		"config:  "+dimStyle.Render(m.opts.ConfigDir),
		"profile: "+profile,
	)
	daemonPanel := panel("Daemon", info, true)

	var body string
	switch {
	case !m.daemonUp:
		body = mutedStyle.Render("Start the daemon to see bound\ndevices and enable live capture.")
	case len(m.engines) == 0:
		body = mutedStyle.Render("No devices currently bound.")
	default:
		var rows []string
		for _, e := range m.engines {
			rows = append(rows, e.Path+dimStyle.Render("  →  "+e.Name))
		}
		body = lipgloss.JoinVertical(lipgloss.Left, rows...)
	}
	return joinPanels(m.width, daemonPanel, panel("Bound devices", body, false))
}
