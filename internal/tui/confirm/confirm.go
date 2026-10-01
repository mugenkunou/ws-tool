// Package confirm is the TUI rendering of the Action Plan pattern: a
// checklist of a plan's actions, all pre-checked. The user toggles actions,
// applies, watches each result arrive, then closes.
//
// Phases: review → running → done.
//   - review:  space toggles, a toggles all, enter applies, esc cancels
//   - running: keys are ignored until every checked action has finished
//   - done:    enter or esc closes
//
// Actions run one at a time, each in its own tea.Cmd, so Update never blocks.
// The owner receives a DoneMsg (with the PlanResult) when the user closes or
// cancels.
package confirm

import (
	"fmt"
	"strings"
	"sync/atomic"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/mugenkunou/ws-tool/internal/plan"
	"github.com/mugenkunou/ws-tool/internal/tui/keys"
	"github.com/mugenkunou/ws-tool/internal/tui/nav"
	"github.com/mugenkunou/ws-tool/internal/tui/theme"
)

var nextID atomic.Int64

type phase int

const (
	phaseReview phase = iota
	phaseRunning
	phaseDone
)

// DoneMsg reports the plan's outcome. Result.Aborted is true when the user
// cancelled during review (no action ran).
type DoneMsg struct {
	ID     int64
	Result plan.PlanResult
}

// stepMsg carries one action's outcome back to the checklist that ran it.
type stepMsg struct {
	id    int64
	index int
	err   error
}

// Model is the checklist state.
type Model struct {
	id      int64
	title   string
	note    string // optional context shown under the title (e.g. warnings)
	plan    plan.Plan
	checked []bool
	status  []string // per action: "" (pending) or a plan.Status* value
	errs    []string
	cursor  int
	phase   phase
	keys    keys.ConfirmMap
	frame   int // animation frame for the running action
	width   int
	height  int
}

// New creates a checklist for p, every action checked.
func New(title string, p plan.Plan) Model {
	n := len(p.Actions)
	checked := make([]bool, n)
	for i := range checked {
		checked[i] = true
	}
	return Model{
		id:      nextID.Add(1),
		title:   title,
		plan:    p,
		checked: checked,
		status:  make([]string, n),
		errs:    make([]string, n),
		keys:    keys.Confirm(),
	}
}

// WithChecked sets which actions start checked (default: all).
func (m Model) WithChecked(checked func(i int, a plan.Action) bool) Model {
	c := make([]bool, len(m.plan.Actions))
	for i, a := range m.plan.Actions {
		c[i] = checked(i, a)
	}
	m.checked = c
	return m
}

// WithNote adds a context line (or lines) under the title.
func (m Model) WithNote(note string) Model {
	m.note = note
	return m
}

// ID identifies this checklist; owners match it against DoneMsg.ID.
func (m Model) ID() int64 { return m.id }

// SetSize fits the checklist into width×height cells.
func (m Model) SetSize(width, height int) Model {
	m.width, m.height = width, height
	return m
}

// Update handles keys and action results.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case nav.FrameMsg:
		m.frame = msg.N
		return m, nil
	case stepMsg:
		if msg.id != m.id || m.phase != phaseRunning {
			return m, nil
		}
		status := append([]string(nil), m.status...)
		errs := append([]string(nil), m.errs...)
		if msg.err != nil {
			status[msg.index] = plan.StatusFailed
			errs[msg.index] = msg.err.Error()
		} else {
			status[msg.index] = plan.StatusExecuted
		}
		m.status, m.errs = status, errs
		return m.runNext(msg.index + 1)

	case tea.KeyPressMsg:
		switch m.phase {
		case phaseReview:
			return m.updateReview(msg)
		case phaseDone:
			if key.Matches(msg, m.keys.Close) {
				return m, m.done(false)
			}
		}
	}
	return m, nil
}

func (m Model) updateReview(msg tea.KeyPressMsg) (Model, tea.Cmd) {
	n := len(m.plan.Actions)
	switch {
	case key.Matches(msg, m.keys.Cancel):
		return m, m.done(true)
	case key.Matches(msg, m.keys.Up):
		m.cursor = max(m.cursor-1, 0)
	case key.Matches(msg, m.keys.Down):
		m.cursor = min(m.cursor+1, max(n-1, 0))
	case key.Matches(msg, m.keys.Toggle):
		if n > 0 {
			m.checked = toggled(m.checked, m.cursor)
		}
	case key.Matches(msg, m.keys.All):
		all := !allTrue(m.checked)
		checked := make([]bool, n)
		for i := range checked {
			checked[i] = all
		}
		m.checked = checked
	case key.Matches(msg, m.keys.Apply):
		if n == 0 {
			return m, m.done(true)
		}
		m.phase = phaseRunning
		status := make([]string, n)
		for i, c := range m.checked {
			if !c {
				status[i] = plan.StatusSkipped
			}
		}
		m.status = status
		return m.runNext(0)
	}
	return m, nil
}

// runNext starts the first checked action at or after i, or finishes.
func (m Model) runNext(i int) (Model, tea.Cmd) {
	for ; i < len(m.plan.Actions); i++ {
		if m.checked[i] {
			id, index, exec := m.id, i, m.plan.Actions[i].Execute
			return m, func() tea.Msg {
				return stepMsg{id: id, index: index, err: exec()}
			}
		}
	}
	m.phase = phaseDone
	return m, nil
}

