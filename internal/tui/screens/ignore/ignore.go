// Package ignore is the sync-hygiene screen: `ignore scan` violations with
// the `ignore fix` actions (exclude rule, suppress warning, move to scratch,
// delete) applied per violation through the plan checklist, plus `ignore check`
// for any path. The screen has two levels: live violations and suppressed
// warnings. The tree view, excluded-file listing, and rule editor are handed
// to the CLI.
package ignore

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	wsignore "github.com/mugenkunou/ws-tool/internal/ignore"
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

// LoadedMsg carries scan violations. Other components (the dashboard) may
// observe it.
type LoadedMsg struct {
	Violations []wsignore.Violation
	Err        error
}

// Actionable counts violations outside safe harbors.
func (m LoadedMsg) Actionable() int {
	n := 0
	for _, v := range m.Violations {
		if !v.InSafeHarbor {
			n++
		}
	}
	return n
}

type checkMsg struct {
	text  string
	level nav.Level
}

type step int

const (
	stepFix step = iota
	stepExcludeAll
	stepCheckPath
)

type screenLevel int

const (
	levelLive       screenLevel = iota // live violations
	levelSuppressed                    // suppressed warnings
)

type handoverTag int

const handoverRules handoverTag = iota

// Model is the ignore screen state.
type Model struct {
	env      env.Env
	keys     keys.IgnoreMap
	liveList reslist.Model[wsignore.Violation] // actionable (not in safe harbor)
	suppList reslist.Model[wsignore.Violation] // suppressed (in safe harbor)
	modal    modal.Model
	all      []wsignore.Violation
	level    screenLevel
	scanning bool
	width    int
	height   int
}

// New creates the screen.
func New(e env.Env) Model {
	schema := wsignore.Schema(e.View(), e.Paths.Workspace)
	cols := []reslist.Col[wsignore.Violation]{
		{Key: "severity", Min: 10, Style: severityStyle},
		{Key: "type", Min: 12},
		{Key: "size_bytes", Min: 9},
		{Key: "path", Min: 20, Weight: 1, Style: func(v wsignore.Violation, t string) string {
			if v.InSafeHarbor {
				return theme.Muted.Render(t)
			}
			return t
		}},
	}
	return Model{
		env:      e,
		keys:     keys.Ignore(),
		scanning: true,
		liveList: reslist.New(schema, cols, theme.Icon(theme.IconBroom)+"Squeaky clean — no sync hygiene violations.").Declare(reslist.Decls(liveActions)),
		suppList: reslist.New(schema, cols, theme.Icon(theme.IconBroom)+"No suppressed warnings.").Declare(reslist.Decls(suppActions)),
	}
}

func severityStyle(v wsignore.Violation, t string) string {
	switch {
	case v.InSafeHarbor:
		return theme.Muted.Render("suppressed")
	case v.Severity == "CRITICAL":
		return theme.Error.Render(t)
	case v.Severity == "WARNING":
		return theme.Warn.Render(t)
	}
	return t
}

func engine(e env.Env) (*wsignore.Engine, error) {
	rules, err := wsconfig.LoadIgnoreRules(e.Paths.Config)
	if err != nil {
		return nil, err
	}
	return wsignore.BuildEngine(rules), nil
}

// Load runs `ignore scan`.
func Load(e env.Env) tea.Cmd {
	return func() tea.Msg {
		eng, err := engine(e)
		if err != nil {
			return LoadedMsg{Err: err}
		}
		vs, err := wsignore.Scan(wsignore.ScanOptions{
			WorkspacePath: e.Paths.Workspace,
			WarnSizeMB:    e.Config.Ignore.WarnSizeMB,
			CritSizeMB:    e.Config.Ignore.CritSizeMB,
			MaxDepth:      e.Config.Ignore.MaxDepth,
			Engine:        eng,
		})
		return LoadedMsg{Violations: vs, Err: err}
	}
}

func check(e env.Env, input string) tea.Cmd {
	return func() tea.Msg {
		abs, err := wsconfig.ExpandUserPath(input)
		if err != nil {
			return checkMsg{level: nav.LevelError, text: "Invalid path: " + err.Error()}
		}
		st, err := os.Stat(abs)
		if err != nil {
			return checkMsg{level: nav.LevelError, text: "Path does not exist: " + abs}
		}
		eng, err := engine(e)
		if err != nil {
			return checkMsg{level: nav.LevelError, text: err.Error()}
		}
		res, _, err := wsignore.Check(eng, e.Paths.Workspace, abs, st.IsDir())
		if err != nil {
			return checkMsg{level: nav.LevelError, text: err.Error()}
		}
		switch {
		case !res.Included:
			return checkMsg{level: nav.LevelWarn, text: fmt.Sprintf("✘ IGNORED  %s — excluded by rule `%s`", abs, res.Rule)}
		case res.SafeHarbor:
			rule := res.OverriddenBy
			if rule == "" {
				rule = res.Rule
			}
			return checkMsg{level: nav.LevelSuccess, text: fmt.Sprintf("✔ SYNCED  %s — suppressed by `%s` (warning hidden, still syncing)", abs, rule)}
		case res.Rule == "<default>":
			return checkMsg{level: nav.LevelSuccess, text: fmt.Sprintf("✔ SYNCED  %s — no matching exclude rule", abs)}
		default:
			return checkMsg{level: nav.LevelSuccess, text: fmt.Sprintf("✔ SYNCED  %s — included by rule `%s`", abs, res.Rule)}
		}
	}
}

