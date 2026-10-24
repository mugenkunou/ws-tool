// Package keys holds every key binding in the TUI. Bindings are declared here
// and nowhere else; the app handles Global, and each component handles its own
// map. The grammar is spec §6.3, the same on every screen:
//   - enter           open the row: drill in, view, or launch
//   - esc             one level up: closes a dialog, clears the filter, clears
//     the marks, leaves a detail view, goes to the Dashboard
//   - :               the command bar, which takes the CLI's own words
//   - /               filter the list (the CLI's --filter expressions)
//   - ?               full help · q quit (ctrl+c always quits)
//   - tab shift+tab ] [ 1-9   next / previous screen; jump to a screen
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
	Screens   []key.Binding // Screens[i] jumps to screen i
	Navigate  key.Binding   // help-only summary of Screens
	Switch    key.Binding   // help-only summary of PrevTab/NextTab/Screens
}

// ScreenDigit is the jump key for the i-th screen: 1-9, then 0 for the tenth.
func ScreenDigit(i int) string {
	if i == 9 {
		return "0"
	}
	return string(rune('1' + i))
}

// Global returns the app-level bindings for n screens (at most 10).
func Global(n int) GlobalMap {
	screens := make([]key.Binding, 0, n)
	for i := range min(n, 10) {
		screens = append(screens, key.NewBinding(key.WithKeys(ScreenDigit(i))))
	}
	return GlobalMap{
		Quit:      key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
		ForceQuit: key.NewBinding(key.WithKeys("ctrl+c")),
		Help:      key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Command:   key.NewBinding(key.WithKeys(":"), key.WithHelp(":", "command")),
		Refresh:   key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("ctrl+r", "refresh")),
		NextTab:   key.NewBinding(key.WithKeys("tab", "]"), key.WithHelp("tab", "next screen")),
		PrevTab:   key.NewBinding(key.WithKeys("shift+tab", "["), key.WithHelp("shift+tab", "prev screen")),
		Screens:   screens,
		Navigate:  key.NewBinding(key.WithKeys("1"), key.WithHelp("1-"+ScreenDigit(min(n, 10)-1), "jump to screen")),
		Switch:    key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "screens")),
	}
}

// ShortHelp implements help.KeyMap.
func (k GlobalMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Switch, k.Command, k.Help, k.Quit}
}

// FullHelp implements help.KeyMap.
func (k GlobalMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.NextTab, k.PrevTab, k.Navigate, k.Refresh}, {k.Command, k.Help, k.Quit}}
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

// ReposMap is the binding set for the repo fleet screen (spec §6.5). The
// fleet keys act on the marked repos, else every repo the filter shows.
type ReposMap struct {
	Table     table.KeyMap
	Open      key.Binding
	Fetch     key.Binding
	Pull      key.Binding
	Save      key.Binding
	Run       key.Binding
	Configure key.Binding
	AddRoot   key.Binding
	Back      key.Binding
}

// Repos returns bindings for the repo fleet screen.
func Repos() ReposMap {
	return ReposMap{
		Table:     Table(),
		Open:      key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "show")),
		Fetch:     key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "fetch")),
		Pull:      key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "pull")),
		Save:      key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "save")),
		Run:       key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "run")),
		Configure: key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "config")),
		AddRoot:   Add(),
		Back:      back(),
	}
}

// ShortHelp implements help.KeyMap. enter is left to the full help so the
// footer fits 100 columns.
func (k ReposMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Fetch, k.Pull, k.Save, k.Run, k.Configure, k.AddRoot}
}

// FullHelp implements help.KeyMap.
func (k ReposMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Table.LineUp, k.Table.LineDown, k.Table.GotoTop, k.Table.GotoBottom},
		{k.Open, k.Fetch, k.Back},
		{k.Pull, k.Save, k.Run},
		{k.Configure, k.AddRoot},
	}
}

// DotfilesMap is the binding set for the dotfiles screen.
type DotfilesMap struct {
	Table    table.KeyMap
	Add      key.Binding
	Remove   key.Binding
	Fix      key.Binding
	Sync     key.Binding
	Reset    key.Binding
	Push     key.Binding
	GitView  key.Binding
	GitSetup key.Binding
	Migrate  key.Binding
	Back     key.Binding
}

// Dotfiles returns bindings for the dotfiles screen.
func Dotfiles() DotfilesMap {
	return DotfilesMap{
		Table:    Table(),
		Add:      key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "add")),
		Remove:   keyDelete("remove"),
		Fix:      key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "fix links")),
		Sync:     key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "sync live→ws")),
		Reset:    key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "reset all")),
		Push:     key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "git push")),
		GitView:  key.NewBinding(key.WithKeys("v"), key.WithHelp("v", "git status/log")),
		GitSetup: key.NewBinding(key.WithKeys("G"), key.WithHelp("G", "git setup…")),
		Migrate:  key.NewBinding(key.WithKeys("M"), key.WithHelp("M", "migrate dirs…")),
		Back:     back(),
	}
}

