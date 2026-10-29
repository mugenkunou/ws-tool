// Package keys holds every key binding in the TUI. Bindings are declared here
// and nowhere else; the app handles Global, and each component handles its own
// map. The grammar is spec §6.3, the same on every screen:
//   - enter           open the row: drill in, view, or launch
//   - esc             one level up: closes a dialog, clears the filter, clears
//     the marks, leaves a detail view, goes to the Dashboard
//   - :               the command bar, which takes the CLI's own words
//   - /               filter the list (the CLI's --filter expressions)
//   - ?               full help · q quit (ctrl+c always quits)
//   - tab shift+tab ] [   next / previous screen (:<screen> jumps to one)
//   - ctrl+r          refresh this screen
//   - d  y            describe the cursor row; show it as JSON
//   - e  a  ctrl+d    edit, add, delete: the same keys on every screen that has
//     the verb (screens relabel them with key.WithHelp)
//   - space ctrl+space ctrl+\ ctrl+a   marks, as in k9s: mark the cursor row
//     (it does not move), mark the range from the nearest mark to the cursor,
//     clear all marks, mark all/none. Action keys act on the marked rows when
//     there are marks (see reslist.Scope)
//   - > <             sort by the next field, reverse the sort
//   - ↑↓ j k, g G home end, pgup pgdn ctrl+b ctrl+f   move
//   - mouse           drag selects text and copies it on release; the wheel
//     scrolls like ↑↓ (WS_NO_MOUSE=1 leaves the mouse to the terminal)
//
// There are no shift+letter actions: G is the only uppercase key, and it is
// movement. Global bindings must never use keys a page needs for the grammar
// above; TestGlobalKeysLeavePageKeysAlone enforces this. Dialogs (prompts,
// checklists) capture every key but ctrl+c, so tab completes inside a prompt.
package keys

import (
	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/viewport"
)

// GlobalMap is handled by the app before keys reach the active component.
// esc is deliberately absent: it is delivered to the active component, which
// owns its navigation levels (see the package doc). While a component has a
// modal open (prompt, checklist), only ForceQuit is global.
type GlobalMap struct {
	Quit      key.Binding
	ForceQuit key.Binding
	Help      key.Binding
	Command   key.Binding // opens the command bar
	Refresh   key.Binding // refresh the active screen
	NextTab   key.Binding
	PrevTab   key.Binding
	Switch    key.Binding // help-only summary of PrevTab/NextTab
}

// Global returns the app-level bindings for n screens (at most 10).
func Global(n int) GlobalMap {
	return GlobalMap{
		Quit:      key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
		ForceQuit: key.NewBinding(key.WithKeys("ctrl+c")),
		Help:      key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Command:   key.NewBinding(key.WithKeys(":"), key.WithHelp(":", "command")),
		Refresh:   key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("ctrl+r", "refresh")),
		NextTab:   key.NewBinding(key.WithKeys("tab", "]"), key.WithHelp("tab", "next screen")),
		PrevTab:   key.NewBinding(key.WithKeys("shift+tab", "["), key.WithHelp("shift+tab", "prev screen")),
		Switch:    key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "screens")),
	}
}

// ShortHelp implements help.KeyMap.
func (k GlobalMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Refresh, k.Command, k.Help, k.Quit}
}

// FullHelp implements help.KeyMap.
func (k GlobalMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.NextTab, k.PrevTab, k.Refresh}, {k.Command, k.Help, k.Quit}}
}

// Table returns the movement bindings shared by every list screen. They are
// passed to bubbles/table so its built-in handling uses our definitions.
func Table() table.KeyMap {
	return table.KeyMap{
		LineUp:       key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		LineDown:     key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		PageUp:       key.NewBinding(key.WithKeys("pgup", "ctrl+b"), key.WithHelp("pgup", "page up")),
		PageDown:     key.NewBinding(key.WithKeys("pgdown", "ctrl+f"), key.WithHelp("pgdn", "page down")),
		HalfPageUp:   key.NewBinding(key.WithDisabled()), // ctrl+u/ctrl+d are not movement: ctrl+d deletes
		HalfPageDown: key.NewBinding(key.WithDisabled()),
		GotoTop:      key.NewBinding(key.WithKeys("home", "g"), key.WithHelp("g", "top")),
		GotoBottom:   key.NewBinding(key.WithKeys("end", "G"), key.WithHelp("G", "bottom")),
	}
}

