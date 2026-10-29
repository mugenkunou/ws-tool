package mirror

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

func TestOptionsFrom(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("HOME", "/home/u")
	cfg := wsconfig.Default()
	for target, want := range map[string]string{"": "", "  ": "", "~/MEGA/ws": "/home/u/MEGA/ws", "/abs/ws": "/abs/ws", "rel/ws": "rel/ws"} {
		cfg.Sync.Mirror.Target = target
		o, err := OptionsFrom("/ws", cfg)
		if err != nil || o.Target != want || o.Workspace != "/ws" {
			t.Errorf("target %q: got %q, %v; want %q", target, o.Target, err, want)
		}
	}
}

func TestFactsAreWhatStatusPrints(t *testing.T) {
	s := Status{Target: "/m", Stamp: StampOK, PendingChanges: 2, PendingDeletes: 1, Files: 1, Bytes: 2048}
	got := s.Facts()
	want := []Fact{
		{"Mirror", "/m"},
		{"Last push", "never (from this machine)"},
		{"Stamp", StampLabel(StampOK)},
		{"Pending", "2 to copy · 1 to delete (1 file, 2.0 KB in all)"},
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("fact %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// TestQuestionsAndPush: a foreign stamp is a question; Push refuses a question
// that was not approved, and runs once it is.
func TestQuestionsAndPush(t *testing.T) {
	needRsync(t)
	o := setup(t)
	push(t, o)
	write(t, filepath.Join(o.Workspace, "notes", "new.md"), "n")
	write(t, filepath.Join(o.Target, StampName), `{"push_id":"other","host":"laptop2"}`)

	p, err := Prepare(o)
	if err != nil {
		t.Fatal(err)
	}
	qs := p.Questions()
	p.Close()
	if len(qs) != 1 || qs[0].Kind != AskForeign || !strings.HasSuffix(qs[0].Prompt(), "Push anyway?") {
		t.Fatalf("questions = %+v", qs)
	}
	if _, err := Push(o, nil); err == nil || !strings.Contains(err.Error(), "changed since you looked") {
		t.Fatalf("unapproved push: %v", err)
	}
	if _, err := os.Stat(filepath.Join(o.Target, "notes", "new.md")); err == nil {
		t.Fatal("a refused push copied files")
	}
	res, err := Push(o, []QuestionKind{AskForeign})
	if err != nil || res.Changed != 1 {
		t.Fatalf("approved push = %+v, %v", res, err)
	}
	if p, err = Prepare(o); err != nil || p.State != StampOK || len(p.Questions()) != 0 || len(p.Changes()) != 0 {
		t.Fatalf("after push: %v, state %v", err, p.State)
	}
	p.Close()
}

func TestChangesListCopiesThenDeletes(t *testing.T) {
	p := &Prepared{Changed: []string{"a"}, Deleted: []string{"b"}}
	got := p.Changes()
	if len(got) != 2 || got[0] != (Change{"a", ActionCopy}) || got[1] != (Change{"b", ActionDelete}) {
		t.Errorf("Changes = %+v", got)
	}
	if id := Schema().ID(got[1]); id != "delete b" {
		t.Errorf("ID = %q", id)
	}
}

func TestPushWithoutATarget(t *testing.T) {
	if _, err := Push(Options{}, nil); err == nil || !strings.Contains(err.Error(), "sync.mirror.target is not set") {
		t.Errorf("Push = %v", err)
	}
}
