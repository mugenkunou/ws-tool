// Package secrets is the secrets screen (spec §6.5): the `secret ls` findings,
// with the keys allow, ignore the file and mute a directory, and the `:` commands
// skip and unskip.
//
// Navigation levels: list → detail (context). esc returns to the list; esc in
// the list asks the app to go up.
package secrets

import (
	"fmt"
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	wsignore "github.com/mugenkunou/ws-tool/internal/ignore"
	"github.com/mugenkunou/ws-tool/internal/plan"
	"github.com/mugenkunou/ws-tool/internal/secret"
	"github.com/mugenkunou/ws-tool/internal/style"
	"github.com/mugenkunou/ws-tool/internal/tui/confirm"
	"github.com/mugenkunou/ws-tool/internal/tui/env"
	"github.com/mugenkunou/ws-tool/internal/tui/format"
	"github.com/mugenkunou/ws-tool/internal/tui/keys"
	"github.com/mugenkunou/ws-tool/internal/tui/modal"
	"github.com/mugenkunou/ws-tool/internal/tui/nav"
	"github.com/mugenkunou/ws-tool/internal/tui/prompt"
	"github.com/mugenkunou/ws-tool/internal/tui/reslist"
	"github.com/mugenkunou/ws-tool/internal/tui/theme"
	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

// LoadedMsg carries scan violations. Other components (the dashboard) may
// observe it.
type LoadedMsg struct {
	Violations []secret.Violation
	Err        error
}

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
	stepFix step = iota
	stepSkipDirInput
)

// Model is the secrets screen state.
type Model struct {
	env        env.Env
	keys       keys.SecretsMap
	detailKeys keys.DetailMap
	list       reslist.Model[secret.Violation]
	detail     viewport.Model
	level      level
	modal      modal.Model

	loaded      bool
	scanning    bool
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
		keys:       keys.Secrets(),
		detailKeys: dk,
		detail:     vp,
		scanning:   true,
		list: reslist.New(secret.Schema(e.View(), e.Paths.Workspace), []reslist.Col[secret.Violation]{
			{Key: "severity", Min: 8, Style: func(v secret.Violation, t string) string {
				switch {
				case v.Muted:
					return theme.Muted.Render(t)
				case v.Severity == "WARNING":
					return theme.Warn.Render(t)
				}
				return t
			}},
			{Key: "location", Min: 20, Weight: 2},
			{Key: "snippet", Min: 16, Weight: 3},
		}, theme.Icon(theme.IconLock)+"No secrets found in your workspace. Nice.").Declare(reslist.Decls(actions)),
	}
}

// Load runs `secret scan`.
func Load(e env.Env) tea.Cmd {
	return func() tea.Msg {
		rules, err := wsconfig.LoadIgnoreRules(e.Paths.Config)
		if err != nil {
			return LoadedMsg{Err: err}
		}
		vs, err := secret.Scan(secret.ScanOptions{
			WorkspacePath: e.Paths.Workspace,
			Engine:        wsignore.BuildEngine(rules),
			Allowlist:     wsconfig.AllowlistMap(e.Config),
			SkipDirs:      secret.MergeSkipDirs(e.Paths.Workspace, e.Config.Secret.SkipDirs, nil),
			Muted:         wsconfig.MutedMap(e.Config),
		})
		return LoadedMsg{Violations: vs, Err: err}
	}
}

func viewContext(e env.Env, v secret.Violation) tea.Cmd {
	return func() tea.Msg {
		abs := style.AbsPath(e.Paths.Workspace, v.Path)
		title := fmt.Sprintf("%s:%d", abs, v.Line)
		ctx, err := secret.GetFileContext(abs, v.Line, 5)
		if err != nil {
			return detailMsg{title: title, text: theme.Error.Render(err.Error())}
		}
		var b strings.Builder
		for _, cl := range ctx {
			mark := "   "
			line := cl.Content
			if cl.IsMatch {
				mark = theme.Warn.Render(" → ")
				line = theme.Bold.Render(line)
			}
			b.WriteString(fmt.Sprintf("%s%s %s\n", mark, theme.Muted.Render(fmt.Sprintf("%4d", cl.Number)), line))
		}
		return detailMsg{title: title, text: strings.TrimRight(b.String(), "\n")}
	}
}

// Init starts the first scan.
func (m Model) Init() tea.Cmd { return Load(m.env) }

// Declared lists the screen's actions and their scopes.
func (m Model) Declared() []reslist.Decl { return m.list.Declared() }

// CapturesKeys implements nav.Component.
func (m Model) CapturesKeys() bool { return m.modal.Open() || m.list.Editing() }

func (m Model) rescan() (Model, tea.Cmd) {
	m.scanning = true
	m.list = m.list.SetLoading()
	return m, Load(m.env)
}

