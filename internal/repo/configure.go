package repo

import (
	"fmt"
	"strings"
)

// Facts are what `ws repo configure` needs to know about a repo to decide its
// questions.
type Facts struct {
	Branch          string
	Remotes         []string
	OriginHasBranch bool // refs/remotes/origin/<branch> exists
	LocalName       string
	GlobalName      string
	LocalEmail      string
	GlobalEmail     string
}

// ReadFacts reads the git settings of a repo.
func ReadFacts(repoPath string) Facts {
	f := Facts{
		LocalName:   gitConfigGet(repoPath, "--local", "user.name"),
		GlobalName:  gitConfigGet(repoPath, "--global", "user.name"),
		LocalEmail:  gitConfigGet(repoPath, "--local", "user.email"),
		GlobalEmail: gitConfigGet(repoPath, "--global", "user.email"),
	}
	if out, err := runGit(repoPath, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		f.Branch = strings.TrimSpace(out)
	}
	if out, err := runGit(repoPath, "remote"); err == nil {
		f.Remotes = strings.Fields(out)
	}
	if f.Branch != "" {
		_, err := runGit(repoPath, "rev-parse", "--verify", "--quiet", "refs/remotes/origin/"+f.Branch)
		f.OriginHasBranch = err == nil
	}
	return f
}

// HasRemote reports whether the repo has a remote called name.
func (f Facts) HasRemote(name string) bool {
	for _, r := range f.Remotes {
		if r == name {
			return true
		}
	}
	return false
}

// SetConfig sets a git config value, globally or in the repo.
func SetConfig(repoPath string, global bool, key, value string) error {
	scope := "--local"
	if global {
		scope = "--global"
	}
	if _, err := runGit(repoPath, "config", scope, key, value); err != nil {
		return fmt.Errorf("git config %s %s: %w", scope, key, err)
	}
	return nil
}

// TrackOrigin makes the current branch track origin/<branch>.
func TrackOrigin(repoPath, branch string) error {
	_, err := runGit(repoPath, "branch", "--set-upstream-to=origin/"+branch, branch)
	return err
}

// PushUpstream pushes the branch to origin and tracks it.
func PushUpstream(repoPath, branch string) error {
	_, err := runGit(repoPath, "push", "-u", "origin", branch)
	return err
}

// AddOrigin adds a remote called origin.
func AddOrigin(repoPath, url string) error {
	_, err := runGit(repoPath, "remote", "add", "origin", url)
	return err
}
