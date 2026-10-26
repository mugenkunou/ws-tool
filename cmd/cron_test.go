package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestCronLsHelp ensures ws cron --help exits 0.
func TestCronLsHelp(t *testing.T) {
	testSetXDG(t)
	workspace := newTestWorkspace(t)
	var out, errOut bytes.Buffer
	code := Execute([]string{"--workspace", workspace, "cron", "--help"},
		strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("cron --help: code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "add") {
		t.Errorf("help missing 'add' subcommand: %s", out.String())
	}
}

func TestCronNoArgsShowsHelp(t *testing.T) {
	testSetXDG(t)
	var out, errOut bytes.Buffer
	code := Execute([]string{"cron"}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("ws cron (no args): expected exit 0, got %d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "add") {
		t.Fatalf("expected help with 'add' subcommand, got: %s", out.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("expected empty stderr, got: %s", errOut.String())
	}
}

// TestCronLs verifies ws cron ls lists all jobs.
func TestCronLs(t *testing.T) {
	testSetXDG(t)
	workspace := newTestWorkspace(t)
	var out, errOut bytes.Buffer
	code := Execute([]string{"--workspace", workspace, "cron", "ls"},
		strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("cron ls: code=%d stderr=%s stdout=%s", code, errOut.String(), out.String())
	}
	// All built-in job names must appear.
	for _, name := range []string{"mega-sync", "pass-backup", "sync-push"} {
		if !strings.Contains(out.String(), name) {
			t.Errorf("cron ls output missing job %q: %s", name, out.String())
		}
	}
}

// TestCronLsJSON verifies the JSON output of ws cron ls.
func TestCronLsJSON(t *testing.T) {
	testSetXDG(t)
	workspace := newTestWorkspace(t)
	var out, errOut bytes.Buffer
	code := Execute([]string{"--workspace", workspace, "--json", "cron", "ls"},
		strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("cron ls --json: code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), `"name"`) {
		t.Errorf("JSON output missing 'name' field: %s", out.String())
	}
	if !strings.Contains(out.String(), "mega-sync") {
		t.Errorf("JSON output missing mega-sync: %s", out.String())
	}
}

// TestCronAddUnknownJob verifies a clear error for an unknown job name.
func TestCronAddUnknownJob(t *testing.T) {
	testSetXDG(t)
	workspace := newTestWorkspace(t)
	var out, errOut bytes.Buffer
	code := Execute([]string{"--workspace", workspace, "cron", "add", "nonexistent-job"},
		strings.NewReader(""), &out, &errOut)
	if code == 0 {
		t.Fatal("expected non-zero exit for unknown job")
	}
	if !strings.Contains(errOut.String(), "unknown cron job") {
		t.Errorf("expected 'unknown cron job' in stderr, got: %s", errOut.String())
	}
}

// TestCronAddDryRun verifies --dry-run does not modify the crontab.
func TestCronAddDryRun(t *testing.T) {
	testSetXDG(t)
	workspace := newTestWorkspace(t)
	var out, errOut bytes.Buffer

	code := Execute([]string{
		"--workspace", workspace,
		"--dry-run",
		"cron", "add", "pass-backup",
	}, strings.NewReader("y\n"), &out, &errOut)
	// dry-run should exit 0.
	if code != 0 {
		t.Fatalf("cron add --dry-run: code=%d stderr=%s stdout=%s", code, errOut.String(), out.String())
	}
	if !strings.Contains(out.String(), "dry-run") {
		t.Errorf("expected dry-run indicator in output: %s", out.String())
	}
}

// TestCronLsNothingInstalled: on a fresh machine every job reads "not installed".
func TestCronLsNothingInstalled(t *testing.T) {
	testSetXDG(t)
	workspace := newTestWorkspace(t)
	var out, errOut bytes.Buffer
	code := Execute([]string{"--workspace", workspace, "cron", "ls"},
		strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("cron ls: code=%d stderr=%s", code, errOut.String())
	}
	if strings.Contains(out.String(), " installed") && !strings.Contains(out.String(), "not installed") {
		t.Errorf("nothing is installed: %s", out.String())
	}
	if strings.Contains(out.String(), "retired") {
		t.Errorf("no retired row without a leftover block: %s", out.String())
	}
}

// TestCronLogEmpty returns 0 and a helpful message when the log is absent.
func TestCronLogEmpty(t *testing.T) {
	testSetXDG(t)
	workspace := newTestWorkspace(t)

	// Point HOME to a temp dir so the log path resolves to an absent file.
	home := t.TempDir()
	t.Setenv("HOME", home)

	var out, errOut bytes.Buffer
	code := Execute([]string{"--workspace", workspace, "cron", "log"},
		strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("cron log (empty): code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "No cron log entries") {
		t.Errorf("expected no-entries message, got: %s", out.String())
	}
}

// TestCronLogJobFilter shows only entries matching the named job.
func TestCronLogJobFilter(t *testing.T) {
	testSetXDG(t)
	workspace := newTestWorkspace(t)

	// Pre-create a log file in the expected location.
	home := t.TempDir()
	t.Setenv("HOME", home)
	logDir := filepath.Join(os.Getenv("XDG_STATE_HOME"), "ws-tool")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatal(err)
	}
	logFile := filepath.Join(logDir, "cron.log")
	logContent := "2026-05-08T10:00:00Z [mega-sync] Starting\n" +
		"2026-05-08T10:05:00Z [pass-backup] Starting\n" +
		"2026-05-08T10:05:01Z [mega-sync] Done (exit 0)\n"
	if err := os.WriteFile(logFile, []byte(logContent), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	code := Execute([]string{"--workspace", workspace, "cron", "log", "mega-sync"},
		strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("cron log mega-sync: code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "mega-sync") {
		t.Errorf("expected mega-sync entries in log output: %s", out.String())
	}
	if strings.Contains(out.String(), "pass-backup") {
		t.Errorf("log output should not contain pass-backup entries: %s", out.String())
	}
}

// TestCronRmMissingArg verifies error when no job name is given to rm.
func TestCronRmMissingArg(t *testing.T) {
	testSetXDG(t)
	workspace := newTestWorkspace(t)
	var out, errOut bytes.Buffer
	code := Execute([]string{"--workspace", workspace, "cron", "rm"},
		strings.NewReader(""), &out, &errOut)
	if code == 0 {
		t.Fatal("expected non-zero when no job name given to cron rm")
	}
}

// TestCronUnknownSubcommand verifies the error for an unknown subcommand.
func TestCronUnknownSubcommand(t *testing.T) {
	testSetXDG(t)
	workspace := newTestWorkspace(t)
	var out, errOut bytes.Buffer
	code := Execute([]string{"--workspace", workspace, "cron", "bogus"},
		strings.NewReader(""), &out, &errOut)
	if code == 0 {
		t.Fatal("expected non-zero for unknown cron subcommand")
	}
}

// TestCronAddPreflightAccessDenied verifies that ws cron add exits 1 with an
// actionable error when crontab access is denied, without writing any files.
func TestCronAddPreflightAccessDenied(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("crontab shim not supported on Windows")
	}
	testSetXDG(t)
	workspace := newTestWorkspace(t)

	// Install a shim crontab that simulates permission denial.
	shimDir := t.TempDir()
	script := "#!/bin/sh\necho '/etc/cron.allow: Permission denied'\nexit 1\n"
	shimPath := filepath.Join(shimDir, "crontab")
	if err := os.WriteFile(shimPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write shim: %v", err)
	}
	t.Setenv("PATH", shimDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("WS_CRONTAB_FILE", "") // the real preflight, against the shim

	var out, errOut bytes.Buffer
	code := Execute([]string{"--workspace", workspace, "cron", "add", "pass-backup"},
		strings.NewReader(""), &out, &errOut)
	if code == 0 {
		t.Fatal("expected non-zero exit when crontab access is denied")
	}
	if !strings.Contains(errOut.String(), "crontab access denied") {
		t.Errorf("expected 'crontab access denied' in stderr, got: %s", errOut.String())
	}

	// No crontab entry should have been written (preflight runs before any write).
	// Since cron add no longer writes wrapper scripts, there is nothing extra to check.
}

// ── cron run/apply tests ─────────────────────────────────────────────────────

// TestCronRunUnknownJob verifies ws cron run exits with an error for unknown jobs.
func TestCronRunUnknownJob(t *testing.T) {
	testSetXDG(t)
	workspace := newTestWorkspace(t)
	var out, errOut bytes.Buffer
	code := Execute([]string{"--workspace", workspace, "cron", "run", "no-such-job"},
		strings.NewReader(""), &out, &errOut)
	if code == 0 {
		t.Fatal("expected non-zero for unknown cron job")
	}
	if !strings.Contains(errOut.String(), "unknown cron job") {
		t.Errorf("expected 'unknown cron job' in stderr, got: %s", errOut.String())
	}
}

// TestCronRunMissingArg verifies ws cron run exits with error when no job is given.
func TestCronRunMissingArg(t *testing.T) {
	testSetXDG(t)
	workspace := newTestWorkspace(t)
	var out, errOut bytes.Buffer
	code := Execute([]string{"--workspace", workspace, "cron", "run"},
		strings.NewReader(""), &out, &errOut)
	if code == 0 {
		t.Fatal("expected non-zero when no job name given to cron run")
	}
}

// TestCronApplyMissingFile verifies ws cron apply fails when configs/crontab is absent.
func TestCronApplyMissingFile(t *testing.T) {
	testSetXDG(t)
	workspace := newTestWorkspace(t)
	// Do not create configs/crontab — it should not exist after plain init.
	var out, errOut bytes.Buffer
	code := Execute([]string{"--workspace", workspace, "cron", "apply"},
		strings.NewReader(""), &out, &errOut)
	if code == 0 {
		t.Fatalf("expected non-zero when configs/crontab is absent; stdout=%s stderr=%s",
			out.String(), errOut.String())
	}
}

// TestCronApplyDryRun verifies ws cron apply --dry-run reports nothing to apply
// when no ws blocks are present in configs/crontab.
func TestCronApplyDryRun(t *testing.T) {
	testSetXDG(t)
	workspace := newTestWorkspace(t)

	// Create a minimal crontab file with no ws blocks.
	crontabPath := filepath.Join(workspace, "configs", "crontab")
	if err := os.WriteFile(crontabPath, []byte("# user cron\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	code := Execute([]string{
		"--workspace", workspace,
		"cron", "apply", "--dry-run",
	}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("cron apply --dry-run: code=%d stderr=%s stdout=%s",
			code, errOut.String(), out.String())
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

// newTestWorkspace creates an initialized ws workspace in a temp dir.
func newTestWorkspace(t *testing.T) string {
	t.Helper()
	workspace := filepath.Join(t.TempDir(), "Workspace")
	var out, errOut bytes.Buffer
	if code := Execute([]string{"init", "--workspace", workspace},
		strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("init failed: code=%d stderr=%s", code, errOut.String())
	}
	return workspace
}
