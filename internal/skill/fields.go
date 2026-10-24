package skill

import (
	"strconv"

	"github.com/mugenkunou/ws-tool/internal/resource"
)

// Entry is one row of the TUI Skills screen: a skill and the tools that have
// something at its place.
type Entry struct {
	Name   string
	Path   string
	Broken bool
	// Tools are the names of the tools where the skill is installed, in table order.
	Tools []string
}

// Entries lists every skill with the tools it is installed in.
func Entries(home, workspace string) ([]Entry, error) {
	skills, err := List(workspace)
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(skills))
	for _, s := range skills {
		e := Entry{Name: s.Name, Path: s.Path, Broken: s.Broken, Tools: []string{}}
		for _, t := range Tools {
			if Installed(home, t, s.Name) {
				e.Tools = append(e.Tools, t.Name)
			}
		}
		out = append(out, e)
	}
	return out, nil
}

// Schema describes skill entries for the TUI list.
func Schema(v resource.View) resource.Schema[Entry] {
	return resource.Schema[Entry]{
		Name: "skill",
		ID:   func(e Entry) string { return e.Name },
		Fields: []resource.Field[Entry]{
			{Key: "skill", Title: "Skill", Value: func(e Entry) any { return e.Name }},
			{Key: "installed_in", Title: "Installed in", Value: func(e Entry) any { return e.Tools }},
			{Key: "tools", Title: "Tools", Value: func(e Entry) any { return len(e.Tools) },
				Text: func(e Entry) string { return strconv.Itoa(len(e.Tools)) }},
			{Key: "path", Title: "Path", Value: func(e Entry) any { return e.Path },
				Text: func(e Entry) string { return v.Path(e.Path) }},
			{Key: "broken", Title: "Broken link", Value: func(e Entry) any { return e.Broken }},
		},
		Columns: []string{"skill", "installed_in", "path"},
	}
}
