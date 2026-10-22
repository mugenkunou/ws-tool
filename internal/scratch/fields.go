package scratch

import (
	"github.com/mugenkunou/ws-tool/internal/resource"
	"github.com/mugenkunou/ws-tool/internal/style"
)

// Schema describes scratch directories for the CLI (`ws scratch ls`) and the
// TUI scratch screen.
func Schema(v resource.View) resource.Schema[Entry] {
	return resource.Schema[Entry]{
		Name: "scratch directory",
		ID:   func(e Entry) string { return e.Path },
		Fields: []resource.Field[Entry]{
			{Key: "name", Title: "Name", Value: func(e Entry) any { return e.Name }},
			{Key: "path", Title: "Path", Value: func(e Entry) any { return e.Path },
				Text: func(e Entry) string { return v.Path(e.Path) }},
			{Key: "age", Title: "Age", Value: func(e Entry) any { return e.Age },
				Text: func(e Entry) string { return style.Age(e.Age) }},
			{Key: "size_bytes", Title: "Size", Value: func(e Entry) any { return e.SizeBytes },
				Text: func(e Entry) string { return style.HumanBytes(e.SizeBytes) }},
			{Key: "items", Title: "Items", Value: func(e Entry) any { return e.Items }},
			{Key: "tags", Title: "Tags", Value: func(e Entry) any { return e.Tags }},
		},
		Columns: []string{"name", "age", "size_bytes", "items", "tags"},
	}
}
