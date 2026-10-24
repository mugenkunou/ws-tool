package app

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/mugenkunou/ws-tool/internal/tui/nav"
	"github.com/mugenkunou/ws-tool/internal/tui/tuitest"
)

// TestSkillsCommands: the `:` commands are the CLI's verbs; scan is the list.
func TestSkillsCommands(t *testing.T) {
	want := []string{"install", "uninstall"}
	if got := commandNames(loaded(t, 100, 30), nav.ScreenSkills); !slices.Equal(got, want) {
		t.Errorf("skills commands = %v, want %v", got, want)
	}
}

// TestSkillsScopes: both keys act on the marked rows, else the cursor row.
func TestSkillsScopes(t *testing.T) {
	want := []string{"i:selected", "u:selected"}
	if got := declaredScopes(t, loaded(t, 100, 30), nav.ScreenSkills); !slices.Equal(got, want) {
		t.Errorf("skills declares %v, want %v", got, want)
	}
}

func TestSkillsNoHardcodedKeyNames(t *testing.T) { assertNoHardcodedKeyNames(t, "skills/skills.go") }

// skillsMachine is a real workspace and home: one skill, Claude detected.
func skillsMachine(t *testing.T) (m Model, home, ws string) {
	t.Helper()
	m = loaded(t, 100, 30)
	home, ws = t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(ws, "agent", "skills", "review-pr")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: review-pr\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	e := testEnv(t)
	e.Paths.Workspace = ws
	t.Setenv("HOME", home) // testEnv resets it
	m = New(m.overrides, e, nil)
	m = send(m, tea.WindowSizeMsg{Width: 100, Height: 30})
	return m, home, ws
}

// loadSkills runs the skills screen's load command and feeds the result back.
func loadSkills(t *testing.T, m Model) Model {
	t.Helper()
	cmd := m.screens[nav.ScreenSkills].Init()
	if cmd == nil {
		t.Fatal("skills screen has no load command")
	}
	next, _ := m.Update(cmd())
	return next.(Model)
}

// TestSkillsInstallThroughTheChooser: i asks for a tool, then shows the CLI's
// checklist; the tool's config folder decides the order, detected tools first.
func TestSkillsInstallThroughTheChooser(t *testing.T) {
	m, home, _ := skillsMachine(t)
	m = loadSkills(t, m)
	m = send(m, press("0"), press("i"))
	v := tuitest.Plain(view(m))
	if !strings.Contains(v, "Install into which tool?") {
		t.Fatalf("no tool chooser:\n%s", v)
	}
	if strings.Index(v, "claude") > strings.Index(v, "amp") {
		t.Errorf("a detected tool must come before the others:\n%s", v)
	}
	m, cmd := sendRun(m, press("enter")) // the first option: claude
	m = pump(t, m, cmd)
	v = tuitest.Plain(view(m))
	if !strings.Contains(v, "Link review-pr → ~/.claude/skills/review-pr") {
		t.Fatalf("no install checklist:\n%s", v)
	}
	if _, err := os.Lstat(filepath.Join(home, ".claude", "skills", "review-pr")); err == nil {
		t.Fatal("the chooser alone must not link anything")
	}
}

// TestSkillsInstallCommandTakesTheTool: `:install claude` skips the chooser, and
// an unknown tool is a notice, not a dialog.
func TestSkillsInstallCommandTakesTheTool(t *testing.T) {
	m, _, _ := skillsMachine(t)
	m = loadSkills(t, m)
	m = send(m, press("0"))
	m = runLineKeys(t, m, "install claude")
	if v := tuitest.Plain(view(m)); !strings.Contains(v, "Link review-pr → ~/.claude/skills/review-pr") {
		t.Fatalf("`:install claude` shows no checklist:\n%s", v)
	}
	m = send(loaded(t, 100, 30), press("0"))
	m = runLineKeys(t, m, "install nosuchtool")
	if v := tuitest.Plain(view(m)); strings.Contains(v, "Install into which tool?") {
		t.Fatalf("an unknown tool opened the chooser:\n%s", v)
	}
}
