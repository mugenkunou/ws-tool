package app

import (
	"slices"
	"strings"
	"testing"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"github.com/charmbracelet/x/ansi"

	"github.com/mugenkunou/ws-tool/internal/tui/keys"
	"github.com/mugenkunou/ws-tool/internal/tui/nav"
	"github.com/mugenkunou/ws-tool/internal/tui/reslist"
)

func helpBindings(km help.KeyMap) []key.Binding {
	var out []key.Binding
	for _, row := range km.FullHelp() {
		out = append(out, row...)
	}
	return out
}

// TestCoverageKeysExist: a coverage entry's key is one its screen offers.
func TestCoverageKeysExist(t *testing.T) {
	m := loaded(t, 100, 30)
	for _, c := range CLICoverage {
		if len(c.Key.Keys()) == 0 {
			continue
		}
		offered := helpBindings(m.screens[c.Screen].KeyMap())
		if !slices.ContainsFunc(offered, func(b key.Binding) bool { return slices.Equal(b.Keys(), c.Key.Keys()) }) {
			t.Errorf("%s: screen %v does not offer key %v", c.CLI, c.Screen, c.Key.Keys())
		}
	}
}

// TestListScreensShareListKeys: every list screen offers the shared list
// bindings, and none reuses their keys for its own actions.
func TestListScreensShareListKeys(t *testing.T) {
	list := keys.List()
	reserved := map[string]bool{}
	for _, b := range list.Bindings() {
		for _, k := range b.Keys() {
			reserved[k] = true
		}
	}
	m := loaded(t, 100, 30)
	for s, km := range listScreenKeys() {
		for _, b := range helpBindings(km) {
			for _, k := range b.Keys() {
				if reserved[k] {
					t.Errorf("screen %v binds shared list key %q to %q", s, k, b.Help().Desc)
				}
			}
		}
		offered := helpBindings(m.screens[s].KeyMap())
		if !slices.ContainsFunc(offered, func(b key.Binding) bool { return slices.Equal(b.Keys(), list.Filter.Keys()) }) {
			t.Errorf("screen %v does not offer the shared list keys (is it a reslist?)", s)
		}
	}
}

// listScreenKeys is each list screen's own key map.
func listScreenKeys() map[nav.Screen]help.KeyMap {
	return map[nav.Screen]help.KeyMap{
		nav.ScreenRepos: keys.Repos(), nav.ScreenDotfiles: keys.Dotfiles(), nav.ScreenScratch: keys.Scratch(),
		nav.ScreenSessions: keys.Logs(), nav.ScreenIgnore: keys.Ignore(), nav.ScreenSecrets: keys.Secrets(),
		nav.ScreenCron: keys.Cron(), nav.ScreenTrash: keys.Trash(),
	}
}

// TestEveryListKeyDeclaresItsRows: every action key on a list screen is
// declared with a reslist.Scope, so it is handed the marked / filtered rows
// by reslist.Run and cannot quietly act on everything. Only movement, back
// and refresh are exempt.
func TestEveryListKeyDeclaresItsRows(t *testing.T) {
	exempt := map[string]bool{"esc": true, "r": true}
	tk := keys.Table()
	for _, b := range []key.Binding{tk.LineUp, tk.LineDown, tk.PageUp, tk.PageDown, tk.HalfPageUp, tk.HalfPageDown, tk.GotoTop, tk.GotoBottom} {
		exempt[strings.Join(b.Keys(), ",")] = true
	}
	m := loaded(t, 100, 30)
	for s, km := range listScreenKeys() {
		d, ok := m.screens[s].(interface{ Declared() []reslist.Decl })
		if !ok {
			t.Errorf("screen %v does not declare its actions", s)
			continue
		}
		declared := map[string]bool{}
		for _, a := range d.Declared() {
			declared[strings.Join(a.Key.Keys(), ",")] = true
		}
		for _, b := range helpBindings(km) {
			k := strings.Join(b.Keys(), ",")
			if !exempt[k] && !declared[k] {
				t.Errorf("screen %v: key %q (%s) has no declared scope", s, k, b.Help().Desc)
			}
		}
	}
}

// TestFooterShowsMarkKeys: every list screen's footer shows the k9s mark
// keys when there is room, and the global keys always survive truncation.
func TestFooterShowsMarkKeys(t *testing.T) {
	for s := range listScreenKeys() {
		key := keys.ScreenDigit(int(s))
		wide := send(loaded(t, 220, 30), press(key))
		if foot := footerLine(view(wide)); !strings.Contains(foot, "space mark • ctrl+space mark range") {
			t.Errorf("screen %v footer at 220 cols lacks the mark keys:\n%s", s, foot)
		}
		narrow := send(loaded(t, 100, 30), press(key))
		if foot := footerLine(view(narrow)); !strings.Contains(foot, "? help • q quit") {
			t.Errorf("screen %v footer at 100 cols lost the global keys:\n%s", s, foot)
		}
	}
}

// footerLine is the last non-blank line of a frame, without styling.
func footerLine(frame string) string {
	lines := strings.Split(strings.TrimRight(ansi.Strip(frame), "\n "), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}
