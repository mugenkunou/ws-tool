package versions

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mugenkunou/ws-tool/internal/agenttmux"
	"github.com/mugenkunou/ws-tool/internal/agenttmux/agenttmuxtest"
)

func TestMain(m *testing.M) {
	cleanup := agenttmuxtest.Hide() // agent-tmux off PATH, WS_NO_VERSION_CHECK=1
	code := m.Run()
	cleanup()
	os.Exit(code)
}

// env isolates the cache, enables the check and forbids forking.
func env(t *testing.T) (spawns *atomic.Int32) {
	t.Helper()
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv(agenttmux.NoCheckEnv, "")
	spawns = new(atomic.Int32)
	t.Cleanup(SetStarter(func(string) error { spawns.Add(1); return nil }))
	return spawns
}

func server(t *testing.T, tag string, status int) *atomic.Int32 {
	t.Helper()
	hits := new(atomic.Int32)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if status != 200 {
			http.Error(w, "nope", status)
			return
		}
		w.Write([]byte(`{"tag_name":"` + tag + `"}`))
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(SetWSReleaseURL(srv.URL))
	return hits
}

func fakeCheck(t *testing.T, out string, err error) {
	t.Helper()
	t.Cleanup(agenttmux.SetCheckRunner(func(context.Context) ([]byte, error) { return []byte(out), err }))
}

func TestStateOf(t *testing.T) {
	cases := []struct {
		inst, latest string
		want         State
	}{
		{"v0.1.0", "v0.2.0", StateOutdated},
		{"v0.2.0", "v0.2.0", StateCurrent},
		{"v0.3.0", "v0.2.0", StateCurrent},
		{"dev", "v0.2.0", StateDev},
		{"dev", "", StateDev},
		{"abc123", "v0.2.0", StateDev},
		{"v0.1.0", "", StateUnknown},
		{"v0.1.0", "nightly", StateUnknown},
		{"", "v0.2.0", StateNotInstalled},
	}
	for _, c := range cases {
		if got := stateOf(c.inst, c.latest); got != c.want {
			t.Errorf("stateOf(%q,%q) = %s, want %s", c.inst, c.latest, got, c.want)
		}
	}
}

func TestCachedEmpty(t *testing.T) {
	env(t)
	hits := server(t, "v9.9.9", 200)
	agenttmuxtest.Fake(t, `echo '{"version":"v0.1.0"}'`)
	r := Cached(context.Background(), "v0.3.0")
	if r.Known() || r.Outdated() || r.WS.State != StateUnknown || r.AgentTmux.State != StateUnknown || r.AgentTmux.Installed != "v0.1.0" {
		t.Errorf("%+v", r)
	}
	if hits.Load() != 0 {
		t.Error("Cached used the network")
	}
}

func TestCachedMissingAgentTmux(t *testing.T) {
	env(t)
	agenttmuxtest.Missing(t)
	r := Cached(context.Background(), "dev")
	if r.AgentTmux.State != StateNotInstalled || r.AgentTmux.Installed != "" || r.WS.State != StateDev {
		t.Errorf("%+v", r)
	}
}

func TestRefreshThenCached(t *testing.T) {
	env(t)
	hits := server(t, "v0.4.0", 200)
	fakeCheck(t, `{"version":"v0.1.0","latest":"v0.2.0","status":"outdated"}`, nil)
	agenttmuxtest.Fake(t, `echo '{"version":"v0.1.0"}'`)

	// WS_NO_VERSION_CHECK is set in TestMain's env; Refresh ignores it.
	t.Setenv(agenttmux.NoCheckEnv, "1")
	r := Refresh(context.Background(), "v0.3.0")
	if !r.Known() || !r.Outdated() || r.WS.Latest != "v0.4.0" || r.WS.State != StateOutdated ||
		r.AgentTmux.Latest != "v0.2.0" || r.AgentTmux.State != StateOutdated {
		t.Errorf("refresh: %+v", r)
	}
	if hits.Load() != 1 {
		t.Errorf("hits %d", hits.Load())
	}

	c := Cached(context.Background(), "v0.3.0")
	if c.WS != r.WS || c.AgentTmux != r.AgentTmux || c.CheckedAt.IsZero() {
		t.Errorf("cached %+v != refreshed %+v", c, r)
	}
	// A newer installed ws than the cached latest is current.
	if c := Cached(context.Background(), "v0.5.0"); c.WS.State != StateCurrent {
		t.Errorf("newer than latest: %+v", c.WS)
	}
	// A dev build keeps the latest but is never outdated.
	if c := Cached(context.Background(), "dev"); c.WS.State != StateDev || c.WS.Latest != "v0.4.0" {
		t.Errorf("dev: %+v", c.WS)
	}
	if hits.Load() != 1 {
		t.Error("Cached used the network")
	}
}

