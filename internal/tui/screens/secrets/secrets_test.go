package secrets

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/mugenkunou/ws-tool/internal/resource"
	"github.com/mugenkunou/ws-tool/internal/secret"
	"github.com/mugenkunou/ws-tool/internal/tui/env"
	"github.com/mugenkunou/ws-tool/internal/tui/nav"
	"github.com/mugenkunou/ws-tool/internal/workspace"
	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

const ws = "/home/tester/Workspace"

var findings = []secret.Violation{
	{Severity: "CRITICAL", Path: "Projects/api/.env", Line: 3, Type: "generic", Snippet: "password=hunter2"},
	{Severity: "CRITICAL", Path: "Notes/keys/token.txt", Line: 7, Type: "generic", Snippet: "token=abc123"},
}

func newModel(t *testing.T, health Health) Model {
	t.Helper()
	t.Setenv("HOME", "/home/tester")
	e := env.Env{Paths: workspace.Paths{Workspace: ws, Config: ws + "/ws.json"}, Config: wsconfig.Default()}
	var c nav.Component = New(e)
	c, _ = c.Update(nav.ResizeMsg{Width: 100, Height: 30})
	c, _ = c.Update(LoadedMsg{Violations: findings, Health: health})
	return c.(Model)
}

func kp(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	}
	return tea.KeyPressMsg{Code: []rune(s)[0], Text: s}
}

func send(m Model, msgs ...tea.Msg) (Model, tea.Cmd) {
	var cmd tea.Cmd
	for _, msg := range msgs {
		var c nav.Component
		c, cmd = m.Update(msg)
		m = c.(Model)
	}
	return m, cmd
}

func typed(m Model, ks ...string) Model {
	for _, k := range ks {
		m, _ = send(m, kp(k))
	}
	return m
}

func view(m Model) string { return ansi.Strip(m.View()) }

var healthy = Health{Pass: secret.PassHealth{Installed: true, GPGAvailable: true, Initialized: true, GitBacked: true, EntryCount: 3}}

func TestAllowActsOnTheMarkedRowsElseTheCursor(t *testing.T) {
	m := newModel(t, healthy)
	// No marks: the cursor row (the first).
	got := view(typed(m, "a"))
	if !strings.Contains(got, "Projects/api/.env:3") || strings.Contains(got, "token.txt") {
		t.Fatalf("a with no marks should allow the cursor row only:\n%s", got)
	}
	// Mark the second row, leave the cursor on the first: the marked row wins.
	got = view(typed(m, "down", "space", "up", "a"))
	if !strings.Contains(got, "token.txt:7") || strings.Contains(got, ".env:3") {
		t.Fatalf("a with marks should allow the marked rows:\n%s", got)
	}
}

func TestIgnoreActsOnTheSelectedRows(t *testing.T) {
	m := newModel(t, healthy)
	got := view(typed(m, "space", "down", "space", "i"))
	if !strings.Contains(got, "Ignore") || !strings.Contains(got, ".env") || !strings.Contains(got, "token.txt") {
		t.Fatalf("i should list every marked file:\n%s", got)
	}
}

func TestMuteAndStoreTakeTheCursorRow(t *testing.T) {
	m := newModel(t, healthy)
	// Marks do not widen a cursor-scoped key: the prompt is about the cursor row.
	marked := typed(m, "down", "space", "up")
	got := view(typed(marked, "m"))
	if !strings.Contains(got, ws+"/Projects/api") || strings.Contains(got, "Notes/keys") {
		t.Fatalf("m should prompt with the cursor row's directory:\n%s", got)
	}
	got = view(typed(marked, "s"))
	if !strings.Contains(got, "Entry name for "+ws+"/Projects/api/.env:3") {
		t.Fatalf("s should prompt for the cursor row's entry:\n%s", got)
	}
}

func TestStoreNeedsAPassStore(t *testing.T) {
	m := newModel(t, Health{})
	m, cmd := send(m, kp("s"))
	if m.CapturesKeys() || cmd == nil {
		t.Fatalf("s without a store should notify, not prompt")
	}
	if msg, ok := cmd().(nav.NoticeMsg); !ok || !strings.Contains(msg.Text, ":pass init") {
		t.Fatalf("notice: %#v", msg)
	}
}

func TestEnterShowsTheFindingInContext(t *testing.T) {
	m := typed(newModel(t, healthy), "enter")
	if m.level != levelDetail {
		t.Fatal("enter should open the detail view")
	}
}

