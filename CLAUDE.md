# Agent instructions

## Temp directories

Always use `~/tmp` for any temporary directory or file. Never use `/tmp` or `~/.cache`.

`/tmp` is mounted noexec here, so `go test` fails with `fork/exec ... permission denied`. Run Go tests like this:

    GOTMPDIR=~/tmp go test ./...

## Changing the CLI or the TUI

Every CLI command, flag and noun needs a TUI decision in the same change. Tests in
`cmd/parity_test.go` and `internal/tui/app/coverage_test.go` fail until it is made, and their
messages name the file to edit:

- New command: a row in `cmd/commands.go` (commands outside the table do not run), then an
  entry in `internal/tui/app/coverage.go` on the screen that owns its noun.
- New flag: a line in `internal/tui/app/flags.go`.
- New noun or screen: follow HANDBOOK.md §9.10 "Adding a screen". Each screen builds its own
  Dashboard card in `card.go`; never add per-screen code to the Dashboard.
- Logic shared by CLI and TUI goes in `internal/`, never in `cmd/`.
