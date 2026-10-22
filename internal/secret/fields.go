package secret

import (
	"strconv"
	"strings"

	"github.com/mugenkunou/ws-tool/internal/resource"
	"github.com/mugenkunou/ws-tool/internal/style"
)

// Schema describes secret findings for the CLI (`ws secret scan`) and the
// TUI secrets screen. Violation paths are workspace-relative; the schema
// reports them absolute.
func Schema(v resource.View, workspacePath string) resource.Schema[Violation] {
	abs := func(x Violation) string { return style.AbsPath(workspacePath, x.Path) }
	return resource.Schema[Violation]{
		Name: "secret finding",
		ID:   func(x Violation) string { return Anchor(x) + ":" + x.Type },
		Fields: []resource.Field[Violation]{
			{Key: "severity", Title: "Severity", Value: func(x Violation) any { return x.Severity }},
			{Key: "location", Title: "Location", Value: func(x Violation) any { return abs(x) + ":" + strconv.Itoa(x.Line) },
				Text: func(x Violation) string { return v.Path(abs(x)) + ":" + strconv.Itoa(x.Line) }},
			{Key: "snippet", Title: "Match", Value: func(x Violation) any { return x.Snippet },
				Text: func(x Violation) string { return strings.Join(strings.Fields(x.Snippet), " ") }},
			{Key: "path", Title: "Path", Value: func(x Violation) any { return abs(x) },
				Text: func(x Violation) string { return v.Path(abs(x)) }},
			{Key: "line", Title: "Line", Value: func(x Violation) any { return x.Line }},
			{Key: "type", Title: "Type", Value: func(x Violation) any { return x.Type }},
			{Key: "group", Title: "Group", Value: func(x Violation) any { return x.Group }},
			{Key: "message", Title: "Message", Value: func(x Violation) any { return x.Message }},
		},
		Columns: []string{"severity", "location", "snippet"},
	}
}
