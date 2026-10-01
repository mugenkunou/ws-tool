// Package choose is a single-choice list in a modal. enter picks the
// highlighted option; esc cancels. The owner receives a ResultMsg.
package choose

import (
	"strings"
	"sync/atomic"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/mugenkunou/ws-tool/internal/tui/keys"
	"github.com/mugenkunou/ws-tool/internal/tui/theme"
)

var nextID atomic.Int64

// Option is one choice.
type Option struct {
	Label  string
	Detail string // muted text after the label
}

// ResultMsg reports the chosen index, or Cancelled.
type ResultMsg struct {
	ID        int64
	Index     int
	Cancelled bool
}

// Model is the chooser state.
type Model struct {
	id      int64
	title   string
	hint    string
	options []Option
	cursor  int
	keys    keys.ChooseMap
	width   int
	height  int
}

// New creates a chooser.
func New(title, hint string, options []Option) Model {
	return Model{id: nextID.Add(1), title: title, hint: hint, options: options, keys: keys.Choose()}
}

// ID identifies this chooser; owners match it against ResultMsg.ID.
func (m Model) ID() int64 { return m.id }

// SetSize fits the chooser into width×height cells.
func (m Model) SetSize(width, height int) Model {
	m.width, m.height = width, height
	return m
}

// Update handles navigation and choice.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	id := m.id
	switch {
	case key.Matches(k, m.keys.Cancel):
		return m, func() tea.Msg { return ResultMsg{ID: id, Cancelled: true} }
	case key.Matches(k, m.keys.Up):
		m.cursor = max(m.cursor-1, 0)
	case key.Matches(k, m.keys.Down):
		m.cursor = min(m.cursor+1, max(len(m.options)-1, 0))
	case key.Matches(k, m.keys.Choose):
		if len(m.options) == 0 {
			return m, func() tea.Msg { return ResultMsg{ID: id, Cancelled: true} }
		}
		i := m.cursor
		return m, func() tea.Msg { return ResultMsg{ID: id, Index: i} }
	}
	return m, nil
}

// View renders the chooser.
func (m Model) View() string {
	head := []string{theme.Title.Render(m.title)}
	if m.hint != "" {
		head = append(head, theme.Muted.Render(m.hint))
	}
	head = append(head, "")

	var rows []string
	for i, o := range m.options {
		line := "  " + o.Label
		if i == m.cursor {
			line = theme.Bold.Render("› " + o.Label)
		}
		if o.Detail != "" {
			line += "  " + theme.Muted.Render(o.Detail)
		}
		rows = append(rows, line)
	}

	avail := max(m.height-len(head), 1)
	start := 0
	if m.cursor >= avail {
		start = m.cursor - avail + 1
	}
	rows = rows[start:min(start+avail, len(rows))]
	return lipgloss.NewStyle().MaxWidth(m.width).MaxHeight(m.height).
		Render(strings.Join(append(head, rows...), "\n"))
}

// KeyMap describes the bindings for the help bar.
func (m Model) KeyMap() help.KeyMap { return m.keys }
