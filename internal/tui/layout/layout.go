// Package layout computes sizes from the terminal dimensions. Nothing in the
// TUI uses absolute coordinates; every region is derived from these helpers.
package layout

import (
	"charm.land/bubbles/v2/table"

	"github.com/mugenkunou/ws-tool/internal/tui/theme"
)

// Col describes a table column's sizing intent.
type Col struct {
	Title  string
	Min    int // content width the column wants at minimum
	Weight int // share of leftover width; 0 means fixed at Min
}

// Columns fits cols into total terminal cells, accounting for cell padding.
// Extra space goes to weighted columns; when space is short every column is
// shrunk proportionally (a column may reach width 0 and be hidden).
func Columns(total int, cols []Col) []table.Column {
	out := make([]table.Column, len(cols))
	avail := max(total-theme.TableCellPadding*len(cols), 0)

	sumMin, sumWeight := 0, 0
	for _, c := range cols {
		sumMin += c.Min
		sumWeight += c.Weight
	}

	if avail <= sumMin {
		for i, c := range cols {
			w := 0
			if sumMin > 0 {
				w = c.Min * avail / sumMin
			}
			out[i] = table.Column{Title: c.Title, Width: w}
		}
		return out
	}

	extra := avail - sumMin
	given := 0
	lastWeighted := -1
	for i, c := range cols {
		w := c.Min
		if sumWeight > 0 && c.Weight > 0 {
			add := extra * c.Weight / sumWeight
			w += add
			given += add
			lastWeighted = i
		}
		out[i] = table.Column{Title: c.Title, Width: w}
	}
	// Hand integer-division leftovers to the last weighted column.
	if lastWeighted >= 0 {
		out[lastWeighted].Width += extra - given
	}
	return out
}

// Clamp bounds v to [lo, hi].
func Clamp(v, lo, hi int) int {
	return min(max(v, lo), hi)
}
