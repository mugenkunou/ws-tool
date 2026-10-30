// Package settings is the Settings screen: ws itself, in groups. Configuration
// (ws.json: its summary, edit, view), Workspace (root, layout and homes as one
// row each), Shell completion (one row per shell), Versions (ws and agent-tmux,
// whether an update is out, how to upgrade, a forced check) and About (the build).
// `config show|edit`, `completion enable|disable` and the update check are its
// `.` commands; enter runs the row under the cursor.
package settings

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/mugenkunou/ws-tool/internal/agenttmux"
	"github.com/mugenkunou/ws-tool/internal/buildinfo"
	"github.com/mugenkunou/ws-tool/internal/editor"
	"github.com/mugenkunou/ws-tool/internal/plan"
	"github.com/mugenkunou/ws-tool/internal/release"
	"github.com/mugenkunou/ws-tool/internal/resource"
	"github.com/mugenkunou/ws-tool/internal/shellcomp"
	"github.com/mugenkunou/ws-tool/internal/tui/confirm"
	"github.com/mugenkunou/ws-tool/internal/tui/env"
	"github.com/mugenkunou/ws-tool/internal/tui/handover"
	"github.com/mugenkunou/ws-tool/internal/tui/keys"
	"github.com/mugenkunou/ws-tool/internal/tui/modal"
	"github.com/mugenkunou/ws-tool/internal/tui/nav"
	"github.com/mugenkunou/ws-tool/internal/tui/reslist"
	"github.com/mugenkunou/ws-tool/internal/tui/theme"
	"github.com/mugenkunou/ws-tool/internal/versions"
	"github.com/mugenkunou/ws-tool/internal/workspace"
	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

// Seams: tests replace them so no test reaches the network, the cache file or
// the real agent-tmux.
var (
	cachedVersions  = versions.Cached
	refreshVersions = versions.Refresh
	executable      = os.Executable
)

// Groups, in the order the screen shows them.
const (
	groupConfig     = "Configuration"
	groupWorkspace  = "Workspace"
	groupCompletion = "Completion"
	groupVersions   = "Versions"
	groupAbout      = "About"
)

// Row kinds.
const (
	kindConfig     = "config" // ws.json itself
	kindEdit       = "edit"   // action: edit ws.json
	kindView       = "view"   // action: view ws.json
	kindRoot       = "root"
	kindLayout     = "layout"
	kindHomes      = "homes"
	kindCompletion = "completion"
	kindVersion    = "version" // a binary's version
	kindHint       = "hint"    // one line of how to upgrade
	kindCheck      = "check"   // action: look up the latest releases now
	kindBuild      = "build"
)

// Row states.
const (
	stateOK           = "ok"
	stateInvalid      = "invalid"
	stateMissing      = "missing"
	stateOn           = "on"
	stateOff          = "off"
	stateCurrent      = "current"
	stateOutdated     = "outdated"
	stateDev          = "dev"
	stateUnknown      = "unknown"
	stateNotInstalled = "not installed"
	stateTooOld       = "too old"
)

// warnMark flags an item that needs an update.
const warnMark = "⚠"

// Row is one line of the screen: a setting, an action or a fact.
type Row struct {
	Key     string // unique within the screen; Name when empty
	Group   string // one of the group constants
	Label   string // the group's name on its first row only, so groups read as blocks
	Kind    string // one of the kind constants
	Name    string // "ws.json", "completion bash", "ws", "check for updates now"
	State   string // ok, invalid, missing, on, off, outdated…; empty when the row has none
	Value   string // the version, or the path the row is about
	IsPath  bool   // Value is an absolute path, shown relative to the workspace or home
	Note    string
	Shell   string // completion rows: bash, zsh or fish
	Current bool   // completion rows: the shell $SHELL names
}

func (r Row) id() string {
	if r.Key != "" {
		return r.Key
	}
	return r.Name
}

// Schema describes the rows for the list and its filter and sort.
func Schema(v resource.View) resource.Schema[Row] {
	return resource.Schema[Row]{
		Name: "setting",
		ID:   Row.id,
		Fields: []resource.Field[Row]{
			{Key: "group", Title: "Group", Value: func(r Row) any { return r.Group },
				Text: func(r Row) string { return r.Label }, Whole: true},
			{Key: "name", Title: "Setting", Value: func(r Row) any { return r.Name }},
			{Key: "kind", Title: "Kind", Value: func(r Row) any { return r.Kind }, Whole: true},
			{Key: "state", Title: "State", Value: func(r Row) any { return r.State }, Whole: true},
			{Key: "value", Title: "Value", Value: func(r Row) any { return r.Value },
				Text: func(r Row) string {
					if r.IsPath {
						return v.Path(r.Value)
					}
					return r.Value
				}},
			{Key: "note", Title: "Note", Value: func(r Row) any { return r.Note }},
		},
		Columns: []string{"group", "name", "state", "value", "note"},
	}
}

