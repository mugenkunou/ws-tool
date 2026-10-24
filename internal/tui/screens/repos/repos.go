// Package repos is the git fleet screen (spec §6.5): the rows of `ws repo ls`,
// a detail level (`repo show`), and the fleet keys fetch, pull, save, run,
// configure and add root. Every key and `:` command is declared with the rows
// it covers: f, p, s, r and push cover the marked repos, else every repo the
// filter shows; c covers the marked repos, else the cursor row; enter covers
// the cursor row; a covers no row.
//
// A key and its `:` command share one function. Pull and save fetch first,
// then ask rebase, merge or skip for each diverged repo before the checklist.
// ws never picks the strategy: the plan builders in internal/repo ask a
// Chooser, and the screen answers it only from the user's choices.
//
// Navigation levels: list → detail | output | roots. esc in a level returns
// to the list; esc in the list asks the app to go up. While a dialog is open
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
	"github.com/mugenkunou/ws-tool/internal/tui/choose"
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

// Entry is one repo's status plus its hygiene findings.
type Entry = repo.Entry

// Summary aggregates fleet state for display.
type Summary struct {
	Repos, Dirty, Ahead, Behind, Detached, Findings int
}

// LoadedMsg carries a fleet scan. Other components (the dashboard) may
// observe it.
type LoadedMsg struct {
	Entries []Entry
	// Fetched lists the repos this load fetched, and FetchFailed maps those
	// that failed to git's one-line error. Both are empty unless the load
	// fetched.
	Fetched     []string
	FetchFailed map[string]string
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
		s.Findings += e.Issues() // warn and error findings; info is not an issue
	}
	return s
}

// SelectMsg asks the screen to select a repo by path (sent by the
// dashboard when it opens this screen at a specific problem).
type SelectMsg struct{ Path string }

// Kind is which command a prepared fetch-and-scan is for.
type Kind int

const (
	KindPull Kind = iota
	KindSave
	KindPush
)

// PreparedMsg carries the state a pull, save or push plans from: the repos as
// scanned after the fetch (none for push), the uncommitted counts, and the
// repos whose fetch failed. The screen sends it to itself after the fetch;
// it is exported so tests can feed a scenario without running git.
type PreparedMsg struct {
	Kind     Kind
	Statuses []repo.RepoStatus
	Changes  map[string]int    // uncommitted changes per dirty repo
	Failed   map[string]string // repo path → its fetch error
	Fetched  []string

	gen int
}

// question is one diverged repo the user must answer for.
type question struct {
	path          string
	local, remote int
}

// flow is a pull, save or push in progress: what it plans from and the
// answers given so far.
type flow struct {
	kind     Kind
	statuses []repo.RepoStatus
	changes  map[string]int
	failed   map[string]string
	answers  map[string]repo.Strategy
	message  string // the save commit message
}

type level int

const (
	levelList level = iota
	levelDetail
	levelOutput
	levelRoots
)

type activity int

const (
	activityIdle activity = iota
	activityScanning
	activityFetching
)

// step tags the open dialog so its result is routed to the right handler.
type step int

const (
	stepPull step = iota
	stepSave
	stepPush
	stepDiverged
	stepRunCommand // prompt for the command
	stepRun        // checklist for running it
	stepAddRootPath
	stepAddRoot
	stepRmRoot
)

// handoverConfigure tags the hand-over of `ws repo configure`.
type handoverConfigure struct{}

// runOutputs collects `repo run` results from action goroutines. It is read
// only after the checklist reports done.
type runOutputs struct {
	mu  sync.Mutex
	res map[string]repo.OperationResult
}

func (r *runOutputs) set(path string, res repo.OperationResult) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.res[path] = res
}

func (r *runOutputs) snapshot() map[string]repo.OperationResult {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := make(map[string]repo.OperationResult, len(r.res))
	for k, v := range r.res {
		cp[k] = v
	}
	return cp
}

