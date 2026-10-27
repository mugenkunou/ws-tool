// Package repos is the git fleet screen (spec §6.5): the rows of `ws repo ls`,
// a detail level (`repo show`), and one action key. ! opens a prompt
// pre-filled with `git ` for the marked repos, else every repo the filter
// shows, or for the one repo the detail level shows. The line runs with
// sh -c in each repo root, so erasing `git ` runs any command. One repo gets
// the terminal (handed over, §6.8); more run through the checklist and their
// output opens in the output view. c configures the marked repos, else the
// cursor row; enter opens the cursor row.
//
// The prompt's ↑ and ↓ step through the lines run before, and its
// suggestions put the most-run matching line first, so the commands you use
// most are a tab away. The `:` commands take the CLI's words: `:git <args>`
// and `:run <cmd>` skip the prompt.
//
// Navigation levels: list → detail | output | roots. esc in a level returns
// to the list; esc in the list asks the app to go up. While a dialog is open
// esc closes it instead.
package repos

import (
	"fmt"
	"os"
	"os/exec"
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
	Err     error

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
)

// step tags the open dialog so its result is routed to the right handler.
type step int

const (
	stepCommand step = iota // prompt for the command line
	stepRun                 // checklist for running it
	stepAddRootPath
	stepAddRoot
	stepRmRoot
)

// handoverConfigure tags the hand-over of `ws repo configure`.
type handoverConfigure struct{}

// handoverRun tags a command line run in one repo on the terminal.
type handoverRun struct{ line, path string }

// runOutputs collects run results from action goroutines. It is read only
// after the checklist reports done.
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
	detailKeys keys.ReposDetailMap

	list     reslist.Model[Entry]
	detail   viewport.Model
	level    level
	activity activity
	modal    modal.Model

	summary  Summary
	selected string // path of the entry shown at levelDetail

	history repo.History // the command lines run, for the prompt
	runLine string       // the line being run, or last run
	runRows []Entry      // the repos it runs in
	running *runOutputs
	output  string // rendered run output shown at levelOutput

	gen    int // incremented per load; stale results are dropped
	width  int
	height int
}

// New creates the screen.
func New(e env.Env) Model {
	dk := keys.ReposDetail()
	vp := viewport.New()
	vp.KeyMap = dk.Viewport
	vp.SoftWrap = true
	k := keys.Repos()
	m := Model{
		env:        e,
		roots:      e.RepoRoots,
		keys:       k,
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
		}, theme.Icon("📦")+"No git repositories found. Type :root add <dir> to add a repo root.").
			Declare(reslist.Decls(actions)),
	}
	if e.HistoryFile != "" {
		m.history = repo.LoadHistory(e.HistoryFile)
	}
	return m
}

// Init starts the first scan.
func (m Model) Init() tea.Cmd { return load(m.env, m.roots, m.gen) }

func discover(e env.Env, roots []string) ([]repo.Repository, error) {
	return repo.Discover(e.Paths.Workspace, roots, e.ExcludeDirs)
}

// load discovers and scans the fleet.
func load(e env.Env, roots []string, gen int) tea.Cmd {
	return func() tea.Msg {
		repos, err := discover(e, roots)
		if err != nil {
			return LoadedMsg{gen: gen, Err: err}
		}
		statuses := repo.Scan(e.Paths.Workspace, repos)
		msg := LoadedMsg{gen: gen, Entries: make([]Entry, 0, len(statuses))}
		for i, st := range statuses {
			findings := repo.Doctor(e.Paths.Workspace, repos[i:i+1], repo.DoctorOptions{})
			msg.Entries = append(msg.Entries, Entry{Status: st, Findings: findings})
		}
		return msg
	}
}

// startLoad begins a scan, superseding any in flight.
func (m Model) startLoad() (Model, tea.Cmd) {
	m.gen++
	m.activity = activityScanning
	m.list = m.list.SetLoading()
	return m, load(m.env, m.roots, m.gen)
}

// actions is every repos key and the rows it acts on.
var actions = func() []reslist.Action[Model, Entry] {
	k := keys.Repos()
	return []reslist.Action[Model, Entry]{
		{Key: k.Open, Scope: reslist.Cursor, Do: Model.openDetail},
		{Key: k.Git, Scope: reslist.All, Do: Model.promptCommand},
		{Key: k.Configure, Scope: reslist.Selected, Do: Model.configure},
	}
}()

