package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestContextCreateFromCLI(t *testing.T) {
	wsRoot := filepath.Join(t.TempDir(), "Workspace")
	project := filepath.Join(wsRoot, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}

	// Initialize workspace first so requireWorkspaceInitialized succeeds.
	var initOut, initErr bytes.Buffer
	if code := Execute([]string{"init", "--workspace", wsRoot}, strings.NewReader("y\n"), &initOut, &initErr); code != 0 {
		t.Fatalf("ws init failed: code=%d stderr=%s", code, initErr.String())
	}

	var out bytes.Buffer
	var errOut bytes.Buffer

	code := Execute([]string{"--workspace", wsRoot, "context", "create", "task-1", "--path", project}, strings.NewReader("y\n"), &out, &errOut)
	if code != 0 {
		t.Fatalf("context create failed: code=%d stderr=%s", code, errOut.String())
	}

	if !strings.Contains(out.String(), "task-1") {
		t.Fatalf("unexpected output: %s", out.String())
	}
	if _, err := os.Stat(filepath.Join(project, ".ws-context", "task-1")); err != nil {
		t.Fatalf("context dir not created: %v", err)
	}
}

func TestContextListAndUpdateFromCLI(t *testing.T) {
	wsRoot := filepath.Join(t.TempDir(), "Workspace")
	project := filepath.Join(wsRoot, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}

	var initOut, initErr bytes.Buffer
	if code := Execute([]string{"init", "--workspace", wsRoot}, strings.NewReader("y\n"), &initOut, &initErr); code != 0 {
		t.Fatalf("ws init failed: code=%d stderr=%s", code, initErr.String())
	}

	var out, errOut bytes.Buffer
	code := Execute([]string{"--workspace", wsRoot, "context", "create", "task-1", "--path", project}, strings.NewReader("y\n"), &out, &errOut)
	if code != 0 {
		t.Fatalf("context create failed: code=%d stderr=%s", code, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	code = Execute([]string{"--workspace", wsRoot, "context", "list"}, strings.NewReader("y\n"), &out, &errOut)
	if code != 0 {
		t.Fatalf("context list failed: code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "task-1") {
		t.Fatalf("expected task-1 in list output, got: %s", out.String())
	}

	if err := os.MkdirAll(filepath.Join(project, ".ws-context", "task-2"), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}

	out.Reset()
	errOut.Reset()
	code = Execute([]string{"--workspace", wsRoot, "context", "list", "--find"}, strings.NewReader("y\n"), &out, &errOut)
	if code != 0 {
		t.Fatalf("context list --find failed: code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "task-2") {
		t.Fatalf("expected task-2 in updated list output, got: %s", out.String())
	}
}

func TestContextRmClearsContextListIndex(t *testing.T) {
	wsRoot := filepath.Join(t.TempDir(), "Workspace")
	project := filepath.Join(wsRoot, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}

	var initOut, initErr bytes.Buffer
	if code := Execute([]string{"init", "--workspace", wsRoot}, strings.NewReader("y\n"), &initOut, &initErr); code != 0 {
		t.Fatalf("ws init failed: code=%d stderr=%s", code, initErr.String())
	}

	var out, errOut bytes.Buffer
	code := Execute([]string{"--workspace", wsRoot, "context", "create", "task-1", "--path", project}, strings.NewReader("y\n"), &out, &errOut)
	if code != 0 {
		t.Fatalf("context create failed: code=%d stderr=%s", code, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	code = Execute([]string{"--workspace", wsRoot, "context", "rm", "--all"}, strings.NewReader("y\n"), &out, &errOut)
	if code != 0 {
		t.Fatalf("context rm --all failed: code=%d stderr=%s", code, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	code = Execute([]string{"--workspace", wsRoot, "context", "list"}, strings.NewReader("y\n"), &out, &errOut)
	if code != 0 {
		t.Fatalf("context list failed: code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "No contexts found.") {
		t.Fatalf("expected empty context list after rm --all, got: %s", out.String())
	}
}
