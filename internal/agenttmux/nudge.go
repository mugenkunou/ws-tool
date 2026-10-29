package agenttmux

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// The version nudge: when GitHub has a newer agent-tmux release than the one
// installed, the TUI and `ws version` say so in one line plus the install
// lines. The answer is cached for a day, and a stale cache is revalidated with
// its ETag, so a check that finds nothing new costs no rate limit.

// latestURL is GitHub's latest-release API for agent-tmux. Tests point it at
// an httptest server.
var latestURL = "https://api.github.com/repos/mugenkunou/agent-tmux/releases/latest"

const (
	cacheTTL     = 24 * time.Hour
	checkTimeout = 3 * time.Second
	cacheName    = "agent-tmux-release.json"
)

// NoCheckEnv turns the check off when set to anything but "" or "0".
const NoCheckEnv = "WS_NO_VERSION_CHECK"

// releaseCache is the cache file: the last answer and when it was fetched.
type releaseCache struct {
	CheckedAt time.Time `json:"checked_at"`
	ETag      string    `json:"etag"`
	Tag       string    `json:"tag_name"`
}

// Disabled reports whether WS_NO_VERSION_CHECK turns the check off.
func Disabled() bool {
	v := os.Getenv(NoCheckEnv)
	return v != "" && v != "0"
}

// cachePath is $XDG_CACHE_HOME/ws/agent-tmux-release.json (default ~/.cache/ws/).
func cachePath() (string, error) {
	dir := os.Getenv("XDG_CACHE_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".cache")
	}
	return filepath.Join(dir, "ws", cacheName), nil
}

func readCache(path string) releaseCache {
	var c releaseCache
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &c)
	}
	return c
}

func writeCache(path string, c releaseCache) {
	data, err := json.Marshal(c)
	if err != nil {
		return
	}
	if os.MkdirAll(filepath.Dir(path), 0o755) != nil {
		return
	}
	tmp := path + ".tmp"
	if os.WriteFile(tmp, data, 0o644) == nil {
		_ = os.Rename(tmp, path)
	}
}

// Latest returns the tag of agent-tmux's newest release: from the cache when
// it is younger than a day, else from GitHub (a 304 keeps the cached tag).
func Latest(ctx context.Context) (string, error) {
	path, err := cachePath()
	if err != nil {
		return "", err
	}
	c := readCache(path)
	now := time.Now()
	if c.Tag != "" && now.Sub(c.CheckedAt) < cacheTTL && !c.CheckedAt.After(now) {
		return c.Tag, nil
	}

	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "ws")
	if c.ETag != "" && c.Tag != "" {
		req.Header.Set("If-None-Match", c.ETag)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusNotModified:
		if c.Tag == "" {
			return "", fmt.Errorf("%s: 304 without a cached release", latestURL)
		}
		c.CheckedAt = now
		writeCache(path, c)
		return c.Tag, nil
	case http.StatusOK:
		var rel struct {
			Tag string `json:"tag_name"`
		}
		if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&rel); err != nil {
			return "", err
		}
		if rel.Tag == "" {
			return "", fmt.Errorf("%s: no tag_name", latestURL)
		}
		writeCache(path, releaseCache{CheckedAt: now, ETag: resp.Header.Get("ETag"), Tag: rel.Tag})
		return rel.Tag, nil
	}
	return "", fmt.Errorf("%s: %s", latestURL, resp.Status)
}

// Nudge is a newer agent-tmux release than the installed one.
type Nudge struct {
	Installed string // e.g. "v0.1.0"
	Latest    string // e.g. "v0.2.0"
}

// Lines are what the TUI and `ws version` print.
func (n Nudge) Lines() []string {
	return append([]string{"agent-tmux " + n.Latest + " is available"}, InstallLines...)
}

// CheckNudge compares the installed agent-tmux with the newest release. ok is
// false when the check is off, agent-tmux is missing, either version cannot be
// read or parsed, or the installed one is not older. It never reports an error:
// a failed check is no nudge.
func CheckNudge(ctx context.Context) (n Nudge, ok bool) {
	if Disabled() {
		return Nudge{}, false
	}
	installed, err := Version(ctx)
	if err != nil {
		return Nudge{}, false
	}
	return NudgeFor(ctx, installed)
}

// NudgeFor is CheckNudge for an installed version the caller already read.
func NudgeFor(ctx context.Context, installed string) (n Nudge, ok bool) {
	if Disabled() {
		return Nudge{}, false
	}
	if _, ok := parseSemver(installed); !ok {
		return Nudge{}, false // "dev" builds never nudge, and never ask GitHub
	}
	latest, err := Latest(ctx)
	if err != nil || !Older(installed, latest) {
		return Nudge{}, false
	}
	return Nudge{Installed: installed, Latest: latest}, true
}

// semver is a parsed vMAJOR.MINOR.PATCH[-pre][+build].
type semver struct {
	num [3]int
	pre string
}

func parseSemver(v string) (semver, bool) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	v, _, _ = strings.Cut(v, "+")
	core, pre, _ := strings.Cut(v, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return semver{}, false
	}
	var s semver
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || p == "" {
			return semver{}, false
		}
		s.num[i] = n
	}
	s.pre = pre
	return s, true
}

// Older reports whether installed is an older version than latest. Either one
// unparseable is false. A pre-release is older than its release; two
// pre-releases of one version compare as text.
func Older(installed, latest string) bool {
	a, okA := parseSemver(installed)
	b, okB := parseSemver(latest)
	if !okA || !okB {
		return false
	}
	for i := range a.num {
		if a.num[i] != b.num[i] {
			return a.num[i] < b.num[i]
		}
	}
	switch {
	case a.pre == b.pre:
		return false
	case a.pre == "":
		return false
	case b.pre == "":
		return true
	}
	return a.pre < b.pre
}
