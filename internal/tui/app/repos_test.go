package app

import (
	"strings"
	"testing"

	"github.com/mugenkunou/ws-tool/internal/tui/nav"
	"github.com/mugenkunou/ws-tool/internal/tui/tuitest"
)

// TestReposMenuListsTheRoots: `.` then `l` shows the discovery roots.
func TestReposMenuListsTheRoots(t *testing.T) {
	m := menuKeys(t, send(loaded(t, 100, 30), press("2")), "l")
	tuitest.Golden(t, "repos_root_ls", view(m))
	if m.active != nav.ScreenRepos || !strings.Contains(tuitest.Plain(view(m)), "Repo discovery roots") {
		t.Errorf("the menu's root ls should show the roots:\n%s", view(m))
	}
}
