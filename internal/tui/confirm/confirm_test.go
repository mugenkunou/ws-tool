package confirm

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/mugenkunou/ws-tool/internal/plan"
	"github.com/mugenkunou/ws-tool/internal/tui/tuitest"
)

func press(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	}
	return tea.KeyPressMsg{Code: []rune(s)[0], Text: s}
}

// recorder builds a plan whose actions append their ID to *ran.
func recorder(ran *[]string, fail map[string]bool, ids ...string) plan.Plan {
	p := plan.Plan{Command: "test"}
	for _, id := range ids {
		p.Actions = append(p.Actions, plan.Action{
			ID:          id,
			Description: "Do " + id,
			Execute: func() error {
				*ran = append(*ran, id)
				if fail[id] {
					return errors.New(id + " broke")
				}
				return nil
			},
		})
	}
	return p
}

// run presses keys, executing every returned command and feeding results
// back, until done; it returns the final model and the DoneMsg, if any.
func run(m Model, keys ...string) (Model, *DoneMsg) {
	var done *DoneMsg
	feed := func(cmd tea.Cmd) {
		for cmd != nil {
			msg := cmd()
			if d, ok := msg.(DoneMsg); ok {
				done = &d
				return
			}
			m, cmd = m.Update(msg)
		}
	}
	for _, k := range keys {
		var cmd tea.Cmd
		m, cmd = m.Update(press(k))
		feed(cmd)
	}
	return m, done
}

func TestApplyRunsCheckedActionsInOrder(t *testing.T) {
	var ran []string
	m := New("T", recorder(&ran, nil, "a", "b", "c")).SetSize(60, 10)
	m, done := run(m, "down", "space", "enter") // untick b, apply
	if got := strings.Join(ran, ","); got != "a,c" {
		t.Fatalf("ran %q, want a,c", got)
	}
	if done != nil {
		t.Fatal("DoneMsg sent before the user closed the results")
	}
	_, done = run(m, "enter")
	if done == nil {
		t.Fatal("enter after finishing did not close")
	}
	want := map[string]string{"a": plan.StatusExecuted, "b": plan.StatusSkipped, "c": plan.StatusExecuted}
	for _, a := range done.Result.Actions {
		if a.Status != want[a.ID] {
			t.Errorf("%s: status %q, want %q", a.ID, a.Status, want[a.ID])
		}
	}
	if done.Result.Aborted {
		t.Error("result marked aborted")
	}
}

func TestFailureIsRecordedAndOthersContinue(t *testing.T) {
	var ran []string
	m := New("T", recorder(&ran, map[string]bool{"a": true}, "a", "b")).SetSize(60, 10)
	m, _ = run(m, "enter")
	_, done := run(m, "enter")
	if strings.Join(ran, ",") != "a,b" {
		t.Fatalf("ran %v", ran)
	}
	if !done.Result.HasFailures() || done.Result.Actions[0].Error != "a broke" {
		t.Fatalf("failure not recorded: %+v", done.Result.Actions)
	}
	if done.Result.ExitCode() != 3 {
		t.Errorf("exit code %d, want 3 (partial)", done.Result.ExitCode())
	}
}

func TestCancelRunsNothing(t *testing.T) {
	var ran []string
	m := New("T", recorder(&ran, nil, "a", "b")).SetSize(60, 10)
	_, done := run(m, "esc")
	if len(ran) != 0 {
		t.Fatalf("cancel ran %v", ran)
	}
	if done == nil || !done.Result.Aborted {
		t.Fatal("cancel did not report an aborted result")
	}
}

func TestToggleAllAndWithChecked(t *testing.T) {
	var ran []string
	m := New("T", recorder(&ran, nil, "a", "b", "c")).
		WithChecked(func(i int, _ plan.Action) bool { return i == 1 }).SetSize(60, 10)
	m, _ = run(m, "enter")
	if strings.Join(ran, ",") != "b" {
		t.Fatalf("WithChecked: ran %v, want b", ran)
	}

	ran = nil
	m = New("T", recorder(&ran, nil, "a", "b")).SetSize(60, 10)
	m, _ = run(m, "a", "enter") // all checked → none
	if len(ran) != 0 {
		t.Fatalf("a (none): ran %v", ran)
	}
	ran = nil
	m = New("T", recorder(&ran, nil, "a", "b")).WithChecked(func(int, plan.Action) bool { return false }).SetSize(60, 10)
	m, _ = run(m, "a", "enter") // none → all
	if strings.Join(ran, ",") != "a,b" {
		t.Fatalf("a (all): ran %v", ran)
	}
}

func TestStaleStepIgnored(t *testing.T) {
	var ran []string
	m := New("T", recorder(&ran, nil, "a")).SetSize(60, 10)
	other := New("T", recorder(&ran, nil, "a"))
	m, _ = m.Update(stepMsg{id: other.ID(), index: 0})
	if m.status[0] != "" {
		t.Fatal("step from another checklist was applied")
	}
}

func TestEmptyPlan(t *testing.T) {
	m := New("Sync", plan.Plan{}).SetSize(60, 10)
	if !strings.Contains(tuitest.Plain(m.View()), "Nothing to do") {
		t.Fatalf("empty plan view:\n%s", m.View())
	}
	if _, done := run(m, "enter"); done == nil || !done.Result.Aborted {
		t.Fatal("enter on an empty plan should close it")
	}
}

func TestViewKeepsCursorVisible(t *testing.T) {
	var ran []string
	ids := []string{"a", "b", "c", "d", "e", "f", "g", "h"}
	m := New("T", recorder(&ran, nil, ids...)).SetSize(40, 5)
	m, _ = run(m, "down", "down", "down", "down", "down", "down", "down")
	v := tuitest.Plain(m.View())
	if !strings.Contains(v, "Do h") {
		t.Fatalf("cursor row scrolled out of view:\n%s", v)
	}
	tuitest.AssertFits(t, m.View(), 40, 5)
}

func TestSummary(t *testing.T) {
	r := plan.PlanResult{Actions: []plan.ActionStatus{
		{Status: plan.StatusExecuted}, {Status: plan.StatusFailed}, {Status: plan.StatusSkipped},
	}}
	if got := Summary(r); got != "1 done · 1 failed · 1 skipped" {
		t.Fatalf("Summary = %q", got)
	}
	if got := Summary(plan.PlanResult{Aborted: true}); !strings.HasPrefix(got, "Cancelled") {
		t.Fatalf("aborted Summary = %q", got)
	}
}