// DashboardMap is the binding set for the dashboard: the attention list
// (↑↓) above a grid of area cards (←→↑↓).
type DashboardMap struct {
	Up     key.Binding
	Down   key.Binding
	Left   key.Binding
	Right  key.Binding
	Top    key.Binding
	Bottom key.Binding
	Open   key.Binding
}

// Dashboard returns bindings for the dashboard.
func Dashboard() DashboardMap {
	return DashboardMap{
		Up:     key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:   key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		Left:   key.NewBinding(key.WithKeys("left", "h"), key.WithHelp("←/h", "left")),
		Right:  key.NewBinding(key.WithKeys("right", "l"), key.WithHelp("→/l", "right")),
		Top:    key.NewBinding(key.WithKeys("home", "g"), key.WithHelp("g", "top")),
		Bottom: key.NewBinding(key.WithKeys("end", "G"), key.WithHelp("G", "bottom")),
		Open:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open")),
	}
}

// Arrows is a help-only summary of the four movement keys.
func (k DashboardMap) Arrows() key.Binding {
	return key.NewBinding(key.WithKeys("up"), key.WithHelp("←→↑↓", "move"))
}

// ShortHelp implements help.KeyMap.
func (k DashboardMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Arrows(), k.Open}
}

// FullHelp implements help.KeyMap.
func (k DashboardMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Up, k.Down, k.Left, k.Right}, {k.Top, k.Bottom}, {k.Open}}
}

// ReposMap is the binding set for the repo fleet screen (spec §6.5). There
// is one action key: ! opens a prompt pre-filled with `git ` for the marked
// repos, else every repo the filter shows. Erasing the `git ` runs any command.
type ReposMap struct {
	Table     table.KeyMap
	Open      key.Binding
	Git       key.Binding
	Configure key.Binding
	Back      key.Binding
}

// Repos returns bindings for the repo fleet screen.
func Repos() ReposMap {
	return ReposMap{
		Table:     Table(),
		Open:      key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "show")),
		Git:       key.NewBinding(key.WithKeys("!"), key.WithHelp("!", "git")),
		Configure: key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "config")),
		Back:      back(),
	}
}

// ShortHelp implements help.KeyMap.
func (k ReposMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Git, k.Configure, k.Open}
}

// FullHelp implements help.KeyMap.
func (k ReposMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Table.LineUp, k.Table.LineDown, k.Table.GotoTop, k.Table.GotoBottom},
		{k.Git, k.Configure},
		{k.Open, k.Back},
	}
}

// ReposDetailMap is the repo detail level: the pane scrolls, and ! and c act
// on the one repo it shows.
type ReposDetailMap struct {
	DetailMap
	Git       key.Binding
	Configure key.Binding
}

// ReposDetail returns bindings for the repo detail level.
func ReposDetail() ReposDetailMap {
	r := Repos()
	return ReposDetailMap{DetailMap: Detail(), Git: r.Git, Configure: r.Configure}
}

// ShortHelp implements help.KeyMap.
func (k ReposDetailMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Git, k.Configure, k.Viewport.Up, k.Viewport.Down, k.Back}
}

// FullHelp implements help.KeyMap.
func (k ReposDetailMap) FullHelp() [][]key.Binding {
	return append([][]key.Binding{{k.Git, k.Configure}}, k.DetailMap.FullHelp()...)
}

// SymlinksMap is the binding set for the Symlinks screen (spec §6.5).
type SymlinksMap struct {
	Table  table.KeyMap
	Add    key.Binding // a: take the marked rows' live files, else prompt for a path
	Unlink key.Binding // ctrl+d
	Link   key.Binding // l: repair the broken and overwritten links
	Back   key.Binding
}

// Symlinks returns bindings for the Symlinks screen.
func Symlinks() SymlinksMap {
	return SymlinksMap{
		Table:  Table(),
		Add:    Add(),
		Unlink: keyDelete("unlink"),
		Link:   key.NewBinding(key.WithKeys("l"), key.WithHelp("l", "link")),
		Back:   back(),
	}
}

