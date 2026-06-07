package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func (m *Model) profilesKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.profCursor > 0 {
			m.profCursor--
		}
	case "down", "j":
		if m.profCursor < len(m.profileNames)-1 {
			m.profCursor++
		}
	case "enter":
		if name, ok := m.selectedProfile(); ok {
			m.setFlash("switching to "+name+"…", false)
			return m, setProfile(m.opts.SocketPath, m.opts.ConfigDir, name)
		}
	case "n":
		m.addingProfile = true
		m.profileInput = newKeyInput("new profile name")
		m.profileInput.Focus()
	case "d":
		if name, ok := m.selectedProfile(); ok {
			m.setFlash("deleting "+name+"…", false)
			return m, deleteProfile(m.opts.SocketPath, m.opts.ConfigDir, name)
		}
	}
	return m, nil
}

func (m *Model) selectedProfile() (string, bool) {
	if m.profCursor < 0 || m.profCursor >= len(m.profileNames) {
		return "", false
	}
	return m.profileNames[m.profCursor], true
}

// profileInputKey handles the new-profile name entry overlay.
func (m *Model) profileInputKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.addingProfile = false
		return m, nil
	case "enter":
		name := sanitizeProfileName(m.profileInput.Value())
		m.addingProfile = false
		if name == "" {
			m.setFlash("profile name can't be empty", true)
			return m, nil
		}
		if _, exists := m.cfg.Profiles[name]; exists {
			m.setFlash("profile "+name+" already exists", true)
			return m, nil
		}
		m.setFlash("creating "+name+"…", false)
		return m, createProfile(m.opts.SocketPath, m.opts.ConfigDir, name, m.activeProfile == "")
	}
	var cmd tea.Cmd
	m.profileInput, cmd = m.profileInput.Update(msg)
	return m, cmd
}

// sanitizeProfileName keeps profile names usable as filenames.
func sanitizeProfileName(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "/", "-")
	return s
}

func (m *Model) profilesView() string {
	var rows []string

	if m.addingProfile {
		rows = append(rows, "new profile: "+m.profileInput.View())
		rows = append(rows, dimStyle.Render("enter to create · esc cancel"))
		rows = append(rows, "")
	}

	if len(m.profileNames) == 0 {
		empty := mutedStyle.Render("No profiles yet — press 'n' to create one.")
		return lipgloss.JoinVertical(lipgloss.Left, append(rows, empty)...)
	}
	for i, name := range m.profileNames {
		marker := "  "
		label := name
		if name == m.activeProfile {
			marker = goodStyle.Render("★ ")
			label = label + dimStyle.Render("  (active)")
		}
		cursor := "  "
		if i == m.profCursor {
			cursor = cursorRowStyle.Render("▶ ")
			label = cursorRowStyle.Render(name)
			if name == m.activeProfile {
				label += dimStyle.Render("  (active)")
			}
		}
		// Show how many device bindings each profile has.
		count := 0
		if p := m.cfg.Profiles[name]; p != nil {
			count = len(p.Devices)
		}
		rows = append(rows, cursor+marker+label+dimStyle.Render(devCountSuffix(count)))
	}
	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

func devCountSuffix(n int) string {
	if n == 1 {
		return "  — 1 device"
	}
	return "  — " + itoa(n) + " devices"
}
