// Package scratch is the scratch-directory screen: list, create, open in the
// editor, tag (manual and auto), search, delete, and prune.
//
// Navigation levels: list → search results. esc in results returns to the
// list; esc in the list asks the app to go up.
package scratch

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/mugenkunou/ws-tool/internal/plan"
	wsscratch "github.com/mugenkunou/ws-tool/internal/scratch"
	"github.com/mugenkunou/ws-tool/internal/style"
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

type searchMsg struct {
	query   string
	results []wsscratch.SearchResult
	err     error
}

type autoTagMsg struct {
	entry wsscratch.Entry
	tags  []string
	err   error
}

// createdMsg reports the path of a newly created scratch dir so it can be
// opened in the editor.
type createdMsg struct{ path string }

type level int

const (
	levelList level = iota
	levelSearch
)

type step int

const (
	stepNewName step = iota
	stepNew
	stepTagInput
	stepTag
	stepAutoTag
	stepSearchQuery
	stepDelete
	stepPrune
)

type handoverTag int

const handoverEditor handoverTag = iota

// Model is the scratch screen state.
type Model struct {
	env     env.Env
	keys    keys.ScratchMap
	list    listview.Model
	results listview.Model
	level   level
	modal   modal.Model

	entries  []wsscratch.Entry
	tags     []string
	found    []wsscratch.SearchResult
	query    string
	newPath  *string // written by the create action, read after the plan finishes
	tagEntry wsscratch.Entry
	width    int
	height   int
}

// New creates the screen.
func New(e env.Env) Model {
	return Model{
		env:  e,
		keys: keys.Scratch(),
		list: listview.New([]layout.Col{
			{Title: "Name", Min: 16, Weight: 3},
			{Title: "Age", Min: 5},
			{Title: "Size", Min: 9},
			{Title: "Items", Min: 5},
			{Title: "Tags", Min: 8, Weight: 2},
		}, "No scratch directories. Press n to create one."),
		results: listview.New([]layout.Col{
			{Title: "Name", Min: 16, Weight: 2},
			{Title: "Match", Min: 7},
			{Title: "Tags", Min: 8, Weight: 1},
			{Title: "Snippet", Min: 10, Weight: 3},
		}, "No matches."),
	}
}

// Load lists scratch directories, newest first, and the tag collection.
func Load(e env.Env) tea.Cmd {
	return func() tea.Msg {
		entries, err := wsscratch.List(wsscratch.ListOptions{RootDir: e.ScratchDir, SortBy: "age"})
		if err != nil {
			return LoadedMsg{Err: err}
		}
		tc, _ := wsscratch.LoadTags(filepath.Join(e.Paths.Workspace, "ws"))
		return LoadedMsg{Entries: entries, Tags: tc.Tags}
	}
}

func search(e env.Env, query string) tea.Cmd {
	return func() tea.Msg {
		res, err := wsscratch.Search(wsscratch.SearchOptions{RootDir: e.ScratchDir, Query: query})
		return searchMsg{query: query, results: res, err: err}
	}
}

func suggestTags(entry wsscratch.Entry) tea.Cmd {
	return func() tea.Msg {
		suggested, err := wsscratch.AutoTag(entry.Path)
		if err != nil {
			return autoTagMsg{entry: entry, err: err}
		}
		have := make(map[string]bool, len(entry.Tags))
		for _, t := range entry.Tags {
			have[t] = true
		}
		var fresh []string
		for _, t := range suggested {
			if !have[t] {
				fresh = append(fresh, t)
			}
		}
		return autoTagMsg{entry: entry, tags: fresh}
	}
}

// Init starts the first load.
func (m Model) Init() tea.Cmd { return Load(m.env) }

// CapturesKeys implements nav.Component.
func (m Model) CapturesKeys() bool { return m.modal.Open() }

