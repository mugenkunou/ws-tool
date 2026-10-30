package app

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/mugenkunou/ws-tool/internal/tui/nav"
	"github.com/mugenkunou/ws-tool/internal/tui/reslist"
	"github.com/mugenkunou/ws-tool/internal/tui/tuitest"
)

// commandNames are the names of a screen's `:` commands, sorted.
func commandNames(m Model, s nav.Screen) []string {
	var out []string
	for _, c := range commander(m.screens[s]) {
		out = append(out, c.Name)
	}
	slices.Sort(out)
	return out
}

// declaredScopes are a screen's declared keys as "key:scope", in order.
func declaredScopes(t *testing.T, m Model, s nav.Screen) []string {
	t.Helper()
	d, ok := m.screens[s].(interface{ Declared() []reslist.Decl })
	if !ok {
		t.Fatalf("%s declares nothing", s.Title())
	}
	names := map[reslist.Scope]string{reslist.Global: "global", reslist.Cursor: "cursor", reslist.Selected: "selected", reslist.All: "all"}
	var got []string
	for _, a := range d.Declared() {
		got = append(got, a.Key.Keys()[0]+":"+names[a.Scope])
	}
	return got
}

// assertNoHardcodedKeyNames: a screen's texts name keys through the bindings,
// never as letters in a string ("Press a…", "H to view").
func assertNoHardcodedKeyNames(t *testing.T, file string) {
	t.Helper()
	re := regexp.MustCompile(`[Pp]ress [A-Za-z]\b|\b[A-Za-z] to view|"[A-Za-z] (to|then) `)
	src, err := os.ReadFile(filepath.Join("..", "screens", file))
	if err != nil {
		t.Fatal(err)
	}
	for i, line := range strings.Split(string(src), "\n") {
		if re.MatchString(line) {
			t.Errorf("%s:%d names a key in text: %s", file, i+1, strings.TrimSpace(line))
		}
	}
}

// TestTrashCommands: the `.` menu is the spec's §6.5 list; status is the header.
func TestTrashCommands(t *testing.T) {
	want := []string{"disable", "empty", "enable"}
	if got := commandNames(loaded(t, 100, 30), nav.ScreenTrash); !slices.Equal(got, want) {
		t.Errorf("trash commands = %v, want %v", got, want)
	}
}

// TestTrashScopes: every key declares the scope of the spec's Rows column.
func TestTrashScopes(t *testing.T) {
	want := []string{"r:selected", "ctrl+d:selected", "p:all"}
	if got := declaredScopes(t, loaded(t, 100, 30), nav.ScreenTrash); !slices.Equal(got, want) {
		t.Errorf("trash declares %v, want %v", got, want)
	}
}

// TestTrashEmptyIsACommandAndStartsUnchecked: the menu's `e` opens the checklist for
// the whole trash with its one action unchecked (spec §6.7), and no key empties.
func TestTrashEmptyIsACommandAndStartsUnchecked(t *testing.T) {
	m := send(loaded(t, 100, 30), press("9"))
	m = menuKeys(t, m, "e")
	v := tuitest.Plain(view(m))
	if !strings.Contains(v, "[ ] Permanently delete 2 items") {
		t.Fatalf("empty checklist is not unchecked:\n%s", v)
	}
	tuitest.Golden(t, "trash_empty_checklist", view(m))
	// Marks do not narrow it: it is `trash empty`, which takes no targets.
	m = send(loaded(t, 100, 30), press("9"), press("space"))
	m = menuKeys(t, m, "e")
	if v := tuitest.Plain(view(m)); !strings.Contains(v, "Permanently delete 2 items") {
		t.Fatalf("marks narrowed the menu's empty:\n%s", v)
	}
}

// TestTrashKeysActOnTheirRows: restore and delete act on the marked rows, else
// the cursor row.
func TestTrashKeysActOnTheirRows(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	for _, c := range []struct {
		keys []string
		want string
	}{
		{[]string{"r"}, "Restore 1 trash item"},
		{[]string{"ctrl+d"}, "Delete 1 trash item"},
		// The second item records no original path, so restore names it as skipped.
		{[]string{"ctrl+a", "r"}, "Skipped: build: no original path recorded"},
		{[]string{"ctrl+a", "ctrl+d"}, "Delete 2 trash items"},
	} {
		m := send(loaded(t, 100, 30), press("9"))
		for _, k := range c.keys {
			m = send(m, press(k))
		}
		if v := tuitest.Plain(view(m)); !strings.Contains(v, c.want) {
			t.Errorf("%v: want %q:\n%s", c.keys, c.want, v)
		}
	}
}

// TestTrashPruneAsksForAnAge: p prompts for an age first; nothing is applied
// from the key.
func TestTrashPruneAsksForAnAge(t *testing.T) {
	m := send(loaded(t, 100, 30), press("9"), press("p"))
	if v := tuitest.Plain(view(m)); !strings.Contains(v, "older than (age)") {
		t.Fatalf("no age prompt:\n%s", v)
	}
}

func TestTrashNoHardcodedKeyNames(t *testing.T) { assertNoHardcodedKeyNames(t, "trash/trash.go") }
