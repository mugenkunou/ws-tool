package sessions

import (
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	wssession "github.com/mugenkunou/ws-tool/internal/session"
	"github.com/mugenkunou/ws-tool/internal/tui/env"
	"github.com/mugenkunou/ws-tool/internal/tui/keys"
	"github.com/mugenkunou/ws-tool/internal/tui/nav"
	"github.com/mugenkunou/ws-tool/internal/tui/reslist"
	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

// The grammar of spec §6.5: keys, the rows each acts on, `:` commands.

var now = time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)

// fixtureRows: the recording in progress, a month-old session and yesterday's.
func fixtureRows() []wssession.Session {
	return []wssession.Session{
		{Tag: "live", StartedAt: now.Add(-time.Hour), Active: true, SizeBytes: 100},
		{Tag: "new1", StartedAt: now.Add(-24 * time.Hour), SizeBytes: 2000},
		{Tag: "old1", StartedAt: now.Add(-40 * 24 * time.Hour), SizeBytes: 3000},
		{Tag: "old2", StartedAt: now.Add(-50 * 24 * time.Hour), SizeBytes: 4000},
	}
}

func screen(t *testing.T, rows []wssession.Session) nav.Component {
	t.Helper()
	e := env.Env{LogDir: "/ws/logs", Config: wsconfig.Default(), Clock: func() time.Time { return now }}
	c, _ := New(e).Update(nav.ResizeMsg{Width: 120, Height: 20})
	c, _ = c.Update(LoadedMsg{Sessions: rows})
	return c
}

func keyMsg(k string) tea.KeyPressMsg {
	switch k {
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "ctrl+d":
		return tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl}
	}
	return tea.KeyPressMsg{Code: rune(k[0]), Text: k}
}

func press(c nav.Component, ks ...string) nav.Component {
	for _, k := range ks {
		c, _ = c.Update(keyMsg(k))
	}
	return c
}

func typeText(c nav.Component, s string) nav.Component {
	for _, r := range s {
		c = press(c, string(r))
	}
	return c
}

// submit presses enter and delivers what it produces back to the screen.
func submit(c nav.Component) nav.Component {
	c, cmd := c.Update(keyMsg("enter"))
	queue := []tea.Cmd{cmd}
	for steps := 0; len(queue) > 0 && steps < 50; steps++ {
		run := queue[0]
		queue = queue[1:]
		if run == nil {
			continue
		}
		switch msg := run().(type) {
		case nil:
		case tea.BatchMsg:
			queue = append(queue, msg...)
		default:
			var next tea.Cmd
			c, next = c.Update(msg)
			queue = append(queue, next)
		}
	}
	return c
}

func plain(c nav.Component) string { return ansi.Strip(c.View()) }

func command(c nav.Component, line string) (nav.Component, tea.Cmd) {
	return c.Update(nav.CommandMsg{Words: strings.Fields(line), Line: line})
}

// TestKeysAreTheSpecTable: the footer is the spec's keys, letters and labels,
// in the table's order.
func TestKeysAreTheSpecTable(t *testing.T) {
	var got []string
	for _, b := range keys.Sessions().ShortHelp() {
		got = append(got, b.Help().Key+" "+b.Help().Desc)
	}
	want := []string{"enter show", "r record", "p prune", "ctrl+d delete"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("keys = %v, want %v", got, want)
	}
}

// TestEveryActionDeclaresItsScope: each key acts on the rows spec §6.5 says.
// Prune covers all rows (`session rm --older-than` takes no rows), so it is declared as
// acting on none: it ignores the marks and the filter.
func TestEveryActionDeclaresItsScope(t *testing.T) {
	want := map[string]reslist.Scope{
		"enter": reslist.Cursor, "r": reslist.Global, "p": reslist.Global, "ctrl+d": reslist.Selected,
	}
	got := map[string]reslist.Scope{}
	for _, d := range New(env.Env{}).Declared() {
		got[d.Key.Keys()[0]] = d.Scope
	}
	for k, s := range want {
		if g, ok := got[k]; !ok || g != s {
			t.Errorf("key %s: scope %v (declared %v), want %v", k, g, ok, s)
		}
	}
	if len(got) != len(want) {
		t.Errorf("declared keys = %v, want exactly %v", got, want)
	}
}

