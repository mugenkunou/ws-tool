// Package versions is the single source of truth for "which versions do I
// have, and what is the newest I know of": ws and agent-tmux. Installed
// versions come from the binaries themselves; the latest ones come from a
// cache file (versions.json beside the other ws caches) that Refresh fills.
// Cached and Stale never touch the network; a background `ws version
// --refresh-cache` keeps the cache young (SpawnRefresh).
package versions

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/mugenkunou/ws-tool/internal/agenttmux"
	"github.com/mugenkunou/ws-tool/internal/release"
)

// State is how an installed version relates to the latest one.
type State string

const (
	StateCurrent      State = "current"
	StateOutdated     State = "outdated"
	StateDev          State = "dev" // unparseable installed version: never outdated
	StateUnknown      State = "unknown"
	StateNotInstalled State = "not_installed"
	StateTooOld       State = "too_old" // agent-tmux without `version --check`
)

// Item is one binary.
type Item struct {
	Name      string // "ws" or "agent-tmux"
	Installed string // "" when not installed
	Latest    string // "" when not known yet
	State     State
	Error     string // set only for a failed lookup, informational
}

// Report is both binaries and when the cache was last filled.
type Report struct {
	WS, AgentTmux Item
	CheckedAt     time.Time // zero when nothing cached
}

// Outdated reports whether either binary has a newer release.
func (r Report) Outdated() bool {
	return r.WS.State == StateOutdated || r.AgentTmux.State == StateOutdated
}

// Known reports whether any lookup ever succeeded.
func (r Report) Known() bool { return !r.CheckedAt.IsZero() }

const (
	cacheName = "versions.json"
	// TTL is how long a cached lookup is young.
	TTL = 24 * time.Hour
	// Backoff is the wait after an attempt, failed or not.
	Backoff = time.Hour
)

// wsReleaseURL is GitHub's latest-release API for ws; tests replace it.
var wsReleaseURL = "https://api.github.com/repos/mugenkunou/ws-tool/releases/latest"

// SetWSReleaseURL points the ws lookup elsewhere (tests) until restore runs.
func SetWSReleaseURL(url string) (restore func()) {
	old := wsReleaseURL
	wsReleaseURL = url
	return func() { wsReleaseURL = old }
}

type cacheItem struct {
	Installed string `json:"installed,omitempty"` // agent-tmux only: what too_old was judged on
	Latest    string `json:"latest,omitempty"`
	State     State  `json:"state,omitempty"`
	Error     string `json:"error,omitempty"`
}

type cacheFile struct {
	WS          cacheItem `json:"ws"`
	AgentTmux   cacheItem `json:"agent_tmux"`
	CheckedAt   time.Time `json:"checked_at"`
	AttemptedAt time.Time `json:"attempted_at"`
}

