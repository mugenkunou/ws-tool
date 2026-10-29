package skill

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	// The agent-tmux installed here must not add a managed row to every scan;
	// tests that want a provider set Providers themselves.
	Providers = nil
	os.Exit(m.Run())
}
