# `ws` Developer Guide 🛠️

Welcome, builder of sane workspaces.

This guide gets you from **fresh Linux box** → **coding** → **testing** → **publishing on GitHub** with minimal pain and maximum momentum.

---

## 1) Prerequisites (OS)

Install baseline tools first:

```bash
sudo apt update
sudo apt install -y \
  git make curl wget ca-certificates gnupg pass \
  coreutils findutils grep diffutils file util-linux sudo
```

Optional-but-useful:

```bash
sudo apt install -y jq
```

> Why these? `ws` intentionally delegates to standard Linux tools (`ln`, `find`, `grep`, `script`, `git`, etc.) instead of reimplementing them.

---

## 2) Install Go (required)

Project target: **Go 1.23+**.

### Option A — Official tarball (recommended)

```bash
cd /tmp
GO_VER="$(curl -fsSL https://go.dev/VERSION?m=text | head -n1)"
curl -LO "https://go.dev/dl/${GO_VER}.linux-amd64.tar.gz"
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf "${GO_VER}.linux-amd64.tar.gz"

echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
source ~/.bashrc

go version
```

### Option B — Use distro package

```bash
sudo apt install -y golang-go
go version
```

If distro Go is old, prefer Option A.

---

## 3) VS Code setup (plugins + settings)

Install these extensions:

- `golang.go` — Go language tooling, tests, debug
- `eamodio.gitlens` — Git history and blame
- `streetsidesoftware.code-spell-checker` — docs/readme sanity
- `yzhang.markdown-all-in-one` — markdown authoring quality
- `esbenp.prettier-vscode` — markdown/json formatting convenience

Quick install from terminal:

```bash
code --install-extension golang.go
code --install-extension eamodio.gitlens
code --install-extension streetsidesoftware.code-spell-checker
code --install-extension yzhang.markdown-all-in-one
code --install-extension esbenp.prettier-vscode
```

Recommended workspace settings (`.vscode/settings.json` if you want):

```json
{
  "go.useLanguageServer": true,
  "go.formatTool": "gofmt",
  "editor.formatOnSave": true,
  "go.testFlags": ["-v"],
  "files.insertFinalNewline": true
}
```

---

## 4) Clone and bootstrap

```bash
git clone https://github.com/mugenkunou/ws-tool.git
cd ws-tool
```

Useful first checks:

```bash
go version
make fmt
make build
./ws version
```

---

## 5) Build, run, deploy

### Build

```bash
make build
```

Binary output: `./ws`

### Run locally

```bash
./ws help
./ws version
./ws init --dry-run
```

### Deploy to your machine

```bash
sudo cp ./ws /usr/local/bin/ws
ws version
```

---

## 6) Test suite

### Standard test run

```bash
make test
```

### Verbose / targeted test run

```bash
TMPDIR=$PWD/tmp GOCACHE=$PWD/.gocache GOTMPDIR=$PWD/.gotmp go test -v -run TestFoo ./cmd/...
```

### Coverage run

```bash
TMPDIR=$PWD/tmp GOCACHE=$PWD/.gocache GOTMPDIR=$PWD/.gotmp go test -cover ./...
```

> **Why the env vars?** This machine mounts `/tmp` with `noexec`. Go needs to
> execute temp artifacts during compilation and testing. The Makefile sets
> `TMPDIR`, `GOCACHE`, and `GOTMPDIR` to repo-local directories automatically.
> When running `go test` directly (for extra flags), you **must** set these
> yourself. The directories (`tmp/`, `.gocache/`, `.gotmp/`) are gitignored.

### Test isolation guarantees

The test suite (`cmd/testmain_test.go` → `TestMain`) automatically isolates:

| Scope | Mechanism | Protects |
|---|---|---|
| XDG config | `XDG_CONFIG_HOME` → temp dir | Real `~/.config` |
| Git global config | `GIT_CONFIG_GLOBAL` → temp file | Real `~/.gitconfig` (credential helper, user settings) |
| Editor shims | Fake `code` binary in `PATH` | Tests that invoke editor don't open VS Code |

