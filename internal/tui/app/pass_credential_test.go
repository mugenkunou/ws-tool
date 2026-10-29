package app

import (
	"testing"

	"github.com/mugenkunou/ws-tool/internal/tui/nav"
	"github.com/mugenkunou/ws-tool/internal/tui/tuitest"
)

// TestSnapshotPassAndCredentials captures the two screens split out of Secrets.
func TestSnapshotPassAndCredentials(t *testing.T) {
	for _, c := range []struct {
		name   string
		screen nav.Screen
	}{
		{"pass", nav.ScreenPass},
		{"credential", nav.ScreenCredential},
	} {
		t.Run(c.name, func(t *testing.T) {
			tuitest.Golden(t, c.name, view(send(loaded(t, 100, 30), jump(c.screen))))
		})
	}
}
