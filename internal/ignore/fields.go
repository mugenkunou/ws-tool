package ignore

import (
	"fmt"

	"github.com/mugenkunou/ws-tool/internal/resource"
	"github.com/mugenkunou/ws-tool/internal/style"
)

// Schema describes scan violations for the CLI (`ws ignore scan`) and the
// TUI ignore screen. Violation paths are workspace-relative; the schema
// reports them absolute.
func Schema(v resource.View, workspacePath string) resource.Schema[Violation] {
	abs := func(x Violation) string { return style.AbsPath(workspacePath, x.Path) }
	return resource.Schema[Violation]{
		Name: "violation",
		ID:   func(x Violation) string { return x.Type + ":" + x.Path },
		Fields: []resource.Field[Violation]{
			{Key: "severity", Title: "Severity", Value: func(x Violation) any { return x.Severity }},
			{Key: "type", Title: "Type", Value: func(x Violation) any { return x.Type }},
			{Key: "size_bytes", Title: "Size", Value: func(x Violation) any { return x.SizeBytes },
				Text: func(x Violation) string {
					switch {
					case x.SizeBytes > 0:
						return style.HumanBytes(x.SizeBytes)
					case x.Depth > 0:
						return fmt.Sprintf("%d lvl", x.Depth)
					}
					return ""
				}},
			{Key: "path", Title: "Path", Value: func(x Violation) any { return abs(x) },
				Text: func(x Violation) string { return v.Path(abs(x)) }},
			{Key: "depth", Title: "Depth", Value: func(x Violation) any { return x.Depth }},
			{Key: "group", Title: "Group", Value: func(x Violation) any { return x.Group }},
			{Key: "message", Title: "Message", Value: func(x Violation) any { return x.Message }},
			{Key: "in_safe_harbor", Title: "Suppressed", Value: func(x Violation) any { return x.InSafeHarbor }},
		},
		Columns: []string{"severity", "type", "size_bytes", "path"},
	}
}
