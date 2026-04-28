package dotfile

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// NeedsSudo reports whether filesystem operations on path require elevated
// privileges. It returns true when the path itself (or, if it doesn't exist,
// its nearest existing ancestor) is owned by UID 0.
func NeedsSudo(path string) bool {
	check := filepath.Clean(path)
	for {
		info, err := os.Lstat(check)
		if err == nil {
			if st, ok := info.Sys().(*syscall.Stat_t); ok {
				return st.Uid == 0
			}
			return false
		}
		parent := filepath.Dir(check)
		if parent == check {
			return false // reached filesystem root without finding anything
		}
		check = parent
	}
}

// sudoRename moves src to dst using "sudo mv". This handles cross-device moves
// and root-owned directories transparently.
func sudoRename(src, dst string) error {
	return sudoRun("mv", "--", src, dst)
}

// sudoSymlink creates a symbolic link at linkPath pointing to target using
// "sudo ln -s". It does not use -f; the caller must ensure linkPath doesn't
// exist before calling.
func sudoSymlink(target, linkPath string) error {
	return sudoRun("ln", "-s", "--", target, linkPath)
}

// sudoRemoveAll removes path using "sudo rm -rf". Use only for paths that
// have been explicitly confirmed as managed dotfile system paths.
func sudoRemoveAll(path string) error {
	return sudoRun("rm", "-rf", "--", path)
}

// sudoMkdirAll creates directory path (and any parents) using "sudo mkdir -p".
func sudoMkdirAll(path string) error {
	return sudoRun("mkdir", "-p", "--", path)
}

// sudoRun executes an arbitrary command under sudo and returns a formatted
// error if it fails.
func sudoRun(name string, args ...string) error {
	cmdArgs := append([]string{name}, args...)
	cmd := exec.Command("sudo", cmdArgs...)
	cmd.Stdin = os.Stdin // allow the terminal to handle the sudo password prompt
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("sudo %s: %w", name, err)
	}
	return nil
}
