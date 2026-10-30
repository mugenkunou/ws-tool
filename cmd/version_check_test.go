package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mugenkunou/ws-tool/internal/agenttmux"
	"github.com/mugenkunou/ws-tool/internal/agenttmux/agenttmuxtest"
	"github.com/mugenkunou/ws-tool/internal/versions"
)

// releaseServer answers every request with tag (or status when it is not 200)
// and counts the requests.
func releaseServer(t *testing.T, tag string, status int) (url string, hits *atomic.Int32) {
	t.Helper()
	hits = new(atomic.Int32)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Header.Get("User-Agent") != "ws" {
			t.Errorf("User-Agent %q", r.Header.Get("User-Agent"))
		}
		if status != http.StatusOK {
			http.Error(w, "nope", status)
			return
		}
		w.Write([]byte(`{"tag_name":"` + tag + `"}`))
	}))
	t.Cleanup(srv.Close)
	return srv.URL, hits
}

// noSpawn makes any background refresh a counted no-op, so no test forks.
func noSpawn(t *testing.T) *atomic.Int32 {
	t.Helper()
	n := new(atomic.Int32)
	t.Cleanup(versions.SetStarter(func(string) error { n.Add(1); return nil }))
	return n
}

// setupCheck isolates the version cache, points the ws lookup at a server,
// sets the ws version and fakes agent-tmux's check (err makes the runner fail).
func setupCheck(t *testing.T, wsVersion, wsTag string, wsStatus int, at string, atErr error) *atomic.Int32 {
	t.Helper()
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	noSpawn(t)
	url, hits := releaseServer(t, wsTag, wsStatus)
	t.Cleanup(versions.SetWSReleaseURL(url))
	oldVer := appVersion
	appVersion = wsVersion
	t.Cleanup(func() { appVersion = oldVer })
	t.Cleanup(agenttmux.SetCheckRunner(func(context.Context) ([]byte, error) { return []byte(at), atErr }))
	// WS_NO_VERSION_CHECK is set by TestMain: --check must ignore it.
	return hits
}

func runVersionCmd(t *testing.T, args ...string) string {
	t.Helper()
	var out, errOut bytes.Buffer
	if code := Execute(append([]string{"version", "--no-color"}, args...), strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	return out.String()
}

func checkContains(t *testing.T, out string, want, not []string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Errorf("lacks %q:\n%s", w, out)
		}
	}
	for _, n := range not {
		if strings.Contains(out, n) {
			t.Errorf("has %q:\n%s", n, out)
		}
	}
}

func TestVersionCheckWS(t *testing.T) {
	cases := []struct {
		name, ver, tag string
		status         int
		want, not      []string
	}{
		{"current", "v0.3.0", "v0.3.0", 200, []string{"v0.3.0  (up to date)"}, []string{"update available", "dev build"}},
		{"outdated", "v0.3.0", "v0.4.0", 200, []string{"v0.3.0  (latest v0.4.0 \u2014 update available)"}, []string{"dev build"}},
		{"dev", "dev", "v0.4.0", 200, []string{"dev  (local build is a dev build; upstream release is v0.4.0)"}, []string{"update available"}},
		{"unparseable", "abc123", "v0.4.0", 200, []string{"local build is a dev build; upstream release is v0.4.0"}, []string{"update available"}},
		{"failed", "v0.3.0", "", 500, []string{"release check failed", "500"}, []string{"update available"}},
		{"dev failed", "dev", "", 500, []string{"local build is a dev build"}, []string{"update available", "upstream release is v"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			setupCheck(t, c.ver, c.tag, c.status, `{"version":"v0.1.0","latest":"v0.1.0","status":"current"}`, nil)
			checkContains(t, runVersionCmd(t, "--check"), c.want, c.not)
		})
	}
}

