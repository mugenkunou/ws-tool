package scratch

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	wsscratch "github.com/mugenkunou/ws-tool/internal/scratch"
	"github.com/mugenkunou/ws-tool/internal/tui/env"
	"github.com/mugenkunou/ws-tool/internal/tui/keys"
	"github.com/mugenkunou/ws-tool/internal/tui/nav"
	"github.com/mugenkunou/ws-tool/internal/tui/reslist"
	"github.com/mugenkunou/ws-tool/internal/workspace"
	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

// The grammar of spec §6.5: keys, the rows each acts on, `:` commands.

func fixtureRows() []wsscratch.Entry {
	return []wsscratch.Entry{
		{Name: "alpha", Path: "/s/alpha", Tags: []string{"net"}},
		{Name: "beta", Path: "/s/beta", Tags: []string{"net", "dns"}},
		{Name: "gamma", Path: "/s/gamma"},
	}
}

func screenIn(t *testing.T, e env.Env, rows []wsscratch.Entry) nav.Component {
	t.Helper()
	c, _ := New(e).Update(nav.ResizeMsg{Width: 120, Height: 20})
	c, _ = c.Update(LoadedMsg{Entries: rows, Tags: []string{"net", "dns", "perf"}})
	return c
}

func screen(t *testing.T, rows []wsscratch.Entry) nav.Component {
	return screenIn(t, env.Env{ScratchDir: "/s", Paths: workspace.Paths{Workspace: "/ws", Config: "/ws/ws.json"}, Config: wsconfig.Default()}, rows)
}

func keyMsg(k string) tea.KeyPressMsg {
	switch k {
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "ctrl+d":
		return tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl}
	}
	return tea.KeyPressMsg{Code: rune(k[0]), Text: k}
}

func press(c nav.Component, ks ...string) nav.Component {
	for _, k := range ks {
		c, _ = c.Update(keyMsg(k))
	}
	return c
}

func typeText(c nav.Component, s string) nav.Component {
	for _, r := range s {
		c = press(c, string(r))
	}
	return c
}

// submit presses enter and delivers what it produces (a prompt's result, a
// batch of commands) back to the screen.
func submit(c nav.Component) nav.Component {
	c, cmd := c.Update(keyMsg("enter"))
	queue := []tea.Cmd{cmd}
	for steps := 0; len(queue) > 0 && steps < 50; steps++ {
		run := queue[0]
		queue = queue[1:]
		if run == nil {
			continue
		}
		switch msg := run().(type) {
		case nil:
		case tea.BatchMsg:
			queue = append(queue, msg...)
		default:
			var next tea.Cmd
			c, next = c.Update(msg)
			queue = append(queue, next)
		}
	}
	return c
}

func plain(c nav.Component) string { return ansi.Strip(c.View()) }

func command(c nav.Component, line string) (nav.Component, tea.Cmd) {
	return c.Update(nav.CommandMsg{Words: strings.Fields(line), Line: line})
}

// TestKeysAreTheSpecTable: the footer is the spec's keys, letters and labels,
// in the table's order.
func TestKeysAreTheSpecTable(t *testing.T) {
	var got []string
	for _, b := range keys.Scratch().ShortHelp() {
		got = append(got, b.Help().Key+" "+b.Help().Desc)
	}
	want := []string{"a new", "enter open", "t tag", "ctrl+d delete"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("keys = %v, want %v", got, want)
	}
}

// TestEveryActionDeclaresItsScope: each key acts on the rows spec §6.5 says.
func TestEveryActionDeclaresItsScope(t *testing.T) {
	want := map[string]reslist.Scope{
		"a": reslist.Global, "enter": reslist.Cursor, "t": reslist.Selected, "ctrl+d": reslist.Selected,
	}
	got := map[string]reslist.Scope{}
	for _, d := range New(env.Env{}).Declared() {
		got[d.Key.Keys()[0]] = d.Scope
	}
	for k, s := range want {
		if g, ok := got[k]; !ok || g != s {
			t.Errorf("key %s: scope %v (declared %v), want %v", k, g, ok, s)
		}
	}
	if len(got) != len(want) {
		t.Errorf("declared keys = %v, want exactly %v", got, want)
	}
}

// TestRemovedKeysDoNothing: n, o, T and F are gone.
func TestRemovedKeysDoNothing(t *testing.T) {
	for _, k := range []string{"n", "o", "T", "F"} {
		c, cmd := screen(t, fixtureRows()).Update(keyMsg(k))
		if c.CapturesKeys() || cmd != nil {
			t.Errorf("%q still does something", k)
		}
	}
}

// TestNoHardcodedKeyNames: text names keys through their bindings.
func TestNoHardcodedKeyNames(t *testing.T) {
	src, err := os.ReadFile("scratch.go")
	if err != nil {
		t.Fatal(err)
	}
	if re := regexp.MustCompile(`"[^"]*\b[Pp]ress [A-Za-z]\b`); re.Match(src) {
		t.Errorf("scratch.go names a key in text: %s", re.Find(src))
	}
	if want := "Press " + keys.Scratch().New.Help().Key + " to start one."; !strings.Contains(plain(screen(t, nil)), want) {
		t.Errorf("empty list lacks %q:\n%s", want, plain(screen(t, nil)))
	}
}

// TestNewPromptsForAName: a acts on no row.
func TestNewPromptsForAName(t *testing.T) {
	c := press(screen(t, fixtureRows()), "a")
	if !c.CapturesKeys() || !strings.Contains(plain(c), "New scratch directory") {
		t.Errorf("a should prompt:\n%s", plain(c))
	}
	c = press(screen(t, nil), "a")
	if !c.CapturesKeys() {
		t.Error("a works on an empty list")
	}
}

// TestEnterOpensTheCursorRowOnly: enter hands the cursor row to the editor and
// opens no dialog.
func TestEnterOpensTheCursorRowOnly(t *testing.T) {
	c := press(screen(t, fixtureRows()), "space", "down", "down") // a mark elsewhere
	c, cmd := c.Update(keyMsg("enter"))
	if c.CapturesKeys() || cmd == nil {
		t.Error("enter should launch the editor")
	}
	if _, cmd := screen(t, nil).Update(keyMsg("enter")); cmd != nil {
		t.Error("enter with no row does nothing")
	}
}

// TestTagActsOnMarksElseCursor and its -tag syntax.
func TestTagActsOnMarksElseCursor(t *testing.T) {
	c := press(screen(t, fixtureRows()), "down", "t")
	if v := plain(c); !strings.Contains(v, "Tag beta") || !strings.Contains(v, "Current: net, dns") || !strings.Contains(v, "-tag removes one") {
		t.Errorf("cursor row's prompt:\n%s", v)
	}
	c = submit(typeText(c, "perf, -net"))
	v := plain(c)
	if !strings.Contains(v, "Tag beta: +perf −net") {
		t.Errorf("plan line:\n%s", v)
	}

	c = press(screen(t, fixtureRows()), "space", "down", "down", "space", "t")
	if v := plain(c); !strings.Contains(v, "Tag 2 directories") {
		t.Errorf("marked rows' prompt:\n%s", v)
	}
	c = submit(typeText(c, "-net"))
	v = plain(c)
	if !strings.Contains(v, "Tag alpha: −net") || strings.Contains(v, "beta") || strings.Contains(v, "gamma") {
		t.Errorf("only the marked row that has the tag changes:\n%s", v)
	}

	c = submit(typeText(press(screen(t, fixtureRows()), "t"), "net")) // alpha already has it
	if c.CapturesKeys() {
		t.Errorf("no change opens no checklist:\n%s", plain(c))
	}
}

// TestTagActionWritesTheTags runs the action for real.
func TestTagActionWritesTheTags(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "alpha")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := wsconfig.ConfigPath(filepath.Join(root, "Workspace"))
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := wsconfig.Save(cfgPath, wsconfig.Default()); err != nil {
		t.Fatal(err)
	}
	m := New(env.Env{ScratchDir: root, Paths: workspace.Paths{Workspace: filepath.Dir(cfgPath), Config: cfgPath}})
	e := wsscratch.Entry{Name: "alpha", Path: dir}
	tags, added, _ := wsscratch.ApplyTags(nil, []string{"net, perf"}, nil)
	if err := m.tagAction(e, tags, added, "").Execute(); err != nil {
		t.Fatal(err)
	}
	meta, err := wsscratch.LoadMeta(dir)
	if err != nil || strings.Join(meta.Tags, ",") != "net,perf" {
		t.Fatalf("meta = %+v, %v", meta, err)
	}
}

// TestDeleteActsOnMarksElseCursor: ctrl+d always opens the checklist.
func TestDeleteActsOnMarksElseCursor(t *testing.T) {
	v := plain(press(screen(t, fixtureRows()), "down", "ctrl+d"))
	if !strings.Contains(v, "Delete 1 scratch directory") || !strings.Contains(v, "/s/beta") || strings.Contains(v, "/s/alpha") {
		t.Errorf("cursor row only:\n%s", v)
	}
	v = plain(press(screen(t, fixtureRows()), "space", "down", "down", "space", "ctrl+d"))
	if !strings.Contains(v, "Delete 2 scratch directories") || !strings.Contains(v, "/s/alpha") || !strings.Contains(v, "/s/gamma") || strings.Contains(v, "/s/beta") {
		t.Errorf("marked rows only:\n%s", v)
	}
}

// TestCommandsAreTheSpecList: the `:` commands are spec §6.5's.
func TestCommandsAreTheSpecList(t *testing.T) {
	var names []string
	for _, c := range New(env.Env{}).Commands() {
		names = append(names, c.Name)
	}
	if strings.Join(names, "|") != "add" {
		t.Errorf("commands = %v, want [add]", names)
	}
}

// TestAddCommandRoutesAndPlans: `:add <path>` reaches the screen as a
// nav.CommandMsg, checks every path, and opens the checklist with where each
// goes.
func TestAddCommandRoutesAndPlans(t *testing.T) {
	root := t.TempDir()
	scratchDir := filepath.Join(root, "Scratch")
	wsDir := filepath.Join(root, "Workspace")
	for _, d := range []string{scratchDir, wsDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	capture := filepath.Join(root, "capture.pcap")
	if err := os.WriteFile(capture, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	e := env.Env{ScratchDir: scratchDir, Paths: workspace.Paths{Workspace: wsDir, Config: wsconfig.ConfigPath(wsDir)}, Config: wsconfig.Default()}

	wide, _ := screenIn(t, e, nil).Update(nav.ResizeMsg{Width: 400, Height: 20})
	c, _ := command(wide, "add "+capture)
	v := plain(c)
	want := "Move " + capture + " → " + filepath.Join(scratchDir, "capture", "capture.pcap")
	if !c.CapturesKeys() || !strings.Contains(v, want) {
		t.Errorf("checklist lacks %q:\n%s", want, v)
	}

	// A path that cannot move stops the whole command: a notice, no dialog.
	c, cmd := command(screenIn(t, e, nil), "add "+capture+" "+filepath.Join(root, "missing"))
	if c.CapturesKeys() {
		t.Error("one bad path must stop the command")
	}
	if n, ok := cmd().(nav.NoticeMsg); !ok || !strings.Contains(n.Text, "path not found") {
		t.Errorf("notice = %#v", cmd())
	}
}
