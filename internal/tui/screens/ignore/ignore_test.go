package ignore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	wsignore "github.com/mugenkunou/ws-tool/internal/ignore"
	"github.com/mugenkunou/ws-tool/internal/tui/env"
	"github.com/mugenkunou/ws-tool/internal/tui/nav"
	"github.com/mugenkunou/ws-tool/internal/workspace"
	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

func press(s string) tea.KeyPressMsg {
	switch s {
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "ctrl+d":
		return tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl}
	}
	return tea.KeyPressMsg{Code: rune(s[0]), Text: s}
}

// fixture is a real workspace in a temp dir with a screen loaded from it.
type fixture struct {
	root string
	cfg  string
	c    nav.Component
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	root := filepath.Join(t.TempDir(), "Workspace")
	cfg := wsconfig.ConfigPath(root)
	if err := os.MkdirAll(filepath.Join(root, "Projects"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := wsconfig.Save(cfg, wsconfig.Default()); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"Projects/a.dat", "Projects/b.dat", "Projects/big.bin"} {
		if err := os.WriteFile(filepath.Join(root, f), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := wsconfig.AddIgnoreExclude(cfg, "Projects/big.bin", ""); err != nil {
		t.Fatal(err)
	}
	e := env.Env{Paths: workspace.Paths{Workspace: root, Config: cfg}, Config: wsconfig.Default(), ScratchDir: filepath.Join(t.TempDir(), "Scratch")}
	var c nav.Component = New(e)
	c, _ = c.Update(nav.ResizeMsg{Width: 400, Height: 20})
	c, _ = c.Update(LoadedMsg{
		Violations: []wsignore.Violation{
			{Type: "bloat", Severity: "WARNING", Path: "Projects/a.dat", SizeBytes: 1},
			{Type: "bloat", Severity: "WARNING", Path: "Projects/b.dat", SizeBytes: 1},
		},
		Ignored: []wsignore.IgnoredPath{{Path: "Projects/big.bin", SizeBytes: 1, Rule: "Projects/big.bin", RuleSource: "user"}},
	})
	return &fixture{root: root, cfg: cfg, c: c}
}

func (f *fixture) press(keys ...string) *fixture {
	for _, k := range keys {
		var cmd tea.Cmd
		f.c, cmd = f.c.Update(press(k))
		if k == "esc" { // closing a dialog answers through a command
			f.run(cmd)
		}
	}
	return f
}

// run feeds the messages a command produces back to the screen.
func (f *fixture) run(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case nil:
	case tea.BatchMsg:
		for _, c := range msg {
			f.run(c)
		}
	default:
		var next tea.Cmd
		f.c, next = f.c.Update(msg)
		f.run(next)
	}
}

func (f *fixture) view() string { return ansi.Strip(f.c.View()) }

func (f *fixture) abs(rel string) string { return filepath.Join(f.root, rel) }

func (f *fixture) expect(t *testing.T, has []string, hasNot ...string) {
	t.Helper()
	v := f.view()
	for _, w := range has {
		if !strings.Contains(v, w) {
			t.Errorf("view lacks %q:\n%s", w, v)
		}
	}
	for _, w := range hasNot {
		if strings.Contains(v, w) {
			t.Errorf("view has %q:\n%s", w, v)
		}
	}
}

// TestWarningsKeysActOnSelectedRows: with nothing marked a key acts on the
// cursor row; with marks, on the marked rows (spec §6.4), and each checklist
// names exactly what happens.
func TestWarningsKeysActOnSelectedRows(t *testing.T) {
	for _, c := range []struct {
		key  string
		line string // what each row's line says
	}{
		{"i", "Ignore %s (ws.json ignore_rules.exclude)"},
		{"m", "Mute %s (it keeps syncing)"},
		{"s", "Move %s → "},
		{"ctrl+d", "Delete %s (1 B) (to trash)"},
	} {
		line := func(rel string) string { return strings.Replace(c.line, "%s", filepath.Join("%ROOT%", rel), 1) }
		f := newFixture(t)
		f.press(c.key)
		f.expect(t, []string{strings.ReplaceAll(line("Projects/a.dat"), "%ROOT%", f.root)}, f.abs("Projects/b.dat"))

		f = newFixture(t)
		f.press("space", "down", "space", c.key)
		f.expect(t, []string{strings.ReplaceAll(line("Projects/a.dat"), "%ROOT%", f.root), strings.ReplaceAll(line("Projects/b.dat"), "%ROOT%", f.root)})
	}
}

func TestMutedKeys(t *testing.T) {
	f := newFixture(t)
	if _, err := wsconfig.AddIgnoreMuted(f.cfg, "Projects"); err != nil {
		t.Fatal(err)
	}
	f.c, _ = f.c.Update(LoadedMsg{
		Violations: []wsignore.Violation{{Type: "bloat", Severity: "WARNING", Path: "Projects/a.dat", Muted: true}},
	})
	f.press("v", "u")
	f.expect(t, []string{"Unmute " + f.abs("Projects") + " (its warnings include " + f.abs("Projects/a.dat") + ")"})

	f.press("esc") // close the checklist
	f.expect(t, []string{"Muted 1"})
	f.press("i")
	f.expect(t, []string{"Ignore " + f.abs("Projects/a.dat") + " (ws.json ignore_rules.exclude)"})
}

// TestMutedViewListsStaleEntries: a muted path that no longer exists is a Muted
// row, u removes the entry, and p sweeps every stale row in one plan.
func TestMutedViewListsStaleEntries(t *testing.T) {
	f := newFixture(t)
	if _, err := wsconfig.AddIgnoreMuted(f.cfg, "old"); err != nil {
		t.Fatal(err)
	}
	f.c, _ = f.c.Update(LoadedMsg{
		Violations: []wsignore.Violation{{Type: "bloat", Severity: "WARNING", Path: "Projects/a.dat"}},
		Stale:      wsignore.StaleViolations(f.root, []string{"old"}),
	})
	f.expect(t, []string{"Warnings 1 · Muted 1 · Ignored 0"})
	f.press("v")
	f.expect(t, []string{"stale-mute", "old"})
	f.press("u")
	f.expect(t, []string{"Remove " + f.abs("old") + " (muted, no longer exists)"})
	if got := (LoadedMsg{Violations: []wsignore.Violation{{Muted: true}, {}}, Stale: wsignore.StaleViolations(f.root, []string{"old"})}).Actionable(); got != 1 {
		t.Errorf("stale rows are not warnings: Actionable = %d", got)
	}
}

// TestPruneSweepsStaleEntries: p is `ignore unmute --stale`: one checklist
// action per stale entry, whatever else the Muted view holds, and nothing to
// do when none are stale.
func TestPruneSweepsStaleEntries(t *testing.T) {
	f := newFixture(t)
	f.c, _ = f.c.Update(LoadedMsg{
		Violations: []wsignore.Violation{{Type: "bloat", Severity: "WARNING", Path: "Projects/a.dat", Muted: true}},
		Stale:      wsignore.StaleViolations(f.root, []string{"old", "gone/deeper"}),
	})
	f.press("v", "p")
	f.expect(t, []string{"Remove 2 stale entries",
		"Remove " + f.abs("old") + " (muted, no longer exists)",
		"Remove " + f.abs("gone/deeper") + " (muted, no longer exists)"},
		"Projects/a.dat") // live muted rows stay out of the plan

	f.press("esc") // cancel: nothing was removed
	if _, err := wsconfig.Load(f.cfg); err != nil {
		t.Fatal(err)
	}

	f.press("p") // still nothing marked or filtered: the whole view again
	f.expect(t, []string{"Remove 2 stale entries"})

	// With the filter the Dashboard's issue opens, p sweeps only what shows.
	f.press("esc", "/")
	f.press("t", "y", "p", "e", "=", "s", "t", "a", "l", "e", "-") // type=stale-
	f.expect(t, []string{"/type=stale-"}, "a.dat")
}

func TestIgnoredStopIgnoringIsRm(t *testing.T) {
	f := newFixture(t)
	f.press("v", "v", "ctrl+d")
	f.expect(t, []string{"Sync again: " + f.abs("Projects/big.bin")})
}

// TestCommandsShareTheKeysFunctions: `:rm` builds the plan the Ignored view's
// ctrl+d does, and `:lint` narrows the Warnings view.
func TestCommands(t *testing.T) {
	f := newFixture(t)
	f.c, _ = f.c.Update(nav.CommandMsg{Words: []string{"rm", f.abs("Projects/big.bin")}})
	f.expect(t, []string{"Sync again: " + f.abs("Projects/big.bin")})

	f = newFixture(t)
	if err := os.MkdirAll(f.abs("Other"), 0o755); err != nil {
		t.Fatal(err)
	}
	f.c, _ = f.c.Update(nav.CommandMsg{Words: []string{"lint", f.abs("Other")}})
	f.expect(t, []string{"Warnings 0 · Muted 0 · Ignored 0", "under"}, "a.dat")
	f.press("esc")
	f.expect(t, []string{"Warnings 2"}, "under")
}

// TestAWarningIsNeverActedOnWithoutAChecklist: no key changes anything
// directly, so a destructive key leaves the file alone until the checklist is
// confirmed.
func TestNothingAppliesFromAKey(t *testing.T) {
	f := newFixture(t)
	f.press("ctrl+d")
	if _, err := os.Stat(f.abs("Projects/a.dat")); err != nil {
		t.Fatalf("ctrl+d deleted the file before the checklist was confirmed: %v", err)
	}
	if !f.c.CapturesKeys() {
		t.Error("the checklist is not open")
	}
}
