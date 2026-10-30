package nav

import "github.com/mugenkunou/ws-tool/internal/versions"

// VersionsMsg carries the latest known ws and agent-tmux versions. The app
// emits it once the background lookup lands; a screen that forced a check
// returns it too. The app keeps the newest one for its footer, and every
// component sees it as a broadcast.
type VersionsMsg struct{ Report versions.Report }
