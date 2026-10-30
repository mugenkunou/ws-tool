// Package credential is the Credential screen (spec §5 credential): the git
// credential helper's state, with the `:` commands that set it up and tear it
// down. `enable` and `disable` hand the terminal to the CLI's wizards;
// `status` pages `ws credential status`. The screen has no verb keys of its
// own.
//
// Navigation: the one list of helper facts; escape asks the app to go up.
package credential

import (
	"os/exec"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/mugenkunou/ws-tool/internal/pass"
	"github.com/mugenkunou/ws-tool/internal/resource"
	"github.com/mugenkunou/ws-tool/internal/tui/env"
	"github.com/mugenkunou/ws-tool/internal/tui/handover"
	"github.com/mugenkunou/ws-tool/internal/tui/keys"
	"github.com/mugenkunou/ws-tool/internal/tui/modal"
	"github.com/mugenkunou/ws-tool/internal/tui/nav"
	"github.com/mugenkunou/ws-tool/internal/tui/reslist"
	"github.com/mugenkunou/ws-tool/internal/tui/theme"
)

// LoadedMsg carries the git credential helper's state. Other components (the
// Dashboard) may observe it.
type LoadedMsg struct {
	Connected bool
	Helper    string
	Pass      pass.PassHealth
	Err       error
}

// fact is one row of the helper list.
type fact struct {
	Label string
	OK    bool
	Warn  bool
	Value string
}

// handoverWizard tags the handover of a credential wizard, so a refresh follows.
type handoverWizard struct{}

// Model is the Credential screen state.
type Model struct {
	env    env.Env
	keys   keys.CredentialMap
	list   reslist.Model[fact]
	modal  modal.Model
	state  LoadedMsg
	loaded bool
	width  int
	height int
}

// New creates the screen.
func New(e env.Env) Model {
	return Model{
		env:  e,
		keys: keys.Credential(),
		list: reslist.New(schema(), []reslist.Col[fact]{
			{Key: "label", Min: 24, Weight: 1},
			{Key: "value", Min: 16, Weight: 2, Style: func(_ fact, t string) string { return theme.Muted.Render(t) }},
		}, "Nothing to show.").Declare(nil),
	}
}

// schema describes the helper rows for the list and its filter and sort.
func schema() resource.Schema[fact] {
	return resource.Schema[fact]{
		Name: "fact",
		ID:   func(f fact) string { return f.Label },
		Fields: []resource.Field[fact]{
			{Key: "label", Title: "Helper", Value: func(f fact) any { return f.Label }},
			{Key: "value", Title: "Detail", Value: func(f fact) any { return f.Value },
				Text: func(f fact) string { return f.Value }},
		},
		Columns: []string{"label", "value"},
	}
}

// Load reads the git credential helper and pass store state.
func Load(e env.Env) tea.Cmd {
	return func() tea.Msg {
		m := LoadedMsg{Pass: pass.CheckPass()}
		if out, err := exec.Command("git", "config", "--global", "credential.helper").Output(); err == nil {
			m.Helper = strings.TrimSpace(string(out))
			m.Connected = m.Helper != ""
		}
		return m
	}
}

// metrics returns the facts in the order the screen shows them.
func (m LoadedMsg) facts() []fact {
	h := m.Pass
	out := []fact{{Label: "gpg available", OK: h.GPGAvailable}}
	out = append(out, fact{Label: "pass installed", OK: h.Installed})
	if h.Initialized {
		out = append(out, fact{Label: "store initialized", OK: true})
	} else {
		out = append(out, fact{Label: "store initialized"})
	}
	if m.Connected {
		out = append(out, fact{Label: "credential helper", OK: true, Value: m.Helper})
	} else {
		out = append(out, fact{Label: "credential helper", Warn: true, Value: "not connected: press . then e (enable)"})
	}
	return out
}

