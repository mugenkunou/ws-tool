// Package keys holds every key binding in the TUI. Bindings are declared here
// and nowhere else; the app handles Global, and each component handles its own
// map.
//
// Key grammar (the same on every screen):
//   - ←→↑↓ / h j k l  move within the page (lists: ↑↓; grids: all four)
//   - enter           open / drill in — the only key that does
//   - esc             up one level: closes a dialog, leaves a detail view,
//     returns to the dashboard; does nothing at the dashboard
//   - tab / shift+tab next / previous screen (global; also ] [ and 1-9)
//   - r               reload the current screen
//   - ?               full help · q quit (ctrl+c always quits)
//   - mouse           drag selects text and copies it on release; the wheel
//     scrolls like ↑↓ (WS_NO_MOUSE=1 leaves the mouse to the terminal)
//
// Global bindings must never use keys a page needs for the grammar above
// (arrows, hjkl, enter, esc, space); TestGlobalKeysLeavePageKeysAlone
// enforces this. Dialogs (prompts, checklists) capture every key but ctrl+c,
// so tab completes inside a prompt.
package keys

import (
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
		NextTab:   key.NewBinding(key.WithKeys("tab", "]"), key.WithHelp("tab", "next screen")),
		PrevTab:   key.NewBinding(key.WithKeys("shift+tab", "["), key.WithHelp("shift+tab", "prev screen")),
		Screens:   screens,
		Navigate:  key.NewBinding(key.WithKeys("1"), key.WithHelp("1-"+ScreenDigit(min(n, 10)-1), "jump to screen")),
		Switch:    key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab/⇧tab", "screens")),
	}
}

// ShortHelp implements help.KeyMap.
func (k GlobalMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Switch, k.Help, k.Quit}
}

// FullHelp implements help.KeyMap.
func (k GlobalMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.NextTab, k.PrevTab, k.Navigate}, {k.Help, k.Quit}}
}

// Table returns the movement bindings shared by every list screen. They are
// passed to bubbles/table so its built-in handling uses our definitions.
func Table() table.KeyMap {
	return table.KeyMap{
		LineUp:       key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		LineDown:     key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		PageUp:       key.NewBinding(key.WithKeys("pgup"), key.WithHelp("pgup", "page up")),
		PageDown:     key.NewBinding(key.WithKeys("pgdown"), key.WithHelp("pgdn", "page down")),
		HalfPageUp:   key.NewBinding(key.WithKeys("ctrl+u"), key.WithHelp("ctrl+u", "½ page up")),
		HalfPageDown: key.NewBinding(key.WithKeys("ctrl+d"), key.WithHelp("ctrl+d", "½ page down")),
		GotoTop:      key.NewBinding(key.WithKeys("home", "g"), key.WithHelp("g", "top")),
		GotoBottom:   key.NewBinding(key.WithKeys("end", "G"), key.WithHelp("G", "bottom")),
	}
}

// DashboardMap is the binding set for the dashboard: the attention list
// (↑↓) above a grid of area cards (←→↑↓).
type DashboardMap struct {
	Up      key.Binding
	Down    key.Binding
	Left    key.Binding
	Right   key.Binding
	Top     key.Binding
	Bottom  key.Binding
	Open    key.Binding
	Fix     key.Binding
	Refresh key.Binding
	Config  key.Binding
	Restore key.Binding
	Reset   key.Binding
}

// Dashboard returns bindings for the dashboard.
func Dashboard() DashboardMap {
	return DashboardMap{
		Up:      key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:    key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		Left:    key.NewBinding(key.WithKeys("left", "h"), key.WithHelp("←/h", "left")),
		Right:   key.NewBinding(key.WithKeys("right", "l"), key.WithHelp("→/l", "right")),
		Top:     key.NewBinding(key.WithKeys("home", "g"), key.WithHelp("g", "top")),
		Bottom:  key.NewBinding(key.WithKeys("end", "G"), key.WithHelp("G", "bottom")),
		Open:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open")),
		Fix:     key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "quick fix")),
		Refresh: key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh all")),
		Config:  key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "view config…")),
		Restore: key.NewBinding(key.WithKeys("O"), key.WithHelp("O", "restore wizard…")),
		Reset:   key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "reset workspace")),
	}
}

// Arrows is a help-only summary of the four movement keys.
func (k DashboardMap) Arrows() key.Binding {
	return key.NewBinding(key.WithKeys("up"), key.WithHelp("←→↑↓", "move"))
}

