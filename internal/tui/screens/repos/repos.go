// Package repos is the git fleet screen: status of every discovered repo,
// a per-repo detail level with hygiene findings, and fleet actions (fetch,
// pull, sync, run, add root) confirmed through the plan checklist.
//
// Navigation levels: list → detail | output. esc in detail/output returns to
// the list; esc in the list asks the app to go up. While a dialog is open
// esc closes it instead.
package repos

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/mugenkunou/ws-tool/internal/config"
	"github.com/mugenkunou/ws-tool/internal/plan"
	"github.com/mugenkunou/ws-tool/internal/repo"
	"github.com/mugenkunou/ws-tool/internal/tui/complete"
	"github.com/mugenkunou/ws-tool/internal/tui/confirm"
	"github.com/mugenkunou/ws-tool/internal/tui/env"
	"github.com/mugenkunou/ws-tool/internal/tui/format"
	"github.com/mugenkunou/ws-tool/internal/tui/keys"
	"github.com/mugenkunou/ws-tool/internal/tui/layout"
	"github.com/mugenkunou/ws-tool/internal/tui/listview"
	"github.com/mugenkunou/ws-tool/internal/tui/modal"
	"github.com/mugenkunou/ws-tool/internal/tui/nav"
	"github.com/mugenkunou/ws-tool/internal/tui/prompt"
	"github.com/mugenkunou/ws-tool/internal/tui/theme"
)

// Entry is one repo's status plus its hygiene findings.
type Entry struct {
	Status   repo.RepoStatus
	Findings []repo.Finding
}

// Summary aggregates fleet state for display.
type Summary struct {
	Repos, Dirty, Ahead, Behind, Detached, Findings int
}

// LoadedMsg carries a fleet scan. Other components (the dashboard) may
// observe it.
type LoadedMsg struct {
	Entries     []Entry
	FetchErrors []string // non-empty only after a fetch
	Err         error

	gen int
}

// Summarize computes fleet totals.
func (m LoadedMsg) Summarize() Summary {
	s := Summary{Repos: len(m.Entries)}
	for _, e := range m.Entries {
		if e.Status.Dirty {
			s.Dirty++
		}
		if e.Status.Ahead > 0 {
			s.Ahead++
		}
		if e.Status.Behind > 0 {
			s.Behind++
		}
		if e.Status.Detached {
			s.Detached++
		}
		s.Findings += len(e.Findings)
	}
	return s
}

// SelectMsg asks the screen to select a repo by path (sent by the
// dashboard when it opens this screen at a specific problem).
type SelectMsg struct{ Path string }

// RequestSyncMsg asks the screen to start a sync (merge), as pressing s does.
type RequestSyncMsg struct{}

// syncPreparedMsg carries sync plans computed after a fetch.
type syncPreparedMsg struct {
	gen      int
	plans    []repo.SyncPlan
	warnings []string
	rebase   bool
	err      error
}

type level int

const (
	levelList level = iota
	levelDetail
	levelOutput
)

type activity int

const (
	activityIdle activity = iota
	activityScanning
	activityFetching
	activityPreparingSync
)

// step tags the open dialog so its result is routed to the right handler.
type step int

const (
	stepPull step = iota
	stepSync
	stepRunCommand // prompt for the command
	stepRun        // checklist for running it
	stepAddRootPath
	stepAddRoot
)

// runOutputs collects `repo run` output from action goroutines. It is read
// only after the checklist reports done.
type runOutputs struct {
	mu  sync.Mutex
	out map[string]string
}

func (r *runOutputs) set(path, out string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.out[path] = out
}

func (r *runOutputs) snapshot() map[string]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := make(map[string]string, len(r.out))
	for k, v := range r.out {
		cp[k] = v
	}
	return cp
}

// Model is the repos screen state.
type Model struct {
	env        env.Env
	roots      []string // starts as env.RepoRoots; grows when a root is added
	keys       keys.ReposMap
	detailKeys keys.DetailMap

	list     listview.Model
	detail   viewport.Model
	level    level
	activity activity
	modal    modal.Model

	entries     []Entry
	summary     Summary
	fetchErrors []string
	selected    int // entry shown at levelDetail

	runCommand string
	running    *runOutputs
	output     string // rendered run output shown at levelOutput

	gen    int // incremented per load; stale results are dropped
	width  int
	height int
}

