package choose

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func press(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	}
	return tea.KeyPressMsg{Code: []rune(s)[0], Text: s}
}

func result(t *testing.T, m Model, k string) ResultMsg {
	t.Helper()
	_, cmd := m.Update(press(k))
	if cmd == nil {
		t.Fatalf("%s: no command", k)
	}
	r, ok := cmd().(ResultMsg)
	if !ok {
		t.Fatalf("%s: not a ResultMsg", k)
	}
	return r
}

func diverged() Model {
	return New("/w/bruno has diverged (2 local, 1 remote commit)", "Pull with", []Option{
		{Label: "rebase", Key: "r"}, {Label: "merge", Key: "m"}, {Label: "skip this repo", Key: "s"},
	}).SetSize(60, 10)
}

func TestOptionLetterPicksAtOnce(t *testing.T) {
	m := diverged()
	for letter, want := range map[string]int{"r": 0, "m": 1, "s": 2} {
		if r := result(t, m, letter); r.Cancelled || r.Index != want || r.ID != m.ID() {
			t.Errorf("%s: %+v, want index %d", letter, r, want)
		}
	}
}

func TestEnterEscAndArrowsStillWork(t *testing.T) {
	m := diverged()
	if r := result(t, m, "enter"); r.Index != 0 {
		t.Errorf("enter on the first option: %+v", r)
	}
	moved, _ := m.Update(press("down"))
	if r := result(t, moved, "enter"); r.Index != 1 {
		t.Errorf("down then enter: %+v", r)
	}
	if r := result(t, m, "esc"); !r.Cancelled {
		t.Errorf("esc cancels: %+v", r)
	}
	// A letter that is no option's does nothing.
	if _, cmd := m.Update(press("z")); cmd != nil {
		t.Error("z picks nothing")
	}
}

func TestViewShowsTheLetters(t *testing.T) {
	v := ansi.Strip(diverged().View())
	for _, want := range []string{"[r] rebase", "[m] merge", "[s] skip this repo", "has diverged (2 local, 1 remote commit)"} {
		if !strings.Contains(v, want) {
			t.Errorf("view lacks %q:\n%s", want, v)
		}
	}
}
