package ignore

import (
	"fmt"

	"github.com/mugenkunou/ws-tool/internal/resource"
	"github.com/mugenkunou/ws-tool/internal/style"
)

// Schema describes scan violations for the CLI (`ws ignore lint`) and the
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
			{Key: "muted", Title: "Muted", Value: func(x Violation) any { return x.Muted }},
		},
		Columns: []string{"severity", "type", "size_bytes", "path"},
	}
}

// IgnoredSchema describes the paths that don't sync (`ws ignore ls`). Paths
// are workspace-relative in the items and absolute in the records.
func IgnoredSchema(v resource.View, workspacePath string) resource.Schema[IgnoredPath] {
	abs := func(x IgnoredPath) string { return style.AbsPath(workspacePath, x.Path) }
	return resource.Schema[IgnoredPath]{
		Name: "ignored",
		ID:   func(x IgnoredPath) string { return x.Path },
		Fields: []resource.Field[IgnoredPath]{
			{Key: "path", Title: "Path", Value: func(x IgnoredPath) any { return abs(x) },
				Text: func(x IgnoredPath) string { return v.Path(abs(x)) }},
			{Key: "size_bytes", Title: "Size", Value: func(x IgnoredPath) any { return x.SizeBytes },
				Text: func(x IgnoredPath) string { return style.HumanBytes(x.SizeBytes) }},
			{Key: "rule", Title: "Rule", Value: func(x IgnoredPath) any { return x.Rule }},
			{Key: "rule_source", Title: "Source", Value: func(x IgnoredPath) any { return x.RuleSource }},
		},
		Columns: []string{"size_bytes", "path", "rule"},
	}
}
