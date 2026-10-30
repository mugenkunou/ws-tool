package app

import (
	"strings"
	"testing"

	"github.com/mugenkunou/ws-tool/internal/pass"
	"github.com/mugenkunou/ws-tool/internal/tui/nav"
	passscreen "github.com/mugenkunou/ws-tool/internal/tui/screens/pass"
	"github.com/mugenkunou/ws-tool/internal/tui/tuitest"
)

// TestSnapshotPassAndCredentials captures the two screens split out of Secrets.
func TestSnapshotPassAndCredentials(t *testing.T) {
	for _, c := range []struct {
		name   string
		screen nav.Screen
	}{
		{"pass", nav.ScreenPass},
		{"credential", nav.ScreenCredential},
	} {
		t.Run(c.name, func(t *testing.T) {
			tuitest.Golden(t, c.name, view(send(loaded(t, 100, 30), jump(c.screen))))
		})
	}
}

// TestPassPinnedRowOffersTheBackupPush: with commits not pushed or changes not
// committed, the first row offers the push; enter on it opens the same checklist
// as `.` then `p`. A clean store has no row.
func TestPassPinnedRowOffersTheBackupPush(t *testing.T) {
	health := pass.PassHealth{Installed: true, GPGAvailable: true, Initialized: true, GitBacked: true, GitRemote: true, EntryCount: 7, StorePath: "/home/tester/.password-store"}
	m := send(loaded(t, 100, 30), passscreen.LoadedMsg{Pass: health, RemoteURL: "git@example.com:me/pass.git", Ahead: 2, Uncommitted: 1}, jump(nav.ScreenPass))
	v := tuitest.Plain(view(m))
	if !strings.Contains(v, "▸ Push the pass backup: 1 uncommitted change, 2 commits not pushed") {
		t.Fatalf("no pinned push row:\n%s", v)
	}
	m = send(m, press("up"))
	m, cmd := sendRun(m, press("enter"))
	m = pump(t, m, cmd)
	if v := tuitest.Plain(view(m)); !strings.Contains(v, "Push the pass store") {
		t.Errorf("enter on the pinned row opens the push checklist:\n%s", v)
	}

	clean := send(loaded(t, 100, 30), passscreen.LoadedMsg{Pass: health, RemoteURL: "git@example.com:me/pass.git"}, jump(nav.ScreenPass))
	if v := tuitest.Plain(view(clean)); strings.Contains(v, "▸") {
		t.Errorf("a clean store has no pinned row:\n%s", v)
	}
}
