package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/mugenkunou/ws-tool/internal/shellcomp"
	"github.com/mugenkunou/ws-tool/internal/tui/env"
	"github.com/mugenkunou/ws-tool/internal/tui/handover"
	"github.com/mugenkunou/ws-tool/internal/tui/nav"
	"github.com/mugenkunou/ws-tool/internal/workspace"
	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

// hermetic points HOME and the shell at a temp dir, so the screen reads and
// writes rc files there and never the real ones, and returns a workspace
// holding a ws.json.
func hermetic(t *testing.T, shell string) (home string, e env.Env) {
	t.Helper()
	home = t.TempDir()
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

func TestRowsListEverySetting(t *testing.T) {
	_, e := hermetic(t, "/bin/zsh")
	m := load(t, New(e))
	got := view(m)
	for _, want := range []string{
		"version", "v1.2.3", "ws.json", "ok",
		"completion bash", "completion zsh", "completion fish", "your shell",
		"layout notes", "layout scripts/lib", "missing", "home scratch", "WS_SCRATCH",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("view lacks %q:\n%s", want, got)
		}
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
	// The cursor is on the version row: not a completion row, so $SHELL.
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

	// Rows: version, ws.json, bash, zsh, fish. Mark bash and zsh.
	m = typed(m, "down", "down", "space", "down", "space")
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
	m = typed(m, "down", "down", "down", "down") // fish
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

func TestEnterEditsConfigOnlyOnTheConfigRow(t *testing.T) {
	_, e := hermetic(t, "/bin/bash")
	m := load(t, New(e))
	if _, cmd := send(m, enter()); cmd != nil {
		t.Fatal("enter on the version row does nothing")
	}
	m = typed(m, "down")
	if _, cmd := send(m, enter()); cmd == nil {
		t.Fatal("enter on the ws.json row hands over config edit")
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
	if strings.Join(names, ",") != "config show,config edit,completion enable,completion disable" {
		t.Fatalf("commands: %v", names)
	}
	if strings.Join(letters, ",") != "s,e,n,d" {
		t.Fatalf("menu letters: %v", letters)
	}
}

// TestEnterOnACompletionRowOpensItsChecklist: enter on an off row enables it,
// on an on row disables it.
func TestEnterOnACompletionRowOpensItsChecklist(t *testing.T) {
	home, e := hermetic(t, "/bin/bash")
	// Rows: version, ws.json, bash, zsh, fish. zsh is the third completion row.
	m := typed(load(t, New(e)), "down", "down", "down")
	m, _ = send(m, enter())
	if got := view(m); !strings.Contains(got, "Enable shell completion") {
		t.Fatalf("enter on an off completion row opens the enable checklist:\n%s", got)
	}

	rc := filepath.Join(home, ".zshrc")
	if err := os.WriteFile(rc, []byte(shellcomp.Block(`eval "$(ws completion zsh)"`)), 0o644); err != nil {
		t.Fatal(err)
	}
	m = typed(load(t, New(e)), "down", "down", "down")
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
