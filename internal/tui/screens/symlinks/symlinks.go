// Package symlinks is the managed-symlinks screen (spec §6.5): every link
// record with its symlink health, plus add, link and unlink. Actions on sudo
// entries are handed to the CLI in the terminal. esc asks the app to go up.
package symlinks

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/mugenkunou/ws-tool/internal/plan"
	"github.com/mugenkunou/ws-tool/internal/style"
	"github.com/mugenkunou/ws-tool/internal/symlink"
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

// Entry is one managed symlink and its health.
type Entry = symlink.Entry

// LoadedMsg carries the result of loading the symlink list. Other components
// (the dashboard) may observe it. Entries are the rows `ws symlink ls` shows.
type LoadedMsg struct {
	Entries []Entry
	// Walked is true when the load included the $HOME walk for untracked links. The
	// first load of every refresh leaves it out (the Dashboard shows that one); the
	// screen follows it with one that walks.
	Walked bool
	Notes  []string
	Err    error
}

// Registered counts the rows that are ws.json entries.
func (m LoadedMsg) Registered() int {
	n := 0
	for _, e := range m.Entries {
		if e.Registered {
			n++
		}
	}
	return n
}

// Issues counts the rows that need attention: an entry that is not ok, an
// untracked link, an orphan.
func (m LoadedMsg) Issues() int {
	n := 0
	for _, e := range m.Entries {
		if e.Status != symlink.StatusOK {
			n++
		}
	}
	return n
}

// SelectMsg asks the screen to select the entry for a system path (sent by
// the dashboard when it opens this screen at a specific problem).
type SelectMsg struct{ System string }

// addCandidate is one thing `symlink add` offers to take: a file, a directory
// entry, a whole directory (`--dir`), or the live file of an overwritten entry.
type addCandidate struct {
	label    string
	files    []string // what to add one by one
	secret   bool     // the catalog lists it as a secret: unchecked unless named exactly
	whole    bool     // the directory itself is the entry (--dir)
	takeLive bool     // the entry exists; the live file replaces the workspace copy
	always   bool     // pre-checked even when secret (named exactly)
	to       string   // the absolute workspace path of the two-path form
}

// addRequest is what `add` was asked to take: the marked rows, or the words of
// `:add <host> [<ws-path>] [--dir]`.
type addRequest struct {
	paths []string
	to    string // the workspace path of the two-path form, as typed
	whole bool   // --dir
}

// args are the request as CLI words, for handing a sudo add to the terminal.
func (r addRequest) args() []string {
	out := []string{"symlink", "add"}
	if r.whole {
		out = append(out, "--dir")
	}
	out = append(out, r.paths...)
	if r.to != "" {
		out = append(out, r.to)
	}
	return out
}

// addPreparedMsg carries what the paths expand to.
type addPreparedMsg struct {
	req        addRequest
	candidates []addCandidate
	notes      []string // paths that need nothing, and why
	sudo       bool     // one of them needs root: the CLI does it in the terminal
	err        error
}

type step int

const (
	stepAdd step = iota
	stepAddPath
	stepUnlink
	stepLink
)

// handoverTag identifies programs run in the terminal.
type handoverTag int

const (
	handoverWizard handoverTag = iota // a sudo action
)

// Model is the Symlinks screen state.
type Model struct {
	env       env.Env
	keys      keys.SymlinksMap
	list      reslist.Model[Entry]
	modal     modal.Model
	preparing bool
	width     int
	height    int
}

// New creates the screen.
func New(e env.Env) Model {
	k := keys.Symlinks()
	return Model{
		env:  e,
		keys: k,
		list: reslist.New(symlink.Schema(e.View()), []reslist.Col[Entry]{
			{Key: "system", Min: 20, Weight: 3},
			{Key: "workspace_path", Min: 12, Weight: 2},
			{Key: "app", Min: 6},
			{Key: "status", Min: 10, Style: statusStyle},
			{Key: "sudo", Min: 4},
		}, theme.Icon(theme.IconSprout)+"No symlinks managed yet. Press "+k.Add.Help().Key+" to add one.").Declare(reslist.Decls(actions)),
	}
}

