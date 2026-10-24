// Package repos is the git fleet screen: status of every discovered repo,
// a per-repo detail level with hygiene findings, and fleet actions (fetch,
// pull, sync, run, add root) confirmed through the plan checklist. Every key
// is declared in actions with the rows it covers: fetch, pull, sync and run
// cover the marked repos, else every repo the filter shows.
//
// Navigation levels: list → detail | output. esc in detail/output returns to
// the list; esc in the list asks the app to go up. While a dialog is open
// esc closes it instead.
package repos

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/mugenkunou/ws-tool/internal/plan"
	"github.com/mugenkunou/ws-tool/internal/repo"
	"github.com/mugenkunou/ws-tool/internal/tui/complete"
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

// Entry is one repo's status plus its hygiene findings.
type Entry = repo.Entry

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
	only     map[string]bool // repo paths to cover, fixed when s was pressed; nil = all
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

	list     reslist.Model[Entry]
	detail   viewport.Model
	level    level
	activity activity
	modal    modal.Model

	summary     Summary
	fetchErrors []string
	selected    string // path of the entry shown at levelDetail

	runCommand string
	runRows    []Entry // the repos the run prompt is for
	running    *runOutputs
	fetching   int    // repos being fetched
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
		list: reslist.New(repo.Schema(e.View()), []reslist.Col[Entry]{
			{Key: "path", Min: 18, Weight: 3},
			{Key: "state", Min: 11, Style: func(e Entry, _ string) string { return stateLabel(e) }},
			{Key: "ahead", Min: 3, Style: countCell},
			{Key: "behind", Min: 3, Style: countCell},
			{Key: "issues", Min: 6, Style: func(_ Entry, t string) string { return theme.Warn.Render(t) }},
			{Key: "branch", Min: 10, Weight: 1},
		}, theme.Icon("📦")+"No git repositories found. Press a to add a repo root.").Declare(reslist.Decls(actions)),
	}
}

// Init starts the first scan.
func (m Model) Init() tea.Cmd { return load(m.env, m.roots, m.gen, nil) }

func discover(e env.Env, roots []string) ([]repo.Repository, error) {
	return repo.Discover(e.Paths.Workspace, roots, e.ExcludeDirs)
}

