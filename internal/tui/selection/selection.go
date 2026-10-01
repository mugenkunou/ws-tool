// Package selection implements mouse text selection over a rendered frame,
// the way terminals do it: from the press cell to the release cell, reading
// left-to-right, top-to-bottom. Positions are cells on the rendered frame
// (what the user sees), so extraction and highlighting work on the frame
// string itself and stay correct for wide characters such as emoji.
package selection

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Point is a cell on the rendered frame: column X, row Y (0-based).
type Point struct{ X, Y int }

// Range is a selection from Anchor (where the drag started) to Head (where
// it is now). Either may come first.
type Range struct{ Anchor, Head Point }

// Empty reports whether nothing would be selected (a click, not a drag).
func (r Range) Empty() bool { return r.Anchor == r.Head }

// ordered returns the range's endpoints in reading order.
func (r Range) ordered() (start, end Point) {
	a, b := r.Anchor, r.Head
	if b.Y < a.Y || (b.Y == a.Y && b.X < a.X) {
		a, b = b, a
	}
	return a, b
}

// span is the half-open cell range [left, right) selected on row y of a
// line w cells wide; ok is false when the row is outside the selection.
func (r Range) span(y, w int) (left, right int, ok bool) {
	start, end := r.ordered()
	if y < start.Y || y > end.Y {
		return 0, 0, false
	}
	left, right = 0, w
	if y == start.Y {
		left = start.X
	}
	if y == end.Y {
		right = min(end.X+1, w) // the end cell is included
	}
	return left, right, left < right
}

// Extract returns the plain text under the selection. Trailing spaces are
// trimmed from each line, as terminals do.
func Extract(frame string, r Range) string {
	if r.Empty() {
		return ""
	}
	lines := strings.Split(frame, "\n")
	start, end := r.ordered()
	var out []string
	for y := start.Y; y <= end.Y && y < len(lines); y++ {
		if y < 0 {
			continue
		}
		plain := ansi.Strip(lines[y])
		left, right, ok := r.span(y, ansi.StringWidth(plain))
		if !ok {
			out = append(out, "")
			continue
		}
		out = append(out, strings.TrimRight(ansi.Cut(plain, left, right), " "))
	}
	return strings.TrimRight(strings.Join(out, "\n"), "\n")
}

// Highlight returns frame with the selected cells redrawn in style. Cells
// outside the selection keep their original styling.
func Highlight(frame string, r Range, style lipgloss.Style) string {
	if r.Empty() {
		return frame
	}
	lines := strings.Split(frame, "\n")
	for y := range lines {
		w := ansi.StringWidth(lines[y])
		left, right, ok := r.span(y, w)
		if !ok {
			continue
		}
		before := ansi.Cut(lines[y], 0, left)
		mid := ansi.Strip(ansi.Cut(lines[y], left, right))
		after := ansi.Cut(lines[y], right, w)
		lines[y] = before + style.Render(mid) + after
	}
	return strings.Join(lines, "\n")
}
