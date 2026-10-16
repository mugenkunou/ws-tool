package workspace

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mugenkunou/ws-tool/internal/ignore"
	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

// layoutDirs are the workspace directories created by `ws init`.
// They follow the phase-1 layout defined in phase-1.md.
var layoutDirs = []string{
	"notes",
	"experiments",
	"artifacts",
	"references",
	"scripts",
	"scripts/lib",
	"agent",
	"agent/skills",
	"configs",
	"configs/dotfiles",
	"configs/dotfiles-git",
	"secrets",
	"secrets/dotfiles",
	"secrets/pass",
	"data",
	"data/ws-log",
	"archive",
}

type InitOptions struct {
	WorkspacePath string
	DryRun        bool
}

type InitResult struct {
	WorkspacePath string   `json:"workspace_path"`
	DryRun        bool     `json:"dry_run"`
	Created       []string `json:"created"`
	Skipped       []string `json:"skipped"`
	Messages      []string `json:"messages"`
}

func Init(opts InitOptions) (InitResult, error) {
	result := InitResult{
		WorkspacePath: opts.WorkspacePath,
		DryRun:        opts.DryRun,
		Created:       []string{},
		Skipped:       []string{},
		Messages:      []string{},
	}

	if opts.WorkspacePath == "" {
		return result, fmt.Errorf("workspace path is required")
	}

	configPath := wsconfig.ConfigPath(opts.WorkspacePath)
	megaignorePath := filepath.Join(opts.WorkspacePath, ".megaignore")

	if opts.DryRun {
		for _, d := range layoutDirs {
			result.Messages = append(result.Messages, "Would create dir: "+filepath.Join(opts.WorkspacePath, d))
		}
		result.Messages = append(result.Messages,
			"Would create: "+configPath,
			"Would create: "+megaignorePath,
		)
		result.DryRun = true
		return result, nil
	}

	// Create layout directories.
	for _, d := range layoutDirs {
		abs := filepath.Join(opts.WorkspacePath, filepath.FromSlash(d))
		if err := os.MkdirAll(abs, 0o755); err != nil {
			return result, fmt.Errorf("creating %s: %w", abs, err)
		}
		result.Created = append(result.Created, abs)
	}

	// Write configs/ws.json if missing.
	if err := writeFileIfMissing(configPath, func() error {
		return wsconfig.Save(configPath, wsconfig.Default())
	}, &result); err != nil {
		return result, err
	}

	// Write .megaignore if missing.
	if err := writeFileIfMissing(megaignorePath, func() error {
		return ignore.WriteMegaignore(megaignorePath, ignore.DefaultUserRules())
	}, &result); err != nil {
		return result, err
	}

	result.Messages = append(result.Messages, "Workspace initialized: "+opts.WorkspacePath)
	return result, nil
}

func writeFileIfMissing(path string, write func() error, result *InitResult) error {
	if _, err := os.Stat(path); err == nil {
		result.Skipped = append(result.Skipped, path)
		result.Messages = append(result.Messages, "Skipped (already exists): "+path)
		return nil
	}
	if err := write(); err != nil {
		return err
	}
	result.Created = append(result.Created, path)
	result.Messages = append(result.Messages, "Created: "+path)
	return nil
}
