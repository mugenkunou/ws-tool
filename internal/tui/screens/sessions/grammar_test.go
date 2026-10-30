package sessions

import (
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/mugenkunou/ws-tool/internal/agenttmux"
	"github.com/mugenkunou/ws-tool/internal/agenttmux/agenttmuxtest"
	"github.com/mugenkunou/ws-tool/internal/tui/env"
	"github.com/mugenkunou/ws-tool/internal/tui/handover"
	"github.com/mugenkunou/ws-tool/internal/tui/keys"
	"github.com/mugenkunou/ws-tool/internal/tui/nav"
	"github.com/mugenkunou/ws-tool/internal/tui/reslist"
	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

// The grammar of spec §6.5: keys, the rows each acts on, the agent-tmux verb
// each runs. agent-tmux is a fake on PATH (agenttmuxtest).

func TestMain(m *testing.M) {
	unhide := agenttmuxtest.Hide()
	code := m.Run()
	unhide()
	os.Exit(code)
}

var now = time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)

// fixtureRows: two live sessions (one from an older agent-tmux, without a
// recording) and two ended recordings.
func fixtureRows() []agenttmux.Session {
	return []agenttmux.Session{
		{Name: "web", ID: "web-2026-10-01-0900", State: "live", Recorder: "running", Clients: 2, StartedAt: "2026-10-01T09:00:00Z", Bytes: 100},
		{Name: "legacy", State: "live", Recorder: "none", StartedAt: "2026-09-30T09:00:00Z"},
		{Name: "web", ID: "web-2026-09-20-1000", State: "ended", StartedAt: "2026-09-20T10:00:00Z", EndedAt: "2026-09-20T11:00:00Z", Bytes: 2000},
		{Name: "build", ID: "build-2026-09-10-0800", State: "ended", StartedAt: "2026-09-10T08:00:00Z", EndedAt: "2026-09-10T08:30:00Z", Bytes: 3000},
	}
}

// fakeAgentTmux answers ls and grep like agent-tmux, and records every call.
func fakeAgentTmux(t *testing.T) (log string) {
	t.Helper()
	return agenttmuxtest.Fake(t, `case "$1" in
ls) echo '{"sessions":[{"name":"web","id":"web-2026-10-01-0900","state":"live","recorder":"running","owner":"","clients":2,"command":"bash","path":"/w","host":"box","started_at":"2026-10-01T09:00:00Z","ended_at":"","history_starts_at":"2026-10-01T09:00:00Z","bytes":100}]}' ;;
grep) echo '{"matches":[{"name":"web","id":"web-2026-09-20-1000","line":40,"text":"kube ctl get pods"},{"name":"build","id":"build-2026-09-10-0800","line":3,"text":"KUBE CTL apply"}],"truncated":false}' ;;
esac`)
}

// handovers records what the screen hands the terminal to.
func handovers(t *testing.T) *[][]string {
	t.Helper()
	var got [][]string
	old := handOver
	handOver = func(c *exec.Cmd, tag any) tea.Cmd {
		got = append(got, append([]string{}, c.Args...))
		return func() tea.Msg { return handover.DoneMsg{Tag: tag} }
	}
	t.Cleanup(func() { handOver = old })
	return &got
}

func screen(t *testing.T, rows []agenttmux.Session) nav.Component {
	t.Helper()
	e := env.Env{Config: wsconfig.Default(), Clock: func() time.Time { return now }}
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

// drive runs cmd and every command it leads to, delivering their messages to
// the screen. Notices are collected.
func drive(c nav.Component, cmd tea.Cmd) (nav.Component, []string) {
	var notices []string
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
		case nav.NoticeMsg:
			notices = append(notices, msg.Text)
		default:
			var next tea.Cmd
			c, next = c.Update(msg)
			queue = append(queue, next)
		}
	}
	return c, notices
}

// hit presses k and drives what it produces.
func hit(c nav.Component, k string) (nav.Component, []string) {
	c, cmd := c.Update(keyMsg(k))
	return drive(c, cmd)
}

func plain(c nav.Component) string { return ansi.Strip(c.View()) }

// TestKeysAreTheSpecTable: the footer is the spec's keys, letters and labels,
// in the table's order.
func TestKeysAreTheSpecTable(t *testing.T) {
	var got []string
	for _, b := range keys.Sessions().ShortHelp() {
		got = append(got, b.Help().Key+" "+b.Help().Desc)
	}
	want := []string{"enter open", "v show", "a new", "s grep", "x kill", "ctrl+d delete"}
	if !slices.Equal(got, want) {
		t.Errorf("keys = %v, want %v", got, want)
	}
}

