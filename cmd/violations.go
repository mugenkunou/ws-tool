package cmd

import (
	"github.com/mugenkunou/ws-tool/internal/style"
)

// ── path conversion ──
//
// Ignore and secret violations carry workspace-relative paths internally
// (rule matching needs them). Every renderer converts a copy to absolute paths
// first, for both text and --json output (spec "Path Rules").

func absPaths(workspacePath string, ps []string) []string {
	if ps == nil {
		return nil
	}
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = style.AbsPath(workspacePath, p)
	}
	return out
}

// ── violation printers ──
//
// Printers expect violations already converted with absIgnoreViolations /
// absSecretViolations.
//
// Each printer accepts an "indent" flag:
//   indent=true  → used by aggregate views (2-space indent, section header)
//   indent=false → used by ws ignore scan / ws secret scan / etc. (no indent, no header)