// ShortHelp implements help.KeyMap.
func (k SymlinksMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Add, k.Unlink, k.Link}
}

// FullHelp implements help.KeyMap.
func (k SymlinksMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Table.LineUp, k.Table.LineDown, k.Table.GotoTop, k.Table.GotoBottom},
		{k.Add, k.Unlink, k.Link},
		{k.Back},
	}
}

// ScratchMap is the binding set for the scratch screen (spec §6.5).
type ScratchMap struct {
	Table  table.KeyMap
	New    key.Binding // a
	Open   key.Binding // enter: in your editor
	Tag    key.Binding // t
	Delete key.Binding // ctrl+d
	Back   key.Binding
}

// Scratch returns bindings for the scratch screen.
func Scratch() ScratchMap {
	return ScratchMap{
		Table:  Table(),
		New:    key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "new")),
		Open:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open")),
		Tag:    key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "tag")),
		Delete: Delete(),
		Back:   back(),
	}
}

// ShortHelp implements help.KeyMap.
func (k ScratchMap) ShortHelp() []key.Binding {
	return []key.Binding{k.New, k.Open, k.Tag, k.Delete}
}

// FullHelp implements help.KeyMap.
func (k ScratchMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Table.LineUp, k.Table.LineDown, k.Table.GotoTop, k.Table.GotoBottom},
		{k.New, k.Open, k.Tag, k.Delete},
		{k.Back},
	}
}

// SessionsMap is the binding set for the Sessions screen (spec §6.5), a
// client of agent-tmux: every key runs one agent-tmux verb.
type SessionsMap struct {
	Table  table.KeyMap
	Open   key.Binding // enter: attach to a live session (`open`); page an ended one (`show`)
	Show   key.Binding // v: page the recording (`show`)
	New    key.Binding // a: create a session and attach (`create`)
	Grep   key.Binding // s: search every recording (`grep`)
	Kill   key.Binding // x: end live sessions (`kill`)
	Delete key.Binding // ctrl+d: ended recordings to the trash (`rm`)
	Back   key.Binding
}

// Sessions returns bindings for the Sessions screen.
func Sessions() SessionsMap {
	return SessionsMap{
		Table:  Table(),
		Open:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open")),
		Show:   key.NewBinding(key.WithKeys("v"), key.WithHelp("v", "show")),
		New:    key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "new")),
		Grep:   key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "grep")),
		Kill:   key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "kill")),
		Delete: Delete(),
		Back:   back(),
	}
}

// ShortHelp implements help.KeyMap.
func (k SessionsMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Open, k.Show, k.New, k.Grep, k.Kill, k.Delete}
}

// FullHelp implements help.KeyMap.
func (k SessionsMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Table.LineUp, k.Table.LineDown, k.Table.GotoTop, k.Table.GotoBottom},
		{k.Open, k.Show, k.New, k.Grep, k.Kill, k.Delete},
		{k.Back},
	}
}

// SessionMatchesMap is the Sessions screen's grep results: enter pages the
// recording of the cursor match.
type SessionMatchesMap struct {
	Table table.KeyMap
	Show  key.Binding
	Back  key.Binding
}

// SessionMatches returns bindings for the grep results.
func SessionMatches() SessionMatchesMap {
	return SessionMatchesMap{
		Table: Table(),
		Show:  key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "show")),
		Back:  back(),
	}
}

// ShortHelp implements help.KeyMap.
func (k SessionMatchesMap) ShortHelp() []key.Binding { return []key.Binding{k.Show, k.Back} }

// FullHelp implements help.KeyMap.
func (k SessionMatchesMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Table.LineUp, k.Table.LineDown, k.Table.GotoTop, k.Table.GotoBottom},
		{k.Show, k.Back},
	}
}

// IgnoreView is one of the Ignore screen's three views (spec §6.5), cycled
// by NextView.
type IgnoreView int

const (
	IgnoreWarnings IgnoreView = iota // `ignore lint`: where the screen opens
	IgnoreMuted                      // `ignore ls --muted`
	IgnoreIgnored                    // `ignore ls`
)

