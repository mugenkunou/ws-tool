package cmdbar

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func kp(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	}
	return tea.KeyPressMsg{Code: []rune(s)[0], Text: s}
}

func typed(m Model, s string) Model {
	for _, r := range s {
		m, _ = m.Update(kp(string(r)))
	}
	return m
}

var cands = []string{"push", "backup status", "repo", "repos", "symlink", "symlinks", "dashboard", "config show", "q"}

func TestMatchesAreProperPrefixesIgnoringCase(t *testing.T) {
	m := typed(New("", cands), "sy")
	if got := strings.Join(m.Matches(), ","); got != "symlink,symlinks" {
		t.Fatalf("matches = %s", got)
	}
	m = typed(New("", cands), "SYMLINK")
	if got := strings.Join(m.Matches(), ","); got != "symlinks" {
		t.Fatalf("an exact match is not offered again: %s", got)
	}
	if got := typed(New("", cands), "zzz").Matches(); len(got) != 0 {
		t.Fatalf("matches = %v", got)
	}
}

func TestTabCompletesTheFirstMatch(t *testing.T) {
	m := typed(New("", cands), "sy")
	m, _ = m.Update(kp("tab"))
	if m.Value() != "symlink" {
		t.Fatalf("value = %q", m.Value())
	}
	// Nothing to complete: tab leaves the text.
	m = typed(New("", cands), "zzz")
	m, _ = m.Update(kp("tab"))
	if m.Value() != "zzz" {
		t.Fatalf("value = %q", m.Value())
	}
}

func TestEnterSubmitsAndEscCancels(t *testing.T) {
	m := typed(New("", cands), " backup push ")
	_, cmd := m.Update(kp("enter"))
	r := cmd().(ResultMsg)
	if r.ID != m.ID() || r.Line != "backup push" || r.Cancelled {
		t.Fatalf("%+v", r)
	}
	_, cmd = m.Update(kp("esc"))
	if r := cmd().(ResultMsg); !r.Cancelled || r.ID != m.ID() {
		t.Fatalf("%+v", r)
	}
}

func TestBackspaceAndCtrlU(t *testing.T) {
	m := typed(New("", cands), "syl")
	m, _ = m.Update(kp("backspace"))
	if m.Value() != "sy" {
		t.Fatalf("value = %q", m.Value())
	}
	m, _ = m.Update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	if m.Value() != "" {
		t.Fatalf("value = %q", m.Value())
	}
}

func TestPrefill(t *testing.T) {
	m := New("symlink add ~/.password-store --dir", cands)
	if m.Value() != "symlink add ~/.password-store --dir" {
		t.Fatalf("value = %q", m.Value())
	}
	_, cmd := m.Update(kp("enter"))
	if r := cmd().(ResultMsg); r.Line != "symlink add ~/.password-store --dir" {
		t.Fatalf("a prefilled line can be run as it is: %+v", r)
	}
}

func TestViewShowsCompletionsDimmedAndClips(t *testing.T) {
	m := typed(New("", cands), "sy").SetWidth(80)
	v := ansi.Strip(m.View())
	if !strings.HasPrefix(v, ":sy") || !strings.Contains(v, "mlink") || !strings.Contains(v, "symlinks") {
		t.Fatalf("view = %q", v)
	}
	if lipgloss.Height(m.View()) != 1 {
		t.Fatal("the bar is one line")
	}
	for _, w := range []int{20, 8, 3} {
		if got := lipgloss.Width(m.SetWidth(w).View()); got > w {
			t.Errorf("width %d: drew %d columns", w, got)
		}
	}
}

func TestClosest(t *testing.T) {
	for typed, want := range map[string]string{"symlin": "symlink", "symlnk": "symlink", "bakup status": "backup status", "xyzzyx": ""} {
		if got := Closest(typed, cands); got != want {
			t.Errorf("Closest(%q) = %q, want %q", typed, got, want)
		}
	}
}
