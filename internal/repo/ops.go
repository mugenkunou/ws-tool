package repo

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Skip is a repo a write command leaves out, and why.
type Skip struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// Strategy is how a pull brings a repo up to date.
type Strategy string

const (
	StrategyFF     Strategy = "fast-forward"
	StrategyRebase Strategy = "rebase"
	StrategyMerge  Strategy = "merge"
	// StrategySkip is the answer "skip this repo" for a diverged repo.
	StrategySkip Strategy = "skip"
)

// ReasonDiverged is why a diverged repo is skipped when nobody chose a strategy.
const ReasonDiverged = "diverged: choose --rebase or --merge"

// Chooser picks the strategy for a diverged repo (local commits not upstream,
// remote commits not local). Returning StrategySkip skips the repo, and the
// reason says why.
type Chooser func(path string, local, remote int) (strategy Strategy, reason string)

// Diverged reports whether the repo and its upstream have both moved.
func (st RepoStatus) Diverged() bool { return st.Ahead > 0 && st.Behind > 0 }

// pullBlocked is the reason a repo can neither pull nor save, or "".
func pullBlocked(st RepoStatus) string {
	switch {
	case st.Error != "":
		return "error: " + st.Error
	case st.Detached:
		return "detached HEAD"
	case !st.HasUpstream:
		return "no upstream (ws repo configure sets one)"
	}
	return ""
}

// PullItem is one repo that pull will bring up to date.
type PullItem struct {
	Path     string
	Behind   int
	Strategy Strategy // StrategyFF, StrategyRebase or StrategyMerge
}

// Describe is the plan line; short is the repo path as the caller shows it.
func (p PullItem) Describe(short string) string {
	if p.Strategy == StrategyFF {
		return fmt.Sprintf("Pull %s (%d behind, fast-forward)", short, p.Behind)
	}
	return fmt.Sprintf("Pull %s (diverged, %s)", short, p.Strategy)
}

// PlanPull decides what pull does for a repo read after a fetch. Neither
// result is set when the repo is already up to date.
func PlanPull(st RepoStatus, choose Chooser) (*PullItem, *Skip) {
	if reason := pullBlocked(st); reason != "" {
		return nil, &Skip{Path: st.Path, Reason: reason}
	}
	if st.Behind == 0 {
		return nil, nil
	}
	item := &PullItem{Path: st.Path, Behind: st.Behind, Strategy: StrategyFF}
	if st.Diverged() {
		s, reason := choose(st.Path, st.Ahead, st.Behind)
		if s == StrategySkip {
			return nil, &Skip{Path: st.Path, Reason: reason}
		}
		item.Strategy = s
	}
	return item, nil
}

// PushSkip is the reason a repo is not pushed, or "" when it is ahead and can
// be pushed.
func PushSkip(st RepoStatus) string {
	switch {
	case st.Error != "":
		return "error: " + st.Error
	case st.Detached:
		return "detached HEAD"
	case !st.HasUpstream:
		return "no upstream (ws repo configure sets one)"
	case st.Behind > 0:
		return "behind or diverged: pull first (or ws repo save)"
	case st.Ahead == 0:
		return "nothing to push"
	}
	return ""
}

// SaveItem is the plan of `ws repo save` for one repo: its steps, in order.
type SaveItem struct {
	Path     string
	Changes  int      // uncommitted changes to commit; 0 means no commit step
	Pull     bool     // the upstream has moved
	Strategy Strategy // how the pull goes
	Push     bool     // the repo is ahead, or will be after the commit
}

// Describe is the plan line; short is the repo path as the caller shows it.
func (s SaveItem) Describe(short string) string {
	var steps []string
	if s.Changes > 0 {
		steps = append(steps, "commit "+changesLabel(s.Changes))
	}
	if s.Pull {
		steps = append(steps, "pull ("+string(s.Strategy)+")")
	}
	if s.Push {
		steps = append(steps, "push")
	}
	return fmt.Sprintf("Save %s: %s", short, strings.Join(steps, ", "))
}

func changesLabel(n int) string {
	if n == 1 {
		return "1 change"
	}
	return fmt.Sprintf("%d changes", n)
}

// PlanSave decides the steps of save for a repo read after a fetch; changes is
// the number of uncommitted changes. Neither result is set when there is
// nothing to commit, pull or push.
func PlanSave(st RepoStatus, changes int, choose Chooser) (*SaveItem, *Skip) {
	if reason := pullBlocked(st); reason != "" {
		return nil, &Skip{Path: st.Path, Reason: reason}
	}
	item := &SaveItem{Path: st.Path, Changes: changes, Pull: st.Behind > 0, Strategy: StrategyFF}
	item.Push = st.Ahead > 0 || changes > 0
	if item.Pull && (st.Ahead > 0 || changes > 0) {
		// Committing makes the repo diverged even when it was only behind.
		local := st.Ahead
		if changes > 0 {
			local++
		}
		s, reason := choose(st.Path, local, st.Behind)
		if s == StrategySkip {
			return nil, &Skip{Path: st.Path, Reason: reason}
		}
		item.Strategy = s
	}
	if item.Changes == 0 && !item.Pull && !item.Push {
		return nil, nil
	}
	return item, nil
}

// DefaultSaveMessage is the commit message of save without -m.
func DefaultSaveMessage(now time.Time, host string) string {
	return fmt.Sprintf("ws save %s (%s)", now.Format("2006-01-02 15:04"), host)
}

// UncommittedCount is the number of changed paths in the working tree,
// untracked files included.
func UncommittedCount(repoPath string) int {
	out, err := runGit(repoPath, "status", "--porcelain")
	if err != nil {
		return 0
	}
	n := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n
}

// LastFetch is when the repo was last fetched, from .git/FETCH_HEAD.
func LastFetch(repoPath string) (time.Time, bool) {
	fi, err := os.Stat(filepath.Join(repoPath, ".git", "FETCH_HEAD"))
	if err != nil {
		return time.Time{}, false
	}
	return fi.ModTime(), true
}

// Pull brings the repo up to date. A failed rebase or merge is aborted, so no
// repo is left half-way.
func Pull(repoPath string, s Strategy) error {
	var args []string
	switch s {
	case StrategyRebase:
		args = []string{"pull", "--rebase"}
	case StrategyMerge:
		args = []string{"pull", "--no-rebase", "--no-edit"}
	default:
		args = []string{"pull", "--ff-only"}
	}
	if _, err := runGit(repoPath, args...); err != nil {
		switch s {
		case StrategyRebase:
			runGit(repoPath, "rebase", "--abort")
		case StrategyMerge:
			runGit(repoPath, "merge", "--abort")
		}
		return err
	}
	return nil
}

// Push pushes the current branch to its upstream. It never forces.
func Push(repoPath string) error {
	_, err := runGit(repoPath, "push")
	return err
}

// Save runs the steps of item, in order. The commit stays when a later step
// fails, and the error names the step.
func Save(item SaveItem, message string) error {
	if item.Changes > 0 {
		if _, err := runGit(item.Path, "add", "-A"); err != nil {
			return fmt.Errorf("commit failed: %w", err)
		}
		if _, err := runGit(item.Path, "commit", "-m", message); err != nil {
			return fmt.Errorf("commit failed: %w", err)
		}
	}
	if item.Pull {
		if err := Pull(item.Path, item.Strategy); err != nil {
			return fmt.Errorf("pull failed: %w", err)
		}
	}
	if item.Push {
		if err := Push(item.Path); err != nil {
			return fmt.Errorf("push failed: %w", err)
		}
	}
	return nil
}
