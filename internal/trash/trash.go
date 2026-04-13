package trash

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type SetupOptions struct {
	RootDir      string
	ShellRM      bool
	VSCodeDelete bool
	FileExplorer bool
	DryRun       bool
}

type SetupResult struct {
	RootDir      string `json:"root_dir"`
	ShellRM      bool   `json:"shell_rm"`
	VSCodeDelete bool   `json:"vscode_delete"`
	FileExplorer bool   `json:"file_explorer"`
	StatePath    string `json:"state_path"`
	DryRun       bool   `json:"dry_run"`
}

type Status struct {
	RootDir                string `json:"root_dir"`
	ShellRMConfigured      bool   `json:"shell_rm_configured"`
	VSCodeConfigured       bool   `json:"vscode_configured"`
	FileExplorerConfigured bool   `json:"file_explorer_configured"`
	StatePath              string `json:"state_path"`
}

func Setup(opts SetupOptions) (SetupResult, error) {
	root := strings.TrimSpace(opts.RootDir)
	if root == "" {
		root = "~/.Trash"
	}
	resolvedRoot, err := expandPath(root)
	if err != nil {
		return SetupResult{}, err
	}

	statePath, err := stateFilePath()
	if err != nil {
		return SetupResult{}, err
	}

	res := SetupResult{
		RootDir:      resolvedRoot,
		ShellRM:      opts.ShellRM,
		VSCodeDelete: opts.VSCodeDelete,
		FileExplorer: opts.FileExplorer,
		StatePath:    statePath,
		DryRun:       opts.DryRun,
	}

	if opts.DryRun {
		return res, nil
	}

	if err := os.MkdirAll(resolvedRoot, 0o755); err != nil {
		return SetupResult{}, err
	}
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		return SetupResult{}, err
	}
	if opts.ShellRM {
		if err := ensureShellRMIntegration(resolvedRoot); err != nil {
			return SetupResult{}, err
		}
	}
	if opts.FileExplorer {
		if err := ensureFileExplorerIntegration(resolvedRoot); err != nil {
			return SetupResult{}, err
		}
	}

	state := map[string]any{
		"root_dir":      resolvedRoot,
		"shell_rm":      opts.ShellRM,
		"vscode_delete": opts.VSCodeDelete,
		"file_explorer": opts.FileExplorer,
	}
	encoded, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return SetupResult{}, err
	}
	encoded = append(encoded, '\n')
	if err := os.WriteFile(statePath, encoded, 0o644); err != nil {
		return SetupResult{}, err
	}

	return res, nil
}

func GetStatus(rootHint string) (Status, error) {
	root := strings.TrimSpace(rootHint)
	if root == "" {
		root = "~/.Trash"
	}
	resolvedRoot, err := expandPath(root)
	if err != nil {
		return Status{}, err
	}
	statePath, err := stateFilePath()
	if err != nil {
		return Status{}, err
	}

	st := Status{RootDir: resolvedRoot, StatePath: statePath}

	content, err := os.ReadFile(statePath)
	if err != nil {
		if os.IsNotExist(err) {
			return st, nil
		}
		return Status{}, err
	}

	var loaded struct {
		RootDir      string `json:"root_dir"`
		ShellRM      bool   `json:"shell_rm"`
		VSCodeDelete bool   `json:"vscode_delete"`
		FileExplorer bool   `json:"file_explorer"`
	}
	if err := json.Unmarshal(content, &loaded); err != nil {
		return Status{}, err
	}

	if loaded.RootDir != "" {
		st.RootDir = loaded.RootDir
	}
	st.ShellRMConfigured = loaded.ShellRM && shellRMConfigured()
	st.VSCodeConfigured = loaded.VSCodeDelete
	st.FileExplorerConfigured = loaded.FileExplorer && fileExplorerConfigured(st.RootDir)

	return st, nil
}

func (s Status) WarningCount() int {
	count := 0
	if !s.ShellRMConfigured {
		count++
	}
	if !s.VSCodeConfigured {
		count++
	}
	if !s.FileExplorerConfigured {
		count++
	}
	return count
}

// ScanOptions configures a trash scan operation.
type ScanOptions struct {
	RootDir    string
	WarnSizeMB int
}

// ScanResult describes the outcome of scanning the trash directory.
type ScanResult struct {
	RootDir    string `json:"root_dir"`
	Exists     bool   `json:"exists"`
	SizeBytes  int64  `json:"size_bytes"`
	FileCount  int    `json:"file_count"`
	WarnSizeMB int    `json:"warn_size_mb"`
	OverLimit  bool   `json:"over_limit"`
}