// New creates the screen.
func New(e env.Env) Model {
	dk := keys.Detail()
	vp := viewport.New()
	vp.KeyMap = dk.Viewport
	vp.SoftWrap = true
	return Model{
		env:        e,
		roots:      e.RepoRoots,
		keys:       keys.Repos(),
		detailKeys: dk,
		detail:     vp,
		activity:   activityScanning,
		list: listview.New([]layout.Col{
			{Title: "Repo", Min: 18, Weight: 3},
			{Title: "State", Min: 11},
			{Title: "↑", Min: 3},
			{Title: "↓", Min: 3},
			{Title: "Issues", Min: 6},
			{Title: "Branch", Min: 10, Weight: 1},
		}, theme.Icon("📦")+"No git repositories found. Press a to add a repo root."),
	}
}

// Init starts the first scan.
func (m Model) Init() tea.Cmd { return load(m.env, m.roots, m.gen, false) }

func discover(e env.Env, roots []string) ([]repo.Repository, error) {
	repos, err := repo.Discover(e.Paths.Workspace, roots, e.ExcludeDirs)
	if err != nil {
		return nil, err
	}
	return repo.AppendMissing(repos, repo.SpecialRepos(e.Paths.Workspace)), nil
}

// load discovers and scans the fleet; with fetch it runs `git fetch` first.
func load(e env.Env, roots []string, gen int, fetch bool) tea.Cmd {
	return func() tea.Msg {
		repos, err := discover(e, roots)
		if err != nil {
			return LoadedMsg{gen: gen, Err: err}
		}

		var fetchErrors []string
		if fetch {
			for _, r := range repo.FetchAll(e.Paths.Workspace, repos) {
				if !r.Success {
					fetchErrors = append(fetchErrors, fmt.Sprintf("%s: %s", e.Rel(r.Path), r.Error))
				}
			}
		}

		statuses := repo.Scan(e.Paths.Workspace, repos)
		entries := make([]Entry, 0, len(statuses))
		for i, st := range statuses {
			findings := repo.Doctor(e.Paths.Workspace, repos[i:i+1], repo.DoctorOptions{})
			entries = append(entries, Entry{Status: st, Findings: findings})
		}
		return LoadedMsg{gen: gen, Entries: entries, FetchErrors: fetchErrors}
	}
}

// prepareSync fetches (workspace repos only, as the CLI does), rescans, and
// plans a sync per repo.
func prepareSync(e env.Env, roots []string, gen int, rebase bool) tea.Cmd {
	return func() tea.Msg {
		repos, err := discover(e, roots)
		if err != nil {
			return syncPreparedMsg{gen: gen, err: err}
		}
		for _, r := range repos {
			if repo.IsWithin(r.Path, e.Paths.Workspace) {
				repo.FetchOne(e.Paths.Workspace, r)
			}
		}
		msg := syncPreparedMsg{gen: gen, rebase: rebase}
		for _, st := range repo.Scan(e.Paths.Workspace, repos) {
			sp := repo.PlanSync(st)
			if sp.Strategy == repo.SyncSkip {
				if sp.Warning != "" {
					msg.warnings = append(msg.warnings, fmt.Sprintf("%s (%s)", e.Rel(sp.Path), sp.Warning))
				}
				continue
			}
			msg.plans = append(msg.plans, sp)
		}
		return msg
	}
}

