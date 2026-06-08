package tui

import (
	"fmt"
	"strings"

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
		return m.singleCentered("Devices", mutedStyle.Render(
			"No readable input devices.\nStart the daemon or run with more privileges (input group / root)."))
	}

	vis := m.visibleDevices()
	if len(vis) == 0 {
		return m.singleCentered("Devices", mutedStyle.Render(
			"No remappable devices (keyboards/mice/gamepads) detected.\nPress 'a' to show all input devices."))
	}

	sel, hasSel := m.selectedDevice()
	hasDetail := hasSel && len(sel.Reasons) > 0
	lw, rw := m.paneWidths(hasDetail)
	listOuter := m.bodyW
	if rw > 0 {
		listOuter = lw
	}
	innerW := panelInnerWidth(listOuter)

	// Pick a column tier by the available width so the table degrades gracefully
	// on narrow terminals instead of overflowing. avail excludes the 2-col cursor.
	avail := innerW - 2
	tier := 0 // 0 full · 1 drop VND:PRD · 2 compact (basename node, no status)
	var headerCols string
	switch {
	case avail >= 74:
		headerCols = fmt.Sprintf("%-18s %-9s %-9s %-7s %s", "PATH", "KIND", "VND:PRD", "STATUS", "NAME")
	case avail >= 54:
		tier = 1
		headerCols = fmt.Sprintf("%-18s %-9s %-7s %s", "PATH", "KIND", "STATUS", "NAME")
	default:
		tier = 2
		headerCols = fmt.Sprintf("%-11s %-9s %s", "NODE", "KIND", "NAME")
	}

	var rows []string
	scope := "remappable devices"
	if m.showAll {
		scope = "all devices"
	}
	rows = append(rows, dimStyle.Render(scope))
	rows = append(rows, dimStyle.Render("  "+headerCols))

	for i, d := range vis {
		suffix := ""
		switch {
		case d.Primary:
			suffix = "  ★ likely"
		case d.IsVirtual && tier < 2:
			suffix = "  (virtual)"
		}
		var prefixCols string
		var nameBudget int
		switch tier {
		case 0:
			prefixCols = fmt.Sprintf("%-18s %-9s %04x:%04x  %-7s ",
				truncate(d.Path, 18), truncate(d.Kind, 9), d.Vendor, d.Product, deviceStatus(d))
			nameBudget = avail - 48
		case 1:
			prefixCols = fmt.Sprintf("%-18s %-9s %-7s ",
				truncate(d.Path, 18), truncate(d.Kind, 9), deviceStatus(d))
			nameBudget = avail - 37
		default:
			prefixCols = fmt.Sprintf("%-11s %-9s ", truncate(pathBase(d.Path), 11), truncate(d.Kind, 9))
			nameBudget = avail - 22
		}
		name := truncate(d.Name, max(1, nameBudget-lipgloss.Width(suffix)))

		switch {
		case i == m.devCursor:
			// Full-width selection bar; markers fold into the bar as plain text.
			rows = append(rows, selBar("▶ "+prefixCols+name+suffix, innerW))
		case d.IsVirtual:
			rows = append(rows, dimStyle.Render("  "+prefixCols+name+suffix))
		case d.Primary:
			// Re-color just the ★ marker on non-selected primary rows.
			rows = append(rows, "  "+prefixCols+name+"  "+goodStyle.Render("★ likely"))
		default:
			rows = append(rows, "  "+prefixCols+name+suffix)
		}
	}
	if !m.devFromDaemon {
		note := "(daemon down — direct enumeration; STATUS unavailable)"
		if avail < lipgloss.Width(note) {
			note = "(daemon down — STATUS n/a)"
		}
		rows = append(rows, "", dimStyle.Render(note))
	}
	list := lipgloss.JoinVertical(lipgloss.Left, rows...)

	// Detail pane for the selected device: recommendation reasons.
	detail := ""
	if hasDetail {
		var rs []string
		verdict := warnStyle.Render("not recommended")
		if sel.Recommended {
			verdict = goodStyle.Render("remappable")
		}
		rs = append(rs, verdict, "")
		for _, r := range sel.Reasons {
			rs = append(rs, dimStyle.Render("· "+r))
		}
		detail = lipgloss.JoinVertical(lipgloss.Left, rs...)
	}

	return m.renderPanes(lw, rw, "Devices", list, "Details", detail)
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

// pathBase returns the last path segment (e.g. "event3" from "/dev/input/event3")
// for the compact device-table tier.
func pathBase(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
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
