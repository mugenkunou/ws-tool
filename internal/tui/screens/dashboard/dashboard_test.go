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

func enter(m Model) (Model, []tea.Msg) {
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	return next.(Model), collect(cmd)
}

var passAdvice = issue{
	sev: sevInfo, screen: nav.ScreenSecrets, advice: true,
	text:    "Your pass store syncs only through its own git remote. A copy in the workspace would sync with MEGA too.",
	prefill: "symlink add ~/.password-store --dir",
}

func TestAdviceIsListedLastAndOpensTheCommandBar(t *testing.T) {
	m := model(map[nav.Screen]area{
		nav.ScreenSecrets: {loaded: true, issues: []issue{passAdvice, {sev: sevWarn, screen: nav.ScreenSecrets, text: "Pass store has no git remote"}}},
		nav.ScreenRepos:   {loaded: true, issues: []issue{{sev: sevInfo, screen: nav.ScreenRepos, text: "1 hygiene finding in 1 repo"}}},
	})
	is := m.issues()
	if len(is) != 3 || !is[2].advice || is[0].sev != sevWarn || is[1].advice {
		t.Fatalf("order: %+v", is)
	}

	m.section, m.att = sectionAttention, 2
	_, msgs := enter(m)
	if len(msgs) != 1 {
		t.Fatalf("messages: %v", msgs)
	}
	open, ok := msgs[0].(nav.OpenCommandBarMsg)
	if !ok || open.Prefill != "symlink add ~/.password-store --dir" {
		t.Fatalf("enter on advice: %#v", msgs[0])
	}
}

func TestAdviceNeverCountsTowardsACardsStatus(t *testing.T) {
	if got := worst([]issue{passAdvice}); got != sevInfo+1 {
		t.Fatalf("advice alone leaves the card ✔: worst = %v", got)
	}
	if got := worst([]issue{passAdvice, {sev: sevWarn}}); got != sevWarn {
		t.Fatalf("worst = %v", got)
	}
	v := ansi.Strip(model(map[nav.Screen]area{nav.ScreenSecrets: {loaded: true, issues: []issue{passAdvice}}}).View())
	if !strings.Contains(v, "ℹ") {
		t.Errorf("advice has its own mark:\n%s", v)
	}
}

func TestProblemRowsOpenTheirScreen(t *testing.T) {
	m := model(map[nav.Screen]area{
		nav.ScreenSymlinks: {loaded: true, issues: []issue{{sev: sevError, screen: nav.ScreenSymlinks, text: "2 symlinks broken", filter: "status=broken"}}},
		nav.ScreenTrash:    {loaded: true, issues: []issue{{sev: sevWarn, screen: nav.ScreenTrash, text: "Soft-delete is off"}}},
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
		t.Fatalf("a row with no filter just opens its screen: %v", msgs)
	}
	if g, ok := msgs[0].(nav.GotoMsg); !ok || g.Screen != nav.ScreenTrash {
		t.Fatalf("messages: %v", msgs)
	}
}

func TestNoActionKeys(t *testing.T) {
	m := model(map[nav.Screen]area{nav.ScreenTrash: {loaded: true, issues: []issue{{sev: sevWarn, screen: nav.ScreenTrash, text: "x"}}}})
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
	a := symlinksArea(symlinks.LoadedMsg{
		Entries: []symlink.Entry{
			row("/h/.ok", symlink.StatusOK, true),
			row("/h/.broken", symlink.StatusBroken, true),
			row("/h/.missing", symlink.StatusMissing, true),
			row("/h/.drifted", symlink.StatusDrifted, true),
			row("/h/.untracked", symlink.StatusUntracked, false),
			row("/h/.orphan", symlink.StatusOrphan, false),
		},
		Found: 4,
	})
	if got := a.lines[0]; got != "4 symlinks managed" {
		t.Errorf("first line = %q", got)
	}
	if len(a.issues) != 2 {
		t.Fatalf("issues = %+v", a.issues)
	}
	if is := a.issues[0]; is.sev != sevError || is.text != "3 symlinks broken, missing or drifted" {
		t.Errorf("problem line = %+v", is)
	}
	if is := a.issues[1]; is.sev != sevInfo || is.text != "4 known configs found, not symlinked" || is.advice {
		t.Errorf("info line = %+v", is)
	}
	if a := symlinksArea(symlinks.LoadedMsg{Entries: []symlink.Entry{row("/h/.ok", symlink.StatusOK, true)}}); len(a.issues) != 0 {
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
	a := ignoreArea(ignore.LoadedMsg{Stale: stale})
	var text string
	for _, l := range a.lines {
		text += ansi.Strip(l) + "\n"
	}
	if !strings.Contains(text, "2 stale mutes") || strings.Contains(text, "included paths") {
		t.Errorf("lines:\n%s", text)
	}
	if len(a.issues) != 1 || a.issues[0].sev != sevInfo || a.issues[0].text != "2 muted paths no longer exist" {
		t.Errorf("issues: %+v", a.issues)
	}
	if one := ignoreArea(ignore.LoadedMsg{Stale: stale[:1]}); one.issues[0].text != "1 muted path no longer exists" {
		t.Errorf("singular: %q", one.issues[0].text)
	}
	if none := ignoreArea(ignore.LoadedMsg{}); len(none.issues) != 0 {
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
	if len(a.issues) != 1 || a.issues[0].filter != "recorder=stopped" {
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
