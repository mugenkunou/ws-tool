package reslist

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/mugenkunou/ws-tool/internal/resource"
	"github.com/mugenkunou/ws-tool/internal/tui/choose"
	"github.com/mugenkunou/ws-tool/internal/tui/theme"
)

type fruit struct {
	Name  string
	Price int
}

var schema = resource.Schema[fruit]{
	Name: "fruit",
	ID:   func(f fruit) string { return f.Name },
	Fields: []resource.Field[fruit]{
		{Key: "name", Title: "Name", Value: func(f fruit) any { return f.Name }},
		{Key: "price", Title: "Price", Value: func(f fruit) any { return f.Price }},
		{Key: "origin", Title: "Origin", Value: func(fruit) any { return "farm" }},
	},
	Columns: []string{"name", "price"},
}

func newList() Model[fruit] {
	m := New(schema, []Col[fruit]{{Key: "name", Min: 10}, {Key: "price", Min: 5}}, "none")
	m = m.SetSize(40, 10)
	return m.SetItems([]fruit{{"pear", 3}, {"apple", 5}, {"fig", 1}})
}

func kp(s string) tea.KeyPressMsg {
	switch s {
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "ctrl+space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Mod: tea.ModCtrl}
	case "ctrl+\\":
		return tea.KeyPressMsg{Code: '\\', Mod: tea.ModCtrl}
	case "ctrl+a":
		return tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl}
	}
	return tea.KeyPressMsg{Code: []rune(s)[0], Text: s}
}

// press feeds keys the way a screen does: shared keys first, then movement.
func press(m Model[fruit], keys ...string) Model[fruit] {
	for _, k := range keys {
		var ok bool
		if m, _, ok = m.HandleKey(kp(k)); !ok {
			m, _ = m.Update(kp(k))
		}
	}
	return m
}

func names(fs []fruit) string {
	out := make([]string, len(fs))
	for i, f := range fs {
		out[i] = f.Name
	}
	return strings.Join(out, ",")
}

func TestFilterLive(t *testing.T) {
	m := press(newList(), "/", "p")
	if !m.Editing() {
		t.Fatal("expected editing after /")
	}
	if got := names(m.Shown()); got != "pear,apple" {
		t.Errorf("shown = %s", got)
	}
	m = press(m, "e", "enter")
	if m.Editing() || names(m.Shown()) != "pear" {
		t.Errorf("after enter: editing=%v shown=%s", m.Editing(), names(m.Shown()))
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "filter pe (1 of 3)") {
		t.Errorf("status line missing:\n%s", v)
	}
	m = press(m, "esc")
	if names(m.Shown()) != "pear,apple,fig" {
		t.Errorf("esc should clear the filter, shown = %s", names(m.Shown()))
	}
}

func TestFilterCancelRestores(t *testing.T) {
	m := press(newList(), "/", "g", "enter", "/", "x", "esc")
	if names(m.Shown()) != "fig" {
		t.Errorf("cancel should restore previous filter, shown = %s", names(m.Shown()))
	}
}

func TestFilterByField(t *testing.T) {
	m := press(newList(), "/")
	for _, r := range "price=5" {
		m = press(m, string(r))
	}
	if names(m.Shown()) != "apple" {
		t.Errorf("shown = %s", names(m.Shown()))
	}
}

func TestSortCyclesFieldsAndKeepsCursor(t *testing.T) {
	m := press(newList(), "down") // cursor on apple
	m = press(m, ">")             // sort by name
	if names(m.Shown()) != "apple,fig,pear" {
		t.Errorf("by name = %s", names(m.Shown()))
	}
	if c, _ := m.Current(); c.Name != "apple" {
		t.Errorf("cursor moved to %s", c.Name)
	}
	m = press(m, "<")
	if names(m.Shown()) != "pear,fig,apple" {
		t.Errorf("reversed = %s", names(m.Shown()))
	}
	m = press(m, ">") // price ascending
	if names(m.Shown()) != "fig,pear,apple" {
		t.Errorf("by price = %s", names(m.Shown()))
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "Price ↑") {
		t.Errorf("sorted column not flagged:\n%s", v)
	}
	m = press(m, ">", ">") // origin, then back to loaded order
	if names(m.Shown()) != "pear,apple,fig" {
		t.Errorf("unsorted = %s", names(m.Shown()))
	}
}

