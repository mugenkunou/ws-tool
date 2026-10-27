package app

import (
	"strings"
	"testing"

	"github.com/mugenkunou/ws-tool/internal/tui/nav"
	"github.com/mugenkunou/ws-tool/internal/tui/tuitest"
)

// TestReposColonCommands: the command bar reaches the screen's own commands,
// from the Repos screen and as `<noun> <command>` from anywhere.
func TestReposColonCommands(t *testing.T) {
	m := runLineKeys(t, send(loaded(t, 100, 30), press("2")), "root ls")
	tuitest.Golden(t, "repos_root_ls", view(m))

	m = runLineKeys(t, loaded(t, 100, 30), "repo root ls")
	if m.active != nav.ScreenRepos || !strings.Contains(tuitest.Plain(view(m)), "Repo discovery roots") {
		t.Errorf(":repo root ls from the Dashboard should switch and run:\n%s", view(m))
	}
}
