// Package dashboard is the top-level screen and the workspace's home.
//
// Layout, top to bottom: a greeting, the "Needs attention" list (problems,
// most severe first, then advice), and an overview grid with one card per
// area. ↑↓ move through the list; in the grid all four arrows move between
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

	"github.com/mugenkunou/ws-tool/internal/style"
	"github.com/mugenkunou/ws-tool/internal/symlink"
	"github.com/mugenkunou/ws-tool/internal/tui/env"
	"github.com/mugenkunou/ws-tool/internal/tui/format"
	"github.com/mugenkunou/ws-tool/internal/tui/keys"
	"github.com/mugenkunou/ws-tool/internal/tui/nav"
	"github.com/mugenkunou/ws-tool/internal/tui/screens/cron"
	"github.com/mugenkunou/ws-tool/internal/tui/screens/ignore"
	"github.com/mugenkunou/ws-tool/internal/tui/screens/repos"
	"github.com/mugenkunou/ws-tool/internal/tui/screens/scratch"
	"github.com/mugenkunou/ws-tool/internal/tui/screens/secrets"
	"github.com/mugenkunou/ws-tool/internal/tui/screens/sessions"
	"github.com/mugenkunou/ws-tool/internal/tui/screens/symlinks"
	"github.com/mugenkunou/ws-tool/internal/tui/screens/trash"
	"github.com/mugenkunou/ws-tool/internal/tui/theme"
)

type severity int

const (
	sevError severity = iota
	sevWarn
	sevInfo
)

// issue is one "Needs attention" row.
type issue struct {
	sev    severity
	text   string
	screen nav.Screen
	focus  tea.Msg // optional: sent with the switch so the screen selects the problem
	// filter is a filter expression (`status!=ok`) the screen opens with, so its
	// own keys act on exactly the affected rows.
	filter string
	// advice rows are not problems: they are listed last at info level, never
	// count towards a card's status, and open the command bar with prefill.
	advice  bool
	prefill string
}

// area is what the dashboard knows about one screen's subsystem.
type area struct {
	loaded bool
	lines  []string // card body, at most cardBodyLines
	issues []issue
}

// areas in grid order (row-major).
var areas = []nav.Screen{
	nav.ScreenRepos, nav.ScreenSymlinks, nav.ScreenScratch,
	nav.ScreenSessions, nav.ScreenIgnore,
	nav.ScreenSecrets, nav.ScreenCron, nav.ScreenTrash,
}

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
	case repos.LoadedMsg:
		m = m.set(nav.ScreenRepos, msg.Err, reposArea(msg))
	case symlinks.LoadedMsg:
		if msg.Walked {
			return m, nil // the screen's second load adds the $HOME walk; the Dashboard needs no walk
		}
		m = m.set(nav.ScreenSymlinks, msg.Err, symlinksArea(msg))
	case scratch.LoadedMsg:
		m = m.set(nav.ScreenScratch, msg.Err, scratchArea(msg))
	case sessions.LoadedMsg:
		m = m.set(nav.ScreenSessions, msg.Err, logsArea(msg))
	case ignore.LoadedMsg:
		m = m.set(nav.ScreenIgnore, msg.Err, ignoreArea(msg))
	case secrets.LoadedMsg:
		m = m.set(nav.ScreenSecrets, msg.Err, secretsArea(msg))
	case cron.LoadedMsg:
		m = m.set(nav.ScreenCron, nil, cronArea(msg))
	case trash.LoadedMsg:
		m = m.set(nav.ScreenTrash, msg.Err, trashArea(msg))
	case tea.KeyPressMsg:
		return m.updateKey(msg)
	}
	return m, nil
}

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
func open(is *issue) tea.Cmd {
	switch {
	case is.advice:
		return send(nav.OpenCommandBarMsg{Prefill: is.prefill})
	case is.filter != "":
		return tea.Batch(nav.Goto(is.screen), send(nav.FilterMsg{Screen: is.screen, Filter: is.filter}))
	case is.focus != nil:
		return tea.Batch(nav.Goto(is.screen), send(is.focus))
	}
	return nav.Goto(is.screen)
}

func send(msg tea.Msg) tea.Cmd { return func() tea.Msg { return msg } }

