package app

import (
	"strings"
	"testing"

	"github.com/mugenkunou/ws-tool/internal/tui/tuitest"
)

// TestSnapshotSecrets captures the finding views and actions.
func TestSnapshotSecrets(t *testing.T) {
	cases := []struct {
		name string
		keys []string
	}{
		{"secrets_allow_checklist", []string{"7", "a"}},
		{"secrets_ignore_checklist", []string{"7", "i"}},
		{"secrets_mute_checklist", []string{"7", "m"}},
		{"secrets_marked", []string{"7", "space"}},
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

// TestSecretsFooterFits: the screen's own keys come first and fit 100 columns.
func TestSecretsFooterFits(t *testing.T) {
	foot := footerLine(view(send(loaded(t, 100, 30), press("7"))))
	for _, want := range []string{"enter show", "a allow", "i ignore", "m mute", "? help • q quit"} {
		if !strings.Contains(foot, want) {
			t.Errorf("footer lacks %q:\n%s", want, foot)
		}
	}
	if i, j := strings.Index(foot, "enter show"), strings.Index(foot, "tab screens"); i < 0 || j >= 0 && j < i {
		t.Errorf("the screen's keys should come first:\n%s", foot)
	}
}
