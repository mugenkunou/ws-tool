package workspace

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mugenkunou/ws-tool/internal/config"
	"github.com/mugenkunou/ws-tool/internal/manifest"
	"github.com/mugenkunou/ws-tool/internal/megaignore"
)

type InitOptions struct {
	WorkspacePath string
	ConfigPath    string
	ManifestPath  string
	DryRun        bool
}

type InitResult struct {
	WorkspacePath string   `json:"workspace_path"`
	DryRun        bool     `json:"dry_run"`
	Created       []string `json:"created"`
	Skipped       []string `json:"skipped"`
	Messages      []string `json:"messages"`
}

func ConfigExists(configPath string) bool {
	_, err := os.Stat(configPath)
	return err == nil
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

	wsDir := filepath.Join(opts.WorkspacePath, "ws")
	megaignorePath := filepath.Join(opts.WorkspacePath, ".megaignore")

	if opts.ConfigPath == "" {
		opts.ConfigPath = filepath.Join(wsDir, "config.json")
	}
	if opts.ManifestPath == "" {
		opts.ManifestPath = filepath.Join(wsDir, "manifest.json")
	}

	if opts.DryRun {
		result.Messages = append(result.Messages,
			"Dry run: no changes applied.",
			"Would create: "+wsDir,
			"Would create: "+opts.ConfigPath,
			"Would create: "+opts.ManifestPath,
			"Would create: "+megaignorePath,
		)
		return result, nil
	}

	if err := os.MkdirAll(wsDir, 0o755); err != nil {
		return result, err
	}
	result.Created = append(result.Created, wsDir)
	result.Messages = append(result.Messages, "Created: "+wsDir)

	if err := writeIfMissing(opts.ConfigPath, func() error {
		return config.Save(opts.ConfigPath, config.Default())
	}, &result); err != nil {
		return result, err
	}

	if err := writeIfMissing(opts.ManifestPath, func() error {
		return manifest.Save(opts.ManifestPath, manifest.Default())
	}, &result); err != nil {
		return result, err
	}

	if err := writeIfMissing(megaignorePath, func() error {
		content := megaignore.EnsureFinalSentinel(megaignore.Template)
		return os.WriteFile(megaignorePath, []byte(content), 0o644)
	}, &result); err != nil {
		return result, err
	}

	result.Messages = append(result.Messages, "Workspace initialized: "+opts.WorkspacePath)
	return result, nil
}

func writeIfMissing(path string, write func() error, result *InitResult) error {
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
