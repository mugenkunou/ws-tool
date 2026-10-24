package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	wscron "github.com/mugenkunou/ws-tool/internal/cron"
	"github.com/mugenkunou/ws-tool/internal/dotfile"
	wsignore "github.com/mugenkunou/ws-tool/internal/ignore"
	wslog "github.com/mugenkunou/ws-tool/internal/log"
	"github.com/mugenkunou/ws-tool/internal/repo"
	wsscratch "github.com/mugenkunou/ws-tool/internal/scratch"
	"github.com/mugenkunou/ws-tool/internal/secret"
	wstrash "github.com/mugenkunou/ws-tool/internal/trash"
	"github.com/mugenkunou/ws-tool/internal/tui/env"
	"github.com/mugenkunou/ws-tool/internal/tui/keys"
	"github.com/mugenkunou/ws-tool/internal/tui/nav"
	"github.com/mugenkunou/ws-tool/internal/tui/screens/cron"
	"github.com/mugenkunou/ws-tool/internal/tui/screens/dotfiles"
	"github.com/mugenkunou/ws-tool/internal/tui/screens/ignore"
	"github.com/mugenkunou/ws-tool/internal/tui/screens/logs"
	"github.com/mugenkunou/ws-tool/internal/tui/screens/repos"
	"github.com/mugenkunou/ws-tool/internal/tui/screens/scratch"
	"github.com/mugenkunou/ws-tool/internal/tui/screens/secrets"
	"github.com/mugenkunou/ws-tool/internal/tui/screens/trash"
	"github.com/mugenkunou/ws-tool/internal/tui/theme"
	"github.com/mugenkunou/ws-tool/internal/tui/tuitest"
	"github.com/mugenkunou/ws-tool/internal/workspace"
	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

const ws = "/home/tester/Workspace"

func init() {
	// Screens render times in local time; pin it so snapshots are stable.
	time.Local = time.UTC
	theme.SetEmoji(true)
}

// fixedClock is a mid-morning instant so the dashboard greeting is stable.
func fixedClock() time.Time { return time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC) }

func testEnv(t *testing.T) env.Env {
	t.Helper()
	t.Setenv("HOME", "/home/tester")
	cfg := wsconfig.Default()
	return env.Env{
		Paths: workspace.Paths{
			Workspace: ws,
			Config:    ws + "/ws.json",
		},
		Config:     cfg,
		ScratchDir: "/home/tester/Scratch",
		LogDir:     ws + "/data/ws-log",
		Clock:      fixedClock,
	}
}

func newApp(t *testing.T, loadErr error) Model {
	return New(workspace.PathOverrides{}, testEnv(t), loadErr)
}

