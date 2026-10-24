package app

import (
	"slices"
	"strings"
	"testing"

	"github.com/mugenkunou/ws-tool/internal/tui/nav"
	"github.com/mugenkunou/ws-tool/internal/tui/tuitest"
)

// TestIgnoreCommands: the `:` commands are the spec's §6.5 list.
func TestIgnoreCommands(t *testing.T) {
	want := []string{"check", "lint", "rm", "tree"}
	if got := commandNames(loaded(t, 100, 30), nav.ScreenIgnore); !slices.Equal(got, want) {
		t.Errorf("ignore commands = %v, want %v", got, want)
	}
}

// TestIgnoreScopes: every key declares the scope of the spec's Rows column.
func TestIgnoreScopes(t *testing.T) {
	want := []string{
		"i:selected", "m:selected", "s:selected", "ctrl+d:selected", "e:global", "v:global", // Warnings
		"i:selected", "u:selected", "e:global", "v:global", // Muted
		"ctrl+d:selected", "e:global", "v:global", // Ignored
	}
	if got := declaredScopes(t, loaded(t, 100, 30), nav.ScreenIgnore); !slices.Equal(got, want) {
		t.Errorf("ignore declares %v, want %v", got, want)
	}
}

// TestIgnoreViewsCycle: v shows Warnings, Muted, Ignored and back, under one
// header with the three counts.
func TestIgnoreViewsCycle(t *testing.T) {
	m := send(loaded(t, 100, 30), press("6"))
	for _, only := range []string{"dump.log", "node_modules", ".env.local", "dump.log"} {
		v := tuitest.Plain(view(m))
		if !strings.Contains(v, "Warnings 2 · Muted 1 · Ignored 2") || !strings.Contains(v, only) {
			t.Fatalf("view should show %q under the three counts:\n%s", only, v)
		}
		m = send(m, press("v"))
	}
}

// TestIgnoreKeysPerView: each view's footer names its own keys and no other's.
func TestIgnoreKeysPerView(t *testing.T) {
	m := send(loaded(t, 220, 30), press("6"))
	for _, c := range []struct{ has, not []string }{
		{[]string{"i ignore", "m mute", "s scratch", "ctrl+d delete", "e edit rules", "v next view"}, []string{"unmute", "stop ignoring"}},
		{[]string{"i ignore", "u unmute", "e edit rules", "v next view"}, []string{"m mute", "scratch", "ctrl+d"}},
		{[]string{"ctrl+d stop ignoring", "e edit rules", "v next view"}, []string{"i ignore", "m mute", "unmute", "scratch"}},
	} {
		foot := footerLine(view(m))
		for _, w := range c.has {
			if !strings.Contains(foot, w) {
				t.Errorf("footer lacks %q:\n%s", w, foot)
			}
		}
		for _, w := range c.not {
			if strings.Contains(foot, w) {
				t.Errorf("footer has %q:\n%s", w, foot)
			}
		}
		m = send(m, press("v"))
	}
}

func TestIgnoreNoHardcodedKeyNames(t *testing.T) { assertNoHardcodedKeyNames(t, "ignore/ignore.go") }