// ShortHelp implements help.KeyMap.
func (k DotfilesMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Add, k.Remove, k.Fix, k.Sync, k.Push}
}

// FullHelp implements help.KeyMap.
func (k DotfilesMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Table.LineUp, k.Table.LineDown, k.Table.GotoTop, k.Table.GotoBottom},
		{k.Add, k.Remove, k.Fix, k.Sync, k.Reset},
		{k.Push, k.GitView, k.GitSetup, k.Migrate},
		{k.Back},
	}
}

// ScratchMap is the binding set for the scratch screen.
type ScratchMap struct {
	Table   table.KeyMap
	New     key.Binding
	Open    key.Binding
	Tag     key.Binding
	AutoTag key.Binding
	Search  key.Binding
	Delete  key.Binding
	Back    key.Binding
}

// Scratch returns bindings for the scratch screen.
func Scratch() ScratchMap {
	return ScratchMap{
		Table:   Table(),
		New:     key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "new")),
		Open:    key.NewBinding(key.WithKeys("enter", "o"), key.WithHelp("enter", "open in editor")),
		Tag:     key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "tag")),
		AutoTag: key.NewBinding(key.WithKeys("T"), key.WithHelp("T", "auto-tag")),
		Search:  key.NewBinding(key.WithKeys("F"), key.WithHelp("F", "find")),
		Delete:  keyDelete("delete"),
		Back:    back(),
	}
}

// ShortHelp implements help.KeyMap.
func (k ScratchMap) ShortHelp() []key.Binding {
	return []key.Binding{k.New, k.Open, k.Tag, k.Search, k.Delete}
}

// FullHelp implements help.KeyMap.
func (k ScratchMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Table.LineUp, k.Table.LineDown, k.Table.GotoTop, k.Table.GotoBottom},
		{k.New, k.Open, k.Search},
		{k.Tag, k.AutoTag, k.Delete},
		{k.Back},
	}
}

// LogsMap is the binding set for the recorded-sessions screen.
type LogsMap struct {
	Table      table.KeyMap
	Start      key.Binding
	Stop       key.Binding
	View       key.Binding
	ToggleMode key.Binding // in the session viewer: full ↔ commands only
	Search     key.Binding
	Delete     key.Binding
	Prune      key.Binding
	Back       key.Binding
}

// Logs returns bindings for the recorded-sessions screen.
func Logs() LogsMap {
	return LogsMap{
		Table:      Table(),
		Start:      key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "start recording")),
		Stop:       key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "stop recording")),
		View:       key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "view")),
		ToggleMode: key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "commands only")),
		Search:     key.NewBinding(key.WithKeys("F"), key.WithHelp("F", "find")),
		Delete:     keyDelete("delete"),
		Prune:      key.NewBinding(key.WithKeys("P"), key.WithHelp("P", "prune")),
		Back:       back(),
	}
}

// ShortHelp implements help.KeyMap.
func (k LogsMap) ShortHelp() []key.Binding {
	return []key.Binding{k.View, k.Start, k.Stop, k.Search, k.Delete, k.Prune}
}

// FullHelp implements help.KeyMap.
func (k LogsMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Table.LineUp, k.Table.LineDown, k.Table.GotoTop, k.Table.GotoBottom},
		{k.View, k.Search, k.Back},
		{k.Start, k.Stop, k.Delete, k.Prune},
	}
}

// ViewerMap is the binding set for a scrollable text viewer with a mode toggle.
type ViewerMap struct {
	Detail     DetailMap
	ToggleMode key.Binding
}

// ShortHelp implements help.KeyMap.
func (k ViewerMap) ShortHelp() []key.Binding {
	return append(k.Detail.ShortHelp(), k.ToggleMode)
}

// FullHelp implements help.KeyMap.
func (k ViewerMap) FullHelp() [][]key.Binding {
	return append(k.Detail.FullHelp(), []key.Binding{k.ToggleMode})
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
}

// Prompt returns bindings for a text prompt.
func Prompt() PromptMap {
	return PromptMap{
		Submit:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "submit")),
		Complete: key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "complete")),
		Cancel:   back(),
	}
}

// ShortHelp implements help.KeyMap.
func (k PromptMap) ShortHelp() []key.Binding { return []key.Binding{k.Submit, k.Complete, k.Cancel} }

// FullHelp implements help.KeyMap.
func (k PromptMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Submit, k.Complete, k.Cancel}}
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
