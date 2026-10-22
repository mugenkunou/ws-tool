package dotfile

import (
	"strings"

	"github.com/mugenkunou/ws-tool/internal/resource"
	"github.com/mugenkunou/ws-tool/internal/style"
	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

// Entry is one managed dotfile and its symlink health: the record that
// `ws dotfile ls` prints and the TUI dotfiles screen lists.
type Entry struct {
	Link          wsconfig.Link
	WorkspacePath string // absolute path of the stored file
	Status        string // "" when healthy, else StatusBroken / StatusOverwritten
	Message       string // why the link is unhealthy
}

// Entries lists every managed dotfile with its health.
func Entries(workspacePath, configPath string) ([]Entry, error) {
	links, err := List(configPath)
	if err != nil {
		return nil, err
	}
	issues, err := Scan(ScanOptions{WorkspacePath: workspacePath, ConfigPath: configPath})
	if err != nil {
		return nil, err
	}
	byPath := make(map[string]Issue, len(issues))
	for _, is := range issues {
		byPath[is.SystemPath] = is
	}
	out := make([]Entry, 0, len(links))
	for _, l := range links {
		is := byPath[l.System]
		out = append(out, Entry{
			Link:          l,
			WorkspacePath: style.AbsPath(workspacePath, LinkDotfilePath(l)),
			Status:        is.Status,
			Message:       is.Message,
		})
	}
	return out, nil
}

// Secret reports whether the entry's workspace copy lives under secrets/.
func (e Entry) Secret() bool {
	return strings.HasPrefix(LinkDotfilePath(e.Link), "secrets/")
}

// Health is "ok", "broken", or "overwritten".
func (e Entry) Health() string {
	if e.Status == "" {
		return "ok"
	}
	return strings.ToLower(e.Status)
}

// Schema describes dotfile entries for the CLI and the TUI.
func Schema(v resource.View) resource.Schema[Entry] {
	return resource.Schema[Entry]{
		Name: "dotfile",
		ID:   func(e Entry) string { return e.Link.System },
		Fields: []resource.Field[Entry]{
			{Key: "system", Title: "System path", Value: func(e Entry) any { return e.Link.System },
				Text: func(e Entry) string { return v.Path(e.Link.System) }},
			{Key: "workspace_path", Title: "Workspace copy", Value: func(e Entry) any { return e.WorkspacePath },
				Text: func(e Entry) string { return v.Path(e.WorkspacePath) }},
			{Key: "original", Title: "Stored as", Value: func(e Entry) any { return e.Link.Original }},
			{Key: "status", Title: "Status", Value: func(e Entry) any { return e.Health() }},
			{Key: "message", Title: "Problem", Value: func(e Entry) any { return e.Message }},
			{Key: "dir", Title: "Directory", Value: func(e Entry) any { return e.Link.Dir }},
			{Key: "sudo", Title: "Sudo", Value: func(e Entry) any { return e.Link.Sudo }},
		},
		Columns: []string{"system", "workspace_path", "status", "sudo"},
	}
}
