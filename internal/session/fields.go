package session

import (
	"github.com/mugenkunou/ws-tool/internal/resource"
	"github.com/mugenkunou/ws-tool/internal/style"
)

// Schema describes recorded sessions for the CLI (`ws session ls`) and the TUI
// Sessions screen.
func Schema(v resource.View) resource.Schema[Session] {
	return resource.Schema[Session]{
		Name: "session",
		ID:   func(s Session) string { return s.Tag },
		Fields: []resource.Field[Session]{
			{Key: "tag", Title: "Tag", Value: func(s Session) any { return s.Tag }},
			{Key: "started_at", Title: "Started", Value: func(s Session) any { return s.StartedAt }},
			{Key: "ended_at", Title: "Ended", Value: func(s Session) any { return s.EndedAt }},
			{Key: "duration_sec", Title: "Duration", Value: func(s Session) any { return s.DurationSec },
				Text: func(s Session) string {
					if s.Active {
						return "—"
					}
					return style.Duration(s.DurationSec)
				}},
			{Key: "commands", Title: "Cmds", Value: func(s Session) any { return s.Commands }},
			{Key: "size_bytes", Title: "Size", Value: func(s Session) any { return s.SizeBytes },
				Text: func(s Session) string { return style.HumanBytes(s.SizeBytes) }},
			{Key: "active", Title: "State", Value: func(s Session) any { return s.Active },
				Text: func(s Session) string {
					if s.Active {
						return "recording"
					}
					return "done"
				}},
		},
		Columns: []string{"tag", "started_at", "duration_sec", "commands", "size_bytes", "active"},
	}
}
