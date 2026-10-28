package cmd

import (
	"os"

	"github.com/mugenkunou/ws-tool/internal/workspace"
	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

// wsEnvValues is what `ws env` prints, for `skill check` to hold
// scripts/lib/common.sh's fallbacks against.
func wsEnvValues(home, workspacePath string, cfg wsconfig.WsConfig) map[string]string {
	getenv := func(name string) string {
		if name == "HOME" {
			return home
		}
		return os.Getenv(name)
	}
	scratch, err := wsconfig.ResolvePath(workspacePath, cfg.Scratch.RootDir)
	if err != nil {
		scratch = ""
	}
	vals := map[string]string{}
	for _, v := range workspace.ScriptEnv(workspacePath, workspace.ScriptHomes(getenv, scratch)) {
		if v.Value != "" {
			vals[v.Name] = v.Value
		}
	}
	return vals
}
