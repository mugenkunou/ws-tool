// Package listview is a table that knows its load state (loading, error,
// empty, ready) and sizes its columns from the space it is given. Screens
// embed it and drive it through its methods; it holds no domain knowledge.
package listview

import (
	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/mugenkunou/ws-tool/internal/tui/keys"
	"github.com/mugenkunou/ws-tool/internal/tui/layout"
	"github.com/mugenkunou/ws-tool/internal/tui/theme"
)

type state int

const (
	stateLoading state = iota
	stateReady
	stateError
)

// Model is the list's state.
type Model struct {
	cols   []layout.Col
	empty  string
	table  table.Model
	rows   []table.Row // styled rows as given; the cursor row is shown plain
	marked []bool      // rows drawn as marked (the cursor bar takes the mark colour)
	state  state
	err    error
	width  int
	height int
}

// New creates a list in the loading state. empty is shown when it has no rows.
func New(cols []layout.Col, empty string) Model {
	t := table.New(table.WithFocused(true))
	t.KeyMap = keys.Table()
	t.SetStyles(theme.Table())
	m := Model{cols: cols, empty: empty, table: t}
	return m.SetSize(0, 0)
}

// SetSize fits the list into width×height cells.
func (m Model) SetSize(width, height int) Model {
	m.width, m.height = width, height
	m.table.SetColumns(layout.Columns(width, m.cols))
	m.table.SetWidth(width)
	m.table.SetHeight(max(height, 1))
	return m
}

// SetTitles renames the columns (one title per column), keeping their sizes.
func (m Model) SetTitles(titles []string) Model {
	cols := make([]layout.Col, len(m.cols))
	copy(cols, m.cols)
	for i := range cols {
		if i < len(titles) {
			cols[i].Title = titles[i]
		}
	}
	m.cols = cols
	return m.SetSize(m.width, m.height)
}

// SetLoading marks a reload in progress. Existing rows stay visible.
func (m Model) SetLoading() Model {
	if m.state == stateError {
		m.state = stateLoading
	}
	return m
}

// SetRows replaces the rows and marks the list ready. Each row must have one
// cell per column.
func (m Model) SetRows(rows []table.Row) Model {
	m.rows = rows
	m.table.SetRows(rows)
	if m.table.Cursor() < 0 && len(rows) > 0 {
		m.table.SetCursor(0)
	}
	m.state, m.err = stateReady, nil
	return m.plainCursorRow()
}

// SetMarked flags rows (by index) as marked; set it before SetRows. Marked
// rows keep whatever styling SetRows gives them; under the cursor the bar is
// drawn in the mark colour.
func (m Model) SetMarked(marked []bool) Model {
	m.marked = marked
	return m
}

// plainCursorRow strips cell styling from the selected row so the
// selection highlight spans the whole row (inner styles would reset it).
func (m Model) plainCursorRow() Model {
	c := m.table.Cursor()
	if c < 0 || c >= len(m.rows) {
		return m
	}
	st := theme.Table()
	if c < len(m.marked) && m.marked[c] {
		st.Selected = theme.SelectedMarked()
	}
	m.table.SetStyles(st)
	shown := make([]table.Row, len(m.rows))
	copy(shown, m.rows)
	plain := make(table.Row, len(m.rows[c]))
	for i, cell := range m.rows[c] {
		plain[i] = ansi.Strip(cell)
	}
	shown[c] = plain
	m.table.SetRows(shown)
	return m
}

// SetError records a load failure.
func (m Model) SetError(err error) Model {
	m.state, m.err = stateError, err
	return m
}

// Update handles cursor movement keys.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	before := m.table.Cursor()
	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	if m.table.Cursor() != before {
		m = m.plainCursorRow()
	}
	return m, cmd
}

// SetCursor selects row i (clamped).
func (m Model) SetCursor(i int) Model {
	if len(m.rows) == 0 {
		return m
	}
	m.table.SetCursor(min(max(i, 0), len(m.rows)-1))
	return m.plainCursorRow()
}

// Cursor is the selected row index, or -1 when there are no rows.
func (m Model) Cursor() int {
	if len(m.table.Rows()) == 0 {
		return -1
	}
	return m.table.Cursor()
}

// Loaded reports whether data (or an error) has arrived at least once.
func (m Model) Loaded() bool { return m.state != stateLoading }

// View renders the list into at most width×height cells.
func (m Model) View() string {
	line := lipgloss.NewStyle().MaxWidth(m.width)
	switch {
	case m.state == stateLoading && len(m.table.Rows()) == 0:
		return line.Render(theme.Muted.Render(theme.Icon("⏳") + "Loading…"))
	case m.state == stateError:
		return lipgloss.NewStyle().Width(m.width).MaxHeight(m.height).
			Render(theme.Error.Render("Error: " + m.err.Error()))
	case len(m.table.Rows()) == 0:
		return line.Render(theme.Muted.Render(m.empty))
	}
	return m.table.View()
}
