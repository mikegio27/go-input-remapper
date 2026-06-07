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
	var rows []string

	conn := errStyle.Render("not connected")
	if m.daemonUp {
		conn = goodStyle.Render("connected")
	}
	rows = append(rows, "daemon:  "+conn)
	rows = append(rows, "socket:  "+dimStyle.Render(m.opts.SocketPath))
	rows = append(rows, "config:  "+dimStyle.Render(m.opts.ConfigDir))

	profile := m.activeProfile
	if profile == "" {
		profile = "(none)"
	}
	rows = append(rows, "profile: "+profile)
	rows = append(rows, "")

	if !m.daemonUp {
		rows = append(rows, mutedStyle.Render("Start the daemon to see bound devices and enable live capture."))
		return panelStyle.Render(lipgloss.JoinVertical(lipgloss.Left, rows...))
	}

	if len(m.engines) == 0 {
		rows = append(rows, mutedStyle.Render("No devices currently bound."))
	} else {
		rows = append(rows, dimStyle.Render("bound devices:"))
		for _, e := range m.engines {
			rows = append(rows, "  "+e.Path+dimStyle.Render("  →  "+e.Name))
		}
	}
	return panelStyle.Render(lipgloss.JoinVertical(lipgloss.Left, rows...))
}
