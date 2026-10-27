package repo

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

// workspaceRel resolves a path to workspace-relative form. Paths may be
// workspace-relative, absolute, or ~-prefixed.
func workspaceRel(workspacePath, value string) (rel string, ok bool) {
	var abs string
	var err error
	if value == "" {
		return "", false
	}
	// ~ or absolute → expand; otherwise join with workspace.
	if len(value) > 0 && value[0] == '~' {
		abs, err = wsconfig.ExpandUserPath(value)
		if err != nil {
			return "", false
		}
	} else if filepath.IsAbs(value) {
		abs = filepath.Clean(value)
	} else {
		abs = filepath.Join(filepath.Clean(workspacePath), filepath.FromSlash(value))
	}
	r, err := filepath.Rel(filepath.Clean(workspacePath), abs)
	if err != nil {
		return "", false
	}
	r = filepath.ToSlash(r)
	if r == ".." || len(r) > 2 && r[:3] == "../" {
		return "", false
	}
	return r, true
}

type Repository struct {
	Path string `json:"path"`
}

type RepoStatus struct {
	Path        string `json:"path"`
	Branch      string `json:"branch"`
	Detached    bool   `json:"detached"`
	Dirty       bool   `json:"dirty"`
	Ahead       int    `json:"ahead"`
	Behind      int    `json:"behind"`
	HasUpstream bool   `json:"has_upstream"`
	Error       string `json:"error,omitempty"`
}

type FetchResult struct {
	Path    string `json:"path"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

type OperationResult struct {
	Path    string `json:"path"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
	Output  string `json:"output,omitempty"`
	// ExitCode is the exit code of a command run in the repo: 0 on success,
	// -1 when the command did not start.
	ExitCode int `json:"exit_code"`
}

type FilterOptions struct {
	// Path is an absolute directory; only repos at or under it (whole path
	// components) are kept.
	Path     string
	Dirty    bool
	Ahead    bool
	Behind   bool
	Detached bool
}

// Discover walks roots (absolute, or relative to workspacePath) and returns every
// git repository found inside the workspace. Repository.Path is always a cleaned
// absolute path. excludeDirs follow the config path convention.
func Discover(workspacePath string, roots []string, excludeDirs []string) ([]Repository, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return nil, errors.New("git is required but was not found in PATH")
	}

	if len(roots) == 0 {
		roots = []string{"."}
	}

	// Config values may be workspace-relative, absolute, or ~-prefixed; match
	// on the workspace-relative form.
	excludeSet := make(map[string]struct{}, len(excludeDirs))
	for _, d := range excludeDirs {
		if rel, ok := workspaceRel(workspacePath, d); ok && rel != "." {
			excludeSet[rel] = struct{}{}
		}
	}

	seen := make(map[string]struct{})
	repos := make([]Repository, 0)

	for _, root := range roots {
		absRoot := root
		if !filepath.IsAbs(absRoot) {
			absRoot = filepath.Join(workspacePath, root)
		}
		absRoot = filepath.Clean(absRoot)
		if !IsWithin(absRoot, workspacePath) {
			continue // repos outside the workspace are never listed
		}

		info, err := os.Stat(absRoot)
		if err != nil || !info.IsDir() {
			continue
		}

		err = filepath.WalkDir(absRoot, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if !d.IsDir() {
				return nil
			}

			// Check if this directory should be excluded (before looking for .git).
			rel, relErr := filepath.Rel(workspacePath, path)
			if relErr == nil {
				relSlash := filepath.ToSlash(rel)
				for excl := range excludeSet {
					if relSlash == excl || strings.HasPrefix(relSlash, excl+"/") {
						return filepath.SkipDir
					}
				}
			}

			if d.Name() != ".git" {
				return nil
			}

			repoPath := filepath.Clean(filepath.Dir(path))
			rel, relErr = filepath.Rel(workspacePath, repoPath)
			if relErr != nil || rel == ".." || strings.HasPrefix(filepath.ToSlash(rel), "../") {
				return filepath.SkipDir
			}
			if _, ok := seen[repoPath]; ok {
				return filepath.SkipDir
			}
			seen[repoPath] = struct{}{}
			repos = append(repos, Repository{Path: repoPath})
			return filepath.SkipDir
		})
		if err != nil {
			return nil, err
		}
	}

	sort.Slice(repos, func(i, j int) bool { return repos[i].Path < repos[j].Path })
	return repos, nil
}