func statusStyle(e Entry, text string) string {
	switch e.Status {
	case symlink.StatusOK:
		return theme.OK.Render("✔ " + text)
	case symlink.StatusBroken:
		return theme.Error.Render("✘ " + text)
	}
	return theme.Warn.Render("⚠ " + text)
}

// Load reads the config and detects every symlink, without the $HOME walk (the
// Dashboard summarizes this one).
func Load(e env.Env) tea.Cmd { return load(e, false) }

func load(e env.Env, walk bool) tea.Cmd {
	return func() tea.Msg {
		l, err := symlink.Load(e.Paths.Workspace, e.Paths.Config, walk)
		if err != nil {
			return LoadedMsg{Err: err, Walked: walk}
		}
		return LoadedMsg{Entries: l.All, Notes: l.Notes, Walked: walk}
	}
}

// prepareAdd plans the paths as `ws symlink add` does: a drifted entry takes the
// live file again, an untracked link is registered, a directory expands to its
// entries (or is taken whole with --dir, or when the catalog knows it as a folder).
func prepareAdd(e env.Env, req addRequest) tea.Cmd {
	return func() tea.Msg {
		msg := addPreparedMsg{req: req}
		var paths []string
		for _, raw := range req.paths {
			abs, err := wsconfig.ExpandUserPath(raw)
			if err != nil {
				msg.err = fmt.Errorf("invalid path %q: %w", raw, err)
				return msg
			}
			if !slices.Contains(paths, abs) {
				paths = append(paths, abs)
			}
		}
		cfg, err := wsconfig.Load(e.Paths.Config)
		if err != nil {
			msg.err = err
			return msg
		}
		to := ""
		if req.to != "" {
			if len(paths) != 1 {
				msg.err = errors.New("a workspace path goes with exactly one host path")
				return msg
			}
			to = req.to
			if !filepath.IsAbs(to) {
				to = filepath.Join(e.Paths.Workspace, to)
			}
			to = filepath.Clean(to)
		}
		for _, abs := range paths {
			_, managed := symlink.FindLink(cfg, abs)
			info, statErr := os.Lstat(abs)
			realDir := statErr == nil && info.Mode()&os.ModeSymlink == 0 && info.IsDir() && !managed
			if realDir && to != "" && !req.whole {
				msg.err = errors.New("a folder goes in whole with --dir to <ws-path>")
				return msg
			}
			if realDir && !req.whole && !symlink.IsCatalogFolder(abs) {
				entries, _, err := symlink.ExpandDir(abs)
				if err != nil {
					msg.err = err
					return msg
				}
				for _, ent := range entries {
					files, err := symlink.CollectFiles(ent)
					if err != nil {
						msg.err = fmt.Errorf("reading %s: %w", ent.AbsPath, err)
						return msg
					}
					label := ent.AbsPath
					if ent.IsDir {
						label += fmt.Sprintf("/  (%s, %s)", format.Plural(ent.FileCount, "file"), style.HumanBytes(ent.Size))
					}
					msg.sudo = msg.sudo || slices.ContainsFunc(files, symlink.NeedsSudo)
					msg.candidates = append(msg.candidates, addCandidate{label: label, files: files, secret: ent.Secret})
				}
				continue
			}
			whole := realDir
			ap, err := symlink.PlanAdd(symlink.AddOptions{
				WorkspacePath: e.Paths.Workspace, ConfigPath: e.Paths.Config, Host: abs, To: to, Dir: whole,
			})
			if err != nil {
				msg.err = fmt.Errorf("%s: %w", abs, err)
				return msg
			}
			switch ap.Case {
			case symlink.CaseManaged:
				msg.notes = append(msg.notes, abs+" is already managed")
			case symlink.CaseNothing:
				msg.notes = append(msg.notes, abs+": "+ap.Note)
			default:
				msg.sudo = msg.sudo || ap.Sudo
				msg.candidates = append(msg.candidates, addCandidate{
					label: abs, files: []string{abs}, to: to, whole: whole && ap.Case == symlink.CaseMove,
					takeLive: ap.Case == symlink.CaseTake, secret: symlink.IsCatalogSecret(abs), always: true,
				})
			}
		}
		return msg
	}
}

