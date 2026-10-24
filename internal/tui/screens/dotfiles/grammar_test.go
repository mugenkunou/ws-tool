package dotfiles

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/mugenkunou/ws-tool/internal/dotfile"
	"github.com/mugenkunou/ws-tool/internal/tui/env"
	"github.com/mugenkunou/ws-tool/internal/tui/keys"
	"github.com/mugenkunou/ws-tool/internal/tui/nav"
	"github.com/mugenkunou/ws-tool/internal/tui/reslist"
	"github.com/mugenkunou/ws-tool/internal/workspace"
	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

// The grammar of spec §6.5: keys, the rows each acts on, `:` commands.

// fixtureRows: healthy, broken, overwritten, and an overwritten sudo entry.
func fixtureRows() []Entry {
	return []Entry{
		{Link: wsconfig.Link{System: "/h/.ok", Original: "configs/dotfiles/ok"}, WorkspacePath: "/ws/configs/dotfiles/ok"},
		{Link: wsconfig.Link{System: "/h/.broken", Original: "configs/dotfiles/broken"}, WorkspacePath: "/ws/configs/dotfiles/broken", Status: dotfile.StatusBroken},
		{Link: wsconfig.Link{System: "/h/.over", Original: "configs/dotfiles/over"}, WorkspacePath: "/ws/configs/dotfiles/over", Status: dotfile.StatusOverwritten},
		{Link: wsconfig.Link{System: "/etc/hosts", Original: "configs/dotfiles/etc/hosts", Sudo: true}, WorkspacePath: "/ws/configs/dotfiles/etc/hosts", Status: dotfile.StatusOverwritten},
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
	for _, b := range keys.Dotfiles().ShortHelp() {
		got = append(got, b.Help().Key+" "+b.Help().Desc)
	}
	want := []string{"a add", "ctrl+d unlink", "l link", "e edit", "b back up"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("keys = %v, want %v", got, want)
	}
}

// TestEveryActionDeclaresItsScope: each key acts on the rows spec §6.5 says.
func TestEveryActionDeclaresItsScope(t *testing.T) {
	want := map[string]reslist.Scope{
		"a": reslist.Marked, "ctrl+d": reslist.Selected, "l": reslist.All, "e": reslist.Cursor, "b": reslist.Global,
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
	src, err := os.ReadFile("dotfiles.go")
	if err != nil {
		t.Fatal(err)
	}
	if re := regexp.MustCompile(`"[^"]*\b[Pp]ress [A-Za-z]\b`); re.Match(src) {
		t.Errorf("dotfiles.go names a key in text: %s", re.Find(src))
	}
	v := plain(screen(t, nil))
	if want := "Press " + keys.Dotfiles().Add.Help().Key + " to add one."; !strings.Contains(v, want) {
		t.Errorf("empty list lacks %q:\n%s", want, v)
	}
}

// TestAddPromptPrefill: without marks, a prompts for a path, pre-filled with
// the cursor row's path when that row is overwritten.
func TestAddPromptPrefill(t *testing.T) {
	c := press(screen(t, fixtureRows()), "a")
	if !c.CapturesKeys() || !strings.Contains(plain(c), "~/") {
		t.Fatalf("a on a healthy row should prompt with ~/:\n%s", plain(c))
	}
	c = press(screen(t, fixtureRows()), "down", "down", "a")
	if v := plain(c); !strings.Contains(v, "/h/.over") {
		t.Errorf("a on an overwritten row should be pre-filled with its path:\n%s", v)
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
	if msg := cmd().(nav.NoticeMsg); !strings.Contains(msg.Text, "none of the marked dotfiles was overwritten") {
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
	if err := os.MkdirAll(filepath.Join(root, "configs/dotfiles"), 0o755); err != nil {
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
	if _, err := dotfile.Add(dotfile.AddOptions{WorkspacePath: root, ConfigPath: cfgPath, SystemPath: live}); err != nil {
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
	if v := plain(c); !strings.Contains(v, "Unlink 1 dotfile") || !strings.Contains(v, "/h/.broken") {
		t.Errorf("cursor row only:\n%s", v)
	}
	c = press(screen(t, rows), "space", "down", "down", "space", "ctrl+d")
	v := plain(c)
	if !strings.Contains(v, "Unlink 2 dotfiles") || !strings.Contains(v, "/h/.ok") || !strings.Contains(v, "/h/.over") || strings.Contains(v, "/h/.broken") {
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
	if !strings.Contains(v, "/h/.broken") || !strings.Contains(v, "/h/.over") || strings.Contains(v, "/h/.ok") {
		t.Errorf("no marks: every broken and overwritten row:\n%s", v)
	}
	c := press(screen(t, rows), "/", "s", "t", "a", "t", "u", "s", "=", "b", "r", "o", "k", "e", "n", "enter", "l")
	if v := plain(c); !strings.Contains(v, "/h/.broken") || strings.Contains(v, "/h/.over") {
		t.Errorf("filtered: only the shown row:\n%s", v)
	}
	c = press(screen(t, rows), "down", "down", "space", "l")
	if v := plain(c); !strings.Contains(v, "/h/.over") || strings.Contains(v, "/h/.broken") {
		t.Errorf("marked: only the marked row:\n%s", v)
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

// TestUnlinkAllStartsUnchecked: `:unlink --all` lists every dotfile with
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

// TestCommandsAreTheSpecList: the `:` commands are spec §6.5's, plus
// `backup push` (the key b's command, §6.6).
func TestCommandsAreTheSpecList(t *testing.T) {
	var names []string
	for _, c := range New(env.Env{}).Commands() {
		names = append(names, c.Name)
	}
	want := []string{"add", "link", "unlink --all", "backup enable", "backup disable", "backup status", "backup push"}
	if strings.Join(names, "|") != strings.Join(want, "|") {
		t.Errorf("commands = %v, want %v", names, want)
	}
}

// TestCommandsRouteThroughCommandMsg: a command reaches the same function its
// key does.
func TestCommandsRouteThroughCommandMsg(t *testing.T) {
	// `add` alone is the key: a prompt.
	c, _ := command(screen(t, fixtureRows()), "add")
	if !c.CapturesKeys() {
		t.Error(":add without paths should open the prompt, as the key does")
	}
	// `add` with paths scans them.
	c, cmd := command(screen(t, fixtureRows()), "add /h/.x --to /ws/configs/dotfiles/x")
	if cmd == nil || !c.(Model).preparing {
		t.Error(":add <path> should scan the path")
	}
	// `link` alone is the key: repairs the shown rows.
	c, _ = command(screen(t, fixtureRows()[:3]), "link")
	if v := plain(c); !strings.Contains(v, "Link dotfiles") {
		t.Errorf(":link should open the link checklist:\n%s", v)
	}
	// `backup enable` hands the terminal to the CLI wizard.
	c, cmd = command(screen(t, nil), "backup enable")
	if cmd == nil || c.CapturesKeys() {
		t.Error(":backup enable should be handed over")
	}
	// `backup status` is a view; esc returns.
	c, cmd = command(screen(t, nil), "backup status")
	if cmd == nil || c.(Model).level != levelBackup {
		t.Fatal(":backup status should open its view")
	}
	c = press(c, "esc")
	if c.(Model).level != levelList {
		t.Error("esc should return to the list")
	}
}

// TestBackupStatusViewReadsTheConfig: with backup disabled the view says so and
// names the command that enables it.
func TestBackupStatusViewReadsTheConfig(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Workspace")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := wsconfig.ConfigPath(root)
	if err := wsconfig.Save(cfgPath, wsconfig.Default()); err != nil {
		t.Fatal(err)
	}
	e := env.Env{Paths: workspace.Paths{Workspace: root, Config: cfgPath}}
	text := ansi.Strip(loadBackupView(e)().(backupViewMsg).text)
	if !strings.Contains(text, "disabled") || !strings.Contains(text, ":backup enable") {
		t.Errorf("view = %q", text)
	}
}

// TestEditOpensTheCursorRowsCopy: e acts on the cursor row only, and says when
// the workspace copy is missing.
func TestEditOpensTheCursorRowsCopy(t *testing.T) {
	c := press(screen(t, fixtureRows()), "down")
	_, cmd := pressRun(c, "e")
	msg, ok := cmd().(nav.NoticeMsg)
	if !ok || !strings.Contains(msg.Text, "/ws/configs/dotfiles/broken") {
		t.Errorf("a missing copy is reported for the cursor row: %#v", cmd())
	}
}
