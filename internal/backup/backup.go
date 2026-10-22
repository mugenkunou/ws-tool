// Package backup holds the git operations behind `ws dotfile backup` and
// `ws pass backup`, which work exactly alike (spec §5 pass backup): the same
// verbs, the same status, the same private-remote rule. Only the repository
// differs, so the git plumbing and the visibility check live here once, and
// each noun supplies its repository path and its own settings.
package backup

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mugenkunou/ws-tool/internal/repo"
	"github.com/mugenkunou/ws-tool/internal/secret"
)

// DefaultBranch is the branch ws commits to and pushes unless settings say
// otherwise.
const DefaultBranch = "main"

// Commit is one line of the backup's history.
type Commit struct {
	Hash    string `json:"hash"`
	Subject string `json:"subject"`
}

// State is what `backup status` reports. It never contacts the remote: ahead
// and behind compare with the remote branch as of the last push or fetch.
type State struct {
	Initialized bool     `json:"initialized"`
	RepoPath    string   `json:"repo_path"`
	RemoteURL   string   `json:"remote_url"`
	Branch      string   `json:"branch"`
	Ahead       int      `json:"ahead"`
	Behind      int      `json:"behind"`
	Uncommitted []string `json:"uncommitted"` // git status --porcelain lines
	LastCommit  string   `json:"last_commit"` // "<date>  <subject>"
	Commits     []Commit `json:"commits"`
}

// Inspect reads the repository at path: remote, branch, ahead and behind,
// pending changes and the last n commits.
func Inspect(path, branch string, n int) State {
	st := State{RepoPath: path, Uncommitted: []string{}, Commits: []Commit{}}
	if !IsRepo(path) {
		return st
	}
	st.Initialized = true
	st.RemoteURL = RemoteURL(path)
	st.Branch = CurrentBranch(path)
	if branch == "" {
		branch = st.Branch
	}
	if st.RemoteURL != "" {
		st.Ahead, st.Behind = AheadBehind(path, branch)
	}
	if lines, err := Porcelain(path); err == nil {
		st.Uncommitted = lines
	}
	st.LastCommit = LastCommit(path)
	if commits, err := Log(path, n); err == nil {
		st.Commits = commits
	}
	return st
}

// IsRepo reports whether path is a git work tree root.
func IsRepo(path string) bool {
	info, err := os.Stat(filepath.Join(path, ".git"))
	return err == nil && info.IsDir()
}

// Init makes path a git repository on branch, with the commit identity
// `ws <ws@localhost>` set in the repository's local config (spec §5 dotfile
// backup). An existing repository is only moved to the branch.
func Init(path, branch string) error {
	if err := os.MkdirAll(path, 0o755); err != nil {
		return err
	}
	if !IsRepo(path) {
		if _, err := Git(path, "init"); err != nil {
			return err
		}
		if _, err := Git(path, "config", "user.email", "ws@localhost"); err != nil {
			return err
		}
		if _, err := Git(path, "config", "user.name", "ws"); err != nil {
			return err
		}
	}
	if branch != "" {
		if CurrentBranch(path) != branch {
			if _, err := Git(path, "checkout", "-B", branch); err != nil {
				return err
			}
		}
	}
	return nil
}

// RemoteURL returns the url of origin, or "".
func RemoteURL(path string) string {
	out, err := Git(path, "remote", "get-url", "origin")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// SetRemote adds origin, or points an existing origin at url.
func SetRemote(path, url string) error {
	verb := "add"
	if RemoteURL(path) != "" {
		verb = "set-url"
	}
	_, err := Git(path, "remote", verb, "origin", url)
	return err
}

// RemoveRemote deletes origin. Nothing happens when there is none.
func RemoveRemote(path string) error {
	if RemoteURL(path) == "" {
		return nil
	}
	_, err := Git(path, "remote", "remove", "origin")
	return err
}

// CurrentBranch returns the checked-out branch, or "".
func CurrentBranch(path string) string {
	// symbolic-ref also works before the first commit, when HEAD names a branch
	// that does not exist yet.
	if out, err := Git(path, "symbolic-ref", "--short", "-q", "HEAD"); err == nil {
		return strings.TrimSpace(out)
	}
	out, err := Git(path, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// AheadBehind compares HEAD with origin/<branch> as of the last push or fetch.
func AheadBehind(path, branch string) (ahead, behind int) {
	if branch == "" {
		branch = DefaultBranch
	}
	out, err := Git(path, "rev-list", "--left-right", "--count", "HEAD...origin/"+branch)
	if err != nil {
		return 0, 0
	}
	parts := strings.Fields(strings.TrimSpace(out))
	if len(parts) == 2 {
		fmt.Sscanf(parts[0], "%d", &ahead)
		fmt.Sscanf(parts[1], "%d", &behind)
	}
	return ahead, behind
}

// Porcelain returns the `git status --porcelain` lines (pending changes).
func Porcelain(path string) ([]string, error) {
	out, err := Git(path, "status", "--porcelain")
	if err != nil {
		return nil, err
	}
	var lines []string
	for _, l := range strings.Split(out, "\n") {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	return lines, nil
}

// LastCommit returns "<date>  <subject>" of HEAD, or "" with no commits.
func LastCommit(path string) string {
	out, err := Git(path, "log", "-1", "--format=%ci  %s")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// Log returns the last n commits, newest first.
func Log(path string, n int) ([]Commit, error) {
	out, err := Git(path, "log", fmt.Sprintf("-%d", n), "--format=%h%x09%s")
	if err != nil {
		return nil, err
	}
	var commits []Commit
	for _, l := range strings.Split(out, "\n") {
		hash, subject, ok := strings.Cut(l, "\t")
		if ok {
			commits = append(commits, Commit{Hash: hash, Subject: subject})
		}
	}
	return commits, nil
}

// CommitAll stages everything and commits it with message, when anything
// changed. It reports whether a commit was made.
func CommitAll(path, message string) (committed bool, err error) {
	if _, err := Git(path, "add", "-A"); err != nil {
		return false, fmt.Errorf("git add: %w", err)
	}
	lines, err := Porcelain(path)
	if err != nil {
		return false, err
	}
	if len(lines) == 0 {
		return false, nil
	}
	if _, err := Git(path, "commit", "-m", message); err != nil {
		return false, fmt.Errorf("git commit: %w", err)
	}
	return true, nil
}

// Push pushes branch to origin, setting it as the upstream. ws never forces,
// so a remote that has commits this repository lacks makes the push fail and
// the commit stays local.
func Push(path, branch string) error {
	if branch == "" {
		branch = CurrentBranch(path)
	}
	if branch == "" {
		branch = DefaultBranch
	}
	_, err := Git(path, "push", "-u", "origin", branch)
	return err
}

// CheckRemote asks the git host's API whether the repository is private, with
// the token from pass (passEntry, else git/<host>). The caller decides what to
// do with the result: VisibilityResult.Refusal says whether to stop, and
// Notice what to warn about.
func CheckRemote(remoteURL, passEntry string) repo.VisibilityResult {
	return repo.CheckRepoVisibility(remoteURL, secret.ResolveGitToken(remoteURL, passEntry))
}

// Git runs git in the repository at path and returns stdout. The error is
// git's own message.
func Git(path string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", path}, args...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		text := strings.TrimSpace(stderr.String())
		if text == "" {
			text = err.Error()
		}
		return "", errors.New(text)
	}
	return stdout.String(), nil
}
