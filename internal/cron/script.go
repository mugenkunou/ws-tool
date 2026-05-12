package cron

import "fmt"

// GenerateScript returns the content of the wrapper bash script for a job.
//
// The script:
//  1. Redirects all output (stdout + stderr) to the shared log file.
//  2. Rotates the log when it exceeds 5 MB (keeps last 2.5 MB).
//  3. Performs missed-run recovery: skips execution if the last run was within
//     half the job's interval (used by the @reboot crontab entry so the job
//     does not fire redundantly on a normal boot).
//  4. Runs the job-specific body in a subshell, capturing exit code.
//  5. Appends a state record "<name> <timestamp> <exit_code>" to the state file.
func GenerateScript(job *BuiltinJob, wsBin, display, workspace, statePath, logPath string) string {
	body := job.scriptBody(wsBin, display, workspace)
	return fmt.Sprintf(`#!/usr/bin/env bash
# ws-managed cron job: %s
# DO NOT EDIT — managed by 'ws cron add'

WS_JOB=%q
WS_INTERVAL_SECS=%d
WS_STATE=%q
WS_LOG=%q
WS_MAX_LOG_BYTES=5242880
WS_HALF_LOG_BYTES=2621440

# Redirect all output to the shared log file.
exec >> "$WS_LOG" 2>&1

# ── log helper ────────────────────────────────────────────────────────────────
_ws_log() { printf '%%s [%%s] %%s\n' "$(date -u +"%%Y-%%m-%%dT%%H:%%M:%%SZ")" "$WS_JOB" "$*"; }

# ── log rotation (keep last half when over limit) ─────────────────────────────
if [ -f "$WS_LOG" ]; then
    LOG_SIZE=$(stat -c %%s "$WS_LOG" 2>/dev/null || echo 0)
    if [ "$LOG_SIZE" -gt "$WS_MAX_LOG_BYTES" ]; then
        tail -c "$WS_HALF_LOG_BYTES" "$WS_LOG" > "${WS_LOG}.tmp" && mv "${WS_LOG}.tmp" "$WS_LOG"
        _ws_log "Log rotated (was ${LOG_SIZE} bytes)"
    fi
fi

# ── missed-run recovery ───────────────────────────────────────────────────────
# When the machine resumes or reboots, the @reboot crontab entry fires this
# script. We skip if the last successful run was within half the interval
# (indicating the machine was not suspended long enough to miss a cycle).
LAST_LINE=$(grep "^$WS_JOB " "$WS_STATE" 2>/dev/null | tail -1)
if [ -n "$LAST_LINE" ]; then
    LAST_TS=$(echo "$LAST_LINE" | awk '{print $2}')
    NOW_EPOCH=$(date +%%s)
    LAST_EPOCH=$(date -d "$LAST_TS" +%%s 2>/dev/null || echo 0)
    ELAPSED=$(( NOW_EPOCH - LAST_EPOCH ))
    HALF=$(( WS_INTERVAL_SECS / 2 ))
    if [ "$ELAPSED" -lt "$HALF" ]; then
        _ws_log "Skipping: last run ${ELAPSED}s ago (< half-interval ${HALF}s)"
        exit 0
    fi
fi

# ── job body ──────────────────────────────────────────────────────────────────
_ws_log "Starting"
EXIT_CODE=0

(
%s
) || EXIT_CODE=$?

# ── state update ──────────────────────────────────────────────────────────────
NOW_TS=$(date -u +"%%Y-%%m-%%dT%%H:%%M:%%SZ")
mkdir -p "$(dirname "$WS_STATE")"
echo "$WS_JOB $NOW_TS $EXIT_CODE" >> "$WS_STATE"

if [ "$EXIT_CODE" -eq 0 ]; then
    _ws_log "Done (exit 0)"
else
    _ws_log "Failed (exit $EXIT_CODE)"
fi

exit $EXIT_CODE
`,
		job.Name,
		job.Name,
		job.IntervalSecs,
		statePath,
		logPath,
		body,
	)
}
