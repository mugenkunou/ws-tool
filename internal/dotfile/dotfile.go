package dotfile

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

type AddOptions struct {
	WorkspacePath string
	ConfigPath    string
	SystemPath    string
	Class         string // "config" or "secret"; empty = auto-detect
	DryRun        bool
}

// LinkOptions configures Link, which registers a system path → workspace
// original mapping WITHOUT moving any files. Use this when the workspace
// original already exists (e.g. agent/AGENTS.md) and the caller just wants ws
// to track the relationship and create the symlink.
type LinkOptions struct {
	WorkspacePath string
	ConfigPath    string
	// SystemPath is the system path that should become the symlink
	// (e.g. ~/.claude/CLAUDE.md). If it already exists and is not a symlink,
	// Link returns an error — use dotfile add to adopt an existing live file.
	SystemPath string
	// WorkspaceRelPath is the path within the workspace of the original file
	// (e.g. "agent/AGENTS.md"). The file must already exist there.
	WorkspaceRelPath string
	// Class is "config", "secret", or "state"; defaults to "config" when empty.
	Class  string
	DryRun bool
}

// LinkResult is returned by Link.
type LinkResult struct {
	Link     wsconfig.Link `json:"link"`
	Messages []string      `json:"messages"`
	DryRun   bool          `json:"dry_run"`
}

type AddResult struct {
	Link     wsconfig.Link `json:"link"`
	Messages []string      `json:"messages"`
	DryRun   bool          `json:"dry_run"`
}

type RemoveOptions struct {
	WorkspacePath string
	ConfigPath    string
	SystemPath    string
	DryRun        bool
}

type RemoveResult struct {
	Link     wsconfig.Link `json:"link"`
	Messages []string      `json:"messages"`
	DryRun   bool          `json:"dry_run"`
}

type ScanOptions struct {
	WorkspacePath string
	ConfigPath    string
}

type FixOptions struct {
	WorkspacePath string
	ConfigPath    string
	DryRun        bool
}

type FixResult struct {
	Fixed     []Issue  `json:"fixed"`
	Unchanged []Issue  `json:"unchanged"`
	Failed    []Issue  `json:"failed"`
	Messages  []string `json:"messages"`
	DryRun    bool     `json:"dry_run"`
}

type Issue struct {
	SystemPath    string `json:"system_path"`
	WorkspacePath string `json:"workspace_path"` // absolute path of the file under configs/dotfiles/ or secrets/dotfiles/
	Status        string `json:"status"`
	Message       string `json:"message"`
}

const (
	StatusBroken      = "BROKEN"
	StatusOverwritten = "OVERWRITTEN"

	// Class constants match wsconfig.Link.Class values.
	ClassConfig = "config"
	ClassSecret = "secret"
)

// dotfilesDirForClass returns the workspace-relative directory for a given class.
func dotfilesDirForClass(class string) string {
	if class == ClassSecret {
		return "secrets/dotfiles"
	}
	return "configs/dotfiles"
}

// DotfilePath returns the workspace-relative path for a dotfile name and class.
func DotfilePath(name, class string) string {
	return filepath.ToSlash(filepath.Join(dotfilesDirForClass(class), name))
}

// LinkDotfilePath returns the workspace-relative path for a wsconfig.Link.
func LinkDotfilePath(link wsconfig.Link) string {
	return DotfilePath(link.Original, link.Class)
}

// resolveClass picks the class: uses provided class if non-empty, otherwise
// classifies the file at absPath. Returns "config" when unambiguous config,
// "secret" when a secret is detected, or "config" as default.
func resolveClass(absPath, class string) string {
	if class == ClassSecret || class == ClassConfig {
		return class
	}
	fc := ClassifyFile(absPath)
	if fc == FileClassSecret {
		return ClassSecret
	}
	return ClassConfig
}

