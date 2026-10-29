// Package nav defines the contract between the TUI app and its screen
// components: the typed screen identifiers, the Component interface, and the
// messages parents and children use to talk to each other.
package nav

import (
	"slices"

	"charm.land/bubbles/v2/help"
	tea "charm.land/bubbletea/v2"
)

// Screen identifies a top-level screen. Never use strings for this.
type Screen int

// Adding a screen: add its constant here and its row in specs, its
// constructor in the app's registry (internal/tui/app/registry.go), and make
// its LoadedMsg a Summary so the Dashboard has its card. Tests fail until
// every step is done (HANDBOOK.md §9.8).
const (
	ScreenDashboard Screen = iota
	ScreenRepos
	ScreenSymlinks
	ScreenScratch
	ScreenSessions
	ScreenIgnore
	ScreenSync
	ScreenSecrets
	ScreenCron
	ScreenTrash
	ScreenSkills
	ScreenWorkspace

	screenCount // sentinel; keep last
)

// Spec is what the app and the command bar know about a screen.
type Spec struct {
	Icon  string // emoji (see theme.Icon for rendering rules)
	Title string // the tab's label
	// Noun is the CLI noun the screen is named after: what `:` takes to
	// switch to it (spec §6.6). The Dashboard and Workspace have none.
	Noun string
	// Hosts are CLI nouns with no screen of their own whose `:` commands this
	// screen runs (`pass audit` on Secrets). The screen's Commands() name them
	// with the noun (`pass audit`), and they work from any screen.
	Hosts []string
}

// specs holds one row per screen; TestEveryScreenHasASpec fails on a gap.
var specs = [screenCount]Spec{
	ScreenDashboard: {Icon: "🏠", Title: "Dashboard"},
	ScreenRepos:     {Icon: "📦", Title: "Repos", Noun: "repo"},
	ScreenSymlinks:  {Icon: "🔗", Title: "Symlinks", Noun: "symlink"},
	ScreenScratch:   {Icon: "🧪", Title: "Scratch", Noun: "scratch"},
	ScreenSessions:  {Icon: "📼", Title: "Sessions", Noun: "session"},
	ScreenIgnore:    {Icon: "🧹", Title: "Ignore", Noun: "ignore"},
	ScreenSync:      {Icon: "🔄", Title: "Sync", Noun: "sync"},
	ScreenSecrets:   {Icon: "🔐", Title: "Secrets", Noun: "secret", Hosts: []string{"pass", "credential", "git-credential-helper"}},
	ScreenCron:      {Icon: "⏰", Title: "Cron", Noun: "cron"},
	ScreenTrash:     {Icon: "🚮", Title: "Trash", Noun: "trash"},
	ScreenSkills:    {Icon: "🧩", Title: "Skills", Noun: "skill"},
	ScreenWorkspace: {Icon: "🔧", Title: "Workspace",
		Hosts: []string{"config", "completion", "version", "layout", "env", "init", "tui", "help"}},
}

// Screens returns every screen in tab order.
func Screens() []Screen {
	out := make([]Screen, 0, screenCount)
	for s := range screenCount {
		out = append(out, s)
	}
	return out
}

// Spec returns the screen's row, or the zero Spec for an unknown screen.
func (s Screen) Spec() Spec {
	if s < 0 || s >= screenCount {
		return Spec{}
	}
	return specs[s]
}

// Icon is the screen's emoji (see theme.Icon for rendering rules).
func (s Screen) Icon() string { return s.Spec().Icon }

// Title is the human label for the screen's tab.
func (s Screen) Title() string {
	if t := s.Spec().Title; t != "" {
		return t
	}
	return "?"
}

// Noun is the CLI noun a screen is named after (see Spec.Noun).
func (s Screen) Noun() string { return s.Spec().Noun }

// ForNoun is the screen that owns a CLI noun: the one named after it, or the
// one that hosts it.
func ForNoun(noun string) (Screen, bool) {
	for _, s := range Screens() {
		sp := s.Spec()
		if sp.Noun == noun || slices.Contains(sp.Hosts, noun) {
			return s, true
		}
	}
	return 0, false
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

// CommandMsg carries a `:` command to a screen: the CLI words after the noun
// (`backup push` for `:backup push` on Symlinks) and the same line as typed.
type CommandMsg struct {
	Words []string
	Line  string
}

// CommandInfo describes one `:` command of a screen, for completion and help.
// Name is the words after the noun, Args their placeholder (`<dir>`).
type CommandInfo struct {
	Name string
	Args string
	Help string
}

// Commander is implemented by screens that have `:` commands. The app asks
// it to complete the command bar and to list the commands in help.
type Commander interface {
	Commands() []CommandInfo
}

// FilterMsg asks a screen to apply a filter expression, as if it had been
// typed and kept, so the Dashboard can open a screen filtered to the rows that
// need attention. The app switches to Screen and sends it the message.
type FilterMsg struct {
	Screen Screen
	Filter string
}

// OpenCommandBarMsg asks the app to open the command bar with text typed in,
// for the Dashboard's advice rows.
type OpenCommandBarMsg struct{ Prefill string }

// Severity is how urgent a Dashboard issue is.
type Severity int

const (
	SevError Severity = iota
	SevWarn
	SevInfo
)

// Issue is one Dashboard "Needs attention" row.
type Issue struct {
	Sev    Severity
	Text   string
	Screen Screen
	// Focus, when set, is sent with the switch so the screen selects the problem.
	Focus tea.Msg
	// Filter is a filter expression (`status!=ok`) the screen opens with, so
	// its own keys act on exactly the affected rows.
	Filter string
	// Advice rows are not problems: they are listed last at info level, never
	// count towards a card's status, and open the command bar with Prefill.
	Advice  bool
	Prefill string
}

// Card is a screen's Dashboard card: its body (the Dashboard shows the first
// two lines) and its attention items.
type Card struct {
	Lines  []string
	Issues []Issue
	Err    error // the load failed: the card says so and lists one error
}

// Summary is implemented by every screen's LoadedMsg, so the Dashboard can
// show a card for each screen without knowing any of them: it observes the
// broadcast load results (see the app's message routing). ok is false for a
// result that must not change the card (a second, partial load).
type Summary interface {
	Card() (s Screen, c Card, ok bool)
}