// Model is the repos screen state.
type Model struct {
	env        env.Env
	roots      []string // starts as env.RepoRoots; re-read after a root changes
	keys       keys.ReposMap
	detailKeys keys.DetailMap

	list     reslist.Model[Entry]
	detail   viewport.Model
	level    level
	activity activity
	modal    modal.Model

	summary     Summary
	fetchFailed map[string]string // repo path → its last fetch's error, this session
	selected    string            // path of the entry shown at levelDetail

	flow       *flow
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
	k := keys.Repos()
	return Model{
		env:         e,
		roots:       e.RepoRoots,
		keys:        k,
		detailKeys:  dk,
		detail:      vp,
		activity:    activityScanning,
		fetchFailed: map[string]string{},
		list: reslist.New(repo.Schema(e.View()), []reslist.Col[Entry]{
			{Key: "path", Min: 18, Weight: 3},
			{Key: "state", Min: 11, Style: func(e Entry, _ string) string { return stateLabel(e) }},
			{Key: "ahead", Min: 3, Style: countCell},
			{Key: "behind", Min: 3, Style: countCell},
			{Key: "issues", Min: 6, Style: func(_ Entry, t string) string { return theme.Warn.Render(t) }},
			{Key: "branch", Min: 10, Weight: 1},
		}, theme.Icon("📦")+"No git repositories found. Press "+k.AddRoot.Help().Key+" to add a repo root.").
			Declare(reslist.Decls(actions)),
	}
}

// Init starts the first scan. It does not fetch.
func (m Model) Init() tea.Cmd { return load(m.env, m.roots, m.gen, nil) }

func discover(e env.Env, roots []string) ([]repo.Repository, error) {
	return repo.Discover(e.Paths.Workspace, roots, e.ExcludeDirs)
}

// oneLine flattens git's multi-line error text for one line.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// load discovers and scans the fleet, first running `git fetch` in the repos
// whose paths are in fetch (nil fetches none).
func load(e env.Env, roots []string, gen int, fetch map[string]bool) tea.Cmd {
	return func() tea.Msg {
		repos, err := discover(e, roots)
		if err != nil {
			return LoadedMsg{gen: gen, Err: err}
		}

		msg := LoadedMsg{gen: gen}
		if fetch != nil {
			var targets []repo.Repository
			for _, r := range repos {
				if fetch[r.Path] {
					targets = append(targets, r)
				}
			}
			msg.FetchFailed = map[string]string{}
			for _, r := range repo.FetchAll(e.Paths.Workspace, targets) {
				msg.Fetched = append(msg.Fetched, r.Path)
				if !r.Success {
					msg.FetchFailed[r.Path] = oneLine(r.Error)
				}
			}
		}

		statuses := repo.Scan(e.Paths.Workspace, repos)
		msg.Entries = make([]Entry, 0, len(statuses))
		for i, st := range statuses {
			findings := repo.Doctor(e.Paths.Workspace, repos[i:i+1], repo.DoctorOptions{})
			msg.Entries = append(msg.Entries, Entry{Status: st, Findings: findings})
		}
		return msg
	}
}

