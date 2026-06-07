package tui

import "github.com/charmbracelet/lipgloss"

// Palette. Kept small and terminal-friendly (256-color safe).
var (
	colAccent = lipgloss.Color("39")  // cyan/blue
	colGood   = lipgloss.Color("42")  // green
	colWarn   = lipgloss.Color("214") // amber
	colErr    = lipgloss.Color("203") // red
	colMuted  = lipgloss.Color("245") // grey
	colDim    = lipgloss.Color("240") // dark grey
)

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("231")).
			Background(colAccent).
			Padding(0, 1)

	headerBarStyle = lipgloss.NewStyle().
			Foreground(colMuted).
			Padding(0, 1)

	tabActiveStyle = lipgloss.NewStyle().Bold(true).Foreground(colAccent)
	tabInactive    = lipgloss.NewStyle().Foreground(colDim)

	footerStyle = lipgloss.NewStyle().Foreground(colMuted).Padding(0, 1)

	cursorRowStyle = lipgloss.NewStyle().Bold(true).Foreground(colAccent)
	dimStyle       = lipgloss.NewStyle().Foreground(colDim)
	mutedStyle     = lipgloss.NewStyle().Foreground(colMuted)
	goodStyle      = lipgloss.NewStyle().Foreground(colGood)
	warnStyle      = lipgloss.NewStyle().Foreground(colWarn)
	errStyle       = lipgloss.NewStyle().Foreground(colErr)

	overlayStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colAccent).
			Padding(1, 2)

	// panelTitleStyle is the heading rendered at the top of a titled panel.
	panelTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(colAccent)
)

// panel wraps body in a titled rounded box. focused panels get a blue border, the
// rest a dim grey one, so the eye lands on the active region.
func panel(title, body string, focused bool) string {
	border := colDim
	if focused {
		border = colAccent
	}
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(border).
		Padding(0, 1)
	head := panelTitleStyle.Render(title)
	return style.Render(lipgloss.JoinVertical(lipgloss.Left, head, "", body))
}

// joinPanels lays two panels side by side when the terminal is wide enough,
// stacking them otherwise so nothing gets clipped on narrow terminals.
func joinPanels(width int, left, right string) string {
	if right == "" {
		return left
	}
	combined := lipgloss.JoinHorizontal(lipgloss.Top, left, "  ", right)
	if width > 0 && lipgloss.Width(combined) > width {
		return lipgloss.JoinVertical(lipgloss.Left, left, right)
	}
	return combined
}

// dot returns a connection indicator: filled green when up, hollow grey when down.
func dot(up bool) string {
	if up {
		return goodStyle.Render("●")
	}
	return dimStyle.Render("○")
}
