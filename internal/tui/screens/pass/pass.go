// Package pass is the Pass screen (spec §5 pass): the Unix Password Store's
// health, with the `:` commands that keep it healthy. `init`, `backup enable`
// and `backup disable` hand the terminal to the CLI's wizards; `status` and
// `backup status` open as read-only views; `audit` is a view of `ws pass audit`;
// `backup push` is a checklist. The screen has no verb keys of its own.
//
// Navigation: the one list of health facts; escape asks the app to go up.
package pass

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/mugenkunou/ws-tool/internal/pass"
	"github.com/mugenkunou/ws-tool/internal/plan"
	"github.com/mugenkunou/ws-tool/internal/resource"
	"github.com/mugenkunou/ws-tool/internal/tui/confirm"
	"github.com/mugenkunou/ws-tool/internal/tui/env"
	"github.com/mugenkunou/ws-tool/internal/tui/format"
	"github.com/mugenkunou/ws-tool/internal/tui/handover"
	"github.com/mugenkunou/ws-tool/internal/tui/keys"
	"github.com/mugenkunou/ws-tool/internal/tui/modal"
	"github.com/mugenkunou/ws-tool/internal/tui/nav"
	"github.com/mugenkunou/ws-tool/internal/tui/reslist"
	"github.com/mugenkunou/ws-tool/internal/tui/theme"
)

// LoadedMsg carries the pass store's state. Other components (the Dashboard)
// may observe it.
type LoadedMsg struct {
	Pass      pass.PassHealth
	RemoteURL string
	CredOK    bool   // the global git credential helper points at ws and works
	Copy      string // where the store is linked into the workspace, or ""
	Err       error
}

// fact is one row of the health list.
type fact struct {
	Label string
	OK    bool
	Warn  bool   // true renders a warning mark instead of check/cross
	Value string // detail after the label
}

// detailMsg opens the detail view.
type detailMsg struct {
	title, text string
}

type level int

const (
	levelList level = iota
	levelDetail
)

type step int

const (
	stepFix  step = iota
	stepMenu      // the `.` menu
)

// handoverWizard tags the handover of a pass wizard, so a refresh follows.
type handoverWizard struct{}

// Model is the Pass screen state.
type Model struct {
	env        env.Env
	keys       keys.PassMap
	detailKeys keys.DetailMap
	list       reslist.Model[fact]
	detail     viewport.Model
	level      level
	modal      modal.Model

	state       LoadedMsg
	loaded      bool
	detailTitle string
	width       int
	height      int
}

// New creates the screen.
func New(e env.Env) Model {
	dk := keys.Detail()
	vp := viewport.New()
	vp.KeyMap = dk.Viewport
	vp.SoftWrap = true
	return Model{
		env:        e,
		keys:       keys.Pass(),
		detailKeys: dk,
		detail:     vp,
		list: reslist.New(schema(), []reslist.Col[fact]{
			{Key: "label", Min: 20, Weight: 1},
			{Key: "value", Min: 16, Weight: 2, Style: func(_ fact, t string) string { return theme.Muted.Render(t) }},
		}, "Nothing to show.").Declare(nil),
	}
}

// schema describes the health rows for the list and its filter and sort.
func schema() resource.Schema[fact] {
	return resource.Schema[fact]{
		Name: "fact",
		ID:   func(f fact) string { return f.Label },
		Fields: []resource.Field[fact]{
			{Key: "label", Title: "Health", Value: func(f fact) any { return f.Label }},
			{Key: "value", Title: "Detail", Value: func(f fact) any { return f.Value },
				Text: func(f fact) string { return f.Value }},
		},
		Columns: []string{"label", "value"},
	}
}

// Load reads the pass store's state, as `ws pass status` does.
func Load(e env.Env) tea.Cmd {
	return func() tea.Msg {
		h := pass.CheckPass()
		m := LoadedMsg{Pass: h}
		if h.GitBacked && h.GitRemote {
			m.RemoteURL = pass.GitRemoteURL()
		}
		if h.Initialized && h.StorePath != "" {
			if target, ok := pass.WorkspaceCopy(h.StorePath, e.Paths.Workspace); ok {
				m.Copy = target
			}
		}
		// The credential helper state is gathered by the Credential screen;
		// pass status also shows it, but this screen owns the pass facts only.
		return m
	}
}

// Init implements nav.Component.
func (m Model) Init() tea.Cmd { return Load(m.env) }

