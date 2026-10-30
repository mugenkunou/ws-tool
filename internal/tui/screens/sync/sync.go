// Package sync is the Sync screen: the one-way mirror of the workspace into a
// folder a sync client watches. Its header is `ws sync status` (mirror, last
// push, stamp, pending totals) and its list is what the next push would copy
// or delete. `.` then `p` is `ws sync push` as a checklist; `ws sync restore` stays
// on the CLI, because it runs on a fresh machine before there is a workspace.
package sync

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/mugenkunou/ws-tool/internal/mirror"
	"github.com/mugenkunou/ws-tool/internal/plan"
	"github.com/mugenkunou/ws-tool/internal/tui/confirm"
	"github.com/mugenkunou/ws-tool/internal/tui/env"
	"github.com/mugenkunou/ws-tool/internal/tui/format"
	"github.com/mugenkunou/ws-tool/internal/tui/keys"
	"github.com/mugenkunou/ws-tool/internal/tui/modal"
	"github.com/mugenkunou/ws-tool/internal/tui/nav"
	"github.com/mugenkunou/ws-tool/internal/tui/reslist"
	"github.com/mugenkunou/ws-tool/internal/tui/theme"
	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

// Change is one pending change: a file the next push copies or deletes.
type Change = mirror.Change

// LoadedMsg carries the mirror's status and pending changes. Other components
// (the Dashboard) may observe it.
type LoadedMsg struct {
	At         time.Time // when the dry run was read, for the card's ages
	Options    mirror.Options
	Configured bool // sync.mirror.target is set
	Status     mirror.Status
	Changes    []Change
	// Questions are what a push must have confirmed first (a foreign stamp, the
	// delete guard); Wiped is the notice for a mirror emptied since the last push.
	Questions []mirror.Question
	Wiped     string
	Err       error
}

// step identifies the one dialog this screen opens.
type step int

const (
	stepPush step = iota
	stepMenu      // the `.` menu
)

// pushID is the checklist action that runs the push.
const pushID = "sync-push"

// Model is the Sync screen state.
type Model struct {
	env    env.Env
	keys   keys.SyncMap
	list   reslist.Model[Change]
	modal  modal.Model
	state  LoadedMsg
	loaded bool
	width  int
	height int
}

// headerLines is the status header: the four facts, then the symlink notice.
const headerLines = 5

// New creates the screen.
func New(e env.Env) Model {
	return Model{
		env:  e,
		keys: keys.Sync(),
		list: reslist.New(mirror.Schema(), []reslist.Col[Change]{
			{Key: "action", Min: 8, Style: func(c Change, t string) string {
				if c.Action == mirror.ActionDelete {
					return theme.Warn.Render(t)
				}
				return theme.Muted.Render(t)
			}},
			{Key: "path", Min: 20, Weight: 1},
		}, theme.Icon(theme.IconSparkles)+"The mirror is up to date. Nothing to copy or delete.").Declare(reslist.Decls(actions)),
	}
}

// pinned is the row above the list when a push has something to copy or delete.
func (m LoadedMsg) pinned() string {
	n := m.Status.PendingChanges + m.Status.PendingDeletes
	if !m.Configured || m.Err != nil || n == 0 {
		return ""
	}
	return fmt.Sprintf("Push to the mirror: %s to copy, %d to delete", format.Plural(m.Status.PendingChanges, "file"), m.Status.PendingDeletes)
}

// Load reads ws.json afresh (so a new target takes effect on refresh) and dry-runs
// the push. Not configured is a state, not an error; a missing rsync is an error.
func Load(e env.Env) tea.Cmd {
	return func() tea.Msg {
		msg := LoadedMsg{At: e.Now()}
		cfg, err := wsconfig.Load(e.Paths.Config)
		if err != nil {
			msg.Err = err
			return msg
		}
		o, err := mirror.OptionsFrom(e.Paths.Workspace, cfg)
		if err != nil {
			msg.Err = err
			return msg
		}
		msg.Options, msg.Configured = o, o.Target != ""
		if !msg.Configured {
			return msg
		}
		p, err := mirror.Prepare(o)
		if err != nil {
			msg.Err = err
			return msg
		}
		defer p.Close()
		msg.Status, msg.Changes = mirror.StatusOf(p), p.Changes()
		msg.Questions, msg.Wiped = p.Questions(), p.WipedWarning()
		return msg
	}
}

// Init starts the first load.
func (m Model) Init() tea.Cmd { return Load(m.env) }

// Declared lists the screen's actions and their scopes: none, since pushing is
// in the `.` menu.
func (m Model) Declared() []reslist.Decl { return m.list.Declared() }

// CapturesKeys implements nav.Component.
func (m Model) CapturesKeys() bool { return m.modal.Open() || m.list.Editing() }

func (m Model) reload() (Model, tea.Cmd) {
	m.list = m.list.SetLoading()
	return m, Load(m.env)
}

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
		m.list = m.list.SetSize(msg.Width, max(msg.Height-headerLines-1, 0))
		m.modal = m.modal.SetSize(msg.Width, msg.Height)
		return m, nil
	case nav.FilterMsg:
		m.list = m.list.SetFilter(msg.Filter)
		return m, nil
	case nav.RefreshMsg:
		return m.reload()
	case LoadedMsg:
		m.loaded, m.state = true, msg
		if msg.Err != nil {
			m.list = m.list.SetError(msg.Err)
			return m, nil
		}
		m.list = m.list.SetItems(msg.Changes).SetPinned(msg.pinned())
		return m, nil
	case reslist.PinnedMsg:
		if m.modal.Open() {
			return m, nil
		}
		next, cmd := m.push()
		return next, cmd
	}

	if _, ok := msg.(nav.MenuMsg); ok {
		if m.modal.Open() {
			return m, nil
		}
		m.modal = m.modal.Choose(stepMenu, reslist.MenuChooser("Sync actions", commands))
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
	if key.Matches(k, m.keys.Back) {
		return m, nav.Up
	}
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(k)
	return m, cmd
}

