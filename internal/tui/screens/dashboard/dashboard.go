// Package dashboard is the top-level overview screen. It owns no data
// loading: it summarizes the LoadedMsg outputs the other screens emit, which
// the app broadcasts to every component.
//
// This is the top navigation level, so esc does nothing here.
package dashboard

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/mugenkunou/ws-tool/internal/style"
	"github.com/mugenkunou/ws-tool/internal/tui/env"
	"github.com/mugenkunou/ws-tool/internal/tui/format"
	"github.com/mugenkunou/ws-tool/internal/tui/keys"
	"github.com/mugenkunou/ws-tool/internal/tui/nav"
	"github.com/mugenkunou/ws-tool/internal/tui/screens/dotfiles"
	"github.com/mugenkunou/ws-tool/internal/tui/screens/ignore"
	"github.com/mugenkunou/ws-tool/internal/tui/screens/logs"
	"github.com/mugenkunou/ws-tool/internal/tui/screens/repos"
	"github.com/mugenkunou/ws-tool/internal/tui/screens/scratch"
	"github.com/mugenkunou/ws-tool/internal/tui/screens/secrets"
	"github.com/mugenkunou/ws-tool/internal/tui/screens/system"
	"github.com/mugenkunou/ws-tool/internal/tui/theme"
)

// card is one subsystem summary. lines nil means still loading.
type card struct {
	screen nav.Screen
	lines  []string
	err    error
}

const cardContentLines = 2

// Model is the dashboard state.
type Model struct {
	env    env.Env
	keys   keys.DashboardMap
	cards  []card
	cursor int
	width  int
	height int
}

// New creates the dashboard.
func New(e env.Env) Model {
	return Model{
		env:  e,
		keys: keys.Dashboard(),
		cards: []card{
			{screen: nav.ScreenRepos},
			{screen: nav.ScreenDotfiles},
			{screen: nav.ScreenScratch},
			{screen: nav.ScreenLogs},
			{screen: nav.ScreenIgnore},
			{screen: nav.ScreenSecrets},
			{screen: nav.ScreenSystem},
		},
	}
}

// Init implements nav.Component. The dashboard loads nothing itself.
func (m Model) Init() tea.Cmd { return nil }

// Update implements nav.Component.
func (m Model) Update(msg tea.Msg) (nav.Component, tea.Cmd) {
	switch msg := msg.(type) {
	case nav.ResizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case nav.RefreshMsg:
		for i := range m.cards {
			m.cards[i] = card{screen: m.cards[i].screen}
		}
	case repos.LoadedMsg:
		m = m.setCard(nav.ScreenRepos, msg.Err, reposLines(msg))
	case dotfiles.LoadedMsg:
		m = m.setCard(nav.ScreenDotfiles, msg.Err, dotfilesLines(msg))
	case scratch.LoadedMsg:
		m = m.setCard(nav.ScreenScratch, msg.Err, scratchLines(msg))
	case logs.LoadedMsg:
		m = m.setCard(nav.ScreenLogs, msg.Err, logsLines(msg))
	case ignore.LoadedMsg:
		m = m.setCard(nav.ScreenIgnore, msg.Err, ignoreLines(msg))
	case secrets.LoadedMsg:
		m = m.setCard(nav.ScreenSecrets, msg.Err, secretsLines(msg))
	case system.LoadedMsg:
		m = m.setCard(nav.ScreenSystem, msg.Err, systemLines(msg))
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, m.keys.Up):
			m.cursor = max(m.cursor-1, 0)
		case key.Matches(msg, m.keys.Down):
			m.cursor = min(m.cursor+1, len(m.cards)-1)
		case key.Matches(msg, m.keys.Open):
			return m, nav.Goto(m.cards[m.cursor].screen)
		case key.Matches(msg, m.keys.Refresh):
			return m, nav.Refresh
		}
	}
	return m, nil
}

func (m Model) setCard(s nav.Screen, err error, lines []string) Model {
	cards := make([]card, len(m.cards))
	copy(cards, m.cards)
	for i := range cards {
		if cards[i].screen == s {
			cards[i] = card{screen: s, lines: lines, err: err}
		}
	}
	m.cards = cards
	return m
}

func reposLines(msg repos.LoadedMsg) []string {
	s := msg.Summarize()
	first := format.Plural(s.Repos, "repo")
	if s.Dirty > 0 {
		first += " · " + theme.Warn.Render(fmt.Sprintf("%d dirty", s.Dirty))
	}
	second := fmt.Sprintf("%d ahead · %d behind", s.Ahead, s.Behind)
	if s.Findings > 0 {
		second += " · " + theme.Warn.Render(format.Plural(s.Findings, "issue"))
	}
	return []string{first, second}
}

