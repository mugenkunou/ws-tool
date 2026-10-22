package reslist

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/mugenkunou/ws-tool/internal/resource"
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
	m = press(m, "A")
	if len(m.marked()) != 3 {
		t.Errorf("A marked %d", len(m.marked()))
	}
	m = press(m, "A")
	if len(m.marked()) != 0 {
		t.Errorf("second A left %d marked", len(m.marked()))
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
	m := press(newList(), "i")
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
	m := newList().Declare([]Decl{
		{Key: kb("o"), Scope: Cursor}, {Key: kb("d"), Scope: Selected}, {Key: kb("f"), Scope: All},
	})
	if v := ansi.Strip(press(m, "space").View()); !strings.Contains(v, "1 marked — d f act on them") {
		t.Errorf("marks hint missing:\n%s", v)
	}
	if v := ansi.Strip(press(m, "/", "p", "enter").View()); !strings.Contains(v, "f act on these 2") {
		t.Errorf("filter hint missing:\n%s", v)
	}
}

func kb(k string) key.Binding { return key.NewBinding(key.WithKeys(k), key.WithHelp(k, k)) }
