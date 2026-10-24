package repo

import (
	"os"
	"os/exec"
)

// gitEnv is the environment of every git command ws runs: git never asks for
// credentials, and never opens an editor for a merge message.
func gitEnv() []string {
	return append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_MERGE_AUTOEDIT=no")
}

// gitCmd builds a git command that runs in repoPath, with gitEnv.
func gitCmd(repoPath string, args ...string) *exec.Cmd {
	cmd := exec.Command("git", append([]string{"-C", repoPath}, args...)...)
	cmd.Env = gitEnv()
	return cmd
}