func TestMarksDriveTargets(t *testing.T) {
	m := newList()
	if names(m.Rows(Selected)) != "pear" {
		t.Errorf("no marks: targets = %s", names(m.Rows(Selected)))
	}
	m = press(m, "space", "down", "down", "space") // pear, then fig
	if names(m.Rows(Selected)) != "pear,fig" {
		t.Errorf("targets = %s", names(m.Rows(Selected)))
	}
	// Marks survive a reload that keeps the items.
	m = m.SetItems([]fruit{{"fig", 1}, {"pear", 3}, {"kiwi", 2}})
	if names(m.Rows(Selected)) != "fig,pear" {
		t.Errorf("after reload targets = %s", names(m.Rows(Selected)))
	}
	m = press(m, "esc")
	if len(m.marked()) != 0 {
		t.Error("esc should clear marks")
	}
	m = press(m, "ctrl+a")
	if len(m.marked()) != 3 {
		t.Errorf("ctrl+a marked %d", len(m.marked()))
	}
	m = press(m, "ctrl+a")
	if len(m.marked()) != 0 {
		t.Errorf("second ctrl+a left %d marked", len(m.marked()))
	}
}

// fiveList has five rows so a range has a middle.
func fiveList() Model[fruit] {
	m := New(schema, []Col[fruit]{{Key: "name", Min: 10}, {Key: "price", Min: 5}}, "none")
	return m.SetSize(40, 10).SetItems([]fruit{{"a", 1}, {"b", 2}, {"c", 3}, {"d", 4}, {"e", 5}})
}

func TestMarkRangeLikeK9s(t *testing.T) {
	// Mark a, move to d, ctrl+space: a..d marked (nearest mark above).
	m := press(fiveList(), "space", "down", "down", "down", "ctrl+space")
	if got := names(m.marked()); got != "a,b,c,d" {
		t.Errorf("range down = %s", got)
	}
	// Nearest mark below when none above: mark e, go to b.
	m = press(fiveList(), "down", "down", "down", "down", "space", "up", "up", "up", "ctrl+space")
	if got := names(m.marked()); got != "b,c,d,e" {
		t.Errorf("range up = %s", got)
	}
	// No mark: marks just the cursor row.
	m = press(fiveList(), "down", "ctrl+space")
	if got := names(m.marked()); got != "b" {
		t.Errorf("range without a mark = %s", got)
	}
	// ctrl+\ clears every mark.
	m = press(fiveList(), "A", "ctrl+\\")
	if n := len(m.marked()); n != 0 {
		t.Errorf("ctrl+\\ left %d marked", n)
	}
}

func TestSpaceTogglesWithoutMoving(t *testing.T) {
	m := press(newList(), "space")
	if c, _ := m.Current(); c.Name != "pear" {
		t.Errorf("space moved the cursor to %s", c.Name)
	}
	if names(m.marked()) != "pear" {
		t.Errorf("marked = %s", names(m.marked()))
	}
	if m = press(m, "space"); len(m.marked()) != 0 {
		t.Error("second space should unmark")
	}
}

// Marked rows are drawn k9s-style: the row's own text in the mark colour,
// nothing inserted; other rows keep their column styles.
func TestMarkedRowIsHighlightedNotPrefixed(t *testing.T) {
	m := press(newList(), "down", "space", "up") // apple marked; cursor on pear
	v := m.View()
	if !strings.Contains(v, theme.Marked.Render("apple")) {
		t.Errorf("marked row not in the mark colour:\n%q", v)
	}
	if strings.Contains(ansi.Strip(v), "✓") {
		t.Errorf("marked row has an inserted tick:\n%s", ansi.Strip(v))
	}
	for _, line := range strings.Split(ansi.Strip(v), "\n") {
		if strings.Contains(line, "apple") && !strings.HasPrefix(strings.TrimSpace(line), "apple") {
			t.Errorf("marked row text shifted: %q", line)
		}
	}
}

// cursorLine is the rendered line holding name.
func cursorLine(m Model[fruit], name string) string {
	for _, l := range strings.Split(m.View(), "\n") {
		if strings.Contains(ansi.Strip(l), name) {
			return l
		}
	}
	return ""
}

