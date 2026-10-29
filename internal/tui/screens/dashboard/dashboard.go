// Package dashboard is the top-level screen and the workspace's home.
//
// Layout, top to bottom: a greeting, the "Needs attention" list (problems,
// most severe first, then advice), and an overview grid with one card per
// screen. Every screen's LoadedMsg is a nav.Summary: the screen derives its
// own card, and the dashboard only lays them out, so a new screen gets its
// card without a change here. ↑↓ move through the list; in the grid all four arrows move between
// cards, and ↑ from the top row returns to the list. The dashboard has no
// actions of its own: enter opens the screen that has the verb, filtered to the
// affected rows (or with the problem selected). Advice rows are not
// problems: enter opens the command bar with the suggested command typed in,
// to run, edit or dismiss. The dashboard never touches another screen's state.
//
// This is the top navigation level, so esc does nothing here.
package dashboard

import (
	"fmt"
	"sort"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/mugenkunou/ws-tool/internal/tui/env"
	"github.com/mugenkunou/ws-tool/internal/tui/keys"
	"github.com/mugenkunou/ws-tool/internal/tui/nav"
	"github.com/mugenkunou/ws-tool/internal/tui/screens/sessions"
	"github.com/mugenkunou/ws-tool/internal/tui/theme"
)

// area is what the dashboard knows about one screen's subsystem.
type area struct {
	loaded bool
	lines  []string // card body, at most cardBodyLines
	issues []nav.Issue
}

// areas are every screen but the dashboard, in tab order (the grid is
// row-major).
var areas = nav.Screens()[1:]

const (
	cardBodyLines = 2
	cardHeight    = cardBodyLines + 3 // title + body + top/bottom border
	gridGap       = 1
)

// section is which part of the page holds the cursor.
type section int

const (
	sectionAttention section = iota
	sectionGrid
)

// Model is the dashboard state.
type Model struct {
	env     env.Env
	keys    keys.DashboardMap
	areas   map[nav.Screen]area
	section section
	att     int  // cursor in the attention list
	cell    int  // cursor in the grid
	moved   bool // the user has moved the cursor; stop auto-placing it
	frame   int
	hour    int
	nudge   string // a newer agent-tmux release, e.g. "v0.2.0"
	width   int
	height  int
}

// New creates the dashboard.
func New(e env.Env) Model {
	return Model{env: e, keys: keys.Dashboard(), areas: map[nav.Screen]area{}, hour: e.Now().Hour(), section: sectionGrid}
}

// Init implements nav.Component. The dashboard loads nothing itself.
func (m Model) Init() tea.Cmd { return nil }

// CapturesKeys implements nav.Component: the dashboard has no dialogs.
func (m Model) CapturesKeys() bool { return false }

// Update implements nav.Component.
func (m Model) Update(msg tea.Msg) (nav.Component, tea.Cmd) {
	switch msg := msg.(type) {
	case nav.ResizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case nav.FrameMsg:
		m.frame = msg.N
		m.hour = m.env.Now().Hour()
	case nav.RefreshMsg:
		m.areas = map[nav.Screen]area{}
	case sessions.NudgeMsg:
		m.nudge = msg.Nudge.Latest
	case tea.KeyPressMsg:
		return m.updateKey(msg)
	case nav.Summary:
		if s, c, ok := msg.Card(); ok {
			m = m.set(s, c)
		}
	}
	return m, nil
}

// Loaded reports whether screen s has delivered its card.
func (m Model) Loaded(s nav.Screen) bool { return m.areas[s].loaded }

// cols is the grid's column count for the current width.
func (m Model) cols() int {
	switch {
	case m.width >= 90:
		return 3
	case m.width >= 56:
		return 2
	default:
		return 1
	}
}

