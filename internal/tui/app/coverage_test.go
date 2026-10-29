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
	"github.com/mugenkunou/ws-tool/internal/tui/screens/dashboard"
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
		// A key may live on one view of the screen (Ignore has three), so the
		// screen's whole key map counts as well as the view that is open.
		offered := helpBindings(m.screens[c.Screen].KeyMap())
		if km, ok := listScreenKeys()[c.Screen]; ok {
			offered = append(offered, helpBindings(km)...)
		}
		if !slices.ContainsFunc(offered, func(b key.Binding) bool { return slices.Equal(b.Keys(), c.Key.Keys()) }) {
			t.Errorf("%s: screen %v does not offer key %v", c.CLI, c.Screen, c.Key.Keys())
		}
	}
}

// TestCoverageCommandsExist: a coverage entry's `:` command is one its screen
// declares.
func TestCoverageCommandsExist(t *testing.T) {
	m := loaded(t, 100, 30)
	for _, c := range CLICoverage {
		if c.Cmd == "" {
			continue
		}
		known := commander(m.screens[c.Screen])
		if !slices.ContainsFunc(known, func(i nav.CommandInfo) bool { return i.Name == c.Cmd }) {
			t.Errorf("%s: screen %v has no `:` command %q", c.CLI, c.Screen, c.Cmd)
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

// listScreenKeys is each list screen's own key map, from the registry.
func listScreenKeys() map[nav.Screen]help.KeyMap {
	out := map[nav.Screen]help.KeyMap{}
	for s, e := range registry {
		if e.Keys != nil {
			out[s] = e.Keys()
		}
	}
	return out
}

// TestRegistryCoversEveryScreen: every nav.Screen has a constructor and a
// spec, and every list screen (one that declares its actions) names its key
// map, so the shared-key tests below cannot skip it.
func TestRegistryCoversEveryScreen(t *testing.T) {
	if len(registry) != len(nav.Screens()) {
		t.Errorf("registry has %d screens, nav has %d", len(registry), len(nav.Screens()))
	}
	m := loaded(t, 100, 30)
	for _, s := range nav.Screens() {
		e, ok := registry[s]
		if !ok || e.New == nil {
			t.Errorf("screen %d (%s) has no registry entry (internal/tui/app/registry.go)", s, s.Title())
			continue
		}
		if sp := s.Spec(); sp.Title == "" || sp.Icon == "" {
			t.Errorf("screen %d has no spec row (internal/tui/nav/nav.go specs)", s)
		}
		_, list := m.screens[s].(interface{ Declared() []reslist.Decl })
		if list != (e.Keys != nil) {
			t.Errorf("screen %v: a reslist screen names its Keys in the registry, and only it (reslist %v, Keys %v)", s, list, e.Keys != nil)
		}
	}
}

// TestDashboardHasACardForEveryScreen: every screen's load result is a
// nav.Summary the Dashboard turns into a card. A screen without one leaves its
// card spinning forever.
func TestDashboardHasACardForEveryScreen(t *testing.T) {
	m := loaded(t, 100, 30)
	d := m.screens[nav.ScreenDashboard].(dashboard.Model)
	for _, s := range nav.Screens()[1:] {
		if !d.Loaded(s) {
			t.Errorf("the Dashboard has no card for %s: make its LoadedMsg a nav.Summary and add one to fixtures() in app_test.go", s.Title())
		}
	}
}

// TestNoCoverageOnTheDashboard: the Dashboard has no actions, so a command
// "covered" there has no TUI home. Give it a screen (a hosting one, see
// nav.Spec.Hosts) instead.
func TestNoCoverageOnTheDashboard(t *testing.T) {
	for _, c := range CLICoverage {
		if c.Screen == nav.ScreenDashboard {
			t.Errorf("%s is parked on the Dashboard: cover it on the screen that hosts its noun", c.CLI)
		}
	}
}

// TestCoverageReasons: an entry with a key or a `:` command is reached by it;
// one with neither says why, and a CLI-only one says so in a note.
func TestCoverageReasons(t *testing.T) {
	for _, c := range CLICoverage {
		reached := len(c.Key.Keys()) > 0 || c.Cmd != ""
		switch {
		case reached && c.Why != Reached:
			t.Errorf("%s has a key or command, so its reason must be Reached", c.CLI)
		case !reached && c.Why == Reached:
			t.Errorf("%s has no key and no command: give the reason (List, Plumbing, Shell, CLIOnly)", c.CLI)
		case c.Why == CLIOnly && c.Note == "":
			t.Errorf("%s is CLI-only: say why in Note", c.CLI)
		}
		if host, ok := nav.ForNoun(strings.Fields(c.CLI)[0]); ok && host != c.Screen {
			t.Errorf("%s is covered on %s, but its noun belongs to %s", c.CLI, c.Screen.Title(), host.Title())
		}
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
		wide := send(loaded(t, 220, 30), jump(s))
		if foot := footerLine(view(wide)); !strings.Contains(foot, "space mark • ctrl+space mark range") {
			t.Errorf("screen %v footer at 220 cols lacks the mark keys:\n%s", s, foot)
		}
		narrow := send(loaded(t, 100, 30), jump(s))
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
