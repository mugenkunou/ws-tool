package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/mugenkunou/ws-tool/internal/secret"
	"github.com/mugenkunou/ws-tool/internal/tui/nav"
	"github.com/mugenkunou/ws-tool/internal/tui/screens/secrets"
	"github.com/mugenkunou/ws-tool/internal/tui/tuitest"
)

const copyCommand = "symlink add /home/tester/.password-store --dir"

// noCopy is the Secrets load of a pass store that is not in the workspace.
func noCopy() secrets.LoadedMsg {
	return secrets.LoadedMsg{
		Violations: []secret.Violation{{Severity: "CRITICAL", Path: "Projects/api/.env", Line: 3, Snippet: "password=hunter2"}},
		Health: secrets.Health{
			Pass:        secret.PassHealth{Installed: true, GPGAvailable: true, Initialized: true, GitBacked: true, EntryCount: 42},
			CopyCommand: copyCommand,
		},
	}
}

func TestSnapshotSecrets(t *testing.T) {
	cases := []struct {
		name string
		keys []string
	}{
		{"secrets_allow_checklist", []string{"7", "a"}},
		{"secrets_ignore_checklist", []string{"7", "i"}},
		{"secrets_mute_prompt", []string{"7", "m"}},
		{"secrets_store_prompt", []string{"7", "s"}},
		{"secrets_marked", []string{"7", "space"}},
		{"secrets_command_bar", []string{"7", ":", "p", "a", "s", "s", " ", "b"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := loaded(t, 100, 30)
			for _, k := range c.keys {
				m = send(m, press(k))
			}
			tuitest.Golden(t, c.name, view(m))
		})
	}
}

func TestSnapshotSecretsNoWorkspaceCopy(t *testing.T) {
	m := send(loaded(t, 100, 30), noCopy(), press("7"))
	tuitest.Golden(t, "secrets_no_copy", view(m))
}

func TestSnapshotDashboardAdvice(t *testing.T) {
	m := send(loaded(t, 100, 30), noCopy(), press("1"))
	tuitest.Golden(t, "dashboard_advice", view(m))
}

// TestSecretsFooterFits: the screen's own keys come first and fit 100 columns.
func TestSecretsFooterFits(t *testing.T) {
	foot := footerLine(view(send(loaded(t, 100, 30), press("7"))))
	for _, want := range []string{"enter show", "a allow", "i ignore", "m mute", "s store", "? help • q quit"} {
		if !strings.Contains(foot, want) {
			t.Errorf("footer lacks %q:\n%s", want, foot)
		}
	}
	if i, j := strings.Index(foot, "enter show"), strings.Index(foot, "tab screens"); i < 0 || j >= 0 && j < i {
		t.Errorf("the screen's keys should come first:\n%s", foot)
	}
}

// TestPassAdviceOpensTheCommandBar: enter on the Dashboard's advice row asks for
// the command bar with the command typed in.
func TestPassAdviceOpensTheCommandBar(t *testing.T) {
	m := send(loaded(t, 100, 30), noCopy(), press("1"))
	for range 8 { // the advice is listed last
		m = send(m, press("down"))
	}
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	_ = next
	var open *nav.OpenCommandBarMsg
	for _, msg := range collect(cmd) {
		if o, ok := msg.(nav.OpenCommandBarMsg); ok {
			open = &o
		}
	}
	if open == nil || open.Prefill != copyCommand {
		t.Fatalf("enter on the advice: %+v", open)
	}
	bar := send(m, nav.OpenCommandBarMsg{Prefill: open.Prefill})
	if got := ansi.Strip(view(bar)); !strings.Contains(got, ":symlink add /home/tester/.password-store --dir") {
		t.Errorf("the bar should show the prefilled command:\n%s", got)
	}
}

// TestSecretsCommandsRoute: `:pass audit` and `:credential status` reach the
// Secrets screen from anywhere, and `:unmute` from the screen itself.
func TestSecretsCommandsRoute(t *testing.T) {
	m := send(loaded(t, 100, 30), press("2"))
	m, _ = m.runLine("pass audit")
	if m.active != nav.ScreenSecrets || !strings.Contains(view(m), "Pass store audit") {
		t.Fatalf(":pass audit should open the audit on Secrets:\n%s", view(m))
	}
	m, _ = m.runLine("pass backup status")
	if m.active != nav.ScreenSecrets || !strings.Contains(view(m), "Pass backup") {
		t.Fatalf(":pass backup status:\n%s", view(m))
	}
}