// IgnoreMap is the binding set for the ignore screen. Which keys a view
// offers is in For; the map itself lists every key of every view.
type IgnoreMap struct {
	Table        table.KeyMap
	Ignore       key.Binding // Warnings, Muted: stop syncing the path
	Mute         key.Binding // Warnings: hide the warning, the path keeps syncing
	Scratch      key.Binding // Warnings: move to scratch
	Delete       key.Binding // Warnings: delete the path (to trash)
	Unmute       key.Binding // Muted
	StopIgnoring key.Binding // Ignored: `ignore rm`, sync it again
	EditRules    key.Binding // any view
	NextView     key.Binding // any view
	Back         key.Binding
}

// Ignore returns bindings for the ignore screen.
func Ignore() IgnoreMap {
	del := Delete()
	del.SetHelp("ctrl+d", "delete")
	stop := Delete()
	stop.SetHelp("ctrl+d", "stop ignoring")
	edit := Edit()
	edit.SetHelp("e", "edit rules")
	return IgnoreMap{
		Table:        Table(),
		Ignore:       key.NewBinding(key.WithKeys("i"), key.WithHelp("i", "ignore")),
		Mute:         key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "mute")),
		Scratch:      key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "scratch")),
		Delete:       del,
		Unmute:       key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "unmute")),
		StopIgnoring: stop,
		EditRules:    edit,
		NextView:     key.NewBinding(key.WithKeys("v"), key.WithHelp("v", "next view")),
		Back:         back(),
	}
}

// viewKeys returns the screen keys of one view, in footer order.
func (k IgnoreMap) viewKeys(v IgnoreView) []key.Binding {
	switch v {
	case IgnoreMuted:
		return []key.Binding{k.Ignore, k.Unmute, k.EditRules, k.NextView}
	case IgnoreIgnored:
		return []key.Binding{k.StopIgnoring, k.EditRules, k.NextView}
	}
	return []key.Binding{k.Ignore, k.Mute, k.Scratch, k.Delete, k.EditRules, k.NextView}
}

// For is the help of one view: its keys and no other view's.
func (k IgnoreMap) For(v IgnoreView) help.KeyMap { return ignoreViewHelp{k, v} }

type ignoreViewHelp struct {
	k IgnoreMap
	v IgnoreView
}

func (h ignoreViewHelp) ShortHelp() []key.Binding { return h.k.viewKeys(h.v) }

func (h ignoreViewHelp) FullHelp() [][]key.Binding {
	t := h.k.Table
	return [][]key.Binding{{t.LineUp, t.LineDown, t.GotoTop, t.GotoBottom}, h.k.viewKeys(h.v)}
}

// ShortHelp implements help.KeyMap: the Warnings view.
func (k IgnoreMap) ShortHelp() []key.Binding { return k.viewKeys(IgnoreWarnings) }

// FullHelp implements help.KeyMap: every key of every view.
func (k IgnoreMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Table.LineUp, k.Table.LineDown, k.Table.GotoTop, k.Table.GotoBottom},
		{k.Ignore, k.Mute, k.Scratch, k.Delete, k.Unmute, k.StopIgnoring, k.EditRules, k.NextView},
	}
}

// SecretsMap is the binding set for the secrets screen (spec §6.5). `a` is
// allow here: the one screen where a global letter takes the screen's own verb.
type SecretsMap struct {
	Table  table.KeyMap
	View   key.Binding // enter: the finding in context (cursor)
	Allow  key.Binding // a: allow, a false positive (selected)
	Ignore key.Binding // i: ignore the file (selected)
	Mute   key.Binding // m: mute a directory (cursor)
	Store  key.Binding // s: store the value in pass (cursor)
	Back   key.Binding
}

// Secrets returns bindings for the secrets screen.
func Secrets() SecretsMap {
	return SecretsMap{
		Table:  Table(),
		View:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "show")),
		Allow:  key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "allow")),
		Ignore: key.NewBinding(key.WithKeys("i"), key.WithHelp("i", "ignore")),
		Mute:   key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "mute")),
		Store:  key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "store")),
		Back:   back(),
	}
}

// ShortHelp implements help.KeyMap.
func (k SecretsMap) ShortHelp() []key.Binding {
	return []key.Binding{k.View, k.Allow, k.Ignore, k.Mute, k.Store}
}