// fixtures are the LoadedMsgs the screens' commands would produce.
func fixtures() []tea.Msg {
	return []tea.Msg{
		repos.LoadedMsg{Entries: []repos.Entry{
			{Status: repo.RepoStatus{Path: ws + "/Projects/api", Branch: "main", HasUpstream: true}},
			{Status: repo.RepoStatus{Path: ws + "/Projects/web", Branch: "feature/login", Dirty: true, Ahead: 2, HasUpstream: true},
				Findings: []repo.Finding{{Repo: ws + "/Projects/web", Check: "identity", Severity: repo.SeverityWarn, Detail: "user.email not set"}}},
			{Status: repo.RepoStatus{Path: ws + "/Projects/old", Detached: true, Behind: 5, HasUpstream: true}},
			{Status: repo.RepoStatus{Path: "/home/tester/.password-store", Branch: "master"}},
		}},
		dotfiles.LoadedMsg{Entries: []dotfiles.Entry{
			{Link: wsconfig.Link{System: "/home/tester/.bashrc", Original: "configs/dotfiles/bashrc"}, WorkspacePath: "/home/tester/Workspace/configs/dotfiles/bashrc"},
			{Link: wsconfig.Link{System: "/home/tester/.gitconfig", Original: "configs/dotfiles/gitconfig"}, WorkspacePath: "/home/tester/Workspace/configs/dotfiles/gitconfig", Status: dotfile.StatusBroken},
			{Link: wsconfig.Link{System: "/etc/hosts", Original: "configs/dotfiles/etc/hosts", Sudo: true}, WorkspacePath: "/home/tester/Workspace/configs/dotfiles/etc/hosts", Status: dotfile.StatusOverwritten},
		}},
		scratch.LoadedMsg{
			Entries: []wsscratch.Entry{
				{Name: "dns-debug.2026-09", Path: "/home/tester/Scratch/dns-debug.2026-09", Age: 3 * time.Hour, SizeBytes: 2048, Items: 4, Tags: []string{"dns", "network"}},
				{Name: "proxy-timeout.2026-08", Path: "/home/tester/Scratch/proxy-timeout.2026-08", Age: 120 * 24 * time.Hour, SizeBytes: 5 << 20, Items: 17},
			},
			Tags: []string{"dns", "network", "proxy"},
		},
		logs.LoadedMsg{Sessions: []wslog.Session{
			{Tag: "deploy-fix", StartedAt: time.Date(2026, 9, 30, 14, 5, 0, 0, time.UTC), Commands: 12, SizeBytes: 40960, Active: true},
			{Tag: "rsync-backup", StartedAt: time.Date(2026, 9, 28, 9, 30, 0, 0, time.UTC), DurationSec: 1865, Commands: 7, SizeBytes: 8192},
		}},
		ignore.LoadedMsg{Violations: []wsignore.Violation{
			{Type: "bloat", Severity: "CRITICAL", Path: "Projects/web/dump.log", SizeBytes: 220 << 20},
			{Type: "depth", Severity: "WARNING", Path: "Notes/a/b/c/d/e/f/g", Depth: 7},
			{Type: "bloat", Severity: "WARNING", Path: "Data/bruno/node_modules", SizeBytes: 40 << 20, Muted: true},
		}, Ignored: []wsignore.IgnoredPath{
			{Path: "Projects/web/.env.local", SizeBytes: 412, Rule: "-g:.env*", RuleSource: "builtin"},
			{Path: "Projects/web/node_modules/.bin/vite", SizeBytes: 1 << 20, Rule: "node_modules/", RuleSource: "user"},
		}},
		secrets.LoadedMsg{
			Violations: []secret.Violation{{Severity: "CRITICAL", Path: "Projects/api/.env", Line: 3, Snippet: "password=hunter2"}},
			Health:     secrets.Health{Pass: secret.PassHealth{Installed: true, GPGAvailable: true, Initialized: true, GitBacked: true, EntryCount: 42}},
		},
		cron.LoadedMsg{Jobs: []cron.Job{
			{Name: "dotfile-backup", Schedule: "0 * * * *", Description: "commit + push dotfiles", Installed: true, CrontabOK: true,
				Interval: time.Hour, LastRun: wscron.RunRecord{Job: "dotfile-backup", Time: fixedClock().Add(-2 * time.Hour)}},
			{Name: "repo-sync", Schedule: "*/30 * * * *", Description: "sync repo fleet", Installed: true, CrontabOK: true,
				Interval: 30 * time.Minute, LastRun: wscron.RunRecord{Job: "repo-sync", Time: fixedClock().Add(-40 * time.Minute), ExitCode: 2}},
			{Name: "all", Preset: true, CrontabOK: true, Description: "installs: dotfile-backup, repo-sync"},
		}},
		trash.LoadedMsg{
			Status: wstrash.Status{
				Root: "/home/tester/.Trash", Items: 2, Files: 12, SizeBytes: 3 << 20, WarnSizeBytes: 1 << 30,
				Integrations: []wstrash.Integration{
					{Name: wstrash.ShellRM, Enabled: true}, {Name: wstrash.VSCode, Enabled: true}, {Name: wstrash.FileExplorer},
				},
			},
			Items: []trash.Item{
				{Name: "old-notes.md", OriginalPath: ws + "/Notes/old-notes.md", SizeBytes: 2048, Files: 1, DeletedAt: fixedClock().Add(-3 * time.Hour)},
				{Name: "build", SizeBytes: 3 << 20, Files: 40, DeletedAt: fixedClock().Add(-50 * time.Hour), Dir: true},
			},
		},
	}
}

