package layout

import (
	"testing"

	"github.com/mugenkunou/ws-tool/internal/tui/theme"
)

func total(cols []Col, width int) int {
	sum := 0
	for _, c := range Columns(width, cols) {
		sum += c.Width + theme.TableCellPadding
	}
	return sum
}

func TestColumnsFillWidth(t *testing.T) {
	cols := []Col{{Title: "a", Min: 10, Weight: 2}, {Title: "b", Min: 5}, {Title: "c", Min: 8, Weight: 1}}
	for _, w := range []int{29, 40, 80, 133} {
		if got := total(cols, w); got != w {
			t.Errorf("width %d: columns use %d cells", w, got)
		}
	}
	got := Columns(80, cols)
	if got[1].Width != 5 {
		t.Errorf("fixed column grew to %d", got[1].Width)
	}
	if got[0].Width <= got[2].Width {
		t.Errorf("weight 2 column (%d) not wider than weight 1 column (%d)", got[0].Width, got[2].Width)
	}
}

func TestColumnsNeverOverflow(t *testing.T) {
	cols := []Col{{Title: "a", Min: 10, Weight: 2}, {Title: "b", Min: 5}, {Title: "c", Min: 8, Weight: 1}}
	for w := 0; w < 40; w++ {
		for _, c := range Columns(w, cols) {
			if c.Width < 0 {
				t.Fatalf("width %d: negative column width", w)
			}
		}
		if w >= len(cols)*theme.TableCellPadding {
			if got := total(cols, w); got > w {
				t.Errorf("width %d: columns use %d cells", w, got)
			}
		}
	}
}
