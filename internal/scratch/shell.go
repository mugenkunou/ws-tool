package scratch

import (
	"os"
	"os/exec"
)

// ShellEnvVar is set in the environment of a scratch shell to the directory it
// was started in. A prompt can show it, and `ws scratch new|open` uses it to
// notice that it is being run from inside a scratch shell. It is not
// WS_SCRATCH, which `ws env` exports as the scratch root.
const ShellEnvVar = "WS_SCRATCH_SHELL"

// ShellCommand returns the command that starts the user's shell in dir: $SHELL,
// else bash, else sh. The shell inherits the environment, plus ShellEnvVar. The
// caller attaches the terminal and runs it.
func ShellCommand(dir string) *exec.Cmd {
	return shellCommand(dir, os.Getenv("SHELL"), exec.LookPath)
}

// shellCommand is ShellCommand with $SHELL and the path lookup injected, for
// tests.
func shellCommand(dir, shell string, lookPath func(string) (string, error)) *exec.Cmd {
	if shell == "" {
		shell = "sh"
		if _, err := lookPath("bash"); err == nil {
			shell = "bash"
		}
	}
	c := exec.Command(shell)
	c.Dir = dir
	c.Env = append(os.Environ(), ShellEnvVar+"="+dir)
	return c
}

// InShell reports the directory of the scratch shell this process runs in, or
// "" when it is not in one.
func InShell() string { return os.Getenv(ShellEnvVar) }
