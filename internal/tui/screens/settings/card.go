package settings

import (
	"strings"

	"github.com/mugenkunou/ws-tool/internal/tui/nav"
	"github.com/mugenkunou/ws-tool/internal/tui/theme"
	"github.com/mugenkunou/ws-tool/internal/versions"
)

// Card implements nav.Summary: the version and whether ws.json loads, whether
// an update is out, which shells have completion, and what is wrong or worth
// doing.
func (m LoadedMsg) Card() (nav.Screen, nav.Card, bool) {
	c := nav.Card{Err: m.Err}
	cfg := theme.OK.Render("ws.json ok")
	if m.ConfigErr != nil {
		cfg = theme.Error.Render("ws.json fails to load")
		c.Issues = append(c.Issues, nav.Issue{Sev: nav.SevError, Screen: nav.ScreenSettings,
			Text: "ws.json does not load: " + firstLine(m.ConfigErr.Error())})
	}
	c.Lines = append(c.Lines, "ws "+m.Info.Version+" · "+cfg)

	var on []string
	for _, s := range m.Completion {
		if s.Enabled {
			on = append(on, s.Shell)
		}
	}
	if len(on) > 0 {
		c.Lines = append(c.Lines, "completion "+theme.OK.Render("on")+" in "+strings.Join(on, ", "))
	} else {
		c.Lines = append(c.Lines, theme.Muted.Render("completion off"))
	}

	var stale []string
	for _, it := range []versions.Item{m.Report.WS, m.Report.AgentTmux} {
		if it.State == versions.StateOutdated {
			stale = append(stale, it.Name)
		}
	}
	if len(stale) > 0 {
		c.Lines = append(c.Lines, theme.Warn.Render(warnMark+" updates available: "+strings.Join(stale, ", ")))
		c.Issues = append(c.Issues, nav.Issue{Sev: nav.SevInfo, Screen: nav.ScreenSettings, Advice: true,
			Text:  "A newer " + strings.Join(stale, " and ") + " is out. The Settings screen says how to upgrade.",
			Focus: SelectMsg{Name: stale[0]}})
	}

	for _, s := range m.Completion {
		if s.Current && !s.Enabled {
			c.Issues = append(c.Issues, nav.Issue{Sev: nav.SevInfo, Screen: nav.ScreenSettings, Advice: true,
				Text:  "Shell completion is off in " + s.Shell + ", the shell you use. Tab would complete ws commands, workspace paths and names.",
				Focus: SelectMsg{Name: "completion " + s.Shell}})
		}
	}
	return nav.ScreenSettings, c, true
}
