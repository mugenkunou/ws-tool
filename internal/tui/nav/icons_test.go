package nav

import (
	"strings"
	"testing"
)

// A variation selector (U+FE0F) makes lipgloss count the icon as 2 cells while
// many terminals draw it in 1, which pushes a card's right border out of line.
func TestIconsHaveNoVariationSelector(t *testing.T) {
	for _, s := range Screens() {
		if strings.ContainsRune(s.Icon(), '️') {
			t.Errorf("%s icon %q contains U+FE0F; pick an emoji that is wide without it", s.Spec().Title, s.Icon())
		}
	}
}
