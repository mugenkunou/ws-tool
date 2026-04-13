package tui

import (
	"strings"
	"testing"
)

// All tests use non-TTY readers (strings.NewReader) so they exercise the
// readLine fallback path. The raw TTY path requires a real terminal.

func TestGhostInputFallbackEmpty(t *testing.T) {
	gi := &GhostInput{Prompt: "Name", Entries: []string{"foo.2026-04", "bar.2026-04"}}
	var out strings.Builder
	name, err := gi.Run(strings.NewReader("\n"), &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "" {
		t.Fatalf("expected empty name, got %q", name)
	}
}

func TestGhostInputFallbackName(t *testing.T) {
	gi := &GhostInput{Prompt: "Name", Entries: []string{"foo.2026-04"}}
	var out strings.Builder
	name, err := gi.Run(strings.NewReader("my-debug\n"), &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "my-debug" {
		t.Fatalf("expected %q, got %q", "my-debug", name)
	}
}

func TestGhostInputFallbackTrimmed(t *testing.T) {
	gi := &GhostInput{Prompt: "Name", Entries: nil}
	var out strings.Builder
	name, err := gi.Run(strings.NewReader("  spaces  \n"), &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "spaces" {
		t.Fatalf("expected %q, got %q", "spaces", name)
	}
}

func TestGhostFilterEmpty(t *testing.T) {
	entries := []string{"CA-incident.2026-04", "proxy-timeout.2026-03", "dns.2026-04"}
	got := ghostFilter(entries, "")
	if len(got) != 3 {
		t.Fatalf("expected all 3 entries, got %d", len(got))
	}
}

func TestGhostFilterMatch(t *testing.T) {
	entries := []string{"CA-incident.2026-04", "proxy-timeout.2026-03", "ca-llm.2026-04"}
	got := ghostFilter(entries, "CA")
	if len(got) != 2 {
		t.Fatalf("expected 2 matches for 'CA', got %d: %v", len(got), got)
	}
	for _, e := range got {
		if !strings.Contains(strings.ToLower(ghostStrip(e)), "ca") {
			t.Errorf("unexpected entry in results: %s", e)
		}
	}
}

func TestGhostFilterNoMatch(t *testing.T) {
	entries := []string{"proxy-timeout.2026-03", "dns.2026-04"}
	got := ghostFilter(entries, "zzz")
	if len(got) != 0 {
		t.Fatalf("expected no matches, got %v", got)
	}
}

func TestGhostStripSuffix(t *testing.T) {
	cases := []struct{ in, want string }{
		{"proxy-timeout.2026-03", "proxy-timeout"},
		{"ca-debug.2026-04", "ca-debug"},
		{"no-suffix", "no-suffix"},
		{"dotted.name.2026-01", "dotted.name"},
		{"short.26-01", "short.26-01"}, // suffix too short, not stripped
	}
	for _, c := range cases {
		got := ghostStrip(c.in)
		if got != c.want {
			t.Errorf("ghostStrip(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestGhostInputFallbackEOF(t *testing.T) {
	gi := &GhostInput{Prompt: "Name", Entries: nil}
	var out strings.Builder
	// EOF with no data returns empty string, no error
	name, err := gi.Run(strings.NewReader(""), &out)
	if err != nil {
		t.Fatalf("unexpected error on EOF: %v", err)
	}
	if name != "" {
		t.Fatalf("expected empty on EOF, got %q", name)
	}
}

func TestGhostFilterMultipleTokens(t *testing.T) {
	entries := []string{"proxy-timeout.2026-03", "proxy-debug.2026-04", "dns-timeout.2026-04"}
	got := ghostFilter(entries, "proxy timeout")
	if len(got) != 1 || got[0] != "proxy-timeout.2026-03" {
		t.Fatalf("expected [proxy-timeout.2026-03], got %v", got)
	}
}

func TestGhostFilterReversedTokens(t *testing.T) {
	entries := []string{"proxy-timeout.2026-03", "proxy-debug.2026-04", "dns-timeout.2026-04"}
	got := ghostFilter(entries, "timeout proxy")
	if len(got) != 1 || got[0] != "proxy-timeout.2026-03" {
		t.Fatalf("expected [proxy-timeout.2026-03], got %v", got)
	}
}

func TestGhostFilterSingleTokenUnchanged(t *testing.T) {
	entries := []string{"CA-incident.2026-04", "proxy-timeout.2026-03", "ca-llm.2026-04"}
	got := ghostFilter(entries, "CA")
	if len(got) != 2 {
		t.Fatalf("expected 2 matches for single token 'CA', got %d: %v", len(got), got)
	}
}

func TestGhostFilterWhitespaceOnly(t *testing.T) {
	entries := []string{"alpha", "beta"}
	got := ghostFilter(entries, "   ")
	if len(got) != 2 {
		t.Fatalf("expected all entries for whitespace-only input, got %d", len(got))
	}
}
