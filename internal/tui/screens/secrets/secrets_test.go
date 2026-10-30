package secrets

import (
	"encoding/json"
	"os"
	"path/filepath"
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

// menuPick opens the `.` menu, presses letter, and feeds the command's result
// back into the screen, to the end.
func menuPick(t *testing.T, m Model, letter string) Model {
	t.Helper()
	m, _ = menuPickNotices(t, m, letter)
	return m
}

// menuPickNotices is menuPick that also returns the notice texts the command
// produced.
func menuPickNotices(t *testing.T, m Model, letter string) (Model, []string) {
	t.Helper()
	m, _ = send(m, nav.MenuMsg{})
	m, cmd := send(m, kp(letter))
	return settle(t, m, cmd)
}

// settle runs cmd and feeds every message it yields back into m, to the end,
// and returns the notice texts it produced.
func settle(t *testing.T, m Model, cmd tea.Cmd) (Model, []string) {
	t.Helper()
	var notices []string
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
		case nav.NoticeMsg:
			notices = append(notices, msg.Text)
		default:
			var next tea.Cmd
			m, next = send(m, msg)
			queue = append(queue, next)
		}
	}
	return m, notices
}

// newModelIn is newModel over a real workspace on disk whose ws.json holds
// skip_dirs (workspace-relative, stored absolute as the CLI writes them), so
// unskip can read them.
func newModelIn(t *testing.T, skipDirs []string) Model {
	t.Helper()
	root := filepath.Join(t.TempDir(), "Workspace")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := wsconfig.Default()
	for _, d := range skipDirs {
		cfg.Secret.SkipDirs = append(cfg.Secret.SkipDirs, filepath.Join(root, d))
	}
	cfgPath := wsconfig.ConfigPath(root)
	if err := wsconfig.Save(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	e := env.Env{Paths: workspace.Paths{Workspace: root, Config: cfgPath}, Config: cfg}
	var c nav.Component = New(e)
	c, _ = c.Update(nav.ResizeMsg{Width: 100, Height: 30})
	c, _ = c.Update(LoadedMsg{Violations: findings})
	return c.(Model)
}

func TestCommands(t *testing.T) {
	m := newModel(t)
	want := []string{"skip", "unskip"}
	wantLetters := []string{"s", "u"}
	var got, letters []string
	for _, c := range m.Commands() {
		got = append(got, c.Name)
		letters = append(letters, c.Menu)
	}
	if strings.Join(got, ",") != strings.Join(want, ",") || strings.Join(letters, ",") != strings.Join(wantLetters, ",") {
		t.Fatalf("commands:\n got %v %v\nwant %v %v", got, letters, want, wantLetters)
	}

	// skip with no directory prompts for one.
	if after := menuPick(t, newModel(t), "s"); !after.modal.Open() {
		t.Error("skip without a directory should prompt for one")
	}
	// unskip with nothing skipped says so, and opens no checklist.
	after, notices := menuPickNotices(t, newModelIn(t, nil), "u")
	if len(notices) != 1 || notices[0] != "No directories are skipped." || after.CapturesKeys() {
		t.Fatalf("unskip with nothing skipped: notices %q, captures keys %v", notices, after.CapturesKeys())
	}
	// unskip with a skipped directory opens the checklist, nothing pre-checked.
	after = menuPick(t, newModelIn(t, []string{"Projects/old"}), "u")
	if !after.CapturesKeys() {
		t.Fatal("unskip with skipped directories opens a checklist")
	}
	if got := view(after); !strings.Contains(got, "Projects/old") || !strings.Contains(got, "[ ]") || strings.Contains(got, "[x]") {
		t.Fatalf("checklist should list the skipped directory, unchecked:\n%s", got)
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
