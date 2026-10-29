package mirror

import "github.com/mugenkunou/ws-tool/internal/resource"

// Change is one pending change in the mirror: a file the next push copies, or
// a path it deletes from the mirror.
type Change struct {
	Path   string // workspace-relative for a copy, mirror-relative for a delete
	Action string // ActionCopy or ActionDelete
}

// Pending change actions.
const (
	ActionCopy   = "copy"
	ActionDelete = "delete"
)

// Changes lists what the push would do: copies first, then deletions.
func (p *Prepared) Changes() []Change {
	out := make([]Change, 0, len(p.Changed)+len(p.Deleted))
	for _, f := range p.Changed {
		out = append(out, Change{Path: f, Action: ActionCopy})
	}
	for _, f := range p.Deleted {
		out = append(out, Change{Path: f, Action: ActionDelete})
	}
	return out
}

// Schema describes pending changes for the TUI Sync list. It is TUI-only:
// `ws sync status` prints the totals, not the rows.
func Schema() resource.Schema[Change] {
	return resource.Schema[Change]{
		Name: "change",
		ID:   func(c Change) string { return c.Action + " " + c.Path },
		Fields: []resource.Field[Change]{
			{Key: "action", Title: "Action", Value: func(c Change) any { return c.Action }},
			{Key: "path", Title: "Path", Value: func(c Change) any { return c.Path }},
		},
		Columns: []string{"action", "path"},
	}
}
