// Package ignore is the sync-hygiene screen (spec §6.5). Three views, cycled by
// the next-view key: Warnings (`ignore lint`, where it opens), Muted
// (`ignore ls --muted`) and Ignored (`ignore ls`). Each key builds the plan the
// CLI's command of the same verb builds and shows it as a checklist; the CLI's
// own words (`rm`, `check`, `tree`, `lint`) are `:` commands. The tree view and
// the rule editor are handed the terminal.
package ignore

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	wsignore "github.com/mugenkunou/ws-tool/internal/ignore"
	"github.com/mugenkunou/ws-tool/internal/plan"
	"github.com/mugenkunou/ws-tool/internal/repo"
	wsscratch "github.com/mugenkunou/ws-tool/internal/scratch"
	"github.com/mugenkunou/ws-tool/internal/style"
	"github.com/mugenkunou/ws-tool/internal/trash"
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

// LoadedMsg carries what the three views show: the scan's violations (the
// muted ones are the Muted view) and the paths that do not sync. Other
// components (the dashboard) may observe it.
type LoadedMsg struct {
	Violations []wsignore.Violation
	Ignored    []wsignore.IgnoredPath
	Err        error
}

// Actionable counts violations outside safe harbors.
func (m LoadedMsg) Actionable() int {
	n := 0
	for _, v := range m.Violations {
		if !v.Muted {
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
	stepPlan step = iota
	stepCheckPath
)

type handoverTag int

const handoverRules handoverTag = iota

// Model is the ignore screen state.
type Model struct {
	env      env.Env
	keys     keys.IgnoreMap
	warn     reslist.Model[wsignore.Violation]   // Warnings
	muted    reslist.Model[wsignore.Violation]   // Muted
	ignored  reslist.Model[wsignore.IgnoredPath] // Ignored
	modal    modal.Model
	all      []wsignore.Violation
	allPaths []wsignore.IgnoredPath
	view     keys.IgnoreView
	under    []string // absolute paths `:lint <path>` narrowed the views to
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
			if v.Muted {
				return theme.Muted.Render(t)
			}
			return t
		}},
	}
	ignoredCols := []reslist.Col[wsignore.IgnoredPath]{
		{Key: "size_bytes", Min: 9},
		{Key: "path", Min: 20, Weight: 1},
		{Key: "rule", Min: 14, Style: func(_ wsignore.IgnoredPath, t string) string { return theme.Muted.Render(t) }},
	}
	return Model{
		env:      e,
		keys:     keys.Ignore(),
		scanning: true,
		warn:     reslist.New(schema, cols, theme.Icon(theme.IconBroom)+"Squeaky clean — no sync hygiene warnings.").Declare(reslist.Decls(warnActions)),
		muted:    reslist.New(schema, cols, theme.Icon(theme.IconBroom)+"No muted warnings.").Declare(reslist.Decls(mutedActions)),
		ignored:  reslist.New(wsignore.IgnoredSchema(e.View(), e.Paths.Workspace), ignoredCols, "Nothing is ignored.").Declare(reslist.Decls(ignoredActions)),
	}
}

func severityStyle(v wsignore.Violation, t string) string {
	switch {
	case v.Muted:
		return theme.Muted.Render("muted")
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

// config is ws.json as it is now on disk (a change made on this screen is not
// in the config the TUI started with).
func (m Model) config() wsconfig.WsConfig {
	if cfg, err := wsconfig.Load(m.env.Paths.Config); err == nil {
		return cfg
	}
	return m.env.Config
}

func mutedList(e env.Env) []string {
	if cfg, err := wsconfig.Load(e.Paths.Config); err == nil {
		return cfg.Ignore.Muted
	}
	return e.Config.Ignore.Muted
}

// Load runs `ignore lint` (whose muted warnings are `ignore ls --muted`) and
// `ignore ls`.
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
			Muted:         mutedList(e),
		})
		return LoadedMsg{Violations: vs, Ignored: wsignore.ListIgnored(eng, e.Paths.Workspace, nil), Err: err}
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
			return checkMsg{level: nav.LevelSuccess, text: fmt.Sprintf("✔ SYNCED  %s — always-sync directory `%s`", abs, res.Rule)}
		case res.Rule == "<default>":
			return checkMsg{level: nav.LevelSuccess, text: fmt.Sprintf("✔ SYNCED  %s — no matching exclude rule", abs)}
		default:
			return checkMsg{level: nav.LevelSuccess, text: fmt.Sprintf("✔ SYNCED  %s — included by rule `%s`", abs, res.Rule)}
		}
	}
}

