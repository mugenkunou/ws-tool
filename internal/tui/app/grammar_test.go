package app

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"unicode"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	wscron "github.com/mugenkunou/ws-tool/internal/cron"
	wsignore "github.com/mugenkunou/ws-tool/internal/ignore"
	wsrepo "github.com/mugenkunou/ws-tool/internal/repo"
	"github.com/mugenkunou/ws-tool/internal/resource"
	wsscratch "github.com/mugenkunou/ws-tool/internal/scratch"
	wssecret "github.com/mugenkunou/ws-tool/internal/secret"
	wssession "github.com/mugenkunou/ws-tool/internal/session"
	symlink "github.com/mugenkunou/ws-tool/internal/symlink"
	wstrash "github.com/mugenkunou/ws-tool/internal/trash"
	"github.com/mugenkunou/ws-tool/internal/tui/keys"
	"github.com/mugenkunou/ws-tool/internal/tui/nav"
	"github.com/mugenkunou/ws-tool/internal/tui/screens/cron"
	"github.com/mugenkunou/ws-tool/internal/tui/screens/ignore"
	"github.com/mugenkunou/ws-tool/internal/tui/screens/repos"
	"github.com/mugenkunou/ws-tool/internal/tui/screens/scratch"
	"github.com/mugenkunou/ws-tool/internal/tui/screens/secrets"
	"github.com/mugenkunou/ws-tool/internal/tui/screens/sessions"
	"github.com/mugenkunou/ws-tool/internal/tui/screens/symlinks"
	"github.com/mugenkunou/ws-tool/internal/tui/screens/trash"
)

// listScreens are the screens that are a reslist.
var listScreens = []nav.Screen{
	nav.ScreenRepos, nav.ScreenSymlinks, nav.ScreenScratch, nav.ScreenSessions,
	nav.ScreenIgnore, nav.ScreenSecrets, nav.ScreenCron, nav.ScreenTrash, nav.ScreenSkills,
}

// everyBinding is every binding of the TUI that a key can reach: each screen
// (in each of its views), the lists, the dialogs, the global keys.
func everyBinding(t *testing.T) []key.Binding {
	t.Helper()
	var out []key.Binding
	add := func(km help.KeyMap) {
		for _, row := range km.FullHelp() {
			out = append(out, row...)
		}
		out = append(out, km.ShortHelp()...)
	}
	m := loaded(t, 100, 30)
	for _, s := range nav.Screens() {
		add(m.screens[s].KeyMap())
	}
	// The Ignore screen's other views.
	ig := send(m, press("6"))
	for range 2 {
		ig = send(ig, press("v"))
		add(ig.screens[nav.ScreenIgnore].KeyMap())
	}
	add(m.keys)
	add(keys.FilterMap{List: keys.List()})
	add(keys.Confirm())
	add(keys.Prompt())
	add(keys.Choose())
	add(keys.Detail())
	out = append(out, keys.List().Bindings()...)
	tk := keys.Table()
	out = append(out, tk.LineUp, tk.LineDown, tk.PageUp, tk.PageDown, tk.GotoTop, tk.GotoBottom)
	return out
}

// TestNoShiftLetterActions: spec §6.3. There are no shift+letter actions;
// G is the only uppercase key and it is movement.
func TestNoShiftLetterActions(t *testing.T) {
	for _, b := range everyBinding(t) {
		for _, k := range b.Keys() {
			r := []rune(k)
			if len(r) == 1 && unicode.IsUpper(r[0]) && k != "G" {
				t.Errorf("binding %q (%s) uses the uppercase key %q", b.Help().Desc, k, k)
			}
			if strings.HasPrefix(k, "shift+") && k != "shift+tab" {
				t.Errorf("binding %q uses %q", b.Help().Desc, k)
			}
		}
	}
}

// TestNoKeyTwiceOnAScreen: a key means one thing on a screen view: the screen's
// own keys, the shared list keys, movement and the global keys never collide.
// esc and enter are the grammar's context keys and are exempt.
func TestNoKeyTwiceOnAScreen(t *testing.T) {
	m := loaded(t, 100, 30)
	type view struct {
		name string
		km   help.KeyMap
	}
	var views []view
	for _, s := range listScreens {
		views = append(views, view{s.Title(), m.screens[s].KeyMap()})
	}
	ig := send(m, press("6"))
	for i := range 2 {
		ig = send(ig, press("v"))
		views = append(views, view{"Ignore view " + string(rune('2'+i)), ig.screens[nav.ScreenIgnore].KeyMap()})
	}
	for _, v := range views {
		owner := map[string]string{}
		seen := map[string]bool{}
		var all []key.Binding
		for _, row := range v.km.FullHelp() {
			all = append(all, row...)
		}
		for _, row := range m.keys.FullHelp() {
			all = append(all, row...)
		}
		tk := keys.Table()
		all = append(all, tk.LineUp, tk.LineDown, tk.PageUp, tk.PageDown, tk.GotoTop, tk.GotoBottom)
		for _, b := range all {
			id := strings.Join(b.Keys(), ",") + "|" + b.Help().Desc
			if seen[id] || !b.Enabled() {
				continue // the same binding listed twice
			}
			seen[id] = true
			for _, k := range b.Keys() {
				if k == "esc" || k == "enter" {
					continue
				}
				if prev, ok := owner[k]; ok {
					t.Errorf("%s: key %q is both %q and %q", v.name, k, prev, b.Help().Desc)
				}
				owner[k] = b.Help().Desc
			}
		}
	}
}

// TestMarkingMovesNoRow: spec §6.1. A list keeps its status line reserved, so
// marking, opening the filter or marking all moves no row; sorting only
// reorders them.
func TestMarkingMovesNoRow(t *testing.T) {
	lines := func(m Model) []string {
		return strings.Split(strings.TrimRight(ansi.Strip(view(m)), "\n"), "\n")
	}
	for _, s := range listScreens {
		base := send(loaded(t, 100, 30), press(screenKey(int(s))))
		before := lines(base)
		for _, k := range [][]string{{"space"}, {"/"}, {"ctrl+a"}, {"space", "down", "space"}} {
			after := lines(send(base, pressAll(k)...))
			if len(after) != len(before) {
				t.Errorf("%s %v: frame is %d lines, was %d", s.Title(), k, len(after), len(before))
				continue
			}
			// Everything above the status line and footer is where it was.
			for i := 0; i < len(before)-2; i++ {
				if before[i] != after[i] && !(k[0] == "space" && len(k) == 3 && i == cursorLine(before)) {
					// the cursor mark glyph may differ; text must not.
					if strings.TrimSpace(strings.TrimLeft(before[i], "› ")) != strings.TrimSpace(strings.TrimLeft(after[i], "› ")) {
						t.Errorf("%s %v: line %d moved:\n  was  %q\n  now  %q", s.Title(), k, i, before[i], after[i])
					}
				}
			}
		}
		// Sorting keeps the title row where it is.
		sorted := lines(send(base, press(">")))
		ti := slices.IndexFunc(before, func(l string) bool {
			return strings.Contains(l, "↑") || strings.Contains(l, "Repo") || strings.Contains(l, "Name") || strings.Contains(l, "Job")
		})
		if ti >= 0 && ti < len(sorted) && strings.TrimSpace(sorted[ti]) == "" {
			t.Errorf("%s: sorting moved the title row", s.Title())
		}
	}
}

func cursorLine(lines []string) int {
	for i, l := range lines {
		if strings.Contains(l, "›") {
			return i
		}
	}
	return -1
}

func pressAll(ks []string) []tea.Msg {
	out := make([]tea.Msg, len(ks))
	for i, k := range ks {
		out[i] = press(k)
	}
	return out
}