// startLoad begins a scan (or fetch+scan), superseding any in flight.
func (m Model) startLoad(fetch bool) (Model, tea.Cmd) {
	m.gen++
	m.activity = activityScanning
	if fetch {
		m.activity = activityFetching
	}
	m.list = m.list.SetLoading()
	return m, load(m.env, m.roots, m.gen, fetch)
}

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
		m.list = m.list.SetSize(msg.Width, max(msg.Height-1, 0)) // one line for the status bar
		m.detail.SetWidth(msg.Width)
		m.detail.SetHeight(msg.Height)
		m.modal = m.modal.SetSize(msg.Width, msg.Height)
		return m.refreshDetail(), nil
	case nav.RefreshMsg:
		return m.startLoad(false)
	case LoadedMsg:
		if msg.gen != m.gen {
			return m, nil
		}
		m.activity = activityIdle
		if msg.Err != nil {
			m.list = m.list.SetError(msg.Err)
			return m, nil
		}
		m.entries = msg.Entries
		m.summary = msg.Summarize()
		m.fetchErrors = msg.FetchErrors
		m.list = m.list.SetRows(m.rows())
		if m.selected >= len(m.entries) && m.level == levelDetail {
			m.level = levelList
		}
		return m.refreshDetail(), nil
	case SelectMsg:
		for i, e := range m.entries {
			if e.Status.Path == msg.Path {
				m.level = levelList
				m.list = m.list.SetCursor(i)
			}
		}
		return m, nil
	case RequestSyncMsg:
		if m.modal.Open() || m.activity != activityIdle {
			return m, nil
		}
		m.level = levelList
		m.gen++
		m.activity = activityPreparingSync
		return m, prepareSync(m.env, m.roots, m.gen, false)
	case syncPreparedMsg:
		if msg.gen != m.gen {
			return m, nil
		}
		m.activity = activityIdle
		if msg.err != nil {
			return m, nav.Notify(nav.LevelError, "Sync: "+msg.err.Error())
		}
		return m.openSync(msg), nil
	}

	if m.modal.Open() {
		var cmd tea.Cmd
		m.modal, cmd = m.modal.Update(msg)
		return m, cmd
	}
	if k, ok := msg.(tea.KeyPressMsg); ok {
		switch m.level {
		case levelDetail, levelOutput:
			return m.updateDetail(k)
		default:
			return m.updateList(k)
		}
	}
	return m, nil
}

func (m Model) updateList(msg tea.KeyPressMsg) (nav.Component, tea.Cmd) {
	busy := m.activity != activityIdle
	switch {
	case key.Matches(msg, m.keys.Back):
		return m, nav.Up
	case key.Matches(msg, m.keys.Refresh):
		return m.startLoad(false)
	case key.Matches(msg, m.keys.Fetch):
		return m.startLoad(true)
	case key.Matches(msg, m.keys.Open):
		if i := m.list.Cursor(); i >= 0 && i < len(m.entries) {
			m.selected = i
			m.level = levelDetail
			m = m.refreshDetail()
			m.detail.GotoTop()
		}
		return m, nil
	case key.Matches(msg, m.keys.Pull), key.Matches(msg, m.keys.PullRebase):
		if busy || len(m.entries) == 0 {
			return m, nil
		}
		return m.openPull(key.Matches(msg, m.keys.PullRebase)), nil
	case key.Matches(msg, m.keys.Sync), key.Matches(msg, m.keys.SyncRebase):
		if busy {
			return m, nil
		}
		m.gen++
		m.activity = activityPreparingSync
		return m, prepareSync(m.env, m.roots, m.gen, key.Matches(msg, m.keys.SyncRebase))
	case key.Matches(msg, m.keys.Run):
		if busy || len(m.entries) == 0 {
			return m, nil
		}
		var cmd tea.Cmd
		m.modal, cmd = m.modal.Prompt(stepRunCommand, prompt.New("Run a command in every repo",
			prompt.WithHint("Runs with sh -c in each repo root. You can untick repos next."),
			prompt.WithPlaceholder("git status --short"),
			prompt.WithValue(m.runCommand)))
		return m, cmd
	case key.Matches(msg, m.keys.AddRoot):
		var cmd tea.Cmd
		m.modal, cmd = m.modal.Prompt(stepAddRootPath, prompt.New("Add a repo discovery root",
			prompt.WithHint(m.rootsHint()),
			prompt.WithValue(m.env.Paths.Workspace+"/"),
			prompt.WithCompleter(complete.Dirs)))
		return m, cmd
	}
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m Model) updateDetail(msg tea.KeyPressMsg) (nav.Component, tea.Cmd) {
	if key.Matches(msg, m.detailKeys.Back) {
		m.level = levelList
		return m, nil
	}
	var cmd tea.Cmd
	m.detail, cmd = m.detail.Update(msg)
	return m, cmd
}

