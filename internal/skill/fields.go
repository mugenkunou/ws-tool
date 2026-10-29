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
	Shelf  Shelf
	// Note says dev mode, the version an ejected copy came from, or a name clash.
	Note     string
	Shadowed bool
	// Tools are the names of the tools where the skill is installed, in table order.
	Tools []string
}

// Entries lists every skill on both shelves with the tools it is installed in.
func Entries(home, workspace string) ([]Entry, error) {
	skills, err := All(home, workspace)
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(skills))
	for _, s := range skills {
		e := Entry{Name: s.Name, Path: s.Path, Broken: s.Broken, Shelf: s.Shelf, Note: Note(s), Shadowed: s.Shadowed, Tools: []string{}}
		for _, t := range Tools {
			if Installed(home, t, s.Name) {
				e.Tools = append(e.Tools, t.Name)
			}
		}
		out = append(out, e)
	}
	return out, nil
}

// Note is the short remark a skill's row carries, or empty.
func Note(s Skill) string {
	switch {
	case s.Shadowed:
		return "same name as a built-in; the built-in wins"
	case s.Dev != "":
		return "dev"
	case s.Ejected != "":
		return "ejected from " + s.Ejected
	case s.Provider != "":
		return "from " + s.Provider
	}
	return ""
}

// Schema describes skill entries for the TUI list.
func Schema(v resource.View) resource.Schema[Entry] {
	return resource.Schema[Entry]{
		Name: "skill",
		ID:   func(e Entry) string { return e.Name },
		Fields: []resource.Field[Entry]{
			{Key: "skill", Title: "Skill", Value: func(e Entry) any { return e.Name }},
			{Key: "shelf", Title: "Shelf", Value: func(e Entry) any { return string(e.Shelf) },
				Text: func(e Entry) string {
					if e.Note != "" {
						return string(e.Shelf) + " (" + e.Note + ")"
					}
					return string(e.Shelf)
				}},
			{Key: "note", Title: "Note", Value: func(e Entry) any { return e.Note }},
			{Key: "installed_in", Title: "Installed in", Value: func(e Entry) any { return e.Tools }},
			{Key: "tools", Title: "Tools", Value: func(e Entry) any { return len(e.Tools) },
				Text: func(e Entry) string { return strconv.Itoa(len(e.Tools)) }},
			{Key: "path", Title: "Path", Value: func(e Entry) any { return e.Path },
				Text: func(e Entry) string { return v.Path(e.Path) }},
			{Key: "broken", Title: "Broken link", Value: func(e Entry) any { return e.Broken }},
		},
		Columns: []string{"skill", "shelf", "installed_in", "path"},
	}
}
