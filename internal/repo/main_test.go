package repo

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain keeps every git command of these tests away from the real machine:
// the global config is a temp file, and there is no system config.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "ws-repo-test-")
	if err != nil {
		panic(err)
	}
	cfg := filepath.Join(dir, "gitconfig")
	if err := os.WriteFile(cfg, []byte("[core]\n"), 0o644); err != nil {
		panic(err)
	}
	os.Setenv("GIT_CONFIG_GLOBAL", cfg)
	os.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	os.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	os.Setenv("HOME", dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