// FullHelp implements help.KeyMap.
func (k SecretsMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Table.LineUp, k.Table.LineDown, k.View, k.Back},
		{k.Allow, k.Ignore, k.Mute, k.Store},
	}
}

// CronMap is the binding set for the cron screen (spec §6.5).
type CronMap struct {
	Table  table.KeyMap
	Output key.Binding // enter: the job's recent output
	Add    key.Binding
	Remove key.Binding
	Run    key.Binding
	Back   key.Binding
}

// Cron returns bindings for the cron screen.
func Cron() CronMap {
	add := Add()
	rm := Delete()
	rm.SetHelp("ctrl+d", "remove")
	return CronMap{
		Table:  Table(),
		Output: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "output")),
		Add:    add,
		Remove: rm,
		Run:    key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "run")),
		Back:   back(),
	}
}

// ShortHelp implements help.KeyMap.
func (k CronMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Output, k.Add, k.Remove, k.Run}
}

// FullHelp implements help.KeyMap.
func (k CronMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Table.LineUp, k.Table.LineDown, k.Table.GotoTop, k.Table.GotoBottom},
		k.ShortHelp(),
	}
}

// TrashMap is the binding set for the trash screen (spec §6.5).
type TrashMap struct {
	Table   table.KeyMap
	Restore key.Binding
	Delete  key.Binding
	Prune   key.Binding
	Back    key.Binding
}

// Trash returns bindings for the trash screen.
func Trash() TrashMap {
	del := Delete()
	del.SetHelp("ctrl+d", "delete forever")
	return TrashMap{
		Table:   Table(),
		Restore: key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "restore")),
		Delete:  del,
		Prune:   key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "prune")),
		Back:    back(),
	}
}

// ShortHelp implements help.KeyMap.
func (k TrashMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Restore, k.Delete, k.Prune}
}

// FullHelp implements help.KeyMap.
func (k TrashMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Table.LineUp, k.Table.LineDown, k.Table.GotoTop, k.Table.GotoBottom},
		k.ShortHelp(),
	}
}

// SkillsMap is the binding set for the skills screen (spec §6.5).
type SkillsMap struct {
	Table     table.KeyMap
	Install   key.Binding
	Uninstall key.Binding
	Back      key.Binding
}

// Skills returns bindings for the skills screen.
func Skills() SkillsMap {
	return SkillsMap{
		Table:     Table(),
		Install:   key.NewBinding(key.WithKeys("i"), key.WithHelp("i", "install")),
		Uninstall: key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "uninstall")),
		Back:      back(),
	}
}

// ShortHelp implements help.KeyMap.
func (k SkillsMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Install, k.Uninstall}
}

// FullHelp implements help.KeyMap.
func (k SkillsMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Table.LineUp, k.Table.LineDown, k.Table.GotoTop, k.Table.GotoBottom},
		k.ShortHelp(),
	}
}

// SetupMap is the binding set for the not-initialized setup screen.
type SetupMap struct {
	Init key.Binding
}

// Setup returns bindings for the setup screen.
func Setup() SetupMap {
	return SetupMap{
		Init: key.NewBinding(key.WithKeys("i", "enter"), key.WithHelp("i", "initialize workspace")),
	}
}

// ShortHelp implements help.KeyMap.
func (k SetupMap) ShortHelp() []key.Binding { return []key.Binding{k.Init} }

// FullHelp implements help.KeyMap.
func (k SetupMap) FullHelp() [][]key.Binding { return [][]key.Binding{{k.Init}} }

// DetailMap is the binding set for a scrollable detail pane. Viewport is
// passed to bubbles/viewport so its built-in scrolling uses our definitions.
type DetailMap struct {
	Viewport viewport.KeyMap
	Back     key.Binding
}

// Detail returns bindings for a detail pane.
func Detail() DetailMap {
	return DetailMap{
		Viewport: viewport.KeyMap{
			Up:           key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "scroll up")),
			Down:         key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "scroll down")),
			PageUp:       key.NewBinding(key.WithKeys("pgup", "ctrl+b"), key.WithHelp("pgup", "page up")),
			PageDown:     key.NewBinding(key.WithKeys("pgdown", "ctrl+f"), key.WithHelp("pgdn", "page down")),
			HalfPageUp:   key.NewBinding(key.WithDisabled()),
			HalfPageDown: key.NewBinding(key.WithDisabled()),
			Left:         key.NewBinding(key.WithDisabled()),
			Right:        key.NewBinding(key.WithDisabled()),
		},
		Back: back(),
	}
}

// ShortHelp implements help.KeyMap.
func (k DetailMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Viewport.Up, k.Viewport.Down, k.Back}
}

// FullHelp implements help.KeyMap.
func (k DetailMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Viewport.Up, k.Viewport.Down, k.Viewport.PageUp, k.Viewport.PageDown},
		{k.Back},
	}
}

// ConfirmMap is the binding set for the plan checklist (confirm component).
type ConfirmMap struct {
	Up     key.Binding
	Down   key.Binding
	Toggle key.Binding
	All    key.Binding
	Apply  key.Binding
	Close  key.Binding // after the plan ran
	Cancel key.Binding
}

// Confirm returns bindings for the plan checklist.
func Confirm() ConfirmMap {
	return ConfirmMap{
		Up:     key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:   key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		Toggle: key.NewBinding(key.WithKeys("space"), key.WithHelp("space", "toggle")),
		All:    key.NewBinding(key.WithKeys("ctrl+a"), key.WithHelp("ctrl+a", "all/none")),
		Apply:  key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "apply")),
		Close:  key.NewBinding(key.WithKeys("enter", "esc"), key.WithHelp("enter", "close")),
		Cancel: back(),
	}
}

// ShortHelp implements help.KeyMap.
func (k ConfirmMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Toggle, k.All, k.Apply, k.Cancel}
}

// FullHelp implements help.KeyMap.
func (k ConfirmMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Up, k.Down}, {k.Toggle, k.All}, {k.Apply, k.Cancel}}
}

// PromptMap is the binding set for a text prompt. Typing goes to the input.
type PromptMap struct {
	Submit   key.Binding
	Complete key.Binding
	Cancel   key.Binding
	// Older and Newer step through the prompt's history. They are disabled
	// unless the prompt has one.
	Older key.Binding
	Newer key.Binding
}

// Prompt returns bindings for a text prompt.
func Prompt() PromptMap {
	return PromptMap{
		Submit:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "submit")),
		Complete: key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "complete")),
		Cancel:   back(),
		Older:    key.NewBinding(key.WithKeys("up"), key.WithHelp("↑↓", "history"), key.WithDisabled()),
		Newer:    key.NewBinding(key.WithKeys("down"), key.WithDisabled()),
	}
}

// ShortHelp implements help.KeyMap.
func (k PromptMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Submit, k.Complete, k.Older, k.Cancel}
}

// FullHelp implements help.KeyMap.
func (k PromptMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Submit, k.Complete, k.Older, k.Cancel}}
}

// ChooseMap is the binding set for a single-choice list.
type ChooseMap struct {
	Up     key.Binding
	Down   key.Binding
	Choose key.Binding
	Cancel key.Binding
}

// Choose returns bindings for a single-choice list.
func Choose() ChooseMap {
	return ChooseMap{
		Up:     key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:   key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		Choose: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "choose")),
		Cancel: back(),
	}
}

// ShortHelp implements help.KeyMap.
func (k ChooseMap) ShortHelp() []key.Binding { return []key.Binding{k.Up, k.Down, k.Choose, k.Cancel} }

// FullHelp implements help.KeyMap.
func (k ChooseMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Up, k.Down}, {k.Choose, k.Cancel}}
}

// ListMap is the binding set every resource list shares (reslist): filter,
// sort, marking, and the all-fields detail pane. Screens must not reuse these
// keys for their own actions.
type ListMap struct {
	Filter    key.Binding
	Sort      key.Binding
	Reverse   key.Binding
	Mark      key.Binding
	MarkRange key.Binding // k9s: from the nearest mark to the cursor
	MarkClear key.Binding
	MarkAll   key.Binding
	Describe  key.Binding // every field of the cursor row
	JSON      key.Binding // the cursor row as `ws <noun> ls --json` prints it
	Clear     key.Binding // esc while a filter or marks are active
	Apply     key.Binding // while typing a filter
	Cancel    key.Binding // while typing a filter
}

