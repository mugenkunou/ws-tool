// Package session is the data behind `ws session`: recorded terminal sessions, each
// a directory <session.root_dir>/<tag>/ holding meta.json, stdout.log (the
// transcript) and stdin.log (the commands, one per line).
package session

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/mugenkunou/ws-tool/internal/trash"
)

const (
	// CommandsFile is the commands, one line per command (the old stdin.log).
	CommandsFile = "stdin.log"
	// TranscriptFile is everything the terminal showed (the old stdout.log).
	TranscriptFile = "stdout.log"
)

// Session is one row of `ws session ls`.
type Session struct {
	Tag         string    `json:"tag"`
	StartedAt   time.Time `json:"started_at"`
	EndedAt     time.Time `json:"ended_at,omitempty"`
	DurationSec int64     `json:"duration_sec,omitempty"`
	Commands    int       `json:"commands"`
	SizeBytes   int64     `json:"size_bytes"`
	// Active is true only for the recording running on this machine.
	Active bool `json:"active"`
}

type sessionMeta struct {
	Tag         string    `json:"tag"`
	StartedAt   time.Time `json:"started_at"`
	EndedAt     time.Time `json:"ended_at,omitempty"`
	DurationSec int64     `json:"duration_sec,omitempty"`
}

// ── The active-recording marker ────────────────────────────────────
//
// Only one recording runs at a time on a machine. The marker is machine-local
// ($XDG_STATE_HOME/ws-tool/session-active.json), not in the synced workspace:
// a synced marker would block `record` on every other machine, and `stop` there
// would signal a PID that belongs to this one.

// Marker is the running recording: its tag, start time, recorder PID and the
// session directory (absolute, so `stop` does not depend on --workspace).
type Marker struct {
	Tag       string    `json:"tag"`
	StartedAt time.Time `json:"started_at"`
	PID       int       `json:"pid,omitempty"`
	Dir       string    `json:"dir,omitempty"`
}

// MarkerPath is where the marker lives.
func MarkerPath(stateDir string) string { return filepath.Join(stateDir, "session-active.json") }

// legacyMarkerPath is where older ws kept the marker, inside the synced
// workspace. It is finalized by Stop and ignored everywhere else.
func legacyMarkerPath(logDir string) string { return filepath.Join(logDir, "active.json") }

func readMarker(path string) (Marker, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Marker{}, nil
		}
		return Marker{}, err
	}
	var m Marker
	if err := json.Unmarshal(content, &m); err != nil {
		return Marker{}, err
	}
	return m, nil
}

func writeMarker(path string, m Marker) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// pidAlive reports whether a process with this PID is running.
func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// ActiveMarker returns the recording running on this machine. A marker whose
// recorder is gone is stale and is not active.
func ActiveMarker(stateDir string) (Marker, bool) {
	m, err := readMarker(MarkerPath(stateDir))
	if err != nil || m.Tag == "" || !pidAlive(m.PID) {
		return Marker{}, false
	}
	return m, true
}

// SetActivePID records the recorder's PID in the marker.
func SetActivePID(stateDir string, pid int) error {
	m, err := readMarker(MarkerPath(stateDir))
	if err != nil {
		return err
	}
	if m.Tag == "" {
		return errors.New("no active session")
	}
	m.PID = pid
	return writeMarker(MarkerPath(stateDir), m)
}

// ── Start and stop ─────────────────────────────────────────────────

