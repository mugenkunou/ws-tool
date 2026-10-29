// Package workspace is the Workspace screen: ws itself, one row per setting:
// its version, ws.json, shell completion for each shell, and the layout of
// the workspace (folders and homes). `config show|edit` and `completion
// enable|disable` are its `:` commands; the settings have no keys of their own.
package workspace

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/mugenkunou/ws-tool/internal/buildinfo"
	"github.com/mugenkunou/ws-tool/internal/plan"
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
	"github.com/mugenkunou/ws-tool/internal/workspace"
	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

// Row kinds.
const (
	kindVersion    = "version"
	kindConfig     = "config"
	kindCompletion = "completion"
	kindLayout     = "layout"
	kindHome       = "home"
)

// Row states.
const (
	stateOK      = "ok"
	stateInvalid = "invalid"
	stateMissing = "missing"
	stateOn      = "on"
	stateOff     = "off"
)

// Row is one setting.
type Row struct {
	Kind    string // one of the kind constants
	Name    string // "version", "ws.json", "completion bash", "layout notes", "home scratch"
	State   string // ok, invalid, missing, on, off; empty when the row has none
	Value   string // the version, or the path the row is about
	IsPath  bool   // Value is an absolute path, shown relative to the workspace or home
	Note    string
	Shell   string // completion rows: bash, zsh or fish
	Current bool   // completion rows: the shell $SHELL names
}

