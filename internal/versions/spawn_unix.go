//go:build !windows

package versions

import (
	"os/exec"
	"syscall"
)

// startDetached runs `exe version --refresh-cache` in its own session with
// stdio on /dev/null and does not wait for it.
func startDetached(exe string) error {
	cmd := exec.Command(exe, "version", "--refresh-cache")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
