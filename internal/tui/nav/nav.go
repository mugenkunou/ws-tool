// Package nav defines the contract between the TUI app and its screen
// components: the typed screen identifiers, the Component interface, and the
// messages parents and children use to talk to each other.
package nav

import (
	"charm.land/bubbles/v2/help"
	tea "charm.land/bubbletea/v2"
)

// Screen identifies a top-level screen. Never use strings for this.
type Screen int

const (
	ScreenDashboard Screen = iota
	ScreenRepos
	ScreenDotfiles
	ScreenScratch
	ScreenLogs
	ScreenCapture
	ScreenIgnore
	ScreenSecrets
	ScreenCron
	ScreenTrash

	screenCount // sentinel; keep last
)

// Screens returns every screen in tab order.
func Screens() []Screen {
	out := make([]Screen, 0, screenCount)
	for s := range screenCount {
		out = append(out, s)
	}
	return out
}

// Icon is the screen's emoji (see theme.Icon for rendering rules).
func (s Screen) Icon() string {
	switch s {
	case ScreenDashboard:
		return "🏠"
	case ScreenRepos:
		return "📦"
	case ScreenDotfiles:
		return "🔗"
	case ScreenScratch:
		return "🧪"
	case ScreenLogs:
		return "📼"
	case ScreenCapture:
		return "📌"
	case ScreenIgnore:
		return "🧹"
	case ScreenSecrets:
		return "🔐"
	case ScreenCron:
		return "⏰"
	case ScreenTrash:
		return "🚮"
	default:
		return ""
	}
}

// Title is the human label for the screen's tab.
func (s Screen) Title() string {
	switch s {
	case ScreenDashboard:
		return "Dashboard"
	case ScreenRepos:
		return "Repos"
	case ScreenDotfiles:
		return "Dotfiles"
	case ScreenScratch:
		return "Scratch"
	case ScreenLogs:
		return "Logs"
	case ScreenCapture:
		return "Capture"
	case ScreenIgnore:
		return "Ignore"
	case ScreenSecrets:
		return "Secrets"
	case ScreenCron:
		return "Cron"
	case ScreenTrash:
		return "Trash"
	default:
		return "?"
	}
}

// Next returns the following screen, wrapping around.
func (s Screen) Next() Screen { return (s + 1) % screenCount }

// Prev returns the preceding screen, wrapping around.
func (s Screen) Prev() Screen { return (s + screenCount - 1) % screenCount }

// Component is what every screen implements. Components own their state; the
// app only talks to them through Update and reads their View and KeyMap.
type Component interface {
	Init() tea.Cmd
	Update(tea.Msg) (Component, tea.Cmd)
	View() string
	// KeyMap describes the component's own bindings for the help bar.
	KeyMap() help.KeyMap
	// CapturesKeys reports that a modal (prompt, checklist) is open: the app
	// then sends every key to the component, except ctrl+c.
	CapturesKeys() bool
}

// ResizeMsg tells a component the size of the region it may render into.
// The app sends it to every component whenever the terminal is resized.
type ResizeMsg struct {
	Width  int
	Height int
}

// UpMsg is sent by a component that received esc while already at its top
// navigation level, asking its parent to move one level up.
type UpMsg struct{}

// GotoMsg asks the app to switch to a screen.
type GotoMsg struct{ Screen Screen }

// FrameMsg advances animations (spinners). The app broadcasts it on a timer.
type FrameMsg struct{ N int }

// ReloadEnvMsg asks the app to re-resolve the workspace environment and
// rebuild its screens — after `init` or `reset` changed whether the
// workspace is initialized.
type ReloadEnvMsg struct{}

// ReloadEnv is a command that emits ReloadEnvMsg.
func ReloadEnv() tea.Msg { return ReloadEnvMsg{} }

// RefreshMsg asks every component to reload its data.
type RefreshMsg struct{}

// Level is the severity of a notice.
type Level int

const (
	LevelInfo Level = iota
	LevelSuccess
	LevelWarn
	LevelError
)

// NoticeMsg asks the app to show a one-line message in the footer until the
// next key press.
type NoticeMsg struct {
	Text  string
	Level Level
}

// Notify returns a command that emits a NoticeMsg.
func Notify(level Level, text string) tea.Cmd {
	return func() tea.Msg { return NoticeMsg{Text: text, Level: level} }
}

// Up is a command that emits UpMsg.
func Up() tea.Msg { return UpMsg{} }

// Goto returns a command that emits GotoMsg for s.
func Goto(s Screen) tea.Cmd { return func() tea.Msg { return GotoMsg{Screen: s} } }

// Refresh is a command that emits RefreshMsg.
func Refresh() tea.Msg { return RefreshMsg{} }
