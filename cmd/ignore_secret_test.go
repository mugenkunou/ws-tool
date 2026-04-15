package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIgnoreCheckAndSecretScan(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "Workspace")
	var out bytes.Buffer
	var errOut bytes.Buffer

	if code := Execute([]string{"init", "--workspace", workspace}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("init failed: code=%d stderr=%s", code, errOut.String())
	}

	file := filepath.Join(workspace, "secrets.txt")
	if err := os.WriteFile(file, []byte("password=letmein\n"), 0o644); err != nil {
		t.Fatalf("write secret file failed: %v", err)
	}

	out.Reset()
	errOut.Reset()
	// Use "ws ignore ls <path>" which replaces the old "ws ignore check" command.
	if code := Execute([]string{"--workspace", workspace, "ignore", "ls", "secrets.txt"}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("ignore ls <path> failed: code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "SYNCED") {
		t.Fatalf("unexpected ignore ls path output: %s", out.String())
	}

	out.Reset()
	errOut.Reset()
	if code := Execute([]string{"--workspace", workspace, "secret", "scan"}, strings.NewReader("y\n"), &out, &errOut); code != 2 {
		t.Fatalf("expected secret scan code 2, got %d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}
}
