package secret

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/mugenkunou/ws-tool/internal/config"
	"github.com/mugenkunou/ws-tool/internal/manifest"
	"github.com/mugenkunou/ws-tool/internal/repo"
)

// Mutations behind `ws secret fix`, shared by the CLI and the TUI.

// Anchor is the stored (workspace-relative) allowlist form of a violation.
func Anchor(v Violation) string { return fmt.Sprintf("%s:%d", v.Path, v.Line) }

// AllowlistMap builds the scan allowlist from the manifest.
func AllowlistMap(m manifest.Manifest) map[string]struct{} {
	allow := make(map[string]struct{}, len(m.Secret.Allowlist))
	for _, a := range m.Secret.Allowlist {
		allow[a] = struct{}{}
	}
	return allow
}

// AddToAllowlist records anchor in the manifest allowlist (idempotent).
func AddToAllowlist(manifestPath, anchor string) error {
	m, err := manifest.Load(manifestPath)
	if err != nil {
		return err
	}
	for _, a := range m.Secret.Allowlist {
		if a == anchor {
			return nil
		}
	}
	m.Secret.Allowlist = append(m.Secret.Allowlist, anchor)
	return manifest.Save(manifestPath, m)
}

// TrackPassEntry records that anchor was resolved by storing it in pass.
func TrackPassEntry(manifestPath, anchor string) error {
	m, err := manifest.Load(manifestPath)
	if err != nil {
		return err
	}
	for _, e := range m.Secret.PassEntries {
		if e == anchor {
			return nil
		}
	}
	m.Secret.PassEntries = append(m.Secret.PassEntries, anchor)
	return manifest.Save(manifestPath, m)
}

// WorkspaceRelDir resolves a user-supplied directory (absolute, ~, or
// relative to the cwd) to its workspace-relative form; it must be inside the
// workspace.
func WorkspaceRelDir(workspacePath, input string) (string, error) {
	abs, err := config.ExpandUserPath(input)
	if err != nil {
		return "", fmt.Errorf("invalid directory %q: %w", input, err)
	}
	if !repo.IsWithin(abs, workspacePath) || abs == filepath.Clean(workspacePath) {
		return "", fmt.Errorf("directory must be inside the workspace %s: %s", workspacePath, abs)
	}
	rel, err := filepath.Rel(workspacePath, abs)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

// MergeSkipDirs combines config and flag skip dirs, deduplicating and
// normalizing to forward-slash workspace-relative paths. Config values follow
// the config path convention (relative, absolute, or ~); extra values are
// already workspace-relative.
func MergeSkipDirs(workspacePath string, configDirs, extra []string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, d := range append(append([]string{}, configDirs...), extra...) {
		rel, ok := config.WorkspaceRel(workspacePath, strings.TrimSpace(d))
		if !ok || rel == "." || seen[rel] {
			continue
		}
		seen[rel] = true
		result = append(result, rel)
	}
	return result
}

// AddSkipDir appends a workspace-relative dir to secret.skip_dirs in config
// (idempotent).
func AddSkipDir(configPath, dir string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	for _, d := range cfg.Secret.SkipDirs {
		if filepath.ToSlash(d) == dir {
			return nil
		}
	}
	cfg.Secret.SkipDirs = append(cfg.Secret.SkipDirs, dir)
	return config.Save(configPath, cfg)
}
