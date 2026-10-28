package workspace

import (
	"os"

	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

// PathOverrides carries explicit path choices (e.g. from --workspace).
// Empty fields fall back to the standard resolution.
type PathOverrides struct {
	Workspace string
}

// Paths holds the resolved, absolute locations of a workspace's key files.
type Paths struct {
	Workspace string // absolute path to the workspace root
	Config    string // absolute path to ws.json
}

// ResolvePaths resolves the workspace and config paths without side effects:
// override → env → default, all absolute.
//
// Resolution:
//   - Workspace: --workspace flag → $WS_WORKSPACE → ~/Workspace
//   - Config: always <workspace>/ws.json
//
// This is a clean-break implementation: there is no fallback to the old
// XDG config (~/.config/ws-tool/config.json) or the old ws/manifest.json.
// The workspace root is only ever changed via the --workspace flag or the
// $WS_WORKSPACE environment variable; no config file stores it.
func ResolvePaths(o PathOverrides) (Paths, error) {
	// Expand any provided override paths.
	if o.Workspace != "" {
		abs, err := wsconfig.ExpandUserPath(o.Workspace)
		if err != nil {
			return Paths{}, err
		}
		o.Workspace = abs
	}

	// Resolve workspace root.
	workspacePath := o.Workspace
	if workspacePath == "" {
		workspacePath = os.Getenv("WS_WORKSPACE")
	}
	if workspacePath == "" {
		def, err := wsconfig.DefaultWorkspace()
		if err != nil {
			return Paths{}, err
		}
		workspacePath = def
	} else {
		abs, err := wsconfig.ExpandUserPath(workspacePath)
		if err != nil {
			return Paths{}, err
		}
		workspacePath = abs
	}

	// Resolve config path.
	configPath := wsconfig.ConfigPath(workspacePath)

	return Paths{Workspace: workspacePath, Config: configPath}, nil
}

// ConfigExists reports whether the config file exists at path.
func ConfigExists(configPath string) bool {
	info, err := os.Stat(configPath)
	return err == nil && !info.IsDir()
}

// Home is a place outside the workspace that scripts use, as `ws layout`
// lists it and `ws env` exports it.
type Home struct {
	Name    string `json:"name"`
	Var     string `json:"var"`
	Path    string `json:"path"`
	Purpose string `json:"purpose"`
}

// EnvVar is one line of `ws env`.
type EnvVar struct {
	Name, Value string
}

// ScriptHomes resolves the homes outside the workspace the way the fallback
// in scripts/lib/common.sh does, so a script gets the same paths with or
// without ws:
//
//	WS_TMP=${TMPDIR:-$HOME/tmp}
//	WS_CACHE=${XDG_CACHE_HOME:-$HOME/.cache}/ws
//	WS_RUN=${XDG_RUNTIME_DIR:-$HOME/.local/state}
//	WS_SCRATCH=<ws.json scratch.root_dir, resolved>   (common.sh: $HOME/Scratch)
//
// getenv is os.Getenv outside tests; an empty value counts as unset, as with
// the shell's :-.
func ScriptHomes(getenv func(string) string, scratchRoot string) []Home {
	or := func(name, def string) string {
		if v := getenv(name); v != "" {
			return v
		}
		return def
	}
	home := getenv("HOME")
	return []Home{
		{"temp", "WS_TMP", or("TMPDIR", home+"/tmp"), "scripts make their private temp dir (WS_TMPD) here"},
		{"cache", "WS_CACHE", or("XDG_CACHE_HOME", home+"/.cache") + "/ws", "regenerable caches, one <system>-<instance>/ each"},
		{"session", "WS_RUN", or("XDG_RUNTIME_DIR", home+"/.local/state"), "short-lived runtime and session files"},
		{"scratch", "WS_SCRATCH", scratchRoot, "throwaway work and captures (ws scratch)"},
	}
}

// ScriptEnv is what `ws env` prints: WS_ROOT, then every home's variable.
func ScriptEnv(workspacePath string, homes []Home) []EnvVar {
	out := []EnvVar{{"WS_ROOT", workspacePath}}
	for _, h := range homes {
		out = append(out, EnvVar{h.Var, h.Path})
	}
	return out
}