func Add(opts AddOptions) (AddResult, error) {
	result := AddResult{Messages: []string{}}

	absSystem, err := expandPath(opts.SystemPath)
	if err != nil {
		return result, err
	}

	if _, err := os.Lstat(absSystem); err != nil {
		return result, fmt.Errorf("dotfile path not found: %s", absSystem)
	}

	// Directories must not be added directly. Callers should expand to individual
	// files first (e.g. via ExpandDir) so every tracked entry is a specific file.
	realInfo, statErr := os.Stat(absSystem)
	if statErr == nil && realInfo.IsDir() {
		return result, fmt.Errorf("cannot add a directory directly: expand it with 'ws dotfile add <dir>' to select individual files interactively")
	}

	cfg, err := wsconfig.Load(opts.ConfigPath)
	if err != nil {
		return result, err
	}

	for _, link := range cfg.Links {
		if filepath.Clean(link.System) == filepath.Clean(absSystem) {
			return result, fmt.Errorf("dotfile already managed: %s", absSystem)
		}
	}

	class := resolveClass(absSystem, opts.Class)
	wsRel, err := workspaceRelativePath(opts.WorkspacePath, absSystem, class)
	if err != nil {
		return result, err
	}
	// wsRel is like "configs/dotfiles/bashrc"; extract just the name (relative to dotfiles dir).
	dotfilesDir := dotfilesDirForClass(class) + "/"
	name := strings.TrimPrefix(filepath.ToSlash(wsRel), dotfilesDir)
	workspaceAbs := filepath.Join(opts.WorkspacePath, wsRel)

	if _, err := os.Stat(workspaceAbs); err == nil {
		return result, fmt.Errorf("workspace dotfile target already exists: %s", workspaceAbs)
	}

	needsSudo := NeedsSudo(absSystem)
	link := wsconfig.Link{
		System:   absSystem,
		Original: name,
		Sudo:     needsSudo,
		Class:    class,
	}
	result.Link = link

	if opts.DryRun {
		result.DryRun = true
		sudoNote := ""
		if needsSudo {
			sudoNote = " (requires sudo)"
		}
		result.Messages = append(result.Messages,
			"Would move: "+absSystem+" -> "+workspaceAbs+sudoNote,
			"Would symlink: "+absSystem+" -> "+workspaceAbs+sudoNote,
			"Would register in ws.json",
		)
		return result, nil
	}

	if err := os.MkdirAll(filepath.Dir(workspaceAbs), 0o755); err != nil {
		return result, err
	}

	if needsSudo {
		if err := sudoRename(absSystem, workspaceAbs); err != nil {
			return result, err
		}
		if err := sudoSymlink(workspaceAbs, absSystem); err != nil {
			return result, err
		}
	} else {
		if err := movePath(absSystem, workspaceAbs); err != nil {
			return result, err
		}
		if err := os.Symlink(workspaceAbs, absSystem); err != nil {
			return result, err
		}
	}

	if err := wsconfig.AddLink(opts.ConfigPath, link); err != nil {
		return result, err
	}

	result.Messages = append(result.Messages,
		"Moved: "+absSystem+" -> "+workspaceAbs,
		"Linked: "+absSystem+" -> "+workspaceAbs,
	)
	return result, nil
}

