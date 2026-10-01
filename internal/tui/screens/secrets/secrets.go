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
	"strconv"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	wsignore "github.com/mugenkunou/ws-tool/internal/ignore"
	"github.com/mugenkunou/ws-tool/internal/manifest"
	"github.com/mugenkunou/ws-tool/internal/plan"
	"github.com/mugenkunou/ws-tool/internal/secret"
	"github.com/mugenkunou/ws-tool/internal/style"
	"github.com/mugenkunou/ws-tool/internal/tui/confirm"
	"github.com/mugenkunou/ws-tool/internal/tui/env"
	"github.com/mugenkunou/ws-tool/internal/tui/format"
	"github.com/mugenkunou/ws-tool/internal/tui/handover"
	"github.com/mugenkunou/ws-tool/internal/tui/keys"
	"github.com/mugenkunou/ws-tool/internal/tui/layout"
	"github.com/mugenkunou/ws-tool/internal/tui/listview"
	"github.com/mugenkunou/ws-tool/internal/tui/modal"
	"github.com/mugenkunou/ws-tool/internal/tui/nav"
	"github.com/mugenkunou/ws-tool/internal/tui/prompt"
	"github.com/mugenkunou/ws-tool/internal/tui/theme"
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
	stepAllowAll
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
	list       listview.Model
	detail     viewport.Model
	level      level
	modal      modal.Model

	violations  []secret.Violation
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
		list: listview.New([]layout.Col{
			{Title: "Severity", Min: 8},
			{Title: "Location", Min: 20, Weight: 2},
			{Title: "Match", Min: 16, Weight: 3},
		}, "✔ No secrets found."),
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

		rules, err := manifest.LoadIgnoreRules(e.Paths.Manifest)
		if err != nil {
			return LoadedMsg{Health: h, Err: err}
		}
		mani, err := manifest.Load(e.Paths.Manifest)
		if err != nil {
			return LoadedMsg{Health: h, Err: err}
		}
		vs, err := secret.Scan(secret.ScanOptions{
			WorkspacePath: e.Paths.Workspace,
			Engine:        wsignore.BuildEngine(rules),
			Allowlist:     secret.AllowlistMap(mani),
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

// CapturesKeys implements nav.Component.
func (m Model) CapturesKeys() bool { return m.modal.Open() }

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
		m.violations = msg.Violations
		m.list = m.list.SetRows(m.rows())
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

func (m Model) selected() (secret.Violation, bool) {
	i := m.list.Cursor()
	if i < 0 || i >= len(m.violations) {
		return secret.Violation{}, false
	}
	return m.violations[i], true
}

func (m Model) openDetail(title string, cmd tea.Cmd) (Model, tea.Cmd) {
	m.level = levelDetail
	m.detailTitle = title
	m.detail.SetContent(theme.Muted.Render("Loading…"))
	m.detail.GotoTop()
	return m, cmd
}

func (m Model) updateList(k tea.KeyPressMsg) (nav.Component, tea.Cmd) {
	v, haveV := m.selected()
	ph := m.health.Pass
	switch {
	case key.Matches(k, m.keys.Back):
		return m, nav.Up
	case key.Matches(k, m.keys.Refresh):
		return m.rescan()
	case key.Matches(k, m.keys.Audit):
		return m.openDetail("Pass store audit", audit())
	case key.Matches(k, m.keys.PassLog):
		if !ph.GitBacked {
			return m, nav.Notify(nav.LevelWarn, "Pass store is not git-backed — press S to run setup")
		}
		return m.openDetail("Pass store git log", passLog())
	case key.Matches(k, m.keys.PassPush):
		if !ph.GitBacked || !ph.GitRemote {
			return m, nav.Notify(nav.LevelWarn, "Pass store has no git remote — press o to set one")
		}
		return m.confirmOne(stepPassGit, "Push pass store", plan.Action{
			ID: "pass-push", Description: "Push pass store to " + m.health.RemoteURL, Execute: secret.GitPush,
		}), nil
	case key.Matches(k, m.keys.PassRemote):
		if !ph.GitBacked {
			return m, nav.Notify(nav.LevelWarn, "Pass store is not git-backed — press S to run setup")
		}
		var cmd tea.Cmd
		m.modal, cmd = m.modal.Prompt(stepRemoteURL, prompt.New("Set pass store git remote",
			prompt.WithValue(m.health.RemoteURL), prompt.WithHint("Use a private repository.")))
		return m, cmd
	case key.Matches(k, m.keys.Setup):
		return m, m.wizard("secret", "setup")
	case key.Matches(k, m.keys.CredSetup):
		return m, m.wizard("git-credential-helper", "setup")
	case key.Matches(k, m.keys.CredStatus):
		return m, handover.Run(handover.Paged(handover.WS(m.env.Paths, "git-credential-helper", "status")), handoverWizard)
	case key.Matches(k, m.keys.CredDisconnect):
		return m, m.wizard("git-credential-helper", "disconnect")
	case key.Matches(k, m.keys.AllowAll):
		return m.openAllowAll(), nil
	case !haveV:
	case key.Matches(k, m.keys.View):
		return m.openDetail("", viewContext(m.env, v))
	case key.Matches(k, m.keys.Allowlist):
		return m.confirmOne(stepFix, "Allowlist", m.allowAction(v)), nil
	case key.Matches(k, m.keys.Exclude):
		return m.confirmOne(stepFix, "Exclude from sync", m.excludeAction(v)), nil
	case key.Matches(k, m.keys.StorePass):
		if !ph.Initialized {
			return m, nav.Notify(nav.LevelWarn, "Pass store not initialized — press S to run setup")
		}
		m.pending = v
		var cmd tea.Cmd
		m.modal, cmd = m.modal.Prompt(stepPassEntryName, prompt.New("Store this secret in pass",
			prompt.WithHint(fmt.Sprintf("Entry name for %s:%d", m.abs(v), v.Line)),
			prompt.WithValue(secret.SuggestPassEntry(v.Path, v.Snippet))))
		return m, cmd
	case key.Matches(k, m.keys.SkipDir):
		m.pending = v
		var cmd tea.Cmd
		m.modal, cmd = m.modal.Prompt(stepSkipDirInput, prompt.New("Skip a directory in secret scans",
			prompt.WithHint("Saved to secret.skip_dirs in config."),
			prompt.WithValue(filepath.Dir(m.abs(v)))))
		return m, cmd
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

func (m Model) allowAction(v secret.Violation) plan.Action {
	mp, anchor := m.env.Paths.Manifest, secret.Anchor(v)
	return plan.Action{
		ID:          "allowlist-" + anchor,
		Description: fmt.Sprintf("Allowlist %s:%d", m.abs(v), v.Line),
		Execute:     func() error { return secret.AddToAllowlist(mp, anchor) },
	}
}

func (m Model) excludeAction(v secret.Violation) plan.Action {
	p, rel := m.env.Paths, v.Path
	return plan.Action{
		ID:          "exclude-" + rel,
		Description: "Exclude " + m.abs(v) + " from sync (.megaignore)",
		Execute: func() error {
			if _, err := manifest.AddIgnoreExclude(p.Manifest, rel, "secret: excluded by ws secret fix"); err != nil {
				return err
			}
			rules, err := manifest.LoadIgnoreRules(p.Manifest)
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
	mp, anchor := m.env.Paths.Manifest, secret.Anchor(v)
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
			return secret.TrackPassEntry(mp, anchor)
		},
	}
}

func (m Model) openAllowAll() Model {
	p := plan.Plan{Command: "secret.fix"}
	for _, v := range m.violations {
		p.Actions = append(p.Actions, m.allowAction(v))
	}
	m.modal = m.modal.Confirm(stepAllowAll, confirm.New("Allowlist "+format.Plural(len(p.Actions), "finding"), p))
	return m
}

func (m Model) rows() []table.Row {
	out := make([]table.Row, 0, len(m.violations))
	for _, v := range m.violations {
		sev := v.Severity
		if sev == "CRITICAL" {
			sev = theme.Error.Render(sev)
		}
		out = append(out, table.Row{
			sev,
			m.env.Rel(m.abs(v)) + ":" + strconv.Itoa(v.Line),
			strings.Join(strings.Fields(v.Snippet), " "),
		})
	}
	return out
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
	return format.Plural(len(m.violations), "finding")
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
	return m.keys
}