// dialogResult handles the outcome of the dialog opened for st.
func (m Model) dialogResult(st step, msg tea.Msg) (nav.Component, tea.Cmd) {
	switch st {
	case stepRunCommand:
		r := msg.(prompt.ResultMsg)
		if r.Cancelled {
			return m, nil
		}
		m.runCommand = r.Value
		return m.openRun(r.Value), nil
	case stepAddRootPath:
		r := msg.(prompt.ResultMsg)
		if r.Cancelled {
			return m, nil
		}
		return m.openAddRoot(r.Value)
	}

	done := msg.(confirm.DoneMsg)
	if done.Result.Aborted {
		if st == stepAddRoot {
			m.roots = m.roots[:len(m.roots)-1]
		}
		return m, nil
	}
	level := nav.LevelSuccess
	if done.Result.HasFailures() {
		level = nav.LevelWarn
		if st == stepAddRoot {
			m.roots = m.roots[:len(m.roots)-1]
		}
	}
	notice := nav.Notify(level, confirm.Summary(done.Result))

	if st == stepRun && m.running != nil {
		m.output = m.renderOutput(m.running.snapshot(), done.Result)
		m.running = nil
		m.level = levelOutput
		m.detail.SetContent(m.output)
		m.detail.GotoTop()
	}
	m, load := m.startLoad(false)
	return m, tea.Batch(notice, load)
}

func (m Model) openPull(rebase bool) Model {
	mode := "ff-only"
	if rebase {
		mode = "rebase"
	}
	ws := m.env.Paths.Workspace
	p := plan.Plan{Command: "repo.pull"}
	for _, e := range m.entries {
		r := repo.Repository{Path: e.Status.Path}
		p.Actions = append(p.Actions, plan.Action{
			ID:          "pull-" + r.Path,
			Description: "Pull " + m.env.Rel(r.Path),
			Execute: func() error {
				res := repo.PullAll(ws, []repo.Repository{r}, rebase)
				if len(res) > 0 && !res[0].Success {
					return fmt.Errorf("%s", res[0].Error)
				}
				return nil
			},
		})
	}
	m.modal = m.modal.Confirm(stepPull, confirm.New(fmt.Sprintf("Pull %s (%s)", format.Plural(len(p.Actions), "repo"), mode), p))
	return m
}

func (m Model) openSync(msg syncPreparedMsg) Model {
	ws := m.env.Paths.Workspace
	opts := repo.SyncOptions{Rebase: msg.rebase}
	p := plan.Plan{Command: "repo.sync"}
	for _, sp := range msg.plans {
		p.Actions = append(p.Actions, plan.Action{
			ID:          "sync-" + sp.Path,
			Description: repo.DescribeSync(sp, m.env.Rel(sp.Path), msg.rebase),
			Execute: func() error {
				if res := repo.SyncOne(ws, sp, opts); !res.Success {
					return fmt.Errorf("%s", res.Error)
				}
				return nil
			},
		})
	}
	title := "Sync — all repositories are up to date"
	if len(p.Actions) > 0 {
		title = "Sync " + format.Plural(len(p.Actions), "repo")
	}
	c := confirm.New(title, p)
	if len(msg.warnings) > 0 {
		c = c.WithNote("Skipped: " + strings.Join(msg.warnings, "; "))
	}
	m.modal = m.modal.Confirm(stepSync, c)
	return m
}

func (m Model) openRun(command string) Model {
	ws := m.env.Paths.Workspace
	out := &runOutputs{out: map[string]string{}}
	argv := []string{"sh", "-c", command}
	p := plan.Plan{Command: "repo.run"}
	for _, e := range m.entries {
		r := repo.Repository{Path: e.Status.Path}
		p.Actions = append(p.Actions, plan.Action{
			ID:          "run-" + r.Path,
			Description: "Run in " + m.env.Rel(r.Path),
			Execute: func() error {
				res := repo.RunAll(ws, []repo.Repository{r}, argv)
				if len(res) == 0 {
					return nil
				}
				out.set(r.Path, res[0].Output)
				if !res[0].Success {
					return fmt.Errorf("%s", res[0].Error)
				}
				return nil
			},
		})
	}
	m.running = out
	m.modal = m.modal.Confirm(stepRun, confirm.New("Run `"+command+"`", p))
	return m
}

