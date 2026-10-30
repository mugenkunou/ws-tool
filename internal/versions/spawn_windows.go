//go:build windows

package versions

import "os/exec"

func startDetached(exe string) error {
	cmd := exec.Command(exe, "version", "--refresh-cache")
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