func TestMarkedRowStaysVisibleUnderCursor(t *testing.T) {
	plain, marked := cursorLine(newList(), "pear"), cursorLine(press(newList(), "space"), "pear")
	if ansi.Strip(plain) != ansi.Strip(marked) {
		t.Fatalf("text differs: %q vs %q", ansi.Strip(plain), ansi.Strip(marked))
	}
	if plain == marked {
		t.Error("a marked cursor row looks the same as an unmarked one")
	}
}

func TestEscWithNothingToClearIsTheScreens(t *testing.T) {
	if _, _, ok := newList().HandleKey(kp("esc")); ok {
		t.Error("esc with no filter or marks must fall through to the screen")
	}
}

func TestDetailShowsEveryField(t *testing.T) {
	m := press(newList(), "d")
	if !m.InDetail() {
		t.Fatal("expected detail pane")
	}
	v := ansi.Strip(m.View())
	for _, want := range []string{"Name", "pear", "Price", "3", "Origin", "farm"} {
		if !strings.Contains(v, want) {
			t.Errorf("detail lacks %q:\n%s", want, v)
		}
	}
	m = press(m, "esc")
	if m.InDetail() {
		t.Error("esc should close the detail pane")
	}
}

func TestSelectClearsHidingFilter(t *testing.T) {
	m := press(newList(), "/", "g", "enter")
	m, ok := m.Select(func(f fruit) bool { return f.Name == "apple" })
	if !ok {
		t.Fatal("apple not found")
	}
	if c, _ := m.Current(); c.Name != "apple" {
		t.Errorf("cursor on %s", c.Name)
	}
}

func TestSelectClosesDetailAndFilterEditing(t *testing.T) {
	m := press(newList(), "i")
	m, _ = m.Select(func(f fruit) bool { return f.Name == "fig" })
	if m.InDetail() {
		t.Error("Select should close the detail pane")
	}
	m = press(m, "/", "x")
	m, _ = m.Select(func(f fruit) bool { return f.Name == "fig" })
	if m.Editing() {
		t.Error("Select should stop filter editing")
	}
	if c, _ := m.Current(); c.Name != "fig" {
		t.Errorf("cursor on %s", c.Name)
	}
}

// screen stands in for a screen model: it records the rows an action got.
type screen struct{ got map[string]string }

func record(name string) func(screen, []fruit) (screen, tea.Cmd) {
	return func(s screen, rows []fruit) (screen, tea.Cmd) {
		s.got[name] = names(rows)
		return s, nil
	}
}

func TestRunHandsEachScopeItsRows(t *testing.T) {
	actions := []Action[screen, fruit]{
		{Key: kb("n"), Scope: Global, Do: record("global")},
		{Key: kb("o"), Scope: Cursor, Do: record("cursor")},
		{Key: kb("d"), Scope: Selected, Do: record("selected")},
		{Key: kb("f"), Scope: All, Do: record("all")},
	}
	run := func(m Model[fruit], keys ...string) map[string]string {
		s := screen{got: map[string]string{}}
		for _, k := range keys {
			var ok bool
			if s, _, ok = Run(s, m, actions, kp(k)); !ok {
				t.Fatalf("%s not handled", k)
			}
		}
		return s.got
	}
	want := func(got map[string]string, name, rows string) {
		t.Helper()
		if got[name] != rows {
			t.Errorf("%s got rows %q, want %q", name, got[name], rows)
		}
	}

	// Nothing marked, no filter: selected = cursor, all = every row.
	got := run(newList(), "n", "o", "d", "f")
	want(got, "global", "")
	want(got, "cursor", "pear")
	want(got, "selected", "pear")
	want(got, "all", "pear,apple,fig")

	// Filter: all covers only the rows shown.
	got = run(press(newList(), "/", "p", "enter"), "f")
	want(got, "all", "pear,apple")

	// Marks: selected and all both cover the marked rows; cursor stays one row.
	m := press(newList(), "space", "down", "space", "down") // pear, apple; cursor on fig
	got = run(m, "o", "d", "f")
	want(got, "cursor", "fig")
	want(got, "selected", "pear,apple")
	want(got, "all", "pear,apple")

	if _, _, ok := Run(screen{got: map[string]string{}}, newList(), actions, kp("z")); ok {
		t.Error("an undeclared key must fall through")
	}
}

