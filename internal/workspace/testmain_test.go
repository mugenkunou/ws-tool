package workspace

import (
	"os"
	"testing"
)

// TestMain points HOME and the XDG dirs at a temp dir. Reset turns off the trash
// integration, which edits ~/.bashrc and ~/.zshrc and deletes ~/.local/bin/ws-trash-rm
// and the legacy trash state file, so a host run would otherwise change the real ones.
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
