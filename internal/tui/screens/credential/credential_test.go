package credential

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mugenkunou/ws-tool/internal/credential"
)

func helperRow(t *testing.T, m LoadedMsg) fact {
	t.Helper()
	for _, f := range m.facts() {
		if f.Label == "credential helper" {
			return f
		}
	}
	t.Fatal("no credential helper row")
	return fact{}
}

// loadedFor is what Load makes of a helper value, without running git.
func loadedFor(helper string) LoadedMsg {
	m := LoadedMsg{Helper: helper}
	m.State, m.Detail = credential.HelperCheck(helper)
	m.Connected = m.State == credential.HelperConnected
	return m
}

// TestHelperRowTellsConnectedFromForeign: only a working ws helper is
// connected. Another helper (or a ws one whose wrapper is gone) is a warning
// that says what it is, as `ws credential status` does.
func TestHelperRowTellsConnectedFromForeign(t *testing.T) {
	wrapper := filepath.Join(t.TempDir(), "ws-credential-helper")
	if err := os.WriteFile(wrapper, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name, helper string
		ok           bool
		value        string
	}{
		{"ws wrapper", "!" + wrapper, true, "!" + wrapper},
		{"another helper", "store", false, "set to store, not ws"},
		{"cache", "cache --timeout=3600", false, "not ws"},
		{"stale wrapper", "!/gone/ws-credential-helper", false, "stale, binary not found"},
		{"legacy wrapper form", "!" + wrapper + " git-credential-helper", false, "duplicate subcommand"},
		{"unset", "", false, "not connected"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m := loadedFor(tt.helper)
			f := helperRow(t, m)
			if f.OK != tt.ok || f.Warn == tt.ok || !strings.Contains(f.Value, tt.value) {
				t.Errorf("row = %+v; want ok=%v containing %q", f, tt.ok, tt.value)
			}
			if m.Connected != tt.ok {
				t.Errorf("Connected = %v, want %v", m.Connected, tt.ok)
			}
			_, card, _ := m.Card()
			if tt.ok != (len(card.Issues) == 0) {
				t.Errorf("card issues = %v for ok=%v", card.Issues, tt.ok)
			}
			if !tt.ok && !strings.Contains(card.Issues[0].Text, "press . then e") {
				t.Errorf("the issue should say how to fix it: %q", card.Issues[0].Text)
			}
		})
	}
}
