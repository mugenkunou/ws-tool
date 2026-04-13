package dotfile

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/mugenkunou/ws-tool/internal/manifest"
)

type AddOptions struct {
	WorkspacePath string
	ManifestPath  string
	SystemPath    string
	DryRun        bool
}

type AddResult struct {
	Record   manifest.DotfileRecord `json:"record"`
	Messages []string               `json:"messages"`
	DryRun   bool                   `json:"dry_run"`
}

type RemoveOptions struct {
	WorkspacePath string
	ManifestPath  string
	SystemPath    string
	DryRun        bool
}

type RemoveResult struct {
	Record   manifest.DotfileRecord `json:"record"`
	Messages []string               `json:"messages"`
	DryRun   bool                   `json:"dry_run"`
}

type ScanOptions struct {
	WorkspacePath string
	ManifestPath  string
}

type FixOptions struct {
	WorkspacePath string
	ManifestPath  string
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
	WorkspacePath string `json:"workspace_path"`
	Status        string `json:"status"`
	Message       string `json:"message"`
}

const (
	StatusBroken      = "BROKEN"
	StatusOverwritten = "OVERWRITTEN"
)

const dotfilesDir = "ws/dotfiles"

// DotfilePath returns the workspace-relative path for a dotfile name.
func DotfilePath(name string) string {
	return filepath.ToSlash(filepath.Join(dotfilesDir, name))
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

	m, err := manifest.Load(opts.ManifestPath)
	if err != nil {
		return result, err
	}

	for _, record := range m.Dotfiles {
		if filepath.Clean(record.System) == filepath.Clean(absSystem) {
			return result, fmt.Errorf("dotfile already managed: %s", absSystem)
		}
	}

	workspaceRel, err := workspaceRelativePath(opts.WorkspacePath, absSystem)
	if err != nil {
		return result, err
	}
	// workspaceRel is like "ws/dotfiles/bashrc"; extract just the name.
	name := strings.TrimPrefix(filepath.ToSlash(workspaceRel), dotfilesDir+"/")
	workspaceAbs := filepath.Join(opts.WorkspacePath, workspaceRel)

	if _, err := os.Stat(workspaceAbs); err == nil {
		return result, fmt.Errorf("workspace dotfile target already exists: %s", workspaceAbs)
	}

	record := manifest.DotfileRecord{
		System: absSystem,
		Name:   name,
	}
	result.Record = record

	if opts.DryRun {
		result.DryRun = true
		result.Messages = append(result.Messages,
			"Would move: "+absSystem+" -> "+workspaceAbs,
			"Would symlink: "+absSystem+" -> "+workspaceAbs,
			"Would register in manifest",
		)
		return result, nil
	}

	if err := os.MkdirAll(filepath.Dir(workspaceAbs), 0o755); err != nil {
		return result, err
	}

	if err := movePath(absSystem, workspaceAbs); err != nil {
		return result, err
	}

	if err := os.Symlink(workspaceAbs, absSystem); err != nil {
		return result, err
	}

	m.Dotfiles = append(m.Dotfiles, record)
	if err := manifest.Save(opts.ManifestPath, m); err != nil {
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

	m, err := manifest.Load(opts.ManifestPath)
	if err != nil {
		return result, err
	}

	idx := -1
	var record manifest.DotfileRecord
	for i, r := range m.Dotfiles {
		if filepath.Clean(r.System) == filepath.Clean(absSystem) {
			idx = i
			record = r
			break
		}
	}
	if idx < 0 {
		return result, fmt.Errorf("dotfile not managed: %s", absSystem)
	}

	targetAbs := filepath.Join(opts.WorkspacePath, filepath.FromSlash(DotfilePath(record.Name)))
	result.Record = record

	if opts.DryRun {
		result.DryRun = true
		result.Messages = append(result.Messages,
			"Would remove symlink: "+absSystem,
			"Would restore: "+targetAbs+" -> "+absSystem,
			"Would unregister from manifest",
		)
		return result, nil
	}

	if _, err := os.Lstat(absSystem); err == nil {
		if err := os.RemoveAll(absSystem); err != nil {
			return result, err
		}
	}

	if err := os.MkdirAll(filepath.Dir(absSystem), 0o755); err != nil {
		return result, err
	}

	if err := movePath(targetAbs, absSystem); err != nil {
		return result, err
	}

	m.Dotfiles = append(m.Dotfiles[:idx], m.Dotfiles[idx+1:]...)
	if err := manifest.Save(opts.ManifestPath, m); err != nil {
		return result, err
	}

	result.Messages = append(result.Messages,
		"Restored: "+targetAbs+" -> "+absSystem,
		"Unregistered: "+absSystem,
	)
	return result, nil
}

func List(manifestPath string) ([]manifest.DotfileRecord, error) {
	m, err := manifest.Load(manifestPath)
	if err != nil {
		return nil, err
	}
	return m.Dotfiles, nil
}

func Scan(opts ScanOptions) ([]Issue, error) {
	m, err := manifest.Load(opts.ManifestPath)
	if err != nil {
		return nil, err
	}

	issues := make([]Issue, 0)
	for _, record := range m.Dotfiles {
		expected := filepath.Join(opts.WorkspacePath, filepath.FromSlash(DotfilePath(record.Name)))

		entry, err := os.Lstat(record.System)
		if err != nil {
			issues = append(issues, Issue{
				SystemPath:    record.System,
				WorkspacePath: DotfilePath(record.Name),
				Status:        StatusBroken,
				Message:       "system path is missing",
			})
			continue
		}

		if entry.Mode()&os.ModeSymlink == 0 {
			issues = append(issues, Issue{
				SystemPath:    record.System,
				WorkspacePath: DotfilePath(record.Name),
				Status:        StatusOverwritten,
				Message:       "system path is no longer a symlink",
			})
			continue
		}

		target, err := os.Readlink(record.System)
		if err != nil {
			issues = append(issues, Issue{
				SystemPath:    record.System,
				WorkspacePath: DotfilePath(record.Name),
				Status:        StatusBroken,
				Message:       "failed to read symlink target",
			})
			continue
		}

		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(record.System), target)
		}
		target = filepath.Clean(target)
		expected = filepath.Clean(expected)

		if target != expected {
			issues = append(issues, Issue{
				SystemPath:    record.System,
				WorkspacePath: DotfilePath(record.Name),
				Status:        StatusOverwritten,
				Message:       "symlink points to an unexpected target",
			})
			continue
		}

		if _, err := os.Stat(expected); err != nil {
			issues = append(issues, Issue{
				SystemPath:    record.System,
				WorkspacePath: DotfilePath(record.Name),
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

	m, err := manifest.Load(opts.ManifestPath)
	if err != nil {
		return result, fmt.Errorf("loading manifest: %w", err)
	}

	for _, record := range m.Dotfiles {
		expectedTarget := filepath.Join(opts.WorkspacePath, filepath.FromSlash(DotfilePath(record.Name)))
		issue := Issue{
			SystemPath:    record.System,
			WorkspacePath: DotfilePath(record.Name),
		}

		// Check if workspace-side file exists.
		if _, err := os.Stat(expectedTarget); err != nil {
			issue.Status = StatusBroken
			issue.Message = "workspace dotfile missing"
			result.Failed = append(result.Failed, issue)
			result.Messages = append(result.Messages, fmt.Sprintf("skip %s: workspace file missing", record.System))
			continue
		}

		// Check current state of system path.
		entry, err := os.Lstat(record.System)
		if err != nil {
			// System path doesn't exist — create symlink.
			if opts.DryRun {
				issue.Status = "WOULD_CREATE"
				issue.Message = fmt.Sprintf("would create symlink %s → %s", record.System, expectedTarget)
				result.Fixed = append(result.Fixed, issue)
				result.Messages = append(result.Messages, issue.Message)
				continue
			}
			if err := os.MkdirAll(filepath.Dir(record.System), 0o755); err != nil {
				issue.Status = StatusBroken
				issue.Message = fmt.Sprintf("failed to create parent dir: %v", err)
				result.Failed = append(result.Failed, issue)
				continue
			}
			if err := os.Symlink(expectedTarget, record.System); err != nil {
				issue.Status = StatusBroken
				issue.Message = fmt.Sprintf("failed to create symlink: %v", err)
				result.Failed = append(result.Failed, issue)
				continue
			}
			issue.Status = "CREATED"
			issue.Message = fmt.Sprintf("created symlink %s → %s", record.System, expectedTarget)
			result.Fixed = append(result.Fixed, issue)
			result.Messages = append(result.Messages, issue.Message)
			continue
		}

		// System path exists — check if it's already a correct symlink.
		if entry.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(record.System)
			if err == nil {
				if !filepath.IsAbs(target) {
					target = filepath.Join(filepath.Dir(record.System), target)
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
		backupPath := record.System + ".ws-backup"

		if opts.DryRun {
			issue.Status = "WOULD_FIX"
			issue.Message = fmt.Sprintf("would backup %s and create symlink → %s", record.System, expectedTarget)
			result.Fixed = append(result.Fixed, issue)
			result.Messages = append(result.Messages, issue.Message)
			continue
		}

		// Remove any stale backup.
		_ = os.RemoveAll(backupPath)
		if err := os.Rename(record.System, backupPath); err != nil {
			issue.Status = StatusBroken
			issue.Message = fmt.Sprintf("failed to backup: %v", err)
			result.Failed = append(result.Failed, issue)
			continue
		}
		if err := os.Symlink(expectedTarget, record.System); err != nil {
			// Restore backup on failure.
			_ = os.Rename(backupPath, record.System)
			issue.Status = StatusBroken
			issue.Message = fmt.Sprintf("failed to create symlink: %v", err)
			result.Failed = append(result.Failed, issue)
			continue
		}
		issue.Status = "FIXED"
		issue.Message = fmt.Sprintf("backed up and re-linked %s → %s", record.System, expectedTarget)
		result.Fixed = append(result.Fixed, issue)
		result.Messages = append(result.Messages, issue.Message)
	}

	return result, nil
}

func workspaceRelativePath(workspacePath, systemPath string) (string, error) {
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

	if strings.HasPrefix(parts[0], ".") {
		parts[0] = strings.TrimPrefix(parts[0], ".")
		if parts[0] == "" {
			parts[0] = "dot"
		}
	}

	rel = filepath.Join(parts...)
	rel = filepath.Clean(rel)

	if strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("invalid relative path: %s", rel)
	}

	_ = workspacePath // reserved for future collision strategies.
	return filepath.Join("ws", "dotfiles", rel), nil
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
