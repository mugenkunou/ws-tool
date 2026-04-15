//go:build !linux

package notify

import (
	"fmt"
	"runtime"
)

// RunDaemon is not supported on this platform.
func RunDaemon(opts DaemonOptions) error {
	return fmt.Errorf("notify daemon is not supported on %s (requires Linux inotify)", runtime.GOOS)
}
