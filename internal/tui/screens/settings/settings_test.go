package settings

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/mugenkunou/ws-tool/internal/shellcomp"
	"github.com/mugenkunou/ws-tool/internal/tui/env"
	"github.com/mugenkunou/ws-tool/internal/tui/handover"
	"github.com/mugenkunou/ws-tool/internal/tui/nav"
	"github.com/mugenkunou/ws-tool/internal/versions"
	"github.com/mugenkunou/ws-tool/internal/workspace"
	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

// report is a version lookup answered from memory.
func report(ws, wsLatest string, wsState versions.State, at, atLatest string, atState versions.State) versions.Report {
	return versions.Report{
		WS:        versions.Item{Name: "ws", Installed: ws, Latest: wsLatest, State: wsState},
		AgentTmux: versions.Item{Name: "agent-tmux", Installed: at, Latest: atLatest, State: atState},
		CheckedAt: time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC),
	}
}

// fakeVersions replaces the screen's seams: the cache says r, a forced check
// says forced, and the binary is a release download. It returns how many
// times the network was asked.
func fakeVersions(t *testing.T, r, forced versions.Report) *int {
	t.Helper()
	oldC, oldR, oldE := cachedVersions, refreshVersions, executable
	calls := new(int)
	cachedVersions = func(context.Context, string) versions.Report { return r }
	refreshVersions = func(context.Context, string) versions.Report { *calls++; return forced }
	executable = func() (string, error) { return filepath.Join(t.TempDir(), "ws"), nil }
	t.Cleanup(func() { cachedVersions, refreshVersions, executable = oldC, oldR, oldE })
	return calls
}

// hermetic points HOME and the shell at a temp dir, so the screen reads and
// writes rc files there and never the real ones, and returns a workspace
// holding a ws.json.
func hermetic(t *testing.T, shell string) (home string, e env.Env) {
	t.Helper()
	home = t.TempDir()
	fakeVersions(t, report("v1.2.3", "v1.2.3", versions.StateCurrent, "v0.5.0", "v0.5.0", versions.StateCurrent), versions.Report{})
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("SHELL", shell)
	wsPath := filepath.Join(home, "Workspace")
	if err := os.MkdirAll(wsPath, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := wsconfig.ConfigPath(wsPath)
	if err := wsconfig.Save(cfgPath, wsconfig.Default()); err != nil {
		t.Fatal(err)
	}
	return home, env.Env{
		Paths:      workspace.Paths{Workspace: wsPath, Config: cfgPath},
		Config:     wsconfig.Default(),
		ScratchDir: filepath.Join(home, "Scratch"),
		Version:    "v1.2.3",
	}
}

// load runs the screen's loader and feeds the result back.
func load(t *testing.T, m Model) Model {
	t.Helper()
	msg := Load(m.env)()
	c, _ := m.Update(nav.ResizeMsg{Width: 120, Height: 40})
	c, _ = c.Update(msg)
	return c.(Model)
}

func view(m Model) string { return ansi.Strip(m.View()) }

func send(m Model, msgs ...tea.Msg) (Model, tea.Cmd) {
	var cmd tea.Cmd
	for _, msg := range msgs {
		var c nav.Component
		c, cmd = m.Update(msg)
		m = c.(Model)
	}
	return m, cmd
}

func enter() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeyEnter} }

// menu drives the `.` menu: it opens, letter picks the command, and the
// chooser's result is fed back so the command runs. It returns what the
// command's Do returned.
func menu(m Model, letter string) (Model, tea.Cmd) {
	m, _ = send(m, nav.MenuMsg{})
	m, cmd := send(m, tea.KeyPressMsg{Code: []rune(letter)[0], Text: letter})
	if cmd == nil {
		return m, nil
	}
	return send(m, cmd())
}

// settle runs cmd and feeds every message it yields back into m, to the end.
func settle(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	queue := []tea.Cmd{cmd}
	for steps := 0; len(queue) > 0; steps++ {
		if steps > 100 {
			t.Fatal("settle: too many steps")
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
			m, next = send(m, msg)
			queue = append(queue, next)
		}
	}
	return m
}

