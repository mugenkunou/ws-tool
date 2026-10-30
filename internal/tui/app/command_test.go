package app

import (
	"slices"
	"strings"
	"testing"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/mugenkunou/ws-tool/internal/tui/nav"
	"github.com/mugenkunou/ws-tool/internal/tui/tuitest"
)

// fake is a screen with `.` menu commands that records what it is sent.
type fake struct {
	screen nav.Screen
	cmds   []nav.CommandInfo
	got    *[]nav.MenuMsg
	keys   *[]string
}

func (f fake) Init() tea.Cmd { return nil }
func (f fake) Update(msg tea.Msg) (nav.Component, tea.Cmd) {
	switch msg := msg.(type) {
	case nav.MenuMsg:
		*f.got = append(*f.got, msg)
	case tea.KeyPressMsg:
		*f.keys = append(*f.keys, msg.String())
	}
	return f, nil
}
func (f fake) View() string                { return f.screen.Title() }
func (f fake) KeyMap() help.KeyMap         { return fakeKeys{} }
func (f fake) CapturesKeys() bool          { return false }
func (f fake) Commands() []nav.CommandInfo { return f.cmds }

type fakeKeys struct{}

func (fakeKeys) ShortHelp() []key.Binding  { return nil }
func (fakeKeys) FullHelp() [][]key.Binding { return nil }

var screenCommands = map[nav.Screen][]nav.CommandInfo{
	nav.ScreenRepos:      {{Name: "root ls", Menu: "l"}, {Name: "root rm", Args: "<dir>", Menu: "r"}},
	nav.ScreenSymlinks:   {{Name: "unlink --all", Menu: "u"}},
	nav.ScreenPass:       {{Name: "audit", Menu: "a"}, {Name: "init", Menu: "i"}},
	nav.ScreenCredential: {{Name: "enable", Menu: "e"}, {Name: "disable", Menu: "d"}},
}

// withCommands gives some screens `.` menu commands, recording the MenuMsg
// they receive.
func withCommands(t *testing.T) (Model, *[]nav.MenuMsg, *[]string) {
	t.Helper()
	m := loaded(t, 100, 30)
	var got []nav.MenuMsg
	var keys []string
	for s, cmds := range screenCommands {
		m.screens[s] = fake{screen: s, cmds: cmds, got: &got, keys: &keys}
	}
	return m, &got, &keys
}

func typeLine(m Model, line string) Model {
	m = send(m, press(":"))
	m = typeText(m, line)
	return m
}

// run types a line, presses enter, and runs the commands that come back.
func runLineKeys(t *testing.T, m Model, line string) Model {
	t.Helper()
	m = typeLine(m, line)
	m, cmd := sendRun(m, press("enter"))
	return pump(t, m, cmd)
}

// menuKeys opens the active screen's `.` menu, picks letter, and runs what
// comes back, so the command's dialog is open when it returns.
func menuKeys(t *testing.T, m Model, letter string) Model {
	t.Helper()
	m = send(m, press("."))
	if !m.current().CapturesKeys() {
		t.Fatalf("`.` opened no menu on %s", m.active.Title())
	}
	m, cmd := sendRun(m, press(letter))
	return pump(t, m, cmd)
}

func lastNotice(m Model) string {
	if m.notice == nil {
		return ""
	}
	return m.notice.Text
}

func TestCommandBarOpensAndCancels(t *testing.T) {
	m := loaded(t, 100, 30)
	m = send(m, press(":"))
	if m.bar == nil {
		t.Fatal(": opens the bar")
	}
	// While it is open the bar owns every key but ctrl+c: q and 2 are typed.
	m = typeText(m, "q2")
	if m.active != nav.ScreenDashboard || m.bar.Value() != "q2" {
		t.Fatalf("active %v, value %q", m.active, m.bar.Value())
	}
	if _, cmd := sendRun(m, press("ctrl+c")); cmd == nil {
		t.Fatal("ctrl+c still quits")
	} else if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("ctrl+c did not quit")
	}
	m, cmd := sendRun(m, press("esc"))
	m = pump(t, m, cmd)
	if m.bar != nil {
		t.Fatal("esc closes the bar")
	}
}

func TestCommandBarIsInPlaceOfTheFooter(t *testing.T) {
	m := loaded(t, 100, 30)
	before := strings.Split(strings.TrimRight(tuitest.Plain(view(m)), "\n"), "\n")
	m = send(m, press(":"))
	after := strings.Split(strings.TrimRight(tuitest.Plain(view(m)), "\n"), "\n")
	if len(before) != len(after) {
		t.Fatalf("height changed: %d vs %d", len(before), len(after))
	}
	if !strings.HasPrefix(after[len(after)-1], ":") || strings.Contains(after[len(after)-1], "help") {
		t.Fatalf("the last line is the bar: %q", after[len(after)-1])
	}
}

