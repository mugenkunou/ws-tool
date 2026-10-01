// Package dotfiles is the managed-dotfiles screen: every manifest record with
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
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"github.com/mugenkunou/ws-tool/internal/config"
	"github.com/mugenkunou/ws-tool/internal/dotfile"
	"github.com/mugenkunou/ws-tool/internal/manifest"
	"github.com/mugenkunou/ws-tool/internal/plan"
	"github.com/mugenkunou/ws-tool/internal/provision"
	"github.com/mugenkunou/ws-tool/internal/repo"
	"github.com/mugenkunou/ws-tool/internal/secret"
	"github.com/mugenkunou/ws-tool/internal/style"
	"github.com/mugenkunou/ws-tool/internal/tui/complete"
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

// Entry is one managed dotfile and its health.
type Entry struct {
	Record manifest.DotfileRecord
	Status string // "" when healthy, else dotfile.StatusBroken / StatusOverwritten
}

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
	list       listview.Model
	git        viewport.Model
	level      level
	modal      modal.Model
	entries    []Entry
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
		list: listview.New([]layout.Col{
			{Title: "System path", Min: 20, Weight: 3},
			{Title: "Stored as", Min: 12, Weight: 2},
			{Title: "Status", Min: 13},
			{Title: "Sudo", Min: 4},
		}, theme.Icon(theme.IconSprout)+"No dotfiles managed yet. Press a to add one."),
	}
}

// Load reads the manifest and checks every symlink.
func Load(e env.Env) tea.Cmd {
	return func() tea.Msg {
		records, err := dotfile.List(e.Paths.Manifest)
		if err != nil {
			return LoadedMsg{Err: err}
		}
		issues, err := dotfile.Scan(dotfile.ScanOptions{WorkspacePath: e.Paths.Workspace, ManifestPath: e.Paths.Manifest})
		if err != nil {
			return LoadedMsg{Err: err}
		}
		status := make(map[string]string, len(issues))
		for _, is := range issues {
			status[is.SystemPath] = is.Status
		}
		entries := make([]Entry, 0, len(records))
		for _, r := range records {
			entries = append(entries, Entry{Record: r, Status: status[r.System]})
		}
		return LoadedMsg{Entries: entries}
	}
}

