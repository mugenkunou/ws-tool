package prompt

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/mugenkunou/ws-tool/internal/tui/tuitest"
)

func press(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	}
	return tea.KeyPressMsg{Code: []rune(s)[0], Text: s}
}

func typed(m Model, s string) Model {
	for _, r := range s {
		m, _ = m.Update(press(string(r)))
	}
	return m
}

func result(t *testing.T, m Model, k string) (Model, *ResultMsg) {
	t.Helper()
	m, cmd := m.Update(press(k))
	if cmd == nil {
		return m, nil
	}
	if r, ok := cmd().(ResultMsg); ok {
		return m, &r
	}
	return m, nil
}

func TestSubmitAndCancel(t *testing.T) {
	m := typed(New("Name").SetSize(40, 10), "  demo  ")
	_, r := result(t, m, "enter")
	if r == nil || r.Cancelled || r.Value != "demo" || r.ID != m.ID() {
		t.Fatalf("submit: %+v", r)
	}
	_, r = result(t, m, "esc")
	if r == nil || !r.Cancelled {
		t.Fatalf("cancel: %+v", r)
	}
}

func TestEmptyRejectedUnlessAllowed(t *testing.T) {
	m, r := result(t, New("Name").SetSize(40, 10), "enter")
	if r != nil {
		t.Fatal("empty value submitted")
	}
	if !strings.Contains(tuitest.Plain(m.View()), "a value is required") {
		t.Fatalf("no error shown:\n%s", m.View())
	}
	if _, r = result(t, New("Tag", AllowEmpty()).SetSize(40, 10), "enter"); r == nil || r.Value != "" {
		t.Fatalf("AllowEmpty: %+v", r)
	}
}

func TestValidate(t *testing.T) {
	m := New("N", WithValidate(func(v string) error {
		if v == "bad" {
			return errors.New("no bad names")
		}
		return nil
	})).SetSize(40, 10)
	m, r := result(t, typed(m, "bad"), "enter")
	if r != nil || !strings.Contains(tuitest.Plain(m.View()), "no bad names") {
		t.Fatalf("validation not enforced: %+v\n%s", r, m.View())
	}
}

func TestSuggestionsFilterAndComplete(t *testing.T) {
	m := New("Open", WithSuggestions([]string{"CA-incident", "proxy-timeout", "ca-llm"})).SetSize(40, 10)
	m = typed(m, "ca")
	v := tuitest.Plain(m.View())
	if !strings.Contains(v, "CA-incident") || !strings.Contains(v, "ca-llm") || strings.Contains(v, "proxy") {
		t.Fatalf("filtering wrong:\n%s", v)
	}
	m, _ = m.Update(press("tab"))
	if _, r := result(t, m, "enter"); r == nil || r.Value != "CA-incident" {
		t.Fatalf("tab did not complete to the first match: %+v", r)
	}
}

func TestSeparatorCompletesLastSegment(t *testing.T) {
	m := New("Tags", WithSuggestions([]string{"network", "dns"}), WithSeparator(",")).SetSize(40, 10)
	m = typed(m, "dns, net")
	m, _ = m.Update(press("tab"))
	if _, r := result(t, m, "enter"); r == nil || r.Value != "dns, network" {
		t.Fatalf("separator completion: %+v", r)
	}
}

func TestCompleterRunsAsCommandAndDropsStale(t *testing.T) {
	calls := 0
	m := New("Path", WithCompleter(func(v string) []string {
		calls++
		return []string{v + "x"}
	})).SetSize(40, 10)
	m, cmd := m.Update(press("a"))
	if calls != 0 {
		t.Fatal("completer ran inside Update")
	}
	stale := cmd // results for "a"
	m, cmd = m.Update(press("b"))
	m, _ = m.Update(findSuggestions(t, cmd))
	m, _ = m.Update(findSuggestions(t, stale)) // arrives late; must be ignored
	if v := tuitest.Plain(m.View()); !strings.Contains(v, "abx") || strings.Contains(v, " ax") {
		t.Fatalf("stale completion applied:\n%s", v)
	}
}

// findSuggestions runs cmd (possibly a batch) and returns its suggestionsMsg.
func findSuggestions(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	queue := []tea.Cmd{cmd}
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		switch msg := c().(type) {
		case tea.BatchMsg:
			queue = append(queue, msg...)
		case suggestionsMsg:
			return msg
		}
	}
	t.Fatal("no suggestionsMsg produced")
	return nil
}
