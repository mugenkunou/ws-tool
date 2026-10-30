package agenttmux

import (
	"context"
	"os"
	"path/filepath"

	"github.com/mugenkunou/ws-tool/internal/release"
)

// The version nudge: when GitHub has a newer agent-tmux release than the one
// installed, the TUI and `ws version` say so in one line plus the install
// lines. The answer is cached for a day, and a stale cache is revalidated with
// its ETag, so a check that finds nothing new costs no rate limit.

// latestURL is GitHub's latest-release API for agent-tmux. Tests point it at
// an httptest server.
var latestURL = "https://api.github.com/repos/mugenkunou/agent-tmux/releases/latest"

const cacheName = "agent-tmux-release.json"

// NoCheckEnv turns the check off when set to anything but "" or "0".
const NoCheckEnv = "WS_NO_VERSION_CHECK"

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

// Latest returns the tag of agent-tmux's newest release: from the cache when
// it is younger than a day, else from GitHub (a 304 keeps the cached tag).
func Latest(ctx context.Context) (string, error) {
	path, err := cachePath()
	if err != nil {
		return "", err
	}
	return release.Latest(ctx, latestURL, path)
}

// Nudge is a newer agent-tmux release than the installed one.
type Nudge struct {
	Installed string // e.g. "v0.1.0"
	Latest    string // e.g. "v0.2.0"
}

// Lines are what the TUI and `ws version` print.
func (n Nudge) Lines() []string {
	return append([]string{"agent-tmux " + n.Latest + " is available"}, InstallLines()...)
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
	if !release.Parseable(installed) {
		return Nudge{}, false // "dev" builds never nudge, and never ask GitHub
	}
	latest, err := Latest(ctx)
	if err != nil || !Older(installed, latest) {
		return Nudge{}, false
	}
	return Nudge{Installed: installed, Latest: latest}, true
}

// Older reports whether installed is an older version than latest; see
// release.Older.
func Older(installed, latest string) bool { return release.Older(installed, latest) }