// LoadedMsg carries what the screen shows. Other components (the Dashboard)
// observe it.
type LoadedMsg struct {
	Info buildinfo.Info
	// ConfigPath is the ws.json path; ConfigErr says why it does not load
	// (missing, unreadable, invalid or of a newer schema), nil when it does.
	ConfigPath string
	ConfigErr  error
	Config     wsconfig.WsConfig // meaningful when ConfigErr is nil
	Editor     string            // the editor `config edit` opens
	Workspace  string
	Completion []shellcomp.Status
	Folders    []workspace.LayoutFolder
	Homes      []workspace.Home
	// Report is the latest known ws and agent-tmux versions (the cache, or a
	// newer lookup); WSUpgrade is how to upgrade this ws binary.
	Report    versions.Report
	WSUpgrade []string
	Err       error // reading the settings failed

	checking bool // a forced lookup is running
}

// Load reads every setting. ws.json is read afresh, not taken from the env, so
// an edit that broke it shows as a row state. The version report comes from the
// cache: the screen never waits on the network.
func Load(e env.Env) tea.Cmd {
	return func() tea.Msg {
		version := e.Version
		if version == "" {
			version = "dev"
		}
		msg := LoadedMsg{
			Info:       buildinfo.New(version),
			ConfigPath: e.Paths.Config,
			Workspace:  e.Paths.Workspace,
			Folders:    workspace.LayoutFolders(e.Paths.Workspace),
			Homes:      workspace.ScriptHomes(os.Getenv, e.ScratchDir),
		}
		var cfgEditor string
		switch {
		case !workspace.ConfigExists(e.Paths.Config):
			msg.ConfigErr = os.ErrNotExist
		default:
			msg.Config, msg.ConfigErr = wsconfig.Load(e.Paths.Config)
			cfgEditor = msg.Config.Editor
		}
		if argv := editor.Command(cfgEditor); len(argv) > 0 {
			msg.Editor = filepath.Base(argv[0])
		}
		msg.Report = cachedVersions(context.Background(), version)
		if exe, err := executable(); err == nil {
			msg.WSUpgrade = release.WSUpgradeLines(exe)
		}
		msg.Completion, msg.Err = shellcomp.Statuses()
		return msg
	}
}