func dotfilesLines(msg dotfiles.LoadedMsg) []string {
	health := theme.OK.Render("✔ all links healthy")
	if n := msg.Issues(); n > 0 {
		health = theme.Error.Render(fmt.Sprintf("✘ %d need attention", n))
	}
	return []string{fmt.Sprintf("%d managed", len(msg.Entries)), health}
}

func scratchLines(msg scratch.LoadedMsg) []string {
	newest := theme.Muted.Render("none yet")
	if len(msg.Entries) > 0 {
		newest = "newest: " + msg.Entries[0].Name
	}
	return []string{format.Plural(len(msg.Entries), "dir") + " · " + style.HumanBytes(msg.TotalBytes()), newest}
}

func logsLines(msg logs.LoadedMsg) []string {
	state := theme.Muted.Render("not recording")
	if msg.Active() {
		state = theme.Error.Render("● recording")
	}
	return []string{format.Plural(len(msg.Sessions), "session"), state}
}

func ignoreLines(msg ignore.LoadedMsg) []string {
	n := msg.Actionable()
	if n == 0 {
		return []string{theme.OK.Render("✔ nothing bloating sync"), theme.Muted.Render(format.Plural(len(msg.Violations), "item") + " in safe harbors")}
	}
	return []string{theme.Warn.Render(format.Plural(n, "violation")), theme.Muted.Render("bloat, depth, build artifacts")}
}

func secretsLines(msg secrets.LoadedMsg) []string {
	first := theme.OK.Render("✔ no secrets found")
	if n := len(msg.Violations); n > 0 {
		first = theme.Error.Render(format.Plural(n, "finding"))
	}
	h := msg.Health.Pass
	second := theme.Muted.Render("pass not set up")
	switch {
	case h.Initialized && h.GitRemote:
		second = fmt.Sprintf("pass: %s, synced", format.Plural(h.EntryCount, "entry"))
	case h.Initialized:
		second = theme.Warn.Render(fmt.Sprintf("pass: %s, no remote", format.Plural(h.EntryCount, "entry")))
	}
	return []string{first, second}
}

func systemLines(msg system.LoadedMsg) []string {
	trash := theme.OK.Render("✔ soft-delete on")
	if msg.Trash.WarningCount() > 0 {
		trash = theme.Warn.Render(fmt.Sprintf("soft-delete: %d of 3 integrations off", msg.Trash.WarningCount()))
	}
	installed, failing := 0, 0
	for _, j := range msg.Jobs {
		if j.Installed && !j.Preset {
			installed++
			if j.LastRun.ExitCode != 0 {
				failing++
			}
		}
	}
	cronLine := format.Plural(installed, "cron job") + " installed"
	if failing > 0 {
		cronLine += " · " + theme.Error.Render(fmt.Sprintf("%d failing", failing))
	}
	return []string{trash, cronLine}
}

// View implements nav.Component.
func (m Model) View() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	header := lipgloss.NewStyle().MaxWidth(m.width).Render(
		theme.Muted.Render("Workspace ") + m.env.Paths.Workspace)

	// Two columns when there is room, otherwise stack.
	cols := 1
	if m.width >= 60 {
		cols = 2
	}
	gap := 1
	cardWidth := (m.width - gap*(cols-1)) / cols

	var rows []string
	for i := 0; i < len(m.cards); i += cols {
		var cells []string
		for j := i; j < min(i+cols, len(m.cards)); j++ {
			cells = append(cells, m.renderCard(j, cardWidth))
			if j < min(i+cols, len(m.cards))-1 {
				cells = append(cells, strings.Repeat(" ", gap))
			}
		}
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, cells...))
	}

	body := lipgloss.JoinVertical(lipgloss.Left, append([]string{header, ""}, rows...)...)
	return lipgloss.NewStyle().MaxWidth(m.width).MaxHeight(m.height).Render(body)
}

func (m Model) renderCard(i, width int) string {
	c := m.cards[i]
	st := theme.Card
	if i == m.cursor {
		st = theme.CardSelected
	}
	// Width includes border; content width is what is left inside padding.
	inner := max(width-st.GetHorizontalFrameSize(), 1)

	title := theme.Bold.Render(c.screen.Title())
	var lines []string
	switch {
	case c.err != nil:
		lines = []string{theme.Error.Render("Error: " + c.err.Error())}
	case c.lines == nil:
		lines = []string{theme.Muted.Render("Loading…")}
	default:
		lines = c.lines
	}
	for len(lines) < cardContentLines {
		lines = append(lines, "")
	}
	clip := lipgloss.NewStyle().MaxWidth(inner)
	content := []string{clip.Render(title)}
	for _, l := range lines[:cardContentLines] {
		content = append(content, clip.Render(l))
	}
	return st.Width(width).Render(strings.Join(content, "\n"))
}

// CapturesKeys implements nav.Component.
func (m Model) CapturesKeys() bool { return false }

// KeyMap implements nav.Component.
func (m Model) KeyMap() help.KeyMap { return m.keys }