func (m Model) updateKey(k tea.KeyPressMsg) (nav.Component, tea.Cmd) {
	m.moved = true
	issues := m.issues()
	n, c := len(areas), m.cols()
	switch {
	case key.Matches(k, m.keys.Top):
		if len(issues) > 0 {
			m.section, m.att = sectionAttention, 0
		} else {
			m.section, m.cell = sectionGrid, 0
		}
	case key.Matches(k, m.keys.Bottom):
		m.section, m.cell = sectionGrid, n-1
	case m.section == sectionAttention:
		switch {
		case key.Matches(k, m.keys.Up):
			m.att = max(m.att-1, 0)
		case key.Matches(k, m.keys.Down):
			if m.att < len(issues)-1 {
				m.att++
			} else {
				m.section, m.cell = sectionGrid, 0
			}
		case key.Matches(k, m.keys.Open):
			return m, open(issues[m.att])
		}
	default: // grid
		row, col := m.cell/c, m.cell%c
		switch {
		case key.Matches(k, m.keys.Left):
			if col > 0 {
				m.cell--
			}
		case key.Matches(k, m.keys.Right):
			if col < c-1 && m.cell+1 < n {
				m.cell++
			}
		case key.Matches(k, m.keys.Up):
			if row > 0 {
				m.cell -= c
			} else if len(issues) > 0 {
				m.section, m.att = sectionAttention, len(issues)-1
			}
		case key.Matches(k, m.keys.Down):
			if m.cell+c < n {
				m.cell += c
			} else if (n-1)/c > row { // short last row: land on its last card
				m.cell = n - 1
			}
		case key.Matches(k, m.keys.Open):
			return m, nav.Goto(areas[m.cell])
		}
	}
	return m, nil
}

// open is enter on an attention row: advice opens the command bar with the
// suggested command; a problem opens its screen, filtered to the affected rows
// or with the problem selected.
func open(is *nav.Issue) tea.Cmd {
	switch {
	case is.Advice:
		return send(nav.OpenCommandBarMsg{Prefill: is.Prefill})
	case is.Filter != "":
		return tea.Batch(nav.Goto(is.Screen), send(nav.FilterMsg{Screen: is.Screen, Filter: is.Filter}))
	case is.Focus != nil:
		return tea.Batch(nav.Goto(is.Screen), send(is.Focus))
	}
	return nav.Goto(is.Screen)
}

func send(msg tea.Msg) tea.Cmd { return func() tea.Msg { return msg } }

func (m Model) set(s nav.Screen, c nav.Card) Model {
	a := area{lines: c.Lines, issues: c.Issues}
	if c.Err != nil {
		a = area{lines: []string{theme.Error.Render("couldn't load")}, issues: []nav.Issue{{
			Sev: nav.SevError, Screen: s, Text: "Couldn't load " + s.Title() + ": " + firstLine(c.Err.Error()),
		}}}
	}
	a.loaded = true
	next := make(map[nav.Screen]area, len(m.areas)+1)
	for k, v := range m.areas {
		next[k] = v
	}
	next[s] = a
	m.areas = next
	switch n := len(m.issues()); {
	case n == 0:
		m.section = sectionGrid
	case !m.moved:
		m.section, m.att = sectionAttention, 0 // start on the most severe problem
	default:
		m.att = min(m.att, n-1)
	}
	return m
}

// issues returns every attention item, most severe first, then in area order.
func (m Model) issues() []*nav.Issue {
	var out []*nav.Issue
	for _, s := range areas {
		a := m.areas[s]
		for i := range a.issues {
			out = append(out, &a.issues[i])
		}
	}
	if m.nudge != "" {
		// The Sessions screen shows the install lines.
		out = append(out, &nav.Issue{Sev: nav.SevInfo, Screen: nav.ScreenSessions, Text: "agent-tmux " + m.nudge + " is available"})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Advice != out[j].Advice {
			return !out[i].Advice // advice is listed last
		}
		return out[i].Sev < out[j].Sev
	})
	return out
}

// Attention returns every "Needs attention" problem, in display order, each
// led by its subsystem's icon. Advice rows are left out: they are not problems.
func (m Model) Attention() []string {
	var out []string
	for _, is := range m.issues() {
		if !is.Advice {
			out = append(out, theme.Icon(is.Screen.Icon())+is.Text)
		}
	}
	return out
}

func (m Model) allLoaded() bool {
	for _, s := range areas {
		if !m.areas[s].loaded {
			return false
		}
	}
	return true
}

// ── rendering ───────────────────────────────────────────────────────────────