// rows lists the settings in the order the screen shows them, grouped.
func (m LoadedMsg) rows() []Row {
	var out []Row
	last := ""
	add := func(group string, rs ...Row) {
		for _, r := range rs {
			r.Group = group
			if group != last {
				r.Label, last = group, group
			}
			out = append(out, r)
		}
	}

	cfg := Row{Kind: kindConfig, Name: "ws.json", State: stateOK, Value: m.ConfigPath, IsPath: true}
	switch {
	case errors.Is(m.ConfigErr, os.ErrNotExist):
		cfg.State, cfg.Note = stateMissing, "ws init creates it"
	case m.ConfigErr != nil:
		cfg.State, cfg.Note = stateInvalid, firstLine(m.ConfigErr.Error())
	default:
		cfg.Note = fmt.Sprintf("schema %d · %s · %s", m.Config.Schema,
			plural(len(m.Config.Symlinks), "symlink"), plural(len(m.Config.Repo.Roots), "repo root"))
	}
	add(groupConfig, cfg,
		Row{Kind: kindEdit, Name: "edit ws.json", Note: "in " + m.Editor + "; ws.json is checked when you close it"},
		Row{Kind: kindView, Name: "view ws.json", Note: "the resolved config, read-only"})

	add(groupWorkspace, Row{Kind: kindRoot, Name: "root", Value: m.Workspace, IsPath: true, Note: "WS_ROOT"})
	var rels, missing []string
	for _, f := range m.Folders {
		rels = append(rels, f.Rel)
		if !f.Exists {
			missing = append(missing, f.Rel)
		}
	}
	layout := Row{Kind: kindLayout, Name: "layout", State: stateOK, Value: plural(len(m.Folders), "folder"), Note: strings.Join(rels, " · ")}
	if len(missing) > 0 {
		layout.State = stateMissing
		layout.Note = strings.Join(missing, ", ") + " missing: ws init creates " + pronoun(len(missing))
	}
	var homes []string
	for _, h := range m.Homes {
		homes = append(homes, h.Name)
	}
	add(groupWorkspace, layout,
		Row{Kind: kindHomes, Name: "homes", Value: strings.Join(homes, " · "), Note: "outside the workspace; ws env exports them"})

	for _, c := range m.Completion {
		r := Row{Kind: kindCompletion, Name: "completion " + c.Shell, State: stateOff, Value: c.RC, IsPath: true, Shell: c.Shell, Current: c.Current}
		if c.Enabled {
			r.State = stateOn
		}
		if c.Current {
			r.Note = "your shell"
		}
		add(groupCompletion, r)
	}

	add(groupVersions, m.versionRow(m.Report.WS, m.Info.Version))
	add(groupVersions, hints("ws", m.Report.WS.State == versions.StateOutdated, m.WSUpgrade)...)
	add(groupVersions, m.versionRow(m.Report.AgentTmux, ""))
	switch m.Report.AgentTmux.State {
	case versions.StateOutdated, versions.StateNotInstalled, versions.StateTooOld:
		add(groupVersions, hints("agent-tmux", true, agenttmux.InstallLines)...)
	}
	check := Row{Kind: kindCheck, Name: "check for updates now", Note: "asks GitHub for the latest ws and agent-tmux releases"}
	switch {
	case m.checking:
		check.Note = "checking…"
	case m.Report.Known():
		check.Note += "; last checked " + m.Report.CheckedAt.Local().Format("2006-01-02 15:04")
	}
	add(groupVersions, check)

	add(groupAbout, Row{Kind: kindBuild, Name: "build", Value: m.Info.Platform + " · " + m.Info.GoVersion,
		Note: fmt.Sprintf("ws.json schema %d", m.Info.ConfigSchema)})
	return out
}

// versionRow is a binary's row. A dev build is never "outdated": it says so and
// names the upstream release. installed overrides the report's version for ws,
// which knows its own.
func (m LoadedMsg) versionRow(it versions.Item, installed string) Row {
	r := Row{Kind: kindVersion, Name: it.Name, Value: it.Installed}
	if installed != "" {
		r.Value = installed
	}
	upstream := ""
	if it.Latest != "" {
		upstream = "latest " + it.Latest
	}
	switch it.State {
	case versions.StateCurrent:
		r.State, r.Note = stateCurrent, upstream
	case versions.StateOutdated:
		r.State, r.Note = stateOutdated, warnMark+" update available: "+it.Latest
	case versions.StateDev:
		r.State, r.Note = stateDev, "local build is dev"
		if it.Latest != "" {
			r.Note += "; upstream is " + it.Latest
		}
	case versions.StateNotInstalled:
		r.State, r.Value, r.Note = stateNotInstalled, "", "ws session needs it"
	case versions.StateTooOld:
		r.State, r.Note = stateTooOld, "too old to check; upgrade"
	default:
		r.State, r.Note = stateUnknown, "not checked yet"
		if it.Error != "" {
			r.Note = "lookup failed: " + firstLine(it.Error)
		} else if m.Report.Known() {
			r.Note = "latest unknown"
		}
	}
	return r
}

