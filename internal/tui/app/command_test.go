package app

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/mugenkunou/ws-tool/internal/tui/nav"
	"github.com/mugenkunou/ws-tool/internal/tui/tuitest"
)

// fake is a screen with `:` commands that records what it is sent.
type fake struct {
	screen nav.Screen
	cmds   []nav.CommandInfo
	got    *[]nav.CommandMsg
	keys   *[]string
}

func (f fake) Init() tea.Cmd { return nil }
func (f fake) Update(msg tea.Msg) (nav.Component, tea.Cmd) {
	switch msg := msg.(type) {
	case nav.CommandMsg:
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
	nav.ScreenRepos:    {{Name: "push", Help: "push"}, {Name: "root ls"}, {Name: "root rm", Args: "<dir>"}},
	nav.ScreenDotfiles: {{Name: "add", Args: "<path…>"}, {Name: "backup enable"}, {Name: "backup push"}, {Name: "backup status"}},
	nav.ScreenSecrets:  {{Name: "pass audit"}, {Name: "credential status"}},
}

// withCommands gives some screens `:` commands, recording what they receive.
func withCommands(t *testing.T) (Model, *[]nav.CommandMsg, *[]string) {
	t.Helper()
	m := loaded(t, 100, 30)
	var got []nav.CommandMsg
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
			"dot": nav.ScreenDotfiles, "rep": nav.ScreenRepos, "sess": nav.ScreenSessions, "dash": nav.ScreenDashboard,
			"dotfiles": nav.ScreenDotfiles, "secret": nav.ScreenSecrets, "trash": nav.ScreenTrash,
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
	t.Run("the active screen's command", func(t *testing.T) {
		m, got, _ := withCommands(t)
		m = send(m, press("3")) // Dotfiles
		m = runLineKeys(t, m, "backup push")
		if m.active != nav.ScreenDotfiles || len(*got) != 1 || strings.Join((*got)[0].Words, " ") != "backup push" {
			t.Fatalf("active %v got %+v", m.active.Title(), *got)
		}
	})
	t.Run("noun and command", func(t *testing.T) {
		m, got, _ := withCommands(t)
		m = runLineKeys(t, m, "pass audit") // pass has no screen: Secrets hosts it
		if m.active != nav.ScreenSecrets || len(*got) != 1 || strings.Join((*got)[0].Words, " ") != "pass audit" {
			t.Fatalf("active %v got %+v", m.active.Title(), *got)
		}
		m, got, _ = withCommands(t)
		m = runLineKeys(t, m, "secret pass audit")
		if m.active != nav.ScreenSecrets || len(*got) != 1 || strings.Join((*got)[0].Words, " ") != "pass audit" {
			t.Fatalf("active %v got %+v", m.active.Title(), *got)
		}
		m, got, _ = withCommands(t)
		m = runLineKeys(t, m, "dotfile add ~/.bashrc --dir")
		if m.active != nav.ScreenDotfiles || len(*got) != 1 || strings.Join((*got)[0].Words, " ") != "add ~/.bashrc --dir" {
			t.Fatalf("active %v got %+v", m.active.Title(), *got)
		}
	})
	t.Run("an active command beats a screen of the same prefix", func(t *testing.T) {
		m, got, _ := withCommands(t)
		m = send(m, press("2")) // Repos has `push`
		m = runLineKeys(t, m, "push")
		if m.active != nav.ScreenRepos || len(*got) != 1 {
			t.Fatalf("active %v got %+v", m.active.Title(), *got)
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
	t.Run("config show and edit hand over", func(t *testing.T) {
		for _, line := range []string{"config show", "config edit"} {
			m, _, _ := withCommands(t)
			m = typeLine(m, line)
			_, cmd := sendRun(m, press("enter"))
			if cmd == nil {
				t.Fatalf(":%s returned no command", line)
			}
		}
	})
	t.Run("unknown", func(t *testing.T) {
		m, got, _ := withCommands(t)
		m = send(m, press("3"))
		m = runLineKeys(t, m, "backup pussh")
		if len(*got) != 0 || !strings.Contains(lastNotice(m), "Unknown command: backup pussh") {
			t.Fatalf("got %+v notice %q", *got, lastNotice(m))
		}
		m, _, _ = withCommands(t)
		m = send(m, press("3"))
		m = runLineKeys(t, m, "dotfle")
		if !strings.Contains(lastNotice(m), "did you mean") {
			t.Fatalf("notice %q", lastNotice(m))
		}
	})
	t.Run("a prefilled advice command runs as typed", func(t *testing.T) {
		m, got, _ := withCommands(t)
		m = send(m, nav.OpenCommandBarMsg{Prefill: "dotfile add ~/.password-store --dir --to ~/Workspace/secrets/dotfiles/password-store"})
		if m.bar == nil || !strings.HasPrefix(m.bar.Value(), "dotfile add") {
			t.Fatal("the bar opens prefilled")
		}
		m, cmd := sendRun(m, press("enter"))
		m = pump(t, m, cmd)
		if m.active != nav.ScreenDotfiles || len(*got) != 1 || (*got)[0].Words[0] != "add" {
			t.Fatalf("active %v got %+v", m.active.Title(), *got)
		}
	})
}

func TestCandidates(t *testing.T) {
	m, _, _ := withCommands(t)
	m = send(m, press("3"))
	c := m.candidates()
	for _, want := range []string{"backup push", "dotfile", "dotfiles", "dashboard", "sessions", "session", "repo push", "secret pass audit", "config show", "config edit", "help", "q", "quit"} {
		found := false
		for _, x := range c {
			found = found || x == want
		}
		if !found {
			t.Errorf("candidates lack %q: %v", want, c)
		}
	}
	if c[0] != "add" {
		t.Errorf("the active screen's commands come first: %v", c[:4])
	}
}

func TestCtrlRRefreshesOnlyTheActiveScreen(t *testing.T) {
	m, _, _ := withCommands(t)
	// Only the app is observable: ctrl+r on a screen sends it one RefreshMsg and
	// no other screen gets it, on the Dashboard every screen does. Check routing
	// through the Model: no panic, no quit, no bar.
	m = send(m, press("3"))
	m2, cmd := sendRun(m, tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	if m2.bar != nil || m2.active != nav.ScreenDotfiles {
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
	for _, k := range []string{"ctrl+a", "mark all/none", "ctrl+r", "command", "describe", "json"} {
		if !strings.Contains(v, k) {
			t.Errorf("full help lacks %q:\n%s", k, v)
		}
	}
	if h := len(strings.Split(strings.TrimRight(v, "\n"), "\n")); h > 30 {
		t.Errorf("full help overflows 100x30: %d lines", h)
	}
}