// Init starts the first scan.
func (m Model) Init() tea.Cmd { return Load(m.env) }

// Declared lists the actions of every view and their scopes.
func (m Model) Declared() []reslist.Decl {
	return append(append(m.warn.Declared(), m.muted.Declared()...), m.ignored.Declared()...)
}

// CapturesKeys implements nav.Component.
func (m Model) CapturesKeys() bool { return m.modal.Open() || m.activeEditing() }

func (m Model) activeEditing() bool {
	switch m.view {
	case keys.IgnoreMuted:
		return m.muted.Editing()
	case keys.IgnoreIgnored:
		return m.ignored.Editing()
	}
	return m.warn.Editing()
}

func (m Model) rescan() (Model, tea.Cmd) {
	m.scanning = true
	m.warn = m.warn.SetLoading()
	m.muted = m.muted.SetLoading()
	m.ignored = m.ignored.SetLoading()
	return m, Load(m.env)
}

// within reports whether abs is at or under one of m.under, or m.under is empty.
func (m Model) within(abs string) bool {
	if len(m.under) == 0 {
		return true
	}
	return slices.ContainsFunc(m.under, func(d string) bool { return repo.IsWithin(abs, d) })
}

// split puts the live warnings in Warnings, the muted ones in Muted, and the
// paths that do not sync in Ignored; `:lint <path>` keeps what is under it.
func (m Model) split() Model {
	var live, muted []wsignore.Violation
	for _, v := range m.all {
		if !m.within(m.abs(v.Path)) {
			continue
		}
		if v.Muted {
			muted = append(muted, v)
		} else {
			live = append(live, v)
		}
	}
	var ignored []wsignore.IgnoredPath
	for _, p := range m.allPaths {
		if m.within(m.abs(p.Path)) {
			ignored = append(ignored, p)
		}
	}
	m.warn = m.warn.SetItems(live)
	m.muted = m.muted.SetItems(muted)
	m.ignored = m.ignored.SetItems(ignored)
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
		m.warn = m.warn.SetSize(msg.Width, listH)
		m.muted = m.muted.SetSize(msg.Width, listH)
		m.ignored = m.ignored.SetSize(msg.Width, listH)
		m.modal = m.modal.SetSize(msg.Width, msg.Height)
		return m, nil
	case nav.RefreshMsg:
		return m.rescan()
	case LoadedMsg:
		m.scanning = false
		if msg.Err != nil {
			m.warn = m.warn.SetError(msg.Err)
			return m, nil
		}
		m.all, m.allPaths = msg.Violations, msg.Ignored
		return m.split(), nil
	case checkMsg:
		return m, nav.Notify(msg.level, msg.text)
	case handover.DoneMsg:
		if _, ok := msg.Tag.(handoverTag); ok {
			return m.rescan()
		}
		return m, nil
	case nav.CommandMsg:
		if m.modal.Open() {
			return m, nil
		}
		next, cmd, _ := reslist.RunCommand(m, m.warn, commands, msg)
		return next, cmd
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
	return m.handleKey(k)
}