// Init starts the first load.
func (m Model) Init() tea.Cmd { return Load(m.env) }

// Declared lists the screen's actions and their scopes.
func (m Model) Declared() []reslist.Decl { return m.list.Declared() }

// Commands implements nav.Commander: the `:` commands (spec §6.5).
func (m Model) Commands() []nav.CommandInfo { return reslist.CommandInfos(commands) }

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
		m.modal = m.modal.SetSize(msg.Width, msg.Height)
		return m, nil
	case nav.FilterMsg:
		m.list = m.list.SetFilter(msg.Filter)
		return m, nil
	case nav.RefreshMsg:
		m.list = m.list.SetLoading()
		return m, Load(m.env)
	case nav.CommandMsg:
		if m.modal.Open() {
			return m, nil
		}
		next, cmd, _ := reslist.RunCommand(m, m.list, commands, msg)
		return next, cmd
	case LoadedMsg:
		if msg.Err != nil {
			m.list = m.list.SetError(msg.Err)
			return m, nil
		}
		m.list = m.list.SetItems(msg.Entries)
		if !msg.Walked {
			// The quick load is what the Dashboard shows; the walk for untracked
			// links follows, as `ws symlink ls` runs it.
			return m, load(m.env, true)
		}
		return m, nil
	case SelectMsg:
		m.list, _ = m.list.Select(func(e Entry) bool { return e.System == msg.System })
		return m, nil
	case addPreparedMsg:
		m.preparing = false
		return m.prepared(msg)
	case handover.DoneMsg:
		tag, ok := msg.Tag.(handoverTag)
		if !ok {
			return m, nil
		}
		_ = tag
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
	return m.updateList(k)
}

// actions is every Symlinks key and the rows it acts on (spec §6.5).
var actions = func() []reslist.Action[Model, Entry] {
	k := keys.Symlinks()
	return []reslist.Action[Model, Entry]{
		{Key: k.Add, Scope: reslist.Marked, Do: Model.add},
		{Key: k.Unlink, Scope: reslist.Selected, Do: Model.unlink},
		{Key: k.Link, Scope: reslist.All, Do: Model.link},
	}
}()

// commands is every `:` command: the CLI's words after `symlink`. A key and its
// command share one function.
var commands = []reslist.Command[Model, Entry]{
	{Name: "add", Args: "[<host> [<ws>]] [--dir]", Scope: reslist.Marked,
		Help: "take a path into the workspace; alone: asks for the path", Do: Model.addCommand},
	{Name: "link", Args: "[<path>…]", Scope: reslist.Global,
		Help: "repair the shown links, or the named ones", Do: Model.linkCommand},
	{Name: "unlink --all", Scope: reslist.Global,
		Help: "unlink every symlink (nothing is pre-checked)", Do: Model.unlinkAllCommand},
}

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

// ── add ────────────────────────────────────────────────────────────

// add is `a`: with marks, take the marked rows that are drifted (the live file wins)
// or untracked (registered, nothing moves); without, ask for the path to take.
func (m Model) add(marked []Entry) (Model, tea.Cmd) {
	if m.preparing {
		return m, nil
	}
	if len(marked) == 0 {
		return m.promptPath()
	}
	var paths []string
	for _, e := range marked {
		if e.Status == symlink.StatusDrifted || e.Status == symlink.StatusUntracked {
			paths = append(paths, e.System)
		}
	}
	if len(paths) == 0 {
		return m, nav.Notify(nav.LevelInfo, "Nothing to take: none of the marked rows is drifted or untracked.")
	}
	m.preparing = true
	return m, prepareAdd(m.env, addRequest{paths: paths})
}

