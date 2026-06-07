package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mikegio27/go-input-remapper/internal/config"
	"github.com/mikegio27/go-input-remapper/internal/control"
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
		return panel("Mappings", mutedStyle.Render(
			"No active profile.\nActivate one in the Profiles tab to see its mappings."), true)
	}

	rows := m.mappingRows()
	if m.mapCursor >= len(rows) {
		m.mapCursor = max(0, len(rows)-1)
	}

	var out []string
	out = append(out, dimStyle.Render("profile: ")+profile, "")
	if len(rows) == 0 {
		out = append(out, mutedStyle.Render(
			"No mappings in this profile yet.\nPress 'a' to add one (pick a device, then remap or macro)."))
		return panel("Mappings", lipgloss.JoinVertical(lipgloss.Left, out...), true)
	}

	out = append(out, dimStyle.Render(fmt.Sprintf("  %-22s %-16s %s", "DEVICE", "FROM", "TO / MACRO")))
	for i, r := range rows {
		cursor := "  "
		if i == m.mapCursor {
			cursor = cursorRowStyle.Render("▶ ")
		}
		dev := truncate(r.devLabel, 22)
		var line string
		if r.isMacro {
			trig := strings.Join(r.trigger, "+")
			line = fmt.Sprintf("%-22s %-16s %s", dev, trig, dimStyle.Render("macro: ")+r.macro)
		} else {
			to := r.to
			if to == "" {
				to = dimStyle.Render("(suppressed)")
			}
			line = fmt.Sprintf("%-22s %-16s → %s", dev, r.from, to)
		}
		if i == m.mapCursor {
			line = cursorRowStyle.Render(line)
		}
		out = append(out, cursor+line)
	}
	return panel("Mappings", lipgloss.JoinVertical(lipgloss.Left, out...), true)
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
