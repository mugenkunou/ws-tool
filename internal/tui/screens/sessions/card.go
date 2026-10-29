package sessions

import (
	"fmt"

	"github.com/mugenkunou/ws-tool/internal/tui/format"
	"github.com/mugenkunou/ws-tool/internal/tui/nav"
	"github.com/mugenkunou/ws-tool/internal/tui/theme"
)

// Card implements nav.Summary: live and ended sessions, and live sessions
// whose recorder stopped.
func (m LoadedMsg) Card() (nav.Screen, nav.Card, bool) {
	if m.Missing {
		return nav.ScreenSessions, nav.Card{Err: m.Err, Lines: []string{theme.Muted.Render("agent-tmux not installed")}}, true
	}
	live := m.Live()
	c := nav.Card{Err: m.Err, Lines: []string{fmt.Sprintf("%d live · %d ended", live, len(m.Sessions)-live)}}
	stopped := 0
	for _, s := range m.Sessions {
		if s.Recorder == "stopped" {
			stopped++
		}
	}
	switch {
	case stopped > 0:
		c.Lines = append(c.Lines, theme.Warn.Render(format.Plural(stopped, "recorder")+" stopped"))
		c.Issues = append(c.Issues, nav.Issue{Sev: nav.SevWarn, Screen: nav.ScreenSessions,
			Text:   format.Plural(stopped, "live session") + " no longer recorded (recorder stopped)",
			Filter: "recorder=stopped"})
	case live > 0:
		c.Lines = append(c.Lines, theme.Error.Render("● recording"))
	default:
		c.Lines = append(c.Lines, theme.Muted.Render("nothing live"))
	}
	return nav.ScreenSessions, c, true
}