// promptPath asks for the host path `a` takes, as `ws symlink add <host-path>` does.
func (m Model) promptPath() (Model, tea.Cmd) {
	var cmd tea.Cmd
	m.modal, cmd = m.modal.Prompt(stepAddPath, prompt.New("Add a path to the workspace",
		prompt.WithHint("A file or folder on this machine. :add <path> [<ws-path>] [--dir] takes more."),
		prompt.WithValue("~/"),
		prompt.WithCompleter(complete.Dirs)))
	return m, cmd
}

// addCommand is `:add`: `add <host> [<ws-path>] [--dir]`; alone, the same as the key.
func (m Model) addCommand(marked []Entry, args []string) (Model, tea.Cmd) {
	if len(args) == 0 {
		return m.add(marked)
	}
	const usage = "usage: add <host-path> [<ws-path>] [--dir]"
	var req addRequest
	var words []string
	for _, a := range args {
		switch {
		case a == "--dir":
			req.whole = true
		case strings.HasPrefix(a, "-"):
			return m, nav.Notify(nav.LevelWarn, "Unknown flag "+a+"; "+usage)
		default:
			words = append(words, a)
		}
	}
	if len(words) == 0 || len(words) > 2 {
		return m, nav.Notify(nav.LevelWarn, usage)
	}
	req.paths = words[:1]
	if len(words) == 2 {
		req.to = words[1]
	}
	if m.preparing {
		return m, nil
	}
	m.preparing = true
	return m, prepareAdd(m.env, req)
}

// prepared opens the add checklist, or hands a sudo add to the terminal.
func (m Model) prepared(msg addPreparedMsg) (Model, tea.Cmd) {
	switch {
	case msg.err != nil:
		return m, nav.Notify(nav.LevelError, msg.err.Error())
	case len(msg.candidates) == 0 && len(msg.notes) > 0:
		return m, nav.Notify(nav.LevelInfo, strings.Join(msg.notes, "; "))
	case len(msg.candidates) == 0:
		return m, nav.Notify(nav.LevelInfo, "Directory is empty — nothing to add.")
	case msg.sudo:
		return m, m.terminal(msg.req.args()...)
	}
	return m.openAdd(msg), nil
}

func (m Model) openAdd(msg addPreparedMsg) Model {
	p := m.env.Paths
	pl := plan.Plan{Command: "symlink.add"}
	for _, c := range msg.candidates {
		c := c
		desc := "Add " + m.env.Rel(c.label)
		switch {
		case c.takeLive:
			desc += theme.Muted.Render("  (the live file replaces the workspace copy; the old copy goes to the trash)")
		case c.whole:
			desc = "Add directory " + m.env.Rel(c.label) + theme.Muted.Render("  (whole)")
		case c.secret:
			desc += theme.Warn.Render("  ⚠ secret")
		}
		pl.Actions = append(pl.Actions, plan.Action{
			ID:          "symlink-add-" + c.label,
			Description: desc,
			Execute: func() error {
				for _, f := range c.files {
					// A drifted entry is taken by the same call: Add sees the entry.
					_, err := symlink.Add(symlink.AddOptions{WorkspacePath: p.Workspace, ConfigPath: p.Config, Host: f, To: c.to, Dir: c.whole})
					if err != nil {
						return fmt.Errorf("%s: %w", f, err)
					}
				}
				return nil
			},
		})
	}
	// Pre-checked as the CLI does: everything except catalog secrets, and everything
	// the user named exactly (--dir, a single path, an overwritten entry).
	cands := msg.candidates
	title := "Add symlinks"
	if len(msg.req.paths) == 1 {
		title += " from " + msg.req.paths[0]
	}
	c := confirm.New(title, pl).WithChecked(func(i int, _ plan.Action) bool {
		return cands[i].always || !cands[i].secret
	})
	m.modal = m.modal.Confirm(stepAdd, c)
	return m
}