// View implements nav.Component.
func (m Model) View() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	issues := m.issues()
	lines := []string{
		theme.Bold.Render(theme.Greeting(m.hour)) + theme.Muted.Render("  ·  "+m.env.Rel(m.env.Paths.Workspace)),
		"",
	}
	focusTop, focusBottom := 0, 0

	switch {
	case len(issues) > 0:
		lines = append(lines, theme.Bold.Render(theme.Icon(theme.IconAlarm)+fmt.Sprintf("Needs attention (%d)", len(issues))))
		for i, is := range issues {
			selected := m.section == sectionAttention && i == m.att
			if selected {
				focusTop, focusBottom = len(lines), len(lines)
			}
			lines = append(lines, m.issueLine(is, selected))
		}
	case m.allLoaded():
		lines = append(lines, theme.OK.Render(theme.Icon(theme.IconSparkles)+"All clear — nothing needs attention"))
	default:
		lines = append(lines, theme.Muted.Render(theme.Spinner(m.frame)+" Checking your workspace…"))
	}

	lines = append(lines, "", theme.Bold.Render(theme.Icon(theme.IconClip)+"Overview"))
	gridTop := len(lines)
	lines = append(lines, strings.Split(m.grid(), "\n")...)
	if m.section == sectionGrid {
		row := m.cell / m.cols()
		focusTop = gridTop + row*cardHeight
		focusBottom = focusTop + cardHeight - 1
	}

	// Scroll so the focused row (or card) stays visible.
	start := 0
	if focusBottom >= m.height {
		start = min(focusTop, focusBottom-m.height+1)
	}
	lines = lines[start:min(start+m.height, len(lines))]
	return lipgloss.NewStyle().MaxWidth(m.width).MaxHeight(m.height).Render(strings.Join(lines, "\n"))
}

func (m Model) grid() string {
	c := m.cols()
	w := (m.width - gridGap*(c-1)) / c
	var rows []string
	for i := 0; i < len(areas); i += c {
		var cells []string
		for j := i; j < min(i+c, len(areas)); j++ {
			if j > i {
				cells = append(cells, strings.Repeat(" ", gridGap))
			}
			cells = append(cells, m.card(j, w))
		}
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, cells...))
	}
	return strings.Join(rows, "\n")
}

func (m Model) card(i, width int) string {
	s := areas[i]
	a := m.areas[s]
	selected := m.section == sectionGrid && i == m.cell
	st := theme.Card
	if selected {
		st = theme.CardSelected
	}
	inner := max(width-st.GetHorizontalFrameSize(), 1)

	mark := theme.OK.Render("✔")
	switch {
	case !a.loaded:
		mark = theme.Muted.Render(theme.Spinner(m.frame))
	case worst(a.issues) == nav.SevError:
		mark = theme.Error.Render("✘")
	case worst(a.issues) == nav.SevWarn:
		mark = theme.Warn.Render("!")
	}
	title := theme.Icon(s.Icon()) + s.Title()
	if selected {
		title = theme.Title.Render(title)
	} else {
		title = theme.Bold.Render(title)
	}
	pad := max(inner-lipgloss.Width(title)-lipgloss.Width(mark), 1)
	head := title + strings.Repeat(" ", pad) + mark

	body := a.lines
	if !a.loaded {
		body = []string{theme.Muted.Render("scanning…")}
	}
	clip := lipgloss.NewStyle().MaxWidth(inner)
	content := []string{clip.Render(head)}
	for k := range cardBodyLines {
		line := ""
		if k < len(body) {
			line = body[k]
		}
		content = append(content, clip.Render(line))
	}
	return st.Width(width).Render(strings.Join(content, "\n"))
}

func (m Model) issueLine(is *nav.Issue, selected bool) string {
	var mark string
	switch is.Sev {
	case nav.SevError:
		mark = theme.Error.Render("✘")
	case nav.SevWarn:
		mark = theme.Warn.Render("!")
	case nav.SevInfo:
		mark = theme.Muted.Render("·")
		if is.Advice {
			mark = theme.Muted.Render("ℹ")
		}
	}
	where := theme.Muted.Render(theme.Icon(is.Screen.Icon()) + is.Screen.Title())
	hint := ""
	lead := "  "
	text := is.Text
	if selected {
		lead = theme.Title.Render("› ")
		text = theme.Bold.Render(text)
	}
	return m.columns(lead+mark+" "+text, where, hint)
}

// columns lays out left text and right-aligned tags within the width,
// truncating the left text first and dropping the tags when too narrow.
func (m Model) columns(left, where, hint string) string {
	right := where
	if hint != "" {
		right += "   " + hint
	}
	rw := lipgloss.Width(right)
	space := m.width - rw - 2
	if space < 12 {
		return lipgloss.NewStyle().MaxWidth(m.width).Render(left)
	}
	left = lipgloss.NewStyle().MaxWidth(space).Render(left)
	pad := max(m.width-lipgloss.Width(left)-rw, 2)
	return left + strings.Repeat(" ", pad) + right
}

func worst(is []nav.Issue) nav.Severity {
	w := nav.SevInfo + 1
	for _, i := range is {
		if !i.Advice {
			w = min(w, i.Sev)
		}
	}
	return w
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// KeyMap implements nav.Component.
func (m Model) KeyMap() help.KeyMap { return m.keys }