// ValidateTag rejects tags that would escape the session directory or hide.
func ValidateTag(tag string) error {
	switch {
	case tag == "" || tag == "." || tag == "..":
		return fmt.Errorf("invalid session tag %q", tag)
	case strings.ContainsAny(tag, `/\`):
		return fmt.Errorf("invalid session tag %q: a tag must not contain /", tag)
	case strings.HasPrefix(tag, "."):
		return fmt.Errorf("invalid session tag %q: a tag must not start with .", tag)
	}
	return nil
}

// StartOptions configures Start.
type StartOptions struct {
	LogDir   string
	StateDir string
	// Tag names the session; empty means the local time, YYYY-MM-DD-HHMM.
	Tag string
	// PID is the recorder's PID. Empty means this process, until the recorder
	// replaces it with SetActivePID.
	PID int
}

// StartResult describes the session Start created.
type StartResult struct {
	Tag        string `json:"tag"`
	SessionDir string `json:"session_dir"`
	StdinPath  string `json:"stdin_path"`
	StdoutPath string `json:"stdout_path"`
}

// Start creates the session directory and the marker. It refuses when a
// recording already runs on this machine, and finalizes a stale marker (a
// recording whose process is gone) first. A tag that already exists gets -2, -3,
// … instead of overwriting the old recording.
func Start(opts StartOptions) (StartResult, error) {
	markerPath := MarkerPath(opts.StateDir)
	if m, _ := readMarker(markerPath); m.Tag != "" {
		if pidAlive(m.PID) {
			return StartResult{}, fmt.Errorf("a recording is already running: %s", m.Tag)
		}
		if _, err := finalize(m, time.Now().UTC()); err != nil {
			return StartResult{}, fmt.Errorf("finalize the stale recording %s: %w", m.Tag, err)
		}
		_ = os.Remove(markerPath)
	}

	tag := strings.TrimSpace(opts.Tag)
	if tag == "" {
		tag = time.Now().Format("2006-01-02-1504")
	}
	if err := ValidateTag(tag); err != nil {
		return StartResult{}, err
	}
	if err := os.MkdirAll(opts.LogDir, 0o755); err != nil {
		return StartResult{}, err
	}
	final, dir := tag, filepath.Join(opts.LogDir, tag)
	for n := 2; ; n++ {
		err := os.Mkdir(dir, 0o755)
		if err == nil {
			break
		}
		if !os.IsExist(err) {
			return StartResult{}, err
		}
		final = fmt.Sprintf("%s-%d", tag, n)
		dir = filepath.Join(opts.LogDir, final)
	}

	res := StartResult{
		Tag:        final,
		SessionDir: dir,
		StdinPath:  filepath.Join(dir, CommandsFile),
		StdoutPath: filepath.Join(dir, TranscriptFile),
	}
	for _, p := range []string{res.StdinPath, res.StdoutPath} {
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			return StartResult{}, err
		}
	}
	started := time.Now().UTC()
	if err := saveMeta(dir, sessionMeta{Tag: final, StartedAt: started}); err != nil {
		return StartResult{}, err
	}
	pid := opts.PID
	if pid == 0 {
		pid = os.Getpid()
	}
	if err := writeMarker(markerPath, Marker{Tag: final, StartedAt: started, PID: pid, Dir: dir}); err != nil {
		return StartResult{}, err
	}
	return res, nil
}

// StopOptions configures Stop and StopActive.
type StopOptions struct {
	LogDir   string
	StateDir string
}

// StopResult describes a finalized recording.
type StopResult struct {
	Tag         string `json:"tag"`
	DurationSec int64  `json:"duration_sec"`
	Stopped     bool   `json:"stopped"`
	Dir         string `json:"-"`
}

// finalize writes the end time and duration into the session's meta.json.
func finalize(m Marker, ended time.Time) (StopResult, error) {
	dir := m.Dir
	if dir == "" {
		return StopResult{}, errors.New("the marker has no session directory")
	}
	meta, err := loadMeta(dir)
	if err != nil {
		return StopResult{}, err
	}
	if meta.EndedAt.IsZero() {
		meta.EndedAt = ended
		meta.DurationSec = max(int64(ended.Sub(meta.StartedAt).Seconds()), 0)
		if err := saveMeta(dir, meta); err != nil {
			return StopResult{}, err
		}
	}
	return StopResult{Tag: meta.Tag, DurationSec: meta.DurationSec, Stopped: true, Dir: dir}, nil
}

// Stop finalizes the recording on this machine: the end time and duration go
// into meta.json and the marker is removed. It is idempotent: with nothing
// recording it reports Stopped false. A marker at the old synced location is
// finalized without signalling any process, then removed.
func Stop(opts StopOptions) (StopResult, error) {
	markerPath := MarkerPath(opts.StateDir)
	if m, err := readMarker(markerPath); err != nil {
		return StopResult{}, err
	} else if m.Tag != "" {
		res, err := finalize(m, time.Now().UTC())
		if err != nil {
			return StopResult{}, err
		}
		if err := os.Remove(markerPath); err != nil && !os.IsNotExist(err) {
			return StopResult{}, err
		}
		return res, nil
	}

	legacyPath := legacyMarkerPath(opts.LogDir)
	lm, err := readMarker(legacyPath)
	if err != nil || lm.Tag == "" {
		return StopResult{}, nil
	}
	lm.Dir = filepath.Join(opts.LogDir, lm.Tag)
	res, err := finalize(lm, time.Now().UTC())
	if err != nil {
		return StopResult{}, err
	}
	if err := os.Remove(legacyPath); err != nil && !os.IsNotExist(err) {
		return StopResult{}, err
	}
	return res, nil
}

// StopActive ends the recording running on this machine, from any terminal: it
// sends the recorder SIGTERM, waits up to 3 seconds for it to exit, then
// finalizes the session. A legacy marker is finalized without a signal.
func StopActive(opts StopOptions) (StopResult, error) {
	if m, ok := ActiveMarker(opts.StateDir); ok && m.PID != os.Getpid() {
		if proc, err := os.FindProcess(m.PID); err == nil {
			_ = proc.Signal(syscall.SIGTERM)
			for range 30 {
				if !pidAlive(m.PID) {
					break
				}
				time.Sleep(100 * time.Millisecond)
			}
		}
	}
	return Stop(opts)
}

// ── Listing ────────────────────────────────────────────────────────

// List returns every session directory that has a meta.json, newest first.
func List(logDir, stateDir string) ([]Session, error) {
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return nil, err
	}
	active, _ := ActiveMarker(stateDir)
	entries, err := os.ReadDir(logDir)
	if err != nil {
		return nil, err
	}
	out := make([]Session, 0)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		s, err := readSession(filepath.Join(logDir, e.Name()), e.Name())
		if err != nil {
			continue
		}
		s.Active = e.Name() == active.Tag
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })
	return out, nil
}

func readSession(dir, tag string) (Session, error) {
	m, err := loadMeta(dir)
	if err != nil {
		return Session{}, err
	}
	stdin := filepath.Join(dir, CommandsFile)
	stdout := filepath.Join(dir, TranscriptFile)
	return Session{
		Tag: tag, StartedAt: m.StartedAt, EndedAt: m.EndedAt, DurationSec: m.DurationSec,
		Commands: countLines(stdin), SizeBytes: fileSize(stdin) + fileSize(stdout),
	}, nil
}

// ── Show and grep ──────────────────────────────────────────────────

// ShowResult is the text `session show` prints and the file it came from.
type ShowResult struct {
	Tag      string
	Path     string
	Commands bool
	Text     string
}

// Show returns a session's transcript with terminal escape sequences and
// carriage returns removed, or with commands true its commands file. The tag
// must name an existing session exactly. A recording in progress shows what has
// been written so far.
func Show(logDir, tag string, commands bool) (ShowResult, error) {
	if err := ValidateTag(tag); err != nil {
		return ShowResult{}, err
	}
	dir := filepath.Join(logDir, tag)
	if _, err := os.Stat(filepath.Join(dir, "meta.json")); err != nil {
		return ShowResult{}, fmt.Errorf("session %q not found", tag)
	}
	file := TranscriptFile
	if commands {
		file = CommandsFile
	}
	path := filepath.Join(dir, file)
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return ShowResult{}, err
	}
	return ShowResult{Tag: tag, Path: path, Commands: commands, Text: StripEscapes(string(data))}, nil
}

// Match is one line of `session grep`.
type Match struct {
	Tag  string `json:"tag"`
	Path string `json:"path"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

// Grep searches the commands and the transcript of every session for text, a
// literal case-insensitive substring. Escape sequences and carriage returns are
// removed before matching. Sessions are searched newest first, the commands
// file before the transcript. max stops after that many matches (0: no limit).
func Grep(logDir, stateDir, text string, max int) ([]Match, error) {
	needle := strings.ToLower(strings.TrimSpace(text))
	if needle == "" {
		return nil, errors.New("text to search for is empty")
	}
	sessions, err := List(logDir, stateDir)
	if err != nil {
		return nil, err
	}
	matches := []Match{}
	for _, s := range sessions {
		for _, file := range []string{CommandsFile, TranscriptFile} {
			path := filepath.Join(logDir, s.Tag, file)
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			for i, line := range strings.Split(StripEscapes(string(data)), "\n") {
				if strings.Contains(strings.ToLower(line), needle) {
					matches = append(matches, Match{Tag: s.Tag, Path: path, Line: i + 1, Text: strings.TrimRight(line, " \t")})
					if max > 0 && len(matches) >= max {
						return matches, nil
					}
				}
			}
		}
	}
	return matches, nil
}

// StripEscapes removes terminal escape sequences (CSI, OSC and the short
// two-character ones) and carriage returns, so a raw recording reads as plain
// text. It uses only the standard library.
func StripEscapes(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\r':
			// dropped: \r\n becomes \n and a lone \r is a redraw
		case c == 0x1b && i+1 < len(s):
			switch s[i+1] {
			case '[': // CSI: parameters, then a final byte @-~
				i += 2
				for i < len(s) && !(s[i] >= '@' && s[i] <= '~') {
					i++
				}
			case ']': // OSC: until BEL or ST (ESC \)
				i += 2
				for i < len(s) && s[i] != 0x07 && !(s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\') {
					i++
				}
				if i < len(s) && s[i] == 0x1b {
					i++
				}
			case '(', ')', '*', '+', '#', '%': // charset and similar: one more byte
				i += 2
			default: // ESC followed by one character
				i++
			}
		case c == 0x1b:
			// a trailing lone ESC
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// ── Removing and pruning ───────────────────────────────────────────

// RemoveOptions configures Remove.
type RemoveOptions struct {
	LogDir   string
	StateDir string
	Tag      string
}

// Remove moves a session to the XDG trash, where `ws trash restore` can bring it
// back. It refuses the recording in progress.
func Remove(opts RemoveOptions) error {
	tag := strings.TrimSpace(opts.Tag)
	if err := ValidateTag(tag); err != nil {
		return err
	}
	if active, ok := ActiveMarker(opts.StateDir); ok && active.Tag == tag {
		return fmt.Errorf("cannot remove the active session: %s (stop it first with `ws session stop`)", tag)
	}
	dir := filepath.Join(opts.LogDir, tag)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return fmt.Errorf("session not found: %s", tag)
	}
	if err := trash.SoftDelete(dir); err != nil {
		return fmt.Errorf("removing session %s: %w", tag, err)
	}
	return nil
}

// SelectOlderThan returns the finished sessions that started at least olderThan
// before now, oldest first. The recording in progress is never chosen.
func SelectOlderThan(sessions []Session, olderThan time.Duration, now time.Time) []Session {
	var out []Session
	for _, s := range sessions {
		if !s.Active && now.Sub(s.StartedAt) >= olderThan {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.Before(out[j].StartedAt) })
	return out
}

// ── Files ──────────────────────────────────────────────────────────

func metaPath(dir string) string { return filepath.Join(dir, "meta.json") }

func saveMeta(dir string, m sessionMeta) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
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

func countLines(path string) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 64*1024), 16*1024*1024)
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