// send feeds msgs through Update, discarding commands (no IO in tests).
func send(m Model, msgs ...tea.Msg) Model {
	for _, msg := range msgs {
		next, _ := m.Update(msg)
		m = next.(Model)
	}
	return m
}

// sendRun feeds msg and returns the resulting command too.
func sendRun(m Model, msg tea.Msg) (Model, tea.Cmd) {
	next, cmd := m.Update(msg)
	return next.(Model), cmd
}

func loaded(t *testing.T, w, h int) Model {
	t.Helper()
	m := send(newApp(t, nil), tea.WindowSizeMsg{Width: w, Height: h})
	return send(m, fixtures()...)
}

func press(s string) tea.KeyPressMsg {
	switch s {
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "shift+tab":
		return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	case "ctrl+c":
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	case "ctrl+space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Mod: tea.ModCtrl}
	}
	if strings.HasPrefix(s, "ctrl+") && len(s) == len("ctrl+")+1 {
		return tea.KeyPressMsg{Code: rune(s[len(s)-1]), Mod: tea.ModCtrl}
	}
	r := []rune(s)[0]
	return tea.KeyPressMsg{Code: r, Text: s}
}

func typeText(m Model, s string) Model {
	for _, r := range s {
		m = send(m, press(string(r)))
	}
	return m
}

func view(m Model) string { return m.View().Content }

// pump runs cmd and feeds its messages (expanding batches) back into the
// model, repeatedly, so a flow completes. Only for tests whose environment is
// a hermetic temp workspace.
func pump(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	queue := []tea.Cmd{cmd}
	for steps := 0; len(queue) > 0; steps++ {
		if steps > 500 {
			t.Fatal("pump: too many steps")
		}
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		switch msg := c().(type) {
		case nil:
		case tea.BatchMsg:
			queue = append(queue, msg...)
		case tea.QuitMsg:
			t.Fatal("pump: unexpected quit")
		default:
			var next tea.Cmd
			m, next = sendRun(m, msg)
			queue = append(queue, next)
		}
	}
	return m
}

func TestSnapshots(t *testing.T) {
	cases := []struct {
		name string
		keys []string
	}{
		{"dashboard", nil},
		{"dashboard_grid_cursor", []string{"down", "down", "down", "down", "down", "down", "down", "down", "down", "right", "down"}},
		{"repos", []string{"2"}},
		{"repos_detail", []string{"2", "down", "enter"}},
		{"repos_pull_fetching", []string{"2", "p"}},
		{"repos_filtered_pull", []string{"2", "/", "s", "t", "a", "t", "e", "=", "c", "l", "e", "a", "n", "enter", "p"}},
		{"repos_marked_fetch", []string{"2", "space", "down", "space", "down", "space", "f"}},
		{"repos_marked", []string{"2", "space", "down", "space", "down", "space"}},
		{"repos_range_marked", []string{"2", "down", "space", "down", "down", "ctrl+space"}},
		{"dotfiles", []string{"3"}},
		{"dotfiles_reset_checklist", []string{"3", "R"}},
		{"dotfiles_filter", []string{"3", "/", "s", "t", "a", "t", "u", "s", "!", "=", "o", "k", "enter"}},
		{"dotfiles_sorted_marked", []string{"3", ">", "space", "down", "space"}},
		{"dotfiles_marked_remove", []string{"3", "space", "down", "space", "ctrl+d"}},
		{"dotfiles_marked_reset", []string{"3", "down", "space", "R"}},
		{"dotfiles_detail", []string{"3", "down", "d"}},
		{"scratch", []string{"4"}},
		{"scratch_new_prompt", []string{"4", "n", "d", "n"}},
		{"scratch_tag_prompt", []string{"4", "t", "n"}},
		{"logs", []string{"5"}},
		{"ignore", []string{"6"}},
		{"ignore_muted", []string{"6", "v"}},
		{"ignore_ignored", []string{"6", "v", "v"}},
		{"ignore_marked", []string{"6", "space", "down", "space"}},
		{"secrets", []string{"7"}},
		{"secrets_detail", []string{"7", "d"}},
		{"cron", []string{"8"}},
		{"trash", []string{"9"}},
		{"trash_purge_prompt", []string{"9", "p"}},
		{"trash_marked_delete", []string{"9", "ctrl+a", "ctrl+d"}},
		{"trash_marked_restore", []string{"9", "space", "r"}},
		{"full_help", []string{"2", "?"}},
		{"repos_json", []string{"2", "down", "y"}},
		{"command_bar", []string{"3", ":", "d", "o"}},
		{"command_bar_empty", []string{"2", ":"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := loaded(t, 100, 30)
			for _, k := range c.keys {
				m = send(m, press(k))
			}
			tuitest.Golden(t, c.name, view(m))
		})
	}
}

