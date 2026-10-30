package app

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/mugenkunou/ws-tool/internal/tui/nav"
	"github.com/mugenkunou/ws-tool/internal/versions"
)

func footerReport(outdated bool) versions.Report {
	r := versions.Report{
		WS:        versions.Item{Name: "ws", Installed: "v0.21.0", State: versions.StateCurrent},
		AgentTmux: versions.Item{Name: "agent-tmux", Installed: "v0.20.0", State: versions.StateCurrent},
		CheckedAt: time.Now(),
	}
	if outdated {
		r.AgentTmux.State, r.AgentTmux.Latest = versions.StateOutdated, "v0.21.0"
	}
	return r
}

func footerAt(t *testing.T, width int, r versions.Report) string {
	t.Helper()
	m := send(newApp(t, nil), tea.WindowSizeMsg{Width: width, Height: 30}, nav.VersionsMsg{Report: r})
	return ansi.Strip(m.footer())
}

func TestFooterIsOneRowWithVersions(t *testing.T) {
	for _, w := range []int{100, 60, 40} {
		f := footerAt(t, w, footerReport(false))
		if strings.Contains(f, "\n") {
			t.Errorf("width %d: footer is more than one line: %q", w, f)
		}
		if w >= 60 && (!strings.Contains(f, "q quit") || !strings.Contains(f, "? help")) {
			t.Errorf("width %d: help lost its keys: %q", w, f)
		}
	}
	f := footerAt(t, 100, footerReport(false))
	if !strings.HasSuffix(f, "ws v0.21.0 · agent-tmux v0.20.0") || strings.Contains(f, "⚠") {
		t.Errorf("width 100 footer = %q", f)
	}
}

func TestFooterWarnsOnlyWhenOutdated(t *testing.T) {
	if f := footerAt(t, 100, footerReport(true)); !strings.HasSuffix(f, "⚠") {
		t.Errorf("outdated footer has no marker: %q", f)
	}
	if f := footerAt(t, 100, footerReport(false)); strings.Contains(f, "⚠") {
		t.Errorf("current footer has a marker: %q", f)
	}
}

func TestFooterDropsVersionsWhenNarrow(t *testing.T) {
	if f := footerAt(t, 60, footerReport(true)); strings.Contains(f, "v0.21.0") || strings.Contains(f, "⚠") {
		t.Errorf("narrow footer still shows versions: %q", f)
	}
	// Room for the short cell only.
	if f := footerAt(t, 90, footerReport(false)); !strings.Contains(f, "tmux v0.20.0") || strings.Contains(f, "agent-tmux") {
		t.Errorf("width 90 footer = %q", f)
	}
}

func TestFooterHeightMatchesBody(t *testing.T) {
	m := send(newApp(t, nil), tea.WindowSizeMsg{Width: 100, Height: 30}, nav.VersionsMsg{Report: footerReport(true)})
	if got := m.bodyHeight(); got != 30-headerHeight-1 {
		t.Errorf("bodyHeight = %d", got)
	}
}