// ShortHelp implements help.KeyMap.
func (k DashboardMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Arrows(), k.Open, k.Fix, k.Refresh}
}

// FullHelp implements help.KeyMap.
func (k DashboardMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Up, k.Down, k.Left, k.Right}, {k.Top, k.Bottom}, {k.Open, k.Fix, k.Refresh}, {k.Config, k.Restore, k.Reset}}
}

// ReposMap is the binding set for the repo fleet screen. Fleet actions
// open a checklist covering every repo, which can then be narrowed.
type ReposMap struct {
	Table      table.KeyMap
	Open       key.Binding
	Fetch      key.Binding
	Pull       key.Binding
	PullRebase key.Binding
	Sync       key.Binding
	SyncRebase key.Binding
	Run        key.Binding
	AddRoot    key.Binding
	Refresh    key.Binding
	Back       key.Binding
}

// Repos returns bindings for the repo fleet screen.
func Repos() ReposMap {
	return ReposMap{
		Table:      Table(),
		Open:       key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "details")),
		Fetch:      key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "fetch all")),
		Pull:       key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "pull (ff-only)")),
		PullRebase: key.NewBinding(key.WithKeys("P"), key.WithHelp("P", "pull --rebase")),
		Sync:       key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "sync (merge)")),
		SyncRebase: key.NewBinding(key.WithKeys("S"), key.WithHelp("S", "sync (rebase)")),
		Run:        key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "run command")),
		AddRoot:    key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "add root")),
		Refresh:    refresh(),
		Back:       back(),
	}
}

// ShortHelp implements help.KeyMap.
func (k ReposMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Open, k.Sync, k.Pull, k.Fetch, k.Run, k.Refresh}
}

// FullHelp implements help.KeyMap.
func (k ReposMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Table.LineUp, k.Table.LineDown, k.Table.GotoTop, k.Table.GotoBottom},
		{k.Open, k.Fetch, k.Refresh, k.Back},
		{k.Sync, k.SyncRebase, k.Pull, k.PullRebase},
		{k.Run, k.AddRoot},
	}
}

// DotfilesMap is the binding set for the dotfiles screen.
type DotfilesMap struct {
	Table    table.KeyMap
	Add      key.Binding
	Remove   key.Binding
	Fix      key.Binding
	Reset    key.Binding
	Push     key.Binding
	GitView  key.Binding
	GitSetup key.Binding
	Migrate  key.Binding
	Refresh  key.Binding
	Back     key.Binding
}

// Dotfiles returns bindings for the dotfiles screen.
func Dotfiles() DotfilesMap {
	return DotfilesMap{
		Table:    Table(),
		Add:      key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "add")),
		Remove:   key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "remove")),
		Fix:      key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "fix links")),
		Reset:    key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "reset all")),
		Push:     key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "git push")),
		GitView:  key.NewBinding(key.WithKeys("v"), key.WithHelp("v", "git status/log")),
		GitSetup: key.NewBinding(key.WithKeys("G"), key.WithHelp("G", "git setup…")),
		Migrate:  key.NewBinding(key.WithKeys("M"), key.WithHelp("M", "migrate dirs…")),
		Refresh:  refresh(),
		Back:     back(),
	}
}

// ShortHelp implements help.KeyMap.
func (k DotfilesMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Add, k.Remove, k.Fix, k.Push, k.GitView, k.Refresh}
}

// FullHelp implements help.KeyMap.
func (k DotfilesMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Table.LineUp, k.Table.LineDown, k.Table.GotoTop, k.Table.GotoBottom},
		{k.Add, k.Remove, k.Fix, k.Reset},
		{k.Push, k.GitView, k.GitSetup, k.Migrate},
		{k.Refresh, k.Back},
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
	Prune   key.Binding
	Refresh key.Binding
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
		Search:  key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search")),
		Delete:  key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete")),
		Prune:   key.NewBinding(key.WithKeys("P"), key.WithHelp("P", "prune old")),
		Refresh: refresh(),
		Back:    back(),
	}
}

// ShortHelp implements help.KeyMap.
func (k ScratchMap) ShortHelp() []key.Binding {
	return []key.Binding{k.New, k.Open, k.Tag, k.Search, k.Delete, k.Prune}
}

// FullHelp implements help.KeyMap.
func (k ScratchMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Table.LineUp, k.Table.LineDown, k.Table.GotoTop, k.Table.GotoBottom},
		{k.New, k.Open, k.Search, k.Refresh},
		{k.Tag, k.AutoTag, k.Delete, k.Prune},
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
	Refresh    key.Binding
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
		Search:     key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search")),
		Delete:     key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete")),
		Prune:      key.NewBinding(key.WithKeys("P"), key.WithHelp("P", "prune")),
		Refresh:    refresh(),
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
		{k.View, k.Search, k.Refresh, k.Back},
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

