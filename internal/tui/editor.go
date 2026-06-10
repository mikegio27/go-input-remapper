package tui

import (
	"fmt"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mikegio27/nereus/internal/config"
	"github.com/mikegio27/nereus/internal/control"
	"github.com/mikegio27/nereus/internal/device"
)

// editorState holds the remap editor for one device's binding in the active
// profile. It edits a working copy; changes are written to disk on save.
type editorState struct {
	device     control.DeviceInfo
	profileKey string
	matcher    config.DeviceMatcher
	bindingIdx int            // index into the profile's Devices, or -1 if new
	macros     []config.Macro // preserved unchanged on save
	remaps     []config.Remap
	cursor     int

	adding    bool
	fromInput textinput.Model
	toInput   textinput.Model
	focusTo   bool
}

// openEditor enters the remap editor for the selected device, loading or starting
// its binding in the active profile.
func (m *Model) openEditor(d control.DeviceInfo) (tea.Model, tea.Cmd) {
	if d.IsVirtual {
		m.setFlash("that's a virtual device — remapping it would loop", true)
		return m, nil
	}
	if m.cfg == nil {
		m.setFlash("config still loading; try again", true)
		return m, loadConfig(m.opts.ConfigDir)
	}
	profileKey := m.activeProfile
	if profileKey == "" || m.cfg.Profiles[profileKey] == nil {
		m.setFlash("no active profile — activate or create one in the Profiles tab", true)
		return m, nil
	}
	prof := m.cfg.Profiles[profileKey]

	id := device.Identity{Name: d.Name, Vendor: d.Vendor, Product: d.Product, Path: d.Path}
	es := &editorState{device: d, profileKey: profileKey, bindingIdx: -1}
	for i, b := range prof.Devices {
		if b.Match.Matches(id) {
			es.bindingIdx = i
			es.matcher = b.Match
			es.remaps = append([]config.Remap{}, b.Remaps...)
			es.macros = b.Macros
			break
		}
	}
	if es.bindingIdx < 0 {
		// Start a new binding matched by model identity.
		es.matcher = config.DeviceMatcher{
			Name:    d.Name,
			Vendor:  config.HexU16(d.Vendor),
			Product: config.HexU16(d.Product),
		}
	}
	m.editor = es
	// If the user picked a secondary node of a multi-node device, point them at
	// the likely-correct one — but still let them proceed.
	if !d.Primary {
		if p, ok := m.primarySiblingPath(d); ok {
			m.setFlash("heads up: "+p+" is the ★ likely node for this device", false)
		}
	}
	return m, nil
}

// primarySiblingPath returns the path of the primary node sharing d's identity
// (same name + vendor + product), if a different one exists in the device list.
func (m *Model) primarySiblingPath(d control.DeviceInfo) (string, bool) {
	for _, o := range m.devices {
		if o.Primary && o.Path != d.Path &&
			o.Name == d.Name && o.Vendor == d.Vendor && o.Product == d.Product {
			return o.Path, true
		}
	}
	return "", false
}

func (m *Model) editorKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	e := m.editor
	if e.adding {
		return m.editorAddingKey(msg)
	}
	switch msg.String() {
	case "esc":
		m.editor = nil
	case "up", "k":
		if e.cursor > 0 {
			e.cursor--
		}
	case "down", "j":
		if e.cursor < len(e.remaps)-1 {
			e.cursor++
		}
	case "a":
		e.startAdding()
	case "d":
		if e.cursor >= 0 && e.cursor < len(e.remaps) {
			e.remaps = append(e.remaps[:e.cursor], e.remaps[e.cursor+1:]...)
			if e.cursor >= len(e.remaps) {
				e.cursor = max(0, len(e.remaps)-1)
			}
		}
	case "s":
		return m.editorSave()
	}
	return m, nil
}

func (m *Model) editorAddingKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	e := m.editor
	switch msg.String() {
	case "esc":
		e.adding = false
		return m, nil
	case "tab":
		e.focusTo = !e.focusTo
		e.syncFocus()
		return m, nil
	case "c":
		// Capture into the focused field.
		purpose := purposeRemapFrom
		prompt := "Press the key to remap (the FROM key)…"
		if e.focusTo {
			purpose = purposeRemapTo
			prompt = "Press the key it should become (the TO key)…"
		}
		return m.beginCapture(e.device.Path, "key", purpose, prompt)
	case "enter":
		return m.editorCommitRow()
	}
	// Forward to the focused text input.
	var cmd tea.Cmd
	if e.focusTo {
		e.toInput, cmd = e.toInput.Update(msg)
	} else {
		e.fromInput, cmd = e.fromInput.Update(msg)
	}
	return m, cmd
}