// handleKey runs a key on the active view: the list's own keys, then the
// view's actions, then esc, then movement.
func (m Model) handleKey(k tea.KeyPressMsg) (nav.Component, tea.Cmd) {
	switch m.view {
	case keys.IgnoreMuted:
		if l, cmd, ok := m.muted.HandleKey(k); ok {
			m.muted = l
			return m, cmd
		}
		if next, cmd, ok := reslist.Run(m, m.muted, mutedActions, k); ok {
			return next, cmd
		}
	case keys.IgnoreIgnored:
		if l, cmd, ok := m.ignored.HandleKey(k); ok {
			m.ignored = l
			return m, cmd
		}
		if next, cmd, ok := reslist.Run(m, m.ignored, ignoredActions, k); ok {
			return next, cmd
		}
	default:
		if l, cmd, ok := m.warn.HandleKey(k); ok {
			m.warn = l
			return m, cmd
		}
		if next, cmd, ok := reslist.Run(m, m.warn, warnActions, k); ok {
			return next, cmd
		}
	}
	if key.Matches(k, m.keys.Back) {
		if len(m.under) > 0 { // leave the `:lint <path>` narrowing
			m.under = nil
			return m.split(), nil
		}
		return m, nav.Up
	}
	var cmd tea.Cmd
	switch m.view {
	case keys.IgnoreMuted:
		m.muted, cmd = m.muted.Update(k)
	case keys.IgnoreIgnored:
		m.ignored, cmd = m.ignored.Update(k)
	default:
		m.warn, cmd = m.warn.Update(k)
	}
	return m, cmd
}

// ── actions ───────────────────────────────────────────────────────

// common are the keys every view has; neither acts on rows.
func common[T any]() []reslist.Action[Model, T] {
	k := keys.Ignore()
	return []reslist.Action[Model, T]{
		{Key: k.EditRules, Scope: reslist.Global, Do: func(m Model, _ []T) (Model, tea.Cmd) {
			return m, handover.Run(handover.WS(m.env.Paths, "ignore", "edit"), handoverRules)
		}},
		{Key: k.NextView, Scope: reslist.Global, Do: func(m Model, _ []T) (Model, tea.Cmd) {
			m.view = (m.view + 1) % 3
			return m, nil
		}},
	}
}

// pathsOf is the rows' absolute paths: what the CLI commands take.
func (m Model) pathsOf(rows []wsignore.Violation) []string {
	out := make([]string, len(rows))
	for i, v := range rows {
		out[i] = m.abs(v.Path)
	}
	return out
}

// warnActions is every key of the Warnings view and the rows it acts on.
var warnActions = func() []reslist.Action[Model, wsignore.Violation] {
	k := keys.Ignore()
	return append([]reslist.Action[Model, wsignore.Violation]{
		{Key: k.Ignore, Scope: reslist.Selected, Do: func(m Model, rows []wsignore.Violation) (Model, tea.Cmd) {
			return m.ignorePaths(m.pathsOf(rows))
		}},
		{Key: k.Mute, Scope: reslist.Selected, Do: func(m Model, rows []wsignore.Violation) (Model, tea.Cmd) {
			return m.mutePaths(m.pathsOf(rows))
		}},
		{Key: k.Scratch, Scope: reslist.Selected, Do: func(m Model, rows []wsignore.Violation) (Model, tea.Cmd) {
			return m.scratchPaths(m.pathsOf(rows))
		}},
		{Key: k.Delete, Scope: reslist.Selected, Do: Model.deleteRows},
	}, common[wsignore.Violation]()...)
}()

// mutedActions is every key of the Muted view.
var mutedActions = func() []reslist.Action[Model, wsignore.Violation] {
	k := keys.Ignore()
	return append([]reslist.Action[Model, wsignore.Violation]{
		{Key: k.Ignore, Scope: reslist.Selected, Do: func(m Model, rows []wsignore.Violation) (Model, tea.Cmd) {
			return m.ignorePaths(m.pathsOf(rows))
		}},
		{Key: k.Unmute, Scope: reslist.Selected, Do: Model.unmuteRows},
	}, common[wsignore.Violation]()...)
}()

// ignoredActions is every key of the Ignored view.
var ignoredActions = func() []reslist.Action[Model, wsignore.IgnoredPath] {
	k := keys.Ignore()
	return append([]reslist.Action[Model, wsignore.IgnoredPath]{
		{Key: k.StopIgnoring, Scope: reslist.Selected, Do: func(m Model, rows []wsignore.IgnoredPath) (Model, tea.Cmd) {
			paths := make([]string, len(rows))
			for i, p := range rows {
				paths[i] = m.abs(p.Path)
			}
			return m.rmPaths(paths)
		}},
	}, common[wsignore.IgnoredPath]()...)
}()

