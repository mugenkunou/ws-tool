package repos

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/mugenkunou/ws-tool/internal/repo"
	"github.com/mugenkunou/ws-tool/internal/tui/env"
	"github.com/mugenkunou/ws-tool/internal/tui/handover"
	"github.com/mugenkunou/ws-tool/internal/tui/nav"
	"github.com/mugenkunou/ws-tool/internal/tui/reslist"
	"github.com/mugenkunou/ws-tool/internal/tui/tuitest"
	"github.com/mugenkunou/ws-tool/internal/workspace"
	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

// testEnv is a hermetic workspace in a temp dir; paths show relative to it.
func testEnv(t *testing.T) env.Env {
	t.Helper()
	ws := t.TempDir()
	t.Setenv("HOME", ws)
	return env.Env{
		Paths:  workspace.Paths{Workspace: ws, Config: filepath.Join(ws, "ws.json")},
		Config: wsconfig.Default(),
		Clock:  func() time.Time { return time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC) },
	}
}

func st(e env.Env, name string, mod func(*repo.RepoStatus)) Entry {
	s := repo.RepoStatus{Path: filepath.Join(e.Paths.Workspace, name), Branch: "main", HasUpstream: true}
	if mod != nil {
		mod(&s)
	}
	return Entry{Status: s}
}

func newModel(t *testing.T, e env.Env, entries ...Entry) Model {
	t.Helper()
	m := New(e)
	m = update(t, m, nav.ResizeMsg{Width: 100, Height: 28})
	return update(t, m, LoadedMsg{Entries: entries})
}

func update(t *testing.T, m Model, msgs ...tea.Msg) Model {
	t.Helper()
	for _, msg := range msgs {
		next, _ := m.Update(msg)
		m = next.(Model)
	}
	return m
}

func updateRun(m Model, msg tea.Msg) (Model, tea.Cmd) {
	next, cmd := m.Update(msg)
	return next.(Model), cmd
}

func press(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	}
	return tea.KeyPressMsg{Code: []rune(s)[0], Text: s}
}

func pressAll(t *testing.T, m Model, ks ...string) Model {
	t.Helper()
	for _, k := range ks {
		var cmd tea.Cmd
		m, cmd = updateRun(m, press(k))
		m = pump(t, m, cmd)
	}
	return m
}

func typeText(t *testing.T, m Model, s string) Model {
	t.Helper()
	for _, r := range s {
		m = update(t, m, press(string(r)))
	}
	return m
}

// pump runs cmd and feeds its messages back, expanding batches.
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
		default:
			var next tea.Cmd
			m, next = updateRun(m, msg)
			queue = append(queue, next)
		}
	}
	return m
}

func view(m Model) string { return tuitest.Plain(m.View()) }

func TestActionScopes(t *testing.T) {
	want := map[string]reslist.Scope{"enter": reslist.Cursor, "!": reslist.All, "c": reslist.Selected}
	got := map[string]reslist.Scope{}
	for _, d := range New(testEnv(t)).Declared() {
		got[d.Key.Keys()[0]] = d.Scope
	}
	if len(got) != len(want) {
		t.Fatalf("declared keys %v, want exactly %v", got, want)
	}
	for k, scope := range want {
		if got[k] != scope {
			t.Errorf("key %s scope = %v, want %v", k, got[k], scope)
		}
	}
}

// inputLine is the prompt's input line, trimmed.
func inputLine(v string) string {
	for _, l := range strings.Split(v, "\n") {
		if strings.HasPrefix(l, "› ") {
			return strings.TrimSpace(l)
		}
	}
	return ""
}

// promptTitle is the first line of the open prompt.
func promptTitle(m Model) string { return strings.SplitN(view(m), "\n", 2)[0] }

