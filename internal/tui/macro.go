package tui

import (
	"fmt"
	"strconv"
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
	macroStageRepeat
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

	// repeat-config stage
	repeatMode       string // config.RepeatMode*
	repeatMsInput    textinput.Model
	repeatCountInput textinput.Model
	repeatFocusCount bool
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
	case macroStageRepeat:
		return m.macroRepeatKey(msg)
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
		return m.beginCapture(ms.device.Path, "chord", purposeMacroStep, "Press key(s) together to add as a tap step (e.g. Shift+3), then release…")
	case "enter", "d":
		if len(ms.draft.Steps) == 0 {
			m.setFlash("add at least one step (k), or esc to cancel", true)
			return m, nil
		}
		// Move on to choosing whether the macro repeats.
		ms.stage = macroStageRepeat
		ms.repeatMode = config.RepeatModeNone
		ms.repeatFocusCount = false
		ms.repeatMsInput = newKeyInput("interval ms, e.g. 50")
		ms.repeatCountInput = newKeyInput("number of runs, e.g. 5")
	}
	return m, nil
}

// macroRepeatKey handles the repeat-configuration stage: pick a mode and (when
// repeating) an interval, plus a run count for "count" mode.
func (m *Model) macroRepeatKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	ms := m.macro
	switch msg.String() {
	case "esc":
		ms.stage = macroStageList
		return m, nil
	case "m":
		ms.repeatMode = nextRepeatMode(ms.repeatMode)
		if ms.repeatMode != config.RepeatModeCount {
			ms.repeatFocusCount = false
		}
		m.syncRepeatFocus()
		return m, nil
	case "tab":
		if ms.repeatMode == config.RepeatModeCount {
			ms.repeatFocusCount = !ms.repeatFocusCount
			m.syncRepeatFocus()
		}
		return m, nil
	case "enter":
		return m.macroRepeatCommit()
	}
	// Forward typing to the focused field (only meaningful while repeating).
	if ms.repeatMode == config.RepeatModeNone {
		return m, nil
	}
	var cmd tea.Cmd
	if ms.repeatFocusCount {
		ms.repeatCountInput, cmd = ms.repeatCountInput.Update(msg)
	} else {
		ms.repeatMsInput, cmd = ms.repeatMsInput.Update(msg)
	}
	return m, cmd
}

// macroRepeatCommit validates the repeat settings, writes them onto the draft,
// appends the finished macro, and returns to the list.
func (m *Model) macroRepeatCommit() (tea.Model, tea.Cmd) {
	ms := m.macro
	ms.draft.Repeat = ms.repeatMode
	ms.draft.RepeatMs = 0
	ms.draft.RepeatCount = 0
	if ms.repeatMode != config.RepeatModeNone {
		interval, err := strconv.Atoi(strings.TrimSpace(ms.repeatMsInput.Value()))
		if err != nil || interval <= 0 {
			m.setFlash("repeat interval must be a positive number of milliseconds", true)
			return m, nil
		}
		ms.draft.RepeatMs = interval
		if ms.repeatMode == config.RepeatModeCount {
			count, err := strconv.Atoi(strings.TrimSpace(ms.repeatCountInput.Value()))
			if err != nil || count <= 0 {
				m.setFlash("repeat count must be a positive number of runs", true)
				return m, nil
			}
			ms.draft.RepeatCount = count
		}
	}
	ms.macros = append(ms.macros, ms.draft)
	ms.draft = config.Macro{}
	ms.stage = macroStageList
	ms.cursor = len(ms.macros) - 1
	m.setFlash("macro added (press s to save)", false)
	return m, nil
}

// nextRepeatMode cycles none → hold → toggle → count → none.
func nextRepeatMode(mode string) string {
	switch mode {
	case config.RepeatModeNone:
		return config.RepeatModeHold
	case config.RepeatModeHold:
		return config.RepeatModeToggle
	case config.RepeatModeToggle:
		return config.RepeatModeCount
	default:
		return config.RepeatModeNone
	}
}