func (m Model) set(s nav.Screen, err error, a area) Model {
	if err != nil {
		a = area{lines: []string{theme.Error.Render("couldn't load")}, issues: []issue{{
			sev: sevError, screen: s, text: "Couldn't load " + s.Title() + ": " + firstLine(err.Error()),
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
func (m Model) issues() []*issue {
	var out []*issue
	for _, s := range areas {
		a := m.areas[s]
		for i := range a.issues {
			out = append(out, &a.issues[i])
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].advice != out[j].advice {
			return !out[i].advice // advice is listed last
		}
		return out[i].sev < out[j].sev
	})
	return out
}

// Attention returns every "Needs attention" problem, in display order, each
// led by its subsystem's icon. Advice rows are left out: they are not problems.
func (m Model) Attention() []string {
	var out []string
	for _, is := range m.issues() {
		if !is.advice {
			out = append(out, theme.Icon(is.screen.Icon())+is.text)
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

// ── per-area derivation ─────────────────────────────────────────────────────

func reposArea(msg repos.LoadedMsg) area {
	s := msg.Summarize()
	a := area{lines: []string{
		format.Plural(s.Repos, "repo") + dirtyNote(s.Dirty),
		fmt.Sprintf("↑ %d ahead · ↓ %d behind", s.Ahead, s.Behind),
	}}
	var failed, outOfSync, withFindings []string
	for _, e := range msg.Entries {
		st := e.Status
		switch {
		case st.Error != "":
			failed = append(failed, st.Path)
		case st.Dirty || st.Ahead > 0 || st.Behind > 0:
			outOfSync = append(outOfSync, st.Path)
		}
		if e.Issues() > 0 {
			withFindings = append(withFindings, st.Path)
		}
	}
	if len(failed) > 0 {
		a.issues = append(a.issues, issue{sev: sevError, screen: nav.ScreenRepos,
			text:   format.Plural(len(failed), "repo") + " failed to scan",
			filter: "state=error"})
	}
	if len(outOfSync) > 0 {
		var parts []string
		if s.Dirty > 0 {
			parts = append(parts, fmt.Sprintf("%d dirty", s.Dirty))
		}
		if s.Behind > 0 {
			parts = append(parts, fmt.Sprintf("%d behind", s.Behind))
		}
		if s.Ahead > 0 {
			parts = append(parts, fmt.Sprintf("%d ahead", s.Ahead))
		}
		a.issues = append(a.issues, issue{sev: sevWarn, screen: nav.ScreenRepos,
			text:  fmt.Sprintf("%s out of sync (%s)", format.Plural(len(outOfSync), "repo"), strings.Join(parts, ", ")),
			focus: repos.SelectMsg{Path: outOfSync[0]}})
	}
	if s.Findings > 0 {
		a.issues = append(a.issues, issue{sev: sevInfo, screen: nav.ScreenRepos,
			text:  fmt.Sprintf("%s in %s", format.Plural(s.Findings, "hygiene finding"), format.Plural(len(withFindings), "repo")),
			focus: repos.SelectMsg{Path: withFindings[0]}})
	}
	return a
}

func dirtyNote(n int) string {
	if n == 0 {
		return ""
	}
	return " · " + theme.Warn.Render(fmt.Sprintf("%d dirty", n))
}

func symlinksArea(msg symlinks.LoadedMsg) area {
	a := area{lines: []string{format.Plural(msg.Registered(), "symlink") + " managed"}}
	// Broken, missing and drifted entries are the problems; untracked links and
	// orphans are in the screen's list but are not alarms.
	var bad []string
	for _, e := range msg.Entries {
		switch e.Status {
		case symlink.StatusBroken, symlink.StatusMissing, symlink.StatusDrifted:
			bad = append(bad, e.System)
		}
	}
	if n := len(bad); n > 0 {
		a.lines = append(a.lines, theme.Error.Render(format.Plural(n, "link")+" need fixing"))
		a.issues = append(a.issues, issue{sev: sevError, screen: nav.ScreenSymlinks,
			text:  format.Plural(n, "symlink") + " broken, missing or drifted",
			focus: symlinks.SelectMsg{System: bad[0]}})
	} else if msg.Registered() > 0 {
		a.lines = append(a.lines, theme.OK.Render("all links healthy"))
	}
	if msg.Found > 0 {
		a.issues = append(a.issues, issue{sev: sevInfo, screen: nav.ScreenSymlinks,
			text: format.Plural(msg.Found, "known config") + " found, not symlinked"})
	}
	return a
}

func scratchArea(msg scratch.LoadedMsg) area {
	a := area{lines: []string{fmt.Sprintf("%s · %s", format.Plural(len(msg.Entries), "dir"), style.HumanBytes(msg.TotalBytes()))}}
	if len(msg.Entries) > 0 {
		a.lines = append(a.lines, theme.Muted.Render("newest ")+msg.Entries[0].Name)
	}
	return a
}

func logsArea(msg sessions.LoadedMsg) area {
	a := area{lines: []string{format.Plural(len(msg.Sessions), "session")}}
	for _, s := range msg.Sessions {
		if s.Active {
			a.lines = append(a.lines, theme.Error.Render("● recording "+s.Tag))
			return a
		}
	}
	a.lines = append(a.lines, theme.Muted.Render("not recording"))
	return a
}

func ignoreArea(msg ignore.LoadedMsg) area {
	n := msg.Actionable()
	included := len(msg.Violations) - n
	a := area{lines: []string{theme.OK.Render("nothing bloating sync")}}
	if n > 0 {
		a.lines = []string{theme.Warn.Render(format.Plural(n, "violation"))}
		a.issues = append(a.issues, issue{sev: sevWarn, screen: nav.ScreenIgnore,
			text: format.Plural(n, "sync hygiene violation") + " (bloat, depth, build artifacts)"})
	}
	if included > 0 {
		a.lines = append(a.lines, theme.Muted.Render(fmt.Sprintf("%d in included paths", included)))
	}
	if n := len(msg.Stale); n > 0 {
		a.lines = append(a.lines, theme.Muted.Render(format.Plural(n, "stale mute")))
		text := format.Plural(n, "muted path") + " no longer exist"
		if n == 1 {
			text += "s"
		}
		a.issues = append(a.issues, issue{sev: sevInfo, screen: nav.ScreenIgnore, text: text})
	}
	return a
}

func secretsArea(msg secrets.LoadedMsg) area {
	h := msg.Health.Pass
	a := area{lines: []string{theme.OK.Render("no secrets found")}}
	if n := len(msg.Violations); n > 0 {
		a.lines = []string{theme.Error.Render(format.Plural(n, "finding"))}
		a.issues = append(a.issues, issue{sev: sevError, screen: nav.ScreenSecrets,
			text: format.Plural(n, "possible secret") + " in workspace files"})
	}
	if h.Initialized {
		a.lines = append(a.lines, "pass: "+format.Plural(h.EntryCount, "entry"))
	} else {
		a.lines = append(a.lines, theme.Muted.Render("pass not set up"))
	}
	if h.Initialized && h.GitBacked && !h.GitRemote {
		a.issues = append(a.issues, issue{sev: sevWarn, screen: nav.ScreenSecrets,
			text: "Pass store has no git remote — secrets live only on this machine"})
	}
	if h.Initialized && msg.Health.CopyCommand != "" {
		a.issues = append(a.issues, issue{sev: sevInfo, screen: nav.ScreenSecrets, advice: true,
			text:    "Your pass store syncs only through its own git remote. A copy in the workspace would sync with MEGA too.",
			prefill: msg.Health.CopyCommand})
	}
	return a
}

func cronArea(msg cron.LoadedMsg) area {
	a := area{lines: []string{format.Plural(msg.Installed(), "job") + " installed"}}
	failing := msg.Failing()
	for _, j := range failing {
		a.issues = append(a.issues, issue{sev: sevError, screen: nav.ScreenCron,
			text:  fmt.Sprintf("Cron job %s failing (exit %d)", j.Name, j.LastRun.ExitCode),
			focus: cron.SelectJobMsg{Name: j.Name}})
	}
	switch {
	case msg.CronErr != nil:
		a.lines = append(a.lines, theme.Warn.Render("crontab not accessible"))
		a.issues = append(a.issues, issue{sev: sevInfo, screen: nav.ScreenCron, text: "Crontab not accessible: " + firstLine(msg.CronErr.Error())})
	case len(failing) > 0:
		a.lines = append(a.lines, theme.Error.Render(fmt.Sprintf("%d failing", len(failing))))
	case msg.Installed() > 0:
		a.lines = append(a.lines, theme.OK.Render("all passing"))
	}
	return a
}

func trashArea(msg trash.LoadedMsg) area {
	off := msg.Status.WarningCount()
	a := area{lines: []string{fmt.Sprintf("%s · %s", style.HumanBytes(msg.Status.SizeBytes), format.Plural(msg.Status.Files, "file"))}}
	if off == 0 {
		a.lines = append(a.lines, theme.OK.Render("soft-delete on"))
	} else {
		a.lines = append(a.lines, theme.Warn.Render(fmt.Sprintf("soft-delete off (%d of 3)", off)))
		a.issues = append(a.issues, issue{sev: sevWarn, screen: nav.ScreenTrash,
			text: fmt.Sprintf("Soft-delete is off for %d of 3 integrations", off)})
	}
	if msg.Status.OverLimit {
		a.issues = append(a.issues, issue{sev: sevWarn, screen: nav.ScreenTrash,
			text: fmt.Sprintf("Trash holds %s (over %s)", style.HumanBytes(msg.Status.SizeBytes), style.HumanBytes(msg.Status.WarnSizeBytes))})
	}
	return a
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
	case worst(a.issues) == sevError:
		mark = theme.Error.Render("✘")
	case worst(a.issues) == sevWarn:
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

func (m Model) issueLine(is *issue, selected bool) string {
	var mark string
	switch is.sev {
	case sevError:
		mark = theme.Error.Render("✘")
	case sevWarn:
		mark = theme.Warn.Render("!")
	case sevInfo:
		mark = theme.Muted.Render("·")
		if is.advice {
			mark = theme.Muted.Render("ℹ")
		}
	}
	where := theme.Muted.Render(theme.Icon(is.screen.Icon()) + is.screen.Title())
	hint := ""
	lead := "  "
	text := is.text
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

func worst(is []issue) severity {
	w := sevInfo + 1
	for _, i := range is {
		if !i.advice {
			w = min(w, i.sev)
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
