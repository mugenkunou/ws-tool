package app

import (
	"os"
	"testing"
	"testing/fstest"

	"github.com/mugenkunou/ws-tool/internal/skill"
)

func TestMain(m *testing.M) {
	// Tests see only the skills they write, not the ones embedded in the binary.
	skill.BuiltinFS = fstest.MapFS{}

	// The skills screen refreshes the built-ins' managed copy under
	// $XDG_DATA_HOME; a test must never write the real one.
	if dir, err := os.MkdirTemp("", "ws-tui-test-data-"); err == nil {
		_ = os.Setenv("XDG_DATA_HOME", dir)
		code := m.Run()
		os.RemoveAll(dir)
		os.Exit(code)
	}
	os.Exit(m.Run())
}
