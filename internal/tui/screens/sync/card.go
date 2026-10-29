package sync

import (
	"fmt"

	"github.com/mugenkunou/ws-tool/internal/mirror"
	"github.com/mugenkunou/ws-tool/internal/tui/format"
	"github.com/mugenkunou/ws-tool/internal/tui/nav"
	"github.com/mugenkunou/ws-tool/internal/tui/theme"
)

// Card implements nav.Summary: the last push's age and what is pending.
//
// Severities: nothing here is an error, because a push still works in every
// case below; they are warnings because the next push will stop and ask, or
// will surprise. A load that fails (rsync missing, the mirror's folder gone)
// is Card.Err. A workspace with no target is a muted line and no issue: sync is
// optional.
func (m LoadedMsg) Card() (nav.Screen, nav.Card, bool) {
	c := nav.Card{Err: m.Err}
	if m.Err != nil {
		return nav.ScreenSync, c, true
	}
	if !m.Configured {
		c.Lines = []string{theme.Muted.Render("no mirror set up")}
		return nav.ScreenSync, c, true
	}
	s := m.Status
	if l := s.LastPush; l != nil {
		c.Lines = append(c.Lines, "last push "+format.Age(m.At.Sub(l.Time))+" ago")
	} else {
		c.Lines = append(c.Lines, theme.Muted.Render("never pushed from this machine"))
	}
	if n := s.PendingChanges + s.PendingDeletes; n == 0 {
		c.Lines = append(c.Lines, theme.OK.Render("mirror up to date"))
	} else {
		c.Lines = append(c.Lines, fmt.Sprintf("%d to copy · %d to delete", s.PendingChanges, s.PendingDeletes))
	}
	switch s.Stamp {
	case mirror.StampWiped:
		c.Issues = append(c.Issues, nav.Issue{Sev: nav.SevWarn, Screen: nav.ScreenSync,
			Text: "The mirror was emptied since the last push; the next push re-uploads everything"})
	case mirror.StampForeign:
		c.Issues = append(c.Issues, nav.Issue{Sev: nav.SevWarn, Screen: nav.ScreenSync,
			Text: "Another push wrote the mirror; the next push asks before overwriting it"})
	}
	for _, q := range m.Questions {
		if q.Kind == mirror.AskGuard {
			c.Issues = append(c.Issues, nav.Issue{Sev: nav.SevWarn, Screen: nav.ScreenSync,
				Text: fmt.Sprintf("The next push deletes %s from the mirror, over the delete guard", format.Plural(s.PendingDeletes, "file"))})
		}
	}
	return nav.ScreenSync, c, true
}