func TestSnapshotLoading(t *testing.T) {
	m := send(newApp(t, nil), tea.WindowSizeMsg{Width: 100, Height: 30})
	tuitest.Golden(t, "dashboard_loading", view(m))
}

func TestSnapshotNotInitialized(t *testing.T) {
	m := send(newApp(t, env.ErrNotInitialized), tea.WindowSizeMsg{Width: 80, Height: 24})
	tuitest.Golden(t, "not_initialized", view(m))
}

func TestSnapshotLoadError(t *testing.T) {
	m := loaded(t, 80, 24)
	m = send(m, dotfiles.LoadedMsg{Err: errors.New("ws.json: permission denied")}, press("3"))
	tuitest.Golden(t, "dotfiles_error", view(m))
}

func TestSnapshotNotice(t *testing.T) {
	m := loaded(t, 80, 24)
	m = send(m, press("2"), nav.NoticeMsg{Level: nav.LevelSuccess, Text: "3 done · 1 skipped"})
	tuitest.Golden(t, "notice", view(m))
	if m = send(m, press("down")); strings.Contains(view(m), "3 done") {
		t.Fatal("notice not dismissed by the next key press")
	}
}

// TestResize renders every screen (and an open dialog) at several sizes,
// including resizing a live model, and asserts nothing overflows.
func TestResize(t *testing.T) {
	sizes := [][2]int{{120, 40}, {80, 24}, {60, 15}, {40, 12}, {20, 6}, {1, 1}}
	flows := [][]string{{"1"}, {"2"}, {"3"}, {"4"}, {"5"}, {"6"}, {"7"}, {"8"}, {"9"},
		{"2", "enter"}, {"2", "p"}, {"4", "n"}, {"1", "R"}, {"1", "down", "down", "down", "down", "down", "down", "down", "down", "right"}}
	for _, sz := range sizes {
		for _, flow := range flows {
			m := loaded(t, 100, 30)
			for _, k := range flow {
				m = send(m, press(k))
			}
			m = send(m, tea.WindowSizeMsg{Width: sz[0], Height: sz[1]})
			tuitest.AssertFits(t, view(m), sz[0], sz[1])
			if !m.current().CapturesKeys() {
				m = send(m, press("?"))
				tuitest.AssertFits(t, view(m), sz[0], sz[1])
			}
		}
	}
}

func TestResizeRestoresLayout(t *testing.T) {
	for _, flow := range [][]string{nil, {"2", "p"}, {"8"}, {"9"}} {
		m := loaded(t, 80, 24)
		for _, k := range flow {
			m = send(m, press(k))
		}
		before := view(m)
		m = send(m, tea.WindowSizeMsg{Width: 30, Height: 8}, tea.WindowSizeMsg{Width: 80, Height: 24})
		if after := view(m); after != before {
			t.Errorf("flow %v: layout changed after shrinking and restoring\n--- before\n%s\n--- after\n%s",
				flow, tuitest.Plain(before), tuitest.Plain(after))
		}
	}
}