func Remove(opts RemoveOptions) (RemoveResult, error) {
	result := RemoveResult{Messages: []string{}}

	absSystem, err := expandPath(opts.SystemPath)
	if err != nil {
		return result, err
	}

	cfg, err := wsconfig.Load(opts.ConfigPath)
	if err != nil {
		return result, err
	}

	idx := -1
	var link wsconfig.Link
	for i, l := range cfg.Links {
		if filepath.Clean(l.System) == filepath.Clean(absSystem) {
			idx = i
			link = l
			break
		}
	}
	if idx < 0 {
		return result, fmt.Errorf("dotfile not managed: %s", absSystem)
	}

	targetAbs := filepath.Join(opts.WorkspacePath, filepath.FromSlash(DotfilePath(link.Original, link.Class)))
	result.Link = link

	if opts.DryRun {
		result.DryRun = true
		result.Messages = append(result.Messages,
			"Would remove symlink: "+absSystem,
			"Would restore: "+targetAbs+" -> "+absSystem,
			"Would unregister from ws.json",
		)
		return result, nil
	}

	if _, err := os.Lstat(absSystem); err == nil {
		if link.Sudo {
			if err := sudoRemoveAll(absSystem); err != nil {
				return result, err
			}
		} else {
			if err := os.RemoveAll(absSystem); err != nil {
				return result, err
			}
		}
	}

	if link.Sudo {
		if err := sudoMkdirAll(filepath.Dir(absSystem)); err != nil {
			return result, err
		}
		if err := sudoRename(targetAbs, absSystem); err != nil {
			return result, err
		}
	} else {
		if err := os.MkdirAll(filepath.Dir(absSystem), 0o755); err != nil {
			return result, err
		}
		if err := movePath(targetAbs, absSystem); err != nil {
			return result, err
		}
	}

	// Remove any empty ancestor directories left behind under the dotfiles root.
	dotfilesRoot := filepath.Clean(filepath.Join(opts.WorkspacePath, dotfilesDirForClass(link.Class)))
	pruneDir := filepath.Dir(targetAbs)
	for {
		pruneDir = filepath.Clean(pruneDir)
		if pruneDir == dotfilesRoot || !strings.HasPrefix(pruneDir, dotfilesRoot+string(os.PathSeparator)) {
			break
		}
		entries, readErr := os.ReadDir(pruneDir)
		if readErr != nil || len(entries) > 0 {
			break
		}
		if err := os.Remove(pruneDir); err != nil {
			break
		}
		pruneDir = filepath.Dir(pruneDir)
	}

	if _, rmErr := wsconfig.RemoveLink(opts.ConfigPath, absSystem); rmErr != nil {
		return result, rmErr
	}

	result.Messages = append(result.Messages,
		"Restored: "+targetAbs+" -> "+absSystem,
		"Unregistered: "+absSystem,
	)
	return result, nil
}

// UnregisterOptions configures Unregister.
type UnregisterOptions struct {
	WorkspacePath string
	ConfigPath    string
	SystemPath    string
}

// Unregister drops a dotfile's link record without touching the live
// file at its system path. This is the registry half of `ws dotfile sync`:
// unlike Remove, it never restores the ws copy onto the system path, so the
// live file (which sync treats as authoritative) is left intact for a
// subsequent Add to capture. The now-superseded ws copy is deleted so Add
// can write the live content to the same location.
func Unregister(opts UnregisterOptions) error {
	absSystem, err := expandPath(opts.SystemPath)
	if err != nil {
		return err
	}

	cfg, err := wsconfig.Load(opts.ConfigPath)
	if err != nil {
		return err
	}

	idx := -1
	var link wsconfig.Link
	for i, l := range cfg.Links {
		if filepath.Clean(l.System) == filepath.Clean(absSystem) {
			idx = i
			link = l
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("dotfile not managed: %s", absSystem)
	}

	targetAbs := filepath.Join(opts.WorkspacePath, filepath.FromSlash(DotfilePath(link.Original, link.Class)))
	if err := os.RemoveAll(targetAbs); err != nil {
		return err
	}

	// Prune now-empty ancestor directories left behind under the dotfiles root.
	dotfilesRoot := filepath.Clean(filepath.Join(opts.WorkspacePath, dotfilesDirForClass(link.Class)))
	pruneDir := filepath.Dir(targetAbs)
	for {
		pruneDir = filepath.Clean(pruneDir)
		if pruneDir == dotfilesRoot || !strings.HasPrefix(pruneDir, dotfilesRoot+string(os.PathSeparator)) {
			break
		}
		entries, readErr := os.ReadDir(pruneDir)
		if readErr != nil || len(entries) > 0 {
			break
		}
		if err := os.Remove(pruneDir); err != nil {
			break
		}
		pruneDir = filepath.Dir(pruneDir)
	}

	_, err = wsconfig.RemoveLink(opts.ConfigPath, absSystem) //nolint:errcheck - idempotent
	return err

}

func List(configPath string) ([]wsconfig.Link, error) {
	cfg, err := wsconfig.Load(configPath)
	if err != nil {
		return nil, err
	}
	return cfg.Links, nil
}

func Scan(opts ScanOptions) ([]Issue, error) {
	cfg, err := wsconfig.Load(opts.ConfigPath)
	if err != nil {
		return nil, err
	}

	issues := make([]Issue, 0)
	for _, link := range cfg.Links {
		expected := filepath.Clean(filepath.Join(opts.WorkspacePath, filepath.FromSlash(DotfilePath(link.Original, link.Class))))

		entry, err := os.Lstat(link.System)
		if err != nil {
			issues = append(issues, Issue{
				SystemPath:    link.System,
				WorkspacePath: expected,
				Status:        StatusBroken,
				Message:       "system path is missing",
			})
			continue
		}

		if entry.Mode()&os.ModeSymlink == 0 {
			issues = append(issues, Issue{
				SystemPath:    link.System,
				WorkspacePath: expected,
				Status:        StatusOverwritten,
				Message:       "system path is no longer a symlink",
			})
			continue
		}

		target, err := os.Readlink(link.System)
		if err != nil {
			issues = append(issues, Issue{
				SystemPath:    link.System,
				WorkspacePath: expected,
				Status:        StatusBroken,
				Message:       "failed to read symlink target",
			})
			continue
		}

		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(link.System), target)
		}
		target = filepath.Clean(target)
		expected = filepath.Clean(expected)

		if target != expected {
			issues = append(issues, Issue{
				SystemPath:    link.System,
				WorkspacePath: expected,
				Status:        StatusOverwritten,
				Message:       "symlink points to an unexpected target",
			})
			continue
		}

		if _, err := os.Stat(expected); err != nil {
			issues = append(issues, Issue{
				SystemPath:    link.System,
				WorkspacePath: expected,
				Status:        StatusBroken,
				Message:       "symlink target is missing",
			})
		}
	}

	return issues, nil
}

