package log

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type StartOptions struct {
	LogDir     string
	Tag        string
	QuietStart bool
	NoPrompt   bool
}

type StartResult struct {
	Tag        string `json:"tag"`
	SessionDir string `json:"session_dir"`
	StdinPath  string `json:"stdin_path"`
	StdoutPath string `json:"stdout_path"`
	Active     bool   `json:"active"`
}

type StopOptions struct {
	LogDir string
}

type StopResult struct {
	Tag         string `json:"tag"`
	DurationSec int64  `json:"duration_sec"`
	Stopped     bool   `json:"stopped"`
}

type Session struct {
	Tag         string    `json:"tag"`
	StartedAt   time.Time `json:"started_at"`
	EndedAt     time.Time `json:"ended_at,omitempty"`
	DurationSec int64     `json:"duration_sec,omitempty"`
	Commands    int       `json:"commands"`
	SizeBytes   int64     `json:"size_bytes"`
	Active      bool      `json:"active"`
}

type ScanResult struct {
	Sessions     int    `json:"sessions"`
	StorageBytes int64  `json:"storage_bytes"`
	CapMB        int    `json:"cap_mb"`
	Active       bool   `json:"active"`
	ActiveTag    string `json:"active_tag,omitempty"`
}

type PruneOptions struct {
	LogDir    string
	OlderThan time.Duration
	All       bool
	DryRun    bool
}

type PruneResult struct {
	Removed    []string `json:"removed"`
	FreedBytes int64    `json:"freed_bytes"`
}

// RemoveOptions configures removal of a single session by tag.
type RemoveOptions struct {
	LogDir string
	Tag    string
	DryRun bool
}

// RemoveResult describes the outcome of removing a session.
type RemoveResult struct {
	Tag        string `json:"tag"`
	FreedBytes int64  `json:"freed_bytes"`
	DryRun     bool   `json:"dry_run"`
}

func Start(opts StartOptions) (StartResult, error) {
	statePath := activeStatePath(opts.LogDir)
	if st, _ := loadActive(statePath); st.Tag != "" {
		return StartResult{}, fmt.Errorf("recording already active: %s", st.Tag)
	}

	tag := strings.TrimSpace(opts.Tag)
	if tag == "" {
		tag = time.Now().Format("2006-01-02-1504")
	}
	dir := filepath.Join(opts.LogDir, tag)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return StartResult{}, err
	}
	stdinPath := filepath.Join(dir, "stdin.log")
	stdoutPath := filepath.Join(dir, "stdout.log")
	if _, err := os.Stat(stdinPath); os.IsNotExist(err) {
		if err := os.WriteFile(stdinPath, []byte{}, 0o644); err != nil {
			return StartResult{}, err
		}
	}
	if _, err := os.Stat(stdoutPath); os.IsNotExist(err) {
		if err := os.WriteFile(stdoutPath, []byte{}, 0o644); err != nil {
			return StartResult{}, err
		}
	}

	meta := sessionMeta{Tag: tag, StartedAt: time.Now().UTC()}
	if err := saveMeta(dir, meta); err != nil {
		return StartResult{}, err
	}
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		return StartResult{}, err
	}
	stateData, _ := json.Marshal(activeState{Tag: tag, StartedAt: meta.StartedAt})
	if err := os.WriteFile(statePath, append(stateData, '\n'), 0o644); err != nil {
		return StartResult{}, err
	}

	return StartResult{Tag: tag, SessionDir: dir, StdinPath: stdinPath, StdoutPath: stdoutPath, Active: true}, nil
}

func Stop(opts StopOptions) (StopResult, error) {
	statePath := activeStatePath(opts.LogDir)
	state, err := loadActive(statePath)
	if err != nil {
		return StopResult{}, err
	}
	if state.Tag == "" {
		return StopResult{Stopped: false}, nil
	}
	metaPath := filepath.Join(opts.LogDir, state.Tag)
	meta, err := loadMeta(metaPath)
	if err != nil {
		return StopResult{}, err
	}
	ended := time.Now().UTC()
	meta.EndedAt = ended
	meta.DurationSec = int64(ended.Sub(meta.StartedAt).Seconds())
	if meta.DurationSec < 0 {
		meta.DurationSec = 0
	}
	if err := saveMeta(metaPath, meta); err != nil {
		return StopResult{}, err
	}
	if err := os.Remove(statePath); err != nil && !os.IsNotExist(err) {
		return StopResult{}, err
	}
	return StopResult{Tag: state.Tag, DurationSec: meta.DurationSec, Stopped: true}, nil
}

func List(logDir string) ([]Session, error) {
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return nil, err
	}
	active, _ := loadActive(activeStatePath(logDir))
	entries, err := os.ReadDir(logDir)
	if err != nil {
		return nil, err
	}
	out := make([]Session, 0)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		tag := e.Name()
		s, err := readSession(filepath.Join(logDir, tag), tag)
		if err != nil {
			continue
		}
		s.Active = tag == active.Tag
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })
	return out, nil
}

func Show(logDir, tag, mode string) (string, error) {
	dir := filepath.Join(logDir, tag)
	if _, err := os.Stat(dir); err != nil {
		return "", fmt.Errorf("session %q not found", tag)
	}

	switch mode {
	case "commands-only":
		data, err := os.ReadFile(filepath.Join(dir, "stdin.log"))
		if err != nil {
			return "", err
		}
		return string(data), nil
	default:
		var parts []string
		for _, name := range []string{"stdin.log", "stdout.log"} {
			data, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				continue
			}
			parts = append(parts, string(data))
		}
		return strings.Join(parts, "\n"), nil
	}
}