func TestScreenSwitching(t *testing.T) {
	m := loaded(t, 80, 24)
	for _, k := range []string{"tab", "]"} {
		for _, want := range append(nav.Screens()[1:], nav.ScreenDashboard) {
			m = send(m, press(k))
			if m.active != want {
				t.Fatalf("%s: active = %v, want %v", k, m.active.Title(), want.Title())
			}
		}
	}
	for _, k := range []string{"shift+tab", "["} {
		if m = send(m, press(k)); m.active != nav.ScreenTrash {
			t.Fatalf("%s from dashboard: active = %v, want Trash", k, m.active.Title())
		}
		m = send(m, press("1"))
	}
	for i, s := range nav.Screens() {
		if m = send(m, press(keys.ScreenDigit(i))); m.active != s {
			t.Fatalf("key %s: active = %v, want %v", keys.ScreenDigit(i), m.active.Title(), s.Title())
		}
	}
}

// TestGlobalKeysLeavePageKeysAlone enforces the key grammar in package keys:
// global bindings never take keys a page needs.
func TestGlobalKeysLeavePageKeysAlone(t *testing.T) {
	reserved := map[string]bool{
		"up": true, "down": true, "left": true, "right": true, "h": true, "j": true, "k": true, "l": true,
		"enter": true, "esc": true, "space": true,
	}
	g := newApp(t, nil).keys
	all := append([]key.Binding{g.Quit, g.ForceQuit, g.Help, g.NextTab, g.PrevTab}, g.Screens...)
	for _, b := range all {
		for _, k := range b.Keys() {
			if reserved[k] {
				t.Errorf("global binding uses page key %q", k)
			}
		}
	}
}

// TestDashboardRanksProblemsFirst: errors lead the attention list.
func TestDashboardRanksProblemsFirst(t *testing.T) {
	v := tuitest.Plain(view(loaded(t, 100, 30)))
	errAt := strings.Index(v, "dotfile links broken")
	warnAt := strings.Index(v, "out of sync")
	if errAt < 0 || warnAt < 0 || errAt > warnAt {
		t.Fatalf("errors should precede warnings:\n%s", v)
	}
	if !strings.Contains(v, "Needs attention") || !strings.Contains(v, "Good morning") {
		t.Fatalf("missing sections:\n%s", v)
	}
}

// dashboardTo moves the dashboard cursor to the first row containing text.
func dashboardTo(t *testing.T, m Model, text string) Model {
	t.Helper()
	for range 30 {
		for _, line := range strings.Split(tuitest.Plain(view(m)), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "›") && strings.Contains(line, text) {
				return m
			}
		}
		m = send(m, press("down"))
	}
	t.Fatalf("no dashboard row %q:\n%s", text, tuitest.Plain(view(m)))
	return m
}

// apply runs cmd and feeds its messages (batches expanded) back, once.
func apply(m Model, cmd tea.Cmd) Model {
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
			m = send(m, msg)
		}
	}
	return m
}

// collect runs cmd and returns every message it produces, batches expanded.
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

// TestDashboardHasNoActionKeys: the Dashboard has no verbs of its own (spec
// §6.5): x, r, c, O and R do nothing there.
func TestDashboardHasNoActionKeys(t *testing.T) {
	for _, k := range []string{"x", "r", "c", "O", "R", "a", "d"} {
		m := dashboardTo(t, loaded(t, 100, 30), "dotfile links broken")
		m2, cmd := sendRun(m, press(k))
		if cmds := collect(cmd); len(cmds) != 0 || m2.active != nav.ScreenDashboard || m2.current().CapturesKeys() {
			t.Errorf("%s on the Dashboard produced %v (active %v)", k, cmds, m2.active.Title())
		}
	}
}

