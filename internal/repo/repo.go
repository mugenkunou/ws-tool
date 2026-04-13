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
)

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
}

type ReconcileSummary struct {
	Scanned int `json:"scanned"`
	Found   int `json:"found"`
}

type TrackedRepo struct {
	Path   string `json:"path"`
	Branch string `json:"branch"`
	Remote string `json:"remote"`
}

type FilterOptions struct {
	Path     string
	Dirty    bool
	Ahead    bool
	Behind   bool
	Detached bool
}

func Discover(workspacePath string, roots []string, excludeDirs []string) ([]Repository, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return nil, errors.New("git is required but was not found in PATH")
	}

	if len(roots) == 0 {
		roots = []string{"."}
	}

	excludeSet := make(map[string]struct{}, len(excludeDirs))
	for _, d := range excludeDirs {
		excludeSet[filepath.ToSlash(filepath.Clean(d))] = struct{}{}
	}

	seen := make(map[string]struct{})
	repos := make([]Repository, 0)

	for _, root := range roots {
		absRoot := root
		if !filepath.IsAbs(absRoot) {
			absRoot = filepath.Join(workspacePath, root)
		}
		absRoot = filepath.Clean(absRoot)

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

			repoPath := filepath.Dir(path)
			rel, relErr = filepath.Rel(workspacePath, repoPath)
			if relErr != nil {
				rel = filepath.ToSlash(repoPath)
			} else {
				rel = filepath.ToSlash(rel)
			}
			if strings.HasPrefix(rel, "../") {
				return filepath.SkipDir
			}
			if _, ok := seen[rel]; ok {
				return filepath.SkipDir
			}
			seen[rel] = struct{}{}
			repos = append(repos, Repository{Path: rel})
			return filepath.SkipDir
		})
		if err != nil {
			return nil, err
		}
	}

	sort.Slice(repos, func(i, j int) bool { return repos[i].Path < repos[j].Path })
	return repos, nil
}

func Scan(workspacePath string, repos []Repository) []RepoStatus {
	statuses := make([]RepoStatus, 0, len(repos))
	for _, r := range repos {
		absPath := filepath.Join(workspacePath, filepath.FromSlash(r.Path))
		statuses = append(statuses, scanOne(absPath, r.Path))
	}
	return statuses
}

func FetchAll(workspacePath string, repos []Repository) []FetchResult {
	results := make([]FetchResult, 0, len(repos))
	for _, r := range repos {
		absPath := filepath.Join(workspacePath, filepath.FromSlash(r.Path))
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
		absPath := filepath.Join(workspacePath, filepath.FromSlash(r.Path))
		out, err := runGit(absPath, args...)
		if err != nil {
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
	absPath := filepath.Join(workspacePath, filepath.FromSlash(plan.Path))
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

func RunAll(workspacePath string, repos []Repository, command []string) []OperationResult {
	results := make([]OperationResult, 0, len(repos))
	if len(command) == 0 {
		return results
	}

	for _, r := range repos {
		absPath := filepath.Join(workspacePath, filepath.FromSlash(r.Path))
		cmd := exec.Command(command[0], command[1:]...)
		cmd.Dir = absPath
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err := cmd.Run()
		if err != nil {
			errText := strings.TrimSpace(stderr.String())
			if errText == "" {
				errText = err.Error()
			}
			results = append(results, OperationResult{Path: r.Path, Success: false, Error: errText})
			continue
		}
		results = append(results, OperationResult{Path: r.Path, Success: true, Output: strings.TrimSpace(stdout.String())})
	}

	return results
}

func Reconcile(workspacePath string, roots []string, excludeDirs []string, tracked []TrackedRepo) (ReconcileSummary, []Repository, error) {
	discovered, err := Discover(workspacePath, roots, excludeDirs)
	if err != nil {
		return ReconcileSummary{}, nil, err
	}

	seen := make(map[string]struct{}, len(discovered))
	for _, r := range discovered {
		seen[r.Path] = struct{}{}
	}

	// Merge tracked repos that weren't discovered (may have been removed or are outside roots).
	for _, tr := range tracked {
		if _, exists := seen[tr.Path]; exists {
			continue
		}
		// Check if the tracked path actually exists on disk.
		absPath := filepath.Join(workspacePath, filepath.FromSlash(tr.Path))
		gitDir := filepath.Join(absPath, ".git")
		if info, err := os.Stat(gitDir); err == nil && info.IsDir() {
			discovered = append(discovered, Repository{Path: tr.Path})
			seen[tr.Path] = struct{}{}
		}
	}

	sort.Slice(discovered, func(i, j int) bool { return discovered[i].Path < discovered[j].Path })

	return ReconcileSummary{
		Scanned: len(discovered),
		Found:   len(discovered),
	}, discovered, nil
}

func FetchOne(workspacePath string, r Repository) FetchResult {
	absPath := filepath.Join(workspacePath, filepath.FromSlash(r.Path))
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
		if opts.Path != "" {
			rel := filepath.ToSlash(s.Path)
			if !strings.HasPrefix(rel, opts.Path) && !strings.HasPrefix(rel, opts.Path+"/") && rel != opts.Path {
				continue
			}
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
	cmd := exec.Command("git", append([]string{"-C", repoPath}, args...)...)
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