// TestEveryActionDeclaresItsScope: each key acts on the rows spec §6.5 says.
func TestEveryActionDeclaresItsScope(t *testing.T) {
	want := map[string]reslist.Scope{
		"enter": reslist.Cursor, "v": reslist.Cursor, "a": reslist.Global, "s": reslist.Global,
		"x": reslist.Selected, "ctrl+d": reslist.Selected,
	}
	got := map[string]reslist.Scope{}
	for _, d := range New(env.Env{}).Declared() {
		got[d.Key.Keys()[0]] = d.Scope
	}
	if len(got) != len(want) {
		t.Errorf("declared keys = %v, want exactly %v", got, want)
	}
	for k, s := range want {
		if g, ok := got[k]; !ok || g != s {
			t.Errorf("key %s: scope %v (declared %v), want %v", k, g, ok, s)
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
	if want := "Press " + keys.Sessions().New.Help().Key + " to start one."; !strings.Contains(plain(screen(t, nil)), want) {
		t.Errorf("empty list lacks %q:\n%s", want, plain(screen(t, nil)))
	}
}

// TestLoadIsAgentTmuxLs: the list is `agent-tmux ls --json`, live and ended
// rows together with their state and recorder.
func TestLoadIsAgentTmuxLs(t *testing.T) {
	log := fakeAgentTmux(t)
	msg := Load()().(LoadedMsg)
	if msg.Err != nil || msg.Missing || len(msg.Sessions) != 1 || msg.Live() != 1 {
		t.Fatalf("loaded %+v", msg)
	}
	if calls := agenttmuxtest.Calls(t, log); !slices.Equal(calls[0], []string{"ls", "--json"}) {
		t.Errorf("argv = %v", calls)
	}
	v := plain(screen(t, fixtureRows()))
	for _, want := range []string{"State", "Rec", "live", "ended", "● rec", "none", "build"} {
		if !strings.Contains(v, want) {
			t.Errorf("list lacks %q:\n%s", want, v)
		}
	}
}

// TestMissingAgentTmuxGreysOut: the install lines, and keys only say why.
func TestMissingAgentTmuxGreysOut(t *testing.T) {
	agenttmuxtest.Missing(t)
	msg := Load()().(LoadedMsg)
	if !msg.Missing || msg.Err != nil {
		t.Fatalf("loaded %+v", msg)
	}
	c, _ := New(env.Env{}).Update(nav.ResizeMsg{Width: 120, Height: 20})
	c, _ = c.Update(msg)
	v := plain(c)
	for _, l := range agenttmux.InstallLines() {
		if !strings.Contains(v, l) {
			t.Errorf("greyed screen lacks %q:\n%s", l, v)
		}
	}
	for _, k := range []string{"a", "s", "enter", "x", "ctrl+d"} {
		next, cmd := c.Update(keyMsg(k))
		if next.CapturesKeys() {
			t.Errorf("%s opened a dialog without agent-tmux", k)
		}
		if k == "a" || k == "s" {
			if n, ok := cmd().(nav.NoticeMsg); !ok || !strings.Contains(n.Text, "not installed") {
				t.Errorf("%s: %#v", k, cmd())
			}
		}
	}
}

// TestEnterOpensLiveAndShowsEnded: enter attaches to a live session and pages
// an ended recording; v pages any recording.
func TestEnterOpensLiveAndShowsEnded(t *testing.T) {
	fakeAgentTmux(t)
	for _, c := range []struct {
		keys []string
		want []string
	}{
		{[]string{"enter"}, []string{"open", "web"}},
		{[]string{"down", "down", "enter"}, []string{"show", "web-2026-09-20-1000"}},
		{[]string{"v"}, []string{"show", "web"}}, // a live session is shown by name
		{[]string{"down", "down", "down", "v"}, []string{"show", "build-2026-09-10-0800"}},
	} {
		got := handovers(t)
		s := screen(t, fixtureRows())
		ks := c.keys
		_, _ = hit(press(s, ks[:len(ks)-1]...), ks[len(ks)-1])
		if len(*got) != 1 || !strings.HasSuffix((*got)[0][0], "agent-tmux") || !slices.Equal((*got)[0][1:], c.want) {
			t.Errorf("%v: handed over %q, want agent-tmux %q", c.keys, *got, c.want)
		}
	}
}

// TestShowWithoutRecordingSaysSo: a session from an older agent-tmux has no
// recording to page.
func TestShowWithoutRecordingSaysSo(t *testing.T) {
	fakeAgentTmux(t)
	got := handovers(t)
	_, notices := hit(press(screen(t, fixtureRows()), "down"), "v")
	if len(*got) != 0 || len(notices) != 1 || !strings.Contains(notices[0], "no recording") {
		t.Errorf("handed over %q, notices %q", *got, notices)
	}
}

// TestNewPromptsThenCreates: a asks a name, then `agent-tmux create <name>`
// runs in the foreground, which attaches.
func TestNewPromptsThenCreates(t *testing.T) {
	fakeAgentTmux(t)
	got := handovers(t)
	c := press(screen(t, fixtureRows()), "a")
	if !c.CapturesKeys() || !strings.Contains(plain(c), "New session") {
		t.Fatalf("a should prompt:\n%s", plain(c))
	}
	_, _ = hit(typeText(c, "api"), "enter")
	if len(*got) != 1 || !slices.Equal((*got)[0][1:], []string{"create", "api"}) {
		t.Errorf("handed over %q", *got)
	}
}

// TestGrepListsMatchesAndEnterShowsOne: s asks the text, the matches are a
// list, enter pages the match's recording, esc returns to the sessions.
func TestGrepListsMatchesAndEnterShowsOne(t *testing.T) {
	log := fakeAgentTmux(t)
	got := handovers(t)
	c := press(screen(t, fixtureRows()), "s")
	if !c.CapturesKeys() || !strings.Contains(plain(c), "Grep every recording") {
		t.Fatalf("s should prompt:\n%s", plain(c))
	}
	c, _ = hit(typeText(c, "kube  ctl"), "enter")
	v := plain(c)
	for _, want := range []string{"grep kube ctl", "2 matches", "kube ctl get pods", "KUBE CTL apply", "build-2026-09-10-0800"} {
		if !strings.Contains(v, want) {
			t.Errorf("matches lack %q:\n%s", want, v)
		}
	}
	if calls := agenttmuxtest.Calls(t, log); !slices.Equal(calls[len(calls)-1], []string{"grep", "--json", "kube ctl"}) {
		t.Errorf("argv = %v", calls)
	}
	c, _ = hit(press(c, "down"), "enter")
	if len(*got) != 1 || !slices.Equal((*got)[0][1:], []string{"show", "build-2026-09-10-0800"}) {
		t.Errorf("handed over %q", *got)
	}
	c = press(c, "esc")
	if v := plain(c); strings.Contains(v, "kube ctl get pods") || !strings.Contains(v, "Clients") {
		t.Errorf("esc should return to the sessions:\n%s", v)
	}
	if _, cmd := c.Update(keyMsg("esc")); cmd == nil {
		t.Error("a second esc should go up")
	} else if _, ok := cmd().(nav.UpMsg); !ok {
		t.Errorf("a second esc: %#v", cmd())
	}
}

// TestGrepNoMatchesKeepsTheList: s with text that matches nothing is a notice,
// and the sessions list stays.
func TestGrepNoMatchesKeepsTheList(t *testing.T) {
	agenttmuxtest.Fake(t, `echo '{"matches":[],"truncated":false}'`)
	c := press(screen(t, fixtureRows()), "s")
	if !c.CapturesKeys() {
		t.Fatalf("s should prompt:\n%s", plain(c))
	}
	c, notices := hit(typeText(c, "nothing"), "enter")
	if len(notices) != 1 || notices[0] != "No matches for nothing" || !strings.Contains(plain(c), "legacy") {
		t.Errorf("notices %q:\n%s", notices, plain(c))
	}
}

// TestKillOffersOnlyLiveSessions: x on marks takes the live ones, and applying
// runs `agent-tmux kill <name>` for each, then reloads.
func TestKillOffersOnlyLiveSessions(t *testing.T) {
	log := fakeAgentTmux(t)
	c := press(screen(t, fixtureRows()), "space", "down", "down", "space", "x")
	v := plain(c)
	if !strings.Contains(v, "Kill 1 session") || !strings.Contains(v, "Kill session web (2 clients attached)") || strings.Contains(v, "build") {
		t.Fatalf("kill checklist:\n%s", v)
	}
	c, _ = hit(c, "enter")        // apply
	c, notices := hit(c, "enter") // close the finished checklist
	calls := agenttmuxtest.Calls(t, log)
	if !slices.ContainsFunc(calls, func(a []string) bool { return slices.Equal(a, []string{"kill", "web"}) }) {
		t.Errorf("calls %q lack kill web", calls)
	}
	if !slices.ContainsFunc(calls, func(a []string) bool { return slices.Equal(a, []string{"ls", "--json"}) }) {
		t.Errorf("no reload after kill: %q", calls)
	}
	if len(notices) == 0 {
		t.Error("no summary notice")
	}
	if c, notices := hit(press(screen(t, fixtureRows()), "down", "down"), "x"); c.CapturesKeys() || len(notices) != 1 {
		t.Errorf("x on an ended row: a notice, no dialog (%q)", notices)
	}
}

// TestDeleteOffersOnlyEndedRecordings: ctrl+d moves recordings to the trash
// with `agent-tmux rm <id>`; a live session is never offered.
func TestDeleteOffersOnlyEndedRecordings(t *testing.T) {
	log := fakeAgentTmux(t)
	c := press(screen(t, fixtureRows()), "ctrl+a", "ctrl+d")
	v := plain(c)
	if !strings.Contains(v, "Delete 2 recordings") || !strings.Contains(v, "web-2026-09-20-1000") ||
		!strings.Contains(v, "build-2026-09-10-0800") || strings.Contains(v, "web-2026-10-01-0900") || strings.Contains(v, "legacy") {
		t.Fatalf("delete checklist:\n%s", v)
	}
	c, _ = hit(c, "enter")
	_, _ = hit(c, "enter")
	calls := agenttmuxtest.Calls(t, log)
	for _, id := range []string{"web-2026-09-20-1000", "build-2026-09-10-0800"} {
		if !slices.ContainsFunc(calls, func(a []string) bool { return slices.Equal(a, []string{"rm", id}) }) {
			t.Errorf("calls %q lack rm %s", calls, id)
		}
	}
	if c, notices := hit(screen(t, fixtureRows()), "ctrl+d"); c.CapturesKeys() || len(notices) != 1 || !strings.Contains(notices[0], "Kill the session") {
		t.Errorf("ctrl+d on a live row: %q", notices)
	}
}

// TestFailedRmShowsAgentTmuxsMessage: agent-tmux's refusal reaches the user.
func TestFailedRmShowsAgentTmuxsMessage(t *testing.T) {
	agenttmuxtest.Fake(t, `[ "$1" = rm ] && { echo 'web is live; kill it first (agent-tmux kill web)' >&2; exit 1; }; echo '{"sessions":[]}'`)
	c := press(screen(t, fixtureRows()), "down", "down", "ctrl+d")
	c, _ = hit(c, "enter")
	c, notices := hit(c, "enter")
	if !strings.Contains(plain(c)+strings.Join(notices, "\n"), "1 failed") {
		t.Errorf("notices %q:\n%s", notices, plain(c))
	}
}

// TestNudgeStandsAboveTheList: a newer agent-tmux release shows its line and
// the install lines.
func TestNudgeStandsAboveTheList(t *testing.T) {
	c, _ := screen(t, fixtureRows()).Update(NudgeMsg{Nudge: agenttmux.Nudge{Installed: "v0.1.0", Latest: "v0.2.0"}})
	v := plain(c)
	for _, want := range append([]string{"agent-tmux v0.2.0 is available"}, agenttmux.InstallLines()...) {
		if !strings.Contains(v, want) {
			t.Errorf("view lacks %q:\n%s", want, v)
		}
	}
	if lines := strings.Count(v, "\n") + 1; lines > 20 {
		t.Errorf("the nudge pushed the view to %d lines, height is 20", lines)
	}
}

// TestHandoverFailureIsANotice: an agent-tmux that exits non-zero after
// having the terminal is reported, and the list reloads.
func TestHandoverFailureIsANotice(t *testing.T) {
	fakeAgentTmux(t)
	c, notices := drive(screen(t, fixtureRows()), func() tea.Msg {
		return handover.DoneMsg{Tag: handoverOpen, Err: &exec.ExitError{}}
	})
	if len(notices) != 1 || !strings.HasPrefix(notices[0], "agent-tmux open: ") {
		t.Errorf("notices %q", notices)
	}
	if len(c.(Model).list.Items()) != 1 {
		t.Error("the list should reload after a handover")
	}
}
