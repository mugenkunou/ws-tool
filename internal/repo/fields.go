package repo

import (
	"strings"

	"github.com/mugenkunou/ws-tool/internal/resource"
)

// Entry is one repo's status plus its hygiene findings: the record
// `ws repo scan` reports and the TUI repos screen lists.
type Entry struct {
	Status   RepoStatus
	Findings []Finding
}

// State is "error", "dirty", "no upstream", or "clean".
func (e Entry) State() string {
	switch st := e.Status; {
	case st.Error != "":
		return "error"
	case st.Dirty:
		return "dirty"
	case !st.HasUpstream:
		return "no upstream"
	}
	return "clean"
}

// Entries pairs statuses with their findings.
func Entries(statuses []RepoStatus, findings []Finding) []Entry {
	byRepo := map[string][]Finding{}
	for _, f := range findings {
		byRepo[f.Repo] = append(byRepo[f.Repo], f)
	}
	out := make([]Entry, len(statuses))
	for i, st := range statuses {
		out[i] = Entry{Status: st, Findings: byRepo[st.Path]}
	}
	return out
}

// Schema describes repo entries for the CLI and the TUI.
func Schema(v resource.View) resource.Schema[Entry] {
	return resource.Schema[Entry]{
		Name: "repo",
		ID:   func(e Entry) string { return e.Status.Path },
		Fields: []resource.Field[Entry]{
			{Key: "path", Title: "Repo", Value: func(e Entry) any { return e.Status.Path },
				Text: func(e Entry) string { return v.Path(e.Status.Path) }},
			{Key: "state", Title: "State", Value: func(e Entry) any { return e.State() }},
			{Key: "ahead", Title: "↑", Value: func(e Entry) any { return e.Status.Ahead }},
			{Key: "behind", Title: "↓", Value: func(e Entry) any { return e.Status.Behind }},
			{Key: "issues", Title: "Issues", Value: func(e Entry) any { return len(e.Findings) },
				Text: func(e Entry) string {
					if len(e.Findings) == 0 {
						return ""
					}
					return resource.Plain(len(e.Findings))
				}},
			{Key: "branch", Title: "Branch", Value: func(e Entry) any { return e.Status.Branch },
				Text: func(e Entry) string {
					if e.Status.Detached {
						return "(detached)"
					}
					return e.Status.Branch
				}},
			{Key: "detached", Title: "Detached", Value: func(e Entry) any { return e.Status.Detached }},
			{Key: "dirty", Title: "Dirty", Value: func(e Entry) any { return e.Status.Dirty }},
			{Key: "has_upstream", Title: "Upstream", Value: func(e Entry) any { return e.Status.HasUpstream }},
			{Key: "findings", Title: "Findings", Value: func(e Entry) any {
				checks := make([]string, len(e.Findings))
				for i, f := range e.Findings {
					checks[i] = f.Check
				}
				return checks
			}, Text: func(e Entry) string {
				parts := make([]string, len(e.Findings))
				for i, f := range e.Findings {
					parts[i] = f.Check + ": " + f.Detail
				}
				return strings.Join(parts, "; ")
			}},
			{Key: "error", Title: "Error", Value: func(e Entry) any { return e.Status.Error }},
		},
		Columns: []string{"path", "state", "ahead", "behind", "issues", "branch"},
	}
}