// Declared lists the screen's actions and their scopes. There are none.
func (m Model) Declared() []reslist.Decl { return m.list.Declared() }

// CapturesKeys implements nav.Component.
func (m Model) CapturesKeys() bool { return m.modal.Open() || m.list.Editing() }

// metrics returns the facts in the order the screen shows them.
func (m LoadedMsg) facts() []fact {
	h := m.Pass
	out := []fact{{Label: "gpg available", OK: h.GPGAvailable}}
	out = append(out, fact{Label: "pass installed", OK: h.Installed})
	if h.Initialized {
		out = append(out, fact{Label: "store initialized", OK: true,
			Value: fmt.Sprintf("%s, %s", h.StorePath, format.Plural(h.EntryCount, "entry"))})
	} else {
		out = append(out, fact{Label: "store initialized"})
	}
	out = append(out, fact{Label: "git-backed", OK: h.GitBacked})
	if h.GitBacked {
		if h.GitRemote {
			out = append(out, fact{Label: "remote", OK: true, Value: m.RemoteURL})
		} else {
			out = append(out, fact{Label: "no remote", Warn: true, Value: "secrets live only on this machine"})
		}
	}
	if m.Copy != "" {
		out = append(out, fact{Label: "workspace copy", OK: true, Value: m.Copy})
	}
	return out
}

func (m Model) reload() (Model, tea.Cmd) {
	m.list = m.list.SetLoading()
	return m, Load(m.env)
}

// wizard hands the terminal to the CLI, then refreshes.
func (m Model) wizard(args ...string) tea.Cmd {
	return handover.Run(handover.Pause(handover.WS(m.env.Paths, args...)), handoverWizard{})
}

// runCLI is a checklist action that runs a CLI command without its own prompt.
func runCLI(e env.Env, args ...string) func() error {
	return func() error {
		out, err := handover.WS(e.Paths, append(args, "--quiet")...).CombinedOutput()
		if err != nil {
			if msg := strings.TrimSpace(string(out)); msg != "" {
				return fmt.Errorf("%s", msg)
			}
			return err
		}
		return nil
	}
}

// viewCLI shows what a read-only CLI command prints, so the view is the CLI's
// own output.
func viewCLI(title string, args ...string) func(*Model) tea.Cmd {
	return func(m *Model) tea.Cmd {
		return func() tea.Msg {
			c := handover.WS(m.env.Paths, append(args, "--no-color")...)
			out, err := c.CombinedOutput()
			text := strings.TrimRight(string(out), "\n")
			if err != nil && text == "" {
				text = theme.Error.Render(err.Error())
			}
			return detailMsg{title: title, text: text}
		}
	}
}

// audit reads `ws pass audit` output as a view.
func (m Model) audit() tea.Cmd {
	return func() tea.Msg {
		res := pass.AuditPassStore(pass.CheckPass())
		if len(res.Findings) == 0 {
			return detailMsg{title: "Pass store audit", text: theme.OK.Render("✔ No problems found in the pass store.")}
		}
		var b strings.Builder
		for _, f := range res.Findings {
			if f.Entry == "" {
				b.WriteString(theme.Warn.Render("! ") + f.Message + "\n")
				continue
			}
			b.WriteString(theme.Warn.Render("! ") + theme.Bold.Render(f.Entry) + "  " + f.Message + "\n")
		}
		return detailMsg{title: "Pass store audit", text: strings.TrimRight(b.String(), "\n")}
	}
}

// confirm opens a checklist with one action that runs the CLI command.
func (m Model) confirm(command, title, desc string, args ...string) Model {
	m.modal = m.modal.Confirm(stepFix, confirm.New(title, plan.Plan{Command: command, Actions: []plan.Action{{
		ID: command, Description: desc, Execute: runCLI(m.env, args...),
	}}}))
	return m
}

// push commits and pushes the pass store: needs the git remote.
func (m Model) push() (Model, tea.Cmd) {
	if !m.loaded {
		return m, nav.Notify(nav.LevelInfo, "Still reading the pass store…")
	}
	h := m.state.Pass
	if !h.GitBacked || !h.GitRemote {
		return m, nav.Notify(nav.LevelWarn, "Pass backup is not enabled: press . then e (backup enable)")
	}
	return m.confirm("pass.backup.push", "Push the pass store",
		"Commit pending changes and push the pass store to "+m.state.RemoteURL,
		"pass", "backup", "push"), nil
}

