package cron

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// RunOptions configures a direct job execution via RunJob.
type RunOptions struct {
	// WorkspacePath is the ws workspace root (used to embed the correct path in
	// job-specific commands).
	WorkspacePath string
	// Display is the X11 DISPLAY for jobs that need one (mega-sync). If empty,
	// DefaultDisplay() is used.
	Display string
	// IfDue runs the job only when it is due: when the last run, recorded under
	// any of the job's names, is older than half its interval. Without it the job
	// always runs.
	IfDue bool
	// Stdout and Stderr get a copy of the job's output as it is produced, in
	// addition to cron.log. Nil means discard.
	Stdout, Stderr io.Writer
	// DryRun returns the job's command and runs and records nothing.
	DryRun bool
}

// RunResult is returned by RunJob.
type RunResult struct {
	Job      string
	ExitCode int
	// Skipped is true when --if-due found the job not due, or the job is retired.
	Skipped bool
	// Command is what the job runs (set for DryRun).
	Command string
}

// logLimit is when cron.log is trimmed, and logKeep how much of its end is kept.
const (
	logLimit = 5 << 20
	logKeep  = 5 << 19 // 2.5 MB
)

// RunJob executes a named job now, in the workspace, and records the result. It
// is the implementation of `ws cron run <job>`: crontab lines call it, so it never
// prompts and the command itself is the confirmation.
//
// An old job name runs silently as the new job. A retired name logs a skip and
// reports success, so old crontab lines do not fail. A preset cannot be run. Each
// line of the job's output goes to cron.log as `<time> [<job>] <line>`, and to the
// writers in opts.
func RunJob(name string, opts RunOptions) (RunResult, error) {
	canon, _ := Canonical(name)
	statePath, err := StateFilePath()
	if err != nil {
		return RunResult{}, err
	}
	logPath, err := LogFilePath()
	if err != nil {
		return RunResult{}, err
	}

	if IsRetired(name) {
		if !opts.DryRun {
			trimLog(logPath)
			logLine(logPath, name, fmt.Sprintf("Skipping: %s is no longer a ws job; run ws cron rm %s", name, name))
		}
		return RunResult{Job: name, Skipped: true}, nil
	}
	job, ok := Builtins[canon]
	if !ok {
		return RunResult{}, fmt.Errorf("unknown cron job %q — run `ws cron ls` to see available jobs", name)
	}

	display := opts.Display
	if display == "" {
		display = DefaultDisplay()
	}
	body := job.scriptBody(WSBinary(), display, opts.WorkspacePath)
	if opts.DryRun {
		return RunResult{Job: job.Name, Command: body}, nil
	}

	trimLog(logPath)
	// --if-due: skip when the last run, under any of the job's names, is newer
	// than half the interval. This handles an @reboot line firing right after a
	// normal run.
	if opts.IfDue {
		last, _ := LastRun(statePath, job.Name)
		if !last.Time.IsZero() {
			elapsed := time.Since(last.Time)
			half := time.Duration(job.IntervalSecs/2) * time.Second
			if elapsed < half {
				logLine(logPath, job.Name, fmt.Sprintf("Skipping: last run %s ago (< half-interval %s)", elapsed.Truncate(time.Second), half))
				return RunResult{Job: job.Name, Skipped: true}, nil
			}
		}
	}

	logLine(logPath, job.Name, "Starting")
	exitCode, lines := runBash(body, job.Name, logPath, opts.Stdout, opts.Stderr)

	if err := appendState(statePath, job.Name, exitCode); err != nil {
		// Non-fatal: log it but don't fail the job.
		_ = logLine(logPath, job.Name, "Warning: could not write state: "+err.Error())
	}
	if exitCode == 0 {
		logLine(logPath, job.Name, "Done (exit 0)")
	} else {
		logFailure(logPath, job.Name, body, exitCode, lines)
	}
	return RunResult{Job: job.Name, ExitCode: exitCode}, nil
}

// runBash runs a bash script body in a subprocess and returns its exit code. Its
// stdout and stderr are read line by line into the cron log under the job's
// name, and copied to the writers. stdin is not connected.
func runBash(body, job, logPath string, stdout, stderr io.Writer) (exitCode, lines int) {
	cmd := exec.Command("bash", "-c", body)
	// Cron's PATH is /usr/bin:/bin. Helpers that run `ws` by name (the git
	// credential wrapper) need the directory ws was started from.
	cmd.Env = append(os.Environ(), "PATH="+filepath.Dir(WSBinary())+string(os.PathListSeparator)+os.Getenv("PATH"))
	outPipe, err := cmd.StdoutPipe()
	if err != nil {
		return 1, 0
	}
	errPipe, err := cmd.StderrPipe()
	if err != nil {
		return 1, 0
	}
	if err := cmd.Start(); err != nil {
		logLine(logPath, job, "error: "+err.Error())
		return 1, 0
	}

	var mu sync.Mutex // serializes the log, the writers and n
	n := 0
	pump := func(r io.Reader, w io.Writer) {
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
		for sc.Scan() {
			line := sc.Text()
			mu.Lock()
			n++
			logLine(logPath, job, line)
			if w != nil {
				fmt.Fprintln(w, line)
			}
			mu.Unlock()
		}
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); pump(outPipe, stdout) }()
	go func() { defer wg.Done(); pump(errPipe, stderr) }()
	wg.Wait()

	if err := cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode(), n
		}
		return 1, 0
	}
	return 0, n
}

// failureEnv are the variables that most often explain a job that works in a
// shell but fails under cron: a missing HOME or PATH, or no display, gpg agent
// or session bus.
var failureEnv = []string{"HOME", "USER", "PATH", "DISPLAY", "GNUPGHOME", "GPG_TTY", "DBUS_SESSION_BUS_ADDRESS", "XDG_RUNTIME_DIR"}

// logFailure writes why a job failed: the exit code, the command, whether it
// printed anything, and the environment it ran in. A job that dies without
// output (a --quiet command, a killed process) is otherwise a bare "Failed".
func logFailure(logPath, job, body string, exitCode, lines int) {
	logLine(logPath, job, fmt.Sprintf("Failed (exit %d)", exitCode))
	if lines == 0 {
		logLine(logPath, job, "The job printed no output before failing.")
	}
	logLine(logPath, job, "command: "+strings.Join(strings.Fields(body), " "))
	for _, k := range failureEnv {
		v, ok := os.LookupEnv(k)
		if !ok {
			v = "(unset)"
		}
		logLine(logPath, job, fmt.Sprintf("env %s=%s", k, v))
	}
	logLine(logPath, job, fmt.Sprintf("To reproduce: ws cron run %s", job))
}

// trimLog keeps only the last logKeep bytes of cron.log when it is over logLimit.
// It starts at a line boundary.
func trimLog(logPath string) {
	fi, err := os.Stat(logPath)
	if err != nil || fi.Size() <= logLimit {
		return
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		return
	}
	data = data[len(data)-logKeep:]
	for i, b := range data {
		if b == '\n' {
			data = data[i+1:]
			break
		}
	}
	_ = os.WriteFile(logPath, data, 0o644)
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
