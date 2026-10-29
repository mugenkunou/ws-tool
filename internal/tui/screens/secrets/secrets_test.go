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

func newModel(t *testing.T) Model {
	t.Helper()
	t.Setenv("HOME", "/home/tester")
	e := env.Env{Paths: workspace.Paths{Workspace: ws, Config: ws + "/ws.json"}, Config: wsconfig.Default()}
	var c nav.Component = New(e)
	c, _ = c.Update(nav.ResizeMsg{Width: 100, Height: 30})
	c, _ = c.Update(LoadedMsg{Violations: findings})
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

func TestAllowActsOnTheMarkedRowsElseTheCursor(t *testing.T) {
	m := newModel(t)
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

// TestMuteActsOnTheMarkedRowsElseTheCursor: m mutes the cursor row with no
// marks, or the marked rows when some are marked — the same pattern as a.
// It stays visible in the checklist rather than disappearing like allow.
func TestMuteActsOnTheMarkedRowsElseTheCursor(t *testing.T) {
	m := newModel(t)
	got := view(typed(m, "m"))
	if !strings.Contains(got, "Projects/api/.env:3") || strings.Contains(got, "token.txt") {
		t.Fatalf("m with no marks should mute the cursor row only:\n%s", got)
	}
	got = view(typed(m, "down", "space", "up", "m"))
	if !strings.Contains(got, "token.txt:7") || strings.Contains(got, ".env:3") {
		t.Fatalf("m with marks should mute the marked rows:\n%s", got)
	}
}

func TestIgnoreActsOnTheSelectedRows(t *testing.T) {
	m := newModel(t)
	got := view(typed(m, "space", "down", "space", "i"))
	if !strings.Contains(got, "Ignore") || !strings.Contains(got, ".env") || !strings.Contains(got, "token.txt") {
		t.Fatalf("i should list every marked file:\n%s", got)
	}
}

func TestEnterShowsTheFindingInContext(t *testing.T) {
	m := typed(newModel(t), "enter")
	if m.level != levelDetail {
		t.Fatal("enter should open the detail view")
	}
}

func TestOldKeysAreGone(t *testing.T) {
	m := newModel(t)
	for _, k := range []string{"l", "L", "e", "p", "D", "g", "v", "o", "S", "C", "c", "X"} {
		after, cmd := send(m, kp(k))
		if after.CapturesKeys() || after.level != levelList || (cmd != nil && k != "g") {
			t.Errorf("key %q still does something", k)
		}
	}
}

func TestCommands(t *testing.T) {
	m := newModel(t)
	want := []string{"skip", "unskip"}
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
	// skip with no directory prompts for one.
	if after, _ := run("skip"); !after.modal.Open() {
		t.Error("skip without a directory should prompt for one")
	}
	// unskip needs a directory.
	if _, cmd := run("unskip"); cmd == nil {
		t.Error("unskip without a directory should say so")
	}
}

func TestFilterMsgNarrowsTheList(t *testing.T) {
	m, _ := send(newModel(t), nav.FilterMsg{Screen: nav.ScreenSecrets, Filter: "path=token"})
	if got := view(m); !strings.Contains(got, "token.txt") || strings.Contains(got, ".env") {
		t.Fatalf("filter not applied:\n%s", got)
	}
}

func TestYIsTheCLIJSON(t *testing.T) {
	m := typed(newModel(t), "y")
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