func (m Model) done(aborted bool) tea.Cmd {
	res := plan.PlanResult{Command: m.plan.Command, Aborted: aborted}
	for i, a := range m.plan.Actions {
		st := m.status[i]
		if aborted || st == "" {
			st = plan.StatusSkipped
		}
		res.Actions = append(res.Actions, plan.ActionStatus{ID: a.ID, Status: st, Error: m.errs[i]})
	}
	id := m.id
	return func() tea.Msg { return DoneMsg{ID: id, Result: res} }
}

// Summary describes a finished result in one line, for a notice.
func Summary(r plan.PlanResult) string {
	var done, failed, skipped int
	for _, a := range r.Actions {
		switch a.Status {
		case plan.StatusExecuted:
			done++
		case plan.StatusFailed:
			failed++
		default:
			skipped++
		}
	}
	if r.Aborted {
		return "Cancelled — nothing changed"
	}
	parts := []string{fmt.Sprintf("%d done", done)}
	if failed > 0 {
		parts = append(parts, fmt.Sprintf("%d failed", failed))
	}
	if skipped > 0 {
		parts = append(parts, fmt.Sprintf("%d skipped", skipped))
	}
	return strings.Join(parts, " · ")
}

// View renders the checklist into at most width×height cells.
func (m Model) View() string {
	var head []string
	head = append(head, theme.Title.Render(m.title))
	if m.note != "" {
		head = append(head, theme.Muted.Render(m.note))
	}
	head = append(head, "")

	var lines []string
	if len(m.plan.Actions) == 0 {
		lines = append(lines, theme.OK.Render("✔ Nothing to do."))
	}
	for i, a := range m.plan.Actions {
		lines = append(lines, m.actionLine(i, a))
	}

	var foot string
	switch m.phase {
	case phaseRunning:
		foot = theme.Muted.Render(theme.Spinner(m.frame) + " Applying…")
	case phaseDone:
		foot = theme.Bold.Render(m.summaryLine())
	}

	// Keep the cursor (or the running action) visible when the list is long.
	avail := max(m.height-len(head)-2, 1)
	focus := m.cursor
	if m.phase != phaseReview {
		focus = m.lastStarted()
	}
	start := 0
	if focus >= avail {
		start = focus - avail + 1
	}
	end := min(start+avail, len(lines))
	visible := lines[start:end]

	out := strings.Join(append(head, visible...), "\n")
	if foot != "" {
		out += "\n\n" + foot
	}
	return lipgloss.NewStyle().MaxWidth(m.width).MaxHeight(m.height).Render(out)
}

func (m Model) actionLine(i int, a plan.Action) string {
	var mark string
	switch m.status[i] {
	case plan.StatusExecuted:
		mark = theme.OK.Render("✔")
	case plan.StatusFailed:
		mark = theme.Error.Render("✘")
	case plan.StatusSkipped:
		mark = theme.Muted.Render("–")
	default:
		if m.phase == phaseRunning && m.checked[i] {
			mark = theme.Warn.Render(theme.Spinner(m.frame))
		} else if m.checked[i] {
			mark = "[x]"
		} else {
			mark = theme.Muted.Render("[ ]")
		}
	}
	desc := a.Description
	if m.status[i] == plan.StatusSkipped || (m.phase == phaseReview && !m.checked[i]) {
		desc = theme.Muted.Render(desc)
	}
	line := mark + " " + desc
	if m.errs[i] != "" {
		line += theme.Error.Render(": " + firstLine(m.errs[i]))
	}
	if m.phase == phaseReview && i == m.cursor {
		line = theme.Bold.Render("›") + " " + line
	} else {
		line = "  " + line
	}
	return line
}

func (m Model) summaryLine() string {
	r := plan.PlanResult{}
	for i := range m.plan.Actions {
		r.Actions = append(r.Actions, plan.ActionStatus{Status: m.status[i]})
	}
	text := Summary(r)
	switch {
	case r.HasFailures():
		text = theme.Icon(theme.IconOops) + text
	case r.ExecutedCount() > 0:
		text = theme.Celebrate("All done — " + text)
	}
	return text + theme.Muted.Render("  (enter to close)")
}

func (m Model) lastStarted() int {
	last := 0
	for i, st := range m.status {
		if st != "" || (m.phase == phaseRunning && m.checked[i]) {
			last = i
			if st == "" {
				break
			}
		}
	}
	return last
}

// KeyMap describes the bindings for the help bar, which depend on the phase.
func (m Model) KeyMap() help.KeyMap {
	switch m.phase {
	case phaseRunning:
		return phaseKeys{}
	case phaseDone:
		return phaseKeys{m.keys.Close}
	}
	return m.keys
}

// phaseKeys is a fixed help list for the running and done phases.
type phaseKeys []key.Binding

func (k phaseKeys) ShortHelp() []key.Binding  { return k }
func (k phaseKeys) FullHelp() [][]key.Binding { return [][]key.Binding{k} }

func toggled(b []bool, i int) []bool {
	out := make([]bool, len(b))
	copy(out, b)
	out[i] = !out[i]
	return out
}

func allTrue(b []bool) bool {
	for _, v := range b {
		if !v {
			return false
		}
	}
	return true
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
