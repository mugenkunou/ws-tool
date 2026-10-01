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

	"github.com/mugenkunou/ws-tool/internal/config"
	"github.com/mugenkunou/ws-tool/internal/manifest"
	"github.com/mugenkunou/ws-tool/internal/workspace"
)

// ErrNotInitialized means the resolved workspace has no ws config.
var ErrNotInitialized = errors.New("workspace not initialized — run `ws init` first")

// Env is the resolved workspace context.
type Env struct {
	Paths       workspace.Paths
	Config      config.Config
	RepoRoots   []string // absolute
	ExcludeDirs []string
	ScratchDir  string // absolute
	LogDir      string // absolute

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
	// Migrate manifest to schema 2 if needed, as the CLI does.
	_ = manifest.MigrateIfNeeded(p.Manifest)

	cfg, err := config.Load(p.Config)
	if err != nil {
		return Env{Paths: p}, err
	}

	e := Env{
		Paths:       p,
		Config:      cfg,
		ExcludeDirs: cfg.Repo.ExcludeDirs,
		LogDir:      filepath.Join(p.Workspace, "ws", "ws-log"),
	}
	for _, r := range cfg.Repo.Roots {
		if resolved, err := config.ResolvePath(p.Workspace, r); err == nil {
			e.RepoRoots = append(e.RepoRoots, resolved)
		}
	}
	if dir, err := config.ResolvePath(p.Workspace, cfg.Scratch.RootDir); err == nil {
		e.ScratchDir = dir
	}
	return e, nil
}

// Rel shortens an absolute path for display relative to the workspace, or
// the home directory, falling back to the path itself.
func (e Env) Rel(path string) string {
	if rel, ok := config.WorkspaceRel(e.Paths.Workspace, path); ok && rel != "." {
		return rel
	}
	if home, err := os.UserHomeDir(); err == nil {
		if rel, err := filepath.Rel(home, path); err == nil && rel != ".." && !strings.HasPrefix(rel, "../") {
			return filepath.Join("~", rel)
		}
	}
	return path
}