func TestVersionCheckAgentTmux(t *testing.T) {
	cases := []struct {
		name, json string
		err        error
		want, not  []string
		installed  string // what `agent-tmux version --json` says; default v0.1.0
	}{
		{"current", `{"version":"v0.1.0","latest":"v0.1.0","status":"current"}`, nil, []string{"v0.1.0  (up to date)"}, nil, ""},
		{"outdated", `{"version":"v0.1.0","latest":"v0.2.0","status":"outdated"}`, nil, []string{"(latest v0.2.0 \u2014 update available)", "mugenkunou/agent-tmux/releases/latest"}, nil, ""},
		{"dev", `{"version":"dev","latest":"v0.2.0","status":"dev"}`, nil, []string{"local build is a dev build; upstream release is v0.2.0"}, []string{"update available"}, "dev"},
		{"unknown", `{"version":"v0.1.0","latest":"","status":"unknown","error":"timeout"}`, nil, []string{"release check failed: timeout"}, []string{"update available"}, ""},
		{"too old (fails)", ``, errors.New("unknown flag: --check"), []string{"too old to check \u2014 upgrade", "mugenkunou/agent-tmux/releases/latest"}, []string{"update available"}, "v0.0.9"},
		{"too old (not json)", `v0.0.9`, nil, []string{"too old to check \u2014 upgrade"}, nil, "v0.0.9"},
		{"missing", ``, agenttmux.ErrMissing, []string{"not installed", "mugenkunou/agent-tmux/releases/latest"}, []string{"update available"}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			setupCheck(t, "v0.3.0", "v0.3.0", 200, c.json, c.err)
			if c.err == agenttmux.ErrMissing {
				agenttmuxtest.Missing(t)
			} else {
				inst := c.installed
				if inst == "" {
					inst = "v0.1.0"
				}
				agenttmuxtest.Fake(t, `echo '{"version":"`+inst+`"}'`) // the old `version --json`
			}
			out := runVersionCmd(t, "--check")
			checkContains(t, out, c.want, c.not)
			if strings.HasPrefix(c.name, "too old") && !strings.Contains(out, "v0.0.9") {
				t.Errorf("too old: the installed version is not shown:\n%s", out)
			}
			// The same layout comes back from the cache, with no lookup.
			checkContains(t, runVersionCmd(t), c.want, c.not)
		})
	}
}

// TestVersionCheckAgentTmuxScript runs a real fake agent-tmux script that
// predates --check: it exits non-zero on the unknown flag.
func TestVersionCheckAgentTmuxScript(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	noSpawn(t)
	url, _ := releaseServer(t, "v0.3.0", 200)
	t.Cleanup(versions.SetWSReleaseURL(url))
	oldVer := appVersion
	appVersion = "v0.3.0"
	t.Cleanup(func() { appVersion = oldVer })
	log := agenttmuxtest.Fake(t, `case "$*" in *--check*) echo "unknown flag: --check" >&2; exit 2;; esac
echo '{"version":"v0.0.9"}'`)
	out := runVersionCmd(t, "--check")
	if !strings.Contains(out, "v0.0.9") || !strings.Contains(out, "too old to check") {
		t.Errorf("output:\n%s", out)
	}
	if calls := agenttmuxtest.Calls(t, log); len(calls) == 0 || strings.Join(calls[0], " ") != "version --check --json" {
		t.Errorf("calls %v", calls)
	}
}

// TestVersionPlainNeverAsks: plain `ws version` reads the cache only, tells
// when it is empty, and asks for a background refresh.
func TestVersionPlainNeverAsks(t *testing.T) {
	hits := setupCheck(t, "v0.3.0", "v0.4.0", 200, `{"version":"v0.1.0","latest":"v0.2.0","status":"outdated"}`, nil)
	agenttmuxtest.Fake(t, `echo '{"version":"v0.1.0"}'`)
	t.Setenv(agenttmux.NoCheckEnv, "")
	spawns := noSpawn(t)

	out := runVersionCmd(t)
	checkContains(t, out, []string{"v0.3.0", "v0.1.0", "latest versions unknown yet", "ws version --check"},
		[]string{"update available", "up to date"})
	if hits.Load() != 0 {
		t.Error("plain version asked GitHub")
	}
	if spawns.Load() != 1 {
		t.Errorf("background refresh spawned %d times, want 1", spawns.Load())
	}

	// --check fills the cache; then no instruction line and no spawn.
	runVersionCmd(t, "--check")
	out = runVersionCmd(t)
	checkContains(t, out, []string{"v0.3.0  (latest v0.4.0 \u2014 update available)", "(latest v0.2.0 \u2014 update available)"},
		[]string{"unknown yet"})
	if hits.Load() != 1 || spawns.Load() != 1 {
		t.Errorf("hits %d spawns %d", hits.Load(), spawns.Load())
	}

	// --short and --json-less quiet paths never spawn either.
	runVersionCmd(t, "--short")
	if spawns.Load() != 1 {
		t.Error("--short spawned")
	}
}

