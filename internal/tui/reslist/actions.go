package reslist

import (
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/mugenkunou/ws-tool/internal/tui/nav"
)

// Scope says which rows an action acts on. Every screen action declares
// one, and Run hands the action exactly those rows, so no action can ignore
// the marks or the filter.
type Scope int

const (
	// Global actions act on no row: add, new, setup, git push.
	Global Scope = iota
	// Cursor actions act on the cursor row only: open, view, or a prompt
	// about one row.
	Cursor
	// Selected actions act on the marked rows, else the cursor row: remove,
	// delete, tag.
	Selected
	// All actions act on the marked rows, else every row shown (filtered):
	// fleet actions such as fetch, pull, run, empty, purge.
	All
	// Marked actions act on the marked rows, and with no marks on none: the
	// action still runs, with no rows (spec §6.5 Dotfiles `a`).
	Marked
)

// Action is one screen action: its key, its scope, and what it does with
// the rows its scope covers. M is the screen's model.
type Action[M, T any] struct {
	Key   key.Binding
	Scope Scope
	Do    func(m M, rows []T) (M, tea.Cmd)
}

// Decl is an action's key and scope, without its behaviour.
type Decl struct {
	Key   key.Binding
	Scope Scope
}

// Decls lists the key and scope of each action.
func Decls[M, T any](actions []Action[M, T]) []Decl {
	out := make([]Decl, len(actions))
	for i, a := range actions {
		out[i] = Decl{Key: a.Key, Scope: a.Scope}
	}
	return out
}

// Declare records the screen's actions, so the status line can say which
// keys act on the marked or filtered rows, and tests can check every key
// has a scope.
func (m Model[T]) Declare(decls []Decl) Model[T] {
	m.decls = decls
	return m
}

// Declared returns the actions recorded by Declare.
func (m Model[T]) Declared() []Decl { return m.decls }

// Rows returns the rows scope covers.
func (m Model[T]) Rows(s Scope) []T {
	switch s {
	case Cursor:
		if it, ok := m.Current(); ok {
			return []T{it}
		}
	case Selected:
		if marked := m.marked(); len(marked) > 0 {
			return marked
		}
		if it, ok := m.Current(); ok {
			return []T{it}
		}
	case All:
		if marked := m.marked(); len(marked) > 0 {
			return marked
		}
		return m.shown
	case Marked:
		return m.marked()
	}
	return nil
}

// Run handles k when it is one of actions' keys: it calls the action's Do
// with the rows its scope covers and reports true. An action that needs
// rows does nothing when there are none.
func Run[M, T any](m M, l Model[T], actions []Action[M, T], k tea.KeyPressMsg) (M, tea.Cmd, bool) {
	for _, a := range actions {
		if !key.Matches(k, a.Key) {
			continue
		}
		rows := l.Rows(a.Scope)
		if a.Scope != Global && a.Scope != Marked && len(rows) == 0 {
			return m, nil, true
		}
		next, cmd := a.Do(m, rows)
		return next, cmd, true
	}
	return m, nil, false
}

// scopedKeys returns each declared action with one of the given scopes as
// "key label" (`ctrl+d unlink`), never as a bare letter.
func (m Model[T]) scopedKeys(scopes ...Scope) []string {
	var out []string
	for _, d := range m.decls {
		for _, s := range scopes {
			if d.Scope == s {
				h := d.Key.Help()
				out = append(out, strings.TrimSpace(h.Key+" "+h.Desc))
			}
		}
	}
	return out
}

// Command is a `:` command of a screen (spec §6.6): the CLI's words after the
// noun. A key and a command for one verb share a function: the Do of the Action
// and of the Command are the same function, and Scope picks the rows as it does
// for a key.
type Command[M, T any] struct {
	Name  string // the words after the noun, e.g. "push" or "backup status"
	Args  string // their placeholder, e.g. "<dir>"; empty when none
	Help  string
	Scope Scope
	Do    func(m M, rows []T, args []string) (M, tea.Cmd)
}

// CommandInfos describes commands for completion and help.
func CommandInfos[M, T any](commands []Command[M, T]) []nav.CommandInfo {
	out := make([]nav.CommandInfo, len(commands))
	for i, c := range commands {
		out[i] = nav.CommandInfo{Name: c.Name, Args: c.Args, Help: c.Help}
	}
	return out
}

// RunCommand handles msg when it starts with one of commands' names: it calls
// Do with the rows the command's scope covers and the words after the name, and
// reports true. The longest matching name wins, so `backup status` beats a
// `backup`. A command that needs rows does nothing when there are none.
func RunCommand[M, T any](m M, l Model[T], commands []Command[M, T], msg nav.CommandMsg) (M, tea.Cmd, bool) {
	best := -1
	for i, c := range commands {
		name := strings.Fields(c.Name)
		if len(msg.Words) < len(name) || !slices.Equal(msg.Words[:len(name)], name) {
			continue
		}
		if best < 0 || len(name) > len(strings.Fields(commands[best].Name)) {
			best = i
		}
	}
	if best < 0 {
		return m, nil, false
	}
	c := commands[best]
	rows := l.Rows(c.Scope)
	if c.Scope != Global && c.Scope != Marked && len(rows) == 0 {
		return m, nil, true
	}
	next, cmd := c.Do(m, rows, msg.Words[len(strings.Fields(c.Name)):])
	return next, cmd, true
}
