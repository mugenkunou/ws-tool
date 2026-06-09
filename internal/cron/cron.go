// Package cron manages ws-scheduled cron jobs for workspace maintenance.
//
// Each built-in job is a named, self-contained bash wrapper script installed
// under ~/.local/share/ws-tool/cron-jobs/. A crontab block with start/end
// markers is written for each job so ws can reliably remove it later.
//
// Three shortcomings of vanilla crontab are addressed:
//   - Missed-run recovery: an @reboot entry checks elapsed time since the
//     last run; if more than half the job's interval has passed, the job
//     runs immediately (equivalent to systemd Persistent=true).
//   - Structured logging: all output is directed to a shared log file
//     (~/.local/share/ws-tool/cron.log) with timestamps and job-name prefixes.
//     The log is size-capped (default 5 MB) to prevent unbounded growth.
//   - Status inspection: ws cron status reads the state file and last log
//     lines — equivalent to systemctl --user status.
package cron

import (
	"fmt"
	"path/filepath"
	"sort"

	"github.com/mugenkunou/ws-tool/internal/config"
)

// BuiltinJob defines a ws-managed cron job.
type BuiltinJob struct {
	// Name is the job identifier used in commands and crontab markers.
	Name string
	// Schedule is the cron expression for the scheduled entry.
	Schedule string
	// IntervalSecs is the expected interval between runs in seconds.
	// Used by the @reboot entry to decide whether a missed run needs catching up.
	IntervalSecs int
	// Description is a one-line human-readable summary.
	Description string
	// scriptBody returns the job-specific shell commands embedded in the
	// wrapper script body. wsBin is the absolute ws binary path, display is
	// the X11 DISPLAY value (may be empty), workspace is the workspace root.
	scriptBody func(wsBin, display, workspace string) string
}

// Builtins is the registry of all built-in named cron jobs.
var Builtins = map[string]*BuiltinJob{
	"mega-sync":     megaSyncJob,
	"dotfile-sync":  dotfileSyncJob,
	"repo-sync":     repoSyncJob,
	"secret-scan":   secretScanJob,
	"ignore-scan":   ignoreScanJob,
	"log-prune":     logPruneJob,
	"scratch-prune": scratchPruneJob,
}

// Presets maps a composite name to the ordered list of job names it expands to.
var Presets = map[string][]string{
	"maintenance": {"ignore-scan", "log-prune", "scratch-prune"},
	"sync":        {"mega-sync", "dotfile-sync", "repo-sync"},
}

// AllNames returns all valid job names and preset names, sorted alphabetically.
func AllNames() []string {
	var names []string
	for k := range Builtins {
		names = append(names, k)
	}
	for k := range Presets {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// Resolve expands a name (single job or preset) to one or more BuiltinJobs.
// Returns an error if the name is not known.
func Resolve(name string) ([]*BuiltinJob, error) {
	if preset, ok := Presets[name]; ok {
		jobs := make([]*BuiltinJob, 0, len(preset))
		for _, n := range preset {
			j, ok := Builtins[n]
			if !ok {
				return nil, fmt.Errorf("internal: preset %q references unknown job %q", name, n)
			}
			jobs = append(jobs, j)
		}
		return jobs, nil
	}
	j, ok := Builtins[name]
	if !ok {
		return nil, fmt.Errorf("unknown cron job %q — run `ws cron ls` to see available jobs", name)
	}
	return []*BuiltinJob{j}, nil
}

// DataDir returns the ws cron data directory (~/.local/share/ws-tool).
func DataDir() (string, error) {
	return config.DataDir()
}

// JobsDir returns the directory where wrapper scripts are written.
func JobsDir() (string, error) {
	d, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "cron-jobs"), nil
}

// StateFilePath returns the absolute path to the cron run-state file.
func StateFilePath() (string, error) {
	d, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "cron.state"), nil
}

// LogFilePath returns the absolute path to the shared cron log file.
func LogFilePath() (string, error) {
	d, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "cron.log"), nil
}

// ScriptPath returns the absolute path for a job's wrapper script.
func ScriptPath(name string) (string, error) {
	dir, err := JobsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name+".sh"), nil
}