// load discovers and scans the fleet, first running `git fetch` in the repos
// whose paths are in fetch (nil fetches none).
func load(e env.Env, roots []string, gen int, fetch map[string]bool) tea.Cmd {
	return func() tea.Msg {
		repos, err := discover(e, roots)
		if err != nil {
			return LoadedMsg{gen: gen, Err: err}
		}

		var fetchErrors []string
		if fetch != nil {
			var targets []repo.Repository
			for _, r := range repos {
				if fetch[r.Path] {
					targets = append(targets, r)
				}
			}
			for _, r := range repo.FetchAll(e.Paths.Workspace, targets) {
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
// plans a sync per repo — only the repos in only, unless it is nil.
func prepareSync(e env.Env, roots []string, gen int, rebase bool, only map[string]bool) tea.Cmd {
	return func() tea.Msg {
		repos, err := discover(e, roots)
		if err != nil {
			return syncPreparedMsg{gen: gen, err: err}
		}
		if only != nil {
			repos = slices.DeleteFunc(repos, func(r repo.Repository) bool { return !only[r.Path] })
		}
		for _, r := range repos {
			if repo.IsWithin(r.Path, e.Paths.Workspace) {
				repo.FetchOne(e.Paths.Workspace, r)
			}
		}
		msg := syncPreparedMsg{gen: gen, rebase: rebase, only: only}
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

// startLoad begins a scan, superseding any in flight. Repos whose paths are
// in fetch are fetched first.
func (m Model) startLoad(fetch map[string]bool) (Model, tea.Cmd) {
	m.gen++
	m.activity = activityScanning
	if fetch != nil {
		m.activity, m.fetching = activityFetching, len(fetch)
	}
	m.list = m.list.SetLoading()
	return m, load(m.env, m.roots, m.gen, fetch)
}

// actions is every repos key and the rows it acts on.
var actions = func() []reslist.Action[Model, Entry] {
	k := keys.Repos()
	pull := func(rebase bool) func(Model, []Entry) (Model, tea.Cmd) {
		return func(m Model, rows []Entry) (Model, tea.Cmd) {
			if m.activity != activityIdle {
				return m, nil
			}
			return m.openPull(rows, rebase), nil
		}
	}
	sync := func(rebase bool) func(Model, []Entry) (Model, tea.Cmd) {
		return func(m Model, rows []Entry) (Model, tea.Cmd) {
			if m.activity != activityIdle {
				return m, nil
			}
			m.gen++
			m.activity = activityPreparingSync
			return m, prepareSync(m.env, m.roots, m.gen, rebase, m.subset(rows))
		}
	}
	return []reslist.Action[Model, Entry]{
		{Key: k.Open, Scope: reslist.Cursor, Do: Model.openDetail},
		{Key: k.Fetch, Scope: reslist.All, Do: func(m Model, rows []Entry) (Model, tea.Cmd) {
			return m.startLoad(paths(rows))
		}},
		{Key: k.Pull, Scope: reslist.All, Do: pull(false)},
		{Key: k.PullRebase, Scope: reslist.All, Do: pull(true)},
		{Key: k.Sync, Scope: reslist.All, Do: sync(false)},
		{Key: k.SyncRebase, Scope: reslist.All, Do: sync(true)},
		{Key: k.Run, Scope: reslist.All, Do: Model.promptRun},
		{Key: k.AddRoot, Scope: reslist.Global, Do: Model.promptAddRoot},
	}
}()

func paths(rows []Entry) map[string]bool {
	out := make(map[string]bool, len(rows))
	for _, e := range rows {
		out[e.Status.Path] = true
	}
	return out
}

// subset is rows as a path set, or nil when rows is every repo.
func (m Model) subset(rows []Entry) map[string]bool {
	if len(rows) == len(m.list.Items()) {
		return nil
	}
	return paths(rows)
}

func (m Model) openDetail(rows []Entry) (Model, tea.Cmd) {
	m.selected = rows[0].Status.Path
	m.level = levelDetail
	m = m.refreshDetail()
	m.detail.GotoTop()
	return m, nil
}

func (m Model) promptRun(rows []Entry) (Model, tea.Cmd) {
	if m.activity != activityIdle {
		return m, nil
	}
	m.runRows = rows
	title := "Run a command in every repo"
	if m.subset(rows) != nil {
		title = "Run a command in " + format.Plural(len(rows), "repo")
	}
	var cmd tea.Cmd
	m.modal, cmd = m.modal.Prompt(stepRunCommand, prompt.New(title,
		prompt.WithHint("Runs with sh -c in each repo root. You can untick repos next."),
		prompt.WithPlaceholder("git status --short"),
		prompt.WithValue(m.runCommand)))
	return m, cmd
}

func (m Model) promptAddRoot([]Entry) (Model, tea.Cmd) {
	var cmd tea.Cmd
	m.modal, cmd = m.modal.Prompt(stepAddRootPath, prompt.New("Add a repo discovery root",
		prompt.WithHint(m.rootsHint()),
		prompt.WithValue(m.env.Paths.Workspace+"/"),
		prompt.WithCompleter(complete.Dirs)))
	return m, cmd
}

// Declared lists the screen's actions and their scopes.
func (m Model) Declared() []reslist.Decl { return m.list.Declared() }

// CapturesKeys implements nav.Component.
func (m Model) CapturesKeys() bool { return m.modal.Open() || m.list.Editing() }

// entry returns the entry for path.
func (m Model) entry(path string) (Entry, bool) {
	for _, e := range m.list.Items() {
		if e.Status.Path == path {
			return e, true
		}
	}
	return Entry{}, false
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
		m.list = m.list.SetSize(msg.Width, max(msg.Height-1, 0)) // one line for the status bar
		m.detail.SetWidth(msg.Width)
		m.detail.SetHeight(msg.Height)
		m.modal = m.modal.SetSize(msg.Width, msg.Height)
		return m.refreshDetail(), nil
	case nav.RefreshMsg:
		return m.startLoad(nil)
	case LoadedMsg:
		if msg.gen != m.gen {
			return m, nil
		}
		m.activity = activityIdle
		if msg.Err != nil {
			m.list = m.list.SetError(msg.Err)
			return m, nil
		}
		m.summary = msg.Summarize()
		m.fetchErrors = msg.FetchErrors
		m.list = m.list.SetItems(msg.Entries)
		if _, ok := m.entry(m.selected); !ok && m.level == levelDetail {
			m.level = levelList
		}
		return m.refreshDetail(), nil
	case SelectMsg:
		var found bool
		if m.list, found = m.list.Select(func(e Entry) bool { return e.Status.Path == msg.Path }); found {
			m.level = levelList
		}
		return m, nil
	case RequestSyncMsg:
		if m.modal.Open() || m.activity != activityIdle {
			return m, nil
		}
		m.level = levelList
		m.gen++
		m.activity = activityPreparingSync
		return m, prepareSync(m.env, m.roots, m.gen, false, nil)
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
	if l, cmd, ok := m.list.HandleKey(msg); ok {
		m.list = l
		return m, cmd
	}
	if next, cmd, ok := reslist.Run(m, m.list, actions, msg); ok {
		return next, cmd
	}
	switch {
	case key.Matches(msg, m.keys.Back):
		return m, nav.Up
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
		return m.openRun(r.Value, m.runRows), nil
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
	m.list = m.list.ClearMarks() // the marks were for this action
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
	m, load := m.startLoad(nil)
	return m, tea.Batch(notice, load)
}

func (m Model) openPull(rows []Entry, rebase bool) Model {
	mode := "ff-only"
	if rebase {
		mode = "rebase"
	}
	ws := m.env.Paths.Workspace
	p := plan.Plan{Command: "repo.pull"}
	for _, e := range rows {
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
		if msg.only != nil && !msg.only[sp.Path] {
			continue
		}
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

func (m Model) openRun(command string, rows []Entry) Model {
	ws := m.env.Paths.Workspace
	out := &runOutputs{out: map[string]string{}}
	argv := []string{"sh", "-c", command}
	p := plan.Plan{Command: "repo.run"}
	for _, e := range rows {
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
	abs, err := wsconfig.ExpandUserPath(input)
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
			cfg, err := wsconfig.Load(cfgPath)
			if err != nil {
				return err
			}
			cfg.Repo.Roots = append(cfg.Repo.Roots, stored)
			return wsconfig.Save(cfgPath, cfg)
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
	for _, e := range m.list.Items() {
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
		if e, ok := m.entry(m.selected); ok {
			m.detail.SetContent(m.detailContent(e))
		}
	}
	return m
}

func stateLabel(e Entry) string {
	switch s := e.State(); s {
	case "error":
		return theme.Error.Render("✘ " + s)
	case "dirty":
		return theme.Warn.Render("● " + s)
	case "no upstream":
		return theme.Muted.Render(s)
	default:
		return theme.OK.Render("✔ " + s)
	}
}

func countCell(_ Entry, t string) string {
	if t == "0" {
		return theme.Muted.Render("·")
	}
	return t
}

func (m Model) statusLine() string {
	var s string
	switch m.activity {
	case activityFetching:
		if m.fetching == len(m.list.Items()) {
			s = theme.Muted.Render("Fetching all remotes…")
		} else {
			s = theme.Muted.Render("Fetching " + format.Plural(m.fetching, "repo") + "…")
		}
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
	field("State", stateLabel(e))
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
	return m.list.Help(m.keys)
}
