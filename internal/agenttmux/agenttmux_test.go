package agenttmux_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mugenkunou/ws-tool/internal/agenttmux"
	"github.com/mugenkunou/ws-tool/internal/agenttmux/agenttmuxtest"
)

func TestMain(m *testing.M) {
	cleanup := agenttmuxtest.Hide()
	code := m.Run()
	cleanup()
	os.Exit(code)
}

const lsJSON = `{"sessions":[
 {"name":"web","id":"web-2026-10-29-1402","state":"live","recorder":"running","owner":"","clients":1,
  "command":"bash","path":"/home/u/src","host":"box","started_at":"2026-10-29T08:32:11Z","ended_at":"",
  "history_starts_at":"2026-10-29T08:32:11Z","bytes":1048576},
 {"name":"old","id":"old-2026-10-01-0900","state":"ended","recorder":"","owner":"","clients":0,
  "command":"","path":"","host":"box","started_at":"2026-10-01T09:00:00Z","ended_at":"2026-10-01T10:00:00Z",
  "history_starts_at":"2026-10-01T09:00:00Z","bytes":2048}
]}`

func TestLs(t *testing.T) {
	log := agenttmuxtest.Fake(t, "cat <<'EOF'\n"+lsJSON+"\nEOF")
	got, err := agenttmux.Ls(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "web" || !got[0].Live() || got[0].Recorder != "running" || got[0].Bytes != 1<<20 {
		t.Fatalf("ls = %+v", got)
	}
	if got[1].Live() || got[1].Ref() != "old-2026-10-01-0900" || got[0].Ref() != "web" {
		t.Errorf("refs: %q %q", got[0].Ref(), got[1].Ref())
	}
	if calls := agenttmuxtest.Calls(t, log); len(calls) != 1 || !slices.Equal(calls[0], []string{"ls", "--json"}) {
		t.Errorf("argv = %v", calls)
	}
}

func TestMissing(t *testing.T) {
	agenttmuxtest.Missing(t)
	if _, err := agenttmux.Ls(context.Background()); !errors.Is(err, agenttmux.ErrMissing) {
		t.Errorf("ls without agent-tmux: %v", err)
	}
	if got := agenttmux.Complete([]string{"ls", ""}); got != nil {
		t.Errorf("complete without agent-tmux = %v", got)
	}
}

func TestErrorCarriesAgentTmuxsMessage(t *testing.T) {
	agenttmuxtest.Fake(t, "echo 'web is live; kill it first (agent-tmux kill web)' >&2; exit 1")
	_, err := agenttmux.Output(context.Background(), "rm", "web")
	if err == nil || err.Error() != "web is live; kill it first (agent-tmux kill web)" {
		t.Errorf("err = %v", err)
	}
}

func TestGrep(t *testing.T) {
	log := agenttmuxtest.Fake(t, `echo '{"matches":[{"name":"web","id":"web-1","line":12,"text":"kube ctl get pods"}],"truncated":true}'`)
	res, err := agenttmux.Grep(context.Background(), "kube ctl", 50)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Truncated || len(res.Matches) != 1 || res.Matches[0].Line != 12 || res.Matches[0].ID != "web-1" {
		t.Errorf("grep = %+v", res)
	}
	if _, err := agenttmux.Grep(context.Background(), "-rf", 0); err != nil {
		t.Fatal(err)
	}
	calls := agenttmuxtest.Calls(t, log)
	if !slices.Equal(calls[0], []string{"grep", "--json", "--limit", "50", "kube ctl"}) {
		t.Errorf("argv = %v", calls[0])
	}
	if !slices.Equal(calls[1], []string{"grep", "--json", "--", "-rf"}) {
		t.Errorf("a text with a leading dash: argv = %v", calls[1])
	}
}

func TestComplete(t *testing.T) {
	log := agenttmuxtest.Fake(t, `printf 'web\tlive session\nweb-2026-10-29-1402\n\nold\r\n'`)
	got := agenttmux.Complete([]string{"show", "w"})
	if !slices.Equal(got, []string{"web", "web-2026-10-29-1402", "old"}) {
		t.Errorf("candidates = %q", got)
	}
	if calls := agenttmuxtest.Calls(t, log); !slices.Equal(calls[0], []string{"__complete", "show", "w"}) {
		t.Errorf("argv = %v", calls[0])
	}
	agenttmuxtest.Fake(t, "echo web; exit 3")
	if got := agenttmux.Complete([]string{""}); got != nil {
		t.Errorf("a failed __complete offers nothing, got %v", got)
	}
}

func TestOlder(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"v0.1.0", "v0.2.0", true},
		{"v0.1.0", "v0.1.0", false},
		{"v0.2.0", "v0.1.9", false},
		{"v0.9.0", "v0.10.0", true},
		{"v1.0.0-rc.1", "v1.0.0", true},
		{"v1.0.0", "v1.0.0-rc.1", false},
		{"v0.1.1-0.20261029100000-abc1234", "v0.1.0", false}, // a go install pseudo-version past the tag
		{"0.1.0", "v0.2.0", true},
		{"dev", "v0.2.0", false},
		{"v0.1.0", "latest", false},
		{"(devel)", "v9.9.9", false},
	} {
		if got := agenttmux.Older(c.a, c.b); got != c.want {
			t.Errorf("Older(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

// releaseServer answers like GitHub: 200 with an ETag, or 304 when the
// request's If-None-Match is that ETag. It counts the requests.
func releaseServer(t *testing.T, tag string) (*httptest.Server, *atomic.Int32, *atomic.Value) {
	t.Helper()
	var hits atomic.Int32
	var lastINM atomic.Value
	lastINM.Store("")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		inm := r.Header.Get("If-None-Match")
		lastINM.Store(inm)
		if inm == `"etag-`+tag+`"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"etag-`+tag+`"`)
		_ = json.NewEncoder(w).Encode(map[string]string{"tag_name": tag, "name": "ignored"})
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(agenttmux.SetLatestURL(srv.URL))
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv(agenttmux.NoCheckEnv, "")
	return srv, &hits, &lastINM
}

func TestLatestCachesAndRevalidates(t *testing.T) {
	_, hits, inm := releaseServer(t, "v0.2.0")
	ctx := context.Background()

	tag, err := agenttmux.Latest(ctx)
	if err != nil || tag != "v0.2.0" || hits.Load() != 1 {
		t.Fatalf("first check: %q %v, %d requests", tag, err, hits.Load())
	}
	if tag, _ := agenttmux.Latest(ctx); tag != "v0.2.0" || hits.Load() != 1 {
		t.Fatalf("a fresh cache asks nobody: %q, %d requests", tag, hits.Load())
	}

	// A day later the cache is stale: the check sends the ETag and a 304 keeps the tag.
	path, _ := agenttmux.CachePath()
	if !strings.HasSuffix(path, "/ws/agent-tmux-release.json") {
		t.Errorf("cache path %s", path)
	}
	var c map[string]any
	data, _ := os.ReadFile(path)
	_ = json.Unmarshal(data, &c)
	c["checked_at"] = time.Now().Add(-25 * time.Hour).Format(time.RFC3339)
	data, _ = json.Marshal(c)
	_ = os.WriteFile(path, data, 0o644)

	tag, err = agenttmux.Latest(ctx)
	if err != nil || tag != "v0.2.0" || hits.Load() != 2 || inm.Load() != `"etag-v0.2.0"` {
		t.Fatalf("revalidate: %q %v, %d requests, If-None-Match %q", tag, err, hits.Load(), inm.Load())
	}
	if _, _ = agenttmux.Latest(ctx); hits.Load() != 2 {
		t.Error("a 304 renews the cache for another day")
	}
}

func TestLatestFailsQuietly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rate limited", http.StatusForbidden)
	}))
	defer srv.Close()
	t.Cleanup(agenttmux.SetLatestURL(srv.URL))
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	if _, err := agenttmux.Latest(context.Background()); err == nil {
		t.Error("a 403 is an error for Latest")
	}
	agenttmuxtest.Fake(t, `echo '{"version":"v0.1.0","commit":"","date":""}'`)
	t.Setenv(agenttmux.NoCheckEnv, "")
	if n, ok := agenttmux.CheckNudge(context.Background()); ok {
		t.Errorf("a failed check is no nudge, got %+v", n)
	}
}