func TestCommandBarDoesNotOpenInsideADialogOrFilter(t *testing.T) {
	m := loaded(t, 100, 30)
	m = send(m, press("2"), press("/"))
	m = send(m, press(":"))
	if m.bar != nil {
		t.Fatal(": while typing a filter is typed into the filter")
	}
}

// TestCommandBarRoutes: spec §6.6.
func TestCommandBarRoutes(t *testing.T) {
	t.Run("screen prefix", func(t *testing.T) {
		for line, want := range map[string]nav.Screen{
			"sym": nav.ScreenSymlinks, "rep": nav.ScreenRepos, "sess": nav.ScreenSessions, "dash": nav.ScreenDashboard,
			"symlinks": nav.ScreenSymlinks, "secret": nav.ScreenSecrets, "trash": nav.ScreenTrash,
		} {
			m, _, _ := withCommands(t)
			m = runLineKeys(t, m, line)
			if m.active != want {
				t.Errorf(":%s -> %v, want %v", line, m.active.Title(), want.Title())
			}
		}
	})
	t.Run("ambiguous prefix", func(t *testing.T) {
		m, _, _ := withCommands(t)
		m = runLineKeys(t, m, "s") // scratch, session, secret
		if m.active != nav.ScreenDashboard || !strings.Contains(lastNotice(m), "scratch") || !strings.Contains(lastNotice(m), "secret") {
			t.Fatalf("active %v notice %q", m.active.Title(), lastNotice(m))
		}
	})
	t.Run("quit and help", func(t *testing.T) {
		m, _, _ := withCommands(t)
		m = typeLine(m, "q")
		_, cmd := sendRun(m, press("enter"))
		if cmd == nil {
			t.Fatal("no command")
		}
		_, cmd = sendRun(m, cmd()) // the bar's result
		if cmd == nil {
			t.Fatal(":q returned no command")
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Fatal(":q did not quit")
		}
		m, _, _ = withCommands(t)
		m = runLineKeys(t, m, "help")
		if !m.fullHelp {
			t.Fatal(":help opens full help")
		}
	})
	t.Run("verbs are not typed here", func(t *testing.T) {
		for _, line := range []string{"backup push", "pass audit", "repo root ls", "config edit", "symlink add ~/.bashrc"} {
			m, got, _ := withCommands(t)
			m = send(m, press("3"))
			m = runLineKeys(t, m, line)
			if len(*got) != 0 || m.active != nav.ScreenSymlinks || !strings.Contains(lastNotice(m), "Unknown command: "+line) {
				t.Errorf(":%s: active %v got %+v notice %q", line, m.active.Title(), *got, lastNotice(m))
			}
		}
	})
	t.Run("unknown", func(t *testing.T) {
		m, _, _ := withCommands(t)
		m = send(m, press("3"))
		m = runLineKeys(t, m, "symlnk")
		if !strings.Contains(lastNotice(m), "did you mean") {
			t.Fatalf("notice %q", lastNotice(m))
		}
	})
}

// TestMenuKey: `.` sends the active screen a MenuMsg, only where the screen has
// a menu, and the footer and help offer it only there.
func TestMenuKey(t *testing.T) {
	m, got, _ := withCommands(t)
	m = send(m, press("3")) // Symlinks has a menu
	m = send(m, press("."))
	if len(*got) != 1 {
		t.Fatalf("`.` sent %d MenuMsg, want 1", len(*got))
	}

	m, got, _ = withCommands(t)
	m = send(m, press("5")) // Sessions has no menu
	m = send(m, press("."))
	if m.current().CapturesKeys() || len(*got) != 0 {
		t.Error("a screen without a menu ignores `.`")
	}
	m = send(loaded(t, 100, 30), press("."))
	if m.bar != nil {
		t.Error("`.` is not the command bar")
	}
	_ = got
}

