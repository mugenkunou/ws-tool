// Package setup is shown instead of the screens when the workspace is not
// initialized. It hands the terminal to the `ws init` wizard and, when that
// exits, asks the app to reload the environment.
package setup

import (
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/mugenkunou/ws-tool/internal/tui/env"
	"github.com/mugenkunou/ws-tool/internal/tui/handover"
	"github.com/mugenkunou/ws-tool/internal/tui/keys"
	"github.com/mugenkunou/ws-tool/internal/tui/nav"
	"github.com/mugenkunou/ws-tool/internal/tui/theme"
)

type handoverTag int

const handoverInit handoverTag = iota

// Model is the setup screen state.
type Model struct {
	env    env.Env
	keys   keys.SetupMap
	err    string
	width  int
	height int
}

// New creates the screen.
func New(e env.Env) Model { return Model{env: e, keys: keys.Setup()} }

// Init implements nav.Component.
func (m Model) Init() tea.Cmd { return nil }

// CapturesKeys implements nav.Component.
func (m Model) CapturesKeys() bool { return false }

// Update implements nav.Component.
func (m Model) Update(msg tea.Msg) (nav.Component, tea.Cmd) {
	switch msg := msg.(type) {
	case nav.ResizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case handover.DoneMsg:
		if _, ok := msg.Tag.(handoverTag); !ok {
			return m, nil
		}
		m.err = ""
		if msg.Err != nil {
			m.err = "ws init exited: " + msg.Err.Error()
		}
		return m, nav.ReloadEnv
	case tea.KeyPressMsg:
		if key.Matches(msg, m.keys.Init) {
			return m, handover.Run(handover.Pause(handover.WS(m.env.Paths, "init")), handoverInit)
		}
	}
	return m, nil
}

// View implements nav.Component.
func (m Model) View() string {
	lines := []string{
		theme.Title.Render("This workspace is not initialized yet."),
		"",
		theme.Muted.Render("Workspace  ") + m.env.Paths.Workspace,
		theme.Muted.Render("Config     ") + m.env.Paths.Config,
		"",
		"Press " + theme.Bold.Render("i") + " to run the " + theme.Bold.Render("ws init") + " wizard. It creates ws/manifest.json,",
		".megaignore, and the config, and offers to set up soft-delete.",
	}
	if m.err != "" {
		lines = append(lines, "", theme.Error.Render(m.err))
	}
	return lipgloss.NewStyle().Width(m.width).MaxHeight(m.height).Render(strings.Join(lines, "\n"))
}

// KeyMap implements nav.Component.
func (m Model) KeyMap() help.KeyMap { return m.keys }