// Init starts the first scan.
func (m Model) Init() tea.Cmd { return Load(m.env) }

// Declared lists the actions of both levels and their scopes.
func (m Model) Declared() []reslist.Decl {
	return append(m.liveList.Declared(), m.suppList.Declared()...)
}

// CapturesKeys implements nav.Component.
func (m Model) CapturesKeys() bool { return m.modal.Open() || m.activeList().Editing() }

func (m Model) rescan() (Model, tea.Cmd) {
	m.scanning = true
	m.liveList = m.liveList.SetLoading()
	m.suppList = m.suppList.SetLoading()
	return m, Load(m.env)
}

// split puts actionable violations in the live list and safe-harbor ones in
// the suppressed list.
func (m Model) split() Model {
	var live, supp []wsignore.Violation
	for _, v := range m.all {
		if v.InSafeHarbor {
			supp = append(supp, v)
		} else {
			live = append(live, v)
		}
	}
	m.liveList = m.liveList.SetItems(live)
	m.suppList = m.suppList.SetItems(supp)
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
		listH := max(msg.Height-1, 0)
		m.liveList = m.liveList.SetSize(msg.Width, listH)
		m.suppList = m.suppList.SetSize(msg.Width, listH)
		m.modal = m.modal.SetSize(msg.Width, msg.Height)
		return m, nil
	case nav.RefreshMsg:
		return m.rescan()
	case LoadedMsg:
		m.scanning = false
		if msg.Err != nil {
			m.liveList = m.liveList.SetError(msg.Err)
			return m, nil
		}
		m.all = msg.Violations
		return m.split(), nil
	case checkMsg:
		return m, nav.Notify(msg.level, msg.text)
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

	if l, cmd, ok := m.activeList().HandleKey(k); ok {
		return m.setActiveList(l), cmd
	}

	acts := liveActions
	if m.level == levelSuppressed {
		acts = suppActions
	}
	if next, cmd, ok := reslist.Run(m, m.activeList(), acts, k); ok {
		return next, cmd
	}
	switch {
	case key.Matches(k, m.keys.Back):
		if m.level == levelSuppressed {
			m.level = levelLive
			return m, nil
		}
		return m, nav.Up
	}

	// Delegate movement to the active list.
	var cmd tea.Cmd
	if m.level == levelSuppressed {
		m.suppList, cmd = m.suppList.Update(k)
	} else {
		m.liveList, cmd = m.liveList.Update(k)
	}
	return m, cmd
}

// fix opens one checklist with each row's action from build; a row build
// skips (ok false) is left out, and none at all notifies empty instead.
// Actions sharing an ID (two rows in the same directory) collapse to one.
func fix(title, empty string, build func(Model, wsignore.Violation) (plan.Action, bool)) func(Model, []wsignore.Violation) (Model, tea.Cmd) {
	return func(m Model, rows []wsignore.Violation) (Model, tea.Cmd) {
		var actions []plan.Action
		seen := make(map[string]bool)
		for _, v := range rows {
			a, ok := build(m, v)
			if !ok || seen[a.ID] {
				continue
			}
			seen[a.ID] = true
			actions = append(actions, a)
		}
		if len(actions) == 0 {
			return m, nav.Notify(nav.LevelInfo, empty)
		}
		return m.openFixMulti(title, actions), nil
	}
}

func always(f func(Model, wsignore.Violation) plan.Action) func(Model, wsignore.Violation) (plan.Action, bool) {
	return func(m Model, v wsignore.Violation) (plan.Action, bool) { return f(m, v), true }
}