// prepare fetches the repos in rows (push does not fetch) and reads their
// state, for the plan of pull, save or push to build on.
func prepare(e env.Env, gen int, kind Kind, rows []Entry) tea.Cmd {
	return func() tea.Msg {
		ws := e.Paths.Workspace
		repos := make([]repo.Repository, len(rows))
		for i, r := range rows {
			repos[i] = repo.Repository{Path: r.Status.Path}
		}
		msg := PreparedMsg{gen: gen, Kind: kind, Failed: map[string]string{}, Changes: map[string]int{}}
		if kind != KindPush {
			for _, r := range repo.FetchAll(ws, repos) {
				msg.Fetched = append(msg.Fetched, r.Path)
				if !r.Success {
					msg.Failed[r.Path] = oneLine(r.Error)
				}
			}
		}
		msg.Statuses = repo.Scan(ws, repos)
		if kind != KindPull {
			for _, st := range msg.Statuses {
				if st.Dirty {
					msg.Changes[st.Path] = repo.UncommittedCount(st.Path)
				}
			}
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
	return []reslist.Action[Model, Entry]{
		{Key: k.Open, Scope: reslist.Cursor, Do: Model.openDetail},
		{Key: k.Fetch, Scope: reslist.All, Do: Model.fetch},
		{Key: k.Pull, Scope: reslist.All, Do: Model.pull},
		{Key: k.Save, Scope: reslist.All, Do: Model.save},
		{Key: k.Run, Scope: reslist.All, Do: Model.promptRun},
		{Key: k.Configure, Scope: reslist.Selected, Do: Model.configure},
		{Key: k.AddRoot, Scope: reslist.Global, Do: Model.promptAddRoot},
	}
}()

// commands is the screen's `:` commands (spec §6.5). Each shares its function
// with the key of the same verb.
var commands = []reslist.Command[Model, Entry]{
	{Name: "push", Help: "push the shown or marked repos", Scope: reslist.All,
		Do: func(m Model, rows []Entry, _ []string) (Model, tea.Cmd) { return m.push(rows) }},
	{Name: "root ls", Help: "list the discovery roots", Scope: reslist.Global,
		Do: func(m Model, _ []Entry, _ []string) (Model, tea.Cmd) { return m.showRoots() }},
	{Name: "root rm", Args: "<dir>", Help: "remove a discovery root", Scope: reslist.Global,
		Do: func(m Model, _ []Entry, args []string) (Model, tea.Cmd) { return m.removeRoot(args) }},
	{Name: "ls --fetch", Help: "fetch the shown or marked repos, then list", Scope: reslist.All,
		Do: func(m Model, rows []Entry, _ []string) (Model, tea.Cmd) { return m.fetch(rows) }},
}

// Commands implements nav.Commander.
func (m Model) Commands() []nav.CommandInfo { return reslist.CommandInfos(commands) }

func paths(rows []Entry) map[string]bool {
	out := make(map[string]bool, len(rows))
	for _, e := range rows {
		out[e.Status.Path] = true
	}
	return out
}

func (m Model) openDetail(rows []Entry) (Model, tea.Cmd) {
	m.selected = rows[0].Status.Path
	m.level = levelDetail
	m = m.refreshDetail()
	m.detail.GotoTop()
	return m, nil
}

func (m Model) fetch(rows []Entry) (Model, tea.Cmd) {
	m.level = levelList
	return m.startLoad(paths(rows))
}

func (m Model) pull(rows []Entry) (Model, tea.Cmd) { return m.startFlow(KindPull, rows) }
func (m Model) save(rows []Entry) (Model, tea.Cmd) { return m.startFlow(KindSave, rows) }
func (m Model) push(rows []Entry) (Model, tea.Cmd) { return m.startFlow(KindPush, rows) }

// startFlow fetches rows (push reads only) and then plans the command.
func (m Model) startFlow(kind Kind, rows []Entry) (Model, tea.Cmd) {
	if m.activity != activityIdle || m.modal.Open() {
		return m, nil
	}
	m.level = levelList
	m.gen++
	if kind == KindPush {
		m.activity = activityScanning
	} else {
		m.activity, m.fetching = activityFetching, len(rows)
	}
	return m, prepare(m.env, m.gen, kind, rows)
}

func (m Model) configure(rows []Entry) (Model, tea.Cmd) {
	args := []string{"repo", "configure"}
	for _, e := range rows {
		args = append(args, e.Status.Path)
	}
	return m, handover.Run(handover.Pause(handover.WS(m.env.Paths, args...)), handoverConfigure{})
}

func (m Model) promptRun(rows []Entry) (Model, tea.Cmd) {
	if m.activity != activityIdle {
		return m, nil
	}
	m.runRows = rows
	title := "Run a command in " + format.Plural(len(rows), "repo")
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

// recordFetch folds a fetch's outcome into the session's failures.
func (m Model) recordFetch(fetched []string, failed map[string]string) Model {
	next := make(map[string]string, len(m.fetchFailed))
	for p, e := range m.fetchFailed {
		next[p] = e
	}
	for _, p := range fetched {
		delete(next, p)
	}
	for p, e := range failed {
		next[p] = e
	}
	m.fetchFailed = next
	return m
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
		m.list = m.list.SetSize(msg.Width, max(msg.Height-1, 0)) // one line for the summary
		m.detail.SetWidth(msg.Width)
		m.detail.SetHeight(msg.Height)
		m.modal = m.modal.SetSize(msg.Width, msg.Height)
		return m.refreshDetail(), nil
	case nav.RefreshMsg:
		return m.startLoad(nil) // ctrl+r re-reads without fetching
	case nav.FilterMsg:
		m.list = m.list.SetFilter(msg.Filter)
		m.level = levelList
		return m, nil
	case nav.CommandMsg:
		if m.modal.Open() {
			return m, nil
		}
		m.level = levelList
		next, cmd, _ := reslist.RunCommand(m, m.list, commands, msg)
		return next, cmd
	case LoadedMsg:
		if msg.gen != m.gen {
			return m, nil
		}
		m.activity = activityIdle
		if msg.Err != nil {
			m.list = m.list.SetError(msg.Err)
			return m, nil
		}
		m = m.recordFetch(msg.Fetched, msg.FetchFailed)
		m.summary = msg.Summarize()
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
	case PreparedMsg:
		if msg.gen != m.gen {
			return m, nil
		}
		m.activity = activityIdle
		m = m.recordFetch(msg.Fetched, msg.Failed)
		f := &flow{kind: msg.Kind, statuses: msg.Statuses, changes: msg.Changes, failed: msg.Failed,
			answers: map[string]repo.Strategy{}}
		if msg.Kind == KindSave {
			host, _ := os.Hostname()
			f.message = repo.DefaultSaveMessage(m.env.Now(), host)
		}
		m.flow = f
		var load tea.Cmd
		if msg.Kind != KindPush {
			m, load = m.startLoad(nil) // the fetch moved the counts
		}
		m, cmd := m.advance()
		return m, tea.Batch(load, cmd)
	case handover.DoneMsg:
		if _, ok := msg.Tag.(handoverConfigure); ok {
			m, load := m.startLoad(nil)
			if msg.Err != nil {
				return m, tea.Batch(load, nav.Notify(nav.LevelError, "repo configure: "+msg.Err.Error()))
			}
			return m, load
		}
		return m, nil
	}

	if m.modal.Open() {
		var cmd tea.Cmd
		m.modal, cmd = m.modal.Update(msg)
		return m, cmd
	}
	if k, ok := msg.(tea.KeyPressMsg); ok {
		switch m.level {
		case levelDetail, levelOutput, levelRoots:
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
	case stepDiverged:
		return m.answerDiverged(msg.(choose.ResultMsg))
	}

	done := msg.(confirm.DoneMsg)
	if done.Result.Aborted {
		m.flow = nil
		return m, nil
	}
	m.flow = nil
	m.list = m.list.ClearMarks() // the marks were for this action
	level := nav.LevelSuccess
	if done.Result.HasFailures() {
		level = nav.LevelWarn
	}
	notice := nav.Notify(level, confirm.Summary(done.Result))

	switch st {
	case stepRun:
		if m.running != nil {
			m.output = m.renderOutput(m.running.snapshot(), done.Result)
			m.running = nil
			m.level = levelOutput
			m.detail.SetContent(m.output)
			m.detail.GotoTop()
		}
	case stepAddRoot, stepRmRoot:
		m.roots = m.readRoots() // the list is re-read from the new roots below
	}
	m, load := m.startLoad(nil)
	return m, tea.Batch(notice, load)
}

// ── pull, save and push ────────────────────────────────────────────

// planned is what a flow's command would do with the answers so far.
type planned struct {
	actions   []plan.Action
	skipped   []repo.Skip
	questions []question
}

// plan builds the actions of the flow. Its Chooser never picks: a diverged
// repo with no answer yet is recorded as a question and skipped for now, and
// the user's answer is passed in when there is one.
func (m Model) plan(f *flow) planned {
	var p planned
	choose := func(path string, local, remote int) (repo.Strategy, string) {
		if s, ok := f.answers[path]; ok {
			return s, "skipped: you chose to skip it"
		}
		if !slices.ContainsFunc(p.questions, func(q question) bool { return q.path == path }) {
			p.questions = append(p.questions, question{path: path, local: local, remote: remote})
		}
		return repo.StrategySkip, repo.ReasonDiverged
	}
	for _, st := range f.statuses {
		if msg, bad := f.failed[st.Path]; bad && f.kind != KindPush {
			p.skipped = append(p.skipped, repo.Skip{Path: st.Path, Reason: "fetch failed: " + msg})
			continue
		}
		path, short := st.Path, m.env.Rel(st.Path)
		switch f.kind {
		case KindPull:
			item, skip := repo.PlanPull(st, choose)
			if skip != nil {
				p.skipped = append(p.skipped, *skip)
			} else if item != nil {
				it := *item
				p.actions = append(p.actions, plan.Action{ID: "pull-" + path, Description: it.Describe(short),
					Execute: func() error { return repo.Pull(it.Path, it.Strategy) }})
			}
		case KindSave:
			item, skip := repo.PlanSave(st, f.changes[path], choose)
			if skip != nil {
				p.skipped = append(p.skipped, *skip)
			} else if item != nil {
				it, msg := *item, f.message
				p.actions = append(p.actions, plan.Action{ID: "save-" + path, Description: it.Describe(short),
					Execute: func() error { return repo.Save(it, msg) }})
			}
		case KindPush:
			if reason := repo.PushSkip(st); reason != "" {
				p.skipped = append(p.skipped, repo.Skip{Path: path, Reason: reason})
				continue
			}
			detail := fmt.Sprintf("%d ahead", st.Ahead)
			if st.Dirty {
				if n := f.changes[path]; n == 1 {
					detail += "; 1 uncommitted change stays local"
				} else if n > 1 {
					detail += fmt.Sprintf("; %d uncommitted changes stay local", n)
				}
			}
			p.actions = append(p.actions, plan.Action{ID: "push-" + path, Description: fmt.Sprintf("Push %s (%s)", short, detail),
				Execute: func() error { return repo.Push(path) }})
		}
	}
	return p
}

// advance asks the next unanswered diverged repo, or opens the checklist when
// every answer is in.
func (m Model) advance() (Model, tea.Cmd) {
	f := m.flow
	p := m.plan(f)
	if len(p.questions) > 0 {
		q := p.questions[0]
		title := fmt.Sprintf("%s has diverged (%d local, %d remote commit%s)",
			m.env.Rel(q.path), q.local, q.remote, map[bool]string{true: "", false: "s"}[q.remote == 1])
		m.modal = m.modal.Choose(stepDiverged, choose.New(title, "Pull with rebase or merge, or skip this repo.", []choose.Option{
			{Label: "rebase", Key: "r"},
			{Label: "merge", Key: "m"},
			{Label: "skip this repo", Key: "s"},
		}))
		return m, nil
	}

	verb, st, nothing := "Pull", stepPull, "All repositories are up to date."
	switch f.kind {
	case KindSave:
		verb, st, nothing = "Save", stepSave, "Nothing to save."
	case KindPush:
		verb, st, nothing = "Push", stepPush, "Nothing to push."
	}
	var skipped []string
	for _, s := range p.skipped {
		skipped = append(skipped, fmt.Sprintf("%s (%s)", m.env.Rel(s.Path), s.Reason))
	}
	if len(p.actions) == 0 {
		m.flow = nil
		text := nothing
		if len(skipped) > 0 {
			text = "Nothing to " + strings.ToLower(verb) + ". Skipped: " + strings.Join(skipped, "; ")
		}
		return m, nav.Notify(nav.LevelInfo, text)
	}
	c := confirm.New(verb+" "+format.Plural(len(p.actions), "repo"), plan.Plan{Command: "repo." + strings.ToLower(verb), Actions: p.actions})
	if len(skipped) > 0 {
		c = c.WithNote("Skipped: " + strings.Join(skipped, "; "))
	}
	m.modal = m.modal.Confirm(st, c)
	return m, nil
}

// answerDiverged records the answer to the open question. esc cancels the
// whole command, so nothing runs.
func (m Model) answerDiverged(r choose.ResultMsg) (nav.Component, tea.Cmd) {
	f := m.flow
	if r.Cancelled || f == nil {
		m.flow = nil
		return m, nil
	}
	qs := m.plan(f).questions
	if len(qs) == 0 {
		return m, nil
	}
	answers := make(map[string]repo.Strategy, len(f.answers)+1)
	for k, v := range f.answers {
		answers[k] = v
	}
	answers[qs[0].path] = [...]repo.Strategy{repo.StrategyRebase, repo.StrategyMerge, repo.StrategySkip}[r.Index]
	next := *f
	next.answers = answers
	m.flow = &next
	return m.advance()
}

// ── run ────────────────────────────────────────────────────────────

func (m Model) openRun(command string, rows []Entry) Model {
	ws := m.env.Paths.Workspace
	out := &runOutputs{res: map[string]repo.OperationResult{}}
	argv := []string{"sh", "-c", command}
	p := plan.Plan{Command: "repo.run"}
	for _, e := range rows {
		r := repo.Repository{Path: e.Status.Path}
		p.Actions = append(p.Actions, plan.Action{
			ID:          "run-" + r.Path,
			Description: "Run in " + m.env.Rel(r.Path),
			Execute: func() error {
				res := repo.RunOne(ws, r, argv)
				out.set(r.Path, res)
				if !res.Success {
					return fmt.Errorf("%s", res.Error)
				}
				return nil
			},
		})
	}
	m.running = out
	m.modal = m.modal.Confirm(stepRun, confirm.New("Run `"+command+"`", p))
	return m
}

func (m Model) renderOutput(results map[string]repo.OperationResult, res plan.PlanResult) string {
	var b strings.Builder
	b.WriteString(theme.Title.Render("Output of `"+m.runCommand+"`") + "\n")
	for _, e := range m.runRows {
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
		r := results[path]
		mark := theme.OK.Render("✔")
		if status == plan.StatusFailed {
			mark = theme.Error.Render(fmt.Sprintf("✘ (exit %d)", r.ExitCode))
		}
		b.WriteString("\n" + mark + " " + theme.Bold.Render(m.env.Rel(path)) + "\n")
		if out := strings.TrimRight(r.Output, "\n"); out != "" {
			b.WriteString(out + "\n")
		}
	}
	return strings.TrimRight(b.String()+"\n\n"+theme.Muted.Render(m.detailKeys.Back.Help().Key+" back"), "\n")
}

// ── roots ──────────────────────────────────────────────────────────

// effectiveRoots is the roots discovery walks: the workspace itself when none
// is configured.
func (m Model) effectiveRoots() []string {
	if len(m.roots) == 0 {
		return []string{filepath.Clean(m.env.Paths.Workspace)}
	}
	return m.roots
}

// readRoots re-reads the roots from ws.json, keeping the current ones when it
// cannot be read.
func (m Model) readRoots() []string {
	cfg, err := wsconfig.Load(m.env.Paths.Config)
	if err != nil {
		return m.roots
	}
	var roots []string
	for _, r := range cfg.Repo.Roots {
		if resolved, err := wsconfig.ResolvePath(m.env.Paths.Workspace, r); err == nil {
			roots = append(roots, resolved)
		}
	}
	return roots
}

func (m Model) rootsHint() string {
	rel := make([]string, 0, len(m.roots))
	for _, r := range m.effectiveRoots() {
		rel = append(rel, m.env.Rel(r))
	}
	return "Current roots: " + strings.Join(rel, ", ")
}

// showRoots is `root ls`: a read-only view; esc returns.
func (m Model) showRoots() (Model, tea.Cmd) {
	var b strings.Builder
	b.WriteString(theme.Title.Render("Repo discovery roots") + "\n\n")
	for _, r := range m.effectiveRoots() {
		b.WriteString(r + "\n")
	}
	b.WriteString("\n" + theme.Muted.Render(m.detailKeys.Back.Help().Key+" back"))
	m.level = levelRoots
	m.detail.SetContent(b.String())
	m.detail.GotoTop()
	return m, nil
}

// openAddRoot checks the path as `ws repo root add` does, then opens the
// one-action checklist. The root is stored workspace-relative.
func (m Model) openAddRoot(input string) (Model, tea.Cmd) {
	abs, err := wsconfig.ExpandUserPath(input)
	if err != nil {
		return m, nav.Notify(nav.LevelError, "Invalid path: "+err.Error())
	}
	abs = filepath.Clean(abs)
	ws, cfgPath := m.env.Paths.Workspace, m.env.Paths.Config
	info, statErr := os.Stat(abs)
	switch {
	case statErr != nil:
		return m, nav.Notify(nav.LevelError, "No such directory: "+abs)
	case !info.IsDir():
		return m, nav.Notify(nav.LevelError, "Not a directory: "+abs)
	case !repo.IsWithin(abs, ws):
		return m, nav.Notify(nav.LevelError, "Outside the workspace: "+abs+" (discovery never lists repos outside it)")
	case slices.Contains(m.effectiveRoots(), abs):
		return m, nav.Notify(nav.LevelWarn, "Already a repo root: "+abs)
	}
	rel, err := filepath.Rel(filepath.Clean(ws), abs)
	if err != nil {
		return m, nav.Notify(nav.LevelError, err.Error())
	}
	stored := filepath.ToSlash(rel)
	p := plan.Plan{Command: "repo.root.add", Actions: []plan.Action{{
		ID:          "root-add-" + abs,
		Description: "Add repo root " + abs,
		Execute: func() error {
			cfg, err := wsconfig.Load(cfgPath)
			if err != nil {
				return err
			}
			cfg.Repo.Roots = append(cfg.Repo.Roots, stored)
			return wsconfig.Save(cfgPath, cfg)
		},
	}}}
	m.modal = m.modal.Confirm(stepAddRoot, confirm.New("Add repo root", p))
	return m, nil
}

// removeRoot is `root rm <dir>`: a one-action checklist. Bare, it lists every
// root with none ticked. The last root cannot go: an empty list means the
// whole workspace, so removing it would widen discovery.
func (m Model) removeRoot(args []string) (Model, tea.Cmd) {
	if m.activity != activityIdle || m.modal.Open() {
		return m, nil
	}
	roots := m.effectiveRoots()
	var targets []string
	switch len(args) {
	case 0:
		targets = roots
	case 1:
		abs, err := wsconfig.ExpandUserPath(args[0])
		if err != nil {
			return m, nav.Notify(nav.LevelError, "Invalid path: "+err.Error())
		}
		abs = filepath.Clean(abs)
		if !slices.Contains(roots, abs) {
			return m, nav.Notify(nav.LevelError, "Not a repo root: "+abs)
		}
		targets = []string{abs}
	default:
		return m, nav.Notify(nav.LevelWarn, "usage: root rm <dir>")
	}
	if len(roots) <= 1 {
		return m, nav.Notify(nav.LevelError, "Cannot remove the last repo root; add another first")
	}

	ws, cfgPath := m.env.Paths.Workspace, m.env.Paths.Config
	p := plan.Plan{Command: "repo.root.rm"}
	for _, abs := range targets {
		p.Actions = append(p.Actions, plan.Action{
			ID:          "root-rm-" + abs,
			Description: "Remove repo root " + abs,
			Execute: func() error {
				cfg, err := wsconfig.Load(cfgPath)
				if err != nil {
					return err
				}
				kept := slices.DeleteFunc(slices.Clone(cfg.Repo.Roots), func(r string) bool {
					resolved, err := wsconfig.ResolvePath(ws, r)
					return err == nil && resolved == abs
				})
				if len(kept) == 0 {
					return fmt.Errorf("cannot remove the last repo root")
				}
				cfg.Repo.Roots = kept
				return wsconfig.Save(cfgPath, cfg)
			},
		})
	}
	c := confirm.New("Remove "+format.Plural(len(targets), "repo root"), p)
	if len(args) == 0 {
		c = c.WithChecked(func(int, plan.Action) bool { return false })
	}
	m.modal = m.modal.Confirm(stepRmRoot, c)
	return m, nil
}

// ── view ───────────────────────────────────────────────────────────

// refreshDetail rebuilds the detail pane content for the current level.
func (m Model) refreshDetail() Model {
	if m.level == levelDetail {
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
		s = theme.Muted.Render("Fetching " + format.Plural(m.fetching, "repo") + "…")
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
		failed := 0
		for _, e := range m.list.Items() {
			if _, bad := m.fetchFailed[e.Status.Path]; bad {
				failed++
			}
		}
		if failed > 0 {
			parts = append(parts, theme.Error.Render(fmt.Sprintf("%d fetch failed", failed)))
		}
		s = strings.Join(parts, theme.Muted.Render(" · "))
	}
	return lipgloss.NewStyle().MaxWidth(m.width).Render(s)
}

// detailContent is `repo show` for e, plus the line for this repo's failed
// fetch in the session.
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
	if msg, bad := m.fetchFailed[st.Path]; bad {
		field("Last fetch", theme.Error.Render("failed: "+msg))
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
	return strings.TrimRight(b.String(), "\n")
}

// View implements nav.Component.
func (m Model) View() string {
	switch {
	case m.modal.Open():
		return m.modal.View()
	case m.level != levelList:
		return m.detail.View()
	}
	return lipgloss.JoinVertical(lipgloss.Left, m.statusLine(), m.list.View())
}

// KeyMap implements nav.Component.
func (m Model) KeyMap() help.KeyMap {
	switch {
	case m.modal.Open():
		return m.modal.KeyMap()
	case m.level != levelList:
		return m.detailKeys
	}
	return m.list.Help(m.keys)
}