// Scan walks the trash root directory and reports its total size.
func Scan(opts ScanOptions) (ScanResult, error) {
	root := strings.TrimSpace(opts.RootDir)
	if root == "" {
		root = "~/.Trash"
	}
	resolvedRoot, err := expandPath(root)
	if err != nil {
		return ScanResult{}, err
	}

	res := ScanResult{
		RootDir:    resolvedRoot,
		WarnSizeMB: opts.WarnSizeMB,
	}

	info, err := os.Stat(resolvedRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return res, nil
		}
		return ScanResult{}, err
	}
	if !info.IsDir() {
		return res, nil
	}
	res.Exists = true

	_ = filepath.WalkDir(resolvedRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		if d.IsDir() {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return nil
		}
		res.SizeBytes += fi.Size()
		res.FileCount++
		return nil
	})

	if opts.WarnSizeMB > 0 && res.SizeBytes > int64(opts.WarnSizeMB)*1024*1024 {
		res.OverLimit = true
	}

	return res, nil
}

func stateFilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(home) == "" {
		return "", errors.New("cannot resolve user home directory")
	}
	return filepath.Join(home, ".config", "ws-tool", "trash-setup.json"), nil
}

func expandPath(path string) (string, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "", errors.New("path cannot be empty")
	}
	if trimmed == "~" || strings.HasPrefix(trimmed, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if trimmed == "~" {
			return home, nil
		}
		return filepath.Join(home, strings.TrimPrefix(trimmed, "~/")), nil
	}
	if filepath.IsAbs(trimmed) {
		return filepath.Clean(trimmed), nil
	}
	abs, err := filepath.Abs(trimmed)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}

func ensureShellRMIntegration(trashRoot string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	scriptPath := filepath.Join(home, ".local", "bin", "ws-trash-rm")
	if err := os.MkdirAll(filepath.Dir(scriptPath), 0o755); err != nil {
		return err
	}

	script := fmt.Sprintf(`#!/usr/bin/env bash
set -euo pipefail

trash_root=%q
mkdir -p "$trash_root"

if [[ $# -eq 0 ]]; then
  echo "usage: rm <path...>" >&2
  exit 1
fi

for arg in "$@"; do
  if [[ "$arg" == -- ]]; then
    continue
  fi
  if [[ "$arg" == -* ]]; then
    continue
  fi
  if [[ ! -e "$arg" && ! -L "$arg" ]]; then
    continue
  fi

  base="$(basename -- "$arg")"
  ts="$(date +%%Y%%m%%d-%%H%%M%%S)"
  dest="$trash_root/${base}.${ts}.$$"
  mv -- "$arg" "$dest"
done
`, trashRoot)

	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		return err
	}

	aliasLine := "alias rm='ws-trash-rm'"
	for _, rc := range []string{filepath.Join(home, ".bashrc"), filepath.Join(home, ".zshrc")} {
		if err := ensureLine(rc, aliasLine); err != nil {
			return err
		}
	}
	return nil
}

func ensureLine(path, line string) error {
	content := ""
	b, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
	} else {
		content = string(b)
	}
	if strings.Contains(content, line) {
		return nil
	}
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	content += line + "\n"
	return os.WriteFile(path, []byte(content), 0o644)
}

func shellRMConfigured() bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	scriptPath := filepath.Join(home, ".local", "bin", "ws-trash-rm")
	if st, err := os.Stat(scriptPath); err != nil || st.Mode()&0o111 == 0 {
		return false
	}
	aliasLine := "alias rm='ws-trash-rm'"
	for _, rc := range []string{filepath.Join(home, ".bashrc"), filepath.Join(home, ".zshrc")} {
		if b, err := os.ReadFile(rc); err == nil && strings.Contains(string(b), aliasLine) {
			return true
		}
	}
	return false
}

// xdgTrashSymlinkPath returns the standard XDG trash directory path.
func xdgTrashSymlinkPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "Trash"), nil
}

// ensureFileExplorerIntegration creates a symlink from the XDG trash directory
// (~/.local/share/Trash) to trashRoot so that XDG-compliant file managers
// (Nautilus, Thunar, Nemo, etc.) move deleted files to the configured trash root.
// If a real directory already exists at the symlink path, it is left untouched
// to avoid data loss.
func ensureFileExplorerIntegration(trashRoot string) error {
	symlinkPath, err := xdgTrashSymlinkPath()
	if err != nil {
		return err
	}

	info, err := os.Lstat(symlinkPath)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			// Already a symlink — update if it points elsewhere.
			target, err := os.Readlink(symlinkPath)
			if err == nil && target == trashRoot {
				return nil // already correct
			}
			if err := os.Remove(symlinkPath); err != nil {
				return fmt.Errorf("removing stale file-explorer symlink: %w", err)
			}
		} else {
			// Real directory exists — don't touch it to avoid data loss.
			return nil
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(symlinkPath), 0o755); err != nil {
		return err
	}
	return os.Symlink(trashRoot, symlinkPath)
}

// fileExplorerConfigured returns true when the XDG trash directory is a symlink
// pointing to the expected trashRoot.
func fileExplorerConfigured(trashRoot string) bool {
	symlinkPath, err := xdgTrashSymlinkPath()
	if err != nil {
		return false
	}
	info, err := os.Lstat(symlinkPath)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		return false
	}
	target, err := os.Readlink(symlinkPath)
	if err != nil {
		return false
	}
	return target == trashRoot
}
