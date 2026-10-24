// Package dotfiles is the managed-dotfiles screen: every link record with
// its symlink health, plus add / remove / fix / reset and the dotfile git
// backup (push, status, log). Multi-step git wizards (setup, migrate) are
// handed to the CLI in the terminal.
//
// Navigation levels: list → git view. esc in the git view returns to the
// list; esc in the list asks the app to go up.
package dotfiles

import (
	"fmt"
	"os"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"github.com/mugenkunou/ws-tool/internal/dotfile"
	"github.com/mugenkunou/ws-tool/internal/plan"
	"github.com/mugenkunou/ws-tool/internal/style"
	"github.com/mugenkunou/ws-tool/internal/tui/complete"
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

// Entry is one managed dotfile and its health.
type Entry = dotfile.Entry

// LoadedMsg carries the result of loading the dotfile list. Other components
// (the dashboard) may observe it.
type LoadedMsg struct {
	Entries []Entry
	Err     error
}

// Issues counts unhealthy entries.
func (m LoadedMsg) Issues() int {
	n := 0
	for _, e := range m.Entries {
		if e.Status != "" {
			n++
		}
	}
	return n
}

// SelectMsg asks the screen to select the entry for a system path (sent by
// the dashboard when it opens this screen at a specific problem).
type SelectMsg struct{ System string }

// RequestFixMsg asks the screen to open its fix-links checklist.
type RequestFixMsg struct{}

// addCandidate is one file or directory entry offered for `dotfile add`.
type addCandidate struct {
	label string
	files []string
	class dotfile.FileClass
}

// addPreparedMsg carries what a path expands to.
type addPreparedMsg struct {
	input      string
	candidates []addCandidate
	err        error
}

// gitViewMsg carries the dotfile git status and log text.
type gitViewMsg struct {
	text string
}

type level int

const (
	levelList level = iota
	levelGit
)

type step int

const (
	stepAddPath step = iota
	stepAdd
	stepRemove
	stepFix
	stepSync
	stepReset
	stepPush
)

// handoverTag identifies CLI wizards run in the terminal.
type handoverTag int

const (
	handoverGitSetup handoverTag = iota
	handoverMigrate
)

// Model is the dotfiles screen state.
type Model struct {
	env        env.Env
	keys       keys.DotfilesMap
	detailKeys keys.DetailMap
	list       reslist.Model[Entry]
	git        viewport.Model
	level      level
	modal      modal.Model
	preparing  bool
	width      int
	height     int
}

// New creates the screen.
func New(e env.Env) Model {
	dk := keys.Detail()
	vp := viewport.New()
	vp.KeyMap = dk.Viewport
	vp.SoftWrap = true
	return Model{
		env:        e,
		keys:       keys.Dotfiles(),
		detailKeys: dk,
		git:        vp,
		list: reslist.New(dotfile.Schema(e.View()), []reslist.Col[Entry]{
			{Key: "system", Min: 20, Weight: 3},
			{Key: "workspace_path", Min: 12, Weight: 2, Style: copyStyle},
			{Key: "status", Min: 13, Style: statusStyle},
			{Key: "sudo", Min: 4},
		}, theme.Icon(theme.IconSprout)+"No dotfiles managed yet. Press a to add one.").Declare(reslist.Decls(actions)),
	}
}

// copyStyle highlights a workspace copy that lives under secrets/.
func copyStyle(e Entry, text string) string {
	if e.Secret() {
		return theme.Warn.Render(text)
	}
	return text
}

func statusStyle(e Entry, text string) string {
	switch e.Status {
	case dotfile.StatusBroken:
		return theme.Error.Render("✘ " + text)
	case dotfile.StatusOverwritten:
		return theme.Warn.Render("⚠ " + text)
	}
	return theme.OK.Render("✔ " + text)
}

// Load reads the config and checks every symlink.
func Load(e env.Env) tea.Cmd {
	return func() tea.Msg {
		entries, err := dotfile.Entries(e.Paths.Workspace, e.Paths.Config)
		return LoadedMsg{Entries: entries, Err: err}
	}
}

// prepareAdd stats the path and, for directories, expands it as the CLI does.
func prepareAdd(input string) tea.Cmd {
	return func() tea.Msg {
		abs, err := wsconfig.ExpandUserPath(input)
		if err != nil {
			return addPreparedMsg{input: input, err: err}
		}
		info, err := os.Stat(abs)
		if err != nil {
			return addPreparedMsg{input: input, err: fmt.Errorf("path not found: %s", abs)}
		}
		if !info.IsDir() {
			return addPreparedMsg{input: input, candidates: []addCandidate{{
				label: abs, files: []string{abs}, class: dotfile.ClassifyFile(abs),
			}}}
		}
		entries, _, err := dotfile.ExpandDir(abs)
		if err != nil {
			return addPreparedMsg{input: input, err: err}
		}
		var cands []addCandidate
		for _, e := range entries {
			files, err := dotfile.CollectFiles(e)
			if err != nil {
				return addPreparedMsg{input: input, err: fmt.Errorf("reading %s: %w", e.AbsPath, err)}
			}
			label := e.AbsPath
			if e.IsDir {
				label += fmt.Sprintf("/  (%s, %s)", format.Plural(e.FileCount, "file"), style.HumanBytes(e.Size))
			}
			cands = append(cands, addCandidate{label: label, files: files, class: e.Class})
		}
		return addPreparedMsg{input: input, candidates: cands}
	}
}

// loadGitView builds the dotfile git status + recent log.
func loadGitView(e env.Env) tea.Cmd {
	return func() tea.Msg {
		repoPath := dotfile.RepoPath(e.Paths.Workspace)
		var b strings.Builder
		b.WriteString(theme.Title.Render("Dotfile git backup") + "\n\n")
		field := func(label, value string) {
			b.WriteString(theme.Muted.Render(fmt.Sprintf("%-12s", label)) + " " + value + "\n")
		}
		if !dotfile.GitIsInitialized(repoPath) {
			field("Repository", theme.Warn.Render("not initialized"))
			b.WriteString("\n" + theme.Muted.Render("Press G (from the list) to run `ws dotfile git setup`."))
			return gitViewMsg{text: b.String()}
		}
		branch := dotfile.GitBranch(repoPath)
		field("Repository", repoPath)
		field("Branch", branch)
		if dotfile.GitHasRemote(repoPath) {
			field("Remote", dotfile.GitRemoteURL(repoPath))
			ahead, behind := dotfile.GitAheadBehind(repoPath, branch)
			field("Upstream", fmt.Sprintf("↑%d ↓%d", ahead, behind))
		} else {
			field("Remote", theme.Warn.Render("none — press G to set up"))
		}
		if last, err := dotfile.GitLastCommit(repoPath); err == nil && last != "" {
			field("Last commit", strings.TrimSpace(last))
		}
		if porcelain, err := dotfile.GitStatus(repoPath); err == nil {
			if p := strings.TrimSpace(porcelain); p != "" {
				b.WriteString("\n" + theme.Bold.Render("Uncommitted") + "\n" + p + "\n")
			} else {
				field("Working tree", theme.OK.Render("clean"))
			}
		}
		if log, err := dotfile.GitLog(repoPath, 20); err == nil && strings.TrimSpace(log) != "" {
			b.WriteString("\n" + theme.Bold.Render("Recent commits") + "\n" + strings.TrimRight(log, "\n"))
		}
		return gitViewMsg{text: b.String()}
	}
}

// Init starts the first load.
func (m Model) Init() tea.Cmd { return Load(m.env) }

// Declared lists the screen's actions and their scopes.
func (m Model) Declared() []reslist.Decl { return m.list.Declared() }

// CapturesKeys implements nav.Component.
func (m Model) CapturesKeys() bool { return m.modal.Open() || m.list.Editing() }

// Update implements nav.Component.
func (m Model) Update(msg tea.Msg) (nav.Component, tea.Cmd) {
	if st, ok := m.modal.Match(msg); ok {
		m.modal = m.modal.Close()
		return m.dialogResult(st.(step), msg)
	}

	switch msg := msg.(type) {
	case nav.ResizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.list = m.list.SetSize(msg.Width, msg.Height)
		m.git.SetWidth(msg.Width)
		m.git.SetHeight(msg.Height)
		m.modal = m.modal.SetSize(msg.Width, msg.Height)
		return m, nil
	case nav.RefreshMsg:
		m.list = m.list.SetLoading()
		return m, Load(m.env)
	case LoadedMsg:
		if msg.Err != nil {
			m.list = m.list.SetError(msg.Err)
			return m, nil
		}
		m.list = m.list.SetItems(msg.Entries)
		return m, nil
	case SelectMsg:
		var found bool
		if m.list, found = m.list.Select(func(e Entry) bool { return e.Link.System == msg.System }); found {
			m.level = levelList
		}
		return m, nil
	case RequestFixMsg:
		if m.modal.Open() {
			return m, nil
		}
		m.level = levelList
		return m.openFix(m.list.Items()), nil
	case addPreparedMsg:
		m.preparing = false
		if msg.err != nil {
			return m, nav.Notify(nav.LevelError, msg.err.Error())
		}
		if len(msg.candidates) == 0 {
			return m, nav.Notify(nav.LevelInfo, "Directory is empty — nothing to add.")
		}
		return m.openAdd(msg), nil
	case gitViewMsg:
		m.git.SetContent(msg.text)
		return m, nil
	case handover.DoneMsg:
		if _, ok := msg.Tag.(handoverTag); !ok {
			return m, nil
		}
		m.list = m.list.SetLoading()
		return m, Load(m.env)
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
	if m.level == levelGit {
		if key.Matches(k, m.detailKeys.Back) {
			m.level = levelList
			return m, nil
		}
		var cmd tea.Cmd
		m.git, cmd = m.git.Update(k)
		return m, cmd
	}
	return m.updateList(k)
}

// actions is every dotfiles key and the rows it acts on.
var actions = func() []reslist.Action[Model, Entry] {
	k := keys.Dotfiles()
	global := func(f func(Model) (Model, tea.Cmd)) func(Model, []Entry) (Model, tea.Cmd) {
		return func(m Model, _ []Entry) (Model, tea.Cmd) { return f(m) }
	}
	plain := func(f func(Model, []Entry) Model) func(Model, []Entry) (Model, tea.Cmd) {
		return func(m Model, rows []Entry) (Model, tea.Cmd) { return f(m, rows), nil }
	}
	return []reslist.Action[Model, Entry]{
		{Key: k.Add, Scope: reslist.Global, Do: global(Model.promptAdd)},
		{Key: k.Remove, Scope: reslist.Selected, Do: plain(Model.openRemove)},
		{Key: k.Sync, Scope: reslist.Selected, Do: plain(Model.openSync)},
		{Key: k.Fix, Scope: reslist.All, Do: plain(Model.openFix)},
		{Key: k.Reset, Scope: reslist.All, Do: plain(Model.openReset)},
		{Key: k.Push, Scope: reslist.Global, Do: global(func(m Model) (Model, tea.Cmd) { return m.openPush(), nil })},
		{Key: k.GitView, Scope: reslist.Global, Do: global(func(m Model) (Model, tea.Cmd) {
			m.level = levelGit
			m.git.SetContent(theme.Muted.Render("Loading…"))
			m.git.GotoTop()
			return m, loadGitView(m.env)
		})},
		{Key: k.GitSetup, Scope: reslist.Global, Do: global(func(m Model) (Model, tea.Cmd) {
			return m, handover.Run(handover.Pause(handover.WS(m.env.Paths, "dotfile", "git", "setup")), handoverGitSetup)
		})},
		{Key: k.Migrate, Scope: reslist.Global, Do: global(func(m Model) (Model, tea.Cmd) {
			return m, handover.Run(handover.Pause(handover.WS(m.env.Paths, "dotfile", "migrate")), handoverMigrate)
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
	}
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(k)
	return m, cmd
}

func (m Model) promptAdd() (Model, tea.Cmd) {
	if m.preparing {
		return m, nil
	}
	var cmd tea.Cmd
	m.modal, cmd = m.modal.Prompt(stepAddPath, prompt.New("Add a dotfile or config directory",
		prompt.WithHint("The original moves into configs/dotfiles/ and is symlinked back."),
		prompt.WithValue("~/"),
		prompt.WithCompleter(complete.Paths)))
	return m, cmd
}

func (m Model) dialogResult(st step, msg tea.Msg) (nav.Component, tea.Cmd) {
	if st == stepAddPath {
		r := msg.(prompt.ResultMsg)
		if r.Cancelled {
			return m, nil
		}
		m.preparing = true
		return m, prepareAdd(r.Value)
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
	cmds := []tea.Cmd{nav.Notify(level, confirm.Summary(done.Result))}
	// Auto-commit/push after a successful add, remove, or sync, as the CLI does.
	if (st == stepAdd || st == stepRemove || st == stepSync) && done.Result.ExecutedCount() > 0 && !done.Result.HasFailures() {
		cmds = append(cmds, m.autoSync(st))
	}
	m.list = m.list.SetLoading()
	cmds = append(cmds, Load(m.env))
	return m, tea.Batch(cmds...)
}

func (m Model) autoSync(st step) tea.Cmd {
	p := m.env.Paths
	msg := "dotfile add"
	switch st {
	case stepRemove:
		msg = "dotfile rm"
	case stepSync:
		msg = "dotfile sync"
	}
	return func() tea.Msg {
		res, enabled := dotfile.AutoSync(p.Workspace, p.Config, msg)
		switch {
		case !enabled:
			return nil
		case res.Error != "":
			return nav.NoticeMsg{Level: nav.LevelWarn, Text: "dotfile git: " + res.Error}
		case res.Pushed:
			return nav.NoticeMsg{Level: nav.LevelSuccess, Text: "dotfile git: committed and pushed"}
		case res.Committed:
			return nav.NoticeMsg{Level: nav.LevelSuccess, Text: "dotfile git: committed"}
		}
		return nil
	}
}

func (m Model) openAdd(msg addPreparedMsg) Model {
	p := m.env.Paths
	pl := plan.Plan{Command: "dotfile.add"}
	for _, c := range msg.candidates {
		desc := "Add " + m.env.Rel(c.label)
		switch c.class {
		case dotfile.FileClassSecret:
			desc += theme.Warn.Render("  ⚠ looks like a secret")
		case dotfile.FileClassState:
			desc += theme.Muted.Render("  likely state")
		}
		files := c.files
		pl.Actions = append(pl.Actions, plan.Action{
			ID:          "dotfile-add-" + c.label,
			Description: desc,
			Execute: func() error {
				for _, f := range files {
					_, err := dotfile.Add(dotfile.AddOptions{WorkspacePath: p.Workspace, ConfigPath: p.Config, SystemPath: f})
					if err != nil {
						return fmt.Errorf("%s: %w", f, err)
					}
				}
				return nil
			},
		})
	}
	// Defaults match the CLI: only config-like entries are pre-selected.
	cands := msg.candidates
	c := confirm.New("Add dotfiles from "+msg.input, pl).WithChecked(func(i int, _ plan.Action) bool {
		return cands[i].class == dotfile.FileClassConfig
	})
	m.modal = m.modal.Confirm(stepAdd, c)
	return m
}

func (m Model) openRemove(targets []Entry) Model {
	m.modal = m.modal.Confirm(stepRemove, confirm.New("Remove "+format.Plural(len(targets), "dotfile"), m.removePlan(targets)))
	return m
}

func (m Model) removePlan(targets []Entry) plan.Plan {
	p := m.env.Paths
	pl := plan.Plan{Command: "dotfile.rm"}
	for _, e := range targets {
		sys := e.Link.System
		pl.Actions = append(pl.Actions, plan.Action{
			ID:          "dotfile-rm-" + sys,
			Description: "Remove dotfile " + sys + theme.Muted.Render("  (restores the file to its system path)"),
			Execute: func() error {
				return dotfile.Unlink(dotfile.UnlinkOptions{WorkspacePath: p.Workspace, ConfigPath: p.Config, SystemPath: sys})
			},
		})
	}
	return pl
}

// openFix links the broken and overwritten rows to their workspace copies: one
// action per entry, the workspace copy winning (`ws dotfile link`).
func (m Model) openFix(rows []Entry) Model {
	p := m.env.Paths
	pl := plan.Plan{Command: "dotfile.link"}
	for _, e := range rows {
		if e.Status == "" {
			continue
		}
		sys := e.Link.System
		rp := dotfile.PlanRelink(p.Workspace, e.Link)
		desc := "Link " + m.env.Rel(sys)
		if rp.Backup != "" {
			desc += theme.Muted.Render("  (the live file moves to " + m.env.Rel(rp.Backup) + ")")
		}
		pl.Actions = append(pl.Actions, plan.Action{
			ID:          "dotfile-link-" + sys,
			Description: desc,
			Execute: func() error {
				return dotfile.Relink(dotfile.RelinkOptions{WorkspacePath: p.Workspace, ConfigPath: p.Config, SystemPath: sys})
			},
		})
	}
	m.modal = m.modal.Confirm(stepFix, confirm.New("Link dotfiles", pl))
	return m
}

func (m Model) openSync(targets []Entry) Model {
	pl := plan.Plan{Command: "dotfile.sync"}
	for _, e := range targets {
		pl.Actions = append(pl.Actions, m.syncAction(e.Link.System))
	}
	m.modal = m.modal.Confirm(stepSync, confirm.New("Sync "+format.Plural(len(targets), "dotfile"), pl))
	return m
}

func (m Model) syncAction(sys string) plan.Action {
	p := m.env.Paths
	return plan.Action{
		ID:          "dotfile-add-" + sys,
		Description: "Add " + sys + theme.Muted.Render("  (the live file replaces the workspace copy)"),
		Execute: func() error {
			return dotfile.TakeLive(dotfile.TakeLiveOptions{WorkspacePath: p.Workspace, ConfigPath: p.Config, SystemPath: sys})
		},
	}
}

// openReset restores rows to their system paths and unregisters them: the
// whole subsystem (`dotfile reset`) when rows is every dotfile, else one
// remove per row. Nothing is pre-checked.
func (m Model) openReset(rows []Entry) Model {
	p := m.env.Paths
	var pl plan.Plan
	if len(rows) == len(m.list.Items()) {
		pl = plan.Plan{Command: "dotfile.reset", Actions: []plan.Action{{
			ID:          "dotfile-reset-all",
			Description: "Reset all " + format.Plural(len(rows), "managed dotfile") + theme.Warn.Render("  (restores originals, unregisters all)"),
			Execute: func() error {
				_, err := dotfile.Reset(dotfile.ResetOptions{WorkspacePath: p.Workspace, ConfigPath: p.Config})
				return err
			},
		}}}
	} else {
		pl = m.removePlan(rows)
	}
	c := confirm.New("Reset "+format.Plural(len(rows), "dotfile"), pl).WithChecked(func(int, plan.Action) bool { return false })
	m.modal = m.modal.Confirm(stepReset, c)
	return m
}

// openPush mirrors `ws dotfile backup push`: commit pending changes, refuse a
// public remote, push.
func (m Model) openPush() Model {
	p := m.env.Paths
	pl := plan.Plan{Command: "dotfile.backup.push", Actions: []plan.Action{{
		ID:          "dotfile-backup-push",
		Description: "Commit pending dotfile changes and push",
		Execute: func() error {
			cfg, err := wsconfig.Load(p.Config)
			if err != nil {
				return err
			}
			_, err = dotfile.PushBackup(dotfile.BackupOptions{
				WorkspacePath: p.Workspace,
				ConfigPath:    p.Config,
				Git:           cfg.Dotfile.Git,
				Message:       "ws dotfile backup push",
			})
			return err
		},
	}}}
	m.modal = m.modal.Confirm(stepPush, confirm.New("Push dotfiles", pl))
	return m
}

// View implements nav.Component.
func (m Model) View() string {
	switch {
	case m.modal.Open():
		return m.modal.View()
	case m.level == levelGit:
		return m.git.View()
	case m.preparing:
		return theme.Muted.Render("Scanning…")
	}
	return m.list.View()
}

// KeyMap implements nav.Component.
func (m Model) KeyMap() help.KeyMap {
	switch {
	case m.modal.Open():
		return m.modal.KeyMap()
	case m.level == levelGit:
		return m.detailKeys
	}
	return m.list.Help(m.keys)
}
