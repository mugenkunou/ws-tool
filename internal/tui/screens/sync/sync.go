// Package sync is the Sync screen: the mirror for folder-sync clients.
//
// PLACEHOLDER: replaced by the real screen in the same series of commits.
package sync

import (
	"charm.land/bubbles/v2/help"
	tea "charm.land/bubbletea/v2"

	"github.com/mugenkunou/ws-tool/internal/tui/env"
	"github.com/mugenkunou/ws-tool/internal/tui/keys"
	"github.com/mugenkunou/ws-tool/internal/tui/nav"
)

// LoadedMsg carries the screen's data.
type LoadedMsg struct{ Err error }

// Card implements nav.Summary.
func (m LoadedMsg) Card() (nav.Screen, nav.Card, bool) {
	return nav.ScreenSync, nav.Card{Err: m.Err}, true
}

// Model is the screen state.
type Model struct {
	env  env.Env
	keys keys.SyncMap
}

// New creates the screen.
func New(e env.Env) Model { return Model{env: e, keys: keys.Sync()} }

// Init implements nav.Component.
func (m Model) Init() tea.Cmd { return func() tea.Msg { return LoadedMsg{} } }

// Update implements nav.Component.
func (m Model) Update(msg tea.Msg) (nav.Component, tea.Cmd) {
	if _, ok := msg.(tea.KeyPressMsg); ok && msg.(tea.KeyPressMsg).String() == "esc" {
		return m, nav.Up
	}
	return m, nil
}

// View implements nav.Component.
func (m Model) View() string { return "" }

// KeyMap implements nav.Component.
func (m Model) KeyMap() help.KeyMap { return m.keys }

// CapturesKeys implements nav.Component.
func (m Model) CapturesKeys() bool { return false }
