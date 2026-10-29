package mirror

import (
	"errors"
	"fmt"
	"strings"

	"github.com/mugenkunou/ws-tool/internal/style"
	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

// OptionsFrom turns ws.json's sync block into mirror options for workspace ws.
// A target starting with ~ or / is expanded; any other value is kept as
// written, and Check rejects it.
func OptionsFrom(ws string, cfg wsconfig.WsConfig) (Options, error) {
	state, err := wsconfig.StateDir()
	if err != nil {
		return Options{}, err
	}
	o := Options{
		Workspace:    ws,
		Rules:        cfg.IgnoreRules,
		Exclude:      cfg.Sync.Exclude,
		MaxDeletePct: cfg.Sync.MaxDeletePct,
		StateDir:     state,
	}
	if t := strings.TrimSpace(cfg.Sync.Mirror.Target); t != "" {
		o.Target = t // relative: Check rejects it
		if strings.HasPrefix(t, "~") || strings.HasPrefix(t, "/") {
			if o.Target, err = wsconfig.ExpandUserPath(t); err != nil {
				return Options{}, err
			}
		}
	}
	return o, nil
}

// Status is what `ws sync status` reports: the mirror, this machine's last
// push to it, the stamp, and what the next push would do.
type Status struct {
	Target         string     `json:"target"`
	Stamp          StampState `json:"stamp"`
	LastPush       *LastPush  `json:"last_push"`
	PendingChanges int        `json:"pending_changes"`
	PendingDeletes int        `json:"pending_deletes"`
	Files          int        `json:"files"`
	Bytes          int64      `json:"bytes"`
	Symlinks       []string   `json:"skipped_symlinks"`
}

// StatusOf reads the status off a prepared push.
func StatusOf(p *Prepared) Status {
	syms := p.Filter.Symlinks
	if syms == nil {
		syms = []string{}
	}
	return Status{Target: p.Options.Target, Stamp: p.State, LastPush: p.Last,
		PendingChanges: len(p.Changed), PendingDeletes: len(p.Deleted),
		Files: p.Filter.Files, Bytes: p.Filter.Bytes, Symlinks: syms}
}

// Fact is one labelled line of the status.
type Fact struct{ Label, Value string }

// Facts is the status as the CLI prints it and the TUI header shows it, so the
// two cannot drift: mirror, last push, stamp, pending.
func (s Status) Facts() []Fact {
	last := "never (from this machine)"
	if l := s.LastPush; l != nil {
		last = fmt.Sprintf("%s · %s · %s · %d copied · %d deleted", l.Time.Local().Format("2006-01-02 15:04"),
			Plural(l.Files, "file"), style.HumanBytes(l.Bytes), l.Changed, l.Deleted)
	}
	return []Fact{
		{"Mirror", s.Target},
		{"Last push", last},
		{"Stamp", StampLabel(s.Stamp)},
		{"Pending", fmt.Sprintf("%d to copy · %d to delete (%s, %s in all)", s.PendingChanges, s.PendingDeletes,
			Plural(s.Files, "file"), style.HumanBytes(s.Bytes))},
	}
}

// Plural renders a count with its noun: "1 file", "2 files".
func Plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// StampLabel says what the stamp state means for the next push.
func StampLabel(s StampState) string {
	switch s {
	case StampOK:
		return "ok (written by this machine's last push)"
	case StampWiped:
		return "missing: the mirror was emptied since the last push; the next push re-uploads"
	case StampForeign:
		return "foreign: another push wrote the mirror; the next push asks first"
	}
	return "none yet (first push)"
}

// SymlinkWarning says that n symlinks are left out of the mirror, or "" for none.
func SymlinkWarning(n int) string {
	if n == 0 {
		return ""
	}
	return fmt.Sprintf("%s skipped: symlinks never sync (ws ignore lint lists them)", Plural(n, "symlink"))
}

// WipedWarning says the mirror was emptied since the last push, or "" when it
// was not. It is a notice, not a question: the push re-uploads.
func (p *Prepared) WipedWarning() string {
	if p.State != StampWiped || p.Last == nil {
		return ""
	}
	return fmt.Sprintf("The mirror was emptied since the last push (%s). Re-uploading %s; older copies are in your sync client's trash.",
		p.Last.Time.Local().Format("2006-01-02 15:04"), Plural(p.Filter.Files, "file"))
}

// QuestionKind names a safety question a push asks before it runs.
type QuestionKind string

const (
	// AskForeign: another push wrote the mirror.
	AskForeign QuestionKind = "foreign"
	// AskGuard: the push would delete more than sync.max_delete_pct of the mirror.
	AskGuard QuestionKind = "guard"
)

// Question is something a push must have confirmed before it runs. Sync never
// answers one for the user: the CLI asks (and refuses under --quiet or
// --json), the TUI starts its checklist unchecked.
type Question struct {
	Kind QuestionKind
	// Warning states the problem; Ask is the question put after it.
	Warning string
	Ask     string
}

// Prompt is the whole question as the CLI asks it.
func (q Question) Prompt() string { return q.Warning + " " + q.Ask }

// Questions lists what this push must have confirmed, in the order the CLI
// asks: a foreign stamp, then the delete guard.
func (p *Prepared) Questions() []Question {
	var out []Question
	if p.State == StampForeign {
		out = append(out, Question{Kind: AskForeign, Ask: "Push anyway?",
			Warning: fmt.Sprintf("%s was last written by another push, not this machine's. Sync is one-way: push from one machine only.", p.Options.Target)})
	}
	if p.GuardTripped() {
		out = append(out, Question{Kind: AskGuard, Ask: "Continue?",
			Warning: fmt.Sprintf("This push deletes %d of %d files in the mirror (over sync.max_delete_pct).", len(p.Deleted), p.MirrorFiles)})
	}
	return out
}

// ErrBusy means another ws sync push holds the lock.
var ErrBusy = errors.New("another ws sync push is running")

// Push runs one push end to end for a front end that asked its questions
// earlier: it checks, takes the lock, prepares afresh and refuses (Questions)
// any question not in approved, so a mirror that changed since the user
// answered is asked about again instead of pushed to.
func Push(o Options, approved []QuestionKind) (Result, error) {
	if err := Check(o); err != nil {
		return Result{}, err
	}
	unlock, busy, err := Lock(o)
	if err != nil {
		return Result{}, err
	}
	if busy {
		return Result{}, ErrBusy
	}
	defer unlock()
	p, err := Prepare(o)
	if err != nil {
		return Result{}, err
	}
	defer p.Close()
	for _, q := range p.Questions() {
		ok := false
		for _, a := range approved {
			ok = ok || a == q.Kind
		}
		if !ok {
			return Result{}, fmt.Errorf("the mirror changed since you looked (%s) — reload and push again", strings.TrimSuffix(q.Warning, "."))
		}
	}
	return p.Run()
}