// prepareAdd stats the path and, for directories, expands it as the CLI does.
func prepareAdd(input string) tea.Cmd {
	return func() tea.Msg {
		abs, err := config.ExpandUserPath(input)
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
		repoPath := filepath.Join(e.Paths.Workspace, "ws", "dotfiles")
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

// CapturesKeys implements nav.Component.
func (m Model) CapturesKeys() bool { return m.modal.Open() }

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
		m.entries = msg.Entries
		m.list = m.list.SetRows(m.rows())
		return m, nil
	case SelectMsg:
		for i, e := range m.entries {
			if e.Record.System == msg.System {
				m.level = levelList
				m.list = m.list.SetCursor(i)
			}
		}
		return m, nil
	case RequestFixMsg:
		if m.modal.Open() {
			return m, nil
		}
		m.level = levelList
		return m.openFix(), nil
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

func (m Model) updateList(k tea.KeyPressMsg) (nav.Component, tea.Cmd) {
	switch {
	case key.Matches(k, m.keys.Back):
		return m, nav.Up
	case key.Matches(k, m.keys.Refresh):
		m.list = m.list.SetLoading()
		return m, Load(m.env)
	case key.Matches(k, m.keys.Add):
		if m.preparing {
			return m, nil
		}
		var cmd tea.Cmd
		m.modal, cmd = m.modal.Prompt(stepAddPath, prompt.New("Add a dotfile or config directory",
			prompt.WithHint("The original moves into ws/dotfiles/ and is symlinked back."),
			prompt.WithValue("~/"),
			prompt.WithCompleter(complete.Paths)))
		return m, cmd
	case key.Matches(k, m.keys.Remove):
		if i := m.list.Cursor(); i >= 0 && i < len(m.entries) {
			return m.openRemove(m.entries[i].Record), nil
		}
		return m, nil
	case key.Matches(k, m.keys.Fix):
		return m.openFix(), nil
	case key.Matches(k, m.keys.Reset):
		return m.openReset(), nil
	case key.Matches(k, m.keys.Push):
		return m.openPush(), nil
	case key.Matches(k, m.keys.GitView):
		m.level = levelGit
		m.git.SetContent(theme.Muted.Render("Loading…"))
		m.git.GotoTop()
		return m, loadGitView(m.env)
	case key.Matches(k, m.keys.GitSetup):
		return m, handover.Run(handover.Pause(handover.WS(m.env.Paths, "dotfile", "git", "setup")), handoverGitSetup)
	case key.Matches(k, m.keys.Migrate):
		return m, handover.Run(handover.Pause(handover.WS(m.env.Paths, "dotfile", "migrate")), handoverMigrate)
	}
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(k)
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
	level := nav.LevelSuccess
	if done.Result.HasFailures() {
		level = nav.LevelWarn
	}
	cmds := []tea.Cmd{nav.Notify(level, confirm.Summary(done.Result))}
	// Auto-commit/push after a successful add or remove, as the CLI does.
	if (st == stepAdd || st == stepRemove) && done.Result.ExecutedCount() > 0 && !done.Result.HasFailures() {
		cmds = append(cmds, m.autoSync(st))
	}
	m.list = m.list.SetLoading()
	cmds = append(cmds, Load(m.env))
	return m, tea.Batch(cmds...)
}

func (m Model) autoSync(st step) tea.Cmd {
	p := m.env.Paths
	msg := "dotfile add"
	if st == stepRemove {
		msg = "dotfile rm"
	}
	return func() tea.Msg {
		res, enabled := dotfile.AutoSync(p.Workspace, p.Config, p.Manifest, msg)
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
		case dotfile.ClassSecret:
			desc += theme.Warn.Render("  ⚠ looks like a secret")
		case dotfile.ClassState:
			desc += theme.Muted.Render("  likely state")
		}
		files := c.files
		pl.Actions = append(pl.Actions, plan.Action{
			ID:          "dotfile-add-" + c.label,
			Description: desc,
			Execute: func() error {
				for _, f := range files {
					res, err := dotfile.Add(dotfile.AddOptions{WorkspacePath: p.Workspace, ManifestPath: p.Manifest, SystemPath: f})
					if err != nil {
						return fmt.Errorf("%s: %w", f, err)
					}
					_ = manifest.RecordProvision(p.Manifest, provision.Entry{
						Type:    provision.TypeSymlink,
						Path:    res.Record.System,
						Target:  dotfile.DotfilePath(res.Record.Name),
						Command: "dotfile add",
					})
				}
				return nil
			},
		})
	}
	// Defaults match the CLI: only config-like entries are pre-selected.
	cands := msg.candidates
	c := confirm.New("Add dotfiles from "+msg.input, pl).WithChecked(func(i int, _ plan.Action) bool {
		return cands[i].class == dotfile.ClassConfig
	})
	m.modal = m.modal.Confirm(stepAdd, c)
	return m
}

func (m Model) openRemove(r manifest.DotfileRecord) Model {
	p := m.env.Paths
	sys := r.System
	pl := plan.Plan{Command: "dotfile.rm", Actions: []plan.Action{{
		ID:          "dotfile-rm",
		Description: "Remove dotfile " + sys + theme.Muted.Render("  (restores the file to its system path)"),
		Execute: func() error {
			res, err := dotfile.Remove(dotfile.RemoveOptions{WorkspacePath: p.Workspace, ManifestPath: p.Manifest, SystemPath: sys})
			if err != nil {
				return err
			}
			_ = manifest.RemoveProvision(p.Manifest, provision.TypeSymlink, res.Record.System)
			return nil
		},
	}}}
	m.modal = m.modal.Confirm(stepRemove, confirm.New("Remove dotfile", pl))
	return m
}

func (m Model) openFix() Model {
	p := m.env.Paths
	var broken []string
	for _, e := range m.entries {
		if e.Status != "" {
			broken = append(broken, m.env.Rel(e.Record.System))
		}
	}
	pl := plan.Plan{Command: "dotfile.fix"}
	if len(broken) > 0 {
		pl.Actions = []plan.Action{{
			ID:          "dotfile-fix",
			Description: "Recreate " + format.Plural(len(broken), "link") + ": " + strings.Join(broken, ", "),
			Execute: func() error {
				res, err := dotfile.Fix(dotfile.FixOptions{WorkspacePath: p.Workspace, ManifestPath: p.Manifest})
				if err != nil {
					return err
				}
				if len(res.Failed) > 0 {
					msgs := make([]string, 0, len(res.Failed))
					for _, f := range res.Failed {
						msgs = append(msgs, f.SystemPath+": "+f.Message)
					}
					return fmt.Errorf("%d failed: %s", len(res.Failed), strings.Join(msgs, "; "))
				}
				return nil
			},
		}}
	}
	m.modal = m.modal.Confirm(stepFix, confirm.New("Fix dotfile links", pl))
	return m
}

func (m Model) openReset() Model {
	p := m.env.Paths
	pl := plan.Plan{Command: "dotfile.reset", Actions: []plan.Action{{
		ID:          "dotfile-reset-all",
		Description: "Reset all " + format.Plural(len(m.entries), "managed dotfile") + theme.Warn.Render("  (restores originals, unregisters all)"),
		Execute: func() error {
			_, err := dotfile.Reset(dotfile.ResetOptions{WorkspacePath: p.Workspace, ManifestPath: p.Manifest})
			return err
		},
	}}}
	c := confirm.New("Reset dotfiles", pl).WithChecked(func(int, plan.Action) bool { return false })
	m.modal = m.modal.Confirm(stepReset, c)
	return m
}

// openPush mirrors `ws dotfile git push`: commit pending changes, refuse a
// public remote, push.
func (m Model) openPush() Model {
	p := m.env.Paths
	pl := plan.Plan{Command: "dotfile.git.push", Actions: []plan.Action{{
		ID:          "dotfile-git-push",
		Description: "Commit pending dotfile changes and push",
		Execute: func() error {
			repoPath := filepath.Join(p.Workspace, "ws", "dotfiles")
			if !dotfile.GitIsInitialized(repoPath) {
				return fmt.Errorf("git not initialized — press G to run setup")
			}
			if !dotfile.GitHasRemote(repoPath) {
				return fmt.Errorf("no remote configured — press G to run setup")
			}
			cfg, err := config.Load(p.Config)
			if err != nil {
				return err
			}
			mani, err := manifest.Load(p.Manifest)
			if err != nil {
				return err
			}
			res := dotfile.GitSync(dotfile.GitSyncOptions{
				WorkspacePath: p.Workspace,
				RepoPath:      repoPath,
				RemoteURL:     mani.DotfileGit.RemoteURL,
				Branch:        mani.DotfileGit.Branch,
				AutoCommit:    true,
				CommitMessage: "manual push",
			})
			if res.Error != "" {
				return fmt.Errorf("commit: %s", res.Error)
			}
			remote := dotfile.GitRemoteURL(repoPath)
			vis := repo.CheckRepoVisibility(remote, secret.ResolveGitToken(remote, cfg.Dotfile.Git.PassEntry))
			if vis.Checked && !vis.Private {
				return repo.ErrPublicRepository
			}
			if err := dotfile.GitPush(repoPath, mani.DotfileGit.Branch); err != nil {
				return fmt.Errorf("push failed: %w", err)
			}
			return nil
		},
	}}}
	m.modal = m.modal.Confirm(stepPush, confirm.New("Push dotfiles", pl))
	return m
}

func (m Model) rows() []table.Row {
	rows := make([]table.Row, 0, len(m.entries))
	for _, e := range m.entries {
		status := theme.OK.Render("✔ ok")
		switch e.Status {
		case dotfile.StatusBroken:
			status = theme.Error.Render("✘ broken")
		case dotfile.StatusOverwritten:
			status = theme.Warn.Render("⚠ overwritten")
		}
		sudo := ""
		if e.Record.Sudo {
			sudo = "yes"
		}
		rows = append(rows, table.Row{m.env.Rel(e.Record.System), e.Record.Name, status, sudo})
	}
	return rows
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
	return m.keys
}
