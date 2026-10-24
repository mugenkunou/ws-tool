package repo

import (
	"bytes"
	"errors"
	"fmt"
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

func FetchAll(workspacePath string, repos []Repository) []FetchResult {
	results := make([]FetchResult, 0, len(repos))
	for _, r := range repos {
		absPath := absRepoPath(workspacePath, r.Path)
		_, err := runGit(absPath, "fetch", "--all", "--prune")
		if err != nil {
			results = append(results, FetchResult{Path: r.Path, Success: false, Error: err.Error()})
			continue
		}
		results = append(results, FetchResult{Path: r.Path, Success: true})
	}
	return results
}

func PullAll(workspacePath string, repos []Repository, rebase bool) []OperationResult {
	results := make([]OperationResult, 0, len(repos))
	args := []string{"pull", "--ff-only"}
	if rebase {
		args = []string{"pull", "--rebase"}
	}
	for _, r := range repos {
		absPath := absRepoPath(workspacePath, r.Path)
		out, err := runGit(absPath, args...)
		if err != nil {
			if rebase {
				runGit(absPath, "rebase", "--abort") // leave no repo half-way
			}
			results = append(results, OperationResult{Path: r.Path, Success: false, Error: err.Error()})
			continue
		}
		results = append(results, OperationResult{Path: r.Path, Success: true, Output: strings.TrimSpace(out)})
	}
	return results
}

// SyncStrategy describes the sync action type for a repo.
type SyncStrategy string

const (
	SyncPull       SyncStrategy = "pull"
	SyncPush       SyncStrategy = "push"
	SyncPullPush   SyncStrategy = "pull+push"
	SyncCommitPush SyncStrategy = "commit+push"
	SyncSkip       SyncStrategy = "skip"
)

// SyncPlan describes the planned sync action for a single repo.
type SyncPlan struct {
	Path     string       `json:"path"`
	Strategy SyncStrategy `json:"strategy"`
	Detail   string       `json:"detail"`
	Warning  string       `json:"warning,omitempty"`
	Status   RepoStatus   `json:"status"`
}

// SyncOptions controls sync behavior.
type SyncOptions struct {
	Rebase bool
}

// PlanSync inspects repo state and returns the planned sync action.
func PlanSync(status RepoStatus) SyncPlan {
	p := SyncPlan{Path: status.Path, Status: status}

	if status.Error != "" {
		p.Strategy = SyncSkip
		p.Warning = "error: " + status.Error
		return p
	}
	if status.Detached {
		p.Strategy = SyncSkip
		p.Warning = "detached HEAD"
		return p
	}

	if !status.HasUpstream {
		p.Strategy = SyncSkip
		p.Warning = "no upstream configured"
		return p
	}

	behind := status.Behind > 0
	ahead := status.Ahead > 0

	switch {
	case behind && ahead:
		p.Strategy = SyncPullPush
		p.Detail = fmt.Sprintf("%d ahead, %d behind", status.Ahead, status.Behind)
		if status.Dirty {
			p.Detail += ", dirty — will commit before pull"
		}
	case behind:
		p.Strategy = SyncPull
		p.Detail = fmt.Sprintf("%d behind", status.Behind)
		if status.Dirty {
			p.Detail += ", dirty — will commit before pull"
		}
	case ahead:
		if status.Dirty {
			p.Strategy = SyncCommitPush
			p.Detail = fmt.Sprintf("%d ahead, dirty — will commit and push", status.Ahead)
		} else {
			p.Strategy = SyncPush
			p.Detail = fmt.Sprintf("%d ahead", status.Ahead)
		}
	default:
		if status.Dirty {
			p.Strategy = SyncCommitPush
			p.Detail = "dirty — will commit and push"
		} else {
			// Fully in sync.
			p.Strategy = SyncSkip
		}
		return p
	}

	return p
}

// SyncOne executes the sync plan for a single repo.
func SyncOne(workspacePath string, plan SyncPlan, opts SyncOptions) OperationResult {
	absPath := absRepoPath(workspacePath, plan.Path)
	result := OperationResult{Path: plan.Path}

	dirty := plan.Status.Dirty

	// Auto-commit dirty changes before pull or push.
	if dirty {
		if _, err := runGit(absPath, "add", "-A"); err != nil {
			result.Success = false
			result.Error = "git add failed: " + err.Error()
			return result
		}
		if _, err := runGit(absPath, "commit", "-m", "ws: auto-sync commit"); err != nil {
			result.Success = false
			result.Error = "auto-commit failed: " + err.Error()
			return result
		}
	}

	// Pull phase
	if plan.Strategy == SyncPull || plan.Strategy == SyncPullPush {
		// If we auto-committed dirty changes, the repo is now diverged
		// even if it was only behind before, so we can't use --ff-only.
		diverged := plan.Strategy == SyncPullPush || dirty
		var pullArgs []string
		if diverged {
			if opts.Rebase {
				pullArgs = []string{"pull", "--rebase"}
			} else {
				pullArgs = []string{"pull", "--no-rebase"}
			}
		} else {
			pullArgs = []string{"pull", "--ff-only"}
		}

		out, err := runGit(absPath, pullArgs...)
		if err != nil {
			// Abort any in-progress merge/rebase to leave repo clean
			if diverged {
				if opts.Rebase {
					runGit(absPath, "rebase", "--abort")
				} else {
					runGit(absPath, "merge", "--abort")
				}
			}
			result.Success = false
			result.Error = "pull failed: " + err.Error()
			return result
		}
		result.Output = strings.TrimSpace(out)
	}

	// Push phase — also push after pull if we auto-committed dirty changes.
	needsPush := plan.Strategy == SyncPush || plan.Strategy == SyncPullPush ||
		plan.Strategy == SyncCommitPush || (plan.Strategy == SyncPull && dirty)
	if needsPush {
		out, err := runGit(absPath, "push")
		if err != nil {
			result.Success = false
			if result.Output != "" {
				result.Error = "push failed after pull: " + err.Error()
			} else {
				result.Error = "push failed: " + err.Error()
			}
			return result
		}
		pushOut := strings.TrimSpace(out)
		if pushOut != "" {
			if result.Output != "" {
				result.Output += "; " + pushOut
			} else {
				result.Output = pushOut
			}
		}
	}

	result.Success = true
	return result
}

// RunAll runs command in each repo with the repo root as its working
// directory. Stdout and stderr are captured together, and stdin is not
// connected. A command that exits non-zero fails; its output is kept.
func RunAll(workspacePath string, repos []Repository, command []string) []OperationResult {
	results := make([]OperationResult, 0, len(repos))
	if len(command) == 0 {
		return results
	}
	for _, r := range repos {
		results = append(results, RunOne(workspacePath, r, command))
	}
	return results
}

// RunOne runs command in one repo; see RunAll.
func RunOne(workspacePath string, r Repository, command []string) OperationResult {
	cmd := exec.Command(command[0], command[1:]...)
	cmd.Dir = absRepoPath(workspacePath, r.Path)
	cmd.Env = gitEnv()
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	res := OperationResult{Path: r.Path}
	err := cmd.Run()
	res.Output = strings.TrimSpace(buf.String())
	if err == nil {
		res.Success = true
		return res
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		res.ExitCode = exitErr.ExitCode()
		res.Error = fmt.Sprintf("exit %d", res.ExitCode)
	} else {
		res.ExitCode = -1
		res.Error = err.Error()
	}
	return res
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