func TestOldKeysAreGone(t *testing.T) {
	m := newModel(t, healthy)
	for _, k := range []string{"l", "L", "e", "p", "D", "g", "v", "o", "S", "C", "c", "X"} {
		after, cmd := send(m, kp(k))
		if after.CapturesKeys() || after.level != levelList || (cmd != nil && k != "g") {
			t.Errorf("key %q still does something", k)
		}
	}
}

func TestCommands(t *testing.T) {
	m := newModel(t, healthy)
	want := []string{"unmute", "pass init", "pass status", "pass audit", "pass backup enable", "pass backup disable",
		"pass backup status", "pass backup push", "credential enable", "credential disable", "credential status"}
	var got []string
	for _, c := range m.Commands() {
		got = append(got, c.Name)
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("commands:\n got %v\nwant %v", got, want)
	}

	run := func(line string) (Model, tea.Cmd) {
		return send(m, nav.CommandMsg{Words: strings.Fields(line), Line: line})
	}
	if after, _ := run("pass audit"); after.level != levelDetail {
		t.Error("pass audit should open a view")
	}
	if after, _ := run("pass status"); after.level != levelDetail {
		t.Error("pass status should open a view")
	}
	if after, _ := run("pass backup status"); after.level != levelDetail {
		t.Error("pass backup status should open a view")
	}
	for _, line := range []string{"pass init", "pass backup enable", "credential enable", "credential disable", "credential status"} {
		if after, cmd := run(line); cmd == nil || after.level == levelDetail || after.CapturesKeys() {
			t.Errorf("%s should hand the terminal over", line)
		}
	}
	// A checklist, not a direct run.
	noRemote := healthy
	noRemote.Pass.GitRemote = false
	if after, _ := send(newModel(t, noRemote), nav.CommandMsg{Words: []string{"pass", "backup", "push"}}); after.CapturesKeys() {
		t.Error("pass backup push without a remote should not open a checklist")
	}
	remote := healthy
	remote.Pass.GitRemote = true
	if after, _ := send(newModel(t, remote), nav.CommandMsg{Words: []string{"pass", "backup", "push"}}); !strings.Contains(view(after), "Push the pass store") {
		t.Errorf("pass backup push should open a checklist:\n%s", view(after))
	}
	if after, _ := run("pass backup disable"); !strings.Contains(view(after), "Disable the pass backup") {
		t.Errorf("pass backup disable should open a checklist:\n%s", view(after))
	}
	// unmute needs a directory.
	if _, cmd := run("unmute"); cmd == nil {
		t.Error("unmute without a directory should say so")
	}
}

func TestCommandLeavesTheDetailView(t *testing.T) {
	m := typed(newModel(t, healthy), "enter")
	m, _ = send(m, nav.CommandMsg{Words: []string{"pass", "audit"}})
	if m.detailTitle != "Pass store audit" {
		t.Fatalf("title %q", m.detailTitle)
	}
}

func TestFilterMsgNarrowsTheList(t *testing.T) {
	m, _ := send(newModel(t, healthy), nav.FilterMsg{Screen: nav.ScreenSecrets, Filter: "path=token"})
	if got := view(m); !strings.Contains(got, "token.txt") || strings.Contains(got, ".env") {
		t.Fatalf("filter not applied:\n%s", got)
	}
}

func TestHeaderCue(t *testing.T) {
	h := healthy
	if strings.Contains(view(newModel(t, h)), "no workspace copy") {
		t.Error("cue shown although the store has a workspace copy")
	}
	h.CopyCommand = "dotfile add ~/.password-store --dir --to ~/Workspace/secrets/dotfiles/password-store"
	first := strings.SplitN(view(newModel(t, h)), "\n", 2)[0]
	if !strings.HasSuffix(strings.TrimSpace(first), "ℹ no workspace copy") {
		t.Errorf("header should end with the cue:\n%s", first)
	}
}

func TestYIsTheCLIJSON(t *testing.T) {
	m := typed(newModel(t, healthy), "y")
	want, err := json.MarshalIndent(secret.Schema(resource.CLIView(), ws).Record(findings[0]), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := view(m)
	for _, line := range strings.Split(string(want), "\n") {
		if !strings.Contains(got, strings.TrimSpace(line)) {
			t.Fatalf("y pane lacks %q:\n%s", line, got)
		}
	}
}

// TestNoKeyNamesInText: strings never name a key; they use the binding's help.
func TestNoKeyNamesInText(t *testing.T) {
	src, err := os.ReadFile("secrets.go")
	if err != nil {
		t.Fatal(err)
	}
	if re := regexp.MustCompile(`(?i)\bpress [a-z]\b`); re.Match(src) {
		t.Fatalf("secrets.go names a key in text: %s", re.Find(src))
	}
}
