package app

import (
	"slices"
	"strings"
	"testing"

	"github.com/mugenkunou/ws-tool/internal/tui/nav"
	"github.com/mugenkunou/ws-tool/internal/tui/screens/cron"
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

// TestCronPinnedRowOffersTheApply: when `cron apply` has changes to make, the
// first row says so, and enter on it runs the same preflight and plan as `.`
// then `a`. With nothing to apply (or an unreadable crontab) there is no row.
func TestCronPinnedRowOffersTheApply(t *testing.T) {
	m := send(loaded(t, 100, 30), press("8")) // Cron
	if v := tuitest.Plain(view(m)); strings.Contains(v, "▸") {
		t.Fatalf("no pending changes, no row:\n%s", v)
	}
	jobs := cron.LoadedMsg{Pending: 2, Jobs: []cron.Job{{Name: "mega-sync", Schedule: "*/30 * * * *", State: "not applied", Declared: true, CrontabOK: true}}}
	m = send(m, jobs)
	v := tuitest.Plain(view(m))
	if !strings.Contains(v, "▸ Apply to the crontab: 2 changes") {
		t.Fatalf("no pinned apply row:\n%s", v)
	}
	m = send(m, press("up"))
	if _, cmd := sendRun(m, press("enter")); cmd == nil {
		t.Error("enter on the pinned row starts the apply (the crontab preflight)")
	}
	m = send(m, cron.LoadedMsg{Pending: 2, CronErr: errString("crontab: command not found")})
	if v := tuitest.Plain(view(m)); strings.Contains(v, "▸") {
		t.Errorf("an unreadable crontab has nothing to apply:\n%s", v)
	}
}