func TestRowsAreGroupedNotADump(t *testing.T) {
	_, e := hermetic(t, "/bin/zsh")
	m := load(t, New(e))
	got := view(m)
	for _, want := range []string{
		"Configuration", "ws.json", "edit ws.json", "view ws.json",
		"Workspace", "root", "layout", "12 folders", "homes",
		"Completion", "completion bash", "completion zsh", "completion fish", "your shell",
		"Versions", "v1.2.3", "agent-tmux", "v0.5.0", "check for updates now",
		"About", "build", "linux", "schema 1",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("view lacks %q:\n%s", want, got)
		}
	}
	// One compact layout row: the per-folder rows are gone.
	for _, gone := range []string{"layout notes", "layout scripts/lib", "home scratch", "WS_SCRATCH"} {
		if strings.Contains(got, gone) {
			t.Errorf("view still lists %q:\n%s", gone, got)
		}
	}
	// Each group's name is on its first row only.
	if n := strings.Count(got, "Versions"); n != 1 {
		t.Errorf("group label repeated %d times:\n%s", n, got)
	}
}

// run executes cmd and returns every message it yields, expanding batches
// (tea.Batch of one command is that command itself).
func run(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		var out []tea.Msg
		for _, c := range msg {
			out = append(out, run(c)...)
		}
		return out
	default:
		return []tea.Msg{msg}
	}
}

func rowByName(m LoadedMsg, name string) Row {
	for _, r := range m.rows() {
		if r.Name == name {
			return r
		}
	}
	return Row{}
}

func TestVersionRowsByState(t *testing.T) {
	_, e := hermetic(t, "/bin/bash")
	load := func(r versions.Report) LoadedMsg {
		fakeVersions(t, r, versions.Report{})
		return Load(e)().(LoadedMsg)
	}

	out := load(report("v1.2.3", "v1.3.0", versions.StateOutdated, "v0.5.0", "v0.6.0", versions.StateOutdated))
	ws := rowByName(out, "ws")
	if ws.State != stateOutdated || !strings.Contains(ws.Note, "update available: v1.3.0") || !strings.Contains(ws.Note, warnMark) {
		t.Fatalf("outdated ws: %+v", ws)
	}
	var hint []string
	for _, r := range out.rows() {
		if r.Kind == kindHint {
			hint = append(hint, r.Note)
		}
	}
	joined := strings.Join(hint, "\n")
	if !strings.Contains(joined, "releases") || !strings.Contains(joined, "agent-tmux") && !strings.Contains(joined, "install") {
		t.Fatalf("outdated items say how to upgrade: %q", joined)
	}
	_, card, _ := out.Card()
	if len(card.Lines) < 3 || !strings.Contains(card.Lines[2], "updates available: ws, agent-tmux") {
		t.Fatalf("card lines: %q", card.Lines)
	}

	// A dev build is never "update available".
	out = load(report("dev", "v1.3.0", versions.StateDev, "v0.5.0", "v0.5.0", versions.StateCurrent))
	ws = rowByName(out, "ws")
	if ws.State != stateDev || strings.Contains(ws.Note, "update") || !strings.Contains(ws.Note, "local build is dev") || !strings.Contains(ws.Note, "upstream is v1.3.0") {
		t.Fatalf("dev ws: %+v", ws)
	}
	for _, r := range out.rows() {
		if r.Kind == kindHint {
			t.Fatalf("a dev build gets no upgrade lines: %+v", r)
		}
	}
	if _, card, _ := out.Card(); len(card.Lines) != 2 || len(card.Issues) != 1 || !strings.Contains(card.Issues[0].Text, "completion") {
		t.Fatalf("dev is not an update on the card: %q %+v", card.Lines, card.Issues)
	}

	// agent-tmux missing and too old both show the install lines.
	for _, st := range []versions.State{versions.StateNotInstalled, versions.StateTooOld} {
		out = load(report("v1.2.3", "v1.2.3", versions.StateCurrent, "", "", st))
		n := 0
		for _, r := range out.rows() {
			if r.Kind == kindHint {
				n++
			}
		}
		if n == 0 {
			t.Fatalf("%s: no install lines", st)
		}
	}

	// Nothing cached yet: unknown, not an error.
	out = load(versions.Report{WS: versions.Item{Name: "ws", State: versions.StateUnknown}, AgentTmux: versions.Item{Name: "agent-tmux", State: versions.StateUnknown}})
	if r := rowByName(out, "ws"); r.State != stateUnknown || r.Value != "v1.2.3" || r.Note != "not checked yet" {
		t.Fatalf("unknown: %+v", r)
	}
}

