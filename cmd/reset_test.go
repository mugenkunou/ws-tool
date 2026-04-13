package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mugenkunou/ws-tool/internal/provision"
)

func TestUninitFailsWithoutWorkspace(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "Workspace")
	var out bytes.Buffer
	var errOut bytes.Buffer

	code := Execute([]string{"--workspace", workspace, "reset"}, strings.NewReader("y\n"), &out, &errOut)
	if code != 1 {
		t.Fatalf("expected exit 1, got %d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}
	if !strings.Contains(errOut.String(), "No ws/ directory") {
		t.Fatalf("expected 'No ws/ directory' error, got stderr=%s", errOut.String())
	}
}

func TestInitAlreadyInitializedWarns(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "Workspace")
	var out bytes.Buffer
	var errOut bytes.Buffer

	// First init should succeed.
	if code := Execute([]string{"init", "--workspace", workspace}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("first init failed: code=%d stderr=%s", code, errOut.String())
	}

	out.Reset()
	errOut.Reset()

	// Second init should warn and not re-initialize.
	code := Execute([]string{"init", "--workspace", workspace}, strings.NewReader("y\n"), &out, &errOut)
	if code != 0 {
		t.Fatalf("expected exit 0 for already initialized, got %d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "already initialized") {
		t.Fatalf("expected 'already initialized' warning, got stdout=%s", out.String())
	}
	if !strings.Contains(out.String(), "ws reset") {
		t.Fatalf("expected suggestion mentioning 'ws reset', got stdout=%s", out.String())
	}
}

func TestInitAlreadyInitializedJSON(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "Workspace")
	var out bytes.Buffer
	var errOut bytes.Buffer

	// First init.
	if code := Execute([]string{"init", "--workspace", workspace}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("first init failed: code=%d stderr=%s", code, errOut.String())
	}

	out.Reset()
	errOut.Reset()

	// Second init with --json.
	code := Execute([]string{"--json", "init", "--workspace", workspace}, strings.NewReader("y\n"), &out, &errOut)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "already_initialized") {
		t.Fatalf("expected JSON with 'already_initialized', got stdout=%s", out.String())
	}
}

func TestInitRecordsProvisions(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "Workspace")
	var out bytes.Buffer
	var errOut bytes.Buffer

	if code := Execute([]string{"init", "--workspace", workspace}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("init failed: code=%d stderr=%s", code, errOut.String())
	}

	provPath := provision.LedgerPath(workspace)
	ledger, err := provision.Load(provPath)
	if err != nil {
		t.Fatalf("load provisions: %v", err)
	}

	if len(ledger.Entries) == 0 {
		t.Fatal("expected provisions to be recorded after init")
	}

	// Should have recorded ws/ dir, config.json, manifest.json, .megaignore.
	types := map[provision.Type]int{}
	for _, e := range ledger.Entries {
		types[e.Type]++
		if e.Command != "init" {
			t.Fatalf("expected command 'init', got %q", e.Command)
		}
	}
	if types[provision.TypeDir] < 1 {
		t.Fatal("expected at least one dir provision")
	}
	if types[provision.TypeFile] < 2 {
		t.Fatal("expected at least two file provisions (config.json, manifest.json)")
	}
}

func TestUninitRemovesWSDirectory(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "Workspace")
	var out bytes.Buffer
	var errOut bytes.Buffer

	// Init first.
	if code := Execute([]string{"init", "--workspace", workspace}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("init failed: code=%d stderr=%s", code, errOut.String())
	}

	// Verify ws/ exists.
	wsDir := filepath.Join(workspace, "ws")
	if _, err := os.Stat(wsDir); err != nil {
		t.Fatalf("ws/ should exist after init: %v", err)
	}

	// Reset.
	out.Reset()
	errOut.Reset()
	code := Execute([]string{"--workspace", workspace, "reset"}, strings.NewReader("y\n"), &out, &errOut)
	if code != 0 {
		t.Fatalf("reset failed: code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}

	// ws/ should be gone.
	if _, err := os.Stat(wsDir); !os.IsNotExist(err) {
		t.Fatal("ws/ should be deleted after reset")
	}

	if !strings.Contains(out.String(), "WORKSPACE reset") {
		t.Fatalf("expected WORKSPACE reset header, got: %s", out.String())
	}
}

func TestUninitDryRunPreservesFiles(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "Workspace")
	var out bytes.Buffer
	var errOut bytes.Buffer

	// Init first.
	if code := Execute([]string{"init", "--workspace", workspace}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("init failed: code=%d stderr=%s", code, errOut.String())
	}

	// Reset --dry-run.
	out.Reset()
	errOut.Reset()
	code := Execute([]string{"--workspace", workspace, "reset", "--dry-run"}, strings.NewReader("y\n"), &out, &errOut)
	if code != 0 {
		t.Fatalf("reset dry-run failed: code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}

	// ws/ should still exist.
	wsDir := filepath.Join(workspace, "ws")
	if _, err := os.Stat(wsDir); err != nil {
		t.Fatal("ws/ should still exist after dry-run")
	}

	if !strings.Contains(out.String(), "dry-run") {
		t.Fatalf("expected dry-run in output, got: %s", out.String())
	}
}

func TestUninitUndoesProvisions(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "Workspace")
	var out bytes.Buffer
	var errOut bytes.Buffer

	// Init workspace.
	if code := Execute([]string{"init", "--workspace", workspace}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("init failed: code=%d stderr=%s", code, errOut.String())
	}

	// Manually record a provision to test undo (a file outside ws/).
	externalFile := filepath.Join(t.TempDir(), "ws-trash-rm")
	os.WriteFile(externalFile, []byte("#!/bin/bash"), 0o755)
	provPath := provision.LedgerPath(workspace)
	provision.Record(provPath, provision.Entry{
		Type:    provision.TypeFile,
		Path:    externalFile,
		Command: "trash enable",
	})

	// Verify external file exists.
	if _, err := os.Stat(externalFile); err != nil {
		t.Fatal("external file should exist before reset")
	}

	// Reset.
	out.Reset()
	errOut.Reset()
	code := Execute([]string{"--workspace", workspace, "reset"}, strings.NewReader("y\n"), &out, &errOut)
	if code != 0 {
		t.Fatalf("reset failed: code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}

	// External file should be deleted.
	if _, err := os.Stat(externalFile); !os.IsNotExist(err) {
		t.Fatal("external file should be deleted by reset")
	}

	// Workspace reset message.
	if !strings.Contains(out.String(), "Workspace reset") {
		t.Fatalf("expected 'Workspace reset' in output, got: %s", out.String())
	}
}

func TestUninitJSON(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "Workspace")
	var out bytes.Buffer
	var errOut bytes.Buffer

	// Init.
	if code := Execute([]string{"init", "--workspace", workspace}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("init failed: code=%d stderr=%s", code, errOut.String())
	}

	// Reset --json.
	out.Reset()
	errOut.Reset()
	code := Execute([]string{"--workspace", workspace, "--json", "reset"}, strings.NewReader("y\n"), &out, &errOut)
	if code != 0 {
		t.Fatalf("reset json failed: code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}

	if !strings.Contains(out.String(), "\"workspace_path\"") {
		t.Fatalf("expected JSON output with workspace_path, got: %s", out.String())
	}
	if !strings.Contains(out.String(), "\"ws_dir_removed\"") {
		t.Fatalf("expected JSON output with ws_dir_removed, got: %s", out.String())
	}
}
