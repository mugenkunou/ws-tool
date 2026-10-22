// Package reslist is the list every resource screen shares: a listview over
// typed items described by a resource.Schema, with filtering (/), sorting
// (> <), k9s-style marking (space, ctrl+space range, ctrl+\ clear, A all)
// and an all-fields detail pane (i) built in, so
// each works the same on every screen. Screens supply the schema, per-column
// sizing and colour, and their actions, each declared with a Scope; Run
// hands an action the rows its scope covers (see actions.go).
//
// The filter uses the same syntax as the CLI's --filter (resource.ParseFilter)
// and the sort cycles through the same field keys as --sort.
package reslist

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/mugenkunou/ws-tool/internal/resource"
	"github.com/mugenkunou/ws-tool/internal/tui/keys"
	"github.com/mugenkunou/ws-tool/internal/tui/layout"
	"github.com/mugenkunou/ws-tool/internal/tui/listview"
	"github.com/mugenkunou/ws-tool/internal/tui/theme"
)

// Col sizes and colours one of the schema's columns.
type Col[T any] struct {
	Key    string
	Min    int
	Weight int
	Style  func(item T, text string) string // optional; colours the plain text
}

// Model is the list state.
type Model[T any] struct {
	schema     resource.Schema[T]
	cols       []Col[T]
	keys       keys.ListMap
	detailKeys keys.DetailMap
	list       listview.Model
	detail     viewport.Model
	showDetail bool

	all   []T
	shown []T
	ready bool // SetItems has been called
	marks map[string]bool
	decls []Decl // the screen's actions, for the status line

	filter  string // as typed
	before  string // filter when editing began; restored on cancel
	editing bool
	terms   []resource.Term
	sortKey int // index into schema.Fields; -1 keeps the loaded order
	desc    bool

	width  int
	height int
}

// New creates a list in the loading state. cols must name exactly the
// schema's Columns, in order; empty is shown when there are no items.
func New[T any](schema resource.Schema[T], cols []Col[T], empty string) Model[T] {
	if len(cols) != len(schema.Columns) {
		panic(fmt.Sprintf("reslist: %s has %d columns, layout has %d", schema.Name, len(schema.Columns), len(cols)))
	}
	lc := make([]layout.Col, len(cols))
	for i, c := range cols {
		f, ok := schema.Field(c.Key)
		if !ok || schema.Columns[i] != c.Key {
			panic(fmt.Sprintf("reslist: %s column %d is %q, layout has %q", schema.Name, i, schema.Columns[i], c.Key))
		}
		lc[i] = layout.Col{Title: f.Title, Min: c.Min, Weight: c.Weight}
	}
	dk := keys.Detail()
	vp := viewport.New()
	vp.KeyMap = dk.Viewport
	vp.SoftWrap = true
	return Model[T]{
		schema:     schema,
		cols:       cols,
		keys:       keys.List(),
		detailKeys: dk,
		list:       listview.New(lc, empty),
		detail:     vp,
		sortKey:    -1,
	}
}

// SetSize fits the list (status line included) into width×height cells.
func (m Model[T]) SetSize(width, height int) Model[T] {
	m.width, m.height = width, height
	m.detail.SetWidth(width)
	m.detail.SetHeight(height)
	return m.layout()
}

func (m Model[T]) layout() Model[T] {
	h := m.height
	if m.statusShown() {
		h--
	}
	m.list = m.list.SetSize(m.width, max(h, 0))
	return m
}

// SetItems replaces the items. Marks on items that are still present are
// kept, and the cursor stays on the same item when it can.
func (m Model[T]) SetItems(items []T) Model[T] {
	m.all, m.ready = items, true
	if len(m.marks) > 0 {
		present := make(map[string]bool, len(items))
		for _, it := range items {
			present[m.schema.ID(it)] = true
		}
		for id := range m.marks {
			if !present[id] {
				delete(m.marks, id)
			}
		}
	}
	return m.refresh()
}

