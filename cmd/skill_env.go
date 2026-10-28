package cmd

import (
	"os"
	"path/filepath"

	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

// wsEnvValues is what `ws env` prints, for `skill check` to hold
// scripts/lib/common.sh's fallbacks against. `ws env` is not in this tree yet,
// so the values are computed here from the same sources; once it lands, this
// function should call its code instead and nothing else needs to change.
func wsEnvValues(home, workspacePath string, cfg wsconfig.WsConfig) map[string]string {
	or := func(env, def string) string {
		if v := os.Getenv(env); v != "" {
			return v
		}
		return def
	}
	vals := map[string]string{
		"WS_ROOT":  workspacePath,
		"WS_TMP":   or("TMPDIR", filepath.Join(home, "tmp")),
		"WS_RUN":   or("XDG_RUNTIME_DIR", filepath.Join(home, ".local", "state")),
		"WS_CACHE": filepath.Join(or("XDG_CACHE_HOME", filepath.Join(home, ".cache")), "ws"),
	}
	if root, err := wsconfig.ResolvePath(workspacePath, cfg.Scratch.RootDir); err == nil && root != "" {
		vals["WS_SCRATCH"] = root
	}
	return vals
}