// commands is the screen's `:` commands (spec §6.5), the CLI's words after
// `repo`. `git` and `run` with words run them; bare, they open the prompt.
var commands = []reslist.Command[Model, Entry]{
	{Name: "git", Args: "<args…>", Help: "run a git command in the shown or marked repos", Scope: reslist.All,
		Do: func(m Model, rows []Entry, args []string) (Model, tea.Cmd) {
			if len(args) == 0 {
				return m.promptCommand(rows)
			}
			return m.start("git "+strings.Join(args, " "), rows)
		}},
	{Name: "run", Args: "<cmd…>", Help: "run any command in the shown or marked repos", Scope: reslist.All,
		Do: func(m Model, rows []Entry, args []string) (Model, tea.Cmd) {
			if len(args) == 0 {
				return m.promptLine(rows, "")
			}
			return m.start(strings.Join(args, " "), rows)
		}},
	{Name: "configure", Help: "fix the hygiene findings of the marked repos, else the cursor repo", Scope: reslist.Selected,
		Do: func(m Model, rows []Entry, _ []string) (Model, tea.Cmd) { return m.configure(rows) }},
	{Name: "root add", Args: "<dir>", Help: "add a discovery root", Scope: reslist.Global,
		Do: func(m Model, _ []Entry, args []string) (Model, tea.Cmd) {
			if len(args) == 0 {
				return m.promptAddRoot()
			}
			return m.openAddRoot(args[0])
		}},
	{Name: "root ls", Help: "list the discovery roots", Scope: reslist.Global,
		Do: func(m Model, _ []Entry, _ []string) (Model, tea.Cmd) { return m.showRoots() }},
	{Name: "root rm", Args: "<dir>", Help: "remove a discovery root", Scope: reslist.Global,
		Do: func(m Model, _ []Entry, args []string) (Model, tea.Cmd) { return m.removeRoot(args) }},
}

// Commands implements nav.Commander.
func (m Model) Commands() []nav.CommandInfo { return reslist.CommandInfos(commands) }

func (m Model) openDetail(rows []Entry) (Model, tea.Cmd) {
	m.selected = rows[0].Status.Path
	m.level = levelDetail
	m = m.refreshDetail()
	m.detail.GotoTop()
	return m, nil
}

func (m Model) configure(rows []Entry) (Model, tea.Cmd) {
	args := []string{"repo", "configure"}
	for _, e := range rows {
		args = append(args, e.Status.Path)
	}
	return m, handover.Run(handover.Pause(handover.WS(m.env.Paths, args...)), handoverConfigure{})
}

