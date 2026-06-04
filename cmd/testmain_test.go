package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/mugenkunou/ws-tool/internal/config"
)

func TestMain(m *testing.M) {
	shimDir, err := os.MkdirTemp("", "ws-tool-test-bin-")
	if err == nil {
		if createEditorShim(shimDir) == nil {
			_ = os.Setenv("PATH", shimDir+string(os.PathListSeparator)+os.Getenv("PATH"))
		}
		defer os.RemoveAll(shimDir)
	}

	// Redirect XDG config to a temp dir so tests don't use the real ~/.config.
	xdgDir, err := os.MkdirTemp("", "ws-tool-test-xdg-")
	if err == nil {
		_ = os.Setenv("XDG_CONFIG_HOME", xdgDir)
		defer os.RemoveAll(xdgDir)
	}

	os.Exit(m.Run())
}

// testSetXDG sets XDG_CONFIG_HOME to a test-scoped temp dir for isolation.
// Must be called at the start of any test that calls ws init.
func testSetXDG(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
}

// testConfigPath returns a per-test config path inside a test-scoped temp dir.
func testConfigPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "ws-tool", "config.json")
}

// xdgConfigPath returns the XDG config path that config.DefaultPath() would resolve
// to in the current test environment. Useful for tests that need to write custom config.
func xdgConfigPath(t *testing.T) string {
	t.Helper()
	p, err := config.DefaultPath()
	if err != nil {
		t.Fatalf("config.DefaultPath: %v", err)
	}
	return p
}

// initWorkspace initialises a workspace with an isolated XDG config.
func initWorkspace(t *testing.T) string {
	t.Helper()
	testSetXDG(t)
	workspace := filepath.Join(t.TempDir(), "Workspace")
	var out, errOut bytes.Buffer
	code := Execute([]string{"init", "--workspace", workspace}, strings.NewReader("y\n"), &out, &errOut)
	if code != 0 {
		t.Fatalf("init failed: code=%d stderr=%s", code, errOut.String())
	}
	return workspace
}

func createEditorShim(dir string) error {
	if runtime.GOOS == "windows" {
		codePath := filepath.Join(dir, "code.cmd")
		content := "@echo off\r\nexit /B 0\r\n"
		return os.WriteFile(codePath, []byte(content), 0o644)
	}

	codePath := filepath.Join(dir, "code")
	content := "#!/bin/sh\nexit 0\n"
	if err := os.WriteFile(codePath, []byte(content), 0o755); err != nil {
		return err
	}
	return nil
}
