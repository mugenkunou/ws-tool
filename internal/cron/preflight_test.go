package cron_test

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/mugenkunou/ws-tool/internal/cron"
)

// shimCrontab installs a fake crontab binary in a temp dir and prepends it to PATH.
// The shim exits with the given code and writes msg to stdout.
// Returns a cleanup function.
func shimCrontab(t *testing.T, exitCode int, msg string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("crontab shim not supported on Windows")
	}
	shimDir := t.TempDir()
	script := fmt.Sprintf("#!/bin/sh\necho '%s'\nexit %d\n", msg, exitCode)
	shimPath := filepath.Join(shimDir, "crontab")
	if err := os.WriteFile(shimPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write shim: %v", err)
	}
	t.Setenv("PATH", shimDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestPreflightAccessDenied(t *testing.T) {
	shimCrontab(t, 1, "/etc/cron.allow: Permission denied")
	err := cron.Preflight()
	if err == nil {
		t.Fatal("expected PreflightError, got nil")
	}
	pe, ok := err.(*cron.PreflightError)
	if !ok {
		t.Fatalf("expected *PreflightError, got %T: %v", err, err)
	}
	if pe.Kind != "access_denied" {
		t.Errorf("expected Kind=access_denied, got %q", pe.Kind)
	}
	if pe.Remediation() == "" {
		t.Error("Remediation should be non-empty")
	}
}

func TestPreflightNoCrontabForUser(t *testing.T) {
	shimCrontab(t, 1, "no crontab for user")
	err := cron.Preflight()
	if err != nil {
		t.Fatalf("expected nil for 'no crontab for user', got: %v", err)
	}
}

func TestPreflightEmpty(t *testing.T) {
	shimCrontab(t, 0, "")
	err := cron.Preflight()
	if err != nil {
		t.Fatalf("expected nil for successful crontab -l, got: %v", err)
	}
}

func TestPreflightBinaryMissing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PATH manipulation not reliable on Windows")
	}
	// Point PATH to an empty directory so crontab is not found.
	empty := t.TempDir()
	t.Setenv("PATH", empty)

	err := cron.Preflight()
	if err == nil {
		t.Fatal("expected PreflightError for missing binary, got nil")
	}
	pe, ok := err.(*cron.PreflightError)
	if !ok {
		t.Fatalf("expected *PreflightError, got %T", err)
	}
	if pe.Kind != "binary_missing" {
		t.Errorf("expected Kind=binary_missing, got %q", pe.Kind)
	}
}