func TestCheckForUpdatesRefreshesOffTheUIThread(t *testing.T) {
	_, e := hermetic(t, "/bin/bash")
	forced := report("v1.2.3", "v1.4.0", versions.StateOutdated, "v0.5.0", "v0.5.0", versions.StateCurrent)
	forced.CheckedAt = forced.CheckedAt.Add(time.Hour)
	calls := fakeVersions(t, report("v1.2.3", "v1.2.3", versions.StateCurrent, "v0.5.0", "v0.5.0", versions.StateCurrent), forced)
	m := load(t, New(e))
	if *calls != 0 {
		t.Fatal("loading the screen never asks the network")
	}

	// Walk to the check row: ws.json, edit, view, root, layout, homes, 3 shells, ws, agent-tmux, check.
	m = typed(m, "down", "down", "down", "down", "down", "down", "down", "down", "down", "down", "down")
	m, cmd := send(m, enter())
	if cmd == nil || *calls != 0 {
		t.Fatalf("enter returns a command and does not block (calls=%d)", *calls)
	}
	if !strings.Contains(view(m), "checking…") {
		t.Fatalf("the row says it is checking:\n%s", view(m))
	}

	got := run(cmd)
	if *calls != 1 {
		t.Fatalf("one refresh, got %d", *calls)
	}
	var vm nav.VersionsMsg
	var notices []nav.NoticeMsg
	for _, g := range got {
		switch g := g.(type) {
		case nav.VersionsMsg:
			vm = g
		case nav.NoticeMsg:
			notices = append(notices, g)
		}
	}
	if vm.Report.WS.Latest != "v1.4.0" || len(notices) != 1 || notices[0].Level != nav.LevelInfo {
		t.Fatalf("msgs: %#v", got)
	}

	// The broadcast lands: the row and the card update, and the user is told.
	m, cmd = send(m, vm)
	v := view(m)
	if strings.Contains(v, "checking") || !strings.Contains(v, "update available: v1.4.0") {
		t.Fatalf("after the check:\n%s", v)
	}
	var loaded LoadedMsg
	var told string
	for _, msg := range run(cmd) {
		switch r := msg.(type) {
		case LoadedMsg:
			loaded = r
		case nav.NoticeMsg:
			told = r.Text
			if r.Level != nav.LevelWarn {
				t.Fatalf("an update is a warning: %v", r)
			}
		}
	}
	if !strings.Contains(told, "ws v1.4.0 is available") {
		t.Fatalf("notice: %q", told)
	}
	if _, card, _ := loaded.Card(); !strings.Contains(strings.Join(card.Lines, "\n"), "updates available: ws") {
		t.Fatalf("the Dashboard card learns of it: %q", card.Lines)
	}

	// A reload from the older cache does not bring the old report back.
	m = load(t, m)
	if !strings.Contains(view(m), "update available: v1.4.0") {
		t.Fatalf("a stale cache must not overwrite a newer report:\n%s", view(m))
	}
}

func TestVersionsBroadcastWithoutAnAskIsSilent(t *testing.T) {
	_, e := hermetic(t, "/bin/bash")
	m := load(t, New(e))
	m, cmd := send(m, nav.VersionsMsg{Report: report("v1.2.3", "v1.9.0", versions.StateOutdated, "v0.5.0", "v0.5.0", versions.StateCurrent)})
	if !strings.Contains(view(m), "update available: v1.9.0") {
		t.Fatalf("the background lookup lands on the screen:\n%s", view(m))
	}
	for _, msg := range run(cmd) {
		if _, ok := msg.(nav.NoticeMsg); ok {
			t.Fatal("no toast for a lookup the user did not ask for")
		}
	}
}

