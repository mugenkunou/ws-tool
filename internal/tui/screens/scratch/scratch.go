// Package scratch is the scratch-directory screen (spec §6.5): list, new, open
// in the editor, tag and delete, plus the `:add` command that moves a path
// into scratch. Tag and delete act on the marked directories, else the cursor
// one.
package scratch

import (
	"fmt"
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/mugenkunou/ws-tool/internal/plan"
	wsscratch "github.com/mugenkunou/ws-tool/internal/scratch"
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

// LoadedMsg carries the scratch directory listing (newest first) and the
// workspace tag collection. Other components (the dashboard) may observe it.
type LoadedMsg struct {
	Entries []wsscratch.Entry
	Tags    []string
	Err     error
}

// TotalBytes sums the size of every scratch directory.
func (m LoadedMsg) TotalBytes() int64 {
	var n int64
	for _, e := range m.Entries {
		n += e.SizeBytes
	}
	return n
}

// createdMsg reports the path of a newly created scratch dir so it can be
// opened in the editor.
type createdMsg struct{ path string }

type step int

const (
	stepNewName step = iota
	stepNew
	stepTagInput
	stepTag
	stepDelete
	stepAdd
	stepAddPath
	stepMenu // the `.` menu
)

type handoverTag int

const handoverEditor handoverTag = iota

// Model is the scratch screen state.
type Model struct {
	env   env.Env
	keys  keys.ScratchMap
	list  reslist.Model[wsscratch.Entry]
	modal modal.Model

	tags    []string // every tag used before: what the tag prompt completes
	newPath *string  // written by the create action, read after the plan finishes
	tagging []wsscratch.Entry
	width   int
	height  int
}

// New creates the screen.
func New(e env.Env) Model {
	k := keys.Scratch()
	return Model{
		env:  e,
		keys: k,
		list: reslist.New(wsscratch.Schema(e.View()), []reslist.Col[wsscratch.Entry]{
			{Key: "name", Min: 16, Weight: 3},
			{Key: "age", Min: 5},
			{Key: "size_bytes", Min: 9},
			{Key: "items", Min: 5},
			{Key: "tags", Min: 8, Weight: 2},
		}, theme.Icon(theme.IconSprout)+"No scratch directories yet. Press "+k.New.Help().Key+" to start one.").Declare(reslist.Decls(actions)),
	}
}

// Load lists scratch directories, newest first, and the tag collection.
func Load(e env.Env) tea.Cmd {
	return func() tea.Msg {
		entries, err := wsscratch.List(wsscratch.ListOptions{RootDir: e.ScratchDir, SortBy: "age"})
		if err != nil {
			return LoadedMsg{Err: err}
		}
		tc, _ := wsscratch.LoadTags(e.Paths.Config)
		return LoadedMsg{Entries: entries, Tags: tc.Tags}
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

func (m Model) reload() (Model, tea.Cmd) {
	m.list = m.list.SetLoading()
	return m, Load(m.env)
}

// Update implements nav.Component.
func (m Model) Update(msg tea.Msg) (nav.Component, tea.Cmd) {
	if st, ok := m.modal.Match(msg); ok {
		m.modal = m.modal.Close()
		if st == stepMenu {
			next, cmd := reslist.MenuPick(m, m.list, commands, msg)
			return next, cmd
		}
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
		return m.reload()
	case nav.MenuMsg:
		if m.modal.Open() {
			return m, nil
		}
		m.modal = m.modal.Choose(stepMenu, reslist.MenuChooser("Scratch actions", commands))
		return m, nil
	case LoadedMsg:
		if msg.Err != nil {
			m.list = m.list.SetError(msg.Err)
			return m, nil
		}
		m.tags = msg.Tags
		m.list = m.list.SetItems(msg.Entries)
		return m, nil
	case createdMsg:
		return m, handover.Editor(m.env.Config.Editor, msg.path, handoverEditor)
	case handover.DoneMsg:
		if _, ok := msg.Tag.(handoverTag); ok && msg.Err != nil {
			return m, nav.Notify(nav.LevelWarn, "Editor launch skipped: "+msg.Err.Error())
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
	return m.updateList(k)
}

func (m Model) names() []string {
	entries := m.list.Items()
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Name
	}
	return out
}

// actions is every scratch key and the rows it acts on (spec §6.5).
var actions = func() []reslist.Action[Model, wsscratch.Entry] {
	k := keys.Scratch()
	return []reslist.Action[Model, wsscratch.Entry]{
		{Key: k.New, Scope: reslist.Global, Do: Model.promptNew},
		{Key: k.Open, Scope: reslist.Cursor, Do: Model.open},
		{Key: k.Tag, Scope: reslist.Selected, Do: Model.promptTag},
		{Key: k.Delete, Scope: reslist.Selected, Do: Model.delete},
	}
}()

// commands is the `.` menu: the CLI's words after `scratch`.
var commands = []reslist.Command[Model, wsscratch.Entry]{
	{Name: "add", Menu: "a", Args: "<path…>", Scope: reslist.Global,
		Help: "move files or directories into scratch", Do: Model.addCommand},
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

// ── new, open ──────────────────────────────────────────────────────

func (m Model) promptNew(_ []wsscratch.Entry) (Model, tea.Cmd) {
	var cmd tea.Cmd
	m.modal, cmd = m.modal.Prompt(stepNewName, prompt.New("New scratch directory",
		prompt.WithHint("Existing names are listed so you can pick a distinctive one."),
		prompt.WithSuggestions(m.names())))
	return m, cmd
}

func (m Model) open(rows []wsscratch.Entry) (Model, tea.Cmd) {
	return m, handover.Editor(m.env.Config.Editor, rows[0].Path, handoverEditor)
}

func (m Model) openNew(name string) Model {
	root := m.env.ScratchDir
	suffix := "auto" // as the CLI: a .YYYY-MM suffix unless --no-date
	path := new(string)
	p := plan.Plan{Command: "scratch.new", Actions: []plan.Action{{
		ID:          "scratch-new",
		Description: "Create scratch directory " + name + theme.Muted.Render("  in "+m.env.Rel(root)),
		Execute: func() error {
			res, err := wsscratch.New(wsscratch.NewOptions{RootDir: root, Name: name, SuffixMode: suffix})
			*path = res.Path
			return err
		},
	}}}
	m.newPath = path
	m.modal = m.modal.Confirm(stepNew, confirm.New("New scratch directory", p))
	return m
}

// ── tag ────────────────────────────────────────────────────────────

// promptTag asks for tags, comma-separated: `-tag` removes one, and tab
// completes tags used before.
func (m Model) promptTag(targets []wsscratch.Entry) (Model, tea.Cmd) {
	m.tagging = targets
	hint := "Comma-separated; -tag removes one."
	title := "Tag " + format.Plural(len(targets), "directory")
	have := map[string]bool{}
	if len(targets) == 1 {
		e := targets[0]
		title = "Tag " + e.Name
		if len(e.Tags) > 0 {
			hint += " Current: " + strings.Join(e.Tags, ", ")
		}
		for _, t := range e.Tags {
			have[t] = true
		}
	}
	var fresh []string
	for _, t := range m.tags {
		if !have[t] {
			fresh = append(fresh, t)
		}
	}
	var cmd tea.Cmd
	m.modal, cmd = m.modal.Prompt(stepTagInput, prompt.New(title,
		prompt.WithHint(hint), prompt.WithSuggestions(fresh), prompt.WithSeparator(",")))
	return m, cmd
}

// splitTagInput separates what was typed into tags to add and tags to remove
// (`-tag`).
func splitTagInput(raw string) (add, remove []string) {
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if rest, ok := strings.CutPrefix(part, "-"); ok {
			remove = append(remove, rest)
		} else {
			add = append(add, part)
		}
	}
	return add, remove
}

// openTag adds and removes the typed tags on each target, one action per
// directory that changes, as `ws scratch tag` words it.
func (m Model) openTag(targets []wsscratch.Entry, raw string) (Model, tea.Cmd) {
	add, remove := splitTagInput(raw)
	p := plan.Plan{Command: "scratch.tag"}
	for _, e := range targets {
		newTags, added, removed := wsscratch.ApplyTags(e.Tags, add, remove)
		if len(added) == 0 && len(removed) == 0 {
			continue
		}
		var change []string
		for _, t := range added {
			change = append(change, "+"+t)
		}
		for _, t := range removed {
			change = append(change, "−"+t)
		}
		p.Actions = append(p.Actions, m.tagAction(e, newTags, added, fmt.Sprintf("Tag %s: %s", e.Name, strings.Join(change, " "))))
	}
	if len(p.Actions) == 0 {
		return m, nav.Notify(nav.LevelInfo, "No change.")
	}
	title := "Tag " + format.Plural(len(targets), "directory")
	if len(targets) == 1 {
		title = "Tag " + targets[0].Name
	}
	m.modal = m.modal.Confirm(stepTag, confirm.New(title, p))
	return m, nil
}

// tagAction writes a directory's tags and keeps every added tag in the
// workspace tag collection, the vocabulary completion offers.
func (m Model) tagAction(e wsscratch.Entry, tags, added []string, desc string) plan.Action {
	configPath := m.env.Paths.Config
	return plan.Action{
		ID:          "scratch-tag-" + e.Name,
		Description: desc,
		Execute: func() error {
			meta, err := wsscratch.LoadMeta(e.Path)
			if err != nil {
				return err
			}
			meta.Tags = tags
			if err := wsscratch.SaveMeta(e.Path, meta); err != nil {
				return err
			}
			tc, _ := wsscratch.LoadTags(configPath)
			if wsscratch.MergeTags(&tc, added) {
				return wsscratch.SaveTags(configPath, tc)
			}
			return nil
		},
	}
}

// ── delete ─────────────────────────────────────────────────────────

func (m Model) delete(targets []wsscratch.Entry) (Model, tea.Cmd) {
	p := plan.Plan{Command: "scratch.rm"}
	for _, e := range targets {
		entry := e
		p.Actions = append(p.Actions, plan.Action{
			ID:          "scratch-rm-" + entry.Path,
			Description: fmt.Sprintf("Delete %s (%s)", entry.Path, style.HumanBytes(entry.SizeBytes)) + theme.Muted.Render("  to the trash"),
			// The directory itself is deleted, never a name: a name that is a
			// substring of another must not take that one with it.
			Execute: func() error { return wsscratch.Remove(entry) },
		})
	}
	m.modal = m.modal.Confirm(stepDelete, confirm.New("Delete "+format.Plural(len(targets), "scratch directory"), p))
	return m, nil
}

// ── add ────────────────────────────────────────────────────────────

// addCommand is `scratch add <path…>`: move paths into scratch. Every path is
// checked before anything moves. With no paths it asks for one.
func (m Model) addCommand(_ []wsscratch.Entry, args []string) (Model, tea.Cmd) {
	if len(args) == 0 {
		var cmd tea.Cmd
		m.modal, cmd = m.modal.Prompt(stepAddPath, prompt.New("Move into scratch",
			prompt.WithHint("A file or folder on this machine; it moves into scratch and a symlink replaces it."),
			prompt.WithValue("~/"),
			prompt.WithCompleter(complete.Paths)))
		return m, cmd
	}
	var paths []string
	for _, raw := range args {
		abs, err := wsconfig.ExpandUserPath(raw)
		if err != nil {
			return m, nav.Notify(nav.LevelWarn, fmt.Sprintf("invalid path %q: %v", raw, err))
		}
		paths = append(paths, filepath.Clean(abs))
	}
	plans, err := wsscratch.PlanAdd(wsscratch.AddOptions{
		RootDir: m.env.ScratchDir, WorkspacePath: m.env.Paths.Workspace, ConfigPath: m.env.Paths.Config,
		Links: m.env.Config.Symlinks, Paths: paths,
	})
	if err != nil {
		return m, nav.Notify(nav.LevelError, err.Error())
	}
	p := plan.Plan{Command: "scratch.add"}
	for _, ap := range plans {
		ap := ap
		p.Actions = append(p.Actions, plan.Action{
			ID:          "scratch-add-" + ap.Source,
			Description: fmt.Sprintf("Move %s → %s", ap.Source, ap.Dest),
			Execute:     func() error { return wsscratch.Add(ap) },
		})
	}
	m.modal = m.modal.Confirm(stepAdd, confirm.New("Add to scratch", p))
	return m, nil
}

func (m Model) dialogResult(st step, msg tea.Msg) (nav.Component, tea.Cmd) {
	switch st {
	case stepAddPath:
		r := msg.(prompt.ResultMsg)
		if v := strings.TrimSpace(r.Value); !r.Cancelled && v != "" && v != "~/" {
			return m.addCommand(nil, []string{v})
		}
		return m, nil
	case stepNewName:
		r := msg.(prompt.ResultMsg)
		if r.Cancelled {
			return m, nil
		}
		return m.openNew(r.Value), nil
	case stepTagInput:
		r := msg.(prompt.ResultMsg)
		if r.Cancelled {
			return m, nil
		}
		return m.openTag(m.tagging, r.Value)
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
	if st == stepNew && done.Result.WasExecuted("scratch-new") && m.newPath != nil && *m.newPath != "" {
		path := *m.newPath
		cmds = append(cmds, func() tea.Msg { return createdMsg{path: path} })
	}
	m, load := m.reload()
	return m, tea.Batch(append(cmds, load)...)
}

// View implements nav.Component.
func (m Model) View() string {
	if m.modal.Open() {
		return m.modal.View()
	}
	return m.list.View()
}

// KeyMap implements nav.Component.
func (m Model) KeyMap() help.KeyMap {
	if m.modal.Open() {
		return m.modal.KeyMap()
	}
	return m.list.Help(m.keys)
}

var _ nav.Commander = Model{}