func TestBangScopeMarksThenShown(t *testing.T) {
	e := testEnv(t)
	m := newModel(t, e,
		st(e, "a", nil), st(e, "b", func(s *repo.RepoStatus) { s.Dirty = true }), st(e, "c", nil))

	all := pressAll(t, m, "!")
	if got := promptTitle(all); got != "Run in 3 repos" || inputLine(view(all)) != "› git" {
		t.Fatalf("! without marks takes every shown repo, pre-filled with git:\n%s", view(all))
	}

	filtered := update(t, m, nav.FilterMsg{Screen: nav.ScreenRepos, Filter: "state=dirty"})
	if got := promptTitle(pressAll(t, filtered, "!")); got != "Run in b" {
		t.Errorf("! under a filter: %q", got)
	}

	marked := pressAll(t, m, "space", "down", "down", "space") // a and c
	if got := promptTitle(pressAll(t, marked, "!")); got != "Run in 2 repos" {
		t.Errorf("! with 2 marks: %q", got)
	}
}

func TestDetailBangActsOnThatRepo(t *testing.T) {
	e := testEnv(t)
	m := newModel(t, e, st(e, "a", nil), st(e, "b", nil))
	m = pressAll(t, m, "down", "enter")
	if m.level != levelDetail {
		t.Fatal("enter opens the detail")
	}
	if got := promptTitle(pressAll(t, m, "!")); got != "Run in b" {
		t.Errorf("! in the detail: %q", got)
	}
}

func TestRefreshRereads(t *testing.T) {
	e := testEnv(t)
	m := newModel(t, e, st(e, "a", nil))
	m, cmd := updateRun(m, nav.RefreshMsg{})
	if cmd == nil || m.activity != activityScanning {
		t.Errorf("ctrl+r: activity %v; want a re-read", m.activity)
	}
}

func TestCommands(t *testing.T) {
	var names []string
	for _, c := range New(testEnv(t)).Commands() {
		names = append(names, c.Name)
	}
	if strings.Join(names, ",") != "git,run,configure,root add,root ls,root rm" {
		t.Errorf("commands = %v", names)
	}
}

func TestOneRepoGetsTheTerminal(t *testing.T) {
	e := testEnv(t)
	m := newModel(t, e, st(e, "a", nil), st(e, "b", nil))
	m = pressAll(t, m, "down", "space") // mark b
	m, cmd := updateRun(m, nav.CommandMsg{Words: []string{"git", "log", "-p"}})
	if cmd == nil || m.modal.Open() {
		t.Fatal("one repo hands the terminal over: no checklist")
	}
	if got := m.history.Recent(); len(got) != 1 || got[0] != "git log -p" {
		t.Errorf("history = %v", got)
	}
	// When the program exits, the list is read again.
	m, cmd = updateRun(m, handover.DoneMsg{Tag: handoverRun{line: "git log -p", path: filepath.Join(e.Paths.Workspace, "b")}})
	if cmd == nil || m.activity != activityScanning {
		t.Error("the screen should re-read after the hand-over")
	}
}

func TestManyReposGoThroughTheChecklistAndJoinTheHistory(t *testing.T) {
	e := testEnv(t)
	e.HistoryFile = filepath.Join(e.Paths.Workspace, "state", "repo-history.json")
	m := newModel(t, e, st(e, "a", nil), st(e, "b", nil))
	m, cmd := updateRun(m, nav.CommandMsg{Words: []string{"git", "status", "-s"}})
	if got := view(m); !strings.Contains(got, "Run `git status -s`") || !strings.Contains(got, "Run in a") || !strings.Contains(got, "Run in b") {
		t.Fatalf("checklist:\n%s", got)
	}
	pump(t, m, cmd) // writes the history
	if h := repo.LoadHistory(e.HistoryFile); len(h.Entries) != 1 || h.Entries[0].Line != "git status -s" {
		t.Errorf("saved history = %+v", h)
	}
	if New(e).history.Recent()[0] != "git status -s" {
		t.Error("a new screen loads the history")
	}
}

func TestPromptStepsThroughHistory(t *testing.T) {
	e := testEnv(t)
	m := newModel(t, e, st(e, "a", nil), st(e, "b", nil))
	m.history = m.history.Add("git pull --rebase", e.Now()).Add("git push", e.Now().Add(time.Minute))
	m = pressAll(t, m, "!")
	m = update(t, m, tea.KeyPressMsg{Code: tea.KeyUp})
	if !strings.Contains(view(m), "› git push") {
		t.Fatalf("↑ shows the most recent line:\n%s", view(m))
	}
	m = update(t, m, tea.KeyPressMsg{Code: tea.KeyUp})
	if !strings.Contains(view(m), "› git pull --rebase") {
		t.Fatalf("↑ again shows the one before:\n%s", view(m))
	}
	m = update(t, m, tea.KeyPressMsg{Code: tea.KeyDown}, tea.KeyPressMsg{Code: tea.KeyDown})
	if input := inputLine(view(m)); input != "› git" {
		t.Fatalf("↓ past the newest returns to the typed line: %q", input)
	}
}