// commonActions are the keys both levels share; none acts on rows.
func commonActions() []reslist.Action[Model, wsignore.Violation] {
	k := keys.Ignore()
	run := func(f func(Model) tea.Cmd) func(Model, []wsignore.Violation) (Model, tea.Cmd) {
		return func(m Model, _ []wsignore.Violation) (Model, tea.Cmd) { return m, f(m) }
	}
	return []reslist.Action[Model, wsignore.Violation]{
		{Key: k.Check, Scope: reslist.Global, Do: func(m Model, _ []wsignore.Violation) (Model, tea.Cmd) {
			var cmd tea.Cmd
			m.modal, cmd = m.modal.Prompt(stepCheckPath, prompt.New("Will this path sync?",
				prompt.WithValue(m.env.Paths.Workspace+"/"), prompt.WithCompleter(complete.Paths)))
			return m, cmd
		}},
		{Key: k.Tree, Scope: reslist.Global, Do: run(func(m Model) tea.Cmd {
			return handover.Run(handover.Paged(handover.WS(m.env.Paths, "ignore", "tree")), handoverRules)
		})},
		{Key: k.List, Scope: reslist.Global, Do: run(func(m Model) tea.Cmd {
			return handover.Run(handover.Paged(handover.WS(m.env.Paths, "ignore", "ls")), handoverRules)
		})},
		{Key: k.EditRules, Scope: reslist.Global, Do: run(func(m Model) tea.Cmd {
			return handover.Run(handover.WS(m.env.Paths, "ignore", "edit"), handoverRules)
		})},
	}
}

// liveActions is every key on the violations level and the rows it acts on.
var liveActions = func() []reslist.Action[Model, wsignore.Violation] {
	k := keys.Ignore()
	return append([]reslist.Action[Model, wsignore.Violation]{
		{Key: k.Exclude, Scope: reslist.Selected, Do: fix("Add to .megaignore", "", always(Model.excludeAction))},
		{Key: k.Suppress, Scope: reslist.Selected, Do: fix("Suppress warning", "Top-level path has no parent to suppress", Model.harborAction)},
		{Key: k.Move, Scope: reslist.Selected, Do: fix("Move to scratch", "Only bloat can be moved to scratch",
			func(m Model, v wsignore.Violation) (plan.Action, bool) { return m.moveAction(v), v.Type == "bloat" })},
		{Key: k.Delete, Scope: reslist.Selected, Do: fix("Delete", "", always(Model.deleteAction))},
		{Key: k.ExcludeAll, Scope: reslist.All, Do: func(m Model, rows []wsignore.Violation) (Model, tea.Cmd) {
			return m.openExcludeAll(rows), nil
		}},
		{Key: k.ViewSuppressed, Scope: reslist.Global, Do: func(m Model, _ []wsignore.Violation) (Model, tea.Cmd) {
			m.level = levelSuppressed
			return m, nil
		}},
	}, commonActions()...)
}()

// suppActions is every key on the suppressed-warnings level.
var suppActions = func() []reslist.Action[Model, wsignore.Violation] {
	k := keys.Ignore()
	return append([]reslist.Action[Model, wsignore.Violation]{
		{Key: k.Unsuppress, Scope: reslist.Selected, Do: fix("Remove suppress rules", "Cannot determine suppress rule for selected items", Model.unsuppressAction)},
	}, commonActions()...)
}()

// activeList returns the list model for the current level.
func (m Model) activeList() reslist.Model[wsignore.Violation] {
	if m.level == levelSuppressed {
		return m.suppList
	}
	return m.liveList
}

func (m Model) setActiveList(l reslist.Model[wsignore.Violation]) Model {
	if m.level == levelSuppressed {
		m.suppList = l
	} else {
		m.liveList = l
	}
	return m
}

func (m Model) dialogResult(st step, msg tea.Msg) (nav.Component, tea.Cmd) {
	if st == stepCheckPath {
		r := msg.(prompt.ResultMsg)
		if r.Cancelled {
			return m, nil
		}
		return m, check(m.env, r.Value)
	}
	done := msg.(confirm.DoneMsg)
	if done.Result.Aborted {
		return m, nil
	}
	m.liveList = m.liveList.ClearMarks() // the marks were for this action
	m.suppList = m.suppList.ClearMarks() // the marks were for this action
	level := nav.LevelSuccess
	if done.Result.HasFailures() {
		level = nav.LevelWarn
	}
	m, cmd := m.rescan()
	return m, tea.Batch(nav.Notify(level, confirm.Summary(done.Result)), cmd)
}

func (m Model) abs(v wsignore.Violation) string { return style.AbsPath(m.env.Paths.Workspace, v.Path) }

// regenerate rewrites .megaignore from the manifest's rules, as `ignore fix`
// does after adding rules.
func regenerate(e env.Env) error {
	rules, err := wsconfig.LoadIgnoreRules(e.Paths.Config)
	if err != nil {
		return err
	}
	return wsignore.WriteMegaignore(filepath.Join(e.Paths.Workspace, ".megaignore"), rules)
}