func (m Model) promptAddRoot() (Model, tea.Cmd) {
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
		m.list = m.list.SetSize(msg.Width, max(msg.Height-1, 0)) // one line for the summary
		m.detail.SetWidth(msg.Width)
		m.detail.SetHeight(msg.Height)
		m.modal = m.modal.SetSize(msg.Width, msg.Height)
		return m.refreshDetail(), nil
	case nav.RefreshMsg:
		return m.startLoad()
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
	case handover.DoneMsg:
		var notice tea.Cmd
		switch tag := msg.Tag.(type) {
		case handoverConfigure:
			if msg.Err != nil {
				notice = nav.Notify(nav.LevelError, "repo configure: "+msg.Err.Error())
			}
		case handoverRun:
			if msg.Err != nil {
				notice = nav.Notify(nav.LevelWarn, fmt.Sprintf("`%s` in %s: %s", tag.line, m.env.Rel(tag.path), msg.Err.Error()))
			}
		default:
			return m, nil
		}
		m, load := m.startLoad() // the command may have changed every count
		return m, tea.Batch(notice, load)
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

// updateDetail scrolls a detail level. In the repo's detail, ! and c act on
// that repo.
func (m Model) updateDetail(msg tea.KeyPressMsg) (nav.Component, tea.Cmd) {
	if key.Matches(msg, m.detailKeys.Back) {
		m.level = levelList
		return m, nil
	}
	if m.level == levelDetail {
		if e, ok := m.entry(m.selected); ok {
			switch {
			case key.Matches(msg, m.detailKeys.Git):
				return m.promptCommand([]Entry{e})
			case key.Matches(msg, m.detailKeys.Configure):
				return m.configure([]Entry{e})
			}
		}
	}
	var cmd tea.Cmd
	m.detail, cmd = m.detail.Update(msg)
	return m, cmd
}

// dialogResult handles the outcome of the dialog opened for st.
func (m Model) dialogResult(st step, msg tea.Msg) (nav.Component, tea.Cmd) {
	switch st {
	case stepCommand:
		r := msg.(prompt.ResultMsg)
		if r.Cancelled {
			return m, nil
		}
		return m.start(r.Value, m.runRows)
	case stepAddRootPath:
		r := msg.(prompt.ResultMsg)
		if r.Cancelled {
			return m, nil
		}
		return m.openAddRoot(r.Value)
	}

	done := msg.(confirm.DoneMsg)
	if done.Result.Aborted {
		m.running = nil
		return m, nil
	}
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
	m, load := m.startLoad()
	return m, tea.Batch(notice, load)
}

// ── git and run ────────────────────────────────────────────────────

// promptCommand is !: the command prompt, pre-filled with `git `.
func (m Model) promptCommand(rows []Entry) (Model, tea.Cmd) { return m.promptLine(rows, "git ") }

// promptLine opens the command prompt for rows with value typed in.
func (m Model) promptLine(rows []Entry, value string) (Model, tea.Cmd) {
	if m.activity != activityIdle || m.modal.Open() {
		return m, nil
	}
	m.runRows = rows
	where := format.Plural(len(rows), "repo")
	hint := "Runs with sh -c in each repo root; you can untick repos next."
	if len(rows) == 1 {
		where = m.env.Rel(rows[0].Status.Path)
		hint = "Runs with sh -c in the repo root, on the terminal."
	}
	paths := make([]string, len(rows))
	for i, e := range rows {
		paths[i] = e.Status.Path
	}
	var cmd tea.Cmd
	m.modal, cmd = m.modal.Prompt(stepCommand, prompt.New("Run in "+where,
		prompt.WithHint(hint+" Erase `git` to run any command."),
		prompt.WithValue(value),
		prompt.WithHistory(m.history.Recent()),
		prompt.WithCompleter(lineCompleter(m.history, paths))))
	return m, cmd
}

// start runs line in rows: one repo gets the terminal, more go through the
// checklist. The line joins the history either way.
func (m Model) start(line string, rows []Entry) (Model, tea.Cmd) {
	line = strings.TrimSpace(line)
	if line == "" || len(rows) == 0 || m.activity != activityIdle || m.modal.Open() {
		return m, nil
	}
	m.history = m.history.Add(line, m.env.Now())
	save := saveHistory(m.env.HistoryFile, m.history)
	m.runLine, m.runRows = line, rows
	if len(rows) == 1 {
		path := rows[0].Status.Path
		c := exec.Command("sh", "-c", line)
		c.Dir = path
		return m, tea.Batch(save, handover.Run(handover.Pause(c), handoverRun{line: line, path: path}))
	}
	return m.openRun(line, rows), save
}

// saveHistory writes h to path off the update loop. A failed write loses
// only suggestions, so it is not reported.
func saveHistory(path string, h repo.History) tea.Cmd {
	if path == "" {
		return nil
	}
	return func() tea.Msg {
		_ = h.Save(path)
		return nil
	}
}

func (m Model) openRun(line string, rows []Entry) Model {
	ws := m.env.Paths.Workspace
	out := &runOutputs{res: map[string]repo.OperationResult{}}
	argv := []string{"sh", "-c", line}
	p := plan.Plan{Command: "repo.run"}
	if strings.HasPrefix(line, "git ") {
		p.Command = "repo.git"
	}
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
	m.modal = m.modal.Confirm(stepRun, confirm.New("Run `"+line+"`", p))
	return m
}

func (m Model) renderOutput(results map[string]repo.OperationResult, res plan.PlanResult) string {
	var b strings.Builder
	b.WriteString(theme.Title.Render("Output of `"+m.runLine+"`") + "\n")
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

// gitCommands are the git subcommands the prompt completes after `git `.
var gitCommands = []string{
	"add", "am", "apply", "bisect", "blame", "branch", "checkout", "cherry-pick", "clean", "commit",
	"config", "describe", "diff", "fetch", "grep", "log", "merge", "mv", "pull", "push", "rebase",
	"reflog", "remote", "reset", "restore", "revert", "rm", "show", "stash", "status", "switch",
	"tag", "worktree",
}

// maxSuggestions caps what the prompt is handed; it shows the first few.
const maxSuggestions = 50

// lineCompleter suggests whole lines for the command prompt: first the lines
// run before that start with the value, the most run first, then the value
// with its last word completed. After `git ` the word is a subcommand; after
// that it is a branch, tag or remote of the repos in paths (read once, when
// first needed). It runs off the update loop.
func lineCompleter(h repo.History, paths []string) prompt.Completer {
	var once sync.Once
	var names []string
	return func(value string) []string {
		out := h.Ranked(value)
		head, last := value, ""
		if i := strings.LastIndex(value, " "); i >= 0 {
			head, last = value[:i+1], value[i+1:]
		} else {
			head, last = "", value
		}
		words := strings.Fields(head)
		var cands []string
		switch {
		case len(words) == 0 || words[0] != "git" || strings.HasPrefix(last, "-"):
		case len(words) == 1:
			cands = gitCommands
		default:
			once.Do(func() { names = repo.CompletionWords(paths) })
			cands = names
		}
		for _, c := range cands {
			if strings.HasPrefix(c, last) && c != last {
				out = append(out, head+c)
			}
		}
		out = slices.DeleteFunc(dedupe(out), func(s string) bool { return s == value })
		if len(out) > maxSuggestions {
			out = out[:maxSuggestions]
		}
		return out
	}
}

// dedupe keeps the first of each line, in order.
func dedupe(lines []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, l := range lines {
		if !seen[l] {
			seen[l] = true
			out = append(out, l)
		}
	}
	return out
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
		s = strings.Join(parts, theme.Muted.Render(" · "))
	}
	return lipgloss.NewStyle().MaxWidth(m.width).Render(s)
}

// detailContent is `repo show` for e.
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
	case m.level == levelDetail:
		return m.detailKeys
	case m.level != levelList:
		return m.detailKeys.DetailMap // output and roots only scroll
	}
	return m.list.Help(m.keys)
}