// commands are the `:` commands: the CLI's words after `ignore`. rm shares
// rmPaths with the Ignored view's stop-ignoring key.
var commands = []reslist.Command[Model, wsignore.Violation]{
	{Name: "rm", Args: "<path…>", Help: "stop ignoring paths: sync them again", Scope: reslist.Global,
		Do: func(m Model, _ []wsignore.Violation, args []string) (Model, tea.Cmd) {
			if len(args) == 0 {
				return m, nav.Notify(nav.LevelWarn, "usage: rm <path…>")
			}
			return m.rmPaths(args)
		}},
	{Name: "check", Args: "<path>", Help: "will this path sync?", Scope: reslist.Global,
		Do: func(m Model, _ []wsignore.Violation, args []string) (Model, tea.Cmd) {
			if len(args) == 0 {
				var cmd tea.Cmd
				m.modal, cmd = m.modal.Prompt(stepCheckPath, prompt.New("Will this path sync?",
					prompt.WithValue(m.env.Paths.Workspace+"/"), prompt.WithCompleter(complete.Paths)))
				return m, cmd
			}
			return m, check(m.env, args[0])
		}},
	{Name: "tree", Args: "[dir]", Help: "what syncs, as a tree (paged)", Scope: reslist.Global,
		Do: func(m Model, _ []wsignore.Violation, args []string) (Model, tea.Cmd) {
			argv := append([]string{"ignore", "tree"}, args...)
			return m, handover.Run(handover.Paged(handover.WS(m.env.Paths, argv...)), handoverRules)
		}},
	{Name: "lint", Args: "<path>", Help: "warnings under a path", Scope: reslist.Global,
		Do: func(m Model, _ []wsignore.Violation, args []string) (Model, tea.Cmd) { return m.lint(args) }},
}

// Commands implements nav.Commander.
func (m Model) Commands() []nav.CommandInfo { return reslist.CommandInfos(commands) }

// lint is `ignore lint <path…>`: the Warnings view, narrowed to what is under
// the paths (none: everything). esc leaves the narrowing.
func (m Model) lint(args []string) (Model, tea.Cmd) {
	var under []string
	for _, raw := range args {
		abs, err := wsconfig.ExpandUserPath(raw)
		if err != nil {
			return m, nav.Notify(nav.LevelError, fmt.Sprintf("invalid path %q: %s", raw, err))
		}
		if !repo.IsWithin(abs, m.env.Paths.Workspace) {
			return m, nav.Notify(nav.LevelError, fmt.Sprintf("path is outside the workspace %s: %s", m.env.Paths.Workspace, abs))
		}
		if _, err := os.Stat(abs); err != nil {
			return m, nav.Notify(nav.LevelError, "path does not exist: "+abs)
		}
		under = append(under, abs)
	}
	m.under, m.view = under, keys.IgnoreWarnings
	return m.split(), nil
}

// ── plans: the CLI's, as checklists ───────────────────────────────

func (m Model) abs(rel string) string { return style.AbsPath(m.env.Paths.Workspace, rel) }

// target resolves one path argument (spec §4.5): inside the workspace, never
// the workspace itself. When mustExist is false a missing path is accepted.
func (m Model) target(raw string, mustExist bool) (abs, rel string, isDir bool, err error) {
	abs, err = wsconfig.ExpandUserPath(raw)
	if err != nil {
		return "", "", false, fmt.Errorf("invalid path %q: %w", raw, err)
	}
	ws := m.env.Paths.Workspace
	if !repo.IsWithin(abs, ws) {
		return "", "", false, fmt.Errorf("path is outside the workspace %s: %s", ws, abs)
	}
	r, err := filepath.Rel(ws, abs)
	if err != nil {
		return "", "", false, err
	}
	rel = wsignore.NormalizeRel(r)
	if rel == "" {
		return "", "", false, fmt.Errorf("%s is the workspace itself", abs)
	}
	st, statErr := os.Stat(abs)
	if statErr != nil {
		if mustExist {
			return "", "", false, fmt.Errorf("path does not exist: %s", abs)
		}
		return abs, rel, false, nil
	}
	return abs, rel, st.IsDir(), nil
}

