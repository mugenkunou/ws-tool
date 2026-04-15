package notify

import "github.com/mugenkunou/ws-tool/internal/config"

// DaemonOptions holds the parameters for the daemon event loop.
type DaemonOptions struct {
	WorkspacePath string
	ConfigPath    string
	ManifestPath  string
	Cfg           config.Config
}
