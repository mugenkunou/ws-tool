// Package env resolves everything the TUI's screens need to know about the
// workspace once, at startup. It is plain data: screens read from it inside
// their commands and never mutate it.
package env

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mugenkunou/ws-tool/internal/repo"
	"github.com/mugenkunou/ws-tool/internal/resource"
	"github.com/mugenkunou/ws-tool/internal/workspace"
	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

// ErrNotInitialized means the resolved workspace has no ws config.
var ErrNotInitialized = errors.New("workspace not initialized — run `ws init` first")

// Env is the resolved workspace context.
type Env struct {
	Paths       workspace.Paths
	Config      wsconfig.WsConfig
	RepoRoots   []string // absolute
	ExcludeDirs []string
	ScratchDir  string // absolute
	LogDir      string // absolute
	// HistoryFile is where the Repos screen keeps the command lines it ran
	// (machine-local state). Empty keeps the history in memory only.
	HistoryFile string

	// Clock is the time source for display-only uses (greetings, ages). Nil
	// means time.Now; tests pin it.
	Clock func() time.Time
}

// Now returns the current time from Clock.
func (e Env) Now() time.Time {
	if e.Clock != nil {
		return e.Clock()
	}
	return time.Now()
}

// Load resolves paths, requires an initialized workspace, and loads config.
func Load(o workspace.PathOverrides) (Env, error) {
	p, err := workspace.ResolvePaths(o)
	if err != nil {
		return Env{}, err
	}
	if !workspace.ConfigExists(p.Config) {
		return Env{Paths: p}, ErrNotInitialized
	}

	cfg, err := wsconfig.Load(p.Config)
	if err != nil {
		return Env{Paths: p}, err
	}

	e := Env{
		Paths:       p,
		Config:      cfg,
		ExcludeDirs: cfg.Repo.ExcludeDirs,
	}
	for _, r := range cfg.Repo.Roots {
		if resolved, err := wsconfig.ResolvePath(p.Workspace, r); err == nil {
			e.RepoRoots = append(e.RepoRoots, resolved)
		}
	}
	if dir, err := wsconfig.ResolvePath(p.Workspace, cfg.Session.RootDir); err == nil {
		e.LogDir = dir
	}
	if dir, err := wsconfig.ResolvePath(p.Workspace, cfg.Scratch.RootDir); err == nil {
		e.ScratchDir = dir
	}
	if dir, err := wsconfig.StateDir(); err == nil {
		e.HistoryFile = repo.HistoryFile(dir)
	}
	return e, nil
}

// View is how resource schemas render on screen: the TUI clock and Rel paths.
func (e Env) View() resource.View { return resource.View{Now: e.Now, Path: e.Rel} }

// Rel shortens an absolute path for display relative to the workspace, or
// the home directory, falling back to the path itself.
func (e Env) Rel(path string) string {
	ws := e.Paths.Workspace
	if rel, err := filepath.Rel(ws, path); err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, "../") {
		return rel
	}
	if home, err := os.UserHomeDir(); err == nil {
		if rel, err := filepath.Rel(home, path); err == nil && rel != ".." && !strings.HasPrefix(rel, "../") {
			return filepath.Join("~", rel)
		}
	}
	return path
}