// TestDashboardOpensTheScreenFiltered: enter on a problem opens its screen
// filtered to the affected rows, so the screen's own key acts on exactly those.
func TestDashboardOpensTheScreenFiltered(t *testing.T) {
	m := dashboardTo(t, loaded(t, 100, 30), "dotfile links broken")
	_, cmd := sendRun(m, press("enter"))
	var gotoDotfiles, filtered bool
	for _, msg := range collect(cmd) {
		switch msg := msg.(type) {
		case nav.GotoMsg:
			gotoDotfiles = msg.Screen == nav.ScreenDotfiles
		case nav.FilterMsg:
			filtered = msg.Screen == nav.ScreenDotfiles && msg.Filter == "status!=ok"
		}
	}
	if !gotoDotfiles || !filtered {
		t.Fatalf("goto %v filter %v", gotoDotfiles, filtered)
	}
}

// TestDashboardGrid: arrows move spatially between cards and never open a
// screen; ↑ from the top row returns to the attention list; enter opens.
func TestDashboardGrid(t *testing.T) {
	m := loaded(t, 100, 30) // 3 columns: Repos Dotfiles Scratch / Logs Ignore Secrets / Cron Trash
	selected := func(m Model) string { return focusedCard(t, m) }
	// Leave the attention list for the grid's first card.
	for range 8 {
		m = send(m, press("down"))
	}
	if got := selected(m); got != "Repos" {
		t.Fatalf("after leaving the list: %q, want Repos", got)
	}
	steps := []struct{ key, want string }{
		{"right", "Dotfiles"}, {"right", "Scratch"}, {"right", "Scratch"}, // edge stops
		{"down", "Secrets"}, {"left", "Ignore"}, {"down", "Trash"}, {"down", "Trash"},
		{"up", "Ignore"}, {"up", "Dotfiles"}, {"left", "Repos"}, {"l", "Dotfiles"}, {"h", "Repos"},
	}
	for _, st := range steps {
		m = send(m, press(st.key))
		if m.active != nav.ScreenDashboard {
			t.Fatalf("%s opened %s — only enter may open", st.key, m.active.Title())
		}
		if got := selected(m); got != st.want {
			t.Fatalf("after %s: %q, want %q", st.key, got, st.want)
		}
	}
	m = send(m, press("up")) // top row → back into the attention list
	if !strings.Contains(tuitest.Plain(view(m)), "›") {
		t.Fatal("↑ from the top row did not return to the attention list")
	}
	m = send(m, press("down"), press("right"), press("down")) // → Ignore
	m, cmd := sendRun(m, press("enter"))
	m = apply(m, cmd)
	if m.active != nav.ScreenIgnore {
		t.Fatalf("enter on Ignore card opened %v", m.active.Title())
	}
}

// focusedCard reports which card holds the cursor: only the focused card's
// title is rendered in the accent style (theme.Title).
func focusedCard(t *testing.T, m Model) string {
	t.Helper()
	v := m.screens[nav.ScreenDashboard].View()
	for _, s := range nav.Screens()[1:] {
		if strings.Contains(v, theme.Title.Render(theme.Icon(s.Icon())+s.Title())) {
			return s.Title()
		}
	}
	return ""
}

func TestEmojiOff(t *testing.T) {
	theme.SetEmoji(false)
	defer theme.SetEmoji(true)
	m := loaded(t, 100, 30)
	tuitest.Golden(t, "dashboard_no_emoji", view(m))
	if v := view(m); strings.ContainsAny(v, "🏠📦🚨✨🌅") {
		t.Fatal("emoji rendered with emoji off")
	}
}

func TestSpinnerAnimates(t *testing.T) {
	m := send(newApp(t, nil), tea.WindowSizeMsg{Width: 100, Height: 30})
	a := view(m)
	m = send(m, frameTickMsg{})
	if view(m) == a {
		t.Fatal("frame tick did not advance the loading spinner")
	}
}