func Fix(opts FixOptions) (FixResult, error) {
	result := FixResult{
		Fixed:     []Issue{},
		Unchanged: []Issue{},
		Failed:    []Issue{},
		Messages:  []string{},
		DryRun:    opts.DryRun,
	}

	cfg, err := wsconfig.Load(opts.ConfigPath)
	if err != nil {
		return result, fmt.Errorf("loading config: %w", err)
	}

	for _, link := range cfg.Links {
		expectedTarget := filepath.Clean(filepath.Join(opts.WorkspacePath, filepath.FromSlash(DotfilePath(link.Original, link.Class))))
		issue := Issue{
			SystemPath:    link.System,
			WorkspacePath: expectedTarget,
		}

		// Check if workspace-side file exists.
		if _, err := os.Stat(expectedTarget); err != nil {
			issue.Status = StatusBroken
			issue.Message = "workspace dotfile missing"
			result.Failed = append(result.Failed, issue)
			result.Messages = append(result.Messages, fmt.Sprintf("skip %s: workspace file missing", link.System))
			continue
		}

		// Check current state of system path.
		entry, err := os.Lstat(link.System)
		if err != nil {
			// System path doesn't exist — create symlink.
			if opts.DryRun {
				issue.Status = "WOULD_CREATE"
				issue.Message = fmt.Sprintf("would create symlink %s → %s", link.System, expectedTarget)
				result.Fixed = append(result.Fixed, issue)
				result.Messages = append(result.Messages, issue.Message)
				continue
			}
			if link.Sudo {
				if mkErr := sudoMkdirAll(filepath.Dir(link.System)); mkErr != nil {
					issue.Status = StatusBroken
					issue.Message = fmt.Sprintf("failed to create parent dir: %v", mkErr)
					result.Failed = append(result.Failed, issue)
					continue
				}
				if lnErr := sudoSymlink(expectedTarget, link.System); lnErr != nil {
					issue.Status = StatusBroken
					issue.Message = fmt.Sprintf("failed to create symlink: %v", lnErr)
					result.Failed = append(result.Failed, issue)
					continue
				}
			} else {
				if err := os.MkdirAll(filepath.Dir(link.System), 0o755); err != nil {
					issue.Status = StatusBroken
					issue.Message = fmt.Sprintf("failed to create parent dir: %v", err)
					result.Failed = append(result.Failed, issue)
					continue
				}
				if err := os.Symlink(expectedTarget, link.System); err != nil {
					issue.Status = StatusBroken
					issue.Message = fmt.Sprintf("failed to create symlink: %v", err)
					result.Failed = append(result.Failed, issue)
					continue
				}
			}
			issue.Status = "CREATED"
			issue.Message = fmt.Sprintf("created symlink %s → %s", link.System, expectedTarget)
			result.Fixed = append(result.Fixed, issue)
			result.Messages = append(result.Messages, issue.Message)
			continue
		}

		// System path exists — check if it's already a correct symlink.
		if entry.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(link.System)
			if err == nil {
				if !filepath.IsAbs(target) {
					target = filepath.Join(filepath.Dir(link.System), target)
				}
				if filepath.Clean(target) == filepath.Clean(expectedTarget) {
					issue.Status = "OK"
					issue.Message = "symlink is correct"
					result.Unchanged = append(result.Unchanged, issue)
					continue
				}
			}
		}

		// System path exists but is not correct: overwritten or wrong symlink.
		// Back up the existing file, then create the symlink.
		backupPath := link.System + ".ws-backup"

		if opts.DryRun {
			issue.Status = "WOULD_FIX"
			issue.Message = fmt.Sprintf("would backup %s and create symlink → %s", link.System, expectedTarget)
			result.Fixed = append(result.Fixed, issue)
			result.Messages = append(result.Messages, issue.Message)
			continue
		}

		// Remove any stale backup.
		if link.Sudo {
			_ = sudoRemoveAll(backupPath)
			if err := sudoRename(link.System, backupPath); err != nil {
				issue.Status = StatusBroken
				issue.Message = fmt.Sprintf("failed to backup: %v", err)
				result.Failed = append(result.Failed, issue)
				continue
			}
			if err := sudoSymlink(expectedTarget, link.System); err != nil {
				_ = sudoRename(backupPath, link.System)
				issue.Status = StatusBroken
				issue.Message = fmt.Sprintf("failed to create symlink: %v", err)
				result.Failed = append(result.Failed, issue)
				continue
			}
		} else {
			_ = os.RemoveAll(backupPath)
			if err := os.Rename(link.System, backupPath); err != nil {
				issue.Status = StatusBroken
				issue.Message = fmt.Sprintf("failed to backup: %v", err)
				result.Failed = append(result.Failed, issue)
				continue
			}
			if err := os.Symlink(expectedTarget, link.System); err != nil {
				// Restore backup on failure.
				_ = os.Rename(backupPath, link.System)
				issue.Status = StatusBroken
				issue.Message = fmt.Sprintf("failed to create symlink: %v", err)
				result.Failed = append(result.Failed, issue)
				continue
			}
		}
		issue.Status = "FIXED"
		issue.Message = fmt.Sprintf("backed up and re-linked %s → %s", link.System, expectedTarget)
		result.Fixed = append(result.Fixed, issue)
		result.Messages = append(result.Messages, issue.Message)
	}

	return result, nil
}