// syncRepeatFocus focuses the right text input for the current repeat mode.
func (m *Model) syncRepeatFocus() {
	ms := m.macro
	ms.repeatMsInput.Blur()
	ms.repeatCountInput.Blur()
	if ms.repeatMode == config.RepeatModeNone {
		return
	}
	if ms.repeatFocusCount {
		ms.repeatCountInput.Focus()
	} else {
		ms.repeatMsInput.Focus()
	}
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
		var step config.MacroStep
		if len(result) == 1 {
			step.Key = result[0]
		} else {
			step.Keys = result
		}
		ms.draft.Steps = append(ms.draft.Steps, step)
		m.setFlash("added step "+strings.Join(result, "+"), false)
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

// repeatModeLabel is the human label for a repeat mode.
func repeatModeLabel(mode string) string {
	switch mode {
	case config.RepeatModeHold:
		return "hold (repeat while trigger held)"
	case config.RepeatModeToggle:
		return "toggle (press to start, press again to stop)"
	case config.RepeatModeCount:
		return "count (run a fixed number of times)"
	default:
		return "none (run once)"
	}
}

// repeatModeHelp is a one-line explanation shown under the selected mode.
func repeatModeHelp(mode string) string {
	switch mode {
	case config.RepeatModeHold:
		return "re-runs every interval until you release the trigger chord"
	case config.RepeatModeToggle:
		return "the trigger latches repeating on/off"
	case config.RepeatModeCount:
		return "runs exactly N times, interval apart, then stops"
	default:
		return "press m to make it repeat"
	}
}

// repeatSummary describes a macro's repeat config for the list view.
func repeatSummary(mac config.Macro) string {
	switch mac.Repeat {
	case config.RepeatModeHold:
		return fmt.Sprintf("repeat while held @%dms", mac.RepeatMs)
	case config.RepeatModeToggle:
		return fmt.Sprintf("toggle @%dms", mac.RepeatMs)
	case config.RepeatModeCount:
		return fmt.Sprintf("%d×@%dms", mac.RepeatCount, mac.RepeatMs)
	default:
		return "once"
	}
}

// stepLabel renders one macro step for display, covering taps, holds/releases,
// typed text, and pure delays.
func stepLabel(s config.MacroStep) string {
	keys := strings.Join(s.KeyNames(), " + ")
	var label string
	switch {
	case keys != "" && s.Hold:
		label = "hold " + keys
	case keys != "" && s.Release:
		label = "release " + keys
	case keys != "":
		label = "tap " + keys
	case s.Text != "":
		label = fmt.Sprintf("type %q", s.Text)
	default:
		label = "wait"
	}
	if s.DelayMs > 0 {
		label = fmt.Sprintf("%s (after %dms)", label, s.DelayMs)
	}
	return label
}

func (m *Model) macroView() string {
	ms := m.macro
	header := lipgloss.JoinVertical(lipgloss.Left,
		tabActiveStyle.Render("Macro recorder")+"  "+ms.device.Name,
		dimStyle.Render(fmt.Sprintf("profile %q · matcher %s", ms.profileKey, matcherSummary(ms.matcher))),
	)
	wrap := func(title, body string) string {
		return lipgloss.JoinVertical(lipgloss.Left, header, "", panel(title, body, true))
	}

	switch ms.stage {
	case macroStageName:
		body := lipgloss.JoinVertical(lipgloss.Left,
			"name: "+ms.nameInput.View(),
			"",
			dimStyle.Render("enter to continue to trigger capture · esc cancel"),
		)
		return wrap("New macro", body)
	case macroStageSteps:
		var rows []string
		rows = append(rows, "trigger: "+tabActiveStyle.Render(strings.Join(ms.draft.Trigger, " + ")))
		rows = append(rows, "")
		rows = append(rows, dimStyle.Render("steps run in order, one after another (top → bottom):"))
		if len(ms.draft.Steps) == 0 {
			rows = append(rows, mutedStyle.Render("no steps yet — press k to capture key(s)"))
		}
		for i, s := range ms.draft.Steps {
			arrow := "  "
			if i > 0 {
				arrow = dimStyle.Render("↓ ")
			}
			rows = append(rows, fmt.Sprintf("%s%d. %s", arrow, i+1, stepLabel(s)))
		}
		rows = append(rows, "", dimStyle.Render("k add key(s) · enter finish macro · esc cancel"))
		return wrap("Steps — "+ms.draft.Name, lipgloss.JoinVertical(lipgloss.Left, rows...))
	case macroStageRepeat:
		var rows []string
		rows = append(rows, dimStyle.Render(fmt.Sprintf("%d step(s) captured", len(ms.draft.Steps))))
		rows = append(rows, "")
		rows = append(rows, "repeat mode: "+tabActiveStyle.Render(repeatModeLabel(ms.repeatMode)))
		rows = append(rows, dimStyle.Render(repeatModeHelp(ms.repeatMode)))
		if ms.repeatMode != config.RepeatModeNone {
			rows = append(rows, "")
			rows = append(rows, "interval: "+ms.repeatMsInput.View()+dimStyle.Render(" ms"))
			if ms.repeatMode == config.RepeatModeCount {
				rows = append(rows, "runs:     "+ms.repeatCountInput.View())
			}
		}
		rows = append(rows, "")
		hint := "m change mode · enter finish macro · esc cancel"
		if ms.repeatMode == config.RepeatModeCount {
			hint = "m change mode · tab switch field · enter finish · esc cancel"
		}
		rows = append(rows, dimStyle.Render(hint))
		return wrap("Repeat — "+ms.draft.Name, lipgloss.JoinVertical(lipgloss.Left, rows...))
	}

	// list stage
	var rows []string
	if len(ms.macros) == 0 {
		rows = append(rows, mutedStyle.Render("no macros yet — press 'n' to record one"))
	}
	for i, mac := range ms.macros {
		cursor := "  "
		if i == ms.cursor {
			cursor = cursorRowStyle.Render("▶ ")
		}
		rows = append(rows, cursor+fmt.Sprintf("%-16s %s → %d step(s) · %s",
			mac.Name, strings.Join(mac.Trigger, "+"), len(mac.Steps), repeatSummary(mac)))
	}
	return wrap("Macros", lipgloss.JoinVertical(lipgloss.Left, rows...))
}
