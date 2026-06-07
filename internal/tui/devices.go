package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mikegio27/go-input-remapper/internal/control"
)

func (m *Model) devicesKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	vis := m.visibleDevices()
	switch msg.String() {
	case "up", "k":
		if m.devCursor > 0 {
			m.devCursor--
		}
	case "down", "j":
		if m.devCursor < len(vis)-1 {
			m.devCursor++
		}
	case "a":
		m.showAll = !m.showAll
		m.devCursor = 0
	case "enter":
		if d, ok := m.selectedDevice(); ok {
			return m.openEditor(d)
		}
	case "m":
		if d, ok := m.selectedDevice(); ok {
			return m.openMacroRecorder(d)
		}
	}
	return m, nil
}

// visibleDevices returns the devices shown on the list: only remappable
// (keyboard/mouse/gamepad) ones by default, or everything when showAll is set.
func (m *Model) visibleDevices() []control.DeviceInfo {
	if m.showAll {
		return m.devices
	}
	var out []control.DeviceInfo
	for _, d := range m.devices {
		if d.Recommended && !d.IsVirtual {
			out = append(out, d)
		}
	}
	return out
}

func (m *Model) selectedDevice() (control.DeviceInfo, bool) {
	vis := m.visibleDevices()
	if m.devCursor < 0 || m.devCursor >= len(vis) {
		return control.DeviceInfo{}, false
	}
	return vis[m.devCursor], true
}

func (m *Model) devicesView() string {
	if len(m.devices) == 0 {
		return panel("Devices", mutedStyle.Render(
			"No readable input devices.\nStart the daemon or run with more privileges (input group / root)."), true)
	}

	vis := m.visibleDevices()
	if len(vis) == 0 {
		return panel("Devices", mutedStyle.Render(
			"No remappable devices (keyboards/mice/gamepads) detected.\nPress 'a' to show all input devices."), true)
	}

	var rows []string
	scope := "remappable devices"
	if m.showAll {
		scope = "all devices"
	}
	rows = append(rows, dimStyle.Render(scope))
	header := fmt.Sprintf("  %-18s %-9s %-9s %-7s %s", "PATH", "KIND", "VND:PRD", "STATUS", "NAME")
	rows = append(rows, dimStyle.Render(header))

	for i, d := range vis {
		cursor := "  "
		if i == m.devCursor {
			cursor = cursorRowStyle.Render("▶ ")
		}
		status := deviceStatus(d)
		name := d.Name
		line := fmt.Sprintf("%-18s %-9s %04x:%04x  %-7s %s",
			truncate(d.Path, 18), d.Kind, d.Vendor, d.Product, status, name)

		styled := line
		switch {
		case d.IsVirtual:
			styled = dimStyle.Render(line + "  (virtual)")
		case i == m.devCursor:
			styled = cursorRowStyle.Render(line)
		}
		if d.Primary {
			styled += "  " + goodStyle.Render("★ likely")
		}
		rows = append(rows, cursor+styled)
	}
	if !m.devFromDaemon {
		rows = append(rows, "", dimStyle.Render("(daemon down — direct enumeration; STATUS unavailable)"))
	}
	list := panel("Devices", lipgloss.JoinVertical(lipgloss.Left, rows...), true)

	// Detail panel for the selected device: recommendation reasons.
	detail := ""
	if d, ok := m.selectedDevice(); ok && len(d.Reasons) > 0 {
		var rs []string
		verdict := warnStyle.Render("not recommended")
		if d.Recommended {
			verdict = goodStyle.Render("remappable")
		}
		rs = append(rs, verdict)
		for _, r := range d.Reasons {
			rs = append(rs, dimStyle.Render("· "+r))
		}
		detail = panel("Details", lipgloss.JoinVertical(lipgloss.Left, rs...), false)
	}

	return joinPanels(m.width, list, detail)
}

func deviceStatus(d control.DeviceInfo) string {
	switch {
	case d.IsVirtual:
		return "-"
	case d.Bound:
		return "bound"
	case d.Recommended:
		return "ok"
	default:
		return ""
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}
