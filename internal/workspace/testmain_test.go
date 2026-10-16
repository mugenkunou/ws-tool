package workspace

import (
	"os"
	"testing"
)

// TestMain points HOME and the XDG dirs at a temp dir. Reset calls trash.Reset,
// which removes the trash state file under the state dir, so a host run would
// otherwise delete the real ~/.local/state/ws-tool/trash-setup.json.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "ws-tool-test-home-")
	if err != nil {
		panic(err)
	}
	for _, key := range []string{"HOME", "XDG_CONFIG_HOME", "XDG_STATE_HOME", "XDG_DATA_HOME"} {
		_ = os.Setenv(key, home)
	}
	code := m.Run()
	_ = os.RemoveAll(home)
	os.Exit(code)
}