This means:
- **No test can clobber your `ws git-credential-helper setup`.** Git global
  config writes go to an isolated temp file, not your real `~/.gitconfig`.
- **No test needs network access or real credentials.** Tests that hit pass/GPG
  boundaries will degrade gracefully.
- **You never need to re-run `ws git-credential-helper setup` after tests.**

### NEVER run bare `go test` / `go build`

```bash
# ALL WRONG — will fail on noexec /tmp
go test ./...
go build .
go run .
```

Always use `make test`, `make build`, or set the env vars explicitly.

---

## 7) Daily developer flow

```bash
# 1) sync
git pull --rebase

# 2) code
# ... edit files ...

# 3) quality gate
make fmt
make test
make build

# 4) commit
git add .
git commit -m "feat: your change"

# 5) push
git push
```

---

## 8) Release pipeline

### Overview

Pushing a semver tag (`v*`) triggers the GitHub Actions release workflow, which:
1. Runs tests
2. Cross-compiles binaries (linux/darwin × amd64/arm64)
3. Generates SHA-256 checksums
4. Creates a GitHub Release with all artifacts

### Secret scanning (gitleaks)

Before every push, a pre-push git hook runs [gitleaks](https://github.com/gitleaks/gitleaks) to scan for leaked secrets. This is critical since the repo is public.

**Install gitleaks:**

```bash
go install github.com/zricethezav/gitleaks/v8@v8.21.2
```

**Install the hook:**

```bash
make hooks
```

This copies `scripts/pre-push` into `.git/hooks/`. Every `git push` will now scan for secrets first.

**Manual scan:**

```bash
gitleaks detect --source . --verbose
```

### Version injection

The binary version is injected at build time via ldflags. The `appVersion` variable in `cmd/version.go` defaults to `"dev"` and is overridden during CI/release builds.

```bash
# local build with version
make build VERSION=v0.2.0

# or directly
go build -ldflags "-s -w -X github.com/mugenkunou/ws-tool/cmd.appVersion=v0.2.0" -o ws .
```

`-s -w` strips debug symbols and DWARF tables — smaller binary, no local paths leaked.

### Tagging a release (step-by-step)

#### 1. Make sure everything is clean and passing

```bash
# check for uncommitted changes — everything must be committed first
git status

# run the full quality gate
make fmt && make test && make build
```

If anything fails, fix it before proceeding. All code must be committed and pushed.

#### 2. Pick the next version number

Check the latest tag:

```bash
git tag --sort=-v:refname | head -3
```

Then pick the next version following [semver](https://semver.org/):

- **Patch** (`v0.2.0` → `v0.2.1`) — bug fixes only, no new features.
- **Minor** (`v0.2.0` → `v0.3.0`) — new features, backward-compatible.
- **Major** (`v0.2.0` → `v1.0.0`) — breaking changes.

To see what changed since the last tag:

```bash
git log v0.2.0..HEAD --oneline
```

#### 3. Run the pre-release check

```bash
make release-check
```

This runs gitleaks (secret scan) + `go vet` + race-condition tests — the full safety gate. Do not skip this.

#### 4. Create the tag

```bash
git tag -a v0.3.0 -m "v0.3.0"
```

- `-a` creates an **annotated** tag (includes author, date, and message).
- `-m` sets the tag message. Keep it matching the version.

#### 5. Push the tag

```bash
git push origin master --follow-tags
```

`--follow-tags` pushes your commits **and** the annotated tag in one go. The pre-push hook will run gitleaks automatically before anything leaves your machine.

#### 6. Verify

Go to the GitHub repo → **Releases**. The CI workflow (`.github/workflows/release.yml`) will automatically:
1. Run tests
2. Cross-compile binaries (linux/darwin × amd64/arm64)
3. Generate SHA-256 checksums
4. Create a GitHub Release with all artifacts

You can also verify locally:

```bash
git tag --sort=-v:refname | head -3
```

#### Quick copy-paste summary

```bash
make release-check
git tag -a v0.3.0 -m "v0.3.0"
git push origin master --follow-tags
```

### CI

Every push to `main` and every PR triggers `.github/workflows/ci.yml`, which runs:
- `gofmt` check (no unformatted code)
- `go vet`
- `go test -race`
- Build verification

### Quick reference

| Command | Purpose |
|---|---|
| `make build` | Local dev build (version=dev) |
| `make build VERSION=v0.2.0` | Local build with version |
| `make test` | Run tests |
| `make release-check` | Full pre-release gate (gitleaks + vet + race tests) |
| `make hooks` | Install git pre-push hook |
| `make clean` | Remove binary |

---

## 9) Adding new commands

### RO vs RW classification

Every command is either **read-only (RO)** or **read-write (RW)**. If it writes to disk, config, manifest, or system state → RW.

- **RO commands** receive `(args, globals, stdout, stderr)` — no stdin, no prompts.
- **RW commands** receive `(args, globals, stdin, stdout, stderr)` — stdin is threaded for interactive prompts.

### RW exemptions

Some RW commands are exempt from the Plan → Confirm → Execute pattern because their writes are non-destructive and time-sensitive. These commands append-only and never modify existing data:

- `ws capture` — appends to `captures.md`. Confirmation would contradict the sub-5-second capture goal. `--dry-run` is still supported.
- `ws log stop` — stops a recording session. No meaningful "undo" to confirm.

New exemptions require strong justification in the spec. Default is always to use the Action Plan pattern.

### Action Plan pattern (required for all RW commands)

All RW commands **must** use the Action Plan pattern defined in `cmd/plan.go`. Do not use `confirm()` for new commands.

```go
func runMyCommand(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
    // 1. Parse flags, validate inputs, gather state
    // ...

    if *dryRun {
        globals.dryRun = true
    }

    // 2. Build the plan — one Action per discrete mutation
    plan := Plan{Command: "mycommand"}
    for _, item := range items {
        item := item // capture loop variable
        plan.Actions = append(plan.Actions, Action{
            ID:          fmt.Sprintf("process-%s", item.Name),
            Description: fmt.Sprintf("Process %s", item.Name),
            Execute: func() error {
                return processItem(item)
            },
        })
    }

    // 3. Execute — RunPlan handles dry-run, prompts, quiet/json auto-accept
    planResult := RunPlan(plan, stdin, stdout, globals)

    // 4. JSON output (include planResult.Actions for programmatic consumers)
    if globals.json {
        return writeJSON(stdout, stderr, "mycommand", map[string]any{
            "actions": planResult.Actions,
        })
    }

    // 5. Return appropriate exit code
    return planResult.ExitCode()
}
```

### Granularity rule

One Action per independently meaningful mutation. Ask: "would a user ever want to say yes to this but no to the next?" If yes → separate Actions.

| Command type | Granularity |
|---|---|
| Per-file operations (init, dotfile add) | One action per file |
| Fleet operations (repo pull/push/run) | One action per repo |
| Cleanup operations (log prune, scratch prune) | One action per item removed |
| Single-mutation commands (log start) | One action total |

### Prompt vocabulary

Interactive mode presents each action with `[y/n/a/q]`:
- `y` (default, Enter) — execute this action
- `n` — skip, continue to next
- `a` — accept all remaining
- `q` — quit, skip all remaining

### Testing RW commands

Tests pass `strings.NewReader("y\n")` as stdin. `promptChoice` returns the default key `"y"` on EOF, so all actions are auto-accepted in tests.

---

## 10) Testing `ws cron` manually

### Unit + integration tests (fast, no crontab touched)

```bash
TMPDIR=/home/siva-14414/tmp go test ./internal/cron/... ./cmd/... -run TestCron -v -count=1
```

### Smoke-test the CLI

```bash
# 1. List all built-in jobs and presets
ws cron ls

# 2. Preview what "add" would do — dry-run writes nothing
ws cron add mega-sync --dry-run

# 3. Actually install a job (writes script + crontab entry)
ws cron add mega-sync

# 4. Confirm the entry is in your crontab
crontab -l | grep -A5 "ws:mega-sync"

# 5. Check job status (last run, next estimated run)
ws cron status mega-sync

# 6. View recent log lines for the job
ws cron log mega-sync

# 7. Install a whole preset at once
ws cron add sync --dry-run   # preview
ws cron add sync             # install mega-sync + dotfile-sync + repo-sync

# 8. Remove a single job
ws cron rm mega-sync

# 9. Remove a preset (removes all jobs in it)
ws cron rm sync
```

### What to verify

| Check | Expected |
| --- | --- |
| `ws cron ls` | Table with 7 jobs; installed column shows `yes`/`no` |
| `ws cron add <job> --dry-run` | Prints plan with two actions (write script, install crontab); crontab unchanged |
| `ws cron add <job>` | Script written to `~/.local/share/ws/cron/<job>.sh`; two crontab lines added (schedule + `@reboot`) |
| `ws cron status` | Shows "no managed jobs installed" when none are installed |
| `ws cron rm <job>` | Crontab block removed; script file deleted |

---

## 11) Troubleshooting quick hits

- **`go: toolchain not available`**
  - Install Go 1.23+ via official tarball.
- **Build/tests fail with `permission denied` or `exec format error`**
  - `/tmp` is mounted with `noexec`. Use `make build` / `make test` — the
    Makefile sets `TMPDIR`, `GOCACHE`, `GOTMPDIR` to repo-local directories.
  - Never run bare `go test ./...` or `go build .`.
- **`ws` command not found**
  - Ensure `/usr/local/bin` is in `PATH`.
- **Formatting drift in PRs**
  - Run `make fmt` before commit.
- **Credential helper points to wrong binary after tests**
  - This should no longer happen — `TestMain` isolates `GIT_CONFIG_GLOBAL`.
  - If it does happen: `ws git-credential-helper setup` (from `/usr/local/bin/ws`).
  - Never run `./ws git-credential-helper setup` — it points the config at the
    repo-local dev binary instead of the system-installed one.

---

## 11) Golden rule

Before every PR/release:

```bash
make fmt && make test && make build
```

If this passes, you're in a very good place. ✅

---

## 12) Design invariants for CLI UX

These invariants exist because the codebase already violated them. They codify what went wrong and what every future change must satisfy. Run through this checklist before adding or modifying any command.

### 12.1) Completion fidelity: completions must mirror reality

The completion system (`cmd/complete.go`) is a **derived artifact** of the command implementations in `cmd/*.go`. It must never diverge.

**Rules:**

1. **`topLevelCommands` must equal the set of routed commands.** Every entry in `topLevelCommands` must have a matching `case` in `Execute()` (`cmd/root.go`). A command that tab-completes but returns "unknown command" is worse than no completion at all.

2. **`completers` subcommand lists must match actual subcommand switches.** When a command's `switch sub` block adds, removes, or renames a subcommand, the corresponding `completers` entry must update in the same commit. Stale names (e.g. `"show"` when the real subcommand is `"rm"`, `"delete"` when it's `"rm"`, `"sync"` when it's `"push"`) are silent failures — the user types the suggested name, nothing happens, and they blame the tool.

3. **`commandFlags()` must list every flag a subcommand actually registers.** Cross-reference against the `flag.NewFlagSet` + `fs.StringVar`/`fs.BoolVar` calls in the command handler. Ghost flags (suggesting `--all` when the command has `--rebase`) actively mislead. Missing flags (omitting `--path`, `--dirty`, etc.) silently degrade the experience.

4. **Commands with dynamic positional arguments must have a `resolve` function.** If a subcommand accepts a repo name, a scratch ID, a dotfile name, a capture location, or any value that can be enumerated at tab-time, the `completers` entry needs a `resolve` function that loads the relevant data from `completionCtx`. A completer with `subcommands` but no `resolve` is only half-wired.

5. **`completionCtx` must carry data for every dynamic completion.** When a new `resolve` function needs workspace state (repo list, tag list, etc.), add the field to `completionCtx` and load it in `loadCompletionCtx`. Do not inline filesystem calls inside resolvers — the context loader is the single best-effort loading point.

**Enforcement:** There is no automated check yet. The developer adding or renaming a command is responsible. When in doubt, run `ws completions install && exec bash` and test every subcommand + flag with Tab.

### 12.2) Path display: one rule for all rendered paths

Every path shown to the user must be **actionable** — the user should be able to copy it and use it in a `cd` or `ls` command without mental translation.

**Reference implementation: `ws scratch ls`.** Scratch already does this correctly — it shows a bold short name on line 1 (the identifier the user thinks in), then the full absolute path in muted style on line 2 (the value the user copies for `cd`). This two-line pattern is the model for all commands that list items the user may want to navigate to.

```
proxy-debug.2026-04          age=2d  size=1.2M  items=7  [k8s]
  ~/Scratch/proxy-debug.2026-04
```

**Rules:**

1. **Short name first, full path second.** The primary line shows the compact identifier (relative path for workspace items, tilde-shortened path for external items) plus metadata. The secondary line (indented, muted) shows the tilde-shortened absolute path — copy-pasteable for `cd`. This is the scratch pattern and applies to all list/scan output where users need to navigate to the item.

2. **External paths use tilde-shortened absolute form as their short name.** Never display a raw absolute path like `/home/user/.password-store` when `~/.password-store` is equivalent. For external items, the short name and the full path are the same value, so the secondary line can be omitted.

3. **Never mix conventions in the same output.** If a command lists 6 repos and 5 are relative while 1 is absolute, the output is broken. A single `DisplayPath(workspacePath, rawPath)` helper in `internal/style/` must handle the normalization so call sites never make ad-hoc decisions.

4. **Storage vs display are separate concerns.** Internal data structures and JSON output may store paths however is convenient (relative, absolute, whatever). The normalization happens at render time, not at storage time. This keeps the internal APIs simple and the display logic centralized.

**Applied to `ws repo scan`** — the target output:

```
⎇  Data/bruno  main DIRTY
   ~/Workspace/Data/bruno
⎇  Experiments/ws-tool  master DIRTY
   ~/Workspace/Experiments/ws-tool
⎇  ~/.password-store  master CLEAN ↑19 ↓0
```

Workspace repos get the two-line treatment (relative name + absolute path). External repos like the pass store show tilde-shortened path as their name — no secondary line needed since the short name is already cd-ready.

### 12.3) Positional targeting: fleet commands should support single-item operation

Fleet commands (`ws repo sync`, `ws repo fetch`, `ws repo pull`) operate on all discovered repos by default. This is correct for the common case. But the user frequently wants to act on a single repo — especially after `ws repo scan` shows one repo that needs attention.

**Rules:**

1. **Fleet commands should accept an optional positional repo path** as a filter. `ws repo sync Data/bruno` should sync only that repo. The positional arg is the same relative path shown by `ws repo scan` / `ws repo ls`.

2. **Positional targeting and `--path` filtering are complementary, not redundant.** `--path` is a prefix filter (all repos under a subpath). A positional arg is an exact match (one specific repo). Both can coexist.

3. **Tab completion for positional repo args** must offer discovered repo paths. This is what makes the feature ergonomic — the user types `ws repo sync D<TAB>` and gets `Data/bruno`.

4. **This pattern may extend to other fleet-style commands** in the future (e.g. `ws dotfile fix <name>`). The same principle applies: if a command operates on a list and the user commonly wants to target one item, accept a positional filter.

### 12.4) Global flag registration: every subcommand must parse global flags

Global flags (`--json`, `--quiet`, `--verbose`, `--no-color`, `--dry-run`) are pre-parsed from the raw args in `parseGlobalFlags`. But subcommands that create their own `flag.FlagSet` must also call `registerGlobalFlags(fs, &globals)` — otherwise a global flag placed after the subcommand position (e.g. `ws cron ls --json`) is treated as an unknown flag and the command errors out.

**Rule:** Every `flag.NewFlagSet` in a command handler must be followed by `registerGlobalFlags(fs, &globals)`. Subcommands that skip `FlagSet` creation entirely (parsing args manually) must not reject recognized global flags.

### 12.5) Dead code in the completion table is a bug, not tech debt

A stale entry in `commandFlags()` or `completers` is not harmless dead code — it is a user-facing lie. The shell will suggest a flag or subcommand that does not exist, or fail to suggest one that does. Treat stale completion entries with the same severity as a broken command handler.