func TestViewAndEditRows(t *testing.T) {
	_, e := hermetic(t, "/bin/bash")
	m := load(t, New(e))
	m = typed(m, "down") // edit ws.json
	if _, cmd := send(m, enter()); cmd == nil {
		t.Fatal("the edit row hands over config edit")
	}
	m = typed(m, "down") // view ws.json
	if _, cmd := send(m, enter()); cmd == nil {
		t.Fatal("the view row hands over config show")
	}
}

func TestBrokenConfigIsARowNotACrash(t *testing.T) {
	_, e := hermetic(t, "/bin/bash")
	if err := os.WriteFile(e.Paths.Config, []byte("{ not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	msg := Load(e)().(LoadedMsg)
	if msg.ConfigErr == nil || msg.Err != nil {
		t.Fatalf("a ws.json that does not parse is ConfigErr, not Err: %+v", msg)
	}
	var cfg Row
	for _, r := range msg.rows() {
		if r.Kind == kindConfig {
			cfg = r
		}
	}
	if cfg.State != stateInvalid || cfg.Note == "" {
		t.Fatalf("ws.json row: %+v", cfg)
	}
	_, card, _ := msg.Card()
	if len(card.Issues) == 0 || card.Issues[0].Sev != nav.SevError {
		t.Fatalf("a broken ws.json is an error on the card: %+v", card.Issues)
	}

	if err := os.Remove(e.Paths.Config); err != nil {
		t.Fatal(err)
	}
	msg = Load(e)().(LoadedMsg)
	if !errors.Is(msg.ConfigErr, os.ErrNotExist) {
		t.Fatalf("missing ws.json: %v", msg.ConfigErr)
	}
}

func TestCardSaysVersionConfigAndCompletion(t *testing.T) {
	home, e := hermetic(t, "/bin/zsh")
	msg := Load(e)().(LoadedMsg)
	_, card, ok := msg.Card()
	if !ok || !strings.Contains(card.Lines[0], "ws v1.2.3") || !strings.Contains(card.Lines[0], "ws.json ok") {
		t.Fatalf("card lines: %q", card.Lines)
	}
	if len(card.Issues) != 1 || !card.Issues[0].Advice {
		t.Fatalf("completion off in the shell in use is advice: %+v", card.Issues)
	}
	if sel, ok := card.Issues[0].Focus.(SelectMsg); !ok || sel.Name != "completion zsh" {
		t.Fatalf("the advice focuses the zsh completion row: %#v", card.Issues[0].Focus)
	}

	// Enabled in the shell in use: no advice, and the card names the shell.
	if err := os.WriteFile(filepath.Join(home, ".zshrc"), []byte(shellcomp.Block(`eval "$(ws completion zsh)"`)), 0o644); err != nil {
		t.Fatal(err)
	}
	_, card, _ = Load(e)().(LoadedMsg).Card()
	if len(card.Issues) != 0 || !strings.Contains(card.Lines[1], "on") || !strings.Contains(card.Lines[1], "zsh") {
		t.Fatalf("card: %q %+v", card.Lines, card.Issues)
	}
}

func TestCompletionEnableDefaultsToTheShellInUse(t *testing.T) {
	home, e := hermetic(t, "/usr/bin/zsh")
	m := load(t, New(e))
	// The cursor is on the ws.json row: not a completion row, so $SHELL.
	m, _ = menu(m, "n")
	if !m.CapturesKeys() {
		t.Fatal("the plan opens a checklist")
	}
	got := view(m)
	rc := filepath.Join(home, ".zshrc")
	if !strings.Contains(got, "Load ws completion from "+rc) || strings.Contains(got, ".bashrc") {
		t.Fatalf("checklist:\n%s", got)
	}
	if _, err := os.Stat(rc); err == nil {
		t.Fatal("the checklist writes nothing before it is applied")
	}

	m, cmd := send(m, enter())
	m = settle(t, m, cmd)
	m, cmd = send(m, enter()) // close
	m = settle(t, m, cmd)
	b, err := os.ReadFile(rc)
	if err != nil || !strings.Contains(string(b), shellcomp.MarkerStart) {
		t.Fatalf("rc file: %q %v", b, err)
	}
	if m.CapturesKeys() || !strings.Contains(view(m), "on") {
		t.Fatalf("the list reloads and shows completion on:\n%s", view(m))
	}
	if _, err := os.Stat(filepath.Join(home, ".bashrc")); err == nil {
		t.Fatal("bash was not asked for")
	}
}

func TestCompletionActsOnMarkedRowsElseTheCursorRow(t *testing.T) {
	home, e := hermetic(t, "/bin/bash")
	m := load(t, New(e))
	rcs := []string{filepath.Join(home, ".bashrc"), filepath.Join(home, ".zshrc")}

	// Rows: ws.json, edit, view, root, layout, homes, bash, zsh, fish, … Mark bash and zsh.
	m = typed(m, "down", "down", "down", "down", "down", "down", "space", "down", "space")
	m, _ = menu(m, "n")
	got := view(m)
	for _, rc := range rcs {
		if !strings.Contains(got, "Load ws completion from "+rc) {
			t.Fatalf("both marked shells are in the plan, missing %s:\n%s", rc, got)
		}
	}
	if strings.Contains(got, "/fish/") {
		t.Fatalf("fish was not marked:\n%s", got)
	}

	// No marks: the completion row under the cursor wins over $SHELL (bash).
	m = load(t, New(e))
	m = typed(m, "down", "down", "down", "down", "down", "down", "down", "down") // fish
	m, _ = menu(m, "n")
	got = view(m)
	if !strings.Contains(got, "/fish/") || strings.Contains(got, ".bashrc") {
		t.Fatalf("the cursor row is the target:\n%s", got)
	}
}

func TestCompletionShellFlagAndErrors(t *testing.T) {
	_, e := hermetic(t, "/bin/bash")
	m := load(t, New(e))
	m, _ = m.completion(true, nil, []string{"--shell", "fish"})
	if got := view(m); !strings.Contains(got, "/fish/") || strings.Contains(got, ".bashrc") {
		t.Fatalf("--shell names the target:\n%s", got)
	}

	for _, args := range [][]string{{"--shell", "powershell"}, {"bash"}} {
		m := load(t, New(e))
		m, cmd := m.completion(true, nil, args)
		if m.CapturesKeys() || cmd == nil {
			t.Fatalf("%v should notify, not open a checklist", args)
		}
		if n, ok := cmd().(nav.NoticeMsg); !ok || n.Level != nav.LevelWarn {
			t.Fatalf("%v: %#v", args, n)
		}
	}

	// A shell ws does not complete cannot be the default.
	t.Setenv("SHELL", "/bin/sh")
	m = load(t, New(e))
	m, cmd := m.completion(true, nil, nil)
	if m.CapturesKeys() || cmd == nil {
		t.Fatal("an unsupported $SHELL notifies")
	}
}

func TestCompletionDisableAndNothingToDo(t *testing.T) {
	home, e := hermetic(t, "/bin/bash")
	m := load(t, New(e))

	m, cmd := menu(m, "d")
	if m.CapturesKeys() {
		t.Fatal("nothing to disable opens no checklist")
	}
	if n := cmd().(nav.NoticeMsg); !strings.Contains(n.Text, "not enabled") {
		t.Fatalf("notice: %q", n.Text)
	}

	rc := filepath.Join(home, ".bashrc")
	if err := os.WriteFile(rc, []byte("export A=1\n"+shellcomp.Block(`eval "$(ws completion bash)"`)), 0o644); err != nil {
		t.Fatal(err)
	}
	m = load(t, m)
	m, _ = menu(m, "d")
	if got := view(m); !strings.Contains(got, "Remove ws completion from "+rc) {
		t.Fatalf("checklist:\n%s", got)
	}
	m, cmd = send(m, enter())
	m = settle(t, m, cmd)
	b, _ := os.ReadFile(rc)
	if strings.Contains(string(b), shellcomp.MarkerStart) || !strings.Contains(string(b), "export A=1") {
		t.Fatalf("rc file after disable: %q", b)
	}

	m, _ = menu(m, "n")
	m, cmd = send(m, tea.KeyPressMsg{Code: tea.KeyEscape}) // cancel
	m = settle(t, m, cmd)
	if m.CapturesKeys() {
		t.Fatal("esc closes the checklist")
	}
	if b, _ := os.ReadFile(rc); strings.Contains(string(b), shellcomp.MarkerStart) {
		t.Fatalf("a cancelled checklist writes nothing: %q", b)
	}
}

func typed(m Model, keys ...string) Model {
	for _, k := range keys {
		var msg tea.KeyPressMsg
		switch k {
		case "down":
			msg = tea.KeyPressMsg{Code: tea.KeyDown}
		case "space":
			msg = tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
		default:
			msg = tea.KeyPressMsg{Code: []rune(k)[0], Text: k}
		}
		m, _ = send(m, msg)
	}
	return m
}

func TestEnterOnAFactRowDoesNothing(t *testing.T) {
	_, e := hermetic(t, "/bin/bash")
	m := load(t, New(e))
	if _, cmd := send(m, enter()); cmd == nil {
		t.Fatal("enter on the ws.json row hands over config edit")
	}
	m = typed(m, "down", "down", "down") // root
	if _, cmd := send(m, enter()); cmd != nil {
		t.Fatal("enter on the root row does nothing")
	}
}

func TestConfigHandoverRefreshesEveryScreen(t *testing.T) {
	_, e := hermetic(t, "/bin/bash")
	m := load(t, New(e))
	m, cmd := send(m, handover.DoneMsg{Tag: configDone{}})
	if _, ok := cmd().(nav.RefreshMsg); !ok {
		t.Fatal("returning from config edit refreshes every screen")
	}
	if _, cmd := send(m, handover.DoneMsg{Tag: configDone{}, Err: errors.New("boom")}); cmd().(nav.NoticeMsg).Level != nav.LevelError {
		t.Fatal("a failed handover is an error notice")
	}
	var names, letters []string
	for _, c := range m.Commands() {
		names = append(names, c.Name)
		letters = append(letters, c.Menu)
	}
	if strings.Join(names, ",") != "config show,config edit,version check,completion enable,completion disable" {
		t.Fatalf("commands: %v", names)
	}
	if strings.Join(letters, ",") != "s,e,u,n,d" {
		t.Fatalf("menu letters: %v", letters)
	}
}

// TestEnterOnACompletionRowOpensItsChecklist: enter on an off row enables it,
// on an on row disables it.
func TestEnterOnACompletionRowOpensItsChecklist(t *testing.T) {
	home, e := hermetic(t, "/bin/bash")
	// zsh is the eighth row.
	m := typed(load(t, New(e)), "down", "down", "down", "down", "down", "down", "down")
	m, _ = send(m, enter())
	if got := view(m); !strings.Contains(got, "Enable shell completion") {
		t.Fatalf("enter on an off completion row opens the enable checklist:\n%s", got)
	}

	rc := filepath.Join(home, ".zshrc")
	if err := os.WriteFile(rc, []byte(shellcomp.Block(`eval "$(ws completion zsh)"`)), 0o644); err != nil {
		t.Fatal(err)
	}
	m = typed(load(t, New(e)), "down", "down", "down", "down", "down", "down", "down")
	m, _ = send(m, enter())
	if got := view(m); !strings.Contains(got, "Disable shell completion") {
		t.Fatalf("enter on an on completion row opens the disable checklist:\n%s", got)
	}
}

// TestSelectMsgMovesTheCursorToTheNamedRow: the Dashboard's advice focuses a
// completion row by name; enter then acts on that row (fish's rc here).
func TestSelectMsgMovesTheCursorToTheNamedRow(t *testing.T) {
	_, e := hermetic(t, "/bin/bash")
	m, _ := send(load(t, New(e)), SelectMsg{Name: "completion fish"})
	m, _ = send(m, enter())
	if got := view(m); !strings.Contains(got, "/fish/") || strings.Contains(got, ".bashrc") {
		t.Fatalf("SelectMsg should move the cursor to the fish row:\n%s", got)
	}
}
