# Agent Instructions — ws-tool

Read and follow these rules before doing anything in this repository.

## Build & Test — MANDATORY

This machine has `/tmp` mounted with `noexec`. Direct `go test` or `go build`
will fail because Go needs to execute temp artifacts during compilation.

**Always use Make targets:**

```bash
make build   # build the binary
make test    # run all tests
make fmt     # format code
```

The Makefile sets `TMPDIR`, `GOCACHE`, and `GOTMPDIR` to repo-local directories
(`tmp/`, `.gocache/`, `.gotmp/`) automatically. These directories are gitignored.

**NEVER run any of these directly:**

```bash
# ALL WRONG — will fail or pollute the system
go test ./...
go build .
go test -v ./cmd/...
go run .
```

If you need flags beyond what `make test` provides (e.g. `-run TestFoo`, `-v`),
set the env vars yourself:

```bash
TMPDIR=$PWD/tmp GOCACHE=$PWD/.gocache GOTMPDIR=$PWD/.gotmp go test -v -run TestFoo ./cmd/...
```

## Directory Hygiene

- **Do NOT create temporary directories** outside the repo (`/tmp`, `~/.cache`,
  random locations). All temp/cache directories are repo-local and gitignored.
- **Do NOT create new top-level directories** without asking. The existing
  `tmp/`, `.gocache/`, `.gotmp/` are sufficient.
- **Do NOT modify `.gitignore`** to add new directories you created. If you find
  yourself needing a new directory, you are doing something wrong.

## Credential Helper — HANDS OFF

The user has `ws git-credential-helper setup` configured globally to point to
`/usr/local/bin/ws`. This is their production credential pipeline backed by
`pass` (Unix Password Store).

**NEVER run any of these:**

```bash
ws git-credential-helper setup       # clobbers global config with local binary path
./ws git-credential-helper setup     # same but worse — points to repo-local binary
ws git-credential-helper disconnect  # breaks credential pipeline
ws secret setup                      # modifies GPG/pass state
ws secret fix                        # modifies pass entries
```

**NEVER modify git credential configuration:**

```bash
git config --global credential.helper ...   # breaks credential pipeline
git config credential.helper ...            # same
```

**Tests are safe.** The test suite isolates `GIT_CONFIG_GLOBAL` so no test can
touch the real `~/.gitconfig`. You do not need to worry about tests clobbering
credentials — just use `make test`.

## GitHub / Network — DO NOT TOUCH

- **Do NOT authenticate to GitHub** or any other service.
- **Do NOT run commands that require network authentication** (e.g. `git push`,
  `git pull` from private repos, `pass` operations on real entries).
- **Do NOT create, modify, or interact with GitHub issues, PRs, or releases.**

## Testing Rules

1. Use `make test` to run the full suite.
2. For targeted tests: `TMPDIR=$PWD/tmp GOCACHE=$PWD/.gocache GOTMPDIR=$PWD/.gotmp go test -v -run TestName ./cmd/...`
3. Tests are designed to be hermetic — they isolate XDG, HOME-equivalent paths,
   and git config. Do not "help" by setting up credentials, tokens, or configs.
4. If a test fails because of missing credentials or network access, that is
   expected in this environment. Do not try to fix it by authenticating.

## Code Conventions

- Read `dev.md` for the full developer guide.
- All RW commands use the Action Plan pattern (`cmd/plan.go`).
- Run `make fmt` before suggesting any code changes.
- Keep test helpers in `cmd/testmain_test.go`.