// Link registers a system path → existing workspace file as a symlink, without
// moving any files. The workspace original at WorkspaceRelPath must already
// exist. If the system path already exists and is not already the correct
// symlink, Link returns an error (use dotfile add to adopt a live file).
func Link(opts LinkOptions) (LinkResult, error) {
	result := LinkResult{Messages: []string{}}

	absSystem, err := expandPath(opts.SystemPath)
	if err != nil {
		return result, err
	}

	if opts.WorkspaceRelPath == "" {
		return result, errors.New("WorkspaceRelPath must not be empty")
	}
	workspaceAbs := filepath.Join(opts.WorkspacePath, filepath.FromSlash(opts.WorkspaceRelPath))
	if _, err := os.Stat(workspaceAbs); err != nil {
		return result, fmt.Errorf("workspace original not found: %s", workspaceAbs)
	}

	class := opts.Class
	if class == "" {
		class = ClassConfig
	}

	// Check for an existing symlink that already points to the right place.
	if fi, err := os.Lstat(absSystem); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			target, readErr := os.Readlink(absSystem)
			if readErr == nil && filepath.Clean(target) == filepath.Clean(workspaceAbs) {
				// Already correct symlink — idempotent.
				link := wsconfig.Link{
					System:   absSystem,
					Original: opts.WorkspaceRelPath,
					Class:    class,
					Sudo:     NeedsSudo(absSystem),
				}
				result.Link = link
				result.Messages = append(result.Messages, "Already linked: "+absSystem+" -> "+workspaceAbs)
				if err := wsconfig.AddLink(opts.ConfigPath, link); err != nil {
					return result, err
				}
				return result, nil
			}
		}
		if !opts.DryRun {
			return result, fmt.Errorf("system path already exists and is not the expected symlink: %s (use 'ws dotfile add' to adopt a live file)", absSystem)
		}
	}

	link := wsconfig.Link{
		System:   absSystem,
		Original: opts.WorkspaceRelPath,
		Class:    class,
		Sudo:     NeedsSudo(absSystem),
	}
	result.Link = link

	if opts.DryRun {
		result.DryRun = true
		result.Messages = append(result.Messages,
			"Would create symlink: "+absSystem+" -> "+workspaceAbs,
			"Would register in ws.json",
		)
		return result, nil
	}

	if err := os.MkdirAll(filepath.Dir(absSystem), 0o755); err != nil {
		return result, err
	}
	if err := os.Symlink(workspaceAbs, absSystem); err != nil {
		return result, err
	}
	if err := wsconfig.AddLink(opts.ConfigPath, link); err != nil {
		return result, err
	}

	result.Messages = append(result.Messages,
		"Linked: "+absSystem+" -> "+workspaceAbs,
		"Registered in ws.json",
	)
	return result, nil
}