// SetLoading marks a reload in progress; existing rows stay visible.
func (m Model[T]) SetLoading() Model[T] {
	m.list = m.list.SetLoading()
	return m
}

// SetError records a load failure.
func (m Model[T]) SetError(err error) Model[T] {
	m.list = m.list.SetError(err)
	return m
}

// Items returns every item, unfiltered, in loaded order.
func (m Model[T]) Items() []T { return m.all }

// Shown returns the items on screen, filtered and sorted.
func (m Model[T]) Shown() []T { return m.shown }

// Current returns the cursor item.
func (m Model[T]) Current() (T, bool) {
	i := m.list.Cursor()
	if i < 0 || i >= len(m.shown) {
		var zero T
		return zero, false
	}
	return m.shown[i], true
}

// marked returns the marked items on screen, in screen order.
func (m Model[T]) marked() []T {
	var out []T
	for _, it := range m.shown {
		if m.marks[m.schema.ID(it)] {
			out = append(out, it)
		}
	}
	return out
}

// ClearMarks unmarks everything.
func (m Model[T]) ClearMarks() Model[T] {
	m.marks = nil
	return m.refresh()
}

// Select moves the cursor to the first item satisfying pred, clearing the
// filter if it hides that item. It reports whether one was found.
func (m Model[T]) Select(pred func(T) bool) (Model[T], bool) {
	if m.showDetail || m.editing {
		m.showDetail, m.editing = false, false
		m = m.layout()
	}
	if i := indexOf(m.shown, pred); i >= 0 {
		m.list = m.list.SetCursor(i)
		return m, true
	}
	if indexOf(m.all, pred) < 0 {
		return m, false
	}
	m.filter, m.terms = "", nil
	m = m.refresh()
	m.list = m.list.SetCursor(indexOf(m.shown, pred))
	return m, true
}

func indexOf[T any](items []T, pred func(T) bool) int {
	for i, it := range items {
		if pred(it) {
			return i
		}
	}
	return -1
}

// Editing reports that the filter is being typed; the screen must then
// report CapturesKeys so global keys type into the filter.
func (m Model[T]) Editing() bool { return m.editing }

// InDetail reports that the all-fields pane is open.
func (m Model[T]) InDetail() bool { return m.showDetail }

// HandleKey handles the shared list keys. It reports false for keys the
// screen should handle (its actions, esc with nothing to clear, movement).
func (m Model[T]) HandleKey(k tea.KeyPressMsg) (Model[T], tea.Cmd, bool) {
	if m.editing {
		return m.editKey(k), nil, true
	}
	if m.showDetail {
		if key.Matches(k, m.detailKeys.Back, m.keys.Detail) {
			m.showDetail = false
			return m, nil, true
		}
		var cmd tea.Cmd
		m.detail, cmd = m.detail.Update(k)
		return m, cmd, true
	}
	switch {
	case key.Matches(k, m.keys.Filter):
		m.editing, m.before = true, m.filter
		return m.layout(), nil, true
	case key.Matches(k, m.keys.Sort):
		m.sortKey++
		if m.sortKey >= len(m.schema.Fields) {
			m.sortKey = -1
		}
		m.desc = false
		return m.refresh(), nil, true
	case key.Matches(k, m.keys.Reverse):
		if m.sortKey < 0 {
			m.sortKey = 0
		} else {
			m.desc = !m.desc
		}
		return m.refresh(), nil, true
	case key.Matches(k, m.keys.Mark):
		it, ok := m.Current()
		if !ok {
			return m, nil, true
		}
		id := m.schema.ID(it)
		if m.marks == nil {
			m.marks = make(map[string]bool)
		}
		if m.marks[id] {
			delete(m.marks, id)
		} else {
			m.marks[id] = true
		}
		return m.refresh(), nil, true
	case key.Matches(k, m.keys.MarkRange):
		return m.markRange(), nil, true
	case key.Matches(k, m.keys.MarkClear):
		return m.ClearMarks(), nil, true
	case key.Matches(k, m.keys.MarkAll):
		all := len(m.shown) > 0 && len(m.marked()) == len(m.shown)
		if m.marks == nil {
			m.marks = make(map[string]bool)
		}
		for _, it := range m.shown {
			if all {
				delete(m.marks, m.schema.ID(it))
			} else {
				m.marks[m.schema.ID(it)] = true
			}
		}
		return m.refresh(), nil, true
	case key.Matches(k, m.keys.Detail):
		it, ok := m.Current()
		if !ok {
			return m, nil, true
		}
		m.showDetail = true
		m.detail.SetContent(m.detailText(it))
		m.detail.GotoTop()
		return m, nil, true
	case key.Matches(k, m.keys.Clear):
		switch {
		case m.filter != "":
			m.filter, m.terms = "", nil
			return m.refresh(), nil, true
		case len(m.marked()) > 0:
			return m.ClearMarks(), nil, true
		}
	}
	return m, nil, false
}

