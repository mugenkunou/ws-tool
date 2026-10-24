// Package secrets is the secrets screen (spec §6.5): pass store and credential
// helper health above the `secret ls` findings, with the keys allow, ignore the
// file, mute a directory and store in pass. The `pass` and `credential` commands
// are `:` commands: views (`pass status`, `pass audit`, `pass backup status`)
// and checklists run here, the wizards (`pass init`, `pass backup enable`,
// `credential enable|disable`) and the paged `credential status` are handed to
// the CLI.
//
// Navigation levels: list → detail (context, status, audit). esc returns to the
// list; esc in the list asks the app to go up.
package secrets

import (
	"fmt"
	"os/exec"
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
	"github.com/mugenkunou/ws-tool/internal/tui/handover"
	"github.com/mugenkunou/ws-tool/internal/tui/keys"
	"github.com/mugenkunou/ws-tool/internal/tui/modal"
	"github.com/mugenkunou/ws-tool/internal/tui/nav"
	"github.com/mugenkunou/ws-tool/internal/tui/prompt"
	"github.com/mugenkunou/ws-tool/internal/tui/reslist"
	"github.com/mugenkunou/ws-tool/internal/tui/theme"
	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

// Health is pass store + credential helper state.
type Health struct {
	Pass       secret.PassHealth
	RemoteURL  string
	CredHelper bool
	// CopyCommand is the `dotfile add` command that would put the pass store
	// into the workspace, what `ws pass status` shows as its tip. Empty when the
	// store is already there or there is no store to copy.
	CopyCommand string
}

// LoadedMsg carries scan violations and health. Other components (the
// dashboard) may observe it.
type LoadedMsg struct {
	Violations []secret.Violation
	Health     Health
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
	stepPassEntryName
	stepMuteDirInput
)

type handoverTag int

const handoverWizard handoverTag = iota

// Model is the secrets screen state.
type Model struct {
	env        env.Env
	keys       keys.SecretsMap
	detailKeys keys.DetailMap
	list       reslist.Model[secret.Violation]
	detail     viewport.Model
	level      level
	modal      modal.Model

	health      Health
	loaded      bool
	scanning    bool
	pending     secret.Violation // target of a multi-step fix
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
				if v.Severity == "CRITICAL" {
					return theme.Error.Render(t)
				}
				return t
			}},
			{Key: "location", Min: 20, Weight: 2},
			{Key: "snippet", Min: 16, Weight: 3},
		}, theme.Icon(theme.IconLock)+"No secrets found in your workspace. Nice.").Declare(reslist.Decls(actions)),
	}
}