func TestStatusLineNamesKeysThatActOnMarks(t *testing.T) {
	m := newList().SetSize(100, 10).Declare([]Decl{
		{Key: kb("o"), Scope: Cursor}, {Key: kb("d"), Scope: Selected}, {Key: kb("f"), Scope: All},
	})
	if v := ansi.Strip(press(m, "space").View()); !strings.Contains(v, "1 marked — d do d · f do f act on them") {
		t.Errorf("marks hint missing:\n%s", v)
	}
	if v := ansi.Strip(press(m, "/", "p", "enter").View()); !strings.Contains(v, "f do f act on these 2") {
		t.Errorf("filter hint missing:\n%s", v)
	}
}

func kb(k string) key.Binding { return key.NewBinding(key.WithKeys(k), key.WithHelp(k, "do "+k)) }

// ── bottom status line, JSON pane, commands, SetFilter ─────────────

func TestStatusLineIsAlwaysTheLastLine(t *testing.T) {
	m := newList() // size 40x10
	lines := func(m Model[fruit]) []string { return strings.Split(ansi.Strip(m.View()), "\n") }
	idle := lines(m)
	if len(idle) != 10 {
		t.Fatalf("view is %d lines, want 10", len(idle))
	}
	if strings.TrimSpace(idle[9]) != "" {
		t.Fatalf("an idle status line is blank: %q", idle[9])
	}
	// Marking, filtering and sorting never move a row: the rows of the idle
	// view stay where they are, and the status line is the last line.
	for _, keys := range [][]string{{"space"}, {"/", "p", "enter"}, {">"}, {"ctrl+a"}} {
		got := lines(press(m, keys...))
		if len(got) != 10 {
			t.Fatalf("%v: view is %d lines, want 10", keys, len(got))
		}
		if strings.TrimSpace(got[0]) == "" || !strings.Contains(got[0], "Name") {
			t.Fatalf("%v: the title row moved: %q", keys, got[0])
		}
		if strings.TrimSpace(got[9]) == "" {
			t.Fatalf("%v: the status line should say something:\n%s", keys, strings.Join(got, "\n"))
		}
	}
}

func TestJSONPaneIsTheRecord(t *testing.T) {
	m := press(newList(), "y")
	if !m.InDetail() {
		t.Fatal("y opens the JSON pane")
	}
	v := ansi.Strip(m.View())
	for _, want := range []string{`"name": "pear"`, `"price": 3`, `"origin": "farm"`} {
		if !strings.Contains(v, want) {
			t.Errorf("JSON pane lacks %q:\n%s", want, v)
		}
	}
	if strings.Index(v, `"name"`) > strings.Index(v, `"origin"`) {
		t.Errorf("keys are in field order:\n%s", v)
	}
	for _, closer := range []string{"y", "d", "esc"} {
		if press(m, closer).InDetail() {
			t.Errorf("%s should close the pane", closer)
		}
	}
}

func TestSetFilter(t *testing.T) {
	m := newList().SetFilter("name=p")
	if names(m.Shown()) != "pear,apple" || m.Filter() != "name=p" {
		t.Fatalf("shown %s filter %q", names(m.Shown()), m.Filter())
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "filter name=p (2 of 3)") {
		t.Errorf("status line:\n%s", v)
	}
	if names(m.SetFilter("").Shown()) != "pear,apple,fig" {
		t.Error("an empty expression clears the filter")
	}
}

func TestMenu(t *testing.T) {
	type model struct{ ran []string }
	do := func(name string) func(model, []fruit, []string) (model, tea.Cmd) {
		return func(m model, rows []fruit, args []string) (model, tea.Cmd) {
			m.ran = append(m.ran, name+":"+names(rows)+":"+strings.Join(args, " "))
			return m, nil
		}
	}
	cmds := []Command[model, fruit]{
		{Name: "backup push", Menu: "p", Scope: Global, Do: do("push")},
		{Name: "hidden", Scope: Global, Do: do("hidden")}, // no letter: not in the menu
		{Name: "fetch", Menu: "f", Scope: All, Do: do("fetch")},
		{Name: "rm", Menu: "r", Args: "<name>", Scope: Selected, Do: do("rm")},
	}
	l := press(newList(), "space", "down") // pear marked, cursor on apple
	pick := func(i int) (model, tea.Cmd) {
		return MenuPick(model{}, l, cmds, choose.ResultMsg{Index: i})
	}
	// The chooser lists only the commands with a letter, and the index counts those.
	if m, _ := pick(0); m.ran[0] != "push::" {
		t.Errorf("push: %v", m.ran)
	}
	if m, _ := pick(1); m.ran[0] != "fetch:pear:" {
		t.Errorf("scope All covers the marked rows, args are empty: %v", m.ran)
	}
	if m, _ := pick(2); m.ran[0] != "rm:pear:" {
		t.Errorf("scope Selected: %v", m.ran)
	}
	if m, cmd := MenuPick(model{}, l, cmds, choose.ResultMsg{Cancelled: true}); len(m.ran) != 0 || cmd != nil {
		t.Errorf("cancelled: %v", m.ran)
	}
	if got := CommandInfos(cmds); len(got) != 4 || got[3].Args != "<name>" || got[0].Menu != "p" {
		t.Errorf("CommandInfos = %v", got)
	}
	// A command that needs rows says so when there are none.
	empty := New(schema, []Col[fruit]{{Key: "name", Min: 10}, {Key: "price", Min: 5}}, "none").SetSize(40, 10).SetItems(nil)
	m, cmd := MenuPick(model{}, empty, cmds, choose.ResultMsg{Index: 2})
	if len(m.ran) != 0 || cmd == nil {
		t.Errorf("no rows: %v", m.ran)
	}
}