// markRange marks the rows from the nearest marked row to the cursor, as
// k9s does: the nearest mark above the cursor, else below it. With no mark,
// it marks the cursor row.
func (m Model[T]) markRange() Model[T] {
	c := m.list.Cursor()
	if c < 0 || c >= len(m.shown) {
		return m
	}
	isMarked := func(i int) bool { return m.marks[m.schema.ID(m.shown[i])] }
	from := c
	for i := c - 1; i >= 0; i-- {
		if isMarked(i) {
			from = i
			break
		}
	}
	if from == c {
		for i := c + 1; i < len(m.shown); i++ {
			if isMarked(i) {
				from = i
				break
			}
		}
	}
	lo, hi := min(from, c), max(from, c)
	if m.marks == nil {
		m.marks = make(map[string]bool)
	}
	for i := lo; i <= hi; i++ {
		m.marks[m.schema.ID(m.shown[i])] = true
	}
	return m.refresh()
}

func (m Model[T]) editKey(k tea.KeyPressMsg) Model[T] {
	switch {
	case key.Matches(k, m.keys.Apply):
		m.editing = false
		return m.refresh()
	case key.Matches(k, m.keys.Cancel):
		m.editing, m.filter = false, m.before
	case k.Code == tea.KeyBackspace:
		if r := []rune(m.filter); len(r) > 0 {
			m.filter = string(r[:len(r)-1])
		}
	case k.Code == 'u' && k.Mod == tea.ModCtrl:
		m.filter = ""
	case k.Text != "":
		m.filter += k.Text
	default:
		return m
	}
	m.terms = resource.ParseFilter(m.filter)
	return m.refresh()
}

// Update handles cursor movement.
func (m Model[T]) Update(msg tea.Msg) (Model[T], tea.Cmd) {
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m Model[T]) sortKeys() []resource.SortKey {
	if m.sortKey < 0 {
		return nil
	}
	return []resource.SortKey{{Key: m.schema.Fields[m.sortKey].Key, Desc: m.desc}}
}

// refresh recomputes the shown items and rows, keeping the cursor item.
func (m Model[T]) refresh() Model[T] {
	var curID string
	cur, hadCur := m.Current()
	if hadCur {
		curID = m.schema.ID(cur)
	}
	m.shown = m.schema.Apply(m.all, resource.Query{Terms: m.terms, Sort: m.sortKeys()})

	rows := make([]table.Row, len(m.shown))
	marked := make([]bool, len(m.shown))
	for i, it := range m.shown {
		marked[i] = m.marks[m.schema.ID(it)]
		row := make(table.Row, len(m.cols))
		for j, c := range m.cols {
			f, _ := m.schema.Field(c.Key)
			text := f.Show(it)
			if c.Style != nil {
				text = c.Style(it, text)
			}
			if marked[i] { // k9s: the whole row in the mark colour
				text = theme.Marked.Render(ansi.Strip(text))
			}
			row[j] = text
		}
		rows[i] = row
	}

	titles := make([]string, len(m.cols))
	for i, c := range m.cols {
		f, _ := m.schema.Field(c.Key)
		titles[i] = f.Title
		if m.sortKey >= 0 && m.schema.Fields[m.sortKey].Key == c.Key {
			titles[i] += m.arrow()
		}
	}
	m.list = m.list.SetTitles(titles)
	if m.ready {
		m.list = m.list.SetMarked(marked).SetRows(rows)
	}
	m = m.layout()
	if hadCur {
		if i := indexOf(m.shown, func(it T) bool { return m.schema.ID(it) == curID }); i >= 0 {
			m.list = m.list.SetCursor(i)
		}
	}
	return m
}

