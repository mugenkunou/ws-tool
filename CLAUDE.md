# Agent instructions

## Temp directories

Always use `~/tmp` for any temporary directory or file. Never use `/tmp` or `~/.cache`.

`/tmp` is mounted noexec here, so `go test` fails with `fork/exec ... permission denied`. Run Go tests like this:

    GOTMPDIR=~/tmp go test ./...
