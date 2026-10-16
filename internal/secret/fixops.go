package secret

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/mugenkunou/ws-tool/internal/repo"
	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

// Mutations behind `ws secret fix`, shared by the CLI and the TUI.

// Anchor is the stored (workspace-relative) allowlist form of a violation.
func Anchor(v Violation) string { return fmt.Sprintf("%s:%d", v.Path, v.Line) }

// AllowlistMap builds the scan allowlist from the config.
// Deprecated: use wsconfig.AllowlistMap directly.
func AllowlistMap(cfg wsconfig.WsConfig) map[string]struct{} {
	return wsconfig.AllowlistMap(cfg)
}

// AddToAllowlist records anchor in the config allowlist (idempotent).
// Deprecated: use wsconfig.AddToAllowlist directly.
func AddToAllowlist(configPath, anchor string) error {
	return wsconfig.AddToAllowlist(configPath, anchor)
}

// TrackPassEntry records that anchor was resolved by storing it in the allowlist.
// (In the new layout there is no separate PassEntries field — we reuse the allowlist.)
func TrackPassEntry(configPath, anchor string) error {
	return wsconfig.AddToAllowlist(configPath, anchor)
}

// WorkspaceRelDir resolves a user-supplied directory (absolute, ~, or
// relative to the cwd) to its workspace-relative form; it must be inside the
// workspace.
func WorkspaceRelDir(workspacePath, input string) (string, error) {
	abs, err := wsconfig.ExpandUserPath(input)
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

// workspaceRel resolves a path to workspace-relative form.
func workspaceRel(workspacePath, value string) (rel string, ok bool) {
	abs, err := wsconfig.ResolvePath(workspacePath, value)
	if err != nil {
		return "", false
	}
	r, err := filepath.Rel(filepath.Clean(workspacePath), abs)
	if err != nil {
		return "", false
	}
	r = filepath.ToSlash(r)
	if r == ".." || strings.HasPrefix(r, "../") {
		return "", false
	}
	return r, true
}

// MergeSkipDirs combines config and flag skip dirs, deduplicating and
// normalizing to forward-slash workspace-relative paths. Config values follow
// the config path convention (relative, absolute, or ~); extra values are
// already workspace-relative.
func MergeSkipDirs(workspacePath string, configDirs, extra []string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, d := range append(append([]string{}, configDirs...), extra...) {
		rel, ok := workspaceRel(workspacePath, strings.TrimSpace(d))
		if !ok || rel == "." || seen[rel] {
			continue
		}
		seen[rel] = true
		result = append(result, rel)
	}
	return result
}

// AddSkipDir appends a workspace-relative dir to secret.skip_dirs in the ws config
// (idempotent).
func AddSkipDir(configPath, dir string) error {
	cfg, err := wsconfig.Load(configPath)
	if err != nil {
		return err
	}
	for _, d := range cfg.Secret.SkipDirs {
		if filepath.ToSlash(d) == dir {
			return nil
		}
	}
	cfg.Secret.SkipDirs = append(cfg.Secret.SkipDirs, dir)
	return wsconfig.Save(configPath, cfg)
}