// TestFooterFits: spec §6.1. At 100 columns the global part is always shown,
// and each screen's own action keys come first, in full.
func TestFooterFits(t *testing.T) {
	m := loaded(t, 100, 30)
	cases := map[string]Model{}
	for _, s := range listScreens {
		cases[s.Title()] = send(m, press(screenKey(int(s))))
	}
	cases["Dashboard"] = m
	ig := send(m, press("6"))
	for i := range 2 {
		ig = send(ig, press("v"))
		cases["Ignore view "+string(rune('2'+i))] = ig
	}
	list := map[string]bool{}
	for _, b := range keys.List().Bindings() {
		list[b.Help().Key] = true
	}
	for name, mm := range cases {
		foot := footerLine(view(mm))
		for _, g := range []string{": command", "? help", "q quit"} {
			if !strings.Contains(foot, g) {
				t.Errorf("%s: footer lost %q:\n%s", name, g, foot)
			}
		}
		var own []string
		for _, b := range mm.current().KeyMap().ShortHelp() {
			if h := b.Help(); b.Enabled() && !list[h.Key] {
				own = append(own, h.Key+" "+h.Desc)
			}
		}
		// The screen's keys come first and are never cut in the middle: either
		// all of them show, or the part ends in an ellipsis after at least the
		// first four (spec §6.1; ? shows the rest).
		shown := 0
		for _, want := range own {
			if strings.Contains(foot, want) {
				shown++
			}
		}
		if shown < min(len(own), 4) || (shown < len(own) && !strings.Contains(foot, "…")) {
			t.Errorf("%s: %d of the screen's %d keys fit, with no ellipsis for the rest:\n%s", name, shown, len(own), foot)
		}
		if shown > 0 && !strings.HasPrefix(foot, own[0]) {
			t.Errorf("%s: the screen's keys do not come first:\n%s", name, foot)
		}
	}
}

// TestJSONPaneNeedsNoView: spec §6.3. `y` shows exactly what `ws <noun> ls
// --json` prints for the row. Both build the record from the noun's one
// schema, and only a field's display text may depend on the view (the clock and
// how paths are shortened), never its value. So the TUI's schema and the CLI's
// give the same record for every row.
func TestJSONPaneNeedsNoView(t *testing.T) {
	cli := resource.CLIView()
	tui := resource.View{Now: fixedClock, Path: func(p string) string { return "~" + p }}
	record := func(v any) string {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	same := func(name string, a, b any) {
		t.Helper()
		if ja, jb := record(a), record(b); ja != jb {
			t.Errorf("%s: the TUI's record differs from the CLI's:\n  cli %s\n  tui %s", name, ja, jb)
		}
	}
	for _, msg := range fixtures() {
		switch msg := msg.(type) {
		case repos.LoadedMsg:
			for _, e := range msg.Entries {
				same("repo", wsrepo.Schema(cli).Record(e), wsrepo.Schema(tui).Record(e))
			}
		case symlinks.LoadedMsg:
			for _, e := range msg.Entries {
				same("symlink", symlink.Schema(cli).Record(e), symlink.Schema(tui).Record(e))
			}
		case scratch.LoadedMsg:
			for _, e := range msg.Entries {
				same("scratch", wsscratch.Schema(cli).Record(e), wsscratch.Schema(tui).Record(e))
			}
		case sessions.LoadedMsg:
			for _, e := range msg.Sessions {
				same("session", wssession.Schema(cli).Record(e), wssession.Schema(tui).Record(e))
			}
		case ignore.LoadedMsg:
			for _, e := range msg.Violations {
				same("ignore", wsignore.Schema(cli, ws).Record(e), wsignore.Schema(tui, ws).Record(e))
			}
		case secrets.LoadedMsg:
			for _, e := range msg.Violations {
				same("secret", wssecret.Schema(cli, ws).Record(e), wssecret.Schema(tui, ws).Record(e))
			}
		case cron.LoadedMsg:
			for _, e := range msg.Jobs {
				same("cron", wscron.Schema(cli).Record(e), wscron.Schema(tui).Record(e))
			}
		case trash.LoadedMsg:
			for _, e := range msg.Items {
				same("trash", wstrash.Schema(cli).Record(e), wstrash.Schema(tui).Record(e))
			}
		}
	}
}

// TestDashboardOpensTheScreenOnTheProblemRow: enter on a Dashboard problem leaves the
// Symlinks screen with the cursor on the first broken, missing or drifted row.
func TestDashboardOpensTheScreenOnTheProblemRow(t *testing.T) {
	m := dashboardTo(t, loaded(t, 100, 30), "symlinks broken, missing or drifted")
	m, cmd := sendRun(m, press("enter"))
	m = apply(m, cmd)
	if m.active != nav.ScreenSymlinks {
		t.Fatalf("active = %v", m.active.Title())
	}
	// `d` describes the cursor row: it must be the broken one.
	v := tuitestPlain(view(send(m, press("d"))))
	if !strings.Contains(v, "Host path       ~/.gitconfig") {
		t.Fatalf("the cursor should be on the broken row:\n%s", v)
	}
}

func tuitestPlain(s string) string { return ansi.Strip(s) }
