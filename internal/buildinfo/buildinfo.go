// Package buildinfo is what `ws version` prints about this build: the
// version the build stamped, the ws.json schema it writes, and where it runs.
// The CLI and the TUI's Workspace screen share it. The version itself is
// stamped into cmd.appVersion by the linker and handed in.
package buildinfo

import (
	"runtime"

	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

// Info is one build of ws.
type Info struct {
	Version      string `json:"version"`
	ConfigSchema int    `json:"config_schema"`
	Platform     string `json:"platform"`
	GoVersion    string `json:"go_version"`
}

// New describes this build, stamped with version.
func New(version string) Info {
	return Info{
		Version:      version,
		ConfigSchema: wsconfig.CurrentSchema,
		Platform:     runtime.GOOS + "/" + runtime.GOARCH,
		GoVersion:    runtime.Version(),
	}
}