func (m Model[T]) arrow() string {
	if m.desc {
		return " ↓"
	}
	return " ↑"
}

func (m Model[T]) statusShown() bool {
	return m.editing || m.filter != "" || m.sortKey >= 0 || len(m.marked()) > 0
}

func (m Model[T]) statusLine() string {
	sep := theme.Muted.Render(" · ")
	if m.editing {
		return theme.Bold.Render("/") + m.filter + theme.Muted.Render("▏") +
			theme.Muted.Render("   key=text · key!=text · text   enter keep · esc cancel")
	}
	var parts []string
	nMarked := len(m.marked())
	if m.filter != "" {
		part := theme.Muted.Render("filter ") + theme.Bold.Render(m.filter) +
			theme.Muted.Render(fmt.Sprintf(" (%d of %d)", len(m.shown), len(m.all)))
		if err := m.schema.Validate(resource.Query{Terms: m.terms}); err != nil {
			part += "  " + theme.Warn.Render(err.Error())
		} else if ks := m.scopedKeys(All); nMarked == 0 && len(ks) > 0 && len(m.shown) > 0 {
			part += theme.Muted.Render(fmt.Sprintf(" — %s act on these %d", strings.Join(ks, " "), len(m.shown)))
		}
		parts = append(parts, part)
	}
	if m.sortKey >= 0 {
		parts = append(parts, theme.Muted.Render("sort ")+theme.Bold.Render(m.schema.Fields[m.sortKey].Title+m.arrow()))
	}
	if nMarked > 0 {
		part := theme.Title.Render(fmt.Sprintf("%d marked", nMarked))
		if ks := m.scopedKeys(Selected, All); len(ks) > 0 {
			part += theme.Muted.Render(" — " + strings.Join(ks, " ") + " act on them")
		}
		parts = append(parts, part+theme.Muted.Render(" · "+m.keys.MarkClear.Help().Key+" clears"))
	}
	return strings.Join(parts, sep)
}

func (m Model[T]) detailText(it T) string {
	w := 0
	for _, f := range m.schema.Fields {
		w = max(w, lipgloss.Width(f.Title))
	}
	var b strings.Builder
	head := m.schema.Name
	if len(m.cols) > 0 {
		head += "  " + m.schema.Display(it, m.cols[0].Key)
	}
	b.WriteString(theme.Title.Render(head) + "\n\n")
	for _, f := range m.schema.Fields {
		b.WriteString(theme.Muted.Render(fmt.Sprintf("%-*s", w, f.Title)) + "  " + f.Show(it) + "\n")
	}
	b.WriteString("\n" + theme.Muted.Render("esc back"))
	return b.String()
}

// View renders the list (or the detail pane) into the size it was given.
func (m Model[T]) View() string {
	if m.showDetail {
		return m.detail.View()
	}
	if !m.statusShown() {
		return m.list.View()
	}
	status := lipgloss.NewStyle().MaxWidth(m.width).Render(m.statusLine())
	return lipgloss.JoinVertical(lipgloss.Left, status, m.list.View())
}

// Help merges the shared list bindings into the screen's own help, or
// replaces it while the filter is typed or the detail pane is open.
func (m Model[T]) Help(screen help.KeyMap) help.KeyMap {
	switch {
	case m.editing:
		return keys.FilterMap{List: m.keys}
	case m.showDetail:
		return m.detailKeys
	}
	return keys.WithList(screen, m.keys)
}