// Load runs `secret scan` and gathers health.
func Load(e env.Env) tea.Cmd {
	return func() tea.Msg {
		h := Health{Pass: secret.CheckPass()}
		if h.Pass.GitBacked && h.Pass.GitRemote {
			h.RemoteURL = secret.GitRemoteURL()
		}
		if h.Pass.Initialized && h.Pass.StorePath != "" {
			if _, ok := secret.WorkspaceCopy(h.Pass.StorePath, e.Paths.Workspace); !ok {
				h.CopyCommand = fmt.Sprintf("dotfile add %s --dir --to %s", h.Pass.StorePath,
					filepath.Join(e.Paths.Workspace, "secrets", "dotfiles", "password-store"))
			}
		}
		if out, err := exec.Command("git", "config", "--global", "credential.helper").Output(); err == nil {
			v := strings.TrimSpace(string(out))
			h.CredHelper = strings.Contains(v, "ws") || strings.Contains(v, "git-credential-helper")
		}

		rules, err := wsconfig.LoadIgnoreRules(e.Paths.Config)
		if err != nil {
			return LoadedMsg{Health: h, Err: err}
		}
		vs, err := secret.Scan(secret.ScanOptions{
			WorkspacePath: e.Paths.Workspace,
			Engine:        wsignore.BuildEngine(rules),
			Allowlist:     wsconfig.AllowlistMap(e.Config),
			SkipDirs:      secret.MergeSkipDirs(e.Paths.Workspace, e.Config.Secret.SkipDirs, nil),
		})
		return LoadedMsg{Violations: vs, Health: h, Err: err}
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

func audit() tea.Cmd {
	return func() tea.Msg {
		res := secret.AuditPassStore(secret.CheckPass())
		if len(res.Findings) == 0 {
			return detailMsg{title: "Pass store audit", text: theme.OK.Render("✔ No problems found in the pass store.")}
		}
		var b strings.Builder
		for _, f := range res.Findings {
			if f.Entry == "" {
				b.WriteString(theme.Warn.Render("⚠ ") + f.Message + "\n")
				continue
			}
			b.WriteString(theme.Warn.Render("⚠ ") + theme.Bold.Render(f.Entry) + "  " + f.Message + "\n")
		}
		return detailMsg{title: "Pass store audit", text: strings.TrimRight(b.String(), "\n")}
	}
}

// viewCLI shows what a read-only CLI command prints, so the view is the CLI's
// own output and cannot drift from it.
func viewCLI(e env.Env, title string, args ...string) tea.Cmd {
	return func() tea.Msg {
		c := handover.WS(e.Paths, append(args, "--no-color")...)
		out, err := c.CombinedOutput()
		text := strings.TrimRight(string(out), "\n")
		if err != nil && text == "" {
			text = theme.Error.Render(err.Error())
		}
		return detailMsg{title: title, text: text}
	}
}

// runCLI is a checklist action that runs a CLI command without its own prompt
// (`--quiet` accepts it), the checklist having asked already.
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
		m.list = m.list.SetSize(msg.Width, max(msg.Height-2, 0)) // two status lines
		m.detail.SetWidth(msg.Width)
		m.detail.SetHeight(max(msg.Height-1, 0))
		m.modal = m.modal.SetSize(msg.Width, msg.Height)
		return m, nil
	case nav.RefreshMsg:
		return m.rescan()
	case LoadedMsg:
		m.scanning, m.loaded = false, true
		m.health = msg.Health
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
	case handover.DoneMsg:
		if _, ok := msg.Tag.(handoverTag); ok {
			return m.rescan()
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

// muteDir asks which directory to mute, starting from the file's (m).
func (m Model) muteDir(rows []secret.Violation) (Model, tea.Cmd) {
	var cmd tea.Cmd
	m.modal, cmd = m.modal.Prompt(stepMuteDirInput, prompt.New("Mute a directory in secret scans",
		prompt.WithHint("Saved to secret.skip_dirs in config."),
		prompt.WithValue(filepath.Dir(m.abs(rows[0])))))
	return m, cmd
}

// store asks for the pass entry name for the finding's value (s).
func (m Model) store(rows []secret.Violation) (Model, tea.Cmd) {
	if h := m.health.Pass; !h.Installed || !h.Initialized {
		return m, nav.Notify(nav.LevelWarn, "The pass store is not initialized: run :pass init")
	}
	v := rows[0]
	m.pending = v
	var cmd tea.Cmd
	m.modal, cmd = m.modal.Prompt(stepPassEntryName, prompt.New("Store this secret in pass",
		prompt.WithHint(fmt.Sprintf("Entry name for %s:%d", m.abs(v), v.Line)),
		prompt.WithValue(secret.SuggestPassEntry(v.Path, v.Snippet))))
	return m, cmd
}

// actions is every secrets key and the rows it acts on.
var actions = func() []reslist.Action[Model, secret.Violation] {
	k := keys.Secrets()
	return []reslist.Action[Model, secret.Violation]{
		{Key: k.View, Scope: reslist.Cursor, Do: Model.show},
		{Key: k.Allow, Scope: reslist.Selected, Do: Model.allow},
		{Key: k.Ignore, Scope: reslist.Selected, Do: Model.ignoreFiles},
		{Key: k.Mute, Scope: reslist.Cursor, Do: Model.muteDir},
		{Key: k.Store, Scope: reslist.Cursor, Do: Model.store},
	}
}()

// wizard hands the terminal to the CLI, then refreshes.
func (m Model) wizard(args ...string) tea.Cmd {
	return handover.Run(handover.Pause(handover.WS(m.env.Paths, args...)), handoverWizard)
}

type commandFn = func(Model, []secret.Violation, []string) (Model, tea.Cmd)

// viewCommand opens a view of what the CLI prints.
func viewCommand(title string, args ...string) commandFn {
	return func(m Model, _ []secret.Violation, _ []string) (Model, tea.Cmd) {
		return m.openDetail(title, viewCLI(m.env, title, args...))
	}
}

// wizardCommand hands the terminal to the CLI command.
func wizardCommand(args ...string) commandFn {
	return func(m Model, _ []secret.Violation, _ []string) (Model, tea.Cmd) { return m, m.wizard(args...) }
}

// cliChecklist is a checklist with one action that runs the CLI command.
func cliChecklist(command, title, desc string, args ...string) commandFn {
	return func(m Model, _ []secret.Violation, _ []string) (Model, tea.Cmd) {
		return m.confirmActions(command, title, []plan.Action{{
			ID: command, Description: desc, Execute: runCLI(m.env, args...),
		}}), nil
	}
}

// backupPush commits and pushes the pass store: needs the git remote.
func (m Model) backupPush(rows []secret.Violation, args []string) (Model, tea.Cmd) {
	if ph := m.health.Pass; !ph.GitBacked || !ph.GitRemote {
		return m, nav.Notify(nav.LevelWarn, "Pass backup is not enabled: run :pass backup enable")
	}
	return cliChecklist("pass.backup.push", "Push the pass store",
		"Commit pending changes and push the pass store to "+m.health.RemoteURL, "pass", "backup", "push")(m, rows, args)
}

// unmute scans directories again: `unmute <dir>`. A directory muted by a parent
// is refused, as the CLI does.
func (m Model) unmute(_ []secret.Violation, args []string) (Model, tea.Cmd) {
	if len(args) == 0 {
		return m, nav.Notify(nav.LevelWarn, "usage: unmute <dir>")
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
		by, exact, muted := secret.MutedBy(m.env.Paths.Workspace, cfg.Secret.SkipDirs, rel)
		switch {
		case seen[rel]:
		case !muted:
			return m, nav.Notify(nav.LevelWarn, style.AbsPath(m.env.Paths.Workspace, rel)+" is not muted")
		case !exact:
			return m, nav.Notify(nav.LevelWarn, fmt.Sprintf("%s is muted by %s; unmute that directory instead",
				style.AbsPath(m.env.Paths.Workspace, rel), style.AbsPath(m.env.Paths.Workspace, by)))
		default:
			acts = append(acts, secret.UnmuteAction(m.env.Paths.Config, m.env.Paths.Workspace, rel))
		}
		seen[rel] = true
	}
	return m.confirmActions("secret.unmute", "Unmute", acts), nil
}

// commands are the screen's `:` commands (spec §6.5): the CLI's words.
var commands = []reslist.Command[Model, secret.Violation]{
	{Name: "unmute", Args: "<dir>", Help: "scan a muted directory again", Scope: reslist.Global, Do: Model.unmute},
	{Name: "pass init", Help: "set up the pass store (hands over to the CLI)", Scope: reslist.Global, Do: wizardCommand("pass", "init")},
	{Name: "pass status", Help: "pass store and credential helper health", Scope: reslist.Global, Do: viewCommand("Pass status", "pass", "status")},
	{Name: "pass audit", Help: "check the pass store for weak spots", Scope: reslist.Global,
		Do: func(m Model, _ []secret.Violation, _ []string) (Model, tea.Cmd) {
			return m.openDetail("Pass store audit", audit())
		}},
	{Name: "pass backup enable", Help: "back the pass store up to a git remote (hands over to the CLI)", Scope: reslist.Global, Do: wizardCommand("pass", "backup", "enable")},
	{Name: "pass backup disable", Help: "stop backing the pass store up", Scope: reslist.Global,
		Do: cliChecklist("pass.backup.disable", "Disable the pass backup", "Remove the pass backup remote and its cron job", "pass", "backup", "disable")},
	{Name: "pass backup status", Help: "remote, ahead/behind, recent commits", Scope: reslist.Global, Do: viewCommand("Pass backup", "pass", "backup", "status")},
	{Name: "pass backup push", Help: "commit and push the pass store", Scope: reslist.Global, Do: Model.backupPush},
	{Name: "credential enable", Help: "set up the git credential helper (hands over to the CLI)", Scope: reslist.Global, Do: wizardCommand("credential", "enable")},
	{Name: "credential disable", Help: "remove the git credential helper (hands over to the CLI)", Scope: reslist.Global, Do: wizardCommand("credential", "disable")},
	{Name: "credential status", Help: "credential helper per remote (paged)", Scope: reslist.Global,
		Do: func(m Model, _ []secret.Violation, _ []string) (Model, tea.Cmd) {
			return m, handover.Run(handover.Paged(handover.WS(m.env.Paths, "credential", "status")), handoverWizard)
		}},
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
	case stepPassEntryName:
		r := msg.(prompt.ResultMsg)
		if r.Cancelled {
			return m, nil
		}
		entry := strings.TrimSpace(r.Value)
		if err := validEntryName(entry); err != nil {
			return m, nav.Notify(nav.LevelError, err.Error())
		}
		return m.confirmActions("secret.store", "Store in pass",
			[]plan.Action{secret.StoreAction(m.env.Paths.Config, m.env.Paths.Workspace, m.pending, entry)}), nil
	case stepMuteDirInput:
		r := msg.(prompt.ResultMsg)
		if r.Cancelled {
			return m, nil
		}
		rel, err := secret.WorkspaceRelDir(m.env.Paths.Workspace, r.Value)
		if err != nil {
			return m, nav.Notify(nav.LevelError, err.Error())
		}
		return m.confirmActions("secret.mute", "Mute",
			[]plan.Action{secret.MuteAction(m.env.Paths.Config, m.env.Paths.Workspace, rel)}), nil
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

// validEntryName rejects an entry name that would leave the pass store, as
// `secret store` does.
func validEntryName(entry string) error {
	if entry == "" {
		return fmt.Errorf("the pass entry name is empty")
	}
	if strings.HasPrefix(entry, "/") || strings.HasPrefix(entry, "~") {
		return fmt.Errorf("pass entry %q must be a name inside the store, such as myapp/password", entry)
	}
	for _, part := range strings.Split(entry, "/") {
		if part == ".." || part == "." || part == "" {
			return fmt.Errorf("pass entry %q is not a valid name", entry)
		}
	}
	return nil
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

func mark(ok bool, label string) string {
	if ok {
		return theme.OK.Render("✔ " + label)
	}
	return theme.Muted.Render("✘ " + label)
}

func (m Model) healthLine() string {
	if !m.loaded {
		return theme.Muted.Render("Checking pass store…")
	}
	h := m.health.Pass
	parts := []string{mark(h.Installed && h.GPGAvailable, "pass")}
	if h.Initialized {
		parts = append(parts, mark(true, fmt.Sprintf("store (%s)", format.Plural(h.EntryCount, "entry"))))
	} else {
		parts = append(parts, mark(false, "store"))
	}
	parts = append(parts, mark(h.GitBacked, "git"))
	if h.GitBacked {
		if h.GitRemote {
			parts = append(parts, mark(true, "remote"))
		} else {
			parts = append(parts, theme.Warn.Render("⚠ no remote"))
		}
	}
	parts = append(parts, mark(m.health.CredHelper, "credential helper"))
	if m.health.CopyCommand != "" {
		parts = append(parts, theme.Muted.Render("ℹ no workspace copy"))
	}
	return strings.Join(parts, "  ")
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
	return lipgloss.JoinVertical(lipgloss.Left, clip.Render(m.healthLine()), clip.Render(m.statusLine()), m.list.View())
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
