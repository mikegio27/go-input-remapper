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

	// selBarStyle is the full-width highlight bar drawn on the selected list row.
	selBarStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("231")).Background(colAccent)

	// stripStyle is the persistent at-a-glance status line above the footer.
	stripStyle = lipgloss.NewStyle().Foreground(colMuted).Padding(0, 1)
)

// Panel chrome: a rounded border (1 col/row each side) plus horizontal padding of
// 1. A titled panel also spends two body rows on the title line and the blank
// under it. These constants let views compute the content area a panel of a given
// outer size will expose, so rows can fill the width and empty states can center.
const (
	panelChromeW = 4 // left border + right border + left pad + right pad
	panelChromeH = 4 // top border + bottom border + title row + blank row
)

// panelInnerWidth is the content width a titled panel of the given outer width
// exposes (0 if the panel is too small or the size isn't known yet).
func panelInnerWidth(outerW int) int {
	if outerW <= panelChromeW {
		return 0
	}
	return outerW - panelChromeW
}

// panelInnerHeight is the body height (below the title row) a titled panel of the
// given outer height exposes.
func panelInnerHeight(outerH int) int {
	if outerH <= panelChromeH {
		return 0
	}
	return outerH - panelChromeH
}

// fillPanel wraps body in a titled rounded box sized to an exact outer width and
// height. Focused panels get a blue border, the rest a dim grey one, so the eye
// lands on the active region. A non-positive w or h sizes that axis to content —
// the path used before the first WindowSizeMsg and by nested/overlay panels.
func fillPanel(title, body string, focused bool, w, h int) string {
	border := colDim
	if focused {
		border = colAccent
	}
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(border).
		Padding(0, 1)
	if w > 0 {
		style = style.Width(w - 2) // -2 leaves room for the L/R border
	}
	if h > 0 {
		style = style.Height(h - 2) // -2 leaves room for the T/B border
	}
	head := panelTitleStyle.Render(title)
	return style.Render(lipgloss.JoinVertical(lipgloss.Left, head, "", body))
}

// panel wraps body in a content-sized titled box (no fixed dimensions). Used for
// nested panels (editor/macro forms) and any caller that doesn't fill the frame.
func panel(title, body string, focused bool) string {
	return fillPanel(title, body, focused, 0, 0)
}

// selBar renders text as a full-width selection bar. innerW is the panel content
// width so the highlight spans the whole row; <=0 falls back to a snug bar.
func selBar(text string, innerW int) string {
	if innerW > 0 {
		return selBarStyle.Width(innerW).Render(text)
	}
	return selBarStyle.Render(text)
}

// centerBody centers body within an innerW×innerH box, for empty states that
// would otherwise hug the top-left of an mostly-empty panel.
func centerBody(body string, innerW, innerH int) string {
	if innerW <= 0 || innerH <= 0 {
		return body
	}
	return lipgloss.Place(innerW, innerH, lipgloss.Center, lipgloss.Center, body)
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
