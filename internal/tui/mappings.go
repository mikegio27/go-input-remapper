package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mikegio27/nereus/internal/config"
	"github.com/mikegio27/nereus/internal/control"
)

// mappingRow is one selectable line on the Mappings screen: either a key remap
// or a macro, tied to the device binding it belongs to.
type mappingRow struct {
	match    config.DeviceMatcher
	devLabel string
	isMacro  bool
	from, to string   // remap
	macro    string   // macro name
	trigger  []string // macro trigger chord
}

// mappingRows flattens the active profile's bindings into one selectable list of
// remaps and macros across every device.
func (m *Model) mappingRows() []mappingRow {
	if m.cfg == nil {
		return nil
	}
	prof := m.cfg.Profiles[m.activeProfile]
	if prof == nil {
		return nil
	}
	var rows []mappingRow
	for _, b := range prof.Devices {
		label := matcherLabel(b.Match)
		for _, r := range b.Remaps {
			rows = append(rows, mappingRow{match: b.Match, devLabel: label, from: r.From, to: r.To})
		}
		for _, mac := range b.Macros {
			rows = append(rows, mappingRow{match: b.Match, devLabel: label, isMacro: true, macro: mac.Name, trigger: mac.Trigger})
		}
	}
	return rows
}

func (m *Model) mappingsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	rows := m.mappingRows()
	switch msg.String() {
	case "up", "k":
		if m.mapCursor > 0 {
			m.mapCursor--
		}
	case "down", "j":
		if m.mapCursor < len(rows)-1 {
			m.mapCursor++
		}
	case "a":
		return m.beginAddFlow()
	case "enter":
		if m.mapCursor < 0 || m.mapCursor >= len(rows) {
			return m, nil
		}
		row := rows[m.mapCursor]
		d := m.deviceInfoForMatcher(row.match)
		if row.isMacro {
			return m.openMacroRecorder(d)
		}
		return m.openEditor(d)
	}
	return m, nil
}

// deviceInfoForMatcher finds the live device matching a binding (preferring the
// primary node so capture works), falling back to a stub built from the matcher
// when the device isn't currently present.
func (m *Model) deviceInfoForMatcher(match config.DeviceMatcher) control.DeviceInfo {
	var fallback *control.DeviceInfo
	for i := range m.devices {
		d := m.devices[i]
		if d.Name == match.Name && d.Vendor == uint16(match.Vendor) && d.Product == uint16(match.Product) {
			if d.Primary {
				return d
			}
			if fallback == nil {
				fallback = &m.devices[i]
			}
		}
	}
	if fallback != nil {
		return *fallback
	}
	return control.DeviceInfo{Name: match.Name, Vendor: uint16(match.Vendor), Product: uint16(match.Product)}
}

func (m *Model) mappingsView() string {
	profile := m.activeProfile
	if profile == "" {
		return m.singleCentered("Mappings", mutedStyle.Render(
			"No active profile.\nActivate one in the Profiles tab to see its mappings."))
	}

	rows := m.mappingRows()
	if m.mapCursor >= len(rows) {
		m.mapCursor = max(0, len(rows)-1)
	}

	if len(rows) == 0 {
		hint := lipgloss.JoinVertical(lipgloss.Center,
			panelTitleStyle.Render("No mappings in "+profile+" yet"),
			"",
			mutedStyle.Render("Press 'a' to add one:"),
			dimStyle.Render("pick a device, then a remap or a macro."),
		)
		return m.singleCentered("Mappings", hint)
	}

	innerW := panelInnerWidth(m.bodyW)
	// Responsive device-label column: shrink it on narrow terminals.
	avail := innerW - 2
	devW, fromW := 22, 16
	if avail < 56 {
		devW, fromW = 12, 12
	}
	toBudget := max(6, avail-devW-1-fromW-3) // " → " / "macro: " spacing
	out := []string{
		dimStyle.Render("profile: ") + profile,
		"",
		dimStyle.Render(fmt.Sprintf("  %-*s %-*s %s", devW, "DEVICE", fromW, "FROM", "TO / MACRO")),
	}
	for i, r := range rows {
		dev := truncate(r.devLabel, devW)
		if i == m.mapCursor {
			var line string
			if r.isMacro {
				line = fmt.Sprintf("%-*s %-*s macro: %s", devW, dev, fromW, truncate(strings.Join(r.trigger, "+"), fromW), truncate(r.macro, toBudget))
			} else {
				to := r.to
				if to == "" {
					to = "(suppressed)"
				}
				line = fmt.Sprintf("%-*s %-*s → %s", devW, dev, fromW, truncate(r.from, fromW), truncate(to, toBudget))
			}
			out = append(out, selBar("▶ "+line, innerW))
			continue
		}
		var line string
		if r.isMacro {
			trig := truncate(strings.Join(r.trigger, "+"), fromW)
			line = fmt.Sprintf("%-*s %-*s %s", devW, dev, fromW, trig, dimStyle.Render("macro: ")+truncate(r.macro, toBudget))
		} else {
			to := r.to
			if to == "" {
				to = dimStyle.Render("(suppressed)")
			} else {
				to = truncate(to, toBudget)
			}
			line = fmt.Sprintf("%-*s %-*s → %s", devW, dev, fromW, truncate(r.from, fromW), to)
		}
		out = append(out, "  "+line)
	}
	return fillPanel("Mappings", lipgloss.JoinVertical(lipgloss.Left, out...), true, m.bodyW, m.bodyH)
}

// matcherLabel renders a device matcher as a short human label for the table.
func matcherLabel(m config.DeviceMatcher) string {
	if m.Name != "" {
		return m.Name
	}
	if m.Vendor != 0 || m.Product != 0 {
		return fmt.Sprintf("%04x:%04x", uint16(m.Vendor), uint16(m.Product))
	}
	if m.Uniq != "" {
		return "uniq:" + m.Uniq
	}
	return "(any device)"
}
