package context

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type ContextRecord struct {
	ProjectPath string `json:"project_path"`
	Task        string `json:"task"`
	ContextDir  string `json:"context_dir"`
	ContextPath string `json:"context_path"`
	LastSeen    string `json:"last_seen"`
}

type ContextIndex struct {
	Contexts []ContextRecord `json:"contexts"`
}

type ListOptions struct {
	WorkspacePath string
	Update        bool
}

type ListResult struct {
	IndexPath string          `json:"index_path"`
	Updated   bool            `json:"updated"`
	Contexts  []ContextRecord `json:"contexts"`
}

func Track(workspacePath, projectPath, task string) error {
	if strings.TrimSpace(workspacePath) == "" {
		return errors.New("workspace path is required")
	}
	if strings.TrimSpace(projectPath) == "" {
		return errors.New("project path is required")
	}
	if strings.TrimSpace(task) == "" {
		return errors.New("task is required")
	}

	indexPath := contextsIndexPath(workspacePath)
	index, err := loadIndex(indexPath)
	if err != nil {
		return err
	}

	contextDir := filepath.ToSlash(filepath.Join(".ws-context", task))
	contextPath := filepath.Join(projectPath, ".ws-context", task)
	rec := ContextRecord{
		ProjectPath: filepath.Clean(projectPath),
		Task:        task,
		ContextDir:  contextDir,
		ContextPath: filepath.Clean(contextPath),
		LastSeen:    time.Now().UTC().Format(time.RFC3339),
	}

	index.Contexts = upsertRecord(index.Contexts, rec)
	sortRecords(index.Contexts)
	return saveIndex(indexPath, index)
}

func List(opts ListOptions) (ListResult, error) {
	if strings.TrimSpace(opts.WorkspacePath) == "" {
		return ListResult{}, errors.New("workspace path is required")
	}

	indexPath := contextsIndexPath(opts.WorkspacePath)
	if opts.Update {
		records, err := scanWorkspaceContexts(opts.WorkspacePath)
		if err != nil {
			return ListResult{}, err
		}
		index := ContextIndex{Contexts: records}
		sortRecords(index.Contexts)
		if err := saveIndex(indexPath, index); err != nil {
			return ListResult{}, err
		}
		return ListResult{IndexPath: indexPath, Updated: true, Contexts: index.Contexts}, nil
	}

	index, err := loadIndex(indexPath)
	if err != nil {
		return ListResult{}, err
	}
	sortRecords(index.Contexts)
	return ListResult{IndexPath: indexPath, Updated: false, Contexts: index.Contexts}, nil
}

func contextsIndexPath(workspacePath string) string {
	return filepath.Join(workspacePath, "ws", "contexts.json")
}

func loadIndex(path string) (ContextIndex, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ContextIndex{Contexts: []ContextRecord{}}, nil
		}
		return ContextIndex{}, err
	}

	idx := ContextIndex{Contexts: []ContextRecord{}}
	if len(strings.TrimSpace(string(content))) == 0 {
		return idx, nil
	}
	if err := json.Unmarshal(content, &idx); err != nil {
		return ContextIndex{}, err
	}
	if idx.Contexts == nil {
		idx.Contexts = []ContextRecord{}
	}
	return idx, nil
}

func saveIndex(path string, idx ContextIndex) error {
	if idx.Contexts == nil {
		idx.Contexts = []ContextRecord{}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	return os.WriteFile(path, encoded, 0o644)
}

func upsertRecord(existing []ContextRecord, rec ContextRecord) []ContextRecord {
	for i := range existing {
		if sameRecord(existing[i], rec) {
			existing[i] = rec
			return existing
		}
	}
	return append(existing, rec)
}

func sameRecord(a, b ContextRecord) bool {
	return filepath.Clean(a.ProjectPath) == filepath.Clean(b.ProjectPath) && a.Task == b.Task
}

func sortRecords(records []ContextRecord) {
	sort.Slice(records, func(i, j int) bool {
		if records[i].ProjectPath == records[j].ProjectPath {
			return records[i].Task < records[j].Task
		}
		return records[i].ProjectPath < records[j].ProjectPath
	})
}

func scanWorkspaceContexts(workspacePath string) ([]ContextRecord, error) {
	var records []ContextRecord
	now := time.Now().UTC().Format(time.RFC3339)

	err := filepath.WalkDir(workspacePath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}

		name := d.Name()
		if name == "ws" || name == ".git" || name == "node_modules" || name == ".venv" {
			if path != workspacePath {
				return filepath.SkipDir
			}
		}

		if name != ".ws-context" {
			return nil
		}

		projectPath := filepath.Dir(path)
		entries, readErr := os.ReadDir(path)
		if readErr != nil {
			return readErr
		}

		for _, child := range entries {
			if !child.IsDir() {
				continue
			}
			task := child.Name()
			records = append(records, ContextRecord{
				ProjectPath: filepath.Clean(projectPath),
				Task:        task,
				ContextDir:  filepath.ToSlash(filepath.Join(".ws-context", task)),
				ContextPath: filepath.Join(path, task),
				LastSeen:    now,
			})
		}

		return filepath.SkipDir
	})
	if err != nil {
		return nil, err
	}

	sortRecords(records)
	return records, nil
}
