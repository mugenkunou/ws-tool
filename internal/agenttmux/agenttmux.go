// Package agenttmux is ws's client of the agent-tmux binary, which owns
// terminal sessions and their recordings. ws has no session code of its own:
// `ws session` execs agent-tmux, completion asks `agent-tmux __complete`, and
// the TUI Sessions screen reads `agent-tmux ls --json` and `grep --json`.
// Everything here goes through the binary found on PATH; nothing reads
// agent-tmux's files.
package agenttmux

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Bin is the program's name on PATH.
const Bin = "agent-tmux"

// InstallLines are the two ways to install agent-tmux, as the TUI and the
// version nudge print them.
var InstallLines = []string{
	"download: https://github.com/mugenkunou/agent-tmux/releases/latest",
	"or:       go install github.com/mugenkunou/agent-tmux/cmd/agent-tmux@latest",
}

// ErrMissing means agent-tmux is not on PATH.
var ErrMissing = errors.New("agent-tmux is not installed")

// Path returns the agent-tmux binary on PATH.
func Path() (string, error) {
	p, err := exec.LookPath(Bin)
	if err != nil {
		return "", ErrMissing
	}
	return p, nil
}

// Command builds `agent-tmux <args…>`, for callers that hand it the terminal.
func Command(args ...string) (*exec.Cmd, error) {
	p, err := Path()
	if err != nil {
		return nil, err
	}
	return exec.Command(p, args...), nil
}

// queryTimeout bounds every call whose output ws reads.
const queryTimeout = 10 * time.Second

// Output runs `agent-tmux <args…>` and returns its stdout. A non-zero exit is
// an error that carries agent-tmux's own message from stderr.
func Output(ctx context.Context, args ...string) ([]byte, error) {
	p, err := Path()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	var stdout, stderr bytes.Buffer
	c := exec.CommandContext(ctx, p, args...)
	c.Stdout, c.Stderr = &stdout, &stderr
	if err := c.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return stdout.Bytes(), errors.New(msg)
		}
		return stdout.Bytes(), fmt.Errorf("agent-tmux %s: %w", strings.Join(args, " "), err)
	}
	return stdout.Bytes(), nil
}

// Session is one row of `agent-tmux ls --json`: a live session or an ended
// recording. Times are RFC3339 strings, "" when unknown.
type Session struct {
	Name            string `json:"name"`
	ID              string `json:"id"`       // the recording's id; "" for a live session without one
	State           string `json:"state"`    // "live" or "ended"
	Recorder        string `json:"recorder"` // "running", "stopped", "none", or "" when ended
	Owner           string `json:"owner"`
	Clients         int    `json:"clients"`
	Command         string `json:"command"`
	Path            string `json:"path"`
	Host            string `json:"host"`
	StartedAt       string `json:"started_at"`
	EndedAt         string `json:"ended_at"`
	HistoryStartsAt string `json:"history_starts_at"`
	Bytes           int64  `json:"bytes"`
}

// Live reports whether the session is running.
func (s Session) Live() bool { return s.State == "live" }

// Ref is how agent-tmux names the session in a command: the name of a live
// one, the id of a recording.
func (s Session) Ref() string {
	if s.Live() || s.ID == "" {
		return s.Name
	}
	return s.ID
}

// Key identifies the row across reloads: a live session by name (its
// recording id can be empty), a recording by id.
func (s Session) Key() string {
	if s.Live() {
		return "live:" + s.Name
	}
	return s.ID
}

// parseTime reads an RFC3339 time; "" and junk are the zero time.
func parseTime(v string) time.Time {
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return time.Time{}
	}
	return t
}

// Ls lists live sessions and recordings, as `agent-tmux ls --json` orders them.
func Ls(ctx context.Context) ([]Session, error) {
	out, err := Output(ctx, "ls", "--json")
	if err != nil {
		return nil, err
	}
	var doc struct {
		Sessions []Session `json:"sessions"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		return nil, fmt.Errorf("agent-tmux ls --json: %w", err)
	}
	return doc.Sessions, nil
}

// Match is one line `agent-tmux grep --json` found.
type Match struct {
	Name string `json:"name"`
	ID   string `json:"id"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

// GrepResult is the answer of `agent-tmux grep --json`.
type GrepResult struct {
	Matches   []Match `json:"matches"`
	Truncated bool    `json:"truncated"`
}

// Grep searches every recording for text (case-insensitive), newest first.
// limit 0 takes agent-tmux's default.
func Grep(ctx context.Context, text string, limit int) (GrepResult, error) {
	args := []string{"grep", "--json"}
	if limit > 0 {
		args = append(args, "--limit", strconv.Itoa(limit))
	}
	// "--" so a text that starts with a dash is not taken for a flag.
	if strings.HasPrefix(text, "-") {
		args = append(args, "--")
	}
	out, err := Output(ctx, append(args, text)...)
	if err != nil {
		return GrepResult{}, err
	}
	var res GrepResult
	if err := json.Unmarshal(out, &res); err != nil {
		return GrepResult{}, fmt.Errorf("agent-tmux grep --json: %w", err)
	}
	return res, nil
}

// completeTimeout keeps a slow agent-tmux from hanging the shell's tab.
const completeTimeout = 2 * time.Second

// Complete asks `agent-tmux __complete <words…>` for the candidates of the
// last word and returns their values, without descriptions. Any failure,
// agent-tmux missing included, is no candidates.
func Complete(words []string) []string {
	ctx, cancel := context.WithTimeout(context.Background(), completeTimeout)
	defer cancel()
	out, err := Output(ctx, append([]string{"__complete"}, words...)...)
	if err != nil {
		return nil
	}
	return ParseCandidates(string(out))
}

// ParseCandidates reads agent-tmux's completion lines: `value` or
// `value<TAB>description`, one per line. Blank lines are skipped.
func ParseCandidates(out string) []string {
	var vals []string
	for _, line := range strings.Split(out, "\n") {
		v, _, _ := strings.Cut(strings.TrimRight(line, "\r"), "\t")
		if v != "" {
			vals = append(vals, v)
		}
	}
	return vals
}

// Version is the installed agent-tmux's version (`agent-tmux version
// --json`), such as "v0.1.0" or "dev".
func Version(ctx context.Context) (string, error) {
	out, err := Output(ctx, "version", "--json")
	if err != nil {
		return "", err
	}
	var v struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(out, &v); err != nil {
		return "", fmt.Errorf("agent-tmux version --json: %w", err)
	}
	return v.Version, nil
}