func Scan(logDir string, capMB int) (ScanResult, error) {
	sessions, err := List(logDir)
	if err != nil {
		return ScanResult{}, err
	}
	res := ScanResult{Sessions: len(sessions), CapMB: capMB}
	for _, s := range sessions {
		res.StorageBytes += s.SizeBytes
		if s.Active {
			res.Active = true
			res.ActiveTag = s.Tag
		}
	}
	return res, nil
}

func Remove(opts RemoveOptions) (RemoveResult, error) {
	tag := strings.TrimSpace(opts.Tag)
	if tag == "" {
		return RemoveResult{}, errors.New("tag is required")
	}

	active, _ := loadActive(activeStatePath(opts.LogDir))
	if tag == active.Tag {
		return RemoveResult{}, fmt.Errorf("cannot remove active session: %s (stop it first with `ws log stop`)", tag)
	}

	dir := filepath.Join(opts.LogDir, tag)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return RemoveResult{}, fmt.Errorf("session not found: %s", tag)
	}

	s, err := readSession(dir, tag)
	if err != nil {
		return RemoveResult{}, fmt.Errorf("reading session %s: %w", tag, err)
	}

	result := RemoveResult{Tag: tag, FreedBytes: s.SizeBytes, DryRun: opts.DryRun}
	if opts.DryRun {
		return result, nil
	}

	if err := os.RemoveAll(dir); err != nil {
		return RemoveResult{}, fmt.Errorf("removing session %s: %w", tag, err)
	}
	return result, nil
}

func Prune(opts PruneOptions) (PruneResult, error) {
	sessions, err := List(opts.LogDir)
	if err != nil {
		return PruneResult{}, err
	}
	active, _ := loadActive(activeStatePath(opts.LogDir))
	res := PruneResult{Removed: []string{}}
	now := time.Now().UTC()
	for _, s := range sessions {
		if s.Tag == active.Tag {
			continue
		}
		remove := opts.All
		if !remove && opts.OlderThan > 0 {
			remove = now.Sub(s.StartedAt) >= opts.OlderThan
		}
		if !remove {
			continue
		}
		if !opts.DryRun {
			dir := filepath.Join(opts.LogDir, s.Tag)
			if err := os.RemoveAll(dir); err != nil {
				return res, err
			}
		}
		res.Removed = append(res.Removed, s.Tag)
		res.FreedBytes += s.SizeBytes
	}
	return res, nil
}

type sessionMeta struct {
	Tag         string    `json:"tag"`
	StartedAt   time.Time `json:"started_at"`
	EndedAt     time.Time `json:"ended_at,omitempty"`
	DurationSec int64     `json:"duration_sec,omitempty"`
}

type activeState struct {
	Tag       string    `json:"tag"`
	StartedAt time.Time `json:"started_at"`
	PID       int       `json:"pid,omitempty"`
}

func activeStatePath(logDir string) string {
	return filepath.Join(logDir, "active.json")
}

func metaPath(dir string) string {
	return filepath.Join(dir, "meta.json")
}

func saveMeta(dir string, m sessionMeta) error {
	b, _ := json.MarshalIndent(m, "", "  ")
	return os.WriteFile(metaPath(dir), append(b, '\n'), 0o644)
}

func loadMeta(dir string) (sessionMeta, error) {
	content, err := os.ReadFile(metaPath(dir))
	if err != nil {
		return sessionMeta{}, err
	}
	var m sessionMeta
	if err := json.Unmarshal(content, &m); err != nil {
		return sessionMeta{}, err
	}
	return m, nil
}

func loadActive(path string) (activeState, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return activeState{}, nil
		}
		return activeState{}, err
	}
	var s activeState
	if err := json.Unmarshal(content, &s); err != nil {
		return activeState{}, err
	}
	return s, nil
}

// SetActivePID updates the active session state with the recording process PID.
func SetActivePID(logDir string, pid int) error {
	statePath := activeStatePath(logDir)
	state, err := loadActive(statePath)
	if err != nil {
		return err
	}
	if state.Tag == "" {
		return errors.New("no active session")
	}
	state.PID = pid
	data, _ := json.Marshal(state)
	return os.WriteFile(statePath, append(data, '\n'), 0o644)
}

// GetActivePID returns the PID of the active recording process, or 0 if none.
func GetActivePID(logDir string) int {
	state, _ := loadActive(activeStatePath(logDir))
	return state.PID
}

func readSession(dir, tag string) (Session, error) {
	m, err := loadMeta(dir)
	if err != nil {
		return Session{}, err
	}
	stdinPath := filepath.Join(dir, "stdin.log")
	stdoutPath := filepath.Join(dir, "stdout.log")
	commands := countLines(stdinPath)
	size := fileSize(stdinPath) + fileSize(stdoutPath)
	return Session{Tag: tag, StartedAt: m.StartedAt, EndedAt: m.EndedAt, DurationSec: m.DurationSec, Commands: commands, SizeBytes: size}, nil
}

func countLines(path string) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	buf := make([]byte, 64*1024)
	s.Buffer(buf, 16*1024*1024)
	count := 0
	for s.Scan() {
		count++
	}
	return count
}

func fileSize(path string) int64 {
	st, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return st.Size()
}
