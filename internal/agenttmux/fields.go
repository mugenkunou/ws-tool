package agenttmux

import (
	"github.com/mugenkunou/ws-tool/internal/resource"
	"github.com/mugenkunou/ws-tool/internal/style"
)

// Schema describes the rows of `agent-tmux ls --json` for the TUI Sessions
// screen. Keys are agent-tmux's JSON keys, and values are its values, so the
// JSON pane shows what `ws session ls --json` prints.
func Schema(v resource.View) resource.Schema[Session] {
	when := func(s string) string { return resource.Plain(parseTime(s)) }
	return resource.Schema[Session]{
		Name: "session",
		ID:   Session.Key,
		Fields: []resource.Field[Session]{
			{Key: "name", Title: "Session", Value: func(s Session) any { return s.Name }},
			{Key: "id", Title: "Recording", Value: func(s Session) any { return s.ID }},
			{Key: "state", Title: "State", Whole: true, Value: func(s Session) any { return s.State }},
			{Key: "recorder", Title: "Rec", Whole: true, Value: func(s Session) any { return s.Recorder },
				Text: func(s Session) string { return RecorderText(s) }},
			{Key: "owner", Title: "Owner", Value: func(s Session) any { return s.Owner }},
			{Key: "clients", Title: "Clients", Value: func(s Session) any { return s.Clients },
				Text: func(s Session) string {
					if !s.Live() {
						return ""
					}
					return resource.Plain(s.Clients)
				}},
			{Key: "command", Title: "Command", Value: func(s Session) any { return s.Command }},
			{Key: "path", Title: "Path", Value: func(s Session) any { return s.Path },
				Text: func(s Session) string {
					if s.Path == "" {
						return ""
					}
					return v.Path(s.Path)
				}},
			{Key: "host", Title: "Host", Value: func(s Session) any { return s.Host }},
			{Key: "started_at", Title: "Started", Value: func(s Session) any { return s.StartedAt },
				Text: func(s Session) string { return when(s.StartedAt) }},
			{Key: "ended_at", Title: "Ended", Value: func(s Session) any { return s.EndedAt },
				Text: func(s Session) string { return when(s.EndedAt) }},
			{Key: "history_starts_at", Title: "History from", Value: func(s Session) any { return s.HistoryStartsAt },
				Text: func(s Session) string { return when(s.HistoryStartsAt) }},
			{Key: "bytes", Title: "Size", Value: func(s Session) any { return s.Bytes },
				Text: func(s Session) string {
					if s.Bytes == 0 && s.ID == "" {
						return ""
					}
					return style.HumanBytes(s.Bytes)
				}},
		},
		Columns: []string{"name", "state", "recorder", "owner", "clients", "started_at", "bytes"},
	}
}

// RecorderText is the Rec column: whether the live session is being recorded.
func RecorderText(s Session) string {
	switch s.Recorder {
	case "running":
		return "● rec"
	case "stopped":
		return "stopped"
	case "none":
		return "none"
	}
	return ""
}

// MatchSchema describes the lines `agent-tmux grep --json` found.
func MatchSchema() resource.Schema[Match] {
	return resource.Schema[Match]{
		Name: "match",
		ID:   func(m Match) string { return m.ID + ":" + resource.Plain(m.Line) },
		Fields: []resource.Field[Match]{
			{Key: "name", Title: "Session", Value: func(m Match) any { return m.Name }},
			{Key: "id", Title: "Recording", Value: func(m Match) any { return m.ID }},
			{Key: "line", Title: "Line", Value: func(m Match) any { return m.Line }},
			{Key: "text", Title: "Text", Value: func(m Match) any { return m.Text }},
		},
		Columns: []string{"id", "line", "text"},
	}
}
