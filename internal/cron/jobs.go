package cron

import "fmt"

var megaSyncJob = &BuiltinJob{
	Name:         "mega-sync",
	Schedule:     "*/30 * * * *",
	IntervalSecs: 30 * 60,
	Description:  "Restart megasync for a 5-minute sync window",
	NeedsDisplay: true,
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

var passBackupJob = &BuiltinJob{
	Name:         "pass-backup",
	Schedule:     "*/30 * * * *",
	IntervalSecs: 30 * 60,
	Description:  "Push the pass store backup (needs ws pass backup enable)",
	scriptBody: func(wsBin, display, workspace string) string {
		return fmt.Sprintf("%s --workspace %s pass backup push --quiet", wsBin, workspace)
	},
}

var sessionPruneJob = &BuiltinJob{
	Name:         "session-prune",
	Schedule:     "0 2 * * *",
	IntervalSecs: 24 * 60 * 60,
	Description:  "Prune sessions over the size cap (nightly at 02:00)",
	scriptBody: func(wsBin, display, workspace string) string {
		return fmt.Sprintf("%s --workspace %s session prune --quiet", wsBin, workspace)
	},
}
