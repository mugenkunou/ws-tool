# Script contract

How every script in ~/Workspace/scripts looks and behaves. Paths and secrets are in [write-rules.md](write-rules.md).

## Shape

- One action per script, named `<system>-<noun>-<verb>.sh` (`dns-record-set.sh`).
- Shared code lives in `scripts/lib/common.sh` for every system and `scripts/lib/<system>.sh` for one. `common.sh` sets the home variables from write-rules.md and gives `die`, `log` and `confirm` (y/N on /dev/tty, default no). Create a lib that is missing; extend one that exists instead of copying its code into a script.
- The header comment is the documentation: one line on what the script does, then Usage lines with real examples. Agents find scripts by reading these headers, so keep them accurate. When a script usually comes before or after another, a `Next:` line names it and what it needs.
- The user runs scripts as `bash x.sh` or `python3 -I x.py`. Write them for that.
- A bash script starts with `set -euo pipefail`, then sources `lib/common.sh` and `lib/<system>.sh`.
- Nouns are positional (ZONES NAME TYPE VALUE), and options are flags.
- The default output is a table for humans. `--json` prints one JSON object per line for agents.
- Output the user keeps (results, reports, downloads) goes to a directory given with `--out DIR`, required when there is such output. It is never written to `$WS_TMPD`, which the EXIT trap removes.

## Writes

- A write dry-runs by default and shows what would change, through the system's own preview or verify endpoint when it has one.
- `--apply` performs it after a y/N on /dev/tty. `--yes` skips the question when the user already approved this run.
- A change that is already in place prints "already as asked" and exits 0.
- Right before writing, re-read the target and refuse if it changed since the preview. After writing, read it back and fail if the change is missing.
- A write to several targets previews every target, collects the failures, and saves nothing if any target failed. When the API takes all targets in one call, or the user decided to skip what fails, report each failed target and exit 1 instead.
- When the system cannot show what is already in place, the header says so, and the script relies on the per-target result instead of "already as asked".

## Sessions

- A `<system>-login.sh` reads the session from the clipboard ("Copy as cURL" in DevTools) or stdin, stores it in `$WS_RUN/<system>-<instance>/`, and prints whose session it is.
- On an expired session, the client asks once for a fresh one when a terminal is attached. Otherwise it fails with the login command to run.
- A login or prompt must also work under Claude Code's `!` prefix, where stdin is empty and not a terminal and `/dev/tty` may be missing: read stdin only when it has data, else the clipboard, and fail with a clear message instead of a `/dev/tty` error.
- A short-lived code (TFA) is the last input the script reads, after every slow lookup, from stdin or /dev/tty, never a flag value. Send every call that needs it inside the code's window, in parallel when there are several.
- When the capture has no response bodies, parse the JSON loosely, save the body to `$WS_CACHE/<system>-<instance>/` when parsing fails, and fix the parser after the first live run. Treat a field the spec took from client source or a placeholder as unconfirmed too: until a live run confirms it, a target the script cannot read is reported as unknown, with its raw body saved, never as failed.

## Bash pitfalls

- Under `pipefail`, `| head` kills the producer with SIGPIPE and fails the pipeline. Take the first N inside jq (`.[0:10][]`, `first`).
- `die` inside `$(...)` exits only the subshell. Let `set -e` catch the failed assignment, or check it.
