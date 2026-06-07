package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mikegio27/go-input-remapper/internal/config"
	"github.com/mikegio27/go-input-remapper/internal/control"
	"github.com/mikegio27/go-input-remapper/internal/device"
)

type macroStage int

const (
	macroStageList macroStage = iota
	macroStageName
	macroStageTrigger
	macroStageSteps
)

// macroState is the macro recorder for one device's binding. Like the remap
// editor, it edits a working copy and writes to disk on save.
type macroState struct {
	device     control.DeviceInfo
	profileKey string
	matcher    config.DeviceMatcher
	bindingIdx int
	remaps     []config.Remap // preserved unchanged on save
	macros     []config.Macro
	cursor     int

	stage     macroStage
	nameInput textinput.Model
	draft     config.Macro
}

func (m *Model) openMacroRecorder(d control.DeviceInfo) (tea.Model, tea.Cmd) {
	if d.IsVirtual {
		m.setFlash("that's a virtual device — can't record macros on it", true)
		return m, nil
	}
	if m.cfg == nil {
		return m, loadConfig(m.opts.ConfigDir)
	}
	profileKey := m.activeProfile
	if profileKey == "" || m.cfg.Profiles[profileKey] == nil {
		m.setFlash("no active profile — activate or create one in the Profiles tab", true)
		return m, nil
	}
	prof := m.cfg.Profiles[profileKey]

	id := device.Identity{Name: d.Name, Vendor: d.Vendor, Product: d.Product, Path: d.Path}
	ms := &macroState{device: d, profileKey: profileKey, bindingIdx: -1, stage: macroStageList}
	for i, b := range prof.Devices {
		if b.Match.Matches(id) {
			ms.bindingIdx = i
			ms.matcher = b.Match
			ms.remaps = b.Remaps
			ms.macros = append([]config.Macro{}, b.Macros...)
			break
		}
	}
	if ms.bindingIdx < 0 {
		ms.matcher = config.DeviceMatcher{Name: d.Name, Vendor: config.HexU16(d.Vendor), Product: config.HexU16(d.Product)}
	}
	m.macro = ms
	return m, nil
}

func (m *Model) macroKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	ms := m.macro
	switch ms.stage {
	case macroStageName:
		return m.macroNameKey(msg)
	case macroStageSteps:
		return m.macroStepsKey(msg)
	case macroStageTrigger:
		// Trigger is captured via the overlay; nothing to do here but cancel.
		if msg.String() == "esc" {
			ms.stage = macroStageList
		}
		return m, nil
	default:
		return m.macroListKey(msg)
	}
}

func (m *Model) macroListKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	ms := m.macro
	switch msg.String() {
	case "esc":
		m.macro = nil
	case "up", "k":
		if ms.cursor > 0 {
			ms.cursor--
		}
	case "down", "j":
		if ms.cursor < len(ms.macros)-1 {
			ms.cursor++
		}
	case "x":
		if ms.cursor >= 0 && ms.cursor < len(ms.macros) {
			ms.macros = append(ms.macros[:ms.cursor], ms.macros[ms.cursor+1:]...)
			if ms.cursor >= len(ms.macros) {
				ms.cursor = max(0, len(ms.macros)-1)
			}
		}
	case "n":
		ms.stage = macroStageName
		ms.draft = config.Macro{}
		ms.nameInput = newKeyInput("macro name, e.g. copy-paste")
		ms.nameInput.Placeholder = "macro name"
		ms.nameInput.Focus()
	case "s":
		return m.macroSave()
	}
	return m, nil
}

func (m *Model) macroNameKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	ms := m.macro
	switch msg.String() {
	case "esc":
		ms.stage = macroStageList
		return m, nil
	case "enter":
		name := strings.TrimSpace(ms.nameInput.Value())
		if name == "" {
			m.setFlash("macro name can't be empty", true)
			return m, nil
		}
		ms.draft.Name = name
		ms.stage = macroStageTrigger
		return m.beginCapture(ms.device.Path, "chord", purposeMacroTrigger, "Press the trigger chord (e.g. Ctrl+J), then release…")
	}
	var cmd tea.Cmd
	ms.nameInput, cmd = ms.nameInput.Update(msg)
	return m, cmd
}

