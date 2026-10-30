package credential

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHelperCheck(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "ws")
	wrapper := filepath.Join(dir, "ws-credential-helper")
	for _, p := range []string{bin, wrapper} {
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, tt := range []struct {
		name, value, state, detail string
	}{
		{"empty", "", HelperDisconnected, ""},
		{"another helper", "store", HelperDisconnected, "not a ws helper"},
		{"cache is not ws", "cache --timeout=3600", HelperDisconnected, "not a ws helper"},
		{"binary exists", "!" + bin + " git-credential-helper", HelperConnected, ""},
		{"binary gone", "!/nonexistent/ws git-credential-helper", HelperStale, "binary not found"},
		{"bare wrapper", "!" + wrapper, HelperConnected, ""},
		{"legacy wrapper form", "!" + wrapper + " git-credential-helper", HelperStale, "duplicate"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			state, detail := HelperCheck(tt.value)
			if state != tt.state || !strings.Contains(detail, tt.detail) {
				t.Errorf("HelperCheck(%q) = %q, %q; want %q containing %q", tt.value, state, detail, tt.state, tt.detail)
			}
		})
	}
}
