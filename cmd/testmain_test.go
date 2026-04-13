package cmd

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestMain(m *testing.M) {
	shimDir, err := os.MkdirTemp("", "ws-tool-test-bin-")
	if err == nil {
		if createEditorShim(shimDir) == nil {
			_ = os.Setenv("PATH", shimDir+string(os.PathListSeparator)+os.Getenv("PATH"))
		}
		defer os.RemoveAll(shimDir)
	}

	os.Exit(m.Run())
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