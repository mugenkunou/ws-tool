// Package modal holds at most one open dialog (prompt, chooser, or plan
// checklist) for a screen, tagged with a step value the screen chose. The
// screen asks Match whether a message is this dialog's result and, if so,
// which step it answers; everything else it forwards with Update while Open.
//
// Typical flow inside a screen's Update:
//
//	if step, ok := m.modal.Match(msg); ok {
//		m.modal = modal.Closed()
//		switch step.(myStep) { … }
//	}
//	if m.modal.Open() {
//		m.modal, cmd = m.modal.Update(msg)
//	}
package modal

import (
	"charm.land/bubbles/v2/help"
	tea "charm.land/bubbletea/v2"

	"github.com/mugenkunou/ws-tool/internal/tui/choose"
	"github.com/mugenkunou/ws-tool/internal/tui/confirm"
	"github.com/mugenkunou/ws-tool/internal/tui/prompt"
)

type kind int

const (
	kindNone kind = iota
	kindPrompt
	kindChoose
	kindConfirm
)

// Model is the open dialog, if any.
type Model struct {
	kind    kind
	step    any
	prompt  prompt.Model
	choose  choose.Model
	confirm confirm.Model
	width   int
	height  int
}

// Closed is the no-dialog state.
func Closed() Model { return Model{} }

// Prompt opens a text prompt for step.
func (m Model) Prompt(step any, p prompt.Model) (Model, tea.Cmd) {
	n := Model{kind: kindPrompt, step: step, prompt: p.SetSize(m.width, m.height), width: m.width, height: m.height}
	return n, n.prompt.Init()
}

// Choose opens a chooser for step.
func (m Model) Choose(step any, c choose.Model) Model {
	return Model{kind: kindChoose, step: step, choose: c.SetSize(m.width, m.height), width: m.width, height: m.height}
}

// Confirm opens a plan checklist for step.
func (m Model) Confirm(step any, c confirm.Model) Model {
	return Model{kind: kindConfirm, step: step, confirm: c.SetSize(m.width, m.height), width: m.width, height: m.height}
}

// Close closes any dialog, keeping the size.
func (m Model) Close() Model { return Model{width: m.width, height: m.height} }

// Open reports whether a dialog is showing.
func (m Model) Open() bool { return m.kind != kindNone }

// SetSize fits the dialog into width×height cells.
func (m Model) SetSize(width, height int) Model {
	m.width, m.height = width, height
	m.prompt = m.prompt.SetSize(width, height)
	m.choose = m.choose.SetSize(width, height)
	m.confirm = m.confirm.SetSize(width, height)
	return m
}

// Match reports whether msg is the open dialog's result, and for which step.
func (m Model) Match(msg tea.Msg) (any, bool) {
	switch msg := msg.(type) {
	case prompt.ResultMsg:
		return m.step, m.kind == kindPrompt && msg.ID == m.prompt.ID()
	case choose.ResultMsg:
		return m.step, m.kind == kindChoose && msg.ID == m.choose.ID()
	case confirm.DoneMsg:
		return m.step, m.kind == kindConfirm && msg.ID == m.confirm.ID()
	}
	return nil, false
}

// Update forwards msg to the open dialog.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	var cmd tea.Cmd
	switch m.kind {
	case kindPrompt:
		m.prompt, cmd = m.prompt.Update(msg)
	case kindChoose:
		m.choose, cmd = m.choose.Update(msg)
	case kindConfirm:
		m.confirm, cmd = m.confirm.Update(msg)
	}
	return m, cmd
}

// View renders the open dialog.
func (m Model) View() string {
	switch m.kind {
	case kindPrompt:
		return m.prompt.View()
	case kindChoose:
		return m.choose.View()
	case kindConfirm:
		return m.confirm.View()
	}
	return ""
}

// KeyMap describes the open dialog's bindings.
func (m Model) KeyMap() help.KeyMap {
	switch m.kind {
	case kindPrompt:
		return m.prompt.KeyMap()
	case kindChoose:
		return m.choose.KeyMap()
	case kindConfirm:
		return m.confirm.KeyMap()
	}
	return nil
}
