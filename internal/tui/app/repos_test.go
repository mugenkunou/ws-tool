package app

import (
	"strings"
	"testing"

	"github.com/mugenkunou/ws-tool/internal/repo"
	"github.com/mugenkunou/ws-tool/internal/tui/nav"
	"github.com/mugenkunou/ws-tool/internal/tui/screens/repos"
	"github.com/mugenkunou/ws-tool/internal/tui/tuitest"
)

// reposPrepared is what a pull reads after its fetch: one fast-forward and
// one diverged repo (2 local, 1 remote commit).
func reposPrepared() repos.PreparedMsg {
	return repos.PreparedMsg{Kind: repos.KindPull, Statuses: []repo.RepoStatus{
		{Path: ws + "/Projects/api", Branch: "main", HasUpstream: true, Behind: 3},
		{Path: ws + "/Projects/web", Branch: "feature/login", HasUpstream: true, Ahead: 2, Behind: 1},
	}}
}

// TestReposDivergedChooser: the chooser opens before the checklist, in the
// frame with the footer, and a letter answers it.
func TestReposDivergedChooser(t *testing.T) {
	m := send(loaded(t, 100, 30), press("2"), reposPrepared())
	tuitest.Golden(t, "repos_pull_diverged", view(m))

	m, cmd := sendRun(m, press("r")) // the chooser answers through a message
	m = pump(t, m, cmd)
	tuitest.Golden(t, "repos_pull_checklist", view(m))
}

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