func TestCheckNudge(t *testing.T) {
	_, hits, _ := releaseServer(t, "v0.2.0")
	ctx := context.Background()

	agenttmuxtest.Fake(t, `echo '{"version":"v0.1.0","commit":"abc1234","date":"2026-10-29T10:00:00Z"}'`)
	n, ok := agenttmux.CheckNudge(ctx)
	if !ok || n.Installed != "v0.1.0" || n.Latest != "v0.2.0" {
		t.Fatalf("nudge = %+v %v", n, ok)
	}
	want := append([]string{"agent-tmux v0.2.0 is available"}, agenttmux.InstallLines()...)
	if !slices.Equal(n.Lines(), want) || !strings.Contains(strings.Join(n.Lines(), "\n"), "releases/latest/download/agent-tmux-$(uname -s") {
		t.Errorf("lines = %q", n.Lines())
	}

	agenttmuxtest.Fake(t, `echo '{"version":"v0.2.0"}'`)
	if _, ok := agenttmux.CheckNudge(ctx); ok {
		t.Error("the latest release installed: no nudge")
	}

	before := hits.Load()
	agenttmuxtest.Fake(t, `echo '{"version":"dev"}'`)
	if _, ok := agenttmux.CheckNudge(ctx); ok || hits.Load() != before {
		t.Error("a dev build: no nudge and no request")
	}

	agenttmuxtest.Fake(t, `echo '{"version":"v0.1.0"}'`)
	t.Setenv(agenttmux.NoCheckEnv, "1")
	if _, ok := agenttmux.CheckNudge(ctx); ok {
		t.Error("WS_NO_VERSION_CHECK=1 turns it off")
	}

	agenttmuxtest.Missing(t)
	t.Setenv(agenttmux.NoCheckEnv, "")
	if _, ok := agenttmux.CheckNudge(ctx); ok {
		t.Error("agent-tmux missing: no nudge")
	}
}