// openAddRoot resolves the path (stat is IO, so it happens in the action),
// and stores it workspace-relative when inside the workspace, as the CLI does.
func (m Model) openAddRoot(input string) (Model, tea.Cmd) {
	abs, err := config.ExpandUserPath(input)
	if err != nil {
		return m, nav.Notify(nav.LevelError, "Invalid path: "+err.Error())
	}
	abs = filepath.Clean(abs)
	for _, r := range m.roots {
		if filepath.Clean(r) == abs {
			return m, nav.Notify(nav.LevelWarn, "Already a repo root: "+abs)
		}
	}
	ws, cfgPath := m.env.Paths.Workspace, m.env.Paths.Config
	stored := abs
	if repo.IsWithin(abs, ws) {
		if rel, err := filepath.Rel(ws, abs); err == nil {
			stored = filepath.ToSlash(rel)
		}
	}
	p := plan.Plan{Command: "repo.add-root", Actions: []plan.Action{{
		ID:          "add-root-" + abs,
		Description: "Add repo root: " + abs,
		Execute: func() error {
			info, err := os.Stat(abs)
			if err != nil {
				return fmt.Errorf("path does not exist or is not accessible")
			}
			if !info.IsDir() {
				return fmt.Errorf("path is not a directory")
			}
			cfg, err := config.Load(cfgPath)
			if err != nil {
				return err
			}
			cfg.Repo.Roots = append(cfg.Repo.Roots, stored)
			return config.Save(cfgPath, cfg)
		},
	}}}
	m.roots = append(append([]string(nil), m.roots...), abs)
	m.modal = m.modal.Confirm(stepAddRoot, confirm.New("Add repo root", p))
	return m, nil
}

func (m Model) rootsHint() string {
	if len(m.roots) == 0 {
		return "No roots configured."
	}
	rel := make([]string, len(m.roots))
	for i, r := range m.roots {
		rel[i] = m.env.Rel(r)
	}
	return "Current roots: " + strings.Join(rel, ", ")
}

