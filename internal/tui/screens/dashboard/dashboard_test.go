package dashboard

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/mugenkunou/ws-tool/internal/agenttmux"
	wsignore "github.com/mugenkunou/ws-tool/internal/ignore"
	"github.com/mugenkunou/ws-tool/internal/symlink"
	"github.com/mugenkunou/ws-tool/internal/tui/env"
	"github.com/mugenkunou/ws-tool/internal/tui/nav"
	"github.com/mugenkunou/ws-tool/internal/tui/screens/ignore"
	"github.com/mugenkunou/ws-tool/internal/tui/screens/sessions"
	"github.com/mugenkunou/ws-tool/internal/tui/screens/symlinks"
	"github.com/mugenkunou/ws-tool/internal/tui/screens/workspace"
)

func model(areas map[nav.Screen]area) Model {
	m := New(env.Env{Clock: time.Now})
	m.areas = areas
	m.width, m.height = 100, 30
	return m
}

func collect(cmd tea.Cmd) []tea.Msg {
	var out []tea.Msg
	queue := []tea.Cmd{cmd}
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		switch msg := c().(type) {
		case nil:
		case tea.BatchMsg:
			queue = append(queue, msg...)
		default:
			out = append(out, msg)
		}
	}
	return out
}

// cardOf is the area a screen's load result gives the dashboard.
func cardOf(msg nav.Summary) area {
	_, c, _ := msg.Card()
	return area{loaded: true, lines: c.Lines, issues: c.Issues}
}

func enter(m Model) (Model, []tea.Msg) {
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	return next.(Model), collect(cmd)
}

var passAdvice = nav.Issue{
	Sev: nav.SevInfo, Screen: nav.ScreenSecrets, Advice: true,
	Text: "Your pass store syncs only through its own git remote. A copy in the workspace would sync with MEGA too.",
}

func TestAdviceIsListedLastAndOpensItsScreen(t *testing.T) {
	m := model(map[nav.Screen]area{
		nav.ScreenSecrets: {loaded: true, issues: []nav.Issue{passAdvice, {Sev: nav.SevWarn, Screen: nav.ScreenSecrets, Text: "Pass store has no git remote"}}},
		nav.ScreenRepos:   {loaded: true, issues: []nav.Issue{{Sev: nav.SevInfo, Screen: nav.ScreenRepos, Text: "1 hygiene finding in 1 repo"}}},
	})
	is := m.issues()
	if len(is) != 3 || !is[2].Advice || is[0].Sev != nav.SevWarn || is[1].Advice {
		t.Fatalf("order: %+v", is)
	}

	m.section, m.att = sectionAttention, 2
	_, msgs := enter(m)
	if len(msgs) != 1 {
		t.Fatalf("messages: %v", msgs)
	}
	if g, ok := msgs[0].(nav.GotoMsg); !ok || g.Screen != nav.ScreenSecrets {
		t.Fatalf("enter on Advice: %#v", msgs[0])
	}

	// Advice with a Focus opens its screen and selects the row.
	focus := nav.Issue{Sev: nav.SevInfo, Screen: nav.ScreenWorkspace, Advice: true,
		Text: "Shell completion is off", Focus: workspace.SelectMsg{Name: "completion zsh"}}
	m = model(map[nav.Screen]area{nav.ScreenWorkspace: {loaded: true, issues: []nav.Issue{focus}}})
	m.section, m.att = sectionAttention, 0
	_, msgs = enter(m)
	var gotoOK, focusOK bool
	for _, msg := range msgs {
		switch v := msg.(type) {
		case nav.GotoMsg:
			gotoOK = v.Screen == nav.ScreenWorkspace
		case workspace.SelectMsg:
			focusOK = v.Name == "completion zsh"
		}
	}
	if !gotoOK || !focusOK {
		t.Fatalf("advice with a Focus: goto %v, focus %v, messages %v", gotoOK, focusOK, msgs)
	}
}

