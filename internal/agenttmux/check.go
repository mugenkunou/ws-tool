package agenttmux

import (
	"context"
	"encoding/json"
	"errors"
)

// Check statuses, as `agent-tmux version --check --json` reports them.
const (
	StatusCurrent  = "current"
	StatusOutdated = "outdated"
	StatusDev      = "dev"
	StatusUnknown  = "unknown"
)

// CheckResult is agent-tmux's answer to `version --check --json`: it asks
// GitHub itself, so ws never queries agent-tmux's releases for this.
type CheckResult struct {
	Version string `json:"version"`         // installed
	Latest  string `json:"latest"`          // "" when unknown
	Status  string `json:"status"`          // current, outdated, dev or unknown
	Error   string `json:"error,omitempty"` // only with status unknown
}

// CheckRunner runs `agent-tmux version --check --json` and returns its stdout.
// Tests replace it with SetCheckRunner.
type CheckRunner func(ctx context.Context) ([]byte, error)

var checkRunner CheckRunner = func(ctx context.Context) ([]byte, error) {
	return Output(ctx, "version", "--check", "--json")
}

// SetCheckRunner swaps the runner until the returned func restores it.
func SetCheckRunner(r CheckRunner) (restore func()) {
	old := checkRunner
	checkRunner = r
	return func() { checkRunner = old }
}

// ErrTooOld means the installed agent-tmux does not know `version --check`
// (it failed, or printed something that is not the JSON contract).
var ErrTooOld = errors.New("installed agent-tmux is too old to check; upgrade")

// RunCheck asks the installed agent-tmux for its version check. It returns
// ErrMissing when agent-tmux is not installed. When it is installed but too
// old, the error is ErrTooOld and the result holds only its Version, read the
// old way ("" if even that fails).
func RunCheck(ctx context.Context) (CheckResult, error) {
	out, err := checkRunner(ctx)
	if errors.Is(err, ErrMissing) {
		return CheckResult{}, ErrMissing
	}
	if err == nil {
		var r CheckResult
		if json.Unmarshal(out, &r) == nil && r.Version != "" {
			switch r.Status {
			case StatusCurrent, StatusOutdated, StatusDev, StatusUnknown:
				return r, nil
			}
		}
	}
	v, _ := Version(ctx)
	return CheckResult{Version: v}, ErrTooOld
}
