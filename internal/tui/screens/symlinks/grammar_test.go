package symlinks

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/mugenkunou/ws-tool/internal/symlink"
	"github.com/mugenkunou/ws-tool/internal/tui/env"
	"github.com/mugenkunou/ws-tool/internal/tui/keys"
	"github.com/mugenkunou/ws-tool/internal/tui/nav"
	"github.com/mugenkunou/ws-tool/internal/tui/reslist"
	"github.com/mugenkunou/ws-tool/internal/workspace"
	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

// The grammar of spec §6.5: keys, the rows each acts on, `:` commands.

// fixtureRows: healthy, broken, drifted, and a missing sudo entry.
func fixtureRows() []Entry {
	return []Entry{
		{System: "/h/.ok", Original: "symlinks/home/.ok", WorkspacePath: "/ws/symlinks/home/.ok", Status: symlink.StatusOK, Registered: true},
		{System: "/h/.broken", Original: "symlinks/home/.broken", WorkspacePath: "/ws/symlinks/home/.broken", Status: symlink.StatusBroken, Registered: true},
		{System: "/h/.over", Original: "symlinks/home/.over", WorkspacePath: "/ws/symlinks/home/.over", Status: symlink.StatusDrifted, Registered: true},
		{System: "/etc/hosts", Original: "symlinks/system/etc/hosts", WorkspacePath: "/ws/symlinks/system/etc/hosts", Sudo: true, Status: symlink.StatusMissing, Registered: true},
	}
}