// TestPinnedRow: the pinned row is not an item. Filters, counts, marks and the
// rows an action gets ignore it; up from the first row focuses it, down leaves
// it, and enter on it sends PinnedMsg.
func TestPinnedRow(t *testing.T) {
	m := newList().SetPinned("3 changes to push")
	if v := ansi.Strip(m.View()); !strings.Contains(v, "▸ 3 changes to push") || !strings.HasPrefix(v, "▸") {
		t.Errorf("the pinned row is the first line:\n%s", v)
	}
	if got := len(m.Shown()); got != 3 {
		t.Errorf("the pinned row is not an item: %d shown", got)
	}
	if lines := strings.Count(m.View(), "\n") + 1; lines != 10 {
		t.Errorf("the view keeps its height with a pinned row: %d lines, want 10", lines)
	}

	// up on the first row focuses the pinned row: no data row is current.
	m = press(m, "up")
	if !m.PinnedFocused() {
		t.Fatal("up from the first row focuses the pinned row")
	}
	if _, ok := m.Current(); ok {
		t.Error("no row is current while the pinned row has the cursor")
	}
	if rows := m.Rows(Selected); len(rows) != 0 {
		t.Errorf("an action on the pinned row gets no data rows: %v", rows)
	}
	if rows := m.Rows(Cursor); len(rows) != 0 {
		t.Errorf("Cursor scope on the pinned row: %v", rows)
	}
	if m = press(m, "up"); !m.PinnedFocused() {
		t.Error("up at the top stays on the pinned row")
	}

	// enter on it is PinnedMsg, not an item action.
	_, cmd, ok := m.HandleKey(kp("enter"))
	if !ok || cmd == nil {
		t.Fatal("enter on the pinned row is handled")
	}
	if _, isPin := cmd().(PinnedMsg); !isPin {
		t.Errorf("enter sent %T, want PinnedMsg", cmd())
	}

	// down leaves it, back on the first data row.
	m = press(m, "down")
	if m.PinnedFocused() {
		t.Error("down leaves the pinned row")
	}
	if it, ok := m.Current(); !ok || it.Name != "pear" {
		t.Errorf("down returns to the first row: %v %v", it, ok)
	}

	// Filter and marks never touch it, and the count is the data rows'.
	m = press(m, "/", "n", "a", "m", "e", "=", "f", "i", "g", "enter")
	if got := names(m.Shown()); got != "fig" {
		t.Errorf("filter shows %q", got)
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "▸ 3 changes to push") || !strings.Contains(v, "(1 of 3)") {
		t.Errorf("the pinned row survives a filter and counts only items:\n%s", v)
	}
	m = press(m, "ctrl+a")
	if got := len(m.Rows(All)); got != 1 {
		t.Errorf("mark all marks the items only: %d", got)
	}

	// Empty text removes it and returns the cursor to the list.
	m = m.SetPinned("")
	if v := ansi.Strip(m.View()); strings.Contains(v, "▸") || m.PinnedFocused() {
		t.Errorf("no pinned row:\n%s", v)
	}
	// Without a pinned row, up at the top just stays.
	if m = press(newList(), "up"); m.PinnedFocused() {
		t.Error("up never focuses a pinned row that is not there")
	}
}
