package credential

import (
	"fmt"
	"os"
	"strings"
)

// Helper states, as HelperCheck reports them.
const (
	HelperConnected    = "connected"    // a ws helper whose wrapper or binary exists
	HelperStale        = "stale"        // a ws helper that is missing or written in an old broken form
	HelperDisconnected = "disconnected" // empty, or not ws
)

// IsWsHelper reports whether a credential.helper value points at ws, either
// through the stable wrapper or by calling the binary directly.
func IsWsHelper(v string) bool {
	return strings.Contains(v, "ws git-credential-helper") || strings.Contains(v, "ws-credential-helper")
}

// HelperCheck classifies a configured credential.helper value as HelperConnected,
// HelperStale or HelperDisconnected. detail says why when it is not connected.
func HelperCheck(helperValue string) (state, detail string) {
	if helperValue == "" {
		return HelperDisconnected, ""
	}
	if !IsWsHelper(helperValue) {
		return HelperDisconnected, "(not a ws helper)"
	}

	// Extract binary/script path from "!<path>" or "!<path> git-credential-helper"
	binPath := strings.TrimPrefix(helperValue, "!")
	if idx := strings.Index(binPath, " git-credential-helper"); idx >= 0 {
		binPath = binPath[:idx]
	}

	// Older setups wrote "!<wrapper> git-credential-helper"; the wrapper then
	// passed the subcommand twice and every lookup failed.
	if strings.HasSuffix(binPath, "ws-credential-helper") && binPath != helperValue[1:] {
		return HelperStale, "wrapper called with a duplicate subcommand"
	}

	if _, err := os.Stat(binPath); err != nil {
		return HelperStale, fmt.Sprintf("binary not found: %s", binPath)
	}

	return HelperConnected, ""
}
