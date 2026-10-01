package selection

import (
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const frame = "hello world  \n\x1b[1mbold\x1b[0m line two\n📦 Repos  ok"

func TestExtractSingleLine(t *testing.T) {
	if got := Extract(frame, Range{Point{6, 0}, Point{10, 0}}); got != "world" {
		t.Fatalf("got %q", got)
	}
	// Reversed drag selects the same text.
	if got := Extract(frame, Range{Point{10, 0}, Point{6, 0}}); got != "world" {
		t.Fatalf("reversed: got %q", got)
	}
}

func TestExtractMultiLineStripsStylesAndTrailingSpace(t *testing.T) {
	got := Extract(frame, Range{Point{6, 0}, Point{3, 1}})
	if got != "world\nbold" {
		t.Fatalf("got %q", got)
	}
}

func TestExtractWideCharacters(t *testing.T) {
	// 📦 occupies cells 0-1; "Repos" starts at cell 3.
	if got := Extract(frame, Range{Point{0, 2}, Point{7, 2}}); got != "📦 Repos" {
		t.Fatalf("got %q", got)
	}
	if got := Extract(frame, Range{Point{3, 2}, Point{7, 2}}); got != "Repos" {
		t.Fatalf("got %q", got)
	}
}

func TestClickIsEmpty(t *testing.T) {
	r := Range{Point{3, 1}, Point{3, 1}}
	if !r.Empty() || Extract(frame, r) != "" {
		t.Fatal("a click must not select anything")
	}
	if Highlight(frame, r, lipgloss.NewStyle().Reverse(true)) != frame {
		t.Fatal("a click must not change the frame")
	}
}

func TestHighlightKeepsTextAndWidth(t *testing.T) {
	r := Range{Point{6, 0}, Point{3, 2}}
	out := Highlight(frame, r, lipgloss.NewStyle().Reverse(true))
	if ansi.Strip(out) != ansi.Strip(frame) {
		t.Fatalf("highlight changed the text:\n%q\n%q", ansi.Strip(out), ansi.Strip(frame))
	}
	if out == frame {
		t.Fatal("nothing highlighted")
	}
}