// IsWithin reports whether path is dir itself or lies under it, comparing whole
// path components (so "/ws/Data" is not within "/ws/Da"). Both must be absolute.
func IsWithin(path, dir string) bool {
	rel, err := filepath.Rel(filepath.Clean(dir), filepath.Clean(path))
	if err != nil {
		return false
	}
	rel = filepath.ToSlash(rel)
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, "../"))
}

// absRepoPath returns the absolute path for a repo. If path is already
// absolute it is returned as-is; otherwise it is joined with workspacePath.
func absRepoPath(workspacePath, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(workspacePath, filepath.FromSlash(path))
}

func Scan(workspacePath string, repos []Repository) []RepoStatus {
	statuses := make([]RepoStatus, 0, len(repos))
	for _, r := range repos {
		absPath := absRepoPath(workspacePath, r.Path)
		statuses = append(statuses, scanOne(absPath, r.Path))
	}
	return statuses
}

func FetchOne(workspacePath string, r Repository) FetchResult {
	absPath := absRepoPath(workspacePath, r.Path)
	_, err := runGit(absPath, "fetch", "--all", "--prune")
	if err != nil {
		return FetchResult{Path: r.Path, Success: false, Error: err.Error()}
	}
	return FetchResult{Path: r.Path, Success: true}
}

func Filter(statuses []RepoStatus, opts FilterOptions) []RepoStatus {
	if opts.Path == "" && !opts.Dirty && !opts.Ahead && !opts.Behind && !opts.Detached {
		return statuses
	}
	filtered := make([]RepoStatus, 0)
	for _, s := range statuses {
		if opts.Path != "" && !IsWithin(s.Path, opts.Path) {
			continue
		}
		if opts.Dirty && !s.Dirty {
			continue
		}
		if opts.Ahead && s.Ahead <= 0 {
			continue
		}
		if opts.Behind && s.Behind <= 0 {
			continue
		}
		if opts.Detached && !s.Detached {
			continue
		}
		filtered = append(filtered, s)
	}
	return filtered
}

func scanOne(absPath, relPath string) RepoStatus {
	status := RepoStatus{Path: relPath, Branch: "unknown"}

	branch, err := runGit(absPath, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		status.Error = err.Error()
		return status
	}
	status.Branch = strings.TrimSpace(branch)
	if status.Branch == "HEAD" {
		status.Detached = true
	}

	porcelain, err := runGit(absPath, "status", "--porcelain")
	if err != nil {
		status.Error = err.Error()
		return status
	}
	status.Dirty = strings.TrimSpace(porcelain) != ""

	upstream, err := runGit(absPath, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}")
	if err == nil && strings.TrimSpace(upstream) != "" {
		status.HasUpstream = true
		counts, err := runGit(absPath, "rev-list", "--left-right", "--count", "HEAD...@{u}")
		if err == nil {
			parts := strings.Fields(strings.TrimSpace(counts))
			if len(parts) == 2 {
				if a, e := strconv.Atoi(parts[0]); e == nil {
					status.Ahead = a
				}
				if b, e := strconv.Atoi(parts[1]); e == nil {
					status.Behind = b
				}
			}
		}
	}

	return status
}

func runGit(repoPath string, args ...string) (string, error) {
	cmd := gitCmd(repoPath, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		errText := strings.TrimSpace(stderr.String())
		if errText == "" {
			errText = err.Error()
		}
		return "", errors.New(errText)
	}
	return stdout.String(), nil
}
