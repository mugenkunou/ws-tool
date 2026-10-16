// Package trash implements ws's soft-delete subsystem. By default, ws shares
// the standard XDG trash directory (~/.local/share/Trash) with the desktop
// environment: items deleted via ws (scratch rm, ignore fix, etc.), via a
// shell `rm` configured by `ws trash setup`, and via the file manager's
// Delete/right-click-Trash all land in the exact same place, in the standard
// files/ + info/*.trashinfo layout (https://specifications.freedesktop.org/trash-spec).
// This means `ws trash empty` and the desktop's own "Empty Trash" action
// operate on the same content, and anything ws deletes is visible and
// restorable from the file manager.
package trash

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

// xdgTrashDir returns the user's standard XDG trash directory:
// $XDG_DATA_HOME/Trash, defaulting to ~/.local/share/Trash. This is also the
// default trash root ws uses when trash.root_dir is unset, so that ws's own
// deletions and the desktop's deletions share one location.
func xdgTrashDir() (string, error) {
	if dataHome := strings.TrimSpace(os.Getenv("XDG_DATA_HOME")); dataHome != "" {
		return filepath.Join(dataHome, "Trash"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "Trash"), nil
}

func filesDir(root string) string { return filepath.Join(root, "files") }
func infoDir(root string) string  { return filepath.Join(root, "info") }

// SoftDelete moves path into the trash (by default the XDG trash directory)
// so it is recoverable and visible to the desktop's file manager. This is
// the implementation of the "soft-delete first" principle: ws-internal
// delete paths (scratch rm, scratch prune, etc.) call SoftDelete instead of
// os.RemoveAll, unconditionally — no `ws trash setup` step is required.
func SoftDelete(path string) error {
	root, err := xdgTrashDir()
	if err != nil {
		return os.RemoveAll(path)
	}
	_, err = moveToTrash(root, path)
	return err
}

// moveToTrash moves path into root's files/ directory and writes a matching
// freedesktop .trashinfo sidecar into root's info/ directory. Returns the
// name the item was stored under (after collision resolution).
func moveToTrash(root, path string) (string, error) {
	if err := os.MkdirAll(filesDir(root), 0o755); err != nil {
		return "", err
	}
	if err := os.MkdirAll(infoDir(root), 0o755); err != nil {
		return "", err
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}

	base := filepath.Base(absPath)
	name := uniqueTrashName(root, base)
	dest := filepath.Join(filesDir(root), name)

	if err := renameOrMove(absPath, dest); err != nil {
		return "", err
	}
	if err := writeTrashInfo(root, name, absPath, time.Now()); err != nil {
		return name, err
	}
	return name, nil
}

// uniqueTrashName returns a name for base that doesn't collide with an
// existing files/ entry or info/ sidecar, appending ".2", ".3", … as needed —
// the same scheme gio/Nautilus use.
func uniqueTrashName(root, base string) string {
	name := base
	for n := 2; ; n++ {
		_, filesErr := os.Lstat(filepath.Join(filesDir(root), name))
		_, infoErr := os.Lstat(filepath.Join(infoDir(root), name+".trashinfo"))
		if os.IsNotExist(filesErr) && os.IsNotExist(infoErr) {
			return name
		}
		name = fmt.Sprintf("%s.%d", base, n)
	}
}

// writeTrashInfo writes the freedesktop .trashinfo sidecar for a trashed
// item: original path (percent-encoded per the spec) and deletion timestamp.
func writeTrashInfo(root, name, originalPath string, deletedAt time.Time) error {
	encoded := (&url.URL{Path: originalPath}).EscapedPath()
	content := fmt.Sprintf("[Trash Info]\nPath=%s\nDeletionDate=%s\n",
		encoded, deletedAt.Format("2006-01-02T15:04:05"))
	return os.WriteFile(filepath.Join(infoDir(root), name+".trashinfo"), []byte(content), 0o644)
}

// renameOrMove renames src to dst, falling back to `mv` (via coreutils) when
// the rename fails because src/dst are on different filesystems.
func renameOrMove(src, dst string) error {
	err := os.Rename(src, dst)
	if err == nil {
		return nil
	}
	var linkErr *os.LinkError
	if errors.As(err, &linkErr) {
		if errno, ok := linkErr.Err.(syscall.Errno); ok && errno == syscall.EXDEV {
			return exec.Command("mv", "--", src, dst).Run()
		}
	}
	return err
}

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

// ErrXDGTrashDirExists is returned by ensureFileExplorerIntegration when
// trash.root_dir has been set to a custom path (different from the default
// XDG trash directory) and ~/.local/share/Trash already exists as a real
// directory. ws cannot replace it with a symlink without risking data loss,
// so the desktop file manager won't see the custom trash root. This doesn't
// apply when trash.root_dir is left at its default — then ws uses the XDG
// directory directly and there is nothing to redirect.
var ErrXDGTrashDirExists = errors.New(
	"~/.local/share/Trash is a real directory managed by the OS; " +
		"ws cannot safely replace it with a symlink to a custom trash.root_dir. " +
		"Leave trash.root_dir at its default to share the same trash as the file manager.",
)

type Status struct {
	RootDir                        string `json:"root_dir"`
	ShellRMConfigured              bool   `json:"shell_rm_configured"`
	VSCodeConfigured               bool   `json:"vscode_configured"`
	FileExplorerConfigured         bool   `json:"file_explorer_configured"`
	FileExplorerUnconfiguredReason string `json:"file_explorer_unconfigured_reason,omitempty"`
	StatePath                      string `json:"state_path"`
}

func resolveRoot(hint string) (string, error) {
	root := strings.TrimSpace(hint)
	if root == "" {
		return xdgTrashDir()
	}
	return expandPath(root)
}

func Setup(opts SetupOptions) (SetupResult, error) {
	resolvedRoot, err := resolveRoot(opts.RootDir)
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

	if err := os.MkdirAll(filesDir(resolvedRoot), 0o755); err != nil {
		return SetupResult{}, err
	}
	if err := os.MkdirAll(infoDir(resolvedRoot), 0o755); err != nil {
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
			if !errors.Is(err, ErrXDGTrashDirExists) {
				return SetupResult{}, err
			}
			// Custom root_dir conflicts with a real XDG trash dir — degrade
			// gracefully. Shell-rm integration is unaffected. The caller
			// learns the situation via GetStatus's FileExplorerUnconfiguredReason.
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
	resolvedRoot, err := resolveRoot(rootHint)
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

	// Populate a human-readable reason when file-explorer integration is not
	// configured, so the CLI and TUI can surface an actionable explanation.
	if !st.FileExplorerConfigured {
		if xdgPath, err := xdgTrashDir(); err == nil {
			if st.RootDir == xdgPath {
				st.FileExplorerUnconfiguredReason = "trash directories have not been created yet; run `ws trash setup`."
			} else if info, lErr := os.Lstat(xdgPath); lErr == nil && info.Mode()&os.ModeSymlink == 0 {
				st.FileExplorerUnconfiguredReason = xdgPath + " is a real directory managed by the OS; " +
					"ws cannot safely redirect it to the custom trash.root_dir. " +
					"Shell-rm soft-delete still works; leave trash.root_dir at its default to unify with the file manager."
			}
		}
	}

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

// EmptyOptions configures emptying the trash directory.
type EmptyOptions struct {
	RootDir string
	DryRun  bool
}

// EmptyResult describes the outcome of emptying the trash directory.
type EmptyResult struct {
	RootDir    string `json:"root_dir"`
	Exists     bool   `json:"exists"`
	EntryCount int    `json:"entry_count"`
	FileCount  int    `json:"file_count"`
	SizeBytes  int64  `json:"size_bytes"`
	DryRun     bool   `json:"dry_run"`
}

// Scan walks the trash root's files/ directory and reports its total size.
func Scan(opts ScanOptions) (ScanResult, error) {
	resolvedRoot, err := resolveRoot(opts.RootDir)
	if err != nil {
		return ScanResult{}, err
	}

	res := ScanResult{
		RootDir:    resolvedRoot,
		WarnSizeMB: opts.WarnSizeMB,
	}

	filesPath := filesDir(resolvedRoot)
	info, err := os.Stat(filesPath)
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

	_ = filepath.WalkDir(filesPath, func(path string, d os.DirEntry, err error) error {
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

// Empty removes all entries inside the trash root's files/ and info/
// directories while keeping the directories themselves intact. If the trash
// directory does not exist, the operation is a successful no-op.
func Empty(opts EmptyOptions) (EmptyResult, error) {
	resolvedRoot, err := resolveRoot(opts.RootDir)
	if err != nil {
		return EmptyResult{}, err
	}

	res := EmptyResult{
		RootDir: resolvedRoot,
		DryRun:  opts.DryRun,
	}

	filesPath := filesDir(resolvedRoot)
	info, err := os.Stat(filesPath)
	if err != nil {
		if os.IsNotExist(err) {
			return res, nil
		}
		return EmptyResult{}, err
	}
	if !info.IsDir() {
		return EmptyResult{}, fmt.Errorf("trash files directory is not a directory: %s", filesPath)
	}
	res.Exists = true

	entries, err := os.ReadDir(filesPath)
	if err != nil {
		return EmptyResult{}, err
	}
	res.EntryCount = len(entries)

	scanRes, err := Scan(ScanOptions{RootDir: resolvedRoot})
	if err != nil {
		return EmptyResult{}, err
	}
	res.FileCount = scanRes.FileCount
	res.SizeBytes = scanRes.SizeBytes

	if opts.DryRun {
		return res, nil
	}

	for _, e := range entries {
		if err := os.RemoveAll(filepath.Join(filesPath, e.Name())); err != nil {
			return EmptyResult{}, err
		}
	}
	if infoEntries, err := os.ReadDir(infoDir(resolvedRoot)); err == nil {
		for _, e := range infoEntries {
			_ = os.RemoveAll(filepath.Join(infoDir(resolvedRoot), e.Name()))
		}
	}

	return res, nil
}

func stateFilePath() (string, error) {
	dir, err := wsconfig.StateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "trash-setup.json"), nil
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

// ensureShellRMIntegration writes a `ws-trash-rm` helper script that moves
// arguments into trashRoot's files/ directory and writes a matching
// freedesktop .trashinfo sidecar into info/, and aliases `rm` to it in the
// user's shell rc files.
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
files_dir="$trash_root/files"
info_dir="$trash_root/info"
mkdir -p "$files_dir" "$info_dir"

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
  name="$base"
  n=2
  while [[ -e "$files_dir/$name" || -e "$info_dir/$name.trashinfo" ]]; do
    name="$base.$n"
    n=$((n + 1))
  done

  orig="$(realpath -m -- "$arg" 2>/dev/null || echo "$arg")"
  encoded="$(python3 -c 'import sys,urllib.parse; print(urllib.parse.quote(sys.argv[1], safe="/"))' "$orig")"
  mv -- "$arg" "$files_dir/$name"
  {
    echo "[Trash Info]"
    echo "Path=$encoded"
    echo "DeletionDate=$(date +%%Y-%%m-%%dT%%H:%%M:%%S)"
  } > "$info_dir/$name.trashinfo"
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

// ensureFileExplorerIntegration makes the desktop file manager's trash and
// ws's trash the same location. When trashRoot is the default XDG trash
// directory, there's nothing to redirect — Setup already created the
// standard files/+info/ structure there, which the file manager reads
// natively. When trashRoot is a custom path (trash.root_dir overridden), a
// symlink from the XDG location to trashRoot is used instead, matching the
// pre-unification behavior; if a real directory already occupies the XDG
// path in that case, it is left untouched (ErrXDGTrashDirExists).
func ensureFileExplorerIntegration(trashRoot string) error {
	xdgPath, err := xdgTrashDir()
	if err != nil {
		return err
	}

	if trashRoot == xdgPath {
		// trashRoot already IS the XDG trash dir; Setup already created the
		// files/+info/ structure there. Nothing further to do.
		return nil
	}

	info, err := os.Lstat(xdgPath)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			// Already a symlink — update if it points elsewhere.
			target, err := os.Readlink(xdgPath)
			if err == nil && target == trashRoot {
				return nil // already correct
			}
			if err := os.Remove(xdgPath); err != nil {
				return fmt.Errorf("removing stale file-explorer symlink: %w", err)
			}
		} else {
			// Real directory exists — don't touch it to avoid data loss.
			return ErrXDGTrashDirExists
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(xdgPath), 0o755); err != nil {
		return err
	}
	return os.Symlink(trashRoot, xdgPath)
}

// fileExplorerConfigured reports whether the file manager and ws share the
// same trash. When trashRoot is the default XDG trash directory, this is
// true as soon as the files/+info/ structure exists there. When trashRoot is
// a custom path, this requires the XDG location to be a symlink pointing at
// trashRoot.
func fileExplorerConfigured(trashRoot string) bool {
	xdgPath, err := xdgTrashDir()
	if err != nil {
		return false
	}

	if trashRoot == xdgPath {
		filesInfo, err := os.Stat(filesDir(trashRoot))
		if err != nil || !filesInfo.IsDir() {
			return false
		}
		infoInfo, err := os.Stat(infoDir(trashRoot))
		return err == nil && infoInfo.IsDir()
	}

	info, err := os.Lstat(xdgPath)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		return false
	}
	target, err := os.Readlink(xdgPath)
	if err != nil {
		return false
	}
	return target == trashRoot
}
