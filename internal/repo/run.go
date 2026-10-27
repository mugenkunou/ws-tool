package repo

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"
)

// runEnv is the environment of a command run in a repo with no terminal
// (`ws repo git`, `ws repo run` across repos, the TUI's output view): on top
// of gitEnv, an editor git would open accepts what is there, so a merge or
// rebase never waits, and `git commit` without -m aborts on the empty message.
func runEnv() []string { return append(gitEnv(), "GIT_EDITOR=true") }

// RunOne runs command in one repo with its output captured, stdout and stderr
// together. Stdin is not connected.
func RunOne(workspacePath string, r Repository, command []string) OperationResult {
	cmd := exec.Command(command[0], command[1:]...)
	cmd.Dir = absRepoPath(workspacePath, r.Path)
	cmd.Env = runEnv()
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	res := result(r.Path, cmd.Run())
	res.Output = strings.TrimSpace(buf.String())
	return res
}

// RunAttached runs command in one repo on the caller's terminal: stdin,
// stdout and stderr pass straight through and the environment is the user's
// own, so git can page, open an editor, or ask for credentials.
func RunAttached(workspacePath string, r Repository, command []string, stdin io.Reader, stdout, stderr io.Writer) OperationResult {
	cmd := exec.Command(command[0], command[1:]...)
	cmd.Dir = absRepoPath(workspacePath, r.Path)
	cmd.Env = os.Environ()
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, stdout, stderr
	return result(r.Path, cmd.Run())
}

// result reads a finished command's error: exit 0 succeeds, a non-zero exit
// keeps its code, and a command that did not start is -1.
func result(path string, err error) OperationResult {
	res := OperationResult{Path: path}
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

// CompletionWords are the names a git command line in these repos may take:
// local and remote branches, tags and remotes, sorted, each once.
func CompletionWords(paths []string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(text string) {
		for _, w := range strings.Fields(text) {
			if !seen[w] {
				seen[w] = true
				out = append(out, w)
			}
		}
	}
	for _, p := range paths {
		if refs, err := runGit(p, "for-each-ref", "--format=%(refname:short)", "refs/heads", "refs/remotes", "refs/tags"); err == nil {
			add(refs)
		}
		if remotes, err := runGit(p, "remote"); err == nil {
			add(remotes)
		}
	}
	slices.Sort(out)
	return out
}
