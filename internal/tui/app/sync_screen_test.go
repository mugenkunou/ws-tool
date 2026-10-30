package app

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/mugenkunou/ws-tool/internal/mirror"
	"github.com/mugenkunou/ws-tool/internal/tui/nav"
	syncscreen "github.com/mugenkunou/ws-tool/internal/tui/screens/sync"
	"github.com/mugenkunou/ws-tool/internal/tui/tuitest"
)

// syncApp is the app on the Sync screen with msg as its load result, or the
// fixture's when msg is nil.
func syncApp(t *testing.T, msg tea.Msg) Model {
	t.Helper()
	m := loaded(t, 100, 30)
	if msg != nil {
		m = send(m, msg)
	}
	return send(m, jump(nav.ScreenSync))
}

// TestSyncCommands: push is the one menu command (restore stays on the CLI) and
// the screen declares no keys.
func TestSyncCommands(t *testing.T) {
	m := loaded(t, 100, 30)
	if got, want := commandNames(m, nav.ScreenSync), []string{"push"}; !slices.Equal(got, want) {
		t.Errorf("sync commands = %v, want %v", got, want)
	}
	if got := declaredScopes(t, m, nav.ScreenSync); len(got) != 0 {
		t.Errorf("sync declares keys %v; pushing is a command", got)
	}
}

// TestSyncPushAsksFirstWhenTheCLIWould: a foreign stamp makes the push action
// start unchecked, with the question in the note; a clean mirror starts it
// checked.
func TestSyncPushAsksFirstWhenTheCLIWould(t *testing.T) {
	m := menuKeys(t, syncApp(t, nil), "p")
	v := tuitest.Plain(view(m))
	for _, want := range []string{"[ ] Push to /home/tester/MEGA/ws-mirror: 3 files to copy, 1 file to delete", "was last written by another push", "Tick the push"} {
		if !strings.Contains(v, want) {
			t.Errorf("checklist lacks %q:\n%s", want, v)
		}
	}
	tuitest.Golden(t, "sync_push_checklist", view(m))

	clean := syncApp(t, syncscreen.LoadedMsg{Configured: true, Options: mirror.Options{Target: "/m"},
		Status:  mirror.Status{Target: "/m", Stamp: mirror.StampOK, PendingChanges: 1, Files: 5},
		Changes: []syncscreen.Change{{Path: "a.md", Action: mirror.ActionCopy}}})
	v = tuitest.Plain(view(menuKeys(t, clean, "p")))
	if !strings.Contains(v, "[x] Push to /m: 1 file to copy, 0 files to delete") || strings.Contains(v, "Tick the push") {
		t.Errorf("a clean push should start checked:\n%s", v)
	}
}

// TestSyncPushWhenThereIsNothingToDo: an up-to-date mirror still gets the
// checklist, as `ws sync push` still runs and records the push; a workspace
// without a target says how to set one.
func TestSyncPushWhenThereIsNothingToDo(t *testing.T) {
	current := syncApp(t, syncscreen.LoadedMsg{Configured: true, Options: mirror.Options{Target: "/m"}, Status: mirror.Status{Target: "/m", Stamp: mirror.StampOK}})
	if v := tuitest.Plain(view(menuKeys(t, current, "p"))); !strings.Contains(v, "[x] Push to /m") {
		t.Errorf("an up-to-date mirror should still offer the push:\n%s", v)
	}
	m := menuKeys(t, syncApp(t, syncscreen.LoadedMsg{}), "p")
	if got := lastNotice(m); !strings.Contains(got, "Sync is not set up") {
		t.Errorf("unconfigured: notice = %q", got)
	}
}

// TestSyncNotConfigured: the empty state says how to set the target, taking
// its keys from the bindings.
func TestSyncNotConfigured(t *testing.T) {
	m := syncApp(t, syncscreen.LoadedMsg{})
	v := tuitest.Plain(view(m))
	if !strings.Contains(v, "sync.mirror.target") || !strings.Contains(v, "config edit") || !strings.Contains(v, "ctrl+r") {
		t.Errorf("empty state does not say how to set it up:\n%s", v)
	}
	tuitest.Golden(t, "sync_not_configured", view(m))
}

// TestSyncError: a failed dry run (rsync missing) shows the error and blocks a push.
func TestSyncError(t *testing.T) {
	m := syncApp(t, syncscreen.LoadedMsg{Configured: true, Options: mirror.Options{Target: "/m"},
		Err: errString("rsync is not installed: ws sync needs it on PATH")})
	if v := tuitest.Plain(view(m)); !strings.Contains(v, "rsync is not installed") {
		t.Errorf("error not shown:\n%s", v)
	}
	m = menuKeys(t, m, "p")
	if got := lastNotice(m); !strings.Contains(got, "Cannot push") {
		t.Errorf("notice = %q", got)
	}
}

type errString string

func (e errString) Error() string { return string(e) }

func TestSyncTextNamesNoKeys(t *testing.T) {
	assertNoHardcodedKeyNames(t, "sync/sync.go")
}

// TestSyncPinnedRowOffersThePush: with changes pending, the first row is the push;
// up from the first change focuses it and enter opens the same checklist as `.`
// then `p`. With nothing pending there is no row.
func TestSyncPinnedRowOffersThePush(t *testing.T) {
	m := syncApp(t, nil)
	v := tuitest.Plain(view(m))
	if !strings.Contains(v, "▸ Push to the mirror: 3 files to copy, 1 to delete") {
		t.Fatalf("no pinned push row:\n%s", v)
	}
	m = send(m, press("up"))
	m, cmd := sendRun(m, press("enter"))
	m = pump(t, m, cmd)
	if v := tuitest.Plain(view(m)); !strings.Contains(v, "Push to /home/tester/MEGA/ws-mirror") {
		t.Errorf("enter on the pinned row opens the push checklist:\n%s", v)
	}

	current := syncApp(t, syncscreen.LoadedMsg{Configured: true, Options: mirror.Options{Target: "/m"}, Status: mirror.Status{Target: "/m", Stamp: mirror.StampOK}})
	if v := tuitest.Plain(view(current)); strings.Contains(v, "▸") {
		t.Errorf("an up-to-date mirror has no pinned row:\n%s", v)
	}
}