// regenerate rewrites .megaignore from the rules now in ws.json.
func regenerate(e env.Env) error {
	rules, err := wsconfig.LoadIgnoreRules(e.Paths.Config)
	if err != nil {
		return err
	}
	return wsignore.WriteMegaignore(filepath.Join(e.Paths.Workspace, ".megaignore"), rules)
}

// open shows actions as a checklist, or says empty when there are none. A
// refusal that left some paths out is the checklist's note.
func (m Model) open(title, empty, note string, actions []plan.Action) (Model, tea.Cmd) {
	if len(actions) == 0 {
		return m, nav.Notify(nav.LevelInfo, empty)
	}
	c := confirm.New(title, plan.Plan{Command: "ignore", Actions: actions})
	if note != "" {
		c = c.WithNote(note)
	}
	m.modal = m.modal.Confirm(stepPlan, c)
	return m, nil
}

// ignorePaths is `ignore add`: stop syncing the paths. One bad path changes
// nothing, as in the CLI.
func (m Model) ignorePaths(paths []string) (Model, tea.Cmd) {
	cfg, e := m.config(), m.env
	var actions []plan.Action
	seen := map[string]bool{}
	for _, raw := range paths {
		abs, rel, isDir, err := m.target(raw, true)
		if err == nil && strings.Contains(rel, "*") {
			err = fmt.Errorf("%s: a name with * would be read as a pattern", abs)
		}
		if err == nil {
			for _, l := range cfg.Links {
				orig := filepath.Join(e.Paths.Workspace, filepath.FromSlash(l.Original))
				if repo.IsWithin(abs, orig) || repo.IsWithin(orig, abs) {
					err = fmt.Errorf("%s is the workspace copy of the dotfile %s: it must sync for dotfile link on your other machines", abs, l.System)
					break
				}
			}
		}
		if err != nil {
			return m, nav.Notify(nav.LevelError, err.Error())
		}
		if seen[rel] || !wsignore.BuildEngine(cfg.IgnoreRules).Evaluate(rel, isDir).Included {
			continue // listed twice, or already ignored
		}
		seen[rel] = true
		actions = append(actions, plan.Action{
			ID:          "ignore-add-" + abs,
			Description: fmt.Sprintf("Ignore %s (ws.json ignore_rules.exclude)", abs),
			Execute: func() error {
				if _, err := wsconfig.AddIgnoreExclude(e.Paths.Config, rel, ""); err != nil {
					return err
				}
				return regenerate(e)
			},
		})
	}
	return m.open("Ignore "+format.Plural(len(actions), "path"), "Already ignored.", "", actions)
}

// rmPaths is `ignore rm`: stop ignoring the paths, so they sync again. A path
// ignored by a wider or built-in rule cannot be undone here: the whole command
// is refused and says why.
func (m Model) rmPaths(paths []string) (Model, tea.Cmd) {
	cfg, e := m.config(), m.env
	var actions []plan.Action
	seen := map[string]bool{}
	for _, raw := range paths {
		abs, rel, isDir, err := m.target(raw, false)
		if err != nil {
			return m, nav.Notify(nav.LevelError, err.Error())
		}
		v := wsignore.ClassifyRm(cfg.IgnoreRules, rel, isDir)
		switch v.Kind {
		case wsignore.RmNotIgnored:
			continue
		case wsignore.RmWider:
			return m, nav.Notify(nav.LevelError, fmt.Sprintf("%s is ignored by your rule `%s`, not by an entry of its own: remove that rule, or change it with %s",
				abs, v.Rule, m.keys.EditRules.Help().Key+" "+m.keys.EditRules.Help().Desc))
		case wsignore.RmBuiltin:
			return m, nav.Notify(nav.LevelError, fmt.Sprintf("%s is ignored by the built-in rule `%s`: ws does not edit built-in rules for one path", abs, v.Rule))
		}
		if seen[v.Entry] {
			continue
		}
		seen[v.Entry] = true
		entry, still := v.Entry, v.Rule
		desc := fmt.Sprintf("Sync again: %s (remove %q from ignore_rules.exclude)", abs, entry)
		if still != "" {
			desc += fmt.Sprintf("; still ignored by `%s`", still)
		}
		actions = append(actions, plan.Action{
			ID:          "ignore-rm-" + abs,
			Description: desc,
			Execute: func() error {
				if _, err := wsconfig.RemoveIgnoreExclude(e.Paths.Config, entry); err != nil {
					return err
				}
				return regenerate(e)
			},
		})
	}
	return m.open("Stop ignoring "+format.Plural(len(actions), "path"), "Not ignored: nothing to do.", "", actions)
}