func screen(t *testing.T, rows []Entry) nav.Component {
	t.Helper()
	e := env.Env{Paths: workspace.Paths{Workspace: "/ws", Config: "/ws/ws.json"}}
	c, _ := New(e).Update(nav.ResizeMsg{Width: 120, Height: 20})
	c, _ = c.Update(LoadedMsg{Entries: rows})
	return c
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

func pressRun(c nav.Component, k string) (nav.Component, tea.Cmd) { return c.Update(keyMsg(k)) }

func plain(c nav.Component) string { return ansi.Strip(c.View()) }

func command(c nav.Component, line string) (nav.Component, tea.Cmd) {
	return c.Update(nav.CommandMsg{Words: strings.Fields(line), Line: line})
}

// TestKeysAreTheSpecTable: the footer is the spec's keys, letters and labels,
// in the table's order.
func TestKeysAreTheSpecTable(t *testing.T) {
	var got []string
	for _, b := range keys.Symlinks().ShortHelp() {
		got = append(got, b.Help().Key+" "+b.Help().Desc)
	}
	want := []string{"a add", "ctrl+d unlink", "l link"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("keys = %v, want %v", got, want)
	}
}

// TestEveryActionDeclaresItsScope: each key acts on the rows spec §6.5 says.
func TestEveryActionDeclaresItsScope(t *testing.T) {
	want := map[string]reslist.Scope{
		"a": reslist.Marked, "ctrl+d": reslist.Selected, "l": reslist.All,
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

// TestNoHardcodedKeyNames: text names keys through their bindings.
func TestNoHardcodedKeyNames(t *testing.T) {
	src, err := os.ReadFile("symlinks.go")
	if err != nil {
		t.Fatal(err)
	}
	if re := regexp.MustCompile(`"[^"]*\b[Pp]ress [A-Za-z]\b`); re.Match(src) {
		t.Errorf("symlinks.go names a key in text: %s", re.Find(src))
	}
	v := plain(screen(t, nil))
	if want := "Press " + keys.Symlinks().Add.Help().Key + " to add one."; !strings.Contains(v, want) {
		t.Errorf("empty list lacks %q:\n%s", want, v)
	}
}

// TestAddWithNothingMarkedScansForTheChecklist: without marks, a looks for what the
// catalog knows about this machine (no prompt for a path).
func TestAddWithNothingMarkedScansForTheChecklist(t *testing.T) {
	c, cmd := pressRun(screen(t, fixtureRows()), "a")
	if cmd == nil || !c.(Model).preparing || c.CapturesKeys() {
		t.Fatal("a with no marks starts the scan, and opens no path prompt")
	}
}

// TestChecklistIsTheCLIsChecklist: the rows are ChecklistRows, checked the same way,
// and nothing found is a notice.
func TestChecklistIsTheCLIsChecklist(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("WS_SYMLINK_ROOT", t.TempDir())
	root := filepath.Join(t.TempDir(), "Workspace")
	if err := os.MkdirAll(filepath.Join(root, "symlinks"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := wsconfig.ConfigPath(root)
	if err := wsconfig.Save(cfgPath, wsconfig.Default()); err != nil {
		t.Fatal(err)
	}
	e := env.Env{Paths: workspace.Paths{Workspace: root, Config: cfgPath}}

	// Nothing found: a notice, no dialog.
	msg := prepareChecklist(e)().(checklistMsg)
	m, cmd := New(e).openChecklist(msg)
	if m.modal.Open() || cmd == nil {
		t.Fatal("nothing found: a notice")
	}

	// A git config (ordinary) and an ssh key (secret).
	for name, body := range map[string]string{".gitconfig": "[user]\n", ".ssh/id_rsa": "KEY\n", ".ssh/id_rsa.pub": "PUB\n"} {
		p := filepath.Join(home, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	msg = prepareChecklist(e)().(checklistMsg)
	if len(msg.rows) != 2 {
		t.Fatalf("rows = %+v", msg.rows)
	}
	sized, _ := New(e).Update(nav.ResizeMsg{Width: 120, Height: 24})
	m, _ = sized.(Model).openChecklist(msg)
	v := ansi.Strip(m.View())
	for _, want := range []string{"Add git: ~/.gitconfig", "Add ssh: ~/.ssh/id_rsa + .pub", "secret"} {
		if !strings.Contains(v, want) {
			t.Errorf("checklist lacks %q:\n%s", want, v)
		}
	}
	if !strings.Contains(v, "[x] Add git") || !strings.Contains(v, "[ ] Add ssh") {
		t.Errorf("ordinary rows are checked, secret rows are not:\n%s", v)
	}
}

// TestAddCommandTakesTwoPaths: `:add <host> [<ws>] [--dir]`.
func TestAddCommandTakesTwoPaths(t *testing.T) {
	c, cmd := command(screen(t, fixtureRows()), "add ~/a agent/a extra")
	if c.(Model).preparing || cmd == nil {
		t.Fatal("three words is a usage notice")
	}
	if msg := cmd().(nav.NoticeMsg); !strings.Contains(msg.Text, "usage: add <host-path> [<ws-path>] [--dir]") {
		t.Errorf("notice = %q", msg.Text)
	}
	c, cmd = command(screen(t, fixtureRows()), "add ~/a agent/a")
	if !c.(Model).preparing || cmd == nil {
		t.Fatal("two words start the scan")
	}
}

// TestAddWithMarksTakesOverwritten: with marks, a acts on the marked rows
// only, and only the overwritten ones can be taken.
func TestAddWithMarksTakesOverwritten(t *testing.T) {
	c := press(screen(t, fixtureRows()), "space", "down", "space") // ok + broken
	c, cmd := pressRun(c, "a")
	if c.CapturesKeys() {
		t.Fatalf("nothing marked is overwritten, so no dialog:\n%s", plain(c))
	}
	if msg := cmd().(nav.NoticeMsg); !strings.Contains(msg.Text, "none of the marked rows is drifted or untracked") {
		t.Errorf("notice = %q", msg.Text)
	}
	c = press(screen(t, fixtureRows()), "down", "down", "space") // overwritten
	c, cmd = pressRun(c, "a")
	if cmd == nil || !c.(Model).preparing {
		t.Fatal("a on a marked overwritten row should start scanning")
	}
}

// TestPrepareAddTakesTheLiveFileOfAnOverwrittenEntry runs the scan for real.
func TestPrepareAddTakesTheLiveFileOfAnOverwrittenEntry(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Workspace")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "data"))
	if err := os.MkdirAll(filepath.Join(root, "symlinks"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := wsconfig.ConfigPath(root)
	if err := wsconfig.Save(cfgPath, wsconfig.Default()); err != nil {
		t.Fatal(err)
	}
	live := filepath.Join(t.TempDir(), ".over")
	if err := os.WriteFile(live, []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := symlink.Add(symlink.AddOptions{WorkspacePath: root, ConfigPath: cfgPath, Host: live}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(live); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(live, []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	e := env.Env{Paths: workspace.Paths{Workspace: root, Config: cfgPath}}
	msg := prepareAdd(e, addRequest{paths: []string{live}})().(addPreparedMsg)
	if msg.err != nil || len(msg.candidates) != 1 || !msg.candidates[0].takeLive {
		t.Fatalf("prepared = %+v", msg)
	}
	m, _ := New(e).prepared(msg)
	if !m.modal.Open() {
		t.Fatal("no checklist")
	}
}

// TestUnlinkActsOnMarksElseCursor: ctrl+d takes the marked rows, else the
// cursor row, and always opens the checklist.
func TestUnlinkActsOnMarksElseCursor(t *testing.T) {
	rows := fixtureRows()[:3]
	c := press(screen(t, rows), "down", "ctrl+d")
	if v := plain(c); !strings.Contains(v, "Unlink 1 symlink") || !strings.Contains(v, "/h/.broken") {
		t.Errorf("cursor row only:\n%s", v)
	}
	c = press(screen(t, rows), "space", "down", "down", "space", "ctrl+d")
	v := plain(c)
	if !strings.Contains(v, "Unlink 2 symlinks") || !strings.Contains(v, "/h/.ok") || !strings.Contains(v, "/h/.over") || strings.Contains(v, "/h/.broken") {
		t.Errorf("marked rows only:\n%s", v)
	}
	if !strings.Contains(v, "[x]") {
		t.Errorf("unlink starts checked:\n%s", v)
	}
}

// TestLinkActsOnShownRows: l repairs the marked rows, else the rows the filter
// shows, and never a healthy one.
func TestLinkActsOnShownRows(t *testing.T) {
	rows := fixtureRows()[:3]
	v := plain(press(screen(t, rows), "l"))
	if !strings.Contains(v, "/h/.broken") || strings.Contains(v, "/h/.over") || strings.Contains(v, "/h/.ok") {
		t.Errorf("no marks: every missing and broken row, never a drifted one (add keeps its live file):\n%s", v)
	}
	c := press(screen(t, rows), "/", "s", "t", "a", "t", "u", "s", "=", "b", "r", "o", "k", "e", "n", "enter", "l")
	if v := plain(c); !strings.Contains(v, "/h/.broken") || strings.Contains(v, "/h/.over") {
		t.Errorf("filtered: only the shown row:\n%s", v)
	}
	c = press(screen(t, rows), "down", "space", "l")
	if v := plain(c); !strings.Contains(v, "Link /h/.broken") || strings.Contains(v, "/h/.over") {
		t.Errorf("marked: only the marked row:\n%s", v)
	}
	if c, cmd := pressRun(press(screen(t, rows), "down", "down", "space"), "l"); c.CapturesKeys() || cmd == nil {
		t.Error("a drifted row is not linked: a notice, no dialog")
	}
	c, cmd := pressRun(screen(t, rows[:1]), "l")
	if c.CapturesKeys() || cmd == nil {
		t.Error("a healthy list has nothing to link: a notice, no dialog")
	}
}

// TestSudoEntriesRunInTheTerminal: a sudo row is handed to the CLI, so sudo can
// ask for its password, instead of running inside the TUI.
func TestSudoEntriesRunInTheTerminal(t *testing.T) {
	c := press(screen(t, fixtureRows()), "down", "down", "down")
	c, cmd := pressRun(c, "ctrl+d")
	if c.CapturesKeys() || cmd == nil {
		t.Errorf("unlink of a sudo row should hand over, not open a checklist:\n%s", plain(c))
	}
	c, cmd = pressRun(press(screen(t, fixtureRows()), "down", "down", "down"), "l")
	if c.CapturesKeys() || cmd == nil {
		t.Errorf("link of a sudo row should hand over:\n%s", plain(c))
	}
}

// TestUnlinkAllStartsUnchecked: `:unlink --all` lists every symlink with
// nothing checked (spec §6.7).
func TestUnlinkAllStartsUnchecked(t *testing.T) {
	c, _ := command(screen(t, fixtureRows()[:3]), "unlink --all")
	v := plain(c)
	for _, want := range []string{"/h/.ok", "/h/.broken", "/h/.over"} {
		if !strings.Contains(v, want) {
			t.Errorf("missing %s:\n%s", want, v)
		}
	}
	if strings.Contains(v, "[x]") || !strings.Contains(v, "[ ]") {
		t.Errorf("nothing is pre-checked:\n%s", v)
	}
	c, cmd := command(screen(t, fixtureRows()[:3]), "unlink")
	if c.CapturesKeys() || cmd != nil {
		t.Errorf("`unlink` alone is not a command")
	}
}

// TestCommandsAreTheSpecList: the `:` commands are spec §6.5's, and
// nothing else: backup, edit and --to are gone (plan 0003).
func TestCommandsAreTheSpecList(t *testing.T) {
	var names []string
	for _, c := range New(env.Env{}).Commands() {
		names = append(names, c.Name)
	}
	want := []string{"add", "link", "unlink --all"}
	if strings.Join(names, "|") != strings.Join(want, "|") {
		t.Errorf("commands = %v, want %v", names, want)
	}
}

// TestCommandsRouteThroughCommandMsg: a command reaches the same function its
// key does.
func TestCommandsRouteThroughCommandMsg(t *testing.T) {
	// `add` alone is the key: the checklist scan.
	c, cmd0 := command(screen(t, fixtureRows()), "add")
	if cmd0 == nil || !c.(Model).preparing {
		t.Error(":add without paths should start the checklist scan, as the key does")
	}
	// `add` with paths scans them.
	c, cmd := command(screen(t, fixtureRows()), "add /h/.x")
	if cmd == nil || !c.(Model).preparing {
		t.Error(":add <path> should scan the path")
	}
	// `link` alone is the key: repairs the shown rows.
	c, _ = command(screen(t, fixtureRows()[:3]), "link")
	if v := plain(c); !strings.Contains(v, "Link symlinks") {
		t.Errorf(":link should open the link checklist:\n%s", v)
	}
}
