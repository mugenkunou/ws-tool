// Package secrets is the secrets screen: pass store and credential helper
// health, `secret scan` violations with the `secret fix` actions (allowlist,
// exclude, move to pass, skip dir), a pass store audit, and pass git
// (push, log, remote). Setup wizards (`secret setup`,
// `git-credential-helper setup|status|disconnect`) are handed to the CLI.
//
// Navigation levels: list → detail (context, audit, log). esc returns to the
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
	stepSkipDirInput
	stepRemoteURL
	stepPassGit
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
			return detailMsg{title: "Pass store audit", text: theme.OK.Render("✔ No findings.")}
		}
		var b strings.Builder
		for _, f := range res.Findings {
			b.WriteString(theme.Warn.Render("⚠ ") + theme.Bold.Render(f.Entry) + "  " + f.Message + "\n")
		}
		return detailMsg{title: "Pass store audit", text: strings.TrimRight(b.String(), "\n")}
	}
}

func passLog() tea.Cmd {
	return func() tea.Msg {
		out, err := secret.GitLog(30)
		if err != nil {
			return detailMsg{title: "Pass store git log", text: theme.Error.Render(err.Error())}
		}
		if strings.TrimSpace(out) == "" {
			out = theme.Muted.Render("(no commits)")
		}
		return detailMsg{title: "Pass store git log", text: strings.TrimRight(out, "\n")}
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

func (m Model) wizard(args ...string) tea.Cmd {
	return handover.Run(handover.Pause(handover.WS(m.env.Paths, args...)), handoverWizard)
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

// actions is every secrets key and the rows it acts on.
var actions = func() []reslist.Action[Model, secret.Violation] {
	k := keys.Secrets()
	global := func(f func(Model) (Model, tea.Cmd)) func(Model, []secret.Violation) (Model, tea.Cmd) {
		return func(m Model, _ []secret.Violation) (Model, tea.Cmd) { return f(m) }
	}
	each := func(title string, build func(Model, secret.Violation) plan.Action) func(Model, []secret.Violation) (Model, tea.Cmd) {
		return func(m Model, rows []secret.Violation) (Model, tea.Cmd) {
			return m.confirmEach(stepFix, title, rows, build), nil
		}
	}
	return []reslist.Action[Model, secret.Violation]{
		{Key: k.View, Scope: reslist.Cursor, Do: func(m Model, rows []secret.Violation) (Model, tea.Cmd) {
			return m.openDetail("", viewContext(m.env, rows[0]))
		}},
		{Key: k.Allowlist, Scope: reslist.Selected, Do: each("Allowlist", Model.allowAction)},
		{Key: k.AllowAll, Scope: reslist.All, Do: each("Allowlist", Model.allowAction)},
		{Key: k.Exclude, Scope: reslist.Selected, Do: each("Exclude from sync", Model.excludeAction)},
		{Key: k.StorePass, Scope: reslist.Cursor, Do: Model.promptStorePass},
		{Key: k.SkipDir, Scope: reslist.Cursor, Do: func(m Model, rows []secret.Violation) (Model, tea.Cmd) {
			m.pending = rows[0]
			var cmd tea.Cmd
			m.modal, cmd = m.modal.Prompt(stepSkipDirInput, prompt.New("Skip a directory in secret scans",
				prompt.WithHint("Saved to secret.skip_dirs in config."),
				prompt.WithValue(filepath.Dir(m.abs(rows[0])))))
			return m, cmd
		}},
		{Key: k.Audit, Scope: reslist.Global, Do: global(func(m Model) (Model, tea.Cmd) {
			return m.openDetail("Pass store audit", audit())
		})},
		{Key: k.PassLog, Scope: reslist.Global, Do: global(func(m Model) (Model, tea.Cmd) {
			if !m.health.Pass.GitBacked {
				return m, nav.Notify(nav.LevelWarn, "Pass store is not git-backed — press S to run setup")
			}
			return m.openDetail("Pass store git log", passLog())
		})},
		{Key: k.PassPush, Scope: reslist.Global, Do: global(func(m Model) (Model, tea.Cmd) {
			if ph := m.health.Pass; !ph.GitBacked || !ph.GitRemote {
				return m, nav.Notify(nav.LevelWarn, "Pass store has no git remote — press o to set one")
			}
			return m.confirmOne(stepPassGit, "Push pass store", plan.Action{
				ID: "pass-push", Description: "Push pass store to " + m.health.RemoteURL, Execute: secret.GitPush,
			}), nil
		})},
		{Key: k.PassRemote, Scope: reslist.Global, Do: global(func(m Model) (Model, tea.Cmd) {
			if !m.health.Pass.GitBacked {
				return m, nav.Notify(nav.LevelWarn, "Pass store is not git-backed — press S to run setup")
			}
			var cmd tea.Cmd
			m.modal, cmd = m.modal.Prompt(stepRemoteURL, prompt.New("Set pass store git remote",
				prompt.WithValue(m.health.RemoteURL), prompt.WithHint("Use a private repository.")))
			return m, cmd
		})},
		{Key: k.Setup, Scope: reslist.Global, Do: global(func(m Model) (Model, tea.Cmd) { return m, m.wizard("secret", "setup") })},
		{Key: k.CredSetup, Scope: reslist.Global, Do: global(func(m Model) (Model, tea.Cmd) {
			return m, m.wizard("git-credential-helper", "setup")
		})},
		{Key: k.CredStatus, Scope: reslist.Global, Do: global(func(m Model) (Model, tea.Cmd) {
			return m, handover.Run(handover.Paged(handover.WS(m.env.Paths, "git-credential-helper", "status")), handoverWizard)
		})},
		{Key: k.CredDisconnect, Scope: reslist.Global, Do: global(func(m Model) (Model, tea.Cmd) {
			return m, m.wizard("git-credential-helper", "disconnect")
		})},
	}
}()

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
	case key.Matches(k, m.keys.Refresh):
		return m.rescan()
	}
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(k)
	return m, cmd
}

func (m Model) promptStorePass(rows []secret.Violation) (Model, tea.Cmd) {
	if !m.health.Pass.Initialized {
		return m, nav.Notify(nav.LevelWarn, "Pass store not initialized — press S to run setup")
	}
	v := rows[0]
	m.pending = v
	var cmd tea.Cmd
	m.modal, cmd = m.modal.Prompt(stepPassEntryName, prompt.New("Store this secret in pass",
		prompt.WithHint(fmt.Sprintf("Entry name for %s:%d", m.abs(v), v.Line)),
		prompt.WithValue(secret.SuggestPassEntry(v.Path, v.Snippet))))
	return m, cmd
}

func (m Model) dialogResult(st step, msg tea.Msg) (nav.Component, tea.Cmd) {
	switch st {
	case stepPassEntryName:
		r := msg.(prompt.ResultMsg)
		if r.Cancelled {
			return m, nil
		}
		return m.confirmOne(stepFix, "Move to pass", m.passAction(m.pending, r.Value)), nil
	case stepSkipDirInput:
		r := msg.(prompt.ResultMsg)
		if r.Cancelled {
			return m, nil
		}
		rel, err := secret.WorkspaceRelDir(m.env.Paths.Workspace, r.Value)
		if err != nil {
			return m, nav.Notify(nav.LevelError, err.Error())
		}
		cfgPath := m.env.Paths.Config
		return m.confirmOne(stepFix, "Skip directory", plan.Action{
			ID:          "skip-dir-" + rel,
			Description: "Skip " + style.AbsPath(m.env.Paths.Workspace, rel) + " in secret scans",
			Execute:     func() error { return secret.AddSkipDir(cfgPath, rel) },
		}), nil
	case stepRemoteURL:
		r := msg.(prompt.ResultMsg)
		if r.Cancelled {
			return m, nil
		}
		url := r.Value
		return m.confirmOne(stepPassGit, "Pass store remote", plan.Action{
			ID: "add-remote", Description: "Set pass git remote to " + url,
			Execute: func() error { return secret.AddGitRemote(url) },
		}), nil
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

func (m Model) confirmOne(st step, title string, a plan.Action) Model {
	m.modal = m.modal.Confirm(st, confirm.New(title, plan.Plan{Command: "secret.fix", Actions: []plan.Action{a}}))
	return m
}

// confirmEach opens a checklist with one action per row. Findings in one
// file share an exclude.
func (m Model) confirmEach(st step, title string, rows []secret.Violation, action func(Model, secret.Violation) plan.Action) Model {
	p := plan.Plan{Command: "secret.fix"}
	seen := map[string]bool{}
	for _, v := range rows {
		a := action(m, v)
		if !seen[a.ID] {
			seen[a.ID] = true
			p.Actions = append(p.Actions, a)
		}
	}
	m.modal = m.modal.Confirm(st, confirm.New(title, p))
	return m
}

func (m Model) allowAction(v secret.Violation) plan.Action {
	mp, anchor := m.env.Paths.Config, secret.Anchor(v)
	return plan.Action{
		ID:          "allowlist-" + anchor,
		Description: fmt.Sprintf("Allowlist %s:%d", m.abs(v), v.Line),
		Execute:     func() error { return wsconfig.AddToAllowlist(mp, anchor) },
	}
}

func (m Model) excludeAction(v secret.Violation) plan.Action {
	p, rel := m.env.Paths, v.Path
	return plan.Action{
		ID:          "exclude-" + rel,
		Description: "Exclude " + m.abs(v) + " from sync (.megaignore)",
		Execute: func() error {
			if _, err := wsconfig.AddIgnoreExclude(p.Config, rel, "secret: excluded by ws secret fix"); err != nil {
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

// passAction stores the matched value in pass, as `secret fix` [p] does. The
// file itself is not rewritten (same as the CLI).
func (m Model) passAction(v secret.Violation, entry string) plan.Action {
	mp, anchor := m.env.Paths.Config, secret.Anchor(v)
	value := secret.ExtractSecretValue(v.Snippet)
	return plan.Action{
		ID:          "pass-" + anchor,
		Description: fmt.Sprintf("Insert pass entry %s from %s:%d", entry, m.abs(v), v.Line),
		Execute: func() error {
			if value == "" {
				return fmt.Errorf("could not extract a value from the match")
			}
			if err := secret.InsertEntry(entry, value); err != nil {
				return err
			}
			return wsconfig.AddToAllowlist(mp, anchor)
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
