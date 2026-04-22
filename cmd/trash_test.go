package cmd

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestTrashStatusAndSetupFromCLI(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	workspace := filepath.Join(t.TempDir(), "Workspace")
	var out bytes.Buffer
	var errOut bytes.Buffer

	if code := Execute([]string{"init", "--workspace", workspace}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("init failed: code=%d stderr=%s", code, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	if code := Execute([]string{"--workspace", workspace, "trash", "status"}, strings.NewReader("y\n"), &out, &errOut); code != 2 {
		t.Fatalf("expected trash status exit 2 before setup, got=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "WARNING") {
		t.Fatalf("unexpected trash status output: %s", out.String())
	}

	out.Reset()
	errOut.Reset()
	if code := Execute([]string{"--workspace", workspace, "trash", "enable"}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("trash enable failed: code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}

	out.Reset()
	errOut.Reset()
	if code := Execute([]string{"--workspace", workspace, "trash", "status"}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("expected trash status exit 0 after setup, got=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "OK") {
		t.Fatalf("unexpected trash status output: %s", out.String())
	}
}
