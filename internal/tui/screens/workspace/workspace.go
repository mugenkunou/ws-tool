// Package workspace is the Workspace screen: ws itself: version, ws.json, completion, layout.
//
// PLACEHOLDER: replaced by the real screen in the same series of commits.
package workspace

import (
	"strings"

	"charm.land/bubbles/v2/help"
	tea "charm.land/bubbletea/v2"

	"github.com/mugenkunou/ws-tool/internal/tui/env"
	"github.com/mugenkunou/ws-tool/internal/tui/handover"
	"github.com/mugenkunou/ws-tool/internal/tui/keys"
	"github.com/mugenkunou/ws-tool/internal/tui/nav"
)

// LoadedMsg carries the screen's data.
type LoadedMsg struct{ Err error }

// Card implements nav.Summary.
func (m LoadedMsg) Card() (nav.Screen, nav.Card, bool) {
	return nav.ScreenWorkspace, nav.Card{Err: m.Err}, true
}

// Model is the screen state.
type Model struct {
	env  env.Env
	keys keys.WorkspaceMap
}

// New creates the screen.
func New(e env.Env) Model { return Model{env: e, keys: keys.Workspace()} }

// Init implements nav.Component.
func (m Model) Init() tea.Cmd { return func() tea.Msg { return LoadedMsg{} } }

// configDone tags the handover of `config edit` and `config show`, so every
// screen reloads when they return: ws.json drives them all.
type configDone struct{}

// Commands implements nav.Commander. The names carry their hosted noun.
func (m Model) Commands() []nav.CommandInfo {
	return []nav.CommandInfo{
		{Name: "config show", Help: "the resolved ws.json (paged)"},
		{Name: "config edit", Help: "edit ws.json (hands over to your editor)"},
	}
}

// Update implements nav.Component.
func (m Model) Update(msg tea.Msg) (nav.Component, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if msg.String() == "esc" {
			return m, nav.Up
		}
	case nav.CommandMsg:
		switch strings.Join(msg.Words, " ") {
		case "config show":
			return m, handover.Run(handover.Paged(handover.WS(m.env.Paths, "config", "show")), configDone{})
		case "config edit":
			return m, handover.Run(handover.WS(m.env.Paths, "config", "edit"), configDone{})
		}
	case handover.DoneMsg:
		if _, ok := msg.Tag.(configDone); ok {
			if msg.Err != nil {
				return m, nav.Notify(nav.LevelError, msg.Err.Error())
			}
			return m, func() tea.Msg { return nav.RefreshMsg{} }
		}
	}
	return m, nil
}

// View implements nav.Component.
func (m Model) View() string { return "" }

// KeyMap implements nav.Component.
func (m Model) KeyMap() help.KeyMap { return m.keys }

// CapturesKeys implements nav.Component.
func (m Model) CapturesKeys() bool { return false }
