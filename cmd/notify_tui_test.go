package cmd

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestNotifyCommands(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "Workspace")
	var out bytes.Buffer
	var errOut bytes.Buffer

	if code := Execute([]string{"init", "--workspace", workspace}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("init failed: code=%d stderr=%s", code, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	if code := Execute([]string{"--workspace", workspace, "notify", "start"}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("notify start failed: code=%d stderr=%s", code, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	if code := Execute([]string{"--workspace", workspace, "notify", "status"}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("notify status failed: code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "ACTIVE") && !strings.Contains(out.String(), "active") && !strings.Contains(out.String(), "running") {
		t.Fatalf("unexpected notify status output: %s", out.String())
	}

	out.Reset()
	errOut.Reset()
	if code := Execute([]string{"--workspace", workspace, "notify", "test"}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("notify test failed: code=%d stderr=%s", code, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	if code := Execute([]string{"--workspace", workspace, "notify", "stop"}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("notify stop failed: code=%d stderr=%s", code, errOut.String())
	}
}

func TestTUICommand(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "Workspace")
	var out bytes.Buffer
	var errOut bytes.Buffer

	if code := Execute([]string{"init", "--workspace", workspace}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("init failed: code=%d stderr=%s", code, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	code := Execute([]string{"--workspace", workspace, "tui"}, strings.NewReader("y\n"), &out, &errOut)
	if code != 0 {
		t.Fatalf("tui failed: code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "dashboard") && !strings.Contains(out.String(), "ws") {
		t.Fatalf("unexpected tui output: %s", out.String())
	}
}
