package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

)

func TestMain(m *testing.M) {
	// The editor resolves from $VISUAL and $EDITOR; the host's must not leak in.
	_ = os.Unsetenv("VISUAL")
	_ = os.Unsetenv("EDITOR")

	shimDir, err := os.MkdirTemp("", "ws-tool-test-bin-")
	if err == nil {
		if createEditorShim(shimDir) == nil {
			_ = os.Setenv("PATH", shimDir+string(os.PathListSeparator)+os.Getenv("PATH"))
		}
		defer os.RemoveAll(shimDir)
	}

	// Redirect XDG config, state and data to a temp dir so tests never use the
	// real ~/.config, ~/.local/state or ~/.local/share/Trash (a host test run once
	// deleted real trash state). Tests that need their own machine set these again.
	xdgDir, err := os.MkdirTemp("", "ws-tool-test-xdg-")
	if err == nil {
		_ = os.Setenv("XDG_CONFIG_HOME", xdgDir)
		_ = os.Setenv("XDG_STATE_HOME", xdgDir)
		_ = os.Setenv("XDG_DATA_HOME", xdgDir)
		defer os.RemoveAll(xdgDir)
	}

	// Isolate git global config so tests never clobber the real ~/.gitconfig.
	// This prevents credential-helper setup (and any other global git config
	// mutation) from overwriting the user's real configuration.
	gitCfgDir, err := os.MkdirTemp("", "ws-tool-test-gitcfg-")
	if err == nil {
		globalCfg := filepath.Join(gitCfgDir, "config")
		_ = os.WriteFile(globalCfg, []byte("[core]\n"), 0o644)
		_ = os.Setenv("GIT_CONFIG_GLOBAL", globalCfg)
		defer os.RemoveAll(gitCfgDir)
	}

	// git must not look above the repository root for a repository: in a git
	// worktree mounted into Docker the .git file there points at a host path
	// that does not exist, and every git command run outside a repo then dies.
	if wd, err := os.Getwd(); err == nil {
		_ = os.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(wd))
	}

	os.Exit(m.Run())
}

// testSetXDG sets XDG_CONFIG_HOME to a test-scoped temp dir for isolation.
// Must be called at the start of any test that calls ws init.
// It also sets PASSWORD_STORE_DIR to an isolated temp dir so that the real
// ~/.password-store is never included in repo operations during tests.
// WS_CRONTAB_FILE is set to an empty temp file so cron commands never read
// or write the real system crontab.
func testSetXDG(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("PASSWORD_STORE_DIR", t.TempDir())

	cronFile := filepath.Join(t.TempDir(), "crontab")
	if err := os.WriteFile(cronFile, []byte{}, 0o600); err != nil {
		t.Fatalf("testSetXDG: create crontab file: %v", err)
	}
	t.Setenv("WS_CRONTAB_FILE", cronFile)
}

// testConfigPath returns a per-test config path inside a test-scoped temp dir.
func testConfigPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "ws-tool", "config.json")
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

	// Both the editors ws may start: vim is the default (spec §4.10), and code
	// is what a user who set `editor` to it gets. Neither may open a real one.
	content := "#!/bin/sh\nexit 0\n"
	for _, name := range []string{"code", "vim"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o755); err != nil {
			return err
		}
	}
	return nil
}
