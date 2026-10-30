package app

import (
	"slices"
	"strings"
	"testing"

	"github.com/mugenkunou/ws-tool/internal/tui/nav"
	"github.com/mugenkunou/ws-tool/internal/tui/tuitest"
)

// TestCronCommands: the `.` menu is the spec's §6.5 list.
func TestCronCommands(t *testing.T) {
	want := []string{"apply"}
	if got := commandNames(loaded(t, 100, 30), nav.ScreenCron); !slices.Equal(got, want) {
		t.Errorf("cron commands = %v, want %v", got, want)
	}
}

// TestCronScopes: every key declares the scope of the spec's Rows column.
func TestCronScopes(t *testing.T) {
	want := []string{"enter:cursor", "a:selected", "ctrl+d:selected", "r:selected"}
	if got := declaredScopes(t, loaded(t, 100, 30), nav.ScreenCron); !slices.Equal(got, want) {
		t.Errorf("cron declares %v, want %v", got, want)
	}
}

// TestCronRunIsACheckedOneActionChecklist: r runs `cron run <job>` through a
// checklist, on the cursor row when nothing is marked.
func TestCronRunIsACheckedOneActionChecklist(t *testing.T) {
	m := send(loaded(t, 100, 30), press("8"), press("r"))
	v := tuitest.Plain(view(m))
	if !strings.Contains(v, "[x] Run pass-backup now") || strings.Contains(v, "repo-sync") {
		t.Fatalf("r on the cursor row:\n%s", v)
	}
	tuitest.Golden(t, "cron_run_checklist", view(m))
}

func TestCronEnterShowsOutputAndEscReturns(t *testing.T) {
	m := send(loaded(t, 100, 30), press("8"), press("enter"))
	if v := tuitest.Plain(view(m)); !strings.Contains(v, "Cron log: pass-backup") {
		t.Fatalf("enter did not open the job's output:\n%s", v)
	}
	m = send(m, press("esc"))
	if m.active != nav.ScreenCron || strings.Contains(tuitest.Plain(view(m)), "Cron log:") {
		t.Fatalf("esc did not return to the list:\n%s", tuitest.Plain(view(m)))
	}
}

func TestCronNoHardcodedKeyNames(t *testing.T) { assertNoHardcodedKeyNames(t, "cron/cron.go") }