// commands are the `.` menu: the CLI's words after `pass`.
var commands = []reslist.Command[Model, fact]{
	{Name: "init", Menu: "i", Help: "set up the pass store (hands over to the CLI)", Scope: reslist.Global,
		Do: func(m Model, _ []fact, _ []string) (Model, tea.Cmd) { return m, m.wizard("pass", "init") }},
	{Name: "audit", Menu: "a", Help: "check the pass store for weak spots", Scope: reslist.Global,
		Do: func(m Model, _ []fact, _ []string) (Model, tea.Cmd) {
			return m.openDetail("Pass store audit", m.audit())
		}},
	{Name: "backup enable", Menu: "e", Help: "back the pass store up to a git remote (hands over to the CLI)", Scope: reslist.Global,
		Do: func(m Model, _ []fact, _ []string) (Model, tea.Cmd) { return m, m.wizard("pass", "backup", "enable") }},
	{Name: "backup disable", Menu: "d", Help: "stop backing the pass store up", Scope: reslist.Global,
		Do: func(m Model, _ []fact, _ []string) (Model, tea.Cmd) {
			return m.confirm("pass.backup.disable", "Disable the pass backup",
				"Remove the pass backup remote and its cron job", "pass", "backup", "disable"), nil
		}},
	{Name: "backup status", Menu: "s", Help: "remote, ahead/behind, recent commits", Scope: reslist.Global,
		Do: func(m Model, _ []fact, _ []string) (Model, tea.Cmd) {
			return m.openDetail("Pass backup", viewCLI("Pass backup", "pass", "backup", "status")(&m))
		}},
	{Name: "backup push", Menu: "p", Help: "commit and push the pass store", Scope: reslist.Global,
		Do: func(m Model, _ []fact, _ []string) (Model, tea.Cmd) { return m.push() }},
}

// Commands implements nav.Commander.
func (m Model) Commands() []nav.CommandInfo { return reslist.CommandInfos(commands) }

func (m Model) openDetail(title string, cmd tea.Cmd) (Model, tea.Cmd) {
	m.level = levelDetail
	m.detailTitle = title
	m.detail.SetContent(theme.Muted.Render("Loading…"))
	m.detail.GotoTop()
	return m, cmd
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
		m.list = m.list.SetSize(msg.Width, msg.Height)
		m.detail.SetWidth(msg.Width)
		m.detail.SetHeight(max(msg.Height-1, 0))
		m.modal = m.modal.SetSize(msg.Width, msg.Height)
		return m, nil
	case nav.RefreshMsg:
		return m.reload()
	case LoadedMsg:
		m.loaded, m.state = true, msg
		if msg.Err != nil {
			m.list = m.list.SetError(msg.Err)
			return m, nil
		}
		m.list = m.list.SetItems(msg.facts())
		return m, nil
	case nav.MenuMsg:
		if m.modal.Open() || m.level != levelList {
			return m, nil
		}
		m.modal = m.modal.Choose(stepMenu, reslist.MenuChooser("Pass actions", commands))
		return m, nil
	case detailMsg:
		m.detailTitle = msg.title
		m.detail.SetContent(msg.text)
		return m, nil
	case handover.DoneMsg:
		if _, ok := msg.Tag.(handoverWizard); ok {
			if msg.Err != nil {
				return m, nav.Notify(nav.LevelError, msg.Err.Error())
			}
			return m.reload()
		}
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
	if m.level == levelDetail {
		if key.Matches(k, m.detailKeys.Back) {
			m.level = levelList
			return m, nil
		}
		var cmd tea.Cmd
		m.detail, cmd = m.detail.Update(k)
		return m, cmd
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

// View implements nav.Component.
func (m Model) View() string {
	switch {
	case m.modal.Open():
		return m.modal.View()
	case m.level == levelDetail:
		clip := lipgloss.NewStyle().MaxWidth(m.width)
		return lipgloss.JoinVertical(lipgloss.Left, clip.Render(theme.Bold.Render(m.detailTitle)), m.detail.View())
	}
	return m.list.View()
}

// KeyMap implements nav.Component.
func (m Model) KeyMap() help.KeyMap {
	switch {
	case m.modal.Open():
		return m.modal.KeyMap()
	case m.level == levelDetail:
		return m.detailKeys
	}
	return m.list.Help(m.keys)
}