// mutePaths is `ignore mute`: hide the paths' warnings; they keep syncing.
func (m Model) mutePaths(paths []string) (Model, tea.Cmd) {
	cfg, e := m.config(), m.env
	eng := wsignore.BuildEngine(cfg.IgnoreRules)
	muted := wsignore.NormalizeMuted(e.Paths.Workspace, cfg.Ignore.Muted)
	var actions []plan.Action
	seen := map[string]bool{}
	for _, raw := range paths {
		abs, rel, isDir, err := m.target(raw, true)
		if err != nil {
			return m, nav.Notify(nav.LevelError, err.Error())
		}
		if seen[rel] || wsignore.MuteStateOf(eng, muted, rel, isDir).Muted {
			continue // listed twice, or already muted
		}
		seen[rel] = true
		actions = append(actions, plan.Action{
			ID:          "ignore-mute-" + abs,
			Description: fmt.Sprintf("Mute %s (it keeps syncing)", abs),
			Execute: func() error {
				_, err := wsconfig.AddIgnoreMuted(e.Paths.Config, rel)
				return err
			},
		})
	}
	return m.open("Mute "+format.Plural(len(actions), "path"), "Already muted.", "", actions)
}

// unmuteRows is `ignore unmute` for Muted rows. A row muted through a parent
// directory unmutes that entry, once however many of its rows are chosen; a row
// muted by an always-sync directory cannot be unmuted here.
func (m Model) unmuteRows(rows []wsignore.Violation) (Model, tea.Cmd) {
	cfg, e := m.config(), m.env
	eng := wsignore.BuildEngine(cfg.IgnoreRules)
	muted := wsignore.NormalizeMuted(e.Paths.Workspace, cfg.Ignore.Muted)
	var actions []plan.Action
	seen := map[string]bool{}
	for _, v := range rows {
		abs := m.abs(v.Path)
		st := wsignore.MuteStateOf(eng, muted, wsignore.NormalizeRel(v.Path), false)
		switch {
		case !st.Muted:
			continue
		case st.Entry == "":
			return m, nav.Notify(nav.LevelError, fmt.Sprintf("%s is under the always-sync directory `%s`: its warnings stay muted. To change it, use %s",
				abs, st.Rule, m.keys.EditRules.Help().Key+" "+m.keys.EditRules.Help().Desc))
		case seen[st.Entry]:
			continue
		}
		entry := st.Entry
		seen[entry] = true
		desc := "Unmute " + m.abs(entry)
		if entry != wsignore.NormalizeRel(v.Path) {
			desc += " (its warnings include " + abs + ")"
		}
		actions = append(actions, plan.Action{
			ID:          "ignore-unmute-" + m.abs(entry),
			Description: desc,
			Execute: func() error {
				_, err := wsconfig.RemoveIgnoreMuted(e.Paths.Config, e.Paths.Workspace, entry)
				return err
			},
		})
	}
	return m.open("Unmute "+format.Plural(len(actions), "path"), "Not muted: nothing to do.", "", actions)
}