func (m *Model) macroStepsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	ms := m.macro
	switch msg.String() {
	case "esc":
		ms.stage = macroStageList
	case "k":
		return m.beginCapture(ms.device.Path, "key", purposeMacroStep, "Press a key to add as a tap step…")
	case "enter", "d":
		if len(ms.draft.Steps) == 0 {
			m.setFlash("add at least one step (k), or esc to cancel", true)
			return m, nil
		}
		ms.macros = append(ms.macros, ms.draft)
		ms.draft = config.Macro{}
		ms.stage = macroStageList
		ms.cursor = len(ms.macros) - 1
		m.setFlash("macro added (press s to save)", false)
	}
	return m, nil
}

// macroCaptured handles a finished trigger/step capture.
func (m *Model) macroCaptured(purpose capturePurpose, result []string) (tea.Model, tea.Cmd) {
	if m.macro == nil || len(result) == 0 {
		return m, nil
	}
	ms := m.macro
	switch purpose {
	case purposeMacroTrigger:
		ms.draft.Trigger = result
		ms.stage = macroStageSteps
		m.setFlash("trigger set: "+strings.Join(result, "+")+" — press k to add key steps", false)
	case purposeMacroStep:
		ms.draft.Steps = append(ms.draft.Steps, config.MacroStep{Key: result[0]})
		m.setFlash("added step "+result[0], false)
	}
	return m, nil
}

func (m *Model) macroSave() (tea.Model, tea.Cmd) {
	ms := m.macro
	prof := m.cfg.Profiles[ms.profileKey]
	if prof == nil {
		m.setFlash("profile vanished; reload", true)
		return m, nil
	}
	binding := config.DeviceBinding{Match: ms.matcher, Remaps: ms.remaps, Macros: ms.macros}
	if ms.bindingIdx >= 0 && ms.bindingIdx < len(prof.Devices) {
		prof.Devices[ms.bindingIdx] = binding
	} else {
		prof.Devices = append(prof.Devices, binding)
		ms.bindingIdx = len(prof.Devices) - 1
	}
	if err := config.SaveProfile(m.opts.ConfigDir, ms.profileKey, prof); err != nil {
		m.setFlash("save failed: "+err.Error(), true)
		return m, nil
	}
	m.macro = nil
	return m, reloadDaemon(m.opts.SocketPath)
}

func (m *Model) macroView() string {
	ms := m.macro
	var rows []string
	rows = append(rows, tabActiveStyle.Render("Macros: ")+ms.device.Name)
	rows = append(rows, dimStyle.Render(fmt.Sprintf("profile %q · matcher %s", ms.profileKey, matcherSummary(ms.matcher))))
	rows = append(rows, "")

	switch ms.stage {
	case macroStageName:
		rows = append(rows, "name: "+ms.nameInput.View())
		rows = append(rows, dimStyle.Render("enter to continue to trigger capture · esc cancel"))
		return lipgloss.JoinVertical(lipgloss.Left, rows...)
	case macroStageSteps:
		rows = append(rows, "building: "+tabActiveStyle.Render(ms.draft.Name))
		rows = append(rows, "trigger: "+strings.Join(ms.draft.Trigger, " + "))
		if len(ms.draft.Steps) == 0 {
			rows = append(rows, mutedStyle.Render("no steps yet — press k to capture a key"))
		}
		for i, s := range ms.draft.Steps {
			rows = append(rows, fmt.Sprintf("  %d. tap %s", i+1, s.Key))
		}
		rows = append(rows, dimStyle.Render("k add key · enter finish macro · esc cancel"))
		return lipgloss.JoinVertical(lipgloss.Left, rows...)
	}

	// list stage
	if len(ms.macros) == 0 {
		rows = append(rows, mutedStyle.Render("no macros yet — press 'n' to record one"))
	}
	for i, mac := range ms.macros {
		cursor := "  "
		if i == ms.cursor {
			cursor = cursorRowStyle.Render("▶ ")
		}
		rows = append(rows, cursor+fmt.Sprintf("%-16s %s → %d step(s)",
			mac.Name, strings.Join(mac.Trigger, "+"), len(mac.Steps)))
	}
	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}