func TestRefreshFailureKeepsLatestAndBacksOff(t *testing.T) {
	spawns := env(t)
	server(t, "v0.4.0", 200)
	fakeCheck(t, `{"version":"v0.1.0","latest":"v0.1.0","status":"current"}`, nil)
	agenttmuxtest.Fake(t, `echo '{"version":"v0.1.0"}'`)
	Refresh(context.Background(), "v0.3.0")

	// The next lookup fails: the old latest stays, the error shows.
	server(t, "", 500)
	r := Refresh(context.Background(), "v0.3.0")
	if r.WS.Latest != "v0.4.0" || r.WS.Error == "" || r.WS.State != StateOutdated {
		t.Errorf("%+v", r.WS)
	}
	if Stale() {
		t.Error("stale right after an attempt")
	}
	if SpawnRefresh(); spawns.Load() != 0 {
		t.Error("spawned while fresh")
	}
}

func TestRefreshFailureOnly(t *testing.T) {
	spawns := env(t)
	server(t, "", 500)
	fakeCheck(t, `{"version":"v0.1.0","latest":"","status":"unknown","error":"timeout"}`, nil)
	agenttmuxtest.Fake(t, `echo '{"version":"v0.1.0"}'`)
	r := Refresh(context.Background(), "v0.3.0")
	if r.Known() || r.WS.State != StateUnknown || r.WS.Error == "" || r.AgentTmux.Error != "timeout" {
		t.Errorf("%+v", r)
	}
	// Nothing cached yet, but the attempt backs off for an hour.
	if Stale() {
		t.Error("failure did not back off")
	}
	_ = spawns
}

func TestRefreshAgentTmuxTooOldAndMissing(t *testing.T) {
	env(t)
	server(t, "v0.4.0", 200)
	fakeCheck(t, ``, errors.New("unknown flag"))
	agenttmuxtest.Fake(t, `echo '{"version":"v0.0.9"}'`)
	r := Refresh(context.Background(), "v0.4.0")
	if r.AgentTmux.State != StateTooOld || r.AgentTmux.Installed != "v0.0.9" {
		t.Errorf("%+v", r.AgentTmux)
	}
	if c := Cached(context.Background(), "v0.4.0"); c.AgentTmux.State != StateTooOld {
		t.Errorf("cached too_old: %+v", c.AgentTmux)
	}
	// Upgrading agent-tmux clears too_old without a refresh.
	agenttmuxtest.Fake(t, `echo '{"version":"v0.2.0"}'`)
	if c := Cached(context.Background(), "v0.4.0"); c.AgentTmux.State == StateTooOld {
		t.Errorf("stale too_old: %+v", c.AgentTmux)
	}

	fakeCheck(t, ``, agenttmux.ErrMissing)
	agenttmuxtest.Missing(t)
	if r := Refresh(context.Background(), "v0.4.0"); r.AgentTmux.State != StateNotInstalled {
		t.Errorf("%+v", r.AgentTmux)
	}
}

func TestStale(t *testing.T) {
	spawns := env(t)
	if !Stale() {
		t.Error("empty cache is stale")
	}
	t.Setenv(agenttmux.NoCheckEnv, "1")
	if Stale() {
		t.Error("WS_NO_VERSION_CHECK must silence Stale")
	}
	t.Setenv(agenttmux.NoCheckEnv, "")

	now := time.Now()
	cases := []struct {
		name               string
		checked, attempted time.Time
		want               bool
	}{
		{"fresh", now.Add(-time.Hour * 2), now.Add(-time.Hour * 2), false},
		{"old, never retried", now.Add(-25 * time.Hour), now.Add(-25 * time.Hour), true},
		{"old, retried recently", now.Add(-25 * time.Hour), now.Add(-10 * time.Minute), false},
		{"old, retry over an hour ago", now.Add(-25 * time.Hour), now.Add(-2 * time.Hour), true},
	}
	for _, c := range cases {
		writeCache(cacheFile{CheckedAt: c.checked, AttemptedAt: c.attempted})
		if got := Stale(); got != c.want {
			t.Errorf("%s: Stale = %v", c.name, got)
		}
	}

	// SpawnRefresh starts once, then the claimed attempt holds it back.
	writeCache(cacheFile{})
	SpawnRefresh()
	SpawnRefresh()
	if spawns.Load() != 1 {
		t.Errorf("spawned %d times, want 1", spawns.Load())
	}
}