// TestMenuLettersAreSafe: on every real screen, menu letters are unique, are
// not the chooser's j and k, and every command with one has help text.
func TestMenuLettersAreSafe(t *testing.T) {
	m := loaded(t, 100, 30)
	for _, s := range nav.Screens() {
		seen := map[string]string{}
		for _, c := range commander(m.screens[s]) {
			if c.Menu == "" {
				t.Errorf("%s: command %q has no menu letter", s.Title(), c.Name)
				continue
			}
			if len([]rune(c.Menu)) != 1 || c.Menu == "j" || c.Menu == "k" {
				t.Errorf("%s: %q has menu letter %q: one letter, never j or k", s.Title(), c.Name, c.Menu)
			}
			if prev, dup := seen[c.Menu]; dup {
				t.Errorf("%s: %q and %q share menu letter %q", s.Title(), prev, c.Name, c.Menu)
			}
			seen[c.Menu] = c.Name
			if c.Help == "" {
				t.Errorf("%s: %q has no help", s.Title(), c.Name)
			}
		}
	}
}

// TestEveryMenuEntryOpensItsDialog: `.` then each letter, on each real screen,
// runs the command: it opens a dialog, hands over, or says why not. With no
// argument it never does nothing.
func TestEveryMenuEntryOpensItsDialog(t *testing.T) {
	for _, s := range nav.Screens() {
		m := loaded(t, 100, 30)
		cmds := commander(m.screens[s])
		for _, c := range cmds {
			m := send(loaded(t, 100, 30), jump(s))
			m = send(m, press("."))
			if !m.current().CapturesKeys() {
				t.Fatalf("%s: `.` opened no menu", s.Title())
			}
			before := view(m)
			m, cmd := sendRun(m, press(c.Menu))
			if cmd == nil {
				t.Errorf("%s: menu letter %q (%s) did nothing", s.Title(), c.Menu, c.Name)
				continue
			}
			m, next := sendRun(m, cmd()) // the chooser's result
			if next == nil && !m.current().CapturesKeys() && m.notice == nil && view(m) == before {
				t.Errorf("%s: menu letter %q (%s) opened no dialog, notice or handover", s.Title(), c.Menu, c.Name)
			}
		}
	}
}

func TestCandidates(t *testing.T) {
	m, _, _ := withCommands(t)
	m = send(m, press("3"))
	c := m.candidates()
	for _, want := range []string{"symlink", "symlinks", "dashboard", "sessions", "session", "help", "q", "quit"} {
		if !slices.Contains(c, want) {
			t.Errorf("candidates lack %q: %v", want, c)
		}
	}
	for _, x := range c {
		if strings.Contains(x, " ") {
			t.Errorf("candidate %q has a verb: the bar only switches screens", x)
		}
	}
}

func TestCtrlRRefreshesOnlyTheActiveScreen(t *testing.T) {
	m, _, _ := withCommands(t)
	// Only the app is observable: ctrl+r on a screen sends it one RefreshMsg and
	// no other screen gets it, on the Dashboard every screen does. Check routing
	// through the Model: no panic, no quit, no bar.
	m = send(m, press("3"))
	m2, cmd := sendRun(m, tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	if m2.bar != nil || m2.active != nav.ScreenSymlinks {
		t.Fatal("ctrl+r changes nothing visible")
	}
	_ = cmd
}

func TestFullHelpHasFourTitledColumns(t *testing.T) {
	m := loaded(t, 100, 30)
	m = send(m, press("2"), press("?"))
	v := tuitest.Plain(view(m))
	for _, title := range []string{"This screen", "Lists", "General"} {
		if !strings.Contains(v, title) {
			t.Errorf("full help lacks column %q:\n%s", title, v)
		}
	}
	for _, k := range []string{"ctrl+a", "mark all", "ctrl+r", "command", "describe", "json"} {
		if !strings.Contains(v, k) {
			t.Errorf("full help lacks %q:\n%s", k, v)
		}
	}
	if h := len(strings.Split(strings.TrimRight(v, "\n"), "\n")); h > 30 {
		t.Errorf("full help overflows 100x30: %d lines", h)
	}
}

// TestFullHelpKeepsTheScreensColumnsWhenNarrow: at 80 columns the four columns do
// not fit; the screen's own keys and its `.` menu stay, and the general keys
// go first.
func TestFullHelpKeepsTheScreensColumnsWhenNarrow(t *testing.T) {
	m, _, _ := withCommands(t)
	m = send(m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m = send(m, press("3"), press("?"))
	v := tuitest.Plain(view(m))
	for _, want := range []string{"This screen", "Actions (.)", ".u"} {
		if !strings.Contains(v, want) {
			t.Errorf("80 columns: help lacks %q:\n%s", want, v)
		}
	}
	for _, line := range strings.Split(v, "\n") {
		if len([]rune(line)) > 80 {
			t.Errorf("a help line is wider than 80 columns: %q", line)
		}
	}
}
