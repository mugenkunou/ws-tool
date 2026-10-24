package dashboard

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/mugenkunou/ws-tool/internal/tui/env"
	"github.com/mugenkunou/ws-tool/internal/tui/nav"
)

func model(areas map[nav.Screen]area) Model {
	m := New(env.Env{Clock: time.Now})
	m.areas = areas
	m.width, m.height = 100, 30
	return m
}

func collect(cmd tea.Cmd) []tea.Msg {
	var out []tea.Msg
	queue := []tea.Cmd{cmd}
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		switch msg := c().(type) {
		case nil:
		case tea.BatchMsg:
			queue = append(queue, msg...)
		default:
			out = append(out, msg)
		}
	}
	return out
}

func enter(m Model) (Model, []tea.Msg) {
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	return next.(Model), collect(cmd)
}

var passAdvice = issue{
	sev: sevInfo, screen: nav.ScreenSecrets, advice: true,
	text:    "Your pass store syncs only through its own git remote. A copy in the workspace would sync with MEGA too.",
	prefill: "dotfile add ~/.password-store --dir --to ~/Workspace/secrets/dotfiles/password-store",
}

func TestAdviceIsListedLastAndOpensTheCommandBar(t *testing.T) {
	m := model(map[nav.Screen]area{
		nav.ScreenSecrets: {loaded: true, issues: []issue{passAdvice, {sev: sevWarn, screen: nav.ScreenSecrets, text: "Pass store has no git remote"}}},
		nav.ScreenRepos:   {loaded: true, issues: []issue{{sev: sevInfo, screen: nav.ScreenRepos, text: "1 hygiene finding in 1 repo"}}},
	})
	is := m.issues()
	if len(is) != 3 || !is[2].advice || is[0].sev != sevWarn || is[1].advice {
		t.Fatalf("order: %+v", is)
	}

	m.section, m.att = sectionAttention, 2
	_, msgs := enter(m)
	if len(msgs) != 1 {
		t.Fatalf("messages: %v", msgs)
	}
	open, ok := msgs[0].(nav.OpenCommandBarMsg)
	if !ok || !strings.HasPrefix(open.Prefill, "dotfile add ~/.password-store --dir --to ") {
		t.Fatalf("enter on advice: %#v", msgs[0])
	}
}

func TestAdviceNeverCountsTowardsACardsStatus(t *testing.T) {
	if got := worst([]issue{passAdvice}); got != sevInfo+1 {
		t.Fatalf("advice alone leaves the card ✔: worst = %v", got)
	}
	if got := worst([]issue{passAdvice, {sev: sevWarn}}); got != sevWarn {
		t.Fatalf("worst = %v", got)
	}
	v := ansi.Strip(model(map[nav.Screen]area{nav.ScreenSecrets: {loaded: true, issues: []issue{passAdvice}}}).View())
	if !strings.Contains(v, "ℹ") {
		t.Errorf("advice has its own mark:\n%s", v)
	}
}

func TestProblemRowsOpenTheirScreen(t *testing.T) {
	m := model(map[nav.Screen]area{
		nav.ScreenDotfiles: {loaded: true, issues: []issue{{sev: sevError, screen: nav.ScreenDotfiles, text: "2 dotfile links broken", filter: "status!=ok"}}},
		nav.ScreenTrash:    {loaded: true, issues: []issue{{sev: sevWarn, screen: nav.ScreenTrash, text: "Soft-delete is off"}}},
	})
	m.section, m.att = sectionAttention, 0
	_, msgs := enter(m)
	var f *nav.FilterMsg
	for _, msg := range msgs {
		if fm, ok := msg.(nav.FilterMsg); ok {
			f = &fm
		}
	}
	if f == nil || f.Filter != "status!=ok" || f.Screen != nav.ScreenDotfiles {
		t.Fatalf("messages: %v", msgs)
	}
	m.att = 1
	_, msgs = enter(m)
	if len(msgs) != 1 {
		t.Fatalf("a row with no filter just opens its screen: %v", msgs)
	}
	if g, ok := msgs[0].(nav.GotoMsg); !ok || g.Screen != nav.ScreenTrash {
		t.Fatalf("messages: %v", msgs)
	}
}

func TestNoActionKeys(t *testing.T) {
	m := model(map[nav.Screen]area{nav.ScreenTrash: {loaded: true, issues: []issue{{sev: sevWarn, screen: nav.ScreenTrash, text: "x"}}}})
	m.section = sectionAttention
	for _, k := range []string{"x", "r", "c", "O", "R"} {
		if _, cmd := m.Update(tea.KeyPressMsg{Code: []rune(k)[0], Text: k}); len(collect(cmd)) != 0 {
			t.Errorf("%s does something on the Dashboard", k)
		}
	}
}
