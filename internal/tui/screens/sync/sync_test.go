package sync

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mugenkunou/ws-tool/internal/mirror"
	"github.com/mugenkunou/ws-tool/internal/tui/env"
	"github.com/mugenkunou/ws-tool/internal/tui/nav"
	"github.com/mugenkunou/ws-tool/internal/workspace"
	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

var now = time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)

func card(t *testing.T, m LoadedMsg) nav.Card {
	t.Helper()
	s, c, ok := m.Card()
	if !ok || s != nav.ScreenSync {
		t.Fatalf("Card() = %v, _, %v", s, ok)
	}
	return c
}

func TestCardNotConfiguredIsAMutedLineNotAnIssue(t *testing.T) {
	c := card(t, LoadedMsg{})
	if len(c.Issues) != 0 || c.Err != nil || len(c.Lines) != 1 || !strings.Contains(c.Lines[0], "no mirror set up") {
		t.Errorf("card = %+v", c)
	}
}

func TestCardErrorIsCardErr(t *testing.T) {
	err := errors.New("rsync is not installed")
	c := card(t, LoadedMsg{Configured: true, Err: err})
	if c.Err != err || len(c.Issues) != 0 {
		t.Errorf("card = %+v", c)
	}
}

func TestCardBodyAndIssues(t *testing.T) {
	last := &mirror.LastPush{Stamp: mirror.Stamp{Time: now.Add(-3 * time.Hour)}}
	for _, c := range []struct {
		name   string
		msg    LoadedMsg
		lines  []string
		issues int
	}{
		{"current", LoadedMsg{At: now, Configured: true, Status: mirror.Status{Stamp: mirror.StampOK, LastPush: last}},
			[]string{"last push 3h ago", "mirror up to date"}, 0},
		{"pending", LoadedMsg{At: now, Configured: true, Status: mirror.Status{Stamp: mirror.StampOK, LastPush: last, PendingChanges: 4, PendingDeletes: 2}},
			[]string{"last push 3h ago", "4 to copy · 2 to delete"}, 0},
		{"first", LoadedMsg{At: now, Configured: true, Status: mirror.Status{Stamp: mirror.StampFirst, PendingChanges: 9}},
			[]string{"never pushed from this machine", "9 to copy · 0 to delete"}, 0},
		{"wiped", LoadedMsg{At: now, Configured: true, Status: mirror.Status{Stamp: mirror.StampWiped, LastPush: last}}, nil, 1},
		{"foreign", LoadedMsg{At: now, Configured: true, Status: mirror.Status{Stamp: mirror.StampForeign, LastPush: last}}, nil, 1},
		{"guard", LoadedMsg{At: now, Configured: true, Status: mirror.Status{Stamp: mirror.StampOK, LastPush: last, PendingDeletes: 30},
			Questions: []mirror.Question{{Kind: mirror.AskGuard}}}, nil, 1},
		{"foreign and guard", LoadedMsg{At: now, Configured: true, Status: mirror.Status{Stamp: mirror.StampForeign, LastPush: last},
			Questions: []mirror.Question{{Kind: mirror.AskForeign}, {Kind: mirror.AskGuard}}}, nil, 2},
	} {
		t.Run(c.name, func(t *testing.T) {
			card := card(t, c.msg)
			if len(card.Issues) != c.issues {
				t.Errorf("issues = %+v, want %d", card.Issues, c.issues)
			}
			for _, is := range card.Issues {
				if is.Sev != nav.SevWarn || is.Screen != nav.ScreenSync {
					t.Errorf("issue %+v should be a warning on Sync", is)
				}
			}
			for i, want := range c.lines {
				if i >= len(card.Lines) || !strings.Contains(card.Lines[i], want) {
					t.Errorf("lines = %q, want %q at %d", card.Lines, want, i)
				}
			}
		})
	}
}

// TestLoadWithoutATargetIsNotAnError: the load reads ws.json and, with no
// sync.mirror.target, reports "not configured" without needing rsync.
func TestLoadWithoutATargetIsNotAnError(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)
	cfg := wsconfig.Default()
	path := dir + "/ws.json"
	if err := wsconfig.Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	e := env.Env{Paths: workspace.Paths{Workspace: dir, Config: path}, Config: cfg, Clock: func() time.Time { return now }}
	msg, ok := Load(e)().(LoadedMsg)
	if !ok || msg.Err != nil || msg.Configured || !msg.At.Equal(now) {
		t.Fatalf("Load = %+v", msg)
	}
}

func TestLoadReportsAMissingConfig(t *testing.T) {
	e := env.Env{Paths: workspace.Paths{Config: t.TempDir() + "/missing.json"}}
	if msg := Load(e)().(LoadedMsg); msg.Err == nil {
		t.Errorf("Load = %+v, want an error", msg)
	}
}

// TestRefreshReloadsAndLoadedFillsTheList: a refresh sends a load; a LoadedMsg
// then fills the list.
func TestRefreshReloadsAndLoadedFillsTheList(t *testing.T) {
	m := New(env.Env{})
	next, cmd := m.Update(nav.RefreshMsg{})
	if cmd == nil {
		t.Fatal("refresh sent no load")
	}
	next, _ = next.Update(nav.ResizeMsg{Width: 100, Height: 30})
	next, _ = next.Update(LoadedMsg{Configured: true, Changes: []Change{{Path: "a", Action: mirror.ActionCopy}, {Path: "b", Action: mirror.ActionDelete}}})
	if v := next.View(); !strings.Contains(v, "delete") || !strings.Contains(v, "copy") {
		t.Errorf("rows missing:\n%s", v)
	}
}
