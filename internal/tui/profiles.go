package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mikegio27/go-input-remapper/internal/config"
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
	if len(m.profileNames) == 0 {
		return m.singleCentered("Profiles", mutedStyle.Render("No profiles yet — press 'n' to create one."))
	}

	// The new-profile form takes the detail pane while it's open; otherwise a
	// summary of the selected profile does.
	rightTitle, rightBody := m.profileDetail()
	lw, rw := m.paneWidths(rightBody != "")
	listOuter := m.bodyW
	if rw > 0 {
		listOuter = lw
	}
	innerW := panelInnerWidth(listOuter)

	var rows []string
	for i, name := range m.profileNames {
		count := 0
		if p := m.cfg.Profiles[name]; p != nil {
			count, _ = profileCounts(p)
		}
		active := name == m.activeProfile
		if i == m.profCursor {
			label := "▶ "
			if active {
				label += "★ "
			} else {
				label += "  "
			}
			label += name
			if active {
				label += "  (active)"
			}
			label += devCountSuffix(count)
			rows = append(rows, selBar(label, innerW))
			continue
		}
		marker := "    "
		label := name
		if active {
			marker = "  " + goodStyle.Render("★ ")
			label += dimStyle.Render("  (active)")
		}
		rows = append(rows, marker+label+dimStyle.Render(devCountSuffix(count)))
	}
	list := lipgloss.JoinVertical(lipgloss.Left, rows...)
	return m.renderPanes(lw, rw, "Profiles", list, rightTitle, rightBody)
}

// profileDetail returns the detail-pane title and body for the Profiles screen:
// the new-profile form while creating, else a summary of the selected profile.
func (m *Model) profileDetail() (title, body string) {
	if m.addingProfile {
		form := lipgloss.JoinVertical(lipgloss.Left,
			"name: "+m.profileInput.View(),
			"",
			dimStyle.Render("enter create · esc cancel"),
		)
		return "New profile", form
	}
	name, ok := m.selectedProfile()
	if !ok {
		return "", ""
	}
	p := m.cfg.Profiles[name]
	if p == nil {
		return "", ""
	}
	devices, maps := profileCounts(p)
	state := dimStyle.Render("inactive")
	if name == m.activeProfile {
		state = goodStyle.Render("active")
	}
	lines := []string{
		panelTitleStyle.Render(name),
		"",
		"state:    " + state,
		"devices:  " + itoa(devices),
		"mappings: " + itoa(maps),
	}
	return "Selected", lipgloss.JoinVertical(lipgloss.Left, lines...)
}

// profileCounts reports a profile's effective device and mapping totals. A device
// binding with no remaps or macros is a placeholder the daemon never grabs (see
// config.DeviceBinding.HasRules), so it doesn't count as a device — keeping the
// displayed "0 devices" consistent with "0 mappings".
func profileCounts(p *config.Profile) (devices, mappings int) {
	for _, b := range p.Devices {
		n := len(b.Remaps) + len(b.Macros)
		mappings += n
		if n > 0 {
			devices++
		}
	}
	return devices, mappings
}

func devCountSuffix(n int) string {
	if n == 1 {
		return "  — 1 device"
	}
	return "  — " + itoa(n) + " devices"
}