// List returns bindings shared by every resource list.
func List() ListMap {
	return ListMap{
		Filter:    key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
		Sort:      key.NewBinding(key.WithKeys(">"), key.WithHelp(">", "sort by next field")),
		Reverse:   key.NewBinding(key.WithKeys("<"), key.WithHelp("<", "reverse sort")),
		Mark:      key.NewBinding(key.WithKeys("space"), key.WithHelp("space", "mark")),
		MarkRange: key.NewBinding(key.WithKeys("ctrl+space"), key.WithHelp("ctrl+space", "mark range")),
		MarkClear: key.NewBinding(key.WithKeys("ctrl+\\"), key.WithHelp("ctrl+\\", "clear marks")),
		MarkAll:   key.NewBinding(key.WithKeys("ctrl+a"), key.WithHelp("ctrl+a", "mark all/none")),
		Describe:  key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "describe")),
		JSON:      key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "json")),
		Clear:     key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "clear filter/marks")),
		Apply:     key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "keep filter")),
		Cancel:    key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
	}
}

// Bindings lists the list bindings shown in help.
func (k ListMap) Bindings() []key.Binding {
	return []key.Binding{k.Filter, k.Sort, k.Reverse, k.Describe, k.JSON, k.Mark, k.MarkRange, k.MarkClear, k.MarkAll}
}

// FilterMap is the help shown while a filter is being typed.
type FilterMap struct{ List ListMap }

// ShortHelp implements help.KeyMap.
func (k FilterMap) ShortHelp() []key.Binding { return []key.Binding{k.List.Apply, k.List.Cancel} }

// FullHelp implements help.KeyMap.
func (k FilterMap) FullHelp() [][]key.Binding { return [][]key.Binding{k.ShortHelp()} }

// WithList adds the shared list bindings to a screen's help: the mark keys
// after the screen's own in the short help, and a column with all of them in
// the full help.
func WithList(m help.KeyMap, l ListMap) help.KeyMap { return withList{m, l} }

type withList struct {
	help.KeyMap
	list ListMap
}

func (w withList) ShortHelp() []key.Binding {
	return append(append([]key.Binding{}, w.KeyMap.ShortHelp()...), w.list.Mark, w.list.MarkRange)
}

func (w withList) FullHelp() [][]key.Binding {
	return append(append([][]key.Binding{}, w.KeyMap.FullHelp()...), w.list.Bindings())
}

func keyDelete(desc string) key.Binding {
	return key.NewBinding(key.WithKeys("ctrl+d"), key.WithHelp("ctrl+d", desc))
}

// Add, Edit and Delete are the verbs every screen binds the same way (spec
// §6.3). A screen relabels them with key.WithHelp, for example
// `a` "add", `ctrl+d` "unlink".
func Add() key.Binding { return key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "add")) }

// Edit is `e`.
func Edit() key.Binding { return key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit")) }

// Delete is `ctrl+d`.
func Delete() key.Binding {
	return key.NewBinding(key.WithKeys("ctrl+d"), key.WithHelp("ctrl+d", "delete"))
}

func back() key.Binding {
	return key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back"))
}

// SyncMap is the binding set for the Sync screen.
type SyncMap struct{}

// Sync returns the Sync screen bindings.
func Sync() SyncMap { return SyncMap{} }

// ShortHelp implements help.KeyMap.
func (k SyncMap) ShortHelp() []key.Binding { return nil }

// FullHelp implements help.KeyMap.
func (k SyncMap) FullHelp() [][]key.Binding { return nil }

// WorkspaceMap is the binding set for the Workspace screen.
type WorkspaceMap struct{}

// Workspace returns the Workspace screen bindings.
func Workspace() WorkspaceMap { return WorkspaceMap{} }

// ShortHelp implements help.KeyMap.
func (k WorkspaceMap) ShortHelp() []key.Binding { return nil }

// FullHelp implements help.KeyMap.
func (k WorkspaceMap) FullHelp() [][]key.Binding { return nil }
