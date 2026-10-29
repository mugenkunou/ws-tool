package symlink

import (
	"os"
	"runtime"

	"github.com/mugenkunou/ws-tool/internal/resource"
	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

// Entry is one row of `ws symlink ls` and of the TUI Symlinks screen: a Record that
// Detect produced.
type Entry = Record

// Listing is what Detect found, ready to show.
type Listing struct {
	// All is every record: ws.json entries first, then untracked and orphan.
	All []Entry
	// Notes are things worth saying once (the $HOME walk stopped at its cap).
	Notes []string
}

// Load runs Detect against this machine. walk turns the $HOME walk on (`ls` does
// it; the Dashboard does not).
func Load(workspacePath, configPath string, walk bool) (Listing, error) {
	cfg, err := wsconfig.Load(configPath)
	if err != nil {
		return Listing{}, err
	}
	home, _ := os.UserHomeDir()
	res := Detect(Options{
		// WS_SYMLINK_ROOT puts a fake root in front of the /etc paths the catalog
		// lists, so tests and demos never read the real ones.
		Root: os.Getenv("WS_SYMLINK_ROOT"),
		Home: home, Workspace: workspacePath, GOOS: runtime.GOOS,
		Entries: cfg.Symlinks, Walk: walk,
	})
	return Listing{All: res.Records, Notes: res.Notes}, nil
}

// Problems counts the rows that need attention: every registered entry that is not
// ok, plus untracked links and orphans.
func (l Listing) Problems() int {
	n := 0
	for _, e := range l.All {
		switch e.Status {
		case StatusOK:
		default:
			n++
		}
	}
	return n
}

// Healthy reports whether an entry needs nothing.
func Healthy(e Entry) bool { return e.Status == StatusOK }

// Schema describes symlink entries for the CLI and the TUI.
func Schema(v resource.View) resource.Schema[Entry] {
	return resource.Schema[Entry]{
		Name: "symlink",
		ID:   func(e Entry) string { return e.System },
		Fields: []resource.Field[Entry]{
			{Key: "system", Title: "Host path", Value: func(e Entry) any { return e.System },
				Text: func(e Entry) string { return v.Path(e.System) }},
			{Key: "original", Title: "Stored as", Value: func(e Entry) any { return e.Original }},
			{Key: "workspace_path", Title: "Workspace copy", Value: func(e Entry) any { return e.WorkspacePath },
				Text: func(e Entry) string { return v.Path(e.WorkspacePath) }},
			{Key: "app", Title: "App", Value: func(e Entry) any { return e.App }},
			{Key: "status", Title: "Status", Value: func(e Entry) any { return string(e.Status) }, Whole: true},
			{Key: "message", Title: "Problem", Value: func(e Entry) any { return e.Message }},
			{Key: "dir", Title: "Directory", Value: func(e Entry) any { return e.Dir }},
			{Key: "sudo", Title: "Sudo", Value: func(e Entry) any { return e.Sudo }},
		},
		Columns: []string{"system", "workspace_path", "app", "status", "sudo"},
	}
}