// CaptureMap is the binding set for the capture screen. enter moves into
// the preview to scroll it; esc comes back to the list.
type CaptureMap struct {
	Table   table.KeyMap
	Focus   key.Binding
	Preview viewport.KeyMap
	Pin     key.Binding
	Amend   key.Binding
	Note    key.Binding
	Edit    key.Binding
	Refresh key.Binding
	Back    key.Binding
}

// Capture returns bindings for the capture screen.
func Capture() CaptureMap {
	return CaptureMap{
		Table:   Table(),
		Focus:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "scroll preview")),
		Preview: Detail().Viewport,
		Pin:     key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "pin clipboard")),
		Amend:   key.NewBinding(key.WithKeys("A"), key.WithHelp("A", "amend last")),
		Note:    key.NewBinding(key.WithKeys("w"), key.WithHelp("w", "write note")),
		Edit:    key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit file")),
		Refresh: refresh(),
		Back:    back(),
	}
}

// ShortHelp implements help.KeyMap.
func (k CaptureMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Focus, k.Pin, k.Amend, k.Note, k.Edit, k.Refresh}
}

// FullHelp implements help.KeyMap.
func (k CaptureMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Table.LineUp, k.Table.LineDown, k.Focus},
		{k.Pin, k.Amend, k.Note, k.Edit},
		{k.Refresh, k.Back},
	}
}

// IgnoreMap is the binding set for the sync-hygiene (ignore) screen.
// Fix actions apply to the selected violation, except ExcludeAll.
type IgnoreMap struct {
	Table        table.KeyMap
	Exclude      key.Binding
	Harbor       key.Binding
	Move         key.Binding
	Delete       key.Binding
	ExcludeAll   key.Binding
	Check        key.Binding
	ToggleHarbor key.Binding
	Tree         key.Binding
	List         key.Binding
	EditRules    key.Binding
	Refresh      key.Binding
	Back         key.Binding
}

// Ignore returns bindings for the ignore screen.
func Ignore() IgnoreMap {
	return IgnoreMap{
		Table:        Table(),
		Exclude:      key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "exclude")),
		Harbor:       key.NewBinding(key.WithKeys("h"), key.WithHelp("h", "safe-harbor parent")),
		Move:         key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "move to scratch")),
		Delete:       key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete")),
		ExcludeAll:   key.NewBinding(key.WithKeys("X"), key.WithHelp("X", "exclude all")),
		Check:        key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "check path")),
		ToggleHarbor: key.NewBinding(key.WithKeys("H"), key.WithHelp("H", "show/hide harbored")),
		Tree:         key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "tree…")),
		List:         key.NewBinding(key.WithKeys("l"), key.WithHelp("l", "list excluded…")),
		EditRules:    key.NewBinding(key.WithKeys("E"), key.WithHelp("E", "edit rules…")),
		Refresh:      key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "rescan")),
		Back:         back(),
	}
}

// ShortHelp implements help.KeyMap.
func (k IgnoreMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Exclude, k.Harbor, k.Move, k.Delete, k.Check, k.Refresh}
}

// FullHelp implements help.KeyMap.
func (k IgnoreMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Table.LineUp, k.Table.LineDown, k.Table.GotoTop, k.Table.GotoBottom},
		{k.Exclude, k.Harbor, k.Move, k.Delete, k.ExcludeAll},
		{k.Check, k.ToggleHarbor, k.Tree, k.List, k.EditRules},
		{k.Refresh, k.Back},
	}
}

// SecretsMap is the binding set for the secrets screen. Fix actions apply
// to the selected violation, except AllowAll.
type SecretsMap struct {
	Table          table.KeyMap
	View           key.Binding
	Allowlist      key.Binding
	AllowAll       key.Binding
	Exclude        key.Binding
	StorePass      key.Binding
	SkipDir        key.Binding
	Audit          key.Binding
	PassPush       key.Binding
	PassLog        key.Binding
	PassRemote     key.Binding
	Setup          key.Binding
	CredSetup      key.Binding
	CredStatus     key.Binding
	CredDisconnect key.Binding
	Refresh        key.Binding
	Back           key.Binding
}

