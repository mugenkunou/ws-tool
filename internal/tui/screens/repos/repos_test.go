package repos

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/mugenkunou/ws-tool/internal/repo"
	"github.com/mugenkunou/ws-tool/internal/tui/env"
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
	want := map[string]reslist.Scope{
		"enter": reslist.Cursor, "f": reslist.All, "p": reslist.All, "s": reslist.All,
		"r": reslist.All, "c": reslist.Selected, "a": reslist.Global,
	}
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

func TestFetchScopeMarksThenShown(t *testing.T) {
	e := testEnv(t)
	m := newModel(t, e,
		st(e, "a", nil), st(e, "b", func(s *repo.RepoStatus) { s.Dirty = true }), st(e, "c", nil))

	all, cmd := updateRun(m, press("f"))
	if cmd == nil || all.fetching != 3 || all.activity != activityFetching {
		t.Fatalf("f without marks: fetching %d, activity %v; want all 3 shown", all.fetching, all.activity)
	}
	if got := view(all); !strings.Contains(got, "Fetching 3 repos…") {
		t.Errorf("status line: %q", strings.SplitN(got, "\n", 2)[0])
	}

	filtered := update(t, m, nav.FilterMsg{Screen: nav.ScreenRepos, Filter: "state=dirty"})
	one, _ := updateRun(filtered, press("f"))
	if one.fetching != 1 {
		t.Errorf("f under a filter fetched %d, want the 1 shown", one.fetching)
	}

	marked := pressAll(t, m, "space", "down", "down", "space") // a and c
	two, _ := updateRun(marked, press("f"))
	if two.fetching != 2 {
		t.Errorf("f with 2 marks fetched %d, want 2", two.fetching)
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

func TestRefreshDoesNotFetch(t *testing.T) {
	e := testEnv(t)
	m := newModel(t, e, st(e, "a", nil))
	m, cmd := updateRun(m, nav.RefreshMsg{})
	if cmd == nil || m.activity != activityScanning || m.fetching != 0 {
		t.Errorf("ctrl+r: activity %v, fetching %d; want a plain re-read", m.activity, m.fetching)
	}
}

func TestCommandsShareKeyFunctions(t *testing.T) {
	e := testEnv(t)
	m := newModel(t, e, st(e, "a", nil), st(e, "b", nil))
	byCmd, cmd := updateRun(m, nav.CommandMsg{Words: []string{"ls", "--fetch"}, Line: "ls --fetch"})
	byKey, _ := updateRun(m, press("f"))
	if cmd == nil || byCmd.fetching != byKey.fetching || byCmd.activity != byKey.activity {
		t.Errorf(":ls --fetch (%d, %v) differs from f (%d, %v)", byCmd.fetching, byCmd.activity, byKey.fetching, byKey.activity)
	}

	var names []string
	for _, c := range m.Commands() {
		names = append(names, c.Name)
	}
	if strings.Join(names, ",") != "push,root ls,root rm,ls --fetch" {
		t.Errorf("commands = %v", names)
	}
}

// diverged is a repo with 2 local and 1 remote commit.
func diverged(e env.Env, name string) Entry {
	return st(e, name, func(s *repo.RepoStatus) { s.Ahead, s.Behind = 2, 1 })
}

func TestPullAsksEveryDivergedRepoThenOpensChecklist(t *testing.T) {
	e := testEnv(t)
	a, b := diverged(e, "a"), diverged(e, "b")
	ff := st(e, "c", func(s *repo.RepoStatus) { s.Behind = 3 })
	m := newModel(t, e, a, b, ff)

	m, cmd := updateRun(m, press("p"))
	if cmd == nil || m.activity != activityFetching {
		t.Fatalf("p did not start fetching")
	}
	m = update(t, m, PreparedMsg{gen: m.gen, Kind: KindPull, Statuses: []repo.RepoStatus{a.Status, b.Status, ff.Status}})

	got := view(m)
	if !strings.Contains(got, "a has diverged (2 local, 1 remote commit)") {
		t.Fatalf("first question missing:\n%s", got)
	}
	for _, opt := range []string{"[r] rebase", "[m] merge", "[s] skip this repo"} {
		if !strings.Contains(got, opt) {
			t.Errorf("chooser lacks %q:\n%s", opt, got)
		}
	}

	// A letter chooses, and nothing is picked for the user: enter on the
	// first option is the only other way.
	m = pressAll(t, m, "r")
	if !strings.Contains(view(m), "b has diverged") {
		t.Fatalf("second question missing:\n%s", view(m))
	}
	m = pressAll(t, m, "s")
	got = view(m)
	if !strings.Contains(got, "Pull 2 repos") {
		t.Fatalf("checklist should hold the rebase and the fast-forward:\n%s", got)
	}
	if !strings.Contains(got, "Pull a (diverged, rebase)") || !strings.Contains(got, "Pull c (3 behind, fast-forward)") {
		t.Errorf("plan lines:\n%s", got)
	}
	if strings.Contains(got, "Pull b") || !strings.Contains(got, "Skipped: b (skipped: you chose to skip it)") {
		t.Errorf("b should be skipped, not planned:\n%s", got)
	}
}

func TestEscInChooserCancelsEverything(t *testing.T) {
	e := testEnv(t)
	a, b := diverged(e, "a"), diverged(e, "b")
	m := newModel(t, e, a, b)
	m, _ = updateRun(m, press("p"))
	m = update(t, m, PreparedMsg{gen: m.gen, Kind: KindPull, Statuses: []repo.RepoStatus{a.Status, b.Status}})
	m = pressAll(t, m, "r") // a answered
	m, cmd := updateRun(m, press("esc"))
	m = pump(t, m, cmd)
	if m.modal.Open() || m.flow != nil {
		t.Fatalf("esc left a dialog open or the flow alive:\n%s", view(m))
	}
	if strings.Contains(view(m), "Pull") {
		t.Errorf("esc must not open the checklist:\n%s", view(m))
	}
}

func TestSaveAsksBeforeTheChecklistAndCountsThePendingCommit(t *testing.T) {
	e := testEnv(t)
	dirtyBehind := st(e, "a", func(s *repo.RepoStatus) { s.Dirty, s.Behind = true, 1 })
	m := newModel(t, e, dirtyBehind)
	m, _ = updateRun(m, press("s"))
	m = update(t, m, PreparedMsg{gen: m.gen, Kind: KindSave, Statuses: []repo.RepoStatus{dirtyBehind.Status},
		Changes: map[string]int{dirtyBehind.Status.Path: 2}})
	if got := view(m); !strings.Contains(got, "a has diverged (1 local, 1 remote commit)") {
		t.Fatalf("committing makes it diverged:\n%s", got)
	}
	m = pressAll(t, m, "m")
	got := view(m)
	if !strings.Contains(got, "Save 1 repo") || !strings.Contains(got, "commit 2 changes, pull (merge), push") {
		t.Errorf("save checklist:\n%s", got)
	}
}

func TestFailedFetchSkipsAndShowsInDetail(t *testing.T) {
	e := testEnv(t)
	a := st(e, "a", func(s *repo.RepoStatus) { s.Behind = 2 })
	m := newModel(t, e, a)
	m, _ = updateRun(m, press("p"))
	m = update(t, m, PreparedMsg{gen: m.gen, Kind: KindPull, Statuses: []repo.RepoStatus{a.Status},
		Fetched: []string{a.Status.Path}, Failed: map[string]string{a.Status.Path: "could not resolve host"}})
	if m.modal.Open() {
		t.Fatalf("nothing to pull when the fetch failed:\n%s", view(m))
	}
	// The re-read after the fetch keeps the failure for the session.
	m = update(t, m, LoadedMsg{gen: m.gen, Entries: []Entry{a}})
	if got := view(m); !strings.Contains(got, "1 fetch failed") {
		t.Errorf("summary lacks the fetch failure:\n%s", got)
	}
	m = pressAll(t, m, "enter")
	if got := view(m); !strings.Contains(got, "Last fetch") || !strings.Contains(got, "failed: could not resolve host") {
		t.Errorf("detail lacks the last fetch failure:\n%s", got)
	}
	// A later good fetch clears it.
	m = update(t, m, LoadedMsg{gen: m.gen, Entries: []Entry{a}, Fetched: []string{a.Status.Path}})
	if _, bad := m.fetchFailed[a.Status.Path]; bad {
		t.Error("a successful fetch should clear the failure")
	}
}

func TestPushCommand(t *testing.T) {
	e := testEnv(t)
	ahead := st(e, "a", func(s *repo.RepoStatus) { s.Ahead = 2 })
	behind := st(e, "b", func(s *repo.RepoStatus) { s.Behind = 1 })
	m := newModel(t, e, ahead, behind)
	m, cmd := updateRun(m, nav.CommandMsg{Words: []string{"push"}, Line: "push"})
	if cmd == nil || m.activity != activityScanning {
		t.Fatalf(":push should read the repos first")
	}
	m = update(t, m, PreparedMsg{gen: m.gen, Kind: KindPush, Statuses: []repo.RepoStatus{ahead.Status, behind.Status}})
	got := view(m)
	if !strings.Contains(got, "Push 1 repo") || !strings.Contains(got, "Push a (2 ahead)") {
		t.Errorf("push checklist:\n%s", got)
	}
	if !strings.Contains(got, "Skipped: b (behind or diverged: pull first") {
		t.Errorf("behind repo should be skipped with its reason:\n%s", got)
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
	m = pressAll(t, m, "a")
	if got := view(m); !strings.Contains(got, "Current roots: a") {
		t.Fatalf("prompt hint:\n%s", got)
	}
	m = typeText(t, m, "new") // the prompt starts as <workspace>/
	m = pressAll(t, m, "enter")
	if got := view(m); !strings.Contains(got, "Add repo root") {
		t.Fatalf("one-action checklist missing:\n%s", got)
	}
	m, cmd := updateRun(m, press("enter"))
	m = pump(t, m, cmd)
	_, cmd = updateRun(m, press("enter"))
	pump(t, m, cmd)
	if got := rootsInConfig(t, e); len(got) != 2 || got[1] != "new" {
		t.Errorf("roots after add = %v", got)
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
	m = pressAll(t, m, "r")
	if !strings.Contains(view(m), "Run a command in 2 repos") {
		t.Fatalf("prompt:\n%s", view(m))
	}
	m = typeText(t, m, `echo "in $(basename "$PWD")"; [ "$(basename "$PWD")" = a ]`)
	m = pressAll(t, m, "enter")
	if got := view(m); !strings.Contains(got, "Run in a") || strings.Contains(got, "[ ]") {
		t.Fatalf("run checklist should be pre-checked:\n%s", got)
	}
	m, cmd := updateRun(m, press("enter")) // apply
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