// TestRemovedKeysDoNothing: s, x, F and P are gone.
func TestRemovedKeysDoNothing(t *testing.T) {
	for _, k := range []string{"s", "x", "F", "P"} {
		c, cmd := screen(t, fixtureRows()).Update(keyMsg(k))
		if c.CapturesKeys() || cmd != nil {
			t.Errorf("%q still does something", k)
		}
	}
}

// TestNoHardcodedKeyNames: text names keys through their bindings.
func TestNoHardcodedKeyNames(t *testing.T) {
	src, err := os.ReadFile("sessions.go")
	if err != nil {
		t.Fatal(err)
	}
	if re := regexp.MustCompile(`"[^"]*\b[Pp]ress [A-Za-z]\b`); re.Match(src) {
		t.Errorf("sessions.go names a key in text: %s", re.Find(src))
	}
	if want := "Press " + keys.Sessions().Record.Help().Key + " to record one."; !strings.Contains(plain(screen(t, nil)), want) {
		t.Errorf("empty list lacks %q:\n%s", want, plain(screen(t, nil)))
	}
}

// TestEnterShowsTheCursorRowsTranscript: enter opens the viewer for the cursor
// row, c toggles commands only, esc returns.
func TestEnterShowsTheCursorRowsTranscript(t *testing.T) {
	c := press(screen(t, fixtureRows()), "space", "down") // a mark elsewhere
	c, cmd := c.Update(keyMsg("enter"))
	m := c.(Model)
	if cmd == nil || m.level != levelViewer || m.viewTag != "new1" {
		t.Fatalf("enter: level %v tag %q", m.level, m.viewTag)
	}
	c = press(c, "c")
	if c.(Model).viewMode != modeCommands {
		t.Error("c should toggle commands only")
	}
	c = press(c, "esc")
	if c.(Model).level != levelList {
		t.Error("esc should return to the list")
	}
}

// TestDeleteActsOnMarksElseCursor: ctrl+d always opens the checklist, and never
// offers the recording in progress.
func TestDeleteActsOnMarksElseCursor(t *testing.T) {
	v := plain(press(screen(t, fixtureRows()), "down", "ctrl+d"))
	if !strings.Contains(v, "Delete 1 session") || !strings.Contains(v, "/ws/logs/new1") || strings.Contains(v, "old1") {
		t.Errorf("cursor row only:\n%s", v)
	}
	v = plain(press(screen(t, fixtureRows()), "space", "down", "down", "space", "ctrl+d"))
	if !strings.Contains(v, "Delete 1 session") || !strings.Contains(v, "/ws/logs/old1") || strings.Contains(v, "/ws/logs/live") {
		t.Errorf("marked rows, minus the recording:\n%s", v)
	}
	if c := press(screen(t, fixtureRows()), "ctrl+d"); c.CapturesKeys() {
		t.Errorf("the recording itself is not offered:\n%s", plain(c))
	}
}

// TestRecordPromptsForATag: r acts on no row, and refuses while recording.
func TestRecordPromptsForATag(t *testing.T) {
	c := press(screen(t, fixtureRows()[1:]), "r")
	if !c.CapturesKeys() || !strings.Contains(plain(c), "Record a shell session") {
		t.Errorf("r should prompt:\n%s", plain(c))
	}
	c, cmd := screen(t, fixtureRows()).Update(keyMsg("r"))
	if c.CapturesKeys() || cmd == nil {
		t.Error("r while recording should only say so")
	}
}

// TestPruneCoversAllRowsAndAsksAnAge: p ignores the marks and the filter, asks
// for an age.
func TestPruneCoversAllRowsAndAsksAnAge(t *testing.T) {
	c := press(screen(t, fixtureRows()), "down", "space", "p") // one mark
	if !c.CapturesKeys() || !strings.Contains(plain(c), "Prune sessions older than") {
		t.Fatalf("p should prompt for an age:\n%s", plain(c))
	}
	c = submit(typeText(c, "30d"))
	v := plain(c)
	if !strings.Contains(v, "/ws/logs/old1") || !strings.Contains(v, "/ws/logs/old2") || strings.Contains(v, "new1") || strings.Contains(v, "/ws/logs/live") {
		t.Errorf("every session older than 30d, marked or not:\n%s", v)
	}
	if !strings.Contains(v, "[x]") || strings.Contains(v, "[ ]") {
		t.Errorf("prune plans start checked:\n%s", v)
	}
}

