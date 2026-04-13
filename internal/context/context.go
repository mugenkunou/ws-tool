package context

import (
	"bufio"
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type CreateOptions struct {
	ProjectPath string
	Task        string
	DryRun      bool
}

type CreateResult struct {
	ProjectPath       string `json:"project"`
	Task              string `json:"task"`
	ContextDir        string `json:"context_dir"`
	Created           bool   `json:"created"`
	GitRepo           bool   `json:"git_repo"`
	GitExcludeUpdated bool   `json:"git_exclude_updated"`
}

func Create(opts CreateOptions) (CreateResult, error) {
	if strings.TrimSpace(opts.Task) == "" {
		return CreateResult{}, errors.New("task is required")
	}

	projectPath := opts.ProjectPath
	if strings.TrimSpace(projectPath) == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return CreateResult{}, err
		}
		projectPath = cwd
	}
	projectPath = filepath.Clean(projectPath)

	contextDirAbs := filepath.Join(projectPath, ".ws-context", opts.Task)
	contextDirRel := filepath.ToSlash(filepath.Join(".ws-context", opts.Task))

	res := CreateResult{
		ProjectPath: projectPath,
		Task:        opts.Task,
		ContextDir:  contextDirRel,
	}

	if opts.DryRun {
		if _, err := os.Stat(contextDirAbs); err != nil {
			res.Created = true
		}
		gr, ok := gitRoot(projectPath)
		res.GitRepo = ok
		if ok {
			updated, _ := shouldUpdateExclude(filepath.Join(gr, ".git", "info", "exclude"))
			res.GitExcludeUpdated = updated
		}
		return res, nil
	}

	if _, err := os.Stat(contextDirAbs); err != nil {
		if err := os.MkdirAll(contextDirAbs, 0o755); err != nil {
			return CreateResult{}, err
		}
		res.Created = true
	}

	gitRootPath, ok := gitRoot(projectPath)
	res.GitRepo = ok
	if ok {
		excludePath := filepath.Join(gitRootPath, ".git", "info", "exclude")
		update, err := shouldUpdateExclude(excludePath)
		if err != nil {
			return CreateResult{}, err
		}
		if update {
			if err := os.MkdirAll(filepath.Dir(excludePath), 0o755); err != nil {
				return CreateResult{}, err
			}
			f, err := os.OpenFile(excludePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
			if err != nil {
				return CreateResult{}, err
			}
			defer f.Close()
			if _, err := f.WriteString(".ws-context/\n"); err != nil {
				return CreateResult{}, err
			}
			res.GitExcludeUpdated = true
		}
	}

	return res, nil
}

func gitRoot(projectPath string) (string, bool) {
	cmd := exec.Command("git", "-C", projectPath, "rev-parse", "--show-toplevel")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", false
	}
	root := strings.TrimSpace(out.String())
	if root == "" {
		return "", false
	}
	return root, true
}

func shouldUpdateExclude(excludePath string) (bool, error) {
	f, err := os.Open(excludePath)
	if err != nil {
		if os.IsNotExist(err) {
			return true, nil
		}
		return false, err
	}
	defer f.Close()

	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == ".ws-context/" {
			return false, nil
		}
	}
	if err := s.Err(); err != nil {
		return false, err
	}
	return true, nil
}