func (m Model) reload() (Model, tea.Cmd) {
	m.list = m.list.SetLoading()
	return m, Load(m.env)
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
		m.list = m.list.SetSize(msg.Width, msg.Height)
		m.results = m.results.SetSize(msg.Width, max(msg.Height-1, 0))
		m.modal = m.modal.SetSize(msg.Width, msg.Height)
		return m, nil
	case nav.RefreshMsg:
		return m.reload()
	case LoadedMsg:
		if msg.Err != nil {
			m.list = m.list.SetError(msg.Err)
			return m, nil
		}
		m.entries, m.tags = msg.Entries, msg.Tags
		m.list = m.list.SetRows(rows(msg.Entries))
		return m, nil
	case searchMsg:
		if msg.err != nil {
			m.results = m.results.SetError(msg.err)
		} else {
			m.found = msg.results
			m.results = m.results.SetRows(resultRows(msg.results))
		}
		return m, nil
	case autoTagMsg:
		if msg.err != nil {
			return m, nav.Notify(nav.LevelError, "Auto-tag: "+msg.err.Error())
		}
		if len(msg.tags) == 0 {
			return m, nav.Notify(nav.LevelInfo, "No new tag suggestions for "+msg.entry.Name)
		}
		return m.openAutoTag(msg), nil
	case createdMsg:
		return m, handover.Editor(m.env.Config.Scratch.EditorCmd, msg.path, handoverEditor)
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
	if m.level == levelSearch {
		return m.updateSearch(k)
	}
	return m.updateList(k)
}

func (m Model) selected() (wsscratch.Entry, bool) {
	i := m.list.Cursor()
	if i < 0 || i >= len(m.entries) {
		return wsscratch.Entry{}, false
	}
	return m.entries[i], true
}

func (m Model) names() []string {
	out := make([]string, len(m.entries))
	for i, e := range m.entries {
		out[i] = e.Name
	}
	return out
}

func (m Model) updateList(k tea.KeyPressMsg) (nav.Component, tea.Cmd) {
	switch {
	case key.Matches(k, m.keys.Back):
		return m, nav.Up
	case key.Matches(k, m.keys.Refresh):
		return m.reload()
	case key.Matches(k, m.keys.New):
		var cmd tea.Cmd
		m.modal, cmd = m.modal.Prompt(stepNewName, prompt.New("New scratch directory",
			prompt.WithHint("Existing names are listed so you can pick a distinctive one."),
			prompt.WithSuggestions(m.names())))
		return m, cmd
	case key.Matches(k, m.keys.Open):
		if e, ok := m.selected(); ok {
			return m, handover.Editor(m.env.Config.Scratch.EditorCmd, e.Path, handoverEditor)
		}
	case key.Matches(k, m.keys.Tag):
		if e, ok := m.selected(); ok {
			m.tagEntry = e
			hint := "Comma-separated."
			if len(e.Tags) > 0 {
				hint += " Current: " + strings.Join(e.Tags, ", ")
			}
			have := make(map[string]bool, len(e.Tags))
			for _, t := range e.Tags {
				have[t] = true
			}
			var fresh []string
			for _, t := range m.tags {
				if !have[t] {
					fresh = append(fresh, t)
				}
			}
			var cmd tea.Cmd
			m.modal, cmd = m.modal.Prompt(stepTagInput, prompt.New("Tag "+e.Name,
				prompt.WithHint(hint), prompt.WithSuggestions(fresh), prompt.WithSeparator(",")))
			return m, cmd
		}
	case key.Matches(k, m.keys.AutoTag):
		if e, ok := m.selected(); ok {
			return m, suggestTags(e)
		}
	case key.Matches(k, m.keys.Search):
		var cmd tea.Cmd
		m.modal, cmd = m.modal.Prompt(stepSearchQuery, prompt.New("Search scratch by tag, name, or content",
			prompt.WithValue(m.query), prompt.WithSuggestions(m.tags)))
		return m, cmd
	case key.Matches(k, m.keys.Delete):
		if e, ok := m.selected(); ok {
			return m.openDelete(e), nil
		}
	case key.Matches(k, m.keys.Prune):
		return m.openPrune(), nil
	default:
		var cmd tea.Cmd
		m.list, cmd = m.list.Update(k)
		return m, cmd
	}
	return m, nil
}

