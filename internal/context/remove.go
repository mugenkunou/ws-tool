package context

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type RemoveOptions struct {
	WorkspacePath string
	ProjectPath   string
	Task          string
	All           bool
	DryRun        bool
}

type RemoveEntry struct {
	ProjectPath string `json:"project_path"`
	Task        string `json:"task"`
	Path        string `json:"path"`
	Action      string `json:"action"` // "removed", "would-remove", "not-found"
}

type RemoveResult struct {
	Entries []RemoveEntry `json:"entries"`
	DryRun  bool          `json:"dry_run"`
}

func Remove(opts RemoveOptions) (RemoveResult, error) {
	if strings.TrimSpace(opts.WorkspacePath) == "" {
		return RemoveResult{}, errors.New("workspace path is required")
	}

	result := RemoveResult{
		Entries: []RemoveEntry{},
		DryRun:  opts.DryRun,
	}

	indexPath := contextsIndexPath(opts.WorkspacePath)
	index, err := loadIndex(indexPath)
	if err != nil {
		return result, fmt.Errorf("loading context index: %w", err)
	}

	var targets []ContextRecord
	if opts.All {
		targets = index.Contexts
	} else {
		if strings.TrimSpace(opts.Task) == "" {
			return result, errors.New("task is required (or use --all)")
		}
		projectPath := opts.ProjectPath
		if strings.TrimSpace(projectPath) == "" {
			cwd, err := os.Getwd()
			if err != nil {
				return result, err
			}
			projectPath = cwd
		}
		projectPath = filepath.Clean(projectPath)
		for _, rec := range index.Contexts {
			if filepath.Clean(rec.ProjectPath) == projectPath && rec.Task == opts.Task {
				targets = append(targets, rec)
				break
			}
		}
		if len(targets) == 0 {
			result.Entries = append(result.Entries, RemoveEntry{
				ProjectPath: projectPath,
				Task:        opts.Task,
				Path:        filepath.Join(projectPath, ".ws-context", opts.Task),
				Action:      "not-found",
			})
			return result, nil
		}
	}

	for _, rec := range targets {
		contextDir := filepath.Join(rec.ProjectPath, ".ws-context", rec.Task)
		entry := RemoveEntry{
			ProjectPath: rec.ProjectPath,
			Task:        rec.Task,
			Path:        contextDir,
		}

		if opts.DryRun {
			entry.Action = "would-remove"
			result.Entries = append(result.Entries, entry)
			continue
		}

		if err := os.RemoveAll(contextDir); err != nil {
			return result, fmt.Errorf("removing %s: %w", contextDir, err)
		}
		entry.Action = "removed"
		result.Entries = append(result.Entries, entry)

		// Clean up empty .ws-context/ parent.
		wsContextDir := filepath.Join(rec.ProjectPath, ".ws-context")
		cleanEmptyDir(wsContextDir)

		// Clean up git exclude if no .ws-context/ tasks remain.
		if isEmpty, _ := isDirEmpty(wsContextDir); isEmpty || !dirExists(wsContextDir) {
			removeGitExcludeLine(rec.ProjectPath)
		}

		// Remove from index.
		if err := Untrack(opts.WorkspacePath, rec.ProjectPath, rec.Task); err != nil {
			return result, fmt.Errorf("untracking: %w", err)
		}
	}

	return result, nil
}

func Untrack(workspacePath, projectPath, task string) error {
	indexPath := contextsIndexPath(workspacePath)
	index, err := loadIndex(indexPath)
	if err != nil {
		return err
	}

	projectPath = filepath.Clean(projectPath)
	var kept []ContextRecord
	for _, rec := range index.Contexts {
		if filepath.Clean(rec.ProjectPath) == projectPath && rec.Task == task {
			continue
		}
		kept = append(kept, rec)
	}

	if kept == nil {
		kept = []ContextRecord{}
	}
	index.Contexts = kept
	return saveIndex(indexPath, index)
}

func cleanEmptyDir(path string) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return
	}
	if len(entries) == 0 {
		os.Remove(path)
	}
}

func isDirEmpty(path string) (bool, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return false, err
	}
	return len(entries) == 0, nil
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func removeGitExcludeLine(projectPath string) {
	gr, ok := gitRoot(projectPath)
	if !ok {
		return
	}
	excludePath := filepath.Join(gr, ".git", "info", "exclude")
	_ = removeLineFromFile(excludePath, ".ws-context/")
}

func removeLineFromFile(path, line string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	var result []string
	for _, l := range strings.Split(string(content), "\n") {
		if strings.TrimSpace(l) != strings.TrimSpace(line) {
			result = append(result, l)
		}
	}

	newContent := strings.Join(result, "\n")
	return os.WriteFile(path, []byte(newContent), 0o644)
}