// FindDirEntries returns all link entries whose workspace copy is a directory.
// These are candidates for migration to file-level entries.
func FindDirEntries(workspacePath, configPath string) ([]wsconfig.Link, error) {
	cfg, err := wsconfig.Load(configPath)
	if err != nil {
		return nil, err
	}
	var dirs []wsconfig.Link
	for _, l := range cfg.Links {
		wsPath := filepath.Join(workspacePath, filepath.FromSlash(DotfilePath(l.Original, l.Class)))
		info, err := os.Stat(wsPath)
		if err == nil && info.IsDir() {
			dirs = append(dirs, l)
		}
	}
	return dirs, nil
}

func workspaceRelativePath(workspacePath, systemPath, class string) (string, error) {
	home, _ := os.UserHomeDir()
	cleanSystem := filepath.Clean(systemPath)

	var rel string
	if home != "" && (cleanSystem == home || strings.HasPrefix(cleanSystem, home+string(os.PathSeparator))) {
		rel = strings.TrimPrefix(cleanSystem, home+string(os.PathSeparator))
		if rel == "" {
			return "", errors.New("cannot register home directory as a dotfile")
		}
	} else {
		rel = strings.TrimPrefix(cleanSystem, string(os.PathSeparator))
	}

	parts := strings.Split(rel, string(os.PathSeparator))
	if len(parts) == 0 {
		return "", fmt.Errorf("invalid path: %s", systemPath)
	}

	if after, ok := strings.CutPrefix(parts[0], "."); ok {
		if after == "" {
			after = "dot"
		}
		parts[0] = after
	}

	rel = filepath.Join(parts...)
	rel = filepath.Clean(rel)

	if strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("invalid relative path: %s", rel)
	}

	_ = workspacePath // reserved for future collision strategies.
	return filepath.Join(dotfilesDirForClass(class), rel), nil
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
	return filepath.Abs(trimmed)
}

func movePath(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	} else {
		var linkErr *os.LinkError
		if errors.As(err, &linkErr) && errors.Is(linkErr.Err, syscall.EXDEV) {
			if err := copyAny(src, dst); err != nil {
				return err
			}
			return os.RemoveAll(src)
		}
		return err
	}
}

func copyAny(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}

	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		return os.Symlink(target, dst)
	}

	if info.IsDir() {
		if err := os.MkdirAll(dst, info.Mode().Perm()); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := copyAny(filepath.Join(src, entry.Name()), filepath.Join(dst, entry.Name())); err != nil {
				return err
			}
		}
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}

	return nil
}
