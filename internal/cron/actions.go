package cron

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mugenkunou/ws-tool/internal/plan"
)

// Note: The old wrapper-script approach (GenerateScript + write to cron-jobs/*.sh)
// has been replaced. Each crontab entry now calls `ws cron run <job>` directly,
// so RunJob handles state/logging. ScriptPath and GenerateScript are kept for
// reference; the legacy script files can be cleaned up by removing the cron-jobs dir.

// WSBinary resolves the running ws binary (symlinks followed) for embedding
// in wrapper scripts.
func WSBinary() string {
	bin, err := os.Executable()
	if err != nil {
		return "ws"
	}
	if resolved, err := filepath.EvalSymlinks(bin); err == nil {
		return resolved
	}
	return bin
}

// DefaultDisplay is the X11 DISPLAY embedded for jobs that need one:
// $DISPLAY, else ":1".
func DefaultDisplay() string {
	if d := os.Getenv("DISPLAY"); d != "" {
		return d
	}
	return ":1"
}

// AddActions builds the `cron add` plan actions: install the crontab entries
// for each job. Each crontab entry calls `ws cron run <job>` directly — no
// separate wrapper script is needed.
func AddActions(jobs []*BuiltinJob, wsBin, display, workspacePath string) ([]plan.Action, error) {
	var actions []plan.Action
	for _, j := range jobs {
		actions = append(actions,
			plan.Action{
				ID:          "cron-install-" + j.Name,
				Description: fmt.Sprintf("Install crontab entries for %s (%s + @reboot)", j.Name, j.Schedule),
				Execute: func() error {
					return AddJob(j, wsBin)
				},
			})
	}
	return actions, nil
}

// RemoveActions builds the `cron rm` plan actions: per job, remove its
// crontab entries and wrapper script.
func RemoveActions(jobs []*BuiltinJob) []plan.Action {
	var actions []plan.Action
	for _, j := range jobs {
		actions = append(actions, plan.Action{
			ID:          "cron-rm-" + j.Name,
			Description: fmt.Sprintf("Remove crontab entries and wrapper script for %s", j.Name),
			Execute: func() error {
				if err := RemoveJob(j.Name); err != nil {
					return err
				}
				if scriptPath, err := ScriptPath(j.Name); err == nil {
					_ = os.Remove(scriptPath) // best-effort; may already be absent
				}
				return nil
			},
		})
	}
	return actions
}
