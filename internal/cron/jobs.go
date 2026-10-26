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

// zohoSyncJob is mega-sync for Zoho WorkDrive. The launcher is a shell wrapper
// that sleeps 5s (30s with -sstart, the autostart flag that starts in the tray
// instead of opening a window) and then execs zohoworkdrived, so the window
// below includes that start-up delay. Process names are matched with -x, never
// -f: the job's own `bash -c` command line contains these names and -f would
// kill it. The kernel reports a script by its file name, so -x zohoworkdrive is
// the wrapper, and QtWebEngineProcess is cut to 15 characters.
var zohoSyncJob = &BuiltinJob{
	Name:         "zoho-sync",
	Schedule:     "*/30 * * * *",
	IntervalSecs: 30 * 60,
	Description:  "Restart Zoho WorkDrive for a 5-minute sync window",
	NeedsDisplay: true,
	scriptBody: func(wsBin, display, workspace string) string {
		if display == "" {
			display = ":1"
		}
		return fmt.Sprintf(
			`export DISPLAY=%s
stop() { pkill -x zohoworkdrive; pkill -x zohoworkdrived; pkill -x QtWebEngineProc; }
launcher="$HOME/.zohoworkdrive/bin/zohoworkdrive"
if [ ! -x "$launcher" ]; then
    echo "Zoho WorkDrive is not installed: $launcher not found"
    exit 1
fi
if pgrep -x zohoworkdrived > /dev/null 2>&1; then
    echo "Zoho WorkDrive already running, killing it..."
    stop
    sleep 2
fi
echo "Starting Zoho WorkDrive..."
"$launcher" -sstart &
sleep 300
echo "Stopping Zoho WorkDrive..."
stop || true`,
			display,
		)
	},
}

var syncPushJob = &BuiltinJob{
	Name:         "sync-push",
	Schedule:     "*/30 * * * *",
	IntervalSecs: 30 * 60,
	Description:  "Mirror the workspace into sync.mirror.target (ws sync push)",
	scriptBody: func(wsBin, display, workspace string) string {
		return fmt.Sprintf("%s --workspace %s sync push --quiet", wsBin, workspace)
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
