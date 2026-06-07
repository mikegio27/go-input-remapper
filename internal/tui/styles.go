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

	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colDim).
			Padding(0, 1)

	overlayStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colAccent).
			Padding(1, 2)
)

// dot returns a connection indicator: filled green when up, hollow grey when down.
func dot(up bool) string {
	if up {
		return goodStyle.Render("●")
	}
	return dimStyle.Render("○")
}
