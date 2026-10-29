package cron

import (
	"fmt"
	"strings"

	"github.com/mugenkunou/ws-tool/internal/tui/format"
	"github.com/mugenkunou/ws-tool/internal/tui/nav"
	"github.com/mugenkunou/ws-tool/internal/tui/theme"
)

// Card implements nav.Summary: installed jobs, failing ones, and a crontab
// that cannot be read.
func (m LoadedMsg) Card() (nav.Screen, nav.Card, bool) {
	c := nav.Card{Lines: []string{format.Plural(m.Installed(), "job") + " installed"}}
	failing := m.Failing()
	for _, j := range failing {
		c.Issues = append(c.Issues, nav.Issue{Sev: nav.SevError, Screen: nav.ScreenCron,
			Text:  fmt.Sprintf("Cron job %s failing (exit %d)", j.Name, j.LastRun.ExitCode),
			Focus: SelectJobMsg{Name: j.Name}})
	}
	switch {
	case m.CronErr != nil:
		first, _, _ := strings.Cut(m.CronErr.Error(), "\n")
		c.Lines = append(c.Lines, theme.Warn.Render("crontab not accessible"))
		c.Issues = append(c.Issues, nav.Issue{Sev: nav.SevInfo, Screen: nav.ScreenCron, Text: "Crontab not accessible: " + first})
	case len(failing) > 0:
		c.Lines = append(c.Lines, theme.Error.Render(fmt.Sprintf("%d failing", len(failing))))
	case m.Installed() > 0:
		c.Lines = append(c.Lines, theme.OK.Render("all passing"))
	}
	return nav.ScreenCron, c, true
}