// scratchPaths is `scratch add`: move the paths into the scratch directory.
func (m Model) scratchPaths(paths []string) (Model, tea.Cmd) {
	e := m.env
	var abs []string
	for _, raw := range paths {
		a, _, _, err := m.target(raw, true)
		if err != nil {
			return m, nav.Notify(nav.LevelError, err.Error())
		}
		abs = append(abs, a)
	}
	plans, err := wsscratch.PlanAdd(wsscratch.AddOptions{
		RootDir: e.ScratchDir, WorkspacePath: e.Paths.Workspace, ConfigPath: e.Paths.Config,
		Links: m.config().Links, Paths: abs,
	})
	if err != nil {
		return m, nav.Notify(nav.LevelError, err.Error())
	}
	var actions []plan.Action
	for _, ap := range plans {
		ap := ap
		actions = append(actions, plan.Action{
			ID:          "scratch-add-" + ap.Source,
			Description: fmt.Sprintf("Move %s → %s", ap.Source, ap.Dest),
			Execute:     func() error { return wsscratch.Add(ap) },
		})
	}
	return m.open("Move "+format.Plural(len(actions), "path")+" to scratch", "Nothing to move.", "", actions)
}

// deleteRows deletes the rows' paths to the trash.
func (m Model) deleteRows(rows []wsignore.Violation) (Model, tea.Cmd) {
	var actions []plan.Action
	seen := map[string]bool{}
	for _, v := range rows {
		p := m.abs(v.Path)
		if seen[p] {
			continue
		}
		seen[p] = true
		desc := "Delete " + p
		if v.SizeBytes > 0 {
			desc += " (" + style.HumanBytes(v.SizeBytes) + ")"
		}
		actions = append(actions, plan.Action{
			ID:          "delete-" + p,
			Description: desc + " (to trash)",
			Execute:     func() error { return trash.SoftDelete(p) },
		})
	}
	return m.open("Delete "+format.Plural(len(actions), "path")+" (to trash)", "Nothing to delete.", "", actions)
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
	m.warn = m.warn.ClearMarks() // the marks were for this action
	m.muted = m.muted.ClearMarks()
	m.ignored = m.ignored.ClearMarks()
	level := nav.LevelSuccess
	if done.Result.HasFailures() {
		level = nav.LevelWarn
	}
	m, cmd := m.rescan()
	return m, tea.Batch(nav.Notify(level, confirm.Summary(done.Result)), cmd)
}

// thousands writes n with a comma between each group of three digits.
func thousands(n int) string {
	s := fmt.Sprint(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

// header is the view line: `Warnings 12 · Muted 47 · Ignored 3,210`, the
// current view in bold.
func (m Model) header() string {
	if m.scanning {
		return theme.Muted.Render("Scanning workspace…")
	}
	tab := func(v keys.IgnoreView, name string, n int) string {
		text := fmt.Sprintf("%s %s", name, thousands(n))
		if m.view == v {
			return theme.Title.Render(text)
		}
		return theme.Muted.Render(text)
	}
	parts := []string{
		tab(keys.IgnoreWarnings, "Warnings", len(m.warn.Items())),
		tab(keys.IgnoreMuted, "Muted", len(m.muted.Items())),
		tab(keys.IgnoreIgnored, "Ignored", len(m.ignored.Items())),
	}
	line := strings.Join(parts, theme.Muted.Render(" · "))
	if len(m.under) > 0 {
		var rel []string
		for _, u := range m.under {
			rel = append(rel, m.env.Rel(u))
		}
		line += theme.Muted.Render("  under " + strings.Join(rel, ", ") + " — " + m.keys.Back.Help().Key + " clears")
	}
	return lipgloss.NewStyle().MaxWidth(m.width).Render(line)
}

func (m Model) activeList() string {
	switch m.view {
	case keys.IgnoreMuted:
		return m.muted.View()
	case keys.IgnoreIgnored:
		return m.ignored.View()
	}
	return m.warn.View()
}

// View implements nav.Component.
func (m Model) View() string {
	if m.modal.Open() {
		return m.modal.View()
	}
	return lipgloss.JoinVertical(lipgloss.Left, m.header(), m.activeList())
}

// KeyMap implements nav.Component.
func (m Model) KeyMap() help.KeyMap {
	if m.modal.Open() {
		return m.modal.KeyMap()
	}
	screen := m.keys.For(m.view)
	switch m.view {
	case keys.IgnoreMuted:
		return m.muted.Help(screen)
	case keys.IgnoreIgnored:
		return m.ignored.Help(screen)
	}
	return m.warn.Help(screen)
}