func (m Model) renderOutput(outputs map[string]string, res plan.PlanResult) string {
	var b strings.Builder
	b.WriteString(theme.Title.Render("Output of `"+m.runCommand+"`") + "\n")
	for _, e := range m.entries {
		path := e.Status.Path
		status := ""
		for _, a := range res.Actions {
			if a.ID == "run-"+path {
				status = a.Status
			}
		}
		if status == plan.StatusSkipped || status == "" {
			continue
		}
		mark := theme.OK.Render("✔")
		if status == plan.StatusFailed {
			mark = theme.Error.Render("✘")
		}
		b.WriteString("\n" + mark + " " + theme.Bold.Render(m.env.Rel(path)) + "\n")
		if out := strings.TrimRight(outputs[path], "\n"); out != "" {
			b.WriteString(out + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// refreshDetail rebuilds the detail pane content for the current level.
func (m Model) refreshDetail() Model {
	switch m.level {
	case levelOutput:
		m.detail.SetContent(m.output)
	default:
		if m.selected >= 0 && m.selected < len(m.entries) {
			m.detail.SetContent(m.detailContent(m.entries[m.selected]))
		}
	}
	return m
}

func (m Model) rows() []table.Row {
	out := make([]table.Row, 0, len(m.entries))
	for _, e := range m.entries {
		st := e.Status
		branch := st.Branch
		if st.Detached {
			branch = "(detached)"
		}
		issues := ""
		if n := len(e.Findings); n > 0 {
			issues = theme.Warn.Render(strconv.Itoa(n))
		}
		out = append(out, table.Row{
			m.env.Rel(st.Path),
			stateLabel(st),
			countCell(st.Ahead),
			countCell(st.Behind),
			issues,
			branch,
		})
	}
	return out
}

func stateLabel(st repo.RepoStatus) string {
	switch {
	case st.Error != "":
		return theme.Error.Render("✘ error")
	case st.Dirty:
		return theme.Warn.Render("● dirty")
	case !st.HasUpstream:
		return theme.Muted.Render("no upstream")
	default:
		return theme.OK.Render("✔ clean")
	}
}

func countCell(n int) string {
	if n == 0 {
		return theme.Muted.Render("·")
	}
	return strconv.Itoa(n)
}

func (m Model) statusLine() string {
	var s string
	switch m.activity {
	case activityFetching:
		s = theme.Muted.Render("Fetching all remotes…")
	case activityPreparingSync:
		s = theme.Muted.Render("Fetching and planning sync…")
	case activityScanning:
		s = theme.Muted.Render("Scanning…")
	default:
		sm := m.summary
		parts := []string{format.Plural(sm.Repos, "repo")}
		if sm.Dirty > 0 {
			parts = append(parts, theme.Warn.Render(fmt.Sprintf("%d dirty", sm.Dirty)))
		}
		if sm.Ahead > 0 {
			parts = append(parts, fmt.Sprintf("%d ahead", sm.Ahead))
		}
		if sm.Behind > 0 {
			parts = append(parts, fmt.Sprintf("%d behind", sm.Behind))
		}
		if sm.Findings > 0 {
			parts = append(parts, theme.Warn.Render(format.Plural(sm.Findings, "issue")))
		}
		if n := len(m.fetchErrors); n > 0 {
			parts = append(parts, theme.Error.Render(fmt.Sprintf("%d fetch failed", n)))
		}
		s = strings.Join(parts, theme.Muted.Render(" · "))
	}
	return lipgloss.NewStyle().MaxWidth(m.width).Render(s)
}

func (m Model) detailContent(e Entry) string {
	st := e.Status
	var b strings.Builder
	b.WriteString(theme.Title.Render(m.env.Rel(st.Path)) + "\n\n")

	field := func(label, value string) {
		b.WriteString(theme.Muted.Render(fmt.Sprintf("%-10s", label)) + " " + value + "\n")
	}
	branch := st.Branch
	if st.Detached {
		branch = theme.Warn.Render("detached HEAD")
	}
	field("Path", st.Path)
	field("Branch", branch)
	field("State", stateLabel(st))
	if st.HasUpstream {
		field("Upstream", fmt.Sprintf("↑%d ↓%d", st.Ahead, st.Behind))
	} else {
		field("Upstream", theme.Muted.Render("none"))
	}
	if st.Error != "" {
		field("Error", theme.Error.Render(st.Error))
	}

	b.WriteString("\n" + theme.Bold.Render("Hygiene") + "\n")
	if len(e.Findings) == 0 {
		b.WriteString(theme.OK.Render("✔ no findings") + "\n")
	}
	for _, f := range e.Findings {
		sev := theme.Muted.Render("info ")
		switch f.Severity {
		case repo.SeverityWarn:
			sev = theme.Warn.Render("warn ")
		case repo.SeverityError:
			sev = theme.Error.Render("error")
		}
		b.WriteString(fmt.Sprintf("%s %s %s\n", sev, theme.Bold.Render(f.Check), f.Detail))
	}

	if len(m.fetchErrors) > 0 {
		b.WriteString("\n" + theme.Bold.Render("Last fetch failures") + "\n")
		for _, fe := range m.fetchErrors {
			b.WriteString(theme.Error.Render(fe) + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// View implements nav.Component.
func (m Model) View() string {
	switch {
	case m.modal.Open():
		return m.modal.View()
	case m.level == levelDetail, m.level == levelOutput:
		return m.detail.View()
	}
	return lipgloss.JoinVertical(lipgloss.Left, m.statusLine(), m.list.View())
}

// KeyMap implements nav.Component.
func (m Model) KeyMap() help.KeyMap {
	switch {
	case m.modal.Open():
		return m.modal.KeyMap()
	case m.level == levelDetail, m.level == levelOutput:
		return m.detailKeys
	}
	return m.keys
}