// esc pressed and its resulting command (if any) applied.
func esc(m Model) Model {
	m, cmd := sendRun(m, press("esc"))
	if cmd != nil {
		if msg := cmd(); msg != nil {
			m = send(m, msg)
		}
	}
	return m
}

// TestEscMovesOneLevelUp walks detail → list → dashboard → (stays).
func TestEscMovesOneLevelUp(t *testing.T) {
	m := loaded(t, 80, 24)
	m = send(m, press("2"), press("enter"))
	detail := view(m)

	m = esc(m)
	if m.active != nav.ScreenRepos || view(m) == detail {
		t.Fatalf("esc from detail should return to the repos list (active = %v)", m.active.Title())
	}
	m = esc(m)
	if m.active != nav.ScreenDashboard {
		t.Fatalf("esc from repos list: active = %v, want Dashboard", m.active.Title())
	}
	m = esc(m)
	if m.active != nav.ScreenDashboard {
		t.Fatalf("esc at dashboard: active = %v, want Dashboard", m.active.Title())
	}
}

// TestEscClosesDialogFirst: with a dialog open, esc closes only the dialog.
func TestEscClosesDialogFirst(t *testing.T) {
	m := loaded(t, 80, 24)
	m = send(m, press("4"), press("n"))
	if !m.current().CapturesKeys() {
		t.Fatal("prompt did not open")
	}
	m = esc(m)
	if m.current().CapturesKeys() {
		t.Fatal("esc did not close the prompt")
	}
	if m.active != nav.ScreenScratch {
		t.Fatalf("esc in a dialog left the screen (active = %v)", m.active.Title())
	}
}

// TestDialogCapturesGlobalKeys: q, digits and tab type into a prompt instead
// of quitting or switching screens; ctrl+c still quits.
func TestDialogCapturesGlobalKeys(t *testing.T) {
	m := loaded(t, 80, 24)
	m = send(m, press("4"), press("n"))
	for _, k := range []string{"q", "2", "?"} {
		var cmd tea.Cmd
		m, cmd = sendRun(m, press(k))
		if cmd != nil {
			if _, quit := cmd().(tea.QuitMsg); quit {
				t.Fatalf("%q quit while a prompt was open", k)
			}
		}
		if m.active != nav.ScreenScratch {
			t.Fatalf("%q switched screens while a prompt was open", k)
		}
	}
	if !strings.Contains(tuitest.Plain(view(m)), "› q2?") {
		t.Fatalf("typed keys did not reach the prompt:\n%s", tuitest.Plain(view(m)))
	}
	_, cmd := sendRun(m, press("ctrl+c"))
	if cmd == nil {
		t.Fatal("ctrl+c returned no command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("ctrl+c did not quit while a prompt was open")
	}
}

func TestQuit(t *testing.T) {
	for _, m := range []Model{loaded(t, 80, 24), newApp(t, env.ErrNotInitialized)} {
		_, cmd := sendRun(m, press("q"))
		if cmd == nil {
			t.Fatal("q returned no command")
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Fatal("q did not quit")
		}
	}
}

func TestRefreshFromDashboard(t *testing.T) {
	m := loaded(t, 80, 24)
	_, cmd := sendRun(m, tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("ctrl+r on the Dashboard returned no reload commands")
	}
}

func TestViewIsPure(t *testing.T) {
	m := loaded(t, 80, 24)
	if view(m) != view(m) {
		t.Fatal("View is not deterministic")
	}
}

// hermetic points every external integration at temp locations.
func hermetic(t *testing.T) (root string) {
	t.Helper()
	root = t.TempDir()
	home := filepath.Join(root, "home")
	for _, d := range []string{home, filepath.Join(root, "xdg"), filepath.Join(root, "pass")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	crontab := filepath.Join(root, "crontab")
	if err := os.WriteFile(crontab, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "xdg"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "xdg-data"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "xdg-state"))
	t.Setenv("PASSWORD_STORE_DIR", filepath.Join(root, "pass"))
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(root, "gitconfig"))
	t.Setenv("WS_CRONTAB_FILE", crontab)
	t.Setenv("WS_WORKSPACE", "")
	return root
}