func (m Model) excludeAction(v wsignore.Violation) plan.Action {
	e, rel := m.env, v.Path
	return plan.Action{
		ID:          "exclude-" + rel,
		Description: "Add to .megaignore: " + m.abs(v),
		Execute: func() error {
			if _, err := wsconfig.AddIgnoreExclude(e.Paths.Config, rel, ""); err != nil {
				return err
			}
			return regenerate(e)
		},
	}
}

func (m Model) harborAction(v wsignore.Violation) (plan.Action, bool) {
	dir := filepath.ToSlash(filepath.Dir(v.Path))
	if dir == "." || dir == "" {
		return plan.Action{}, false
	}
	e, pattern := m.env, dir+"/**"
	return plan.Action{
		ID:          "harbor-" + dir,
		Description: "Suppress warnings for " + style.AbsPath(e.Paths.Workspace, dir) + "/**",
		Execute: func() error {
			added, err := wsconfig.AddIgnoreSafeHarbor(e.Paths.Config, pattern, "")
			if err != nil {
				return err
			}
			if !added {
				// Already suppressed — report it instead of passing silently.
				return errors.New("already suppressed")
			}
			return regenerate(e)
		},
	}, true
}

func (m Model) unsuppressAction(v wsignore.Violation) (plan.Action, bool) {
	dir := filepath.ToSlash(filepath.Dir(v.Path))
	if dir == "." || dir == "" {
		return plan.Action{}, false
	}
	e, pattern := m.env, dir+"/**"
	return plan.Action{
		ID:          "unsuppress-" + dir,
		Description: "Remove suppress rule for " + style.AbsPath(e.Paths.Workspace, dir) + "/**",
		Execute: func() error {
			if _, err := wsconfig.RemoveIgnoreSafeHarbor(e.Paths.Config, pattern); err != nil {
				return err
			}
			return regenerate(e)
		},
	}, true
}

func (m Model) moveAction(v wsignore.Violation) plan.Action {
	src := m.abs(v)
	dest := filepath.Join(m.env.ScratchDir, filepath.Base(v.Path))
	return plan.Action{
		ID:          "move-" + v.Path,
		Description: fmt.Sprintf("Move %s → %s", src, dest),
		Execute: func() error {
			if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				return err
			}
			return os.Rename(src, dest)
		},
	}
}

func (m Model) deleteAction(v wsignore.Violation) plan.Action {
	p := m.abs(v)
	size := ""
	if v.SizeBytes > 0 {
		size = " (" + style.HumanBytes(v.SizeBytes) + ")"
	}
	return plan.Action{
		ID:          "delete-" + v.Path,
		Description: "Delete " + p + size,
		Execute:     func() error { return os.RemoveAll(p) },
	}
}

func (m Model) openFixMulti(title string, actions []plan.Action) Model {
	p := plan.Plan{Command: "ignore.fix", Actions: actions}
	m.modal = m.modal.Confirm(stepFix, confirm.New(title, p))
	return m
}

func (m Model) openExcludeAll(rows []wsignore.Violation) Model {
	p := plan.Plan{Command: "ignore.fix"}
	for _, v := range rows {
		p.Actions = append(p.Actions, m.excludeAction(v))
	}
	title := "Add " + format.Plural(len(p.Actions), "violation") + " to .megaignore"
	m.modal = m.modal.Confirm(stepExcludeAll, confirm.New(title, p))
	return m
}

func (m Model) statusLine() string {
	if m.scanning {
		return theme.Muted.Render("Scanning workspace…")
	}
	live, supp := len(m.liveList.Items()), len(m.suppList.Items())
	if m.level == levelSuppressed {
		parts := []string{
			theme.Title.Render("Suppressed warnings"),
			theme.Muted.Render(fmt.Sprintf("%d item(s)", supp)),
			theme.Muted.Render("u unsuppress · esc back"),
		}
		return lipgloss.NewStyle().MaxWidth(m.width).Render(strings.Join(parts, theme.Muted.Render(" · ")))
	}
	parts := []string{format.Plural(live, "violation")}
	if supp > 0 {
		parts = append(parts, theme.Muted.Render(fmt.Sprintf("%d suppressed (H to view)", supp)))
	}
	return lipgloss.NewStyle().MaxWidth(m.width).Render(strings.Join(parts, theme.Muted.Render(" · ")))
}

// View implements nav.Component.
func (m Model) View() string {
	if m.modal.Open() {
		return m.modal.View()
	}
	return lipgloss.JoinVertical(lipgloss.Left, m.statusLine(), m.activeList().View())
}

// KeyMap implements nav.Component.
func (m Model) KeyMap() help.KeyMap {
	if m.modal.Open() {
		return m.modal.KeyMap()
	}
	return m.activeList().Help(m.keys)
}