func (m Model) reload() (Model, tea.Cmd) {
	m.list = m.list.SetLoading()
	return m, Load(m.env)
}

// wizard hands the terminal to the CLI, then refreshes.
func (m Model) wizard(args ...string) tea.Cmd {
	return handover.Run(handover.Pause(handover.WS(m.env.Paths, args...)), handoverWizard{})
}

var commands = []reslist.Command[Model, fact]{
	{Name: "enable", Menu: "e", Help: "set up the git credential helper (hands over to the CLI)", Scope: reslist.Global,
		Do: func(m Model, _ []fact, _ []string) (Model, tea.Cmd) { return m, m.wizard("credential", "enable") }},
	{Name: "disable", Menu: "d", Help: "remove the git credential helper (hands over to the CLI)", Scope: reslist.Global,
		Do: func(m Model, _ []fact, _ []string) (Model, tea.Cmd) { return m, m.wizard("credential", "disable") }},
	{Name: "status", Menu: "s", Help: "credential helper per remote (paged)", Scope: reslist.Global,
		Do: func(m Model, _ []fact, _ []string) (Model, tea.Cmd) {
			return m, handover.Run(handover.Paged(handover.WS(m.env.Paths, "credential", "status")), handoverWizard{})
		}},
}

// Commands implements nav.Commander.
func (m Model) Commands() []nav.CommandInfo { return reslist.CommandInfos(commands) }

// Init implements nav.Component.
func (m Model) Init() tea.Cmd { return Load(m.env) }

// Declared lists the screen's actions and their scopes: none.
func (m Model) Declared() []reslist.Decl { return m.list.Declared() }

// CapturesKeys implements nav.Component.
func (m Model) CapturesKeys() bool { return m.modal.Open() || m.list.Editing() }

// Update implements nav.Component.
func (m Model) Update(msg tea.Msg) (nav.Component, tea.Cmd) {
	if _, ok := m.modal.Match(msg); ok {
		m.modal = m.modal.Close()
		next, cmd := reslist.MenuPick(m, m.list, commands, msg)
		return next, cmd
	}
	switch msg := msg.(type) {
	case nav.ResizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.list = m.list.SetSize(msg.Width, msg.Height)
		m.modal = m.modal.SetSize(msg.Width, msg.Height)
		return m, nil
	case nav.RefreshMsg:
		return m.reload()
	case LoadedMsg:
		m.loaded, m.state = true, msg
		if msg.Err != nil {
			m.list = m.list.SetError(msg.Err)
			return m, nil
		}
		m.list = m.list.SetItems(msg.facts())
		return m, nil
	case nav.MenuMsg:
		if m.modal.Open() {
			return m, nil
		}
		m.modal = m.modal.Choose("menu", reslist.MenuChooser("Credentials actions", commands))
		return m, nil
	case handover.DoneMsg:
		if _, ok := msg.Tag.(handoverWizard); ok {
			if msg.Err != nil {
				return m, nav.Notify(nav.LevelError, msg.Err.Error())
			}
			return m.reload()
		}
		return m, nil
	}

	if m.modal.Open() {
		var cmd tea.Cmd
		m.modal, cmd = m.modal.Update(msg)
		return m, cmd
	}
	if m.list.Editing() {
		var cmd tea.Cmd
		m.list, cmd = m.list.Update(msg)
		return m, cmd
	}
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	if l, cmd, ok := m.list.HandleKey(k); ok {
		m.list = l
		return m, cmd
	}
	if key.Matches(k, m.keys.Back) {
		return m, nav.Up
	}
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(k)
	return m, cmd
}

// View implements nav.Component.
func (m Model) View() string {
	if m.modal.Open() {
		return m.modal.View()
	}
	return m.list.View()
}

// KeyMap implements nav.Component.
func (m Model) KeyMap() help.KeyMap {
	if m.modal.Open() {
		return m.modal.KeyMap()
	}
	return m.list.Help(m.keys)
}
