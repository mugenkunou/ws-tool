// Package dotfiles is the managed-dotfiles screen (spec §6.5): every link
// record with its symlink health, plus add, link, unlink, edit and the dotfile
// backup (back up now, and the `:backup` commands). Multi-step wizards
// (`backup enable`) and actions on sudo entries are handed to the CLI in the
// terminal.
//
// Navigation levels: list → backup status view. esc in the view returns to
// the list; esc in the list asks the app to go up.
package dotfiles

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"github.com/mugenkunou/ws-tool/internal/backup"
	"github.com/mugenkunou/ws-tool/internal/cron"
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

// RequestFixMsg asks the screen to open its link checklist.
type RequestFixMsg struct{}

// addCandidate is one thing `dotfile add` offers to take: a file, a directory
// entry, a whole directory (`--dir`), or the live file of an overwritten entry.
type addCandidate struct {
	label    string
	files    []string // what to add one by one
	class    dotfile.FileClass
	whole    bool // the directory itself is the entry (--dir)
	takeLive bool // the entry exists; the live file replaces the workspace copy
	always   bool // pre-checked whatever its class
}

// addRequest is what `add` was asked to take: a path typed in the prompt, or
// the words of `:add`.
type addRequest struct {
	paths []string
	whole bool   // --dir
	to    string // --to, as typed
}

// args are the request as CLI words, for handing a sudo add to the terminal.
func (r addRequest) args() []string {
	out := []string{"dotfile", "add"}
	if r.whole {
		out = append(out, "--dir")
	}
	if r.to != "" {
		out = append(out, "--to", r.to)
	}
	return append(out, r.paths...)
}

// addPreparedMsg carries what the paths expand to.
type addPreparedMsg struct {
	req        addRequest
	candidates []addCandidate
	notes      []string // paths that need nothing, and why
	sudo       bool     // one of them needs root: the CLI does it in the terminal
	err        error
}

// backupViewMsg carries the `backup status` text.
type backupViewMsg struct{ text string }

type level int

const (
	levelList level = iota
	levelBackup
)

type step int

const (
	stepAddPath step = iota
	stepAdd
	stepUnlink
	stepLink
	stepLinkTo
	stepBackupPush
	stepBackupDisable
)

// handoverTag identifies programs run in the terminal.
type handoverTag int

const (
	handoverWizard handoverTag = iota // `backup enable`, or a sudo action
	handoverEditor
)

// Model is the dotfiles screen state.
type Model struct {
	env        env.Env
	keys       keys.DotfilesMap
	detailKeys keys.DetailMap
	list       reslist.Model[Entry]
	view       viewport.Model // the `backup status` view
	level      level
	modal      modal.Model
	preparing  bool
	width      int
	height     int
}