// TestScratchNewEndToEnd drives prompt → checklist → apply against a real
// temp workspace and checks the directory exists afterwards.
func TestScratchNewEndToEnd(t *testing.T) {
	root := hermetic(t)
	wsPath := filepath.Join(root, "Workspace")
	configsDir := filepath.Join(wsPath, "configs")
	if err := os.MkdirAll(configsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := wsconfig.Default()
	cfg.Scratch.RootDir = filepath.Join(root, "Scratch")
	cfg.Editor = "true" // the editor launched after creation
	cfgPath := filepath.Join(wsPath, "ws.json")
	if err := wsconfig.Save(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	o := workspace.PathOverrides{Workspace: wsPath, Config: cfgPath}
	e, err := env.Load(o)
	if err != nil {
		t.Fatal(err)
	}

	m := New(o, e, nil)
	m = send(m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = pump(t, m, m.loadAll())
	m = send(m, press("4"), press("n"))
	m = typeText(m, "e2e-demo")
	m, cmd := sendRun(m, press("enter")) // submit name → checklist
	m = pump(t, m, cmd)
	if !strings.Contains(tuitest.Plain(view(m)), "Create scratch directory e2e-demo") {
		t.Fatalf("checklist not shown:\n%s", tuitest.Plain(view(m)))
	}
	m, cmd = sendRun(m, press("enter")) // apply
	m = pump(t, m, cmd)
	if !strings.Contains(tuitest.Plain(view(m)), "1 done") {
		t.Fatalf("plan did not finish:\n%s", tuitest.Plain(view(m)))
	}
	m, cmd = sendRun(m, press("enter")) // close → notice + reload
	m = pump(t, m, cmd)

	entries, err := os.ReadDir(cfg.Scratch.RootDir)
	if err != nil || len(entries) != 1 || !strings.HasPrefix(entries[0].Name(), "e2e-demo") {
		t.Fatalf("scratch dir not created: %v %v", entries, err)
	}
	if !strings.Contains(tuitest.Plain(view(m)), entries[0].Name()) {
		t.Fatalf("list not reloaded with the new dir:\n%s", tuitest.Plain(view(m)))
	}
}

// TestCancelChangesNothing: esc in a checklist runs no action.
func TestCancelChangesNothing(t *testing.T) {
	root := hermetic(t)
	wsPath := filepath.Join(root, "Workspace")
	configsDir2 := filepath.Join(wsPath, "configs")
	if err := os.MkdirAll(configsDir2, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg2 := wsconfig.Default()
	cfg2.Scratch.RootDir = filepath.Join(root, "Scratch")
	cfgPath2 := filepath.Join(wsPath, "ws.json")
	_ = wsconfig.Save(cfgPath2, cfg2)
	o := workspace.PathOverrides{Workspace: wsPath, Config: cfgPath2}
	e, err := env.Load(o)
	if err != nil {
		t.Fatal(err)
	}
	m := send(New(o, e, nil), tea.WindowSizeMsg{Width: 100, Height: 30})
	m = send(m, press("4"), press("n"))
	m = typeText(m, "never")
	m, cmd := sendRun(m, press("enter"))
	m = pump(t, m, cmd)
	m, cmd = sendRun(m, press("esc"))
	m = pump(t, m, cmd)
	if _, err := os.Stat(cfg2.Scratch.RootDir); !os.IsNotExist(err) {
		entries, _ := os.ReadDir(cfg2.Scratch.RootDir)
		if len(entries) > 0 {
			t.Fatalf("cancel created %v", entries)
		}
	}
	if m.current().CapturesKeys() {
		t.Fatal("checklist still open after esc")
	}
}