func (m Model) updateSearch(k tea.KeyPressMsg) (nav.Component, tea.Cmd) {
	switch {
	case key.Matches(k, m.keys.Back):
		m.level = levelList
		return m, nil
	case key.Matches(k, m.keys.Open):
		if i := m.results.Cursor(); i >= 0 && i < len(m.found) {
			return m, handover.Editor(m.env.Config.Scratch.EditorCmd, m.found[i].Entry.Path, handoverEditor)
		}
		return m, nil
	case key.Matches(k, m.keys.Search):
		var cmd tea.Cmd
		m.modal, cmd = m.modal.Prompt(stepSearchQuery, prompt.New("Search scratch by tag, name, or content",
			prompt.WithValue(m.query), prompt.WithSuggestions(m.tags)))
		return m, cmd
	}
	var cmd tea.Cmd
	m.results, cmd = m.results.Update(k)
	return m, cmd
}

func (m Model) dialogResult(st step, msg tea.Msg) (nav.Component, tea.Cmd) {
	switch st {
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
		return m.openTag(m.tagEntry, strings.Split(r.Value, ",")), nil
	case stepSearchQuery:
		r := msg.(prompt.ResultMsg)
		if r.Cancelled {
			return m, nil
		}
		m.query = r.Value
		m.level = levelSearch
		m.results = m.results.SetLoading()
		return m, search(m.env, r.Value)
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
	if st == stepNew && done.Result.WasExecuted("scratch-new") && m.newPath != nil && *m.newPath != "" {
		path := *m.newPath
		cmds = append(cmds, func() tea.Msg { return createdMsg{path: path} })
	}
	m, load := m.reload()
	return m, tea.Batch(append(cmds, load)...)
}

func (m Model) openNew(name string) Model {
	root, suffix := m.env.ScratchDir, m.env.Config.Scratch.NameSuffix
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

func (m Model) openTag(e wsscratch.Entry, raw []string) Model {
	have := make(map[string]bool, len(e.Tags))
	for _, t := range e.Tags {
		have[t] = true
	}
	var add []string
	for _, t := range raw {
		t = wsscratch.NormalizeTag(t)
		if t != "" && !have[t] {
			have[t] = true
			add = append(add, t)
		}
	}
	p := plan.Plan{Command: "scratch.tag"}
	if len(add) > 0 {
		p.Actions = []plan.Action{m.tagAction(e, add, "Add tags "+strings.Join(add, ", ")+" to "+e.Name)}
	}
	m.modal = m.modal.Confirm(stepTag, confirm.New("Tag "+e.Name, p))
	return m
}

func (m Model) openAutoTag(msg autoTagMsg) Model {
	p := plan.Plan{Command: "scratch.tag.auto"}
	for _, t := range msg.tags {
		p.Actions = append(p.Actions, m.tagAction(msg.entry, []string{t}, fmt.Sprintf("Add tag %q to %s", t, msg.entry.Name)))
	}
	m.modal = m.modal.Confirm(stepAutoTag, confirm.New("Suggested tags for "+msg.entry.Name, p))
	return m
}

// tagAction adds tags to a scratch dir's metadata and the workspace tag
// collection. Each action reloads the metadata, so several can run in turn.
func (m Model) tagAction(e wsscratch.Entry, tags []string, desc string) plan.Action {
	wsDir := filepath.Join(m.env.Paths.Workspace, "ws")
	return plan.Action{
		ID:          "tag-" + e.Name + "-" + strings.Join(tags, ","),
		Description: desc,
		Execute: func() error {
			meta, err := wsscratch.LoadMeta(e.Path)
			if err != nil {
				return err
			}
			meta.Tags = append(meta.Tags, tags...)
			if err := wsscratch.SaveMeta(e.Path, meta); err != nil {
				return err
			}
			tc, _ := wsscratch.LoadTags(wsDir)
			if wsscratch.MergeTags(&tc, meta.Tags) {
				return wsscratch.SaveTags(wsDir, tc)
			}
			return nil
		},
	}
}

func (m Model) openDelete(e wsscratch.Entry) Model {
	root, name := m.env.ScratchDir, e.Name
	p := plan.Plan{Command: "scratch.rm", Actions: []plan.Action{{
		ID:          "scratch-rm",
		Description: fmt.Sprintf("Delete %s (%s)", e.Path, style.HumanBytes(e.SizeBytes)),
		Execute: func() error {
			_, err := wsscratch.Delete(wsscratch.DeleteOptions{RootDir: root, Name: name})
			return err
		},
	}}}
	m.modal = m.modal.Confirm(stepDelete, confirm.New("Delete scratch directory", p))
	return m
}

// openPrune lists every scratch dir; those older than scratch.prune_after_days
// are pre-checked (the CLI's default), the rest can be ticked (its --all).
func (m Model) openPrune() Model {
	days := m.env.Config.Scratch.PruneAfterDays
	threshold := time.Duration(days) * 24 * time.Hour
	p := plan.Plan{Command: "scratch.prune"}
	var old []bool
	for _, e := range m.entries {
		path := e.Path
		p.Actions = append(p.Actions, plan.Action{
			ID:          "prune-" + e.Name,
			Description: fmt.Sprintf("Remove %s  %s, %s old", e.Name, style.HumanBytes(e.SizeBytes), format.Age(e.Age)),
			Execute:     func() error { return os.RemoveAll(path) },
		})
		old = append(old, days > 0 && e.Age >= threshold)
	}
	note := "Nothing is older than the threshold; tick entries to remove them anyway."
	n := 0
	for _, o := range old {
		if o {
			n++
		}
	}
	if days <= 0 {
		note = "scratch.prune_after_days is 0; tick entries to remove."
	} else if n > 0 {
		note = fmt.Sprintf("%s older than %d days pre-selected.", format.Plural(n, "directory"), days)
	}
	c := confirm.New("Prune scratch directories", p).WithNote(note).
		WithChecked(func(i int, _ plan.Action) bool { return old[i] })
	m.modal = m.modal.Confirm(stepPrune, c)
	return m
}

func rows(entries []wsscratch.Entry) []table.Row {
	out := make([]table.Row, 0, len(entries))
	for _, e := range entries {
		out = append(out, table.Row{
			e.Name,
			format.Age(e.Age),
			style.HumanBytes(e.SizeBytes),
			strconv.Itoa(e.Items),
			strings.Join(e.Tags, ", "),
		})
	}
	return out
}

func resultRows(results []wsscratch.SearchResult) []table.Row {
	out := make([]table.Row, 0, len(results))
	for _, r := range results {
		out = append(out, table.Row{
			r.Entry.Name,
			r.MatchOn,
			strings.Join(r.Entry.Tags, ", "),
			strings.Join(strings.Fields(r.Snippet), " "),
		})
	}
	return out
}

// View implements nav.Component.
func (m Model) View() string {
	switch {
	case m.modal.Open():
		return m.modal.View()
	case m.level == levelSearch:
		head := lipgloss.NewStyle().MaxWidth(m.width).Render(
			theme.Muted.Render("Results for ") + theme.Bold.Render(m.query) + theme.Muted.Render("  (enter opens · / new search · esc back)"))
		return lipgloss.JoinVertical(lipgloss.Left, head, m.results.View())
	}
	return m.list.View()
}

// KeyMap implements nav.Component.
func (m Model) KeyMap() help.KeyMap {
	if m.modal.Open() {
		return m.modal.KeyMap()
	}
	return m.keys
}
