package cron

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mugenkunou/ws-tool/internal/manifest"
	"github.com/mugenkunou/ws-tool/internal/plan"
	"github.com/mugenkunou/ws-tool/internal/provision"
)

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

// AddActions builds the `cron add` plan actions: per job, write its wrapper
// script, then install its crontab entries and record the provision.
func AddActions(jobs []*BuiltinJob, wsBin, display, workspacePath, manifestPath string) ([]plan.Action, error) {
	statePath, err := StateFilePath()
	if err != nil {
		return nil, err
	}
	logPath, err := LogFilePath()
	if err != nil {
		return nil, err
	}
	var actions []plan.Action
	for _, j := range jobs {
		scriptPath, err := ScriptPath(j.Name)
		if err != nil {
			return nil, err
		}
		content := GenerateScript(j, wsBin, display, workspacePath, statePath, logPath)
		actions = append(actions,
			plan.Action{
				ID:          "cron-write-script-" + j.Name,
				Description: fmt.Sprintf("Write wrapper script %s", scriptPath),
				Execute: func() error {
					if err := os.MkdirAll(filepath.Dir(scriptPath), 0o755); err != nil {
						return err
					}
					return os.WriteFile(scriptPath, []byte(content), 0o755)
				},
			},
			plan.Action{
				ID:          "cron-install-" + j.Name,
				Description: fmt.Sprintf("Install crontab entries for %s (%s + @reboot)", j.Name, j.Schedule),
				Execute: func() error {
					if err := AddJob(j, scriptPath); err != nil {
						return err
					}
					return manifest.RecordProvision(manifestPath, provision.Entry{
						Type:    provision.TypeCronJob,
						Path:    scriptPath,
						Line:    j.Name,
						Command: "cron add",
					})
				},
			})
	}
	return actions, nil
}

// RemoveActions builds the `cron rm` plan actions: per job, remove its
// crontab entries and wrapper script, and drop the provision.
func RemoveActions(jobs []*BuiltinJob, manifestPath string) []plan.Action {
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
				return manifest.RemoveCronJobProvision(manifestPath, j.Name)
			},
		})
	}
	return actions
}