// editorCaptured fills the focused field with the captured key name.
func (m *Model) editorCaptured(purpose capturePurpose, result []string) (tea.Model, tea.Cmd) {
	if m.editor == nil || len(result) == 0 {
		return m, nil
	}
	if purpose == purposeRemapTo {
		m.editor.toInput.SetValue(result[0])
	} else {
		m.editor.fromInput.SetValue(result[0])
	}
	return m, nil
}

func (m *Model) editorCommitRow() (tea.Model, tea.Cmd) {
	e := m.editor
	from := e.fromInput.Value()
	to := e.toInput.Value()
	if !config.IsKeyName(from) {
		m.setFlash("invalid FROM key: "+from, true)
		return m, nil
	}
	if to != "" && !config.IsKeyName(to) {
		m.setFlash("invalid TO key: "+to, true)
		return m, nil
	}
	e.remaps = append(e.remaps, config.Remap{From: from, To: to})
	e.adding = false
	e.cursor = len(e.remaps) - 1
	m.setFlash("added remap (press s to save)", false)
	return m, nil
}

func (m *Model) editorSave() (tea.Model, tea.Cmd) {
	e := m.editor
	prof := m.cfg.Profiles[e.profileKey]
	if prof == nil {
		m.setFlash("profile vanished; reload", true)
		return m, nil
	}
	binding := config.DeviceBinding{Match: e.matcher, Remaps: e.remaps, Macros: e.macros}
	if e.bindingIdx >= 0 && e.bindingIdx < len(prof.Devices) {
		prof.Devices[e.bindingIdx] = binding
	} else {
		prof.Devices = append(prof.Devices, binding)
		e.bindingIdx = len(prof.Devices) - 1
	}
	if err := config.SaveProfile(m.opts.ConfigDir, e.profileKey, prof); err != nil {
		m.setFlash("save failed: "+err.Error(), true)
		return m, nil
	}
	m.editor = nil
	return m, reloadDaemon(m.opts.SocketPath)
}

func (e *editorState) startAdding() {
	e.adding = true
	e.focusTo = false
	e.fromInput = newKeyInput("FROM key, e.g. KEY_CAPSLOCK")
	e.toInput = newKeyInput("TO key (empty = suppress)")
	e.syncFocus()
}

func (e *editorState) syncFocus() {
	if e.focusTo {
		e.fromInput.Blur()
		e.toInput.Focus()
	} else {
		e.toInput.Blur()
		e.fromInput.Focus()
	}
}

func newKeyInput(placeholder string) textinput.Model {
	ti := textinput.New()
	ti.Placeholder = placeholder
	ti.CharLimit = 32
	ti.Prompt = "› "
	return ti
}

func (m *Model) editorView() string {
	e := m.editor
	header := lipgloss.JoinVertical(lipgloss.Left,
		tabActiveStyle.Render("Remap editor")+"  "+e.device.Name,
		dimStyle.Render(fmt.Sprintf("profile %q · matcher %s", e.profileKey, matcherSummary(e.matcher))),
	)

	var rows []string
	if len(e.remaps) == 0 {
		rows = append(rows, mutedStyle.Render("no remaps yet — press 'a' to add one"))
	}
	for i, r := range e.remaps {
		cursor := "  "
		if i == e.cursor && !e.adding {
			cursor = cursorRowStyle.Render("▶ ")
		}
		to := r.To
		if to == "" {
			to = dimStyle.Render("(suppressed)")
		}
		rows = append(rows, cursor+fmt.Sprintf("%-16s → %s", r.From, to))
	}
	list := panel("Remaps", lipgloss.JoinVertical(lipgloss.Left, rows...), !e.adding)

	if e.adding {
		form := lipgloss.JoinVertical(lipgloss.Left,
			"from: "+e.fromInput.View(),
			"to:   "+e.toInput.View(),
			"",
			dimStyle.Render("tab switch field · c capture key · enter add · esc cancel"),
		)
		return lipgloss.JoinVertical(lipgloss.Left, header, "", joinPanels(m.width, list, panel("Add remap", form, true)))
	}
	return lipgloss.JoinVertical(lipgloss.Left, header, "", list)
}

func matcherSummary(m config.DeviceMatcher) string {
	s := ""
	if m.Name != "" {
		s += m.Name
	}
	if m.Vendor != 0 || m.Product != 0 {
		s += fmt.Sprintf(" %04x:%04x", uint16(m.Vendor), uint16(m.Product))
	}
	if m.Uniq != "" {
		s += " uniq=" + m.Uniq
	}
	return s
}
