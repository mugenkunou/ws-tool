package scratch

import (
	"fmt"

	"github.com/mugenkunou/ws-tool/internal/style"
	"github.com/mugenkunou/ws-tool/internal/tui/format"
	"github.com/mugenkunou/ws-tool/internal/tui/nav"
	"github.com/mugenkunou/ws-tool/internal/tui/theme"
)

// Card implements nav.Summary: how many, how big, and the newest.
func (m LoadedMsg) Card() (nav.Screen, nav.Card, bool) {
	c := nav.Card{Err: m.Err, Lines: []string{fmt.Sprintf("%s · %s", format.Plural(len(m.Entries), "dir"), style.HumanBytes(m.TotalBytes()))}}
	if len(m.Entries) > 0 {
		c.Lines = append(c.Lines, theme.Muted.Render("newest ")+m.Entries[0].Name)
	}
	return nav.ScreenScratch, c, true
}
