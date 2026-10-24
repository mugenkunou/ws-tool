// Package cron manages ws-scheduled jobs (spec §5 cron).
//
// A row is a built-in job or a preset. Its state comes from three places: the
// live user crontab (crontab -l), the declared crontab <workspace>/configs/crontab
// (synced with the workspace), and the run history in $XDG_STATE_HOME/ws-tool/
// (cron.state and cron.log, machine-local). ws owns every block between
// `# >>> ws:<name> >>>` and `# <<< ws:<name> <<<` and never changes a line
// outside the markers.
//
// Three shortcomings of a plain crontab are addressed:
//   - Missed-run recovery: both crontab lines pass --if-due, and an @reboot line
//     catches up on slots missed while the machine was off.
//   - Structured logging: a job's output goes to cron.log with a timestamp and a
//     job-name prefix, size-capped at 5 MB.
//   - Declared state: add and rm write configs/crontab too, and apply installs it
//     on a new machine.
package cron

import (
	"fmt"
	"path/filepath"
	"slices"
	"sort"

	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

// BuiltinJob defines a ws-managed cron job.
type BuiltinJob struct {
	// Name is the job identifier used in commands and crontab markers.
	Name string
	// Schedule is the cron expression for the scheduled entry.
	Schedule string
	// IntervalSecs is the expected interval between runs in seconds. A job is
	// due when its last run is older than half of it.
	IntervalSecs int
	// Description is a one-line human-readable summary.
	Description string
	// NeedsDisplay is true for jobs that run a GUI program: their crontab lines
	// carry --display.
	NeedsDisplay bool
	// scriptBody returns the shell commands the job runs. wsBin is the absolute
	// ws binary path, display the X11 DISPLAY value, workspace the workspace root.
	scriptBody func(wsBin, display, workspace string) string
}

// Builtins is the registry of all built-in named cron jobs.
var Builtins = map[string]*BuiltinJob{
	"mega-sync":   megaSyncJob,
	"pass-backup": passBackupJob,
}

// Aliases maps an old job name to the job that replaced it. Old crontab lines
// keep working (`cron run` takes the old name silently). None are defined now.
var Aliases = map[string]string{}

// Retired are jobs that no longer exist. `cron add` rejects them, `cron rm` still
// removes their blocks, and `cron run` logs a skip and exits 0, so old crontab
// lines do not fail. Nothing commits your work unattended, so repo-sync is gone, and
// session-prune went with the sessions size cap (`ws session rm --older-than` is manual).
var Retired = []string{"secret-scan", "ignore-scan", "repo-sync", "session-prune"}

// AllNames returns every job name, sorted. It holds neither aliases nor retired
// names: those are hidden.
func AllNames() []string { return JobNames() }

// JobNames returns the built-in job names, sorted.
func JobNames() []string {
	var names []string
	for k := range Builtins {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// IsRetired reports whether name is a retired job.
func IsRetired(name string) bool { return slices.Contains(Retired, name) }

// Canonical maps an old job name to its current one. aliased is true when name
// was an old name.
func Canonical(name string) (job string, aliased bool) {
	if c, ok := Aliases[name]; ok {
		return c, true
	}
	return name, false
}

// Names returns a job's current name followed by its old names.
func Names(job string) []string {
	out := []string{job}
	for old, cur := range Aliases {
		if cur == job {
			out = append(out, old)
		}
	}
	sort.Strings(out[1:])
	return out
}

// Resolve expands a name (a job, an old job name, or a preset) to its
// BuiltinJobs. aliased is true when name was an old job name. A retired name is an
// error that says so, and so is an unknown one.
func Resolve(name string) (jobs []*BuiltinJob, aliased bool, err error) {
	canon, aliased := Canonical(name)
	if j, ok := Builtins[canon]; ok {
		return []*BuiltinJob{j}, aliased, nil
	}
	if IsRetired(name) {
		return nil, false, fmt.Errorf("cron job %q was removed", name)
	}
	return nil, false, fmt.Errorf("unknown cron job %q — run `ws cron ls` to see available jobs", name)
}

// DataDir returns the machine-local state directory ($XDG_STATE_HOME/ws-tool).
func DataDir() (string, error) {
	return wsconfig.StateDir()
}

// JobsDir returns the directory where legacy wrapper scripts were written.
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

// ScriptPath returns the absolute path of a job's legacy wrapper script, which
// `cron rm` still deletes if one is there.
func ScriptPath(name string) (string, error) {
	dir, err := JobsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name+".sh"), nil
}

// DeclaredPath is <workspace>/configs/crontab, the declared crontab.
func DeclaredPath(workspace string) string {
	return filepath.Join(workspace, "configs", "crontab")
}