func TestAdviceNeverCountsTowardsACardsStatus(t *testing.T) {
	if got := worst([]nav.Issue{passAdvice}); got != nav.SevInfo+1 {
		t.Fatalf("advice alone leaves the card ✔: worst = %v", got)
	}
	if got := worst([]nav.Issue{passAdvice, {Sev: nav.SevWarn}}); got != nav.SevWarn {
		t.Fatalf("worst = %v", got)
	}
	v := ansi.Strip(model(map[nav.Screen]area{nav.ScreenSecrets: {loaded: true, issues: []nav.Issue{passAdvice}}}).View())
	if !strings.Contains(v, "ℹ") {
		t.Errorf("advice has its own mark:\n%s", v)
	}
}

func TestProblemRowsOpenTheirScreen(t *testing.T) {
	m := model(map[nav.Screen]area{
		nav.ScreenSymlinks: {loaded: true, issues: []nav.Issue{{Sev: nav.SevError, Screen: nav.ScreenSymlinks, Text: "2 symlinks broken", Filter: "status=broken"}}},
		nav.ScreenTrash:    {loaded: true, issues: []nav.Issue{{Sev: nav.SevWarn, Screen: nav.ScreenTrash, Text: "Soft-delete is off"}}},
	})
	m.section, m.att = sectionAttention, 0
	_, msgs := enter(m)
	var f *nav.FilterMsg
	for _, msg := range msgs {
		if fm, ok := msg.(nav.FilterMsg); ok {
			f = &fm
		}
	}
	if f == nil || f.Filter != "status=broken" || f.Screen != nav.ScreenSymlinks {
		t.Fatalf("messages: %v", msgs)
	}
	m.att = 1
	_, msgs = enter(m)
	if len(msgs) != 1 {
		t.Fatalf("a row with no filter just opens its Screen: %v", msgs)
	}
	if g, ok := msgs[0].(nav.GotoMsg); !ok || g.Screen != nav.ScreenTrash {
		t.Fatalf("messages: %v", msgs)
	}
}

func TestNoActionKeys(t *testing.T) {
	m := model(map[nav.Screen]area{nav.ScreenTrash: {loaded: true, issues: []nav.Issue{{Sev: nav.SevWarn, Screen: nav.ScreenTrash, Text: "x"}}}})
	m.section = sectionAttention
	for _, k := range []string{"x", "r", "c", "O", "R"} {
		if _, cmd := m.Update(tea.KeyPressMsg{Code: []rune(k)[0], Text: k}); len(collect(cmd)) != 0 {
			t.Errorf("%s does something on the Dashboard", k)
		}
	}
}

// TestSymlinksAreaCountsEntriesAndNamesProblems: "N symlinks managed" counts ws.json
// entries only; broken, missing and drifted entries are one problem line; found
// configs are an info line; untracked links and orphans are not alarms.
func TestSymlinksAreaCountsEntriesAndNamesProblems(t *testing.T) {
	row := func(sys string, st symlink.Status, registered bool) symlink.Entry {
		return symlink.Entry{System: sys, Status: st, Registered: registered}
	}
	a := cardOf(symlinks.LoadedMsg{
		Entries: []symlink.Entry{
			row("/h/.ok", symlink.StatusOK, true),
			row("/h/.broken", symlink.StatusBroken, true),
			row("/h/.missing", symlink.StatusMissing, true),
			row("/h/.drifted", symlink.StatusDrifted, true),
			row("/h/.untracked", symlink.StatusUntracked, false),
			row("/h/.orphan", symlink.StatusOrphan, false),
		},
	})
	if got := a.lines[0]; got != "4 symlinks managed" {
		t.Errorf("first line = %q", got)
	}
	if len(a.issues) != 1 {
		t.Fatalf("issues = %+v", a.issues)
	}
	if is := a.issues[0]; is.Sev != nav.SevError || is.Text != "3 symlinks broken, missing or drifted" {
		t.Errorf("problem line = %+v", is)
	}
	if a := cardOf(symlinks.LoadedMsg{Entries: []symlink.Entry{row("/h/.ok", symlink.StatusOK, true)}}); len(a.issues) != 0 {
		t.Errorf("a healthy list raises nothing: %+v", a.issues)
	}
}