func TestLineCompleter(t *testing.T) {
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "--allow-empty", "-m", "x"},
		{"branch", "feature"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Skipf("git: %v %s", err, out)
		}
	}
	t0 := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	h := repo.History{}.Add("git push", t0).Add("git pull --rebase", t0).Add("git pull --rebase", t0)
	complete := lineCompleter(h, []string{dir})

	if got := strings.Join(complete("git pu"), "|"); got != "git pull --rebase|git push|git pull" {
		t.Errorf("history first, the most run first, then subcommands: %q", got)
	}
	if got := strings.Join(complete("git checkout fe"), "|"); got != "git checkout feature" {
		t.Errorf("branch names after the subcommand: %q", got)
	}
	if got := complete("git log --on"); len(got) != 0 {
		t.Errorf("flags are git's: %q", got)
	}
	if got := complete("make te"); len(got) != 0 {
		t.Errorf("not git, no history: %q", got)
	}
}

func TestRunShowsOutputPerRepo(t *testing.T) {
	e := testEnv(t)
	for _, d := range []string{"a", "b"} {
		if err := os.MkdirAll(filepath.Join(e.Paths.Workspace, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	m := newModel(t, e, st(e, "a", nil), st(e, "b", nil))
	m, cmd := updateRun(m, nav.CommandMsg{Words: []string{"run"}}) // bare: the prompt, empty
	m = pump(t, m, cmd)
	if got := promptTitle(m); got != "Run in 2 repos" {
		t.Fatalf("prompt:\n%s", view(m))
	}
	m = typeText(t, m, `echo "in $(basename "$PWD")"; [ "$(basename "$PWD")" = a ]`)
	m = pressAll(t, m, "enter")
	if got := view(m); !strings.Contains(got, "Run in a") || strings.Contains(got, "[ ]") {
		t.Fatalf("run checklist should be pre-checked:\n%s", got)
	}
	m, cmd = updateRun(m, press("enter")) // apply
	m = pump(t, m, cmd)
	m, cmd = updateRun(m, press("enter")) // close
	m = pump(t, m, cmd)
	if m.level != levelOutput {
		t.Fatalf("expected the output view, level = %v\n%s", m.level, view(m))
	}
	tuitest.Golden(t, "repos_output", view(m))
	m = pressAll(t, m, "esc")
	if m.level != levelList {
		t.Error("esc should return to the list")
	}
}

func TestFilterMsgSetsFilter(t *testing.T) {
	e := testEnv(t)
	m := newModel(t, e, st(e, "a", nil), st(e, "b", func(s *repo.RepoStatus) { s.Error = "boom" }))
	m = update(t, m, nav.FilterMsg{Screen: nav.ScreenRepos, Filter: "state=error"})
	if m.list.Filter() != "state=error" || len(m.list.Shown()) != 1 {
		t.Errorf("filter %q shows %d rows, want state=error showing 1", m.list.Filter(), len(m.list.Shown()))
	}
}

// withRoots writes a ws.json with the roots and returns the screen env.
func withRoots(t *testing.T, e env.Env, roots ...string) env.Env {
	t.Helper()
	cfg := wsconfig.Default()
	cfg.Repo.Roots = roots
	if err := wsconfig.Save(e.Paths.Config, cfg); err != nil {
		t.Fatal(err)
	}
	for _, r := range roots {
		if err := os.MkdirAll(filepath.Join(e.Paths.Workspace, r), 0o755); err != nil {
			t.Fatal(err)
		}
		e.RepoRoots = append(e.RepoRoots, filepath.Join(e.Paths.Workspace, r))
	}
	return e
}

func rootsInConfig(t *testing.T, e env.Env) []string {
	t.Helper()
	cfg, err := wsconfig.Load(e.Paths.Config)
	if err != nil {
		t.Fatal(err)
	}
	return cfg.Repo.Roots
}

func TestRootRmOneRootThroughTheChecklist(t *testing.T) {
	e := withRoots(t, testEnv(t), "a", "b")
	m := newModel(t, e)
	m, _ = updateRun(m, nav.CommandMsg{Words: []string{"root", "rm", filepath.Join(e.Paths.Workspace, "a")}})
	got := view(m)
	if !strings.Contains(got, "Remove 1 repo root") || !strings.Contains(got, "Remove repo root ") { // the path itself is cut to the view width
		t.Fatalf("checklist:\n%s", got)
	}
	if strings.Contains(got, "[ ]") {
		t.Errorf("a named root should start ticked:\n%s", got)
	}
	m, cmd := updateRun(m, press("enter")) // apply
	m = pump(t, m, cmd)
	m, cmd = updateRun(m, press("enter")) // close
	m = pump(t, m, cmd)
	if got := rootsInConfig(t, e); len(got) != 1 || got[0] != "b" {
		t.Errorf("roots after rm = %v, want [b]", got)
	}
	if len(m.roots) != 1 || filepath.Base(m.roots[0]) != "b" {
		t.Errorf("the screen did not re-read its roots: %v", m.roots)
	}
}

func TestBareRootRmListsEveryRootUnticked(t *testing.T) {
	e := withRoots(t, testEnv(t), "a", "b")
	m := newModel(t, e)
	m, _ = updateRun(m, nav.CommandMsg{Words: []string{"root", "rm"}})
	got := view(m)
	if !strings.Contains(got, "Remove 2 repo roots") || strings.Count(got, "[ ]") != 2 || strings.Contains(got, "[x]") {
		t.Errorf("bare :root rm should list both roots, none ticked:\n%s", got)
	}
}

func TestRootRmRefusesTheLastRoot(t *testing.T) {
	e := withRoots(t, testEnv(t), "a")
	m := newModel(t, e)
	m, cmd := updateRun(m, nav.CommandMsg{Words: []string{"root", "rm", filepath.Join(e.Paths.Workspace, "a")}})
	if m.modal.Open() {
		t.Fatal("the last root must not get a checklist")
	}
	n, ok := cmd().(nav.NoticeMsg)
	if !ok || !strings.Contains(n.Text, "last repo root") {
		t.Errorf("notice = %#v", n)
	}
}

func TestRootLsIsAReadOnlyView(t *testing.T) {
	e := withRoots(t, testEnv(t), "a", "b")
	m := newModel(t, e)
	m, _ = updateRun(m, nav.CommandMsg{Words: []string{"root", "ls"}})
	got := view(m)
	for _, r := range []string{"a", "b"} {
		if !strings.Contains(got, filepath.Join(e.Paths.Workspace, r)) {
			t.Errorf("root %s not listed:\n%s", r, got)
		}
	}
	if m = pressAll(t, m, "esc"); m.level != levelList {
		t.Error("esc should return to the list")
	}
}

func TestAddRootPromptShowsRootsAndAddsThroughChecklist(t *testing.T) {
	e := withRoots(t, testEnv(t), "a")
	if err := os.MkdirAll(filepath.Join(e.Paths.Workspace, "new"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := newModel(t, e)
	m, cmd := updateRun(m, nav.CommandMsg{Words: []string{"root", "add"}}) // bare: the prompt
	m = pump(t, m, cmd)
	if got := view(m); !strings.Contains(got, "Current roots: a") {
		t.Fatalf("prompt hint:\n%s", got)
	}
	m = typeText(t, m, "new") // the prompt starts as <workspace>/
	m = pressAll(t, m, "enter")
	if got := view(m); !strings.Contains(got, "Add repo root") {
		t.Fatalf("one-action checklist missing:\n%s", got)
	}
	m, cmd = updateRun(m, press("enter"))
	m = pump(t, m, cmd)
	_, cmd = updateRun(m, press("enter"))
	pump(t, m, cmd)
	if got := rootsInConfig(t, e); len(got) != 2 || got[1] != "new" {
		t.Errorf("roots after add = %v", got)
	}
}