// hints turns upgrade instructions into rows, one per line; the first names
// what they are for.
func hints(what string, show bool, lines []string) []Row {
	if !show {
		return nil
	}
	var out []Row
	for i, l := range lines {
		r := Row{Kind: kindHint, Key: fmt.Sprintf("upgrade %s %d", what, i), Note: strings.TrimSpace(l)}
		if i == 0 {
			r.Name = "upgrade " + what
		}
		out = append(out, r)
	}
	return out
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

func pronoun(n int) string {
	if n == 1 {
		return "it"
	}
	return "them"
}

// firstLine is s up to its first line break.
func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

type step int

const (
	stepCompletion step = iota
	stepMenu            // the `.` menu
)

// SelectMsg asks the screen to select a row by name (sent by the Dashboard's
// advice rows, so enter there lands on the row to act on).
type SelectMsg struct{ Name string }

// Model is the screen state.
type Model struct {
	env      env.Env
	keys     keys.SettingsMap
	list     reslist.Model[Row]
	modal    modal.Model
	loaded   LoadedMsg // the last load, with the newest version report merged in
	ready    bool      // loaded holds a load
	report   versions.Report
	checking bool
	width    int
	height   int
}

// New creates the screen.
func New(e env.Env) Model {
	return Model{
		env:  e,
		keys: keys.Settings(),
		list: reslist.New(Schema(e.View()), []reslist.Col[Row]{
			{Key: "group", Min: 13, Style: func(_ Row, t string) string { return theme.Bold.Render(t) }},
			{Key: "name", Min: 20, Weight: 1},
			{Key: "state", Min: 13, Style: stateStyle},
			{Key: "value", Min: 16, Weight: 2, Style: func(_ Row, t string) string { return theme.Muted.Render(t) }},
			{Key: "note", Min: 16, Weight: 3, Style: noteStyle},
		}, "Nothing to show.").Declare(reslist.Decls(actions)),
	}
}

// stateStyle colours a state: green when fine, red when ws.json does not load,
// and amber for what needs doing (a missing folder, an update, completion off in
// the shell you use).
func stateStyle(r Row, t string) string {
	switch r.State {
	case stateOK, stateOn, stateCurrent:
		return theme.OK.Render(t)
	case stateInvalid:
		return theme.Error.Render(t)
	case stateMissing, stateOutdated, stateNotInstalled, stateTooOld:
		return theme.Warn.Render(t)
	case stateOff:
		if r.Current {
			return theme.Warn.Render(t)
		}
	}
	return theme.Muted.Render(t)
}

func noteStyle(r Row, t string) string {
	if r.State == stateOutdated {
		return theme.Warn.Render(t)
	}
	return theme.Muted.Render(t)
}

// Init implements nav.Component.
func (m Model) Init() tea.Cmd { return Load(m.env) }

// Declared lists the screen's actions and their scopes.
func (m Model) Declared() []reslist.Decl { return m.list.Declared() }

// CapturesKeys implements nav.Component.
func (m Model) CapturesKeys() bool { return m.modal.Open() || m.list.Editing() }

func (m Model) reload() (Model, tea.Cmd) {
	m.list = m.list.SetLoading()
	return m, Load(m.env)
}

// show puts the loaded message on the list, merging the newest version report
// and the running check, and returns the message as the card's source.
func (m Model) show(msg LoadedMsg) Model {
	if msg.Report.CheckedAt.Before(m.report.CheckedAt) {
		msg.Report = m.report
	} else {
		m.report = msg.Report
	}
	msg.checking = m.checking
	m.loaded, m.ready = msg, true
	m.list = m.list.SetItems(msg.rows())
	return m
}

// configDone tags the handover of `config edit` and `config show`, so every
// screen reloads when they return: ws.json drives them all.
type configDone struct{}

// Update implements nav.Component.
func (m Model) Update(msg tea.Msg) (nav.Component, tea.Cmd) {
	if st, ok := m.modal.Match(msg); ok {
		m.modal = m.modal.Close()
		if st == stepMenu {
			next, cmd := reslist.MenuPick(m, m.list, commands, msg)
			return next, cmd
		}
		done := msg.(confirm.DoneMsg)
		if done.Result.Aborted {
			return m, nil
		}
		m.list = m.list.ClearMarks() // the marks were for this action
		level := nav.LevelSuccess
		if done.Result.HasFailures() {
			level = nav.LevelWarn
		}
		m, cmd := m.reload()
		return m, tea.Batch(nav.Notify(level, confirm.Summary(done.Result)), cmd)
	}

	switch msg := msg.(type) {
	case nav.ResizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.list = m.list.SetSize(msg.Width, msg.Height)
		m.modal = m.modal.SetSize(msg.Width, msg.Height)
		return m, nil
	case nav.FilterMsg:
		m.list = m.list.SetFilter(msg.Filter)
		return m, nil
	case nav.RefreshMsg:
		return m.reload()
	case SelectMsg:
		m.list, _ = m.list.Select(func(r Row) bool { return r.id() == msg.Name })
		return m, nil
	case LoadedMsg:
		if msg.Err != nil {
			m.list = m.list.SetError(msg.Err)
			return m, nil
		}
		return m.show(msg), nil
	case nav.VersionsMsg:
		return m.versionsArrived(msg.Report)
	case handover.DoneMsg:
		if _, ok := msg.Tag.(configDone); ok {
			if msg.Err != nil {
				return m, nav.Notify(nav.LevelError, msg.Err.Error())
			}
			// ws.json changed what every screen shows.
			return m, func() tea.Msg { return nav.RefreshMsg{} }
		}
		return m, nil
	}

	if _, ok := msg.(nav.MenuMsg); ok {
		if m.modal.Open() {
			return m, nil
		}
		m.modal = m.modal.Choose(stepMenu, reslist.MenuChooser("Settings actions", commands))
		return m, nil
	}

	if m.modal.Open() {
		var cmd tea.Cmd
		m.modal, cmd = m.modal.Update(msg)
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
	if next, cmd, ok := reslist.Run(m, m.list, actions, k); ok {
		return next, cmd
	}
	if key.Matches(k, m.keys.Back) {
		return m, nav.Up
	}
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(k)
	return m, cmd
}

// versionsArrived takes a newer version report. The card shows it too, so the
// screen re-announces its load. A check the user asked for says how it ended.
func (m Model) versionsArrived(r versions.Report) (Model, tea.Cmd) {
	if r.CheckedAt.Before(m.report.CheckedAt) {
		return m, nil
	}
	m.report = r
	wasChecking := m.checking
	m.checking = false
	var cmds []tea.Cmd
	if m.ready {
		loaded := m.loaded
		loaded.Report, loaded.checking = r, false
		m = m.show(loaded)
		loaded = m.loaded
		cmds = append(cmds, func() tea.Msg { return loaded })
	}
	if wasChecking {
		cmds = append(cmds, nav.Notify(checkLevel(r), checkSummary(r)))
	}
	return m, tea.Batch(cmds...)
}

func checkLevel(r versions.Report) nav.Level {
	switch {
	case !r.Known():
		return nav.LevelWarn
	case r.Outdated():
		return nav.LevelWarn
	}
	return nav.LevelSuccess
}

func checkSummary(r versions.Report) string {
	var out []string
	for _, it := range []versions.Item{r.WS, r.AgentTmux} {
		switch it.State {
		case versions.StateOutdated:
			out = append(out, it.Name+" "+it.Latest+" is available")
		case versions.StateUnknown:
			if it.Error != "" {
				out = append(out, it.Name+": lookup failed")
			}
		case versions.StateNotInstalled:
			out = append(out, it.Name+" is not installed")
		case versions.StateTooOld:
			out = append(out, it.Name+" is too old to check")
		}
	}
	if len(out) > 0 {
		return strings.Join(out, "; ")
	}
	if !r.Known() {
		return "Could not look up the latest releases"
	}
	return "Up to date"
}

// actions is every Settings key and the rows it acts on.
var actions = func() []reslist.Action[Model, Row] {
	k := keys.Settings()
	return []reslist.Action[Model, Row]{
		{Key: k.Open, Scope: reslist.Cursor, Do: Model.open},
	}
}()

// commands are the `.` menu, named with the CLI words they host.
var commands = []reslist.Command[Model, Row]{
	{Name: "config show", Menu: "s", Help: "view the resolved ws.json (paged)", Scope: reslist.Global,
		Do: func(m Model, _ []Row, _ []string) (Model, tea.Cmd) { return m, m.configShow() }},
	{Name: "config edit", Menu: "e", Help: "edit ws.json (hands over to your editor)", Scope: reslist.Global,
		Do: func(m Model, _ []Row, _ []string) (Model, tea.Cmd) { return m, m.configEdit() }},
	{Name: "version check", Menu: "u", Help: "look up the latest ws and agent-tmux releases now", Scope: reslist.Global,
		Do: func(m Model, _ []Row, _ []string) (Model, tea.Cmd) { return m.checkNow() }},
	{Name: "completion enable", Menu: "n", Args: "[--shell bash|zsh|fish]", Help: "load ws completion from the shell's rc file", Scope: reslist.Selected,
		Do: func(m Model, rows []Row, args []string) (Model, tea.Cmd) { return m.completion(true, rows, args) }},
	{Name: "completion disable", Menu: "d", Args: "[--shell bash|zsh|fish]", Help: "remove ws completion from the shell's rc file", Scope: reslist.Selected,
		Do: func(m Model, rows []Row, args []string) (Model, tea.Cmd) { return m.completion(false, rows, args) }},
}

// Commands implements nav.Commander.
func (m Model) Commands() []nav.CommandInfo { return reslist.CommandInfos(commands) }

func (m Model) configShow() tea.Cmd {
	return handover.Run(handover.Paged(handover.WS(m.env.Paths, "config", "show")), configDone{})
}

// configEdit hands the terminal to `ws config edit`, which opens the editor and
// reports an invalid ws.json as the CLI does; the reload then shows the row's
// state.
func (m Model) configEdit() tea.Cmd {
	return handover.Run(handover.WS(m.env.Paths, "config", "edit"), configDone{})
}

// checkNow forces a release lookup off the UI thread; the result comes back as
// a nav.VersionsMsg, which the app also keeps for its footer.
func (m Model) checkNow() (Model, tea.Cmd) {
	if m.checking {
		return m, nav.Notify(nav.LevelInfo, "Already checking for updates")
	}
	m.checking = true
	if m.ready {
		m.loaded.checking = true
		m.list = m.list.SetItems(m.loaded.rows())
	}
	version := m.env.Version
	if version == "" {
		version = "dev"
	}
	look := func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return nav.VersionsMsg{Report: refreshVersions(ctx, version)}
	}
	return m, tea.Batch(nav.Notify(nav.LevelInfo, "Checking for updates…"), look)
}

// open is enter: it runs the row's action. ws.json and its edit row open your
// editor, the view row pages the config, a completion row flips (the enable
// checklist when it is off, the disable one when it is on) and the check row
// looks up releases.
func (m Model) open(rows []Row) (Model, tea.Cmd) {
	if len(rows) != 1 {
		return m, nil
	}
	switch r := rows[0]; r.Kind {
	case kindConfig, kindEdit:
		return m, m.configEdit()
	case kindView:
		return m, m.configShow()
	case kindCompletion:
		return m.completion(r.State != stateOn, rows, nil)
	case kindCheck:
		return m.checkNow()
	}
	return m, nil
}

// completion is `completion enable|disable`: it acts on the completion rows
// that are marked or under the cursor, else on the shell $SHELL names, as the
// CLI does; --shell names one instead. The plan is the CLI's, one action per
// shell, all pre-checked.
func (m Model) completion(enable bool, rows []Row, args []string) (Model, tea.Cmd) {
	shells, err := completionShells(rows, args)
	if err != nil {
		return m, nav.Notify(nav.LevelWarn, err.Error())
	}
	verb, title, build := "disable", "Disable shell completion", shellcomp.DisablePlan
	if enable {
		verb, title, build = "enable", "Enable shell completion", shellcomp.EnablePlan
	}
	pl := plan.Plan{Command: "completion." + verb}
	var rcs []string
	for _, sh := range shells {
		rc, actions, err := build(sh)
		if err != nil {
			return m, nav.Notify(nav.LevelError, err.Error())
		}
		rcs = append(rcs, rc)
		for _, a := range actions {
			a.ID += "-" + sh // unique within the plan
			pl.Actions = append(pl.Actions, a)
		}
	}
	if len(pl.Actions) == 0 {
		text := "Completion is already enabled in " + strings.Join(rcs, ", ")
		if !enable {
			text = "Completion is not enabled in " + strings.Join(rcs, ", ")
		}
		return m, nav.Notify(nav.LevelInfo, text)
	}
	c := confirm.New(title, pl)
	if enable {
		note := "Open a new terminal to activate."
		if len(rcs) == 1 {
			note = "Run `source " + rcs[0] + "` or open a new terminal to activate."
		}
		c = c.WithNote(note)
	}
	m.modal = m.modal.Confirm(stepCompletion, c)
	return m, nil
}

// completionShells is the shells a completion command acts on: --shell when
// given, else the completion rows of rows (marked, or under the cursor), else
// the shell $SHELL names.
func completionShells(rows []Row, args []string) ([]string, error) {
	var name string
	var named bool
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--shell" && i+1 < len(args):
			name, named = args[i+1], true
			i++
		case strings.HasPrefix(a, "--shell="):
			name, named = strings.TrimPrefix(a, "--shell="), true
		default:
			return nil, fmt.Errorf("unexpected argument %q (use --shell bash|zsh|fish)", a)
		}
	}
	if named {
		sh, err := shellcomp.Resolve(name)
		return []string{sh}, err
	}
	var out []string
	for _, r := range rows {
		if r.Kind == kindCompletion {
			out = append(out, r.Shell)
		}
	}
	if len(out) > 0 {
		return out, nil
	}
	sh, err := shellcomp.Resolve("")
	return []string{sh}, err
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
