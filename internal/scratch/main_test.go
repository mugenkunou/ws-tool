package scratch

import (
	"os"
	"testing"
)

// TestMain gives every test in the package its own home and XDG directories. Code
// under test soft-deletes into the XDG trash and may edit shell rc files, and a
// host test run once deleted real trash state; nothing here may reach the user's
// real ~ or ~/.local/share/Trash.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "ws-tool-pkg-test-")
	if err != nil {
		panic(err)
	}
	for k, v := range map[string]string{
		"HOME":            dir,
		"XDG_DATA_HOME":   dir + "/data",
		"XDG_CONFIG_HOME": dir + "/config",
		"XDG_STATE_HOME":  dir + "/state",
	} {
		_ = os.Setenv(k, v)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}