// Schema describes the rows for the list and its filter and sort.
func Schema(v resource.View) resource.Schema[Row] {
	return resource.Schema[Row]{
		Name: "setting",
		ID:   func(r Row) string { return r.Name },
		Fields: []resource.Field[Row]{
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
		Columns: []string{"name", "state", "value", "note"},
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
	Completion []shellcomp.Status
	Folders    []workspace.LayoutFolder
	Homes      []workspace.Home
	Err        error // reading the settings failed
}

// Load reads every setting. ws.json is read afresh, not taken from the env, so
// an edit that broke it shows as a row state.
func Load(e env.Env) tea.Cmd {
	return func() tea.Msg {
		version := e.Version
		if version == "" {
			version = "dev"
		}
		msg := LoadedMsg{
			Info:       buildinfo.New(version),
			ConfigPath: e.Paths.Config,
			Folders:    workspace.LayoutFolders(e.Paths.Workspace),
			Homes:      workspace.ScriptHomes(os.Getenv, e.ScratchDir),
		}
		switch {
		case !workspace.ConfigExists(e.Paths.Config):
			msg.ConfigErr = os.ErrNotExist
		default:
			_, msg.ConfigErr = wsconfig.Load(e.Paths.Config)
		}
		msg.Completion, msg.Err = shellcomp.Statuses()
		return msg
	}
}

// rows lists the settings in the order the screen shows them.
func (m LoadedMsg) rows() []Row {
	out := []Row{{Kind: kindVersion, Name: "version", Value: m.Info.Version,
		Note: fmt.Sprintf("ws.json schema %d · %s · %s", m.Info.ConfigSchema, m.Info.Platform, m.Info.GoVersion)}}

	cfg := Row{Kind: kindConfig, Name: "ws.json", State: stateOK, Value: m.ConfigPath, IsPath: true}
	switch {
	case errors.Is(m.ConfigErr, os.ErrNotExist):
		cfg.State, cfg.Note = stateMissing, "ws init creates it"
	case m.ConfigErr != nil:
		cfg.State, cfg.Note = stateInvalid, firstLine(m.ConfigErr.Error())
	}
	out = append(out, cfg)

	for _, c := range m.Completion {
		r := Row{Kind: kindCompletion, Name: "completion " + c.Shell, State: stateOff, Value: c.RC, IsPath: true, Shell: c.Shell, Current: c.Current}
		if c.Enabled {
			r.State = stateOn
		}
		if c.Current {
			r.Note = "your shell"
		}
		out = append(out, r)
	}

	for _, f := range m.Folders {
		r := Row{Kind: kindLayout, Name: "layout " + f.Rel, State: stateOK, Value: f.Path, IsPath: true, Note: f.Purpose}
		if !f.Exists {
			r.State, r.Note = stateMissing, f.Purpose+" (ws init creates it)"
		}
		out = append(out, r)
	}
	for _, h := range m.Homes {
		out = append(out, Row{Kind: kindHome, Name: "home " + h.Name, Value: h.Path, IsPath: true, Note: h.Var + ": " + h.Purpose})
	}
	return out
}

// firstLine is s up to its first line break.
func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

type step int

const stepCompletion step = iota

// Model is the screen state.
type Model struct {
	env    env.Env
	keys   keys.WorkspaceMap
	list   reslist.Model[Row]
	modal  modal.Model
	width  int
	height int
}

// New creates the screen.
func New(e env.Env) Model {
	return Model{
		env:  e,
		keys: keys.Workspace(),
		list: reslist.New(Schema(e.View()), []reslist.Col[Row]{
			{Key: "name", Min: 20, Weight: 1},
			{Key: "state", Min: 8, Style: stateStyle},
			{Key: "value", Min: 16, Weight: 2, Style: func(_ Row, t string) string { return theme.Muted.Render(t) }},
			{Key: "note", Min: 16, Weight: 3, Style: func(_ Row, t string) string { return theme.Muted.Render(t) }},
		}, "Nothing to show.").Declare(reslist.Decls(actions)),
	}
}

// stateStyle colours a state: green when fine, red when ws.json does not load,
// and amber for what needs doing (a missing folder, completion off in the
// shell you use).
func stateStyle(r Row, t string) string {
	switch r.State {
	case stateOK, stateOn:
		return theme.OK.Render(t)
	case stateInvalid:
		return theme.Error.Render(t)
	case stateMissing:
		return theme.Warn.Render(t)
	case stateOff:
		if r.Current {
			return theme.Warn.Render(t)
		}
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

// configDone tags the handover of `config edit` and `config show`, so every
// screen reloads when they return: ws.json drives them all.
type configDone struct{}

// Update implements nav.Component.
func (m Model) Update(msg tea.Msg) (nav.Component, tea.Cmd) {
	if _, ok := m.modal.Match(msg); ok {
		m.modal = m.modal.Close()
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
	case LoadedMsg:
		if msg.Err != nil {
			m.list = m.list.SetError(msg.Err)
			return m, nil
		}
		m.list = m.list.SetItems(msg.rows())
		return m, nil
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

	if c, ok := msg.(nav.CommandMsg); ok {
		if m.modal.Open() {
			return m, nil
		}
		next, cmd, _ := reslist.RunCommand(m, m.list, commands, c)
		return next, cmd
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

// actions is every Workspace key and the rows it acts on.
var actions = func() []reslist.Action[Model, Row] {
	k := keys.Workspace()
	return []reslist.Action[Model, Row]{
		{Key: k.Open, Scope: reslist.Cursor, Do: Model.open},
	}
}()

// commands are the `:` commands, named with the CLI noun they host.
var commands = []reslist.Command[Model, Row]{
	{Name: "config show", Help: "the resolved ws.json (paged)", Scope: reslist.Global,
		Do: func(m Model, _ []Row, _ []string) (Model, tea.Cmd) { return m, m.configShow() }},
	{Name: "config edit", Help: "edit ws.json (hands over to your editor)", Scope: reslist.Global,
		Do: func(m Model, _ []Row, _ []string) (Model, tea.Cmd) { return m, m.configEdit() }},
	{Name: "completion enable", Args: "[--shell bash|zsh|fish]", Help: "load ws completion from the shell's rc file", Scope: reslist.Selected,
		Do: func(m Model, rows []Row, args []string) (Model, tea.Cmd) { return m.completion(true, rows, args) }},
	{Name: "completion disable", Args: "[--shell bash|zsh|fish]", Help: "remove ws completion from the shell's rc file", Scope: reslist.Selected,
		Do: func(m Model, rows []Row, args []string) (Model, tea.Cmd) { return m.completion(false, rows, args) }},
}

// Commands implements nav.Commander.
func (m Model) Commands() []nav.CommandInfo { return reslist.CommandInfos(commands) }

func (m Model) configShow() tea.Cmd {
	return handover.Run(handover.Paged(handover.WS(m.env.Paths, "config", "show")), configDone{})
}

func (m Model) configEdit() tea.Cmd {
	return handover.Run(handover.WS(m.env.Paths, "config", "edit"), configDone{})
}

// open is enter: the ws.json row opens in your editor. The other rows change
// through commands.
func (m Model) open(rows []Row) (Model, tea.Cmd) {
	if len(rows) == 1 && rows[0].Kind == kindConfig {
		return m, m.configEdit()
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