// New creates the screen.
func New(e env.Env) Model {
	k := keys.Dotfiles()
	dk := keys.Detail()
	vp := viewport.New()
	vp.KeyMap = dk.Viewport
	vp.SoftWrap = true
	return Model{
		env:        e,
		keys:       k,
		detailKeys: dk,
		view:       vp,
		list: reslist.New(dotfile.Schema(e.View()), []reslist.Col[Entry]{
			{Key: "system", Min: 20, Weight: 3},
			{Key: "workspace_path", Min: 12, Weight: 2, Style: copyStyle},
			{Key: "status", Min: 13, Style: statusStyle},
			{Key: "sudo", Min: 4},
		}, theme.Icon(theme.IconSprout)+"No dotfiles managed yet. Press "+k.Add.Help().Key+" to add one.").Declare(reslist.Decls(actions)),
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

// prepareAdd stats the paths and expands them as `ws dotfile add` does: a
// managed path that was overwritten takes the live file again, a directory
// expands to its entries (or is taken whole with --dir).
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
		if req.to != "" && len(paths) != 1 {
			msg.err = fmt.Errorf("--to takes exactly one path")
			return msg
		}
		cfg, err := wsconfig.Load(e.Paths.Config)
		if err != nil {
			msg.err = err
			return msg
		}
		issues, err := dotfile.Scan(dotfile.ScanOptions{WorkspacePath: e.Paths.Workspace, ConfigPath: e.Paths.Config})
		if err != nil {
			msg.err = err
			return msg
		}
		issueFor := map[string]dotfile.Issue{}
		for _, is := range issues {
			issueFor[is.SystemPath] = is
		}
		for _, abs := range paths {
			if link, managed := dotfile.FindLink(cfg, abs); managed {
				is, bad := issueFor[link.System]
				switch {
				case req.to != "":
					msg.err = fmt.Errorf("--to applies to a new path, and %s is already managed", abs)
					return msg
				case !bad:
					msg.notes = append(msg.notes, abs+" is already managed")
				case is.Status == dotfile.StatusOverwritten:
					msg.sudo = msg.sudo || link.Sudo
					msg.candidates = append(msg.candidates, addCandidate{label: abs, files: []string{abs}, takeLive: true, always: true})
				default:
					msg.notes = append(msg.notes, abs+": nothing to take ("+is.Message+")")
				}
				continue
			}
			info, statErr := os.Lstat(abs)
			if statErr != nil {
				msg.err = fmt.Errorf("path not found: %s", abs)
				return msg
			}
			isDir := info.IsDir()
			switch {
			case req.whole:
				if !isDir {
					msg.err = fmt.Errorf("--dir needs a directory: %s", abs)
					return msg
				}
				msg.sudo = msg.sudo || dotfile.NeedsSudo(abs)
				msg.candidates = append(msg.candidates, addCandidate{label: abs, files: []string{abs}, whole: true, always: true})
			case isDir:
				entries, _, err := dotfile.ExpandDir(abs)
				if err != nil {
					msg.err = err
					return msg
				}
				for _, ent := range entries {
					files, err := dotfile.CollectFiles(ent)
					if err != nil {
						msg.err = fmt.Errorf("reading %s: %w", ent.AbsPath, err)
						return msg
					}
					label := ent.AbsPath
					if ent.IsDir {
						label += fmt.Sprintf("/  (%s, %s)", format.Plural(ent.FileCount, "file"), style.HumanBytes(ent.Size))
					}
					msg.sudo = msg.sudo || slices.ContainsFunc(files, dotfile.NeedsSudo)
					msg.candidates = append(msg.candidates, addCandidate{label: label, files: files, class: ent.Class})
				}
			default:
				msg.sudo = msg.sudo || dotfile.NeedsSudo(abs)
				msg.candidates = append(msg.candidates, addCandidate{
					label: abs, files: []string{abs}, class: dotfile.ClassifyFile(abs), always: req.to != "",
				})
			}
		}
		return msg
	}
}

// loadBackupView builds the `backup status` view: the same facts as
// `ws dotfile backup status`.
func loadBackupView(e env.Env) tea.Cmd {
	return func() tea.Msg {
		var b strings.Builder
		b.WriteString(theme.Title.Render("Dotfile backup") + "\n\n")
		field := func(label, value string) {
			b.WriteString(theme.Muted.Render(fmt.Sprintf("%-13s", label)) + " " + value + "\n")
		}
		cfg, err := wsconfig.Load(e.Paths.Config)
		if err != nil {
			return backupViewMsg{text: theme.Error.Render(err.Error())}
		}
		g := cfg.Dotfile.Git
		branch := g.Branch
		if branch == "" {
			branch = backup.DefaultBranch
		}
		repoPath := dotfile.RepoPath(e.Paths.Workspace)
		st := backup.Inspect(repoPath, branch, 20)
		if g.RemoteURL == "" {
			field("Backup", theme.Warn.Render("disabled"))
			if !st.Initialized {
				field("Repository", repoPath+theme.Muted.Render("  (not a git repository yet)"))
			}
			b.WriteString("\n" + theme.Muted.Render("Run :backup enable to set it up."))
			return backupViewMsg{text: b.String()}
		}
		onOff := func(v bool) string {
			if v {
				return "on"
			}
			return "off"
		}
		field("Backup", theme.OK.Render("enabled"))
		field("Repository", repoPath)
		field("Remote", g.RemoteURL)
		field("Branch", st.Branch)
		field("Ahead/behind", fmt.Sprintf("↑%d ↓%d", st.Ahead, st.Behind))
		field("Auto-commit", onOff(g.AutoCommit))
		field("Auto-push", onOff(g.AutoPush))
		if st.LastCommit != "" {
			field("Last commit", st.LastCommit)
		}
		if len(st.Uncommitted) > 0 {
			b.WriteString("\n" + theme.Bold.Render("Uncommitted") + "\n")
			for _, l := range st.Uncommitted {
				b.WriteString("  " + l + "\n")
			}
		} else {
			field("Working tree", theme.OK.Render("clean"))
		}
		if len(st.Commits) > 0 {
			b.WriteString("\n" + theme.Bold.Render("Recent commits") + "\n")
			for _, c := range st.Commits {
				b.WriteString("  " + c.Hash + " " + c.Subject + "\n")
			}
		}
		return backupViewMsg{text: strings.TrimRight(b.String(), "\n")}
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
		m.view.SetWidth(msg.Width)
		m.view.SetHeight(msg.Height)
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
		m.level = levelList
		next, cmd, _ := reslist.RunCommand(m, m.list, commands, msg)
		return next, cmd
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
		return m.link(m.list.Items())
	case addPreparedMsg:
		m.preparing = false
		return m.prepared(msg)
	case backupViewMsg:
		m.view.SetContent(msg.text)
		return m, nil
	case handover.DoneMsg:
		tag, ok := msg.Tag.(handoverTag)
		if !ok {
			return m, nil
		}
		var notice tea.Cmd
		if tag == handoverEditor && msg.Err != nil {
			notice = nav.Notify(nav.LevelWarn, "Editor launch skipped: "+msg.Err.Error())
		}
		m.list = m.list.SetLoading()
		return m, tea.Batch(Load(m.env), notice)
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
	if m.level == levelBackup {
		if key.Matches(k, m.detailKeys.Back) {
			m.level = levelList
			return m, nil
		}
		var cmd tea.Cmd
		m.view, cmd = m.view.Update(k)
		return m, cmd
	}
	return m.updateList(k)
}

// actions is every dotfiles key and the rows it acts on (spec §6.5).
var actions = func() []reslist.Action[Model, Entry] {
	k := keys.Dotfiles()
	return []reslist.Action[Model, Entry]{
		{Key: k.Add, Scope: reslist.Marked, Do: Model.add},
		{Key: k.Unlink, Scope: reslist.Selected, Do: Model.unlink},
		{Key: k.Link, Scope: reslist.All, Do: Model.link},
		{Key: k.Edit, Scope: reslist.Cursor, Do: Model.edit},
		{Key: k.Backup, Scope: reslist.Global, Do: Model.backupPush},
	}
}()

// commands is every `:` command: the CLI's words after `dotfile`. A key and its
// command share one function.
var commands = []reslist.Command[Model, Entry]{
	{Name: "add", Args: "<path…> [--dir] [--to <ws-path>]", Scope: reslist.Marked,
		Help: "take files or directories into the workspace", Do: Model.addCommand},
	{Name: "link", Args: "<path> --to <ws-path>", Scope: reslist.Global,
		Help: "link a path to a file already in the workspace; alone, repair the shown links", Do: Model.linkCommand},
	{Name: "unlink --all", Scope: reslist.Global,
		Help: "unlink every dotfile (nothing is pre-checked)", Do: Model.unlinkAllCommand},
	{Name: "backup enable", Scope: reslist.Global,
		Help: "set up the dotfile backup (the CLI wizard, in the terminal)", Do: Model.backupEnable},
	{Name: "backup disable", Scope: reslist.Global,
		Help: "remove the backup remote and its cron job", Do: func(m Model, _ []Entry, _ []string) (Model, tea.Cmd) { return m.backupDisable() }},
	{Name: "backup status", Scope: reslist.Global,
		Help: "remote, ahead/behind, uncommitted changes, recent commits", Do: func(m Model, _ []Entry, _ []string) (Model, tea.Cmd) { return m.backupStatus() }},
	{Name: "backup push", Scope: reslist.Global,
		Help: "commit pending changes and push", Do: func(m Model, _ []Entry, _ []string) (Model, tea.Cmd) { return m.backupPush(nil) }},
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

// add is `a`: with marks, take the live files of the marked rows that were
// overwritten; without, ask for a path, pre-filled with the cursor row's path
// when that row is overwritten.
func (m Model) add(marked []Entry) (Model, tea.Cmd) {
	if m.preparing {
		return m, nil
	}
	if len(marked) == 0 {
		prefill := "~/"
		if cur, ok := m.list.Current(); ok && cur.Status == dotfile.StatusOverwritten {
			prefill = cur.Link.System
		}
		var cmd tea.Cmd
		m.modal, cmd = m.modal.Prompt(stepAddPath, prompt.New("Add a dotfile or config directory",
			prompt.WithHint("The original moves into configs/dotfiles/ and is symlinked back. A directory lists its entries."),
			prompt.WithValue(prefill),
			prompt.WithCompleter(complete.Paths)))
		return m, cmd
	}
	var paths []string
	for _, e := range marked {
		if e.Status == dotfile.StatusOverwritten {
			paths = append(paths, e.Link.System)
		}
	}
	if len(paths) == 0 {
		return m, nav.Notify(nav.LevelInfo, "Nothing to take: none of the marked dotfiles was overwritten.")
	}
	m.preparing = true
	return m, prepareAdd(m.env, addRequest{paths: paths})
}

// addCommand is `:add`: with paths, add them; alone, the same as the key.
func (m Model) addCommand(marked []Entry, args []string) (Model, tea.Cmd) {
	if len(args) == 0 {
		return m.add(marked)
	}
	var req addRequest
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--dir":
			req.whole = true
		case a == "--to":
			if i+1 >= len(args) {
				return m, nav.Notify(nav.LevelWarn, "--to needs a workspace path")
			}
			i++
			req.to = args[i]
		case strings.HasPrefix(a, "--to="):
			req.to = strings.TrimPrefix(a, "--to=")
		case strings.HasPrefix(a, "-"):
			return m, nav.Notify(nav.LevelWarn, "Unknown flag "+a+"; usage: add <path…> [--dir] [--to <ws-path>]")
		default:
			req.paths = append(req.paths, a)
		}
	}
	if len(req.paths) == 0 {
		return m, nav.Notify(nav.LevelWarn, "usage: add <path…> [--dir] [--to <ws-path>]")
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
	to := ""
	if msg.req.to != "" {
		to, _ = wsconfig.ExpandUserPath(msg.req.to)
	}
	pl := plan.Plan{Command: "dotfile.add"}
	for _, c := range msg.candidates {
		c := c
		desc := "Add " + m.env.Rel(c.label)
		switch {
		case c.takeLive:
			desc += theme.Muted.Render("  (the live file replaces the workspace copy; the old copy goes to the trash)")
		case c.whole:
			desc = "Add directory " + m.env.Rel(c.label) + theme.Muted.Render("  (whole)")
		case c.class == dotfile.FileClassSecret:
			desc += theme.Warn.Render("  ⚠ looks like a secret")
		case c.class == dotfile.FileClassState:
			desc += theme.Muted.Render("  likely state")
		}
		if to != "" {
			desc += theme.Muted.Render("  → " + m.env.Rel(to))
		}
		pl.Actions = append(pl.Actions, plan.Action{
			ID:          "dotfile-add-" + c.label,
			Description: desc,
			Execute: func() error {
				for _, f := range c.files {
					var err error
					if c.takeLive {
						err = dotfile.TakeLive(dotfile.TakeLiveOptions{WorkspacePath: p.Workspace, ConfigPath: p.Config, SystemPath: f})
					} else {
						_, err = dotfile.Add(dotfile.AddOptions{WorkspacePath: p.Workspace, ConfigPath: p.Config, SystemPath: f, To: to, Dir: c.whole})
					}
					if err != nil {
						return fmt.Errorf("%s: %w", f, err)
					}
				}
				return nil
			},
		})
	}
	// Pre-checked as the CLI does: config-like entries, and everything the
	// user named exactly (--dir, --to, an overwritten entry).
	cands := msg.candidates
	title := "Add dotfiles"
	if len(msg.req.paths) == 1 {
		title += " from " + msg.req.paths[0]
	}
	c := confirm.New(title, pl).WithChecked(func(i int, _ plan.Action) bool {
		return cands[i].always || cands[i].class == dotfile.FileClassConfig
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
		return m, nav.Notify(nav.LevelInfo, "No managed dotfiles.")
	}
	return m.openUnlink(m.list.Items(), true)
}

// openUnlink mirrors `ws dotfile unlink`. Unlinking everything starts with
// nothing checked (spec §6.7). Sudo entries run in the terminal.
func (m Model) openUnlink(rows []Entry, nothingChecked bool) (Model, tea.Cmd) {
	if sys := sudoPaths(rows); len(sys) > 0 {
		return m, m.terminal(append([]string{"dotfile", "unlink"}, systemPaths(rows)...)...)
	}
	p := m.env.Paths
	// An unreadable ws.json only costs the plan line its detail about shared
	// copies; running the action reports the error.
	cfg, _ := wsconfig.Load(p.Config)
	pl := plan.Plan{Command: "dotfile.unlink"}
	for _, e := range rows {
		sys := e.Link.System
		pl.Actions = append(pl.Actions, plan.Action{
			ID:          "dotfile-unlink-" + sys,
			Description: unlinkDescription(dotfile.PlanUnlink(p.Workspace, cfg, e.Link)),
			Execute: func() error {
				return dotfile.Unlink(dotfile.UnlinkOptions{WorkspacePath: p.Workspace, ConfigPath: p.Config, SystemPath: sys})
			},
		})
	}
	c := confirm.New("Unlink "+format.Plural(len(rows), "dotfile"), pl)
	if nothingChecked {
		c = c.WithChecked(func(int, plan.Action) bool { return false })
	}
	m.modal = m.modal.Confirm(stepUnlink, c)
	return m, nil
}

// unlinkDescription is the plan line of one unlink, as the CLI words it.
func unlinkDescription(p dotfile.UnlinkPlan) string {
	switch p.Mode {
	case dotfile.ModeMissing:
		return fmt.Sprintf("Unregister %s (workspace copy missing; nothing to restore)", p.Link.System)
	case dotfile.ModeCopy:
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

// link is `l`: repair the broken and overwritten rows, the workspace copy
// winning (`ws dotfile link`). Sudo entries run in the terminal.
func (m Model) link(rows []Entry) (Model, tea.Cmd) {
	var bad []Entry
	for _, e := range rows {
		if e.Status != "" {
			bad = append(bad, e)
		}
	}
	if len(bad) == 0 {
		return m, nav.Notify(nav.LevelInfo, "Nothing to link: every shown dotfile is healthy.")
	}
	if len(sudoPaths(bad)) > 0 {
		return m, m.terminal(append([]string{"dotfile", "link"}, systemPaths(bad)...)...)
	}
	p := m.env.Paths
	pl := plan.Plan{Command: "dotfile.link"}
	for _, e := range bad {
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
	m.modal = m.modal.Confirm(stepLink, confirm.New("Link dotfiles", pl))
	return m, nil
}

// linkCommand is `:link`. With `<path> --to <ws-path>` it manages the path as a
// link to a file already in the workspace; with a path alone it repairs that
// entry; with nothing it is the key.
func (m Model) linkCommand(_ []Entry, args []string) (Model, tea.Cmd) {
	var paths []string
	to := ""
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--to":
			if i+1 >= len(args) {
				return m, nav.Notify(nav.LevelWarn, "--to needs a workspace path")
			}
			i++
			to = args[i]
		case strings.HasPrefix(a, "--to="):
			to = strings.TrimPrefix(a, "--to=")
		case strings.HasPrefix(a, "-"):
			return m, nav.Notify(nav.LevelWarn, "Unknown flag "+a+"; usage: link <path> --to <ws-path>")
		default:
			paths = append(paths, a)
		}
	}
	switch {
	case to != "" && len(paths) == 1:
		return m.openLinkTo(paths[0], to)
	case to != "":
		return m, nav.Notify(nav.LevelWarn, "usage: link <path> --to <ws-path>")
	case len(paths) == 0:
		return m.link(m.list.Rows(reslist.All))
	}
	var rows []Entry
	for _, raw := range paths {
		abs, err := wsconfig.ExpandUserPath(raw)
		if err != nil {
			return m, nav.Notify(nav.LevelWarn, err.Error())
		}
		i := slices.IndexFunc(m.list.Items(), func(e Entry) bool { return e.Link.System == abs })
		if i < 0 {
			return m, nav.Notify(nav.LevelWarn, "Not a managed dotfile: "+abs)
		}
		rows = append(rows, m.list.Items()[i])
	}
	return m.link(rows)
}

// openLinkTo mirrors `ws dotfile link <path> --to <ws-path>`.
func (m Model) openLinkTo(path, to string) (Model, tea.Cmd) {
	sys, err := wsconfig.ExpandUserPath(path)
	if err != nil {
		return m, nav.Notify(nav.LevelWarn, err.Error())
	}
	toAbs, err := wsconfig.ExpandUserPath(to)
	if err != nil {
		return m, nav.Notify(nav.LevelWarn, err.Error())
	}
	p := m.env.Paths
	opts := dotfile.LinkToOptions{WorkspacePath: p.Workspace, ConfigPath: p.Config, SystemPath: sys, To: toAbs}
	lp, err := dotfile.PlanLinkTo(opts)
	if err != nil {
		return m, nav.Notify(nav.LevelError, err.Error())
	}
	if lp.Sudo {
		return m, m.terminal("dotfile", "link", sys, "--to", toAbs)
	}
	desc := fmt.Sprintf("Link %s → %s (new entry)", m.env.Rel(lp.System), m.env.Rel(lp.Workspace))
	if lp.Backup != "" {
		desc = fmt.Sprintf("Link %s → %s (new entry; the live file moves to %s)", m.env.Rel(lp.System), m.env.Rel(lp.Workspace), m.env.Rel(lp.Backup))
	}
	pl := plan.Plan{Command: "dotfile.link", Actions: []plan.Action{{
		ID:          "dotfile-link-" + lp.System,
		Description: desc,
		Execute: func() error {
			_, err := dotfile.LinkTo(opts)
			return err
		},
	}}}
	m.modal = m.modal.Confirm(stepLinkTo, confirm.New("Link "+m.env.Rel(lp.System), pl))
	return m, nil
}

// ── edit ───────────────────────────────────────────────────────────

// edit is `e`: the workspace copy in your editor. A terminal editor takes the
// terminal; a GUI editor is launched.
func (m Model) edit(rows []Entry) (Model, tea.Cmd) {
	e := rows[0]
	if _, err := os.Stat(e.WorkspacePath); err != nil {
		return m, nav.Notify(nav.LevelError, "Workspace copy "+e.WorkspacePath+" is missing")
	}
	cmds := []tea.Cmd{handover.Editor(m.env.Config.Editor, e.WorkspacePath, handoverEditor)}
	if e.Status == dotfile.StatusOverwritten {
		cmds = append(cmds, nav.Notify(nav.LevelWarn, e.Link.System+" is a separate live file: edits reach it only after "+m.keys.Link.Help().Key+" "+m.keys.Link.Help().Desc))
	}
	return m, tea.Batch(cmds...)
}

// ── backup ─────────────────────────────────────────────────────────

// backupPush is `b` and `:backup push`: commit pending changes and push,
// mirroring `ws dotfile backup push`.
func (m Model) backupPush(_ []Entry) (Model, tea.Cmd) {
	p := m.env.Paths
	cfg, err := wsconfig.Load(p.Config)
	if err != nil {
		return m, nav.Notify(nav.LevelError, err.Error())
	}
	if cfg.Dotfile.Git.RemoteURL == "" {
		return m, nav.Notify(nav.LevelWarn, "Backup is not enabled. Run :backup enable")
	}
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
	m.modal = m.modal.Confirm(stepBackupPush, confirm.New("Back up dotfiles", pl))
	return m, nil
}

func (m Model) backupEnable(_ []Entry, _ []string) (Model, tea.Cmd) {
	return m, m.terminal("dotfile", "backup", "enable")
}

func (m Model) backupStatus() (Model, tea.Cmd) {
	m.level = levelBackup
	m.view.SetContent(theme.Muted.Render("Loading…"))
	m.view.GotoTop()
	return m, loadBackupView(m.env)
}

// backupDisable mirrors `ws dotfile backup disable`: drop the remote and the
// cron job; the repository and its history stay.
func (m Model) backupDisable() (Model, tea.Cmd) {
	p := m.env.Paths
	cfg, err := wsconfig.Load(p.Config)
	if err != nil {
		return m, nav.Notify(nav.LevelError, err.Error())
	}
	repoPath := dotfile.RepoPath(p.Workspace)
	remote := cfg.Dotfile.Git.RemoteURL
	if remote == "" && backup.IsRepo(repoPath) {
		remote = backup.RemoteURL(repoPath)
	}
	pl := plan.Plan{Command: "dotfile.backup.disable"}
	if remote != "" {
		pl.Actions = append(pl.Actions, plan.Action{
			ID:          "remove-remote",
			Description: "Remove the backup remote " + remote,
			Execute: func() error {
				if backup.IsRepo(repoPath) {
					if err := backup.RemoveRemote(repoPath); err != nil {
						return err
					}
				}
				cfg, err := wsconfig.Load(p.Config)
				if err != nil {
					return err
				}
				cfg.Dotfile.Git.RemoteURL = ""
				return wsconfig.Save(p.Config, cfg)
			},
		})
	}
	jobs := []string{"dotfile-backup", "dotfile-sync"}
	if slices.ContainsFunc(jobs, func(j string) bool { has, err := cron.HasJob(j); return err == nil && has }) {
		pl.Actions = append(pl.Actions, plan.Action{
			ID:          "remove-cron-job",
			Description: "Remove the dotfile-backup cron job",
			Execute: func() error {
				for _, j := range jobs {
					if err := cron.RemoveJob(j); err != nil {
						return err
					}
				}
				return nil
			},
		})
	}
	if len(pl.Actions) == 0 {
		return m, nav.Notify(nav.LevelInfo, "Backup is not enabled.")
	}
	m.modal = m.modal.Confirm(stepBackupDisable, confirm.New("Disable dotfile backup", pl).
		WithNote("The repository and its history stay in "+m.env.Rel(repoPath)+"."))
	return m, nil
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
		out[i] = e.Link.System
	}
	return out
}

// sudoPaths are the system paths of the rows that need root.
func sudoPaths(rows []Entry) []string {
	var out []string
	for _, e := range rows {
		if e.Link.Sudo {
			out = append(out, e.Link.System)
		}
	}
	return out
}

func (m Model) dialogResult(st step, msg tea.Msg) (nav.Component, tea.Cmd) {
	if st == stepAddPath {
		r := msg.(prompt.ResultMsg)
		if r.Cancelled {
			return m, nil
		}
		m.preparing = true
		return m, prepareAdd(m.env, addRequest{paths: []string{r.Value}})
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
	// Auto-commit/push after a successful add, unlink or link --to, as the CLI does.
	if (st == stepAdd || st == stepUnlink || st == stepLinkTo) && done.Result.ExecutedCount() > 0 && !done.Result.HasFailures() {
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
	case stepUnlink:
		msg = "dotfile unlink"
	case stepLinkTo:
		msg = "dotfile link"
	}
	return func() tea.Msg {
		res, enabled := dotfile.AutoSync(p.Workspace, p.Config, msg)
		switch {
		case !enabled:
			return nil
		case res.Error != "":
			return nav.NoticeMsg{Level: nav.LevelWarn, Text: "dotfile backup: " + res.Error}
		case res.Pushed:
			return nav.NoticeMsg{Level: nav.LevelSuccess, Text: "dotfile backup: committed and pushed"}
		case res.Committed:
			return nav.NoticeMsg{Level: nav.LevelSuccess, Text: "dotfile backup: committed"}
		}
		return nil
	}
}

// View implements nav.Component.
func (m Model) View() string {
	switch {
	case m.modal.Open():
		return m.modal.View()
	case m.level == levelBackup:
		return m.view.View()
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
	case m.level == levelBackup:
		return m.detailKeys
	}
	return m.list.Help(m.keys)
}

var _ nav.Commander = Model{}
