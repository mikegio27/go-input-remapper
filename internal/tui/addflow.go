package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mikegio27/go-input-remapper/internal/control"
)

// addFlowStage is the step of the "add new mapping" wizard.
type addFlowStage int

const (
	addFlowPickDevice addFlowStage = iota
	addFlowPickType
)

// addFlowState is the modal "add new mapping" wizard launched from the Mappings
// tab: pick a device, then choose remap or macro. It then hands off to the
// existing remap editor / macro recorder for that device.
type addFlowState struct {
	stage   addFlowStage
	cursor  int
	devices []control.DeviceInfo
	device  control.DeviceInfo // chosen at addFlowPickDevice
}

// remappableDevices returns the present, non-virtual, recommended devices —
// the ones it makes sense to add a mapping to (regardless of the showAll toggle).
func (m *Model) remappableDevices() []control.DeviceInfo {
	var out []control.DeviceInfo
	for _, d := range m.devices {
		if d.Recommended && !d.IsVirtual {
			out = append(out, d)
		}
	}
	return out
}

// beginAddFlow opens the add-mapping wizard if there's an active profile and at
// least one remappable device.
func (m *Model) beginAddFlow() (tea.Model, tea.Cmd) {
	if m.cfg == nil || m.activeProfile == "" || m.cfg.Profiles[m.activeProfile] == nil {
		m.setFlash("no active profile — activate or create one in the Profiles tab", true)
		return m, nil
	}
	devs := m.remappableDevices()
	if len(devs) == 0 {
		m.setFlash("no remappable devices detected (is the daemon running?)", true)
		return m, nil
	}
	m.addFlow = &addFlowState{stage: addFlowPickDevice, devices: devs}
	return m, nil
}

func (m *Model) addFlowKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	af := m.addFlow
	switch af.stage {
	case addFlowPickDevice:
		switch msg.String() {
		case "esc":
			m.addFlow = nil
		case "up", "k":
			if af.cursor > 0 {
				af.cursor--
			}
		case "down", "j":
			if af.cursor < len(af.devices)-1 {
				af.cursor++
			}
		case "enter":
			af.device = af.devices[af.cursor]
			af.stage = addFlowPickType
			af.cursor = 0
		}
	case addFlowPickType:
		switch msg.String() {
		case "esc":
			af.stage = addFlowPickDevice
		case "up", "k":
			af.cursor = 0
		case "down", "j":
			af.cursor = 1
		case "r":
			d := af.device
			m.addFlow = nil
			return m.openEditor(d)
		case "m":
			d := af.device
			m.addFlow = nil
			return m.openMacroRecorder(d)
		case "enter":
			d := af.device
			choice := af.cursor
			m.addFlow = nil
			if choice == 0 {
				return m.openEditor(d)
			}
			return m.openMacroRecorder(d)
		}
	}
	return m, nil
}

func (m *Model) addFlowOverlay() string {
	af := m.addFlow
	var lines []string
	switch af.stage {
	case addFlowPickDevice:
		lines = append(lines, tabActiveStyle.Render("Add mapping — choose a device"), "")
		for i, d := range af.devices {
			cursor := "  "
			label := fmt.Sprintf("%-9s %04x:%04x  %s", d.Kind, d.Vendor, d.Product, d.Name)
			if i == af.cursor {
				cursor = cursorRowStyle.Render("▶ ")
				label = cursorRowStyle.Render(label)
			}
			lines = append(lines, cursor+label)
		}
		lines = append(lines, "", dimStyle.Render("↑/↓ move · enter select · esc cancel"))
	case addFlowPickType:
		lines = append(lines, tabActiveStyle.Render("Add to ")+af.device.Name, "")
		opts := []string{"Remap  — swap or suppress a key", "Macro  — trigger a sequence of keys"}
		for i, o := range opts {
			cursor := "  "
			if i == af.cursor {
				cursor = cursorRowStyle.Render("▶ ")
				o = cursorRowStyle.Render(o)
			}
			lines = append(lines, cursor+o)
		}
		lines = append(lines, "", dimStyle.Render("↑/↓ move · enter choose · r remap · m macro · esc back"))
	}
	box := overlayStyle.Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
	if m.width == 0 || m.height == 0 {
		return box
	}
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}