// Update implements nav.Component.
func (m Model) Update(msg tea.Msg) (nav.Component, tea.Cmd) {
	if st, ok := m.modal.Match(msg); ok {
		m.modal = m.modal.Close()
		return m.dialogResult(st.(step), msg)
	}

	switch msg := msg.(type) {
	case nav.ResizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.list = m.list.SetSize(msg.Width, max(msg.Height-1, 0)) // one scan-status line above the list
		m.detail.SetWidth(msg.Width)
		m.detail.SetHeight(max(msg.Height-1, 0))
		m.modal = m.modal.SetSize(msg.Width, msg.Height)
		return m, nil
	case nav.RefreshMsg:
		return m.rescan()
	case LoadedMsg:
		m.scanning, m.loaded = false, true
		if msg.Err != nil {
			m.list = m.list.SetError(msg.Err)
			return m, nil
		}
		m.list = m.list.SetItems(msg.Violations)
		return m, nil
	case nav.FilterMsg:
		m.list = m.list.SetFilter(msg.Filter)
		return m, nil
	case nav.CommandMsg:
		m.level = levelList
		next, cmd, _ := reslist.RunCommand(m, m.list, commands, msg)
		return next, cmd
	case detailMsg:
		m.detailTitle = msg.title
		m.detail.SetContent(msg.text)
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
	return m.updateList(k)
}

func (m Model) openDetail(title string, cmd tea.Cmd) (Model, tea.Cmd) {
	m.level = levelDetail
	m.detailTitle = title
	m.detail.SetContent(theme.Muted.Render("Loading…"))
	m.detail.GotoTop()
	return m, cmd
}

// The functions below are shared by the keys and the `:` commands.

// show opens the finding in context (enter).
func (m Model) show(rows []secret.Violation) (Model, tea.Cmd) {
	return m.openDetail("", viewContext(m.env, rows[0]))
}

// allow allowlists the findings as false positives (a).
func (m Model) allow(rows []secret.Violation) (Model, tea.Cmd) {
	acts := secret.AllowActions(m.env.Paths.Config, m.env.Paths.Workspace, rows, nil)
	return m.confirmActions("secret.allow", "Allow", acts), nil
}

// ignoreFiles stops syncing the findings' files (i), as `ignore add` does.
func (m Model) ignoreFiles(rows []secret.Violation) (Model, tea.Cmd) {
	var acts []plan.Action
	seen := map[string]bool{}
	for _, v := range rows {
		if a := m.ignoreAction(v); !seen[a.ID] {
			seen[a.ID] = true
			acts = append(acts, a)
		}
	}
	return m.confirmActions("ignore.add", "Ignore", acts), nil
}

// mute toggles the selected findings' quiet state directly (m): an unmuted
// finding is muted and a muted one is unmuted, so the key reads the same way
// `ignore mute` does — it never deletes the finding, only its volume.
func (m Model) mute(rows []secret.Violation) (Model, tea.Cmd) {
	var toMute, toUnmute []secret.Violation
	for _, v := range rows {
		if v.Muted {
			toUnmute = append(toUnmute, v)
		} else {
			toMute = append(toMute, v)
		}
	}
	acts := append(
		secret.MuteActions(m.env.Paths.Config, m.env.Paths.Workspace, toMute),
		secret.UnmuteActions(m.env.Paths.Config, m.env.Paths.Workspace, toUnmute)...,
	)
	return m.confirmActions("secret.mute", "Mute", acts), nil
}

// actions is every secrets key and the rows it acts on.
var actions = func() []reslist.Action[Model, secret.Violation] {
	k := keys.Secrets()
	return []reslist.Action[Model, secret.Violation]{
		{Key: k.View, Scope: reslist.Cursor, Do: Model.show},
		{Key: k.Allow, Scope: reslist.Selected, Do: Model.allow},
		{Key: k.Ignore, Scope: reslist.Selected, Do: Model.ignoreFiles},
		{Key: k.Mute, Scope: reslist.Selected, Do: Model.mute},
	}
}()

// skip asks which directory to stop scanning entirely: `skip <dir>` (or a
// prompt, pre-filled with the cursor finding's directory, when no args).
func (m Model) skip(rows []secret.Violation, args []string) (Model, tea.Cmd) {
	if len(args) > 0 {
		return m.skipDir(args[0])
	}
	var cmd tea.Cmd
	m.modal, cmd = m.modal.Prompt(stepSkipDirInput, prompt.New("Skip a directory in secret scans",
		prompt.WithHint("Saved to secret.skip_dirs in config. Nothing under it is scanned again."),
		prompt.WithValue(filepath.Dir(m.abs(rows[0])))))
	return m, cmd
}

func (m Model) skipDir(raw string) (Model, tea.Cmd) {
	rel, err := secret.WorkspaceRelDir(m.env.Paths.Workspace, raw)
	if err != nil {
		return m, nav.Notify(nav.LevelError, err.Error())
	}
	return m.confirmActions("secret.skip", "Skip",
		[]plan.Action{secret.SkipAction(m.env.Paths.Config, m.env.Paths.Workspace, rel)}), nil
}

// unskip scans directories again: `unskip <dir>`. A directory skipped by a
// parent is refused, as the CLI does.
func (m Model) unskip(_ []secret.Violation, args []string) (Model, tea.Cmd) {
	if len(args) == 0 {
		return m, nav.Notify(nav.LevelWarn, "usage: unskip <dir>")
	}
	cfg, err := wsconfig.Load(m.env.Paths.Config)
	if err != nil {
		return m, nav.Notify(nav.LevelError, err.Error())
	}
	var acts []plan.Action
	seen := map[string]bool{}
	for _, a := range args {
		rel, err := secret.WorkspaceRelDir(m.env.Paths.Workspace, a)
		if err != nil {
			return m, nav.Notify(nav.LevelError, err.Error())
		}
		by, exact, skipped := secret.SkippedBy(m.env.Paths.Workspace, cfg.Secret.SkipDirs, rel)
		switch {
		case seen[rel]:
		case !skipped:
			return m, nav.Notify(nav.LevelWarn, style.AbsPath(m.env.Paths.Workspace, rel)+" is not skipped")
		case !exact:
			return m, nav.Notify(nav.LevelWarn, fmt.Sprintf("%s is skipped by %s; unskip that directory instead",
				style.AbsPath(m.env.Paths.Workspace, rel), style.AbsPath(m.env.Paths.Workspace, by)))
		default:
			acts = append(acts, secret.UnskipAction(m.env.Paths.Config, m.env.Paths.Workspace, rel))
		}
		seen[rel] = true
	}
	return m.confirmActions("secret.unskip", "Unskip", acts), nil
}

// commands are the screen's `:` commands (spec §6.5): the CLI's words.
var commands = []reslist.Command[Model, secret.Violation]{
	{Name: "skip", Args: "[dir]", Help: "stop scanning a directory entirely", Scope: reslist.Cursor, Do: Model.skip},
	{Name: "unskip", Args: "<dir>", Help: "scan a skipped directory again", Scope: reslist.Global, Do: Model.unskip},
}

// Commands implements nav.Commander.
func (m Model) Commands() []nav.CommandInfo { return reslist.CommandInfos(commands) }

func (m Model) updateList(k tea.KeyPressMsg) (nav.Component, tea.Cmd) {
	if l, cmd, ok := m.list.HandleKey(k); ok {
		m.list = l
		return m, cmd
	}
	if next, cmd, ok := reslist.Run(m, m.list, actions, k); ok {
		return next, cmd
	}
	switch {
	case key.Matches(k, m.keys.Back):
		return m, nav.Up
	}
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(k)
	return m, cmd
}

func (m Model) dialogResult(st step, msg tea.Msg) (nav.Component, tea.Cmd) {
	switch st {
	case stepSkipDirInput:
		r := msg.(prompt.ResultMsg)
		if r.Cancelled {
			return m, nil
		}
		return m.skipDir(r.Value)
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
	m, cmd := m.rescan()
	return m, tea.Batch(nav.Notify(level, confirm.Summary(done.Result)), cmd)
}

func (m Model) abs(v secret.Violation) string { return style.AbsPath(m.env.Paths.Workspace, v.Path) }

// confirmActions opens the checklist for a plan.
func (m Model) confirmActions(command, title string, acts []plan.Action) Model {
	m.modal = m.modal.Confirm(stepFix, confirm.New(title, plan.Plan{Command: command, Actions: acts}))
	return m
}

// ignoreAction stops syncing the finding's file, as `ignore add` does.
func (m Model) ignoreAction(v secret.Violation) plan.Action {
	p, rel := m.env.Paths, v.Path
	abs := m.abs(v)
	return plan.Action{
		ID:          "ignore-add-" + abs,
		Description: fmt.Sprintf("Ignore %s (ws.json ignore_rules.exclude)", abs),
		Execute: func() error {
			if _, err := wsconfig.AddIgnoreExclude(p.Config, rel, ""); err != nil {
				return err
			}
			rules, err := wsconfig.LoadIgnoreRules(p.Config)
			if err != nil {
				return err
			}
			return wsignore.WriteMegaignore(filepath.Join(p.Workspace, ".megaignore"), rules)
		},
	}
}

func (m Model) statusLine() string {
	if m.scanning {
		return theme.Muted.Render("Scanning for secrets…")
	}
	return format.Plural(len(m.list.Items()), "finding")
}

// View implements nav.Component.
func (m Model) View() string {
	clip := lipgloss.NewStyle().MaxWidth(m.width)
	switch {
	case m.modal.Open():
		return m.modal.View()
	case m.level == levelDetail:
		return lipgloss.JoinVertical(lipgloss.Left, clip.Render(theme.Bold.Render(m.detailTitle)), m.detail.View())
	}
	return lipgloss.JoinVertical(lipgloss.Left, clip.Render(m.statusLine()), m.list.View())
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
