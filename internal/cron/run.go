package cron

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// RunOptions configures a direct job execution via RunJob.
type RunOptions struct {
	// WorkspacePath is the ws workspace root (used to embed the correct path in
	// job-specific commands and to resolve the state/log files).
	WorkspacePath string
	// Display is the X11 DISPLAY for jobs that need one (mega-sync). If empty,
	// DefaultDisplay() is used.
	Display string
}

// RunResult is returned by RunJob.
type RunResult struct {
	Job      string
	ExitCode int
}

// RunJob executes a named builtin job inline, writing structured log and state
// entries to the state/log files in the cron data dir. It is the implementation
// of `ws cron run <job>`: crontab entries call `ws cron run <job>` instead of
// a separate wrapper script, so the logic lives entirely in the ws binary.
func RunJob(name string, opts RunOptions) (RunResult, error) {
	job, ok := Builtins[name]
	if !ok {
		return RunResult{}, fmt.Errorf("unknown cron job %q — run `ws cron ls` to see available jobs", name)
	}

	statePath, err := StateFilePath()
	if err != nil {
		return RunResult{}, err
	}
	logPath, err := LogFilePath()
	if err != nil {
		return RunResult{}, err
	}

	wsBin := WSBinary()
	display := opts.Display
	if display == "" {
		display = DefaultDisplay()
	}
	workspace := opts.WorkspacePath

	// Missed-run recovery: if the last successful run was within half the
	// interval, skip (this handles @reboot firing right after a normal reboot).
	last, _ := LastRun(statePath, job.Name)
	if !last.Time.IsZero() {
		elapsed := time.Since(last.Time)
		half := time.Duration(job.IntervalSecs/2) * time.Second
		if elapsed < half {
			logLine(logPath, job.Name, fmt.Sprintf("Skipping: last run %s ago (< half-interval %s)", elapsed.Truncate(time.Second), half))
			return RunResult{Job: job.Name, ExitCode: 0}, nil
		}
	}

	logLine(logPath, job.Name, "Starting")

	// Build and run the job body as a bash subshell.
	body := job.scriptBody(wsBin, display, workspace)
	exitCode := runBash(body)

	// Write state record.
	if err := appendState(statePath, job.Name, exitCode); err != nil {
		// Non-fatal: log it but don't fail the job.
		_ = logLine(logPath, job.Name, "Warning: could not write state: "+err.Error())
	}

	if exitCode == 0 {
		logLine(logPath, job.Name, "Done (exit 0)")
	} else {
		logLine(logPath, job.Name, fmt.Sprintf("Failed (exit %d)", exitCode))
	}

	return RunResult{Job: job.Name, ExitCode: exitCode}, nil
}

// runBash runs a bash script body in a subprocess and returns its exit code.
func runBash(body string) int {
	cmd := exec.Command("bash", "-c", body)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode()
		}
		return 1
	}
	return 0
}

// logLine appends a timestamped line to the cron log file. Errors are silently
// ignored (best-effort logging).
func logLine(logPath, jobName, msg string) error {
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	ts := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	_, err = fmt.Fprintf(f, "%s [%s] %s\n", ts, jobName, msg)
	return err
}

// appendState writes a JSONL state record for the given job run.
func appendState(statePath, jobName string, exitCode int) error {
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(statePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	ts := time.Now().UTC().Format(time.RFC3339)
	_, err = fmt.Fprintf(f, `{"job":%q,"ts":%q,"exit":%d}`+"\n", jobName, ts, exitCode)
	return err
}
