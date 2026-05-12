package cron

import "fmt"

var megaSyncJob = &BuiltinJob{
	Name:         "mega-sync",
	Schedule:     "*/30 * * * *",
	IntervalSecs: 30 * 60,
	Description:  "Restart megasync for a 5-minute sync window every 30 minutes",
	scriptBody: func(wsBin, display, workspace string) string {
		if display == "" {
			display = ":1"
		}
		return fmt.Sprintf(
			`export DISPLAY=%s
if pgrep megasync > /dev/null 2>&1; then
    echo "Megasync already running, killing it..."
    pkill megasync
fi
echo "Starting Megasync..."
megasync &
sleep 300
echo "Stopping Megasync..."
pkill megasync || true`,
			display,
		)
	},
}

var dotfileSyncJob = &BuiltinJob{
	Name:         "dotfile-sync",
	Schedule:     "0 * * * *",
	IntervalSecs: 60 * 60,
	Description:  "Commit and push dotfiles to remote git (requires dotfile.git.enabled=true)",
	scriptBody: func(wsBin, display, workspace string) string {
		return fmt.Sprintf("%s --workspace %s dotfile git push --quiet", wsBin, workspace)
	},
}

var repoSyncJob = &BuiltinJob{
	Name:         "repo-sync",
	Schedule:     "*/30 * * * *",
	IntervalSecs: 30 * 60,
	Description:  "Sync workspace git fleet (pull behind, push ahead)",
	scriptBody: func(wsBin, display, workspace string) string {
		return fmt.Sprintf("%s --workspace %s repo sync --quiet", wsBin, workspace)
	},
}

var secretScanJob = &BuiltinJob{
	Name:         "secret-scan",
	Schedule:     "0 * * * *",
	IntervalSecs: 60 * 60,
	Description:  "Scan for exposed secrets; notify if violations found",
	scriptBody: func(wsBin, display, workspace string) string {
		return fmt.Sprintf("%s --workspace %s secret scan --quiet", wsBin, workspace)
	},
}

var ignoreScanJob = &BuiltinJob{
	Name:         "ignore-scan",
	Schedule:     "0 */6 * * *",
	IntervalSecs: 6 * 60 * 60,
	Description:  "Scan workspace sync hygiene (bloat, depth, project-meta) every 6 hours",
	scriptBody: func(wsBin, display, workspace string) string {
		return fmt.Sprintf("%s --workspace %s ignore scan --quiet", wsBin, workspace)
	},
}

var logPruneJob = &BuiltinJob{
	Name:         "log-prune",
	Schedule:     "0 2 * * *",
	IntervalSecs: 24 * 60 * 60,
	Description:  "Evict old log sessions that exceed log.cap_mb (nightly at 02:00)",
	scriptBody: func(wsBin, display, workspace string) string {
		return fmt.Sprintf("%s --workspace %s log prune --quiet", wsBin, workspace)
	},
}

var scratchPruneJob = &BuiltinJob{
	Name:         "scratch-prune",
	Schedule:     "0 3 * * 0",
	IntervalSecs: 7 * 24 * 60 * 60,
	Description:  "Remove scratch dirs older than scratch.prune_after_days (weekly, Sunday 03:00)",
	scriptBody: func(wsBin, display, workspace string) string {
		return fmt.Sprintf("%s --workspace %s scratch prune --quiet", wsBin, workspace)
	},
}
