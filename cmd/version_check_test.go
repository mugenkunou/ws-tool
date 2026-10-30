package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mugenkunou/ws-tool/internal/agenttmux"
	"github.com/mugenkunou/ws-tool/internal/agenttmux/agenttmuxtest"
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

// setupCheck points the ws lookup at a server, sets the ws version and fakes
// agent-tmux's check (nil result with err means the runner fails).
func setupCheck(t *testing.T, wsVersion, wsTag string, wsStatus int, at string, atErr error) *atomic.Int32 {
	t.Helper()
	url, hits := releaseServer(t, wsTag, wsStatus)
	oldURL, oldVer := wsReleaseURL, appVersion
	wsReleaseURL, appVersion = url, wsVersion
	t.Cleanup(func() { wsReleaseURL, appVersion = oldURL, oldVer })
	restore := agenttmux.SetCheckRunner(func(context.Context) ([]byte, error) { return []byte(at), atErr })
	t.Cleanup(restore)
	// WS_NO_VERSION_CHECK is set by TestMain: --check must ignore it.
	return hits
}

func runVersionCheck(t *testing.T, extra ...string) string {
	t.Helper()
	var out, errOut bytes.Buffer
	args := append([]string{"version", "--check", "--no-color"}, extra...)
	if code := Execute(args, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	return out.String()
}

func TestVersionCheckWS(t *testing.T) {
	cases := []struct {
		name, ver, tag string
		status         int
		want, not      []string
	}{
		{"current", "v0.3.0", "v0.3.0", 200, []string{"v0.3.0 (latest v0.3.0)"}, []string{"update available", "dev build"}},
		{"outdated", "v0.3.0", "v0.4.0", 200, []string{"update available: v0.4.0"}, []string{"dev build"}},
		{"dev", "dev", "v0.4.0", 200, []string{"local binary is a dev build, upstream release is v0.4.0"}, []string{"update available"}},
		{"unparseable", "abc123", "v0.4.0", 200, []string{"dev build, upstream release is v0.4.0"}, []string{"update available"}},
		{"failed", "v0.3.0", "", 500, []string{"release check failed", "500"}, []string{"update available"}},
		{"dev failed", "dev", "", 500, []string{"dev build", "release check failed"}, []string{"update available", "upstream release is v"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			setupCheck(t, c.ver, c.tag, c.status, `{"version":"v0.1.0","latest":"v0.1.0","status":"current"}`, nil)
			out := runVersionCheck(t)
			for _, w := range c.want {
				if !strings.Contains(out, w) {
					t.Errorf("lacks %q:\n%s", w, out)
				}
			}
			for _, n := range c.not {
				if strings.Contains(out, n) {
					t.Errorf("has %q:\n%s", n, out)
				}
			}
		})
	}
}

func TestVersionCheckAgentTmux(t *testing.T) {
	cases := []struct {
		name, json string
		err        error
		want, not  []string
	}{
		{"current", `{"version":"v0.1.0","latest":"v0.1.0","status":"current"}`, nil, []string{"v0.1.0 (latest v0.1.0)"}, nil},
		{"outdated", `{"version":"v0.1.0","latest":"v0.2.0","status":"outdated"}`, nil, []string{"update available: v0.2.0"}, nil},
		{"dev", `{"version":"dev","latest":"v0.2.0","status":"dev"}`, nil, []string{"dev build, upstream release is v0.2.0"}, []string{"update available"}},
		{"unknown", `{"version":"v0.1.0","latest":"","status":"unknown","error":"timeout"}`, nil, []string{"release check failed: timeout"}, []string{"update available"}},
		{"too old (fails)", ``, errors.New("unknown flag: --check"), []string{"too old to check; upgrade", agenttmux.InstallLines[0]}, []string{"update available"}},
		{"too old (not json)", `v0.0.9`, nil, []string{"too old to check; upgrade"}, nil},
		{"missing", ``, agenttmux.ErrMissing, []string{"not installed", agenttmux.InstallLines[1]}, []string{"update available"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			setupCheck(t, "v0.3.0", "v0.3.0", 200, c.json, c.err)
			agenttmuxtest.Fake(t, `echo '{"version":"v0.0.9"}'`) // the old `version --json`
			out := runVersionCheck(t)
			for _, w := range c.want {
				if !strings.Contains(out, w) {
					t.Errorf("lacks %q:\n%s", w, out)
				}
			}
			for _, n := range c.not {
				if strings.Contains(out, n) {
					t.Errorf("has %q:\n%s", n, out)
				}
			}
			if strings.HasPrefix(c.name, "too old") && !strings.Contains(out, "v0.0.9") {
				t.Errorf("too old: the installed version is not shown:\n%s", out)
			}
		})
	}
}

// TestVersionCheckAgentTmuxScript runs a real fake agent-tmux script that
// predates --check: it exits non-zero on the unknown flag.
func TestVersionCheckAgentTmuxScript(t *testing.T) {
	url, _ := releaseServer(t, "v0.3.0", 200)
	oldURL, oldVer := wsReleaseURL, appVersion
	wsReleaseURL, appVersion = url, "v0.3.0"
	t.Cleanup(func() { wsReleaseURL, appVersion = oldURL, oldVer })
	log := agenttmuxtest.Fake(t, `case "$*" in *--check*) echo "unknown flag: --check" >&2; exit 2;; esac
echo '{"version":"v0.0.9"}'`)
	out := runVersionCheck(t)
	if !strings.Contains(out, "v0.0.9") || !strings.Contains(out, "too old to check; upgrade") {
		t.Errorf("output:\n%s", out)
	}
	if calls := agenttmuxtest.Calls(t, log); len(calls) == 0 || strings.Join(calls[0], " ") != "version --check --json" {
		t.Errorf("calls %v", calls)
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
	if d["agent_tmux"] != "v0.1.0" || d["agent_tmux_latest"] != "v0.2.0" {
		t.Errorf("legacy fields: %v", d)
	}
	c, _ := d["agent_tmux_check"].(map[string]any)
	if c["version"] != "v0.1.0" || c["latest"] != "v0.2.0" || c["status"] != "outdated" {
		t.Errorf("agent_tmux_check: %v", c)
	}
	if hits.Load() != 1 {
		t.Errorf("GitHub asked %d times", hits.Load())
	}

	// Without --check the new fields stay out.
	out.Reset()
	Execute([]string{"version", "--json"}, strings.NewReader(""), &out, &errOut)
	if strings.Contains(out.String(), "agent_tmux_check") || strings.Contains(out.String(), `"status"`) {
		t.Errorf("plain version has check fields: %s", out.String())
	}
	if hits.Load() != 1 {
		t.Errorf("plain version asked GitHub")
	}
}