// TestDashboardIgnoresTheWalkedLoad: the Dashboard summarizes the load without the
// $HOME walk; the screen's second, walked load must not replace it.
func TestDashboardIgnoresTheWalkedLoad(t *testing.T) {
	m := model(map[nav.Screen]area{})
	next, _ := m.Update(symlinks.LoadedMsg{Entries: []symlink.Entry{{System: "/h/.a", Status: symlink.StatusOK, Registered: true}}})
	m = next.(Model)
	next, _ = m.Update(symlinks.LoadedMsg{Walked: true, Entries: nil})
	m = next.(Model)
	if got := m.areas[nav.ScreenSymlinks].lines; len(got) == 0 || got[0] != "1 symlink managed" {
		t.Fatalf("area after the walked load = %v", got)
	}
}

// TestIgnoreAreaCountsStaleMutesApartFromIncludedPaths: a muted path that no
// longer exists is an info line, and is not counted as an included-path warning.
func TestIgnoreAreaCountsStaleMutesApartFromIncludedPaths(t *testing.T) {
	stale := wsignore.StaleViolations(t.TempDir(), []string{"a", "b"})
	a := cardOf(ignore.LoadedMsg{Stale: stale})
	var text string
	for _, l := range a.lines {
		text += ansi.Strip(l) + "\n"
	}
	if !strings.Contains(text, "2 stale mutes") || strings.Contains(text, "included paths") {
		t.Errorf("lines:\n%s", text)
	}
	if len(a.issues) != 1 || a.issues[0].Sev != nav.SevInfo || a.issues[0].Text != "2 muted paths no longer exist" {
		t.Errorf("issues: %+v", a.issues)
	}
	if one := cardOf(ignore.LoadedMsg{Stale: stale[:1]}); one.issues[0].Text != "1 muted path no longer exists" {
		t.Errorf("singular: %q", one.issues[0].Text)
	}
	if none := cardOf(ignore.LoadedMsg{}); len(none.issues) != 0 {
		t.Errorf("nothing stale, nothing to say: %+v", none.issues)
	}
}

// TestSessionsCard: agent-tmux's sessions, a stopped recorder as a problem
// that opens the screen filtered to it, the version nudge as a row, and a
// plain card when agent-tmux is missing.
func TestSessionsCard(t *testing.T) {
	m := model(map[nav.Screen]area{})
	next, _ := m.Update(sessions.LoadedMsg{Sessions: []agenttmux.Session{
		{Name: "web", State: "live", Recorder: "stopped"},
		{Name: "api", State: "live", Recorder: "running"},
		{Name: "old", ID: "old-1", State: "ended"},
	}})
	m = next.(Model)
	a := m.areas[nav.ScreenSessions]
	if got := ansi.Strip(strings.Join(a.lines, "|")); got != "2 live · 1 ended|1 recorder stopped" {
		t.Errorf("card = %q", got)
	}
	if len(a.issues) != 1 || a.issues[0].Filter != "recorder=stopped" {
		t.Errorf("issues = %+v", a.issues)
	}

	next, _ = m.Update(sessions.NudgeMsg{Nudge: agenttmux.Nudge{Installed: "v0.1.0", Latest: "v0.2.0"}})
	m = next.(Model)
	if att := strings.Join(m.Attention(), "|"); !strings.Contains(att, "agent-tmux v0.2.0 is available") {
		t.Errorf("attention = %q", att)
	}

	next, _ = m.Update(sessions.LoadedMsg{Missing: true})
	if got := ansi.Strip(strings.Join(next.(Model).areas[nav.ScreenSessions].lines, "|")); got != "agent-tmux not installed" {
		t.Errorf("card without agent-tmux = %q", got)
	}
}

func TestVersionFooterIsTheLastRow(t *testing.T) {
	m := model(map[nav.Screen]area{})
	m.env.Version = "v0.20.0"

	view := func(m Model) []string { return strings.Split(ansi.Strip(m.View()), "\n") }
	rows := view(m)
	if len(rows) != m.height {
		t.Fatalf("view is %d rows, want %d", len(rows), m.height)
	}
	if got := strings.TrimSpace(rows[len(rows)-1]); got != "ws v0.20.0" {
		t.Errorf("footer = %q, want ws only while agent-tmux's version is unknown", got)
	}

	next, _ := m.Update(agentTmuxVersionMsg{Version: "v0.19.0"})
	rows = view(next.(Model))
	if got := strings.TrimSpace(rows[len(rows)-1]); got != "ws v0.20.0  ·  agent-tmux v0.19.0" {
		t.Errorf("footer = %q", got)
	}
}