// TestCommandsAreTheSpecList: the `:` commands are spec §6.5's.
func TestCommandsAreTheSpecList(t *testing.T) {
	var names []string
	for _, c := range New(env.Env{}).Commands() {
		names = append(names, c.Name)
	}
	if strings.Join(names, "|") != "stop|grep" {
		t.Errorf("commands = %v, want [stop grep]", names)
	}
}

// TestStopRoutesThroughCommandMsg: `:stop` opens the stop checklist when a
// recording is in progress, and says so when not.
func TestStopRoutesThroughCommandMsg(t *testing.T) {
	c, _ := command(screen(t, fixtureRows()), "stop")
	if v := plain(c); !c.CapturesKeys() || !strings.Contains(v, "Stop recording session live") {
		t.Errorf("stop checklist:\n%s", v)
	}
	c, cmd := command(screen(t, fixtureRows()[1:]), "stop")
	if c.CapturesKeys() || cmd == nil {
		t.Error("no recording: a notice, no dialog")
	}
}

// TestGrepNarrowsTheListAndEscClearsIt: `:grep <text>` keeps the sessions with a
// match; enter opens the first match; esc clears it before leaving the screen.
func TestGrepNarrowsTheListAndEscClearsIt(t *testing.T) {
	c, cmd := command(screen(t, fixtureRows()), "grep kube ctl")
	if cmd == nil || c.(Model).pending != "kube ctl" {
		t.Fatal(":grep should search for the words joined by a space")
	}
	c, _ = c.Update(grepMsg{query: "kube ctl", matches: []wssession.Match{{Tag: "old1", Line: 3}, {Tag: "old1", Line: 9}, {Tag: "new1", Line: 1}}})
	v := plain(c)
	if strings.Contains(v, "old2") || strings.Contains(v, "live") || !strings.Contains(v, "old1") || !strings.Contains(v, "new1") {
		t.Errorf("only sessions with a match:\n%s", v)
	}
	if !strings.Contains(v, "grep kube ctl") || !strings.Contains(v, "2 sessions") {
		t.Errorf("the header names the grep:\n%s", v)
	}

	// enter opens the viewer at the first line with the text.
	c, _ = c.Update(keyMsg("enter"))
	text := strings.Repeat("line\n", 30) + "run KUBE ctl get pods\n" + strings.Repeat("tail\n", 30)
	c, _ = c.Update(showMsg{tag: c.(Model).viewTag, mode: modeFull, text: text})
	vm := c.(Model)
	if got := vm.viewer.YOffset(); got != 30 {
		t.Errorf("viewer offset = %d, want 30 (the first match)", got)
	}
	c = press(c, "esc")

	// esc clears the grep first, then asks the app to go up.
	c, cmd = c.Update(keyMsg("esc"))
	if cmd != nil || c.(Model).query != "" || !strings.Contains(plain(c), "old2") {
		t.Errorf("esc should clear the grep:\n%s", plain(c))
	}
	if _, cmd = c.Update(keyMsg("esc")); cmd == nil {
		t.Error("a second esc should go up")
	} else if _, ok := cmd().(nav.UpMsg); !ok {
		t.Errorf("a second esc: %#v", cmd())
	}
}

// TestGrepWithNoMatchesSaysSo and an older answer cannot replace a newer one.
func TestGrepWithNoMatchesSaysSo(t *testing.T) {
	c, _ := command(screen(t, fixtureRows()), "grep nothing")
	c, cmd := c.Update(grepMsg{query: "nothing"})
	if cmd == nil || c.(Model).query != "" || !strings.Contains(plain(c), "old2") {
		t.Fatalf("no matches should leave the list alone:\n%s", plain(c))
	}
	if n := cmd().(nav.NoticeMsg); !strings.Contains(n.Text, "No matches for nothing") {
		t.Errorf("notice = %q", n.Text)
	}
	c, _ = command(screen(t, fixtureRows()), "grep new")
	c, _ = c.Update(grepMsg{query: "old", matches: []wssession.Match{{Tag: "old1"}}})
	if c.(Model).query != "" {
		t.Error("a stale answer replaced the grep")
	}
	if _, cmd := command(screen(t, fixtureRows()), "grep"); cmd == nil {
		t.Error("`grep` with no text should say how to use it")
	}
}