// CachePath is $XDG_CACHE_HOME/ws/versions.json (default ~/.cache/ws/).
func CachePath() (string, error) {
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

func readCache() cacheFile {
	var c cacheFile
	if p, err := CachePath(); err == nil {
		if data, err := os.ReadFile(p); err == nil {
			_ = json.Unmarshal(data, &c)
		}
	}
	return c
}

func writeCache(c cacheFile) {
	p, err := CachePath()
	if err != nil {
		return
	}
	data, err := json.Marshal(c)
	if err != nil || os.MkdirAll(filepath.Dir(p), 0o755) != nil {
		return
	}
	tmp := p + ".tmp"
	if os.WriteFile(tmp, data, 0o644) == nil {
		_ = os.Rename(tmp, p)
	}
}

// stateOf compares installed with latest. installed "" means not installed.
func stateOf(installed, latest string) State {
	switch {
	case installed == "":
		return StateNotInstalled
	case !release.Parseable(installed):
		return StateDev
	case latest == "" || !release.Parseable(latest):
		return StateUnknown
	case release.Older(installed, latest):
		return StateOutdated
	}
	return StateCurrent
}

// Cached reads installed versions and the cache; it never uses the network and
// never runs agent-tmux --check.
func Cached(ctx context.Context, wsVersion string) Report {
	c := readCache()
	r := Report{CheckedAt: c.CheckedAt}
	r.WS = Item{Name: "ws", Installed: wsVersion, Latest: c.WS.Latest, Error: c.WS.Error}
	r.WS.State = stateOf(wsVersion, r.WS.Latest)

	at := Item{Name: agenttmux.Bin, Latest: c.AgentTmux.Latest, Error: c.AgentTmux.Error}
	v, err := agenttmux.Version(ctx)
	switch {
	case errors.Is(err, agenttmux.ErrMissing):
		at.Latest, at.Error = "", ""
		at.State = StateNotInstalled
	case err != nil:
		at.State, at.Error = StateUnknown, err.Error()
	default:
		at.Installed = v
		at.State = stateOf(v, at.Latest)
		// too_old was judged on one installed version; a different one has not been judged.
		if c.AgentTmux.State == StateTooOld && c.AgentTmux.Installed == v {
			at.State = StateTooOld
		}
	}
	r.AgentTmux = at
	return r
}

// Stale reports whether the cache wants a refresh: missing or older than 24h,
// and no attempt in the last hour. Always false when WS_NO_VERSION_CHECK is set.
func Stale() bool {
	if agenttmux.Disabled() {
		return false
	}
	c := readCache()
	now := time.Now()
	young := !c.CheckedAt.IsZero() && now.Sub(c.CheckedAt) < TTL && !c.CheckedAt.After(now)
	recent := !c.AttemptedAt.IsZero() && now.Sub(c.AttemptedAt) < Backoff && !c.AttemptedAt.After(now)
	return !young && !recent
}

// Refresh looks both latests up now (3s each), writes the cache and returns
// the fresh report. It ignores WS_NO_VERSION_CHECK: it is an explicit request.
// A failed lookup keeps the previously cached latest for that binary.
func Refresh(ctx context.Context, wsVersion string) Report {
	old := readCache()
	now := time.Now()
	next := cacheFile{WS: old.WS, AgentTmux: old.AgentTmux, CheckedAt: old.CheckedAt, AttemptedAt: now}
	ok := false

	// ws
	latest, err := release.Fetch(ctx, wsReleaseURL)
	if err != nil {
		next.WS.Error = err.Error()
	} else {
		next.WS = cacheItem{Latest: latest}
		ok = true
	}
	wsItem := Item{Name: "ws", Installed: wsVersion, Latest: next.WS.Latest, Error: next.WS.Error}
	wsItem.State = stateOf(wsVersion, wsItem.Latest)
	if err == nil && wsItem.State == StateUnknown {
		wsItem.Error = "release tag " + latest + " is not a version"
		next.WS.Error = wsItem.Error
	}
	next.WS.State = wsItem.State

	// agent-tmux
	atItem := Item{Name: agenttmux.Bin}
	res, err := agenttmux.RunCheck(ctx)
	switch {
	case errors.Is(err, agenttmux.ErrMissing):
		atItem.State = StateNotInstalled
		next.AgentTmux = cacheItem{State: StateNotInstalled}
	case err != nil:
		atItem.Installed, atItem.State, atItem.Error = res.Version, StateTooOld, err.Error()
		next.AgentTmux = cacheItem{Installed: res.Version, State: StateTooOld, Error: atItem.Error}
	default:
		atItem.Installed, atItem.Latest, atItem.Error = res.Version, res.Latest, res.Error
		if atItem.Latest == "" { // lookup failed inside agent-tmux: keep the old latest
			atItem.Latest = old.AgentTmux.Latest
		} else {
			ok = true
		}
		atItem.State = stateOf(res.Version, atItem.Latest)
		next.AgentTmux = cacheItem{Latest: atItem.Latest, State: atItem.State, Error: atItem.Error}
	}

	if ok {
		next.CheckedAt = now
	}
	writeCache(next)
	return Report{WS: wsItem, AgentTmux: atItem, CheckedAt: next.CheckedAt}
}

// starter launches the detached refresh; tests replace it so they never fork.
var starter = startDetached

// SetStarter replaces the process starter (tests) until restore runs. The
// starter receives the path of the ws binary.
func SetStarter(f func(exe string) error) (restore func()) {
	old := starter
	starter = f
	return func() { starter = old }
}

// SpawnRefresh starts a detached `ws version --refresh-cache` when the cache
// is Stale; otherwise it does nothing. It claims the attempt first, so
// concurrent ws runs do not each spawn one. It never waits and never fails.
func SpawnRefresh() {
	if !Stale() {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		return
	}
	c := readCache()
	c.AttemptedAt = time.Now()
	writeCache(c)
	_ = starter(exe)
}