func TestVersionRefreshCacheFlag(t *testing.T) {
	hits := setupCheck(t, "v0.3.0", "v0.4.0", 200, `{"version":"v0.1.0","latest":"v0.1.0","status":"current"}`, nil)
	if out := runVersionCmd(t, "--refresh-cache"); out != "" {
		t.Errorf("--refresh-cache is silent, printed %q", out)
	}
	if hits.Load() != 1 {
		t.Errorf("hits %d", hits.Load())
	}
	if !versions.Cached(context.Background(), "v0.3.0").Known() {
		t.Error("cache not written")
	}
}

func TestVersionCheckJSON(t *testing.T) {
	hits := setupCheck(t, "v0.3.0", "v0.4.0", 200, `{"version":"v0.1.0","latest":"v0.2.0","status":"outdated"}`, nil)
	var out, errOut bytes.Buffer
	if code := Execute([]string{"version", "--check", "--json"}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	var env struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	d := env.Data
	if d["latest"] != "v0.4.0" || d["status"] != "outdated" || d["version"] != "v0.3.0" {
		t.Errorf("ws fields: %v", d)
	}
	if _, ok := d["error"]; ok {
		t.Errorf("error present without a failure: %v", d)
	}
	if d["agent_tmux"] != "v0.1.0" || d["agent_tmux_latest"] != "v0.2.0" || d["checked_at"] == nil {
		t.Errorf("legacy fields: %v", d)
	}
	c, _ := d["agent_tmux_check"].(map[string]any)
	if c["version"] != "v0.1.0" || c["latest"] != "v0.2.0" || c["status"] != "outdated" {
		t.Errorf("agent_tmux_check: %v", c)
	}
	if hits.Load() != 1 {
		t.Errorf("GitHub asked %d times", hits.Load())
	}

	// Plain --json carries the same fields from the cache, without asking.
	out.Reset()
	Execute([]string{"version", "--json"}, strings.NewReader(""), &out, &errOut)
	if !strings.Contains(out.String(), `"agent_tmux_check"`) || !strings.Contains(out.String(), `"latest": "v0.4.0"`) {
		t.Errorf("plain version json: %s", out.String())
	}
	if hits.Load() != 1 {
		t.Errorf("plain version asked GitHub")
	}
}

func TestVersionWSUpgradeHint(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "ws")
	if err := os.WriteFile(exe, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	old := executablePath
	executablePath = func() (string, error) { return exe, nil }
	t.Cleanup(func() { executablePath = old })

	current := `{"version":"v0.1.0","latest":"v0.1.0","status":"current"}`
	setupCheck(t, "v0.3.0", "v0.4.0", 200, current, nil)
	if out := runVersionCmd(t, "--check"); !strings.Contains(out, "releases/latest/download/ws-linux-amd64") {
		t.Errorf("release copy lacks the release page:\n%s", out)
	}
	// From the cache too.
	if out := runVersionCmd(t); !strings.Contains(out, "releases/latest/download/ws-linux-amd64") {
		t.Errorf("cached run lacks the release page:\n%s", out)
	}

	if err := os.WriteFile(filepath.Join(dir, ".ws-source"), []byte("make\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	setupCheck(t, "v0.3.0", "v0.4.0", 200, current, nil)
	if out := runVersionCmd(t, "--check"); !strings.Contains(out, "git pull && make install") {
		t.Errorf("make install copy lacks the rebuild line:\n%s", out)
	}

	setupCheck(t, "v0.4.0", "v0.4.0", 200, current, nil)
	if out := runVersionCmd(t, "--check"); strings.Contains(out, "make install") || strings.Contains(out, "download:") {
		t.Errorf("current ws prints an upgrade hint:\n%s", out)
	}
}
