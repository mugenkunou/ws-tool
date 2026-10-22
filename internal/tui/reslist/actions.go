package reslist

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
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
		if a.Scope != Global && len(rows) == 0 {
			return m, nil, true
		}
		next, cmd := a.Do(m, rows)
		return next, cmd, true
	}
	return m, nil, false
}

// scopedKeys returns the help keys of the declared actions with one of the
// given scopes.
func (m Model[T]) scopedKeys(scopes ...Scope) []string {
	var out []string
	for _, d := range m.decls {
		for _, s := range scopes {
			if d.Scope == s {
				out = append(out, d.Key.Help().Key)
			}
		}
	}
	return out
}