// ── unlink ─────────────────────────────────────────────────────────

// unlink is ctrl+d: restore each row's file to its system path and forget it.
func (m Model) unlink(rows []Entry) (Model, tea.Cmd) { return m.openUnlink(rows, false) }

// unlinkAllCommand is `:unlink --all`.
func (m Model) unlinkAllCommand(_ []Entry, args []string) (Model, tea.Cmd) {
	if len(args) != 0 {
		return m, nav.Notify(nav.LevelWarn, "usage: unlink --all")
	}
	if len(m.list.Items()) == 0 {
		return m, nav.Notify(nav.LevelInfo, "No managed symlinks.")
	}
	return m.openUnlink(m.list.Items(), true)
}

// openUnlink mirrors `ws symlink unlink`. Unlinking everything starts with
// nothing checked (spec §6.7). Sudo entries run in the terminal.
func (m Model) openUnlink(rows []Entry, nothingChecked bool) (Model, tea.Cmd) {
	// Only ws.json entries can be unlinked; an untracked link or an orphan is not one.
	rows = slices.DeleteFunc(slices.Clone(rows), func(e Entry) bool { return !e.Registered })
	if len(rows) == 0 {
		return m, nav.Notify(nav.LevelInfo, "Nothing to unlink: none of the rows is a managed symlink.")
	}
	if sys := sudoPaths(rows); len(sys) > 0 {
		return m, m.terminal(append([]string{"symlink", "unlink"}, systemPaths(rows)...)...)
	}
	p := m.env.Paths
	// An unreadable ws.json only costs the plan line its detail about shared
	// copies; running the action reports the error.
	cfg, _ := wsconfig.Load(p.Config)
	pl := plan.Plan{Command: "symlink.unlink"}
	for _, e := range rows {
		sys := e.System
		link := wsconfig.Link{System: sys, Original: e.Original, Dir: e.Dir, Sudo: e.Sudo}
		pl.Actions = append(pl.Actions, plan.Action{
			ID:          "symlink-unlink-" + sys,
			Description: unlinkDescription(symlink.PlanUnlink(p.Workspace, cfg, link)),
			Execute: func() error {
				return symlink.Unlink(symlink.UnlinkOptions{WorkspacePath: p.Workspace, ConfigPath: p.Config, SystemPath: sys})
			},
		})
	}
	c := confirm.New("Unlink "+format.Plural(len(rows), "symlink"), pl)
	if nothingChecked {
		c = c.WithChecked(func(int, plan.Action) bool { return false })
	}
	m.modal = m.modal.Confirm(stepUnlink, c)
	return m, nil
}

// unlinkDescription is the plan line of one unlink, as the CLI words it.
func unlinkDescription(p symlink.UnlinkPlan) string {
	switch p.Mode {
	case symlink.ModeMissing:
		return fmt.Sprintf("Unregister %s (workspace copy missing; nothing to restore)", p.Link.System)
	case symlink.ModeCopy:
		d := fmt.Sprintf("Unlink %s (copy %s there; the original stays)", p.Link.System, p.Workspace)
		if p.Backup != "" {
			d = strings.TrimSuffix(d, ")") + fmt.Sprintf("; the live file moves to %s)", p.Backup)
		}
		return d
	}
	d := fmt.Sprintf("Unlink %s (move %s back)", p.Link.System, p.Workspace)
	if p.Backup != "" {
		d = strings.TrimSuffix(d, ")") + fmt.Sprintf("; the live file moves to %s)", p.Backup)
	}
	return d
}

// ── link ───────────────────────────────────────────────────────────