// Secrets returns bindings for the secrets screen.
func Secrets() SecretsMap {
	return SecretsMap{
		Table:          Table(),
		View:           key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "view context")),
		Allowlist:      key.NewBinding(key.WithKeys("l"), key.WithHelp("l", "allowlist")),
		AllowAll:       key.NewBinding(key.WithKeys("L"), key.WithHelp("L", "allowlist all")),
		Exclude:        key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "exclude file")),
		StorePass:      key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "move to pass")),
		SkipDir:        key.NewBinding(key.WithKeys("D"), key.WithHelp("D", "skip dir")),
		Audit:          key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "audit pass store")),
		PassPush:       key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "push pass store")),
		PassLog:        key.NewBinding(key.WithKeys("v"), key.WithHelp("v", "pass git log")),
		PassRemote:     key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "set pass remote")),
		Setup:          key.NewBinding(key.WithKeys("S"), key.WithHelp("S", "pass setup…")),
		CredSetup:      key.NewBinding(key.WithKeys("C"), key.WithHelp("C", "credential setup…")),
		CredStatus:     key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "credential status…")),
		CredDisconnect: key.NewBinding(key.WithKeys("X"), key.WithHelp("X", "credential disconnect…")),
		Refresh:        key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "rescan")),
		Back:           back(),
	}
}

// ShortHelp implements help.KeyMap.
func (k SecretsMap) ShortHelp() []key.Binding {
	return []key.Binding{k.View, k.Allowlist, k.Exclude, k.StorePass, k.SkipDir, k.Refresh}
}

// FullHelp implements help.KeyMap.
func (k SecretsMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Table.LineUp, k.Table.LineDown, k.View, k.Refresh, k.Back},
		{k.Allowlist, k.AllowAll, k.Exclude, k.StorePass, k.SkipDir},
		{k.Audit, k.PassPush, k.PassLog, k.PassRemote, k.Setup},
		{k.CredStatus, k.CredSetup, k.CredDisconnect},
	}
}

// CronMap is the binding set for the cron screen. Actions apply to the
// selected job.
type CronMap struct {
	Table   table.KeyMap
	Add     key.Binding
	Remove  key.Binding
	Log     key.Binding
	Refresh key.Binding
	Back    key.Binding
}

// Cron returns bindings for the cron screen.
func Cron() CronMap {
	return CronMap{
		Table:   Table(),
		Add:     key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "install")),
		Remove:  key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "remove")),
		Log:     key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "view log")),
		Refresh: refresh(),
		Back:    back(),
	}
}

// ShortHelp implements help.KeyMap.
func (k CronMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Log, k.Add, k.Remove, k.Refresh}
}

// FullHelp implements help.KeyMap.
func (k CronMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Table.LineUp, k.Table.LineDown, k.Table.GotoTop, k.Table.GotoBottom},
		{k.Log, k.Add, k.Remove},
		{k.Refresh, k.Back},
	}
}

// TrashMap is the binding set for the trash screen.
type TrashMap struct {
	Table   table.KeyMap
	Enable  key.Binding
	Disable key.Binding
	Empty   key.Binding
	Refresh key.Binding
	Back    key.Binding
}

// Trash returns bindings for the trash screen.
func Trash() TrashMap {
	return TrashMap{
		Table:   Table(),
		Enable:  key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "enable soft-delete")),
		Disable: key.NewBinding(key.WithKeys("D"), key.WithHelp("D", "disable")),
		Empty:   key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "empty trash")),
		Refresh: refresh(),
		Back:    back(),
	}
}

// ShortHelp implements help.KeyMap.
func (k TrashMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Enable, k.Disable, k.Empty, k.Refresh}
}

// FullHelp implements help.KeyMap.
func (k TrashMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Table.LineUp, k.Table.LineDown},
		{k.Enable, k.Disable, k.Empty},
		{k.Refresh, k.Back},
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
			PageUp:       key.NewBinding(key.WithKeys("pgup"), key.WithHelp("pgup", "page up")),
			PageDown:     key.NewBinding(key.WithKeys("pgdown"), key.WithHelp("pgdn", "page down")),
			HalfPageUp:   key.NewBinding(key.WithKeys("ctrl+u"), key.WithHelp("ctrl+u", "½ page up")),
			HalfPageDown: key.NewBinding(key.WithKeys("ctrl+d"), key.WithHelp("ctrl+d", "½ page down")),
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
		{k.Viewport.HalfPageUp, k.Viewport.HalfPageDown, k.Back},
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
		All:    key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "all/none")),
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

func refresh() key.Binding {
	return key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh"))
}

func back() key.Binding {
	return key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back"))
}
