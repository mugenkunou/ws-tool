// Package theme holds the TUI's lipgloss styles. It uses the standard
// 16-color palette, matching internal/style, so the TUI and CLI look alike and
// render correctly on light and dark terminals.
package theme

import (
	"charm.land/bubbles/v2/table"
	"charm.land/lipgloss/v2"
)

var (
	colorAccent = lipgloss.Color("6") // cyan
	colorOK     = lipgloss.Color("2") // green
	colorWarn   = lipgloss.Color("3") // yellow
	colorError  = lipgloss.Color("1") // red
	colorMuted  = lipgloss.Color("8") // bright black
)

var (
	Title     = lipgloss.NewStyle().Bold(true).Foreground(colorAccent)
	TabActive = lipgloss.NewStyle().Bold(true).Foreground(colorAccent).Underline(true)
	TabIdle   = lipgloss.NewStyle().Foreground(colorMuted)
	Muted     = lipgloss.NewStyle().Foreground(colorMuted)
	Bold      = lipgloss.NewStyle().Bold(true)
	OK        = lipgloss.NewStyle().Foreground(colorOK)
	Warn      = lipgloss.NewStyle().Foreground(colorWarn)
	Error     = lipgloss.NewStyle().Foreground(colorError)
	Rule      = lipgloss.NewStyle().Foreground(colorMuted)

	// Card frames a dashboard summary card; CardSelected marks the cursor.
	Card         = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colorMuted).Padding(0, 1)
	CardSelected = Card.BorderForeground(colorAccent)
)

// Table returns styles for bubbles/table consistent with the theme.
func Table() table.Styles {
	return table.Styles{
		Header:   lipgloss.NewStyle().Bold(true).Foreground(colorAccent).Padding(0, 1),
		Cell:     lipgloss.NewStyle().Padding(0, 1),
		Selected: lipgloss.NewStyle().Bold(true).Reverse(true),
	}
}

// TableCellPadding is the horizontal padding Table() adds around each cell.
const TableCellPadding = 2