// link is `l`: repair the missing and broken rows and register orphans, the
// workspace copy winning (`ws symlink link`). A drifted row is left for `add`, which
// keeps the live file. Sudo entries run in the terminal.
func (m Model) link(rows []Entry) (Model, tea.Cmd) {
	var bad []Entry
	for _, e := range rows {
		switch e.Status {
		case symlink.StatusMissing, symlink.StatusBroken, symlink.StatusOrphan:
			bad = append(bad, e)
		}
	}
	if len(bad) == 0 {
		return m, nav.Notify(nav.LevelInfo, "Nothing to link: no shown row is missing, broken or an orphan.")
	}
	if len(sudoPaths(bad)) > 0 {
		return m, m.terminal(append([]string{"symlink", "link"}, systemPaths(bad)...)...)
	}
	p := m.env.Paths
	pl := plan.Plan{Command: "symlink.link"}
	for _, e := range bad {
		sys := e.System
		desc := "Link " + m.env.Rel(sys)
		if rp, err := symlink.PlanLink(p.Workspace, p.Config, sys); err == nil && rp.Backup != "" {
			desc += theme.Muted.Render("  (the live file moves to " + m.env.Rel(rp.Backup) + ")")
		}
		pl.Actions = append(pl.Actions, plan.Action{
			ID:          "symlink-link-" + sys,
			Description: desc,
			Execute: func() error {
				return symlink.Link(symlink.LinkOptions{WorkspacePath: p.Workspace, ConfigPath: p.Config, SystemPath: sys})
			},
		})
	}
	m.modal = m.modal.Confirm(stepLink, confirm.New("Link symlinks", pl))
	return m, nil
}

// linkCommand is `:link`. With paths it repairs those entries; with nothing it
// is the key.
func (m Model) linkCommand(_ []Entry, args []string) (Model, tea.Cmd) {
	var paths []string
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			return m, nav.Notify(nav.LevelWarn, "Unknown flag "+a+"; usage: link [<path>…]")
		}
		paths = append(paths, a)
	}
	if len(paths) == 0 {
		return m.link(m.list.Rows(reslist.All))
	}
	var rows []Entry
	for _, raw := range paths {
		abs, err := wsconfig.ExpandUserPath(raw)
		if err != nil {
			return m, nav.Notify(nav.LevelWarn, err.Error())
		}
		i := slices.IndexFunc(m.list.Items(), func(e Entry) bool { return e.System == abs })
		if i < 0 {
			return m, nav.Notify(nav.LevelWarn, "Not a managed symlink: "+abs)
		}
		rows = append(rows, m.list.Items()[i])
	}
	return m.link(rows)
}

// ── terminal ───────────────────────────────────────────────────────

// terminal hands the terminal to `ws <args>`: the CLI wizards and the actions
// on sudo entries, where sudo may ask for a password (spec §6.8).
func (m Model) terminal(args ...string) tea.Cmd {
	return handover.Run(handover.Pause(handover.WS(m.env.Paths, args...)), handoverWizard)
}

func systemPaths(rows []Entry) []string {
	out := make([]string, len(rows))
	for i, e := range rows {
		out[i] = e.System
	}
	return out
}

// sudoPaths are the system paths of the rows that need root.
func sudoPaths(rows []Entry) []string {
	var out []string
	for _, e := range rows {
		if e.Sudo {
			out = append(out, e.System)
		}
	}
	return out
}

func (m Model) dialogResult(st step, msg tea.Msg) (nav.Component, tea.Cmd) {
	if st == stepAddPath {
		r := msg.(prompt.ResultMsg)
		if v := strings.TrimSpace(r.Value); !r.Cancelled && v != "" && v != "~/" && !m.preparing {
			m.preparing = true
			return m, prepareAdd(m.env, addRequest{paths: []string{v}})
		}
		return m, nil
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
	m.list = m.list.SetLoading()
	cmds = append(cmds, Load(m.env))
	return m, tea.Batch(cmds...)
}

// View implements nav.Component.
func (m Model) View() string {
	switch {
	case m.modal.Open():
		return m.modal.View()
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
	}
	return m.list.Help(m.keys)
}

var _ nav.Commander = Model{}