// actions is every Sync key and the rows it acts on: none. The screen has no
// verb keys; push is in the `.` menu.
var actions []reslist.Action[Model, Change]

// commands are the `.` menu: the CLI's words after `sync`. restore is not here:
// it runs on a fresh machine, before the TUI has a workspace.
var commands = []reslist.Command[Model, Change]{
	{Name: "push", Menu: "p", Help: "mirror the workspace into the target", Scope: reslist.Global,
		Do: func(m Model, _ []Change, _ []string) (Model, tea.Cmd) { return m.push() }},
}

// Commands implements nav.Commander.
func (m Model) Commands() []nav.CommandInfo { return reslist.CommandInfos(commands) }

// push is `sync push`: a checklist with the one action the CLI runs. Where the
// CLI would ask first (another push wrote the mirror, or the push would delete
// too much of it) the action starts unchecked, so ticking it is the answer.
func (m Model) push() (Model, tea.Cmd) {
	s := m.state
	switch {
	case !m.loaded:
		return m, nav.Notify(nav.LevelInfo, "Still reading the mirror…")
	case !s.Configured:
		return m, nav.Notify(nav.LevelWarn, "Sync is not set up. "+m.setupHint())
	case s.Err != nil:
		return m, nav.Notify(nav.LevelWarn, "Cannot push: "+s.Err.Error())
	}
	o := s.Options
	var approved []mirror.QuestionKind
	var notes []string
	for _, q := range s.Questions {
		approved = append(approved, q.Kind)
		notes = append(notes, q.Warning)
	}
	if s.Wiped != "" {
		notes = append(notes, s.Wiped)
	}
	if w := mirror.SymlinkWarning(len(s.Status.Symlinks)); w != "" {
		notes = append(notes, w)
	}
	pl := plan.Plan{Command: "sync.push", Actions: []plan.Action{{
		ID: pushID,
		Description: fmt.Sprintf("Push to %s: %s to copy, %s to delete", s.Status.Target,
			mirror.Plural(s.Status.PendingChanges, "file"), mirror.Plural(s.Status.PendingDeletes, "file")),
		Execute: func() error {
			_, err := mirror.Push(o, approved)
			return err
		},
	}}}
	asks := len(s.Questions) > 0
	c := confirm.New("Push to the mirror", pl).WithChecked(func(int, plan.Action) bool { return !asks })
	if asks {
		notes = append(notes, "Tick the push to go ahead anyway.")
	}
	if len(notes) > 0 {
		// The checklist clips its note, so wrap it here.
		c = c.WithNote(lipgloss.NewStyle().Width(max(m.width, 20)).Render(strings.Join(notes, "\n")))
	}
	m.modal = m.modal.Confirm(stepPush, c)
	return m, nil
}

// setupHint says how to set the mirror target, naming keys from their bindings.
func (m Model) setupHint() string {
	g := keys.Global(len(nav.Screens()))
	return "Set sync.mirror.target in ws.json to a folder your sync client watches (" +
		"`ws config edit`), then " + g.Refresh.Help().Desc + " (" + g.Refresh.Help().Key + ")."
}

// header is the status block, the facts of `ws sync status`. It is always
// headerLines tall so the list never moves.
func (m Model) header() []string {
	lines := make([]string, headerLines)
	if !m.loaded {
		lines[0] = theme.Muted.Render("Reading mirror…")
		return lines
	}
	s := m.state
	if s.Err != nil {
		if s.Configured {
			lines[0] = fact(mirror.Fact{Label: "Mirror", Value: s.Options.Target})
		}
		return lines
	}
	for i, f := range s.Status.Facts() {
		lines[i] = fact(f)
		if f.Label == "Stamp" {
			switch s.Status.Stamp {
			case mirror.StampWiped, mirror.StampForeign:
				lines[i] = "  " + theme.Muted.Render(pad(f.Label)) + theme.Warn.Render(f.Value)
			case mirror.StampFirst:
				lines[i] = "  " + theme.Muted.Render(pad(f.Label)+f.Value)
			}
		}
	}
	if w := mirror.SymlinkWarning(len(s.Status.Symlinks)); w != "" {
		lines[4] = "  " + theme.Warn.Render(w)
	}
	return lines
}

func pad(label string) string { return label + strings.Repeat(" ", max(12-len(label), 1)) }

func fact(f mirror.Fact) string { return "  " + theme.Muted.Render(pad(f.Label)) + f.Value }

// View implements nav.Component.
func (m Model) View() string {
	if m.modal.Open() {
		return m.modal.View()
	}
	clip := lipgloss.NewStyle().MaxWidth(m.width)
	if m.loaded && m.state.Err == nil && !m.state.Configured {
		text := theme.Icon(nav.ScreenSync.Icon()) + "Sync is not set up.\n\n" + m.setupHint()
		return lipgloss.NewStyle().Width(m.width).Render(theme.Muted.Render(text))
	}
	lines := m.header()
	for i := range lines {
		lines[i] = clip.Render(lines[i])
	}
	return lipgloss.JoinVertical(lipgloss.Left, strings.Join(lines, "\n"), "", m.list.View())
}

// KeyMap implements nav.Component.
func (m Model) KeyMap() help.KeyMap {
	if m.modal.Open() {
		return m.modal.KeyMap()
	}
	return m.list.Help(m.keys)
}
