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

Project target: **Go 1.26+** (required by the Charm TUI libraries).

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

### Default: Docker-isolated test run

```bash
make test
```

Tests run inside a Docker container with the source tree bind-mounted read-only.
This provides full isolation from the host machine — live crontabs, credential
helpers, XDG config, and password stores are completely invisible to the tests.

On first run, `make test` builds the test image (`ws-tool-test:local`) from
`Dockerfile.test`. The image is cached; subsequent runs skip the build step
unless `Dockerfile.test` changes.

**Prerequisites:** Docker must be installed and running.

### Host machine (LOCAL=1)

For CI pipelines, or when Docker is not available, or for quick targeted runs:

```bash
make test LOCAL=1
```

This is also what CI should set — CI runners are already isolated environments,
so Docker-in-Docker is unnecessary overhead.

### Targeted / verbose runs (host)

```bash
TMPDIR=$PWD/tmp GOCACHE=$PWD/.gocache GOTMPDIR=$PWD/.gotmp go test -v -run TestFoo ./cmd/...
```

### Coverage run (host)

```bash
TMPDIR=$PWD/tmp GOCACHE=$PWD/.gocache GOTMPDIR=$PWD/.gotmp go test -cover ./...
```

> **Why the env vars?** This machine mounts `/tmp` with `noexec`. Go needs to
> execute temp artifacts during compilation and testing. The Makefile sets
> `TMPDIR`, `GOCACHE`, and `GOTMPDIR` to repo-local directories automatically.
> When running `go test` directly (for extra flags), you **must** set these
> yourself. The directories (`tmp/`, `.gocache/`, `.gotmp/`) are gitignored.
> Inside Docker these dirs are writable bind-mounts, so the Go cache is warm
> across container runs.

### Docker image management

```bash
make test-image        # build (or rebuild) the test image explicitly
make test-image-clean  # remove the image and stamp, forcing a full rebuild
```

### Test isolation guarantees

There are **two independent layers** of isolation. Both are always active.

**Layer 1 — Docker (process-level):** host crontab, credential helpers, XDG
config, password store, and git config are unreachable from inside the
container. Applies to the entire test binary.

**Layer 2 — `testSetXDG` (test-level):** redirects `XDG_CONFIG_HOME`,
`PASSWORD_STORE_DIR`, and `WS_CRONTAB_FILE` to per-test temp dirs, and
`TestMain` isolates `GIT_CONFIG_GLOBAL` and the editor `code` binary.

| Scope | Mechanism | Protects |
|---|---|---|
| XDG config | `XDG_CONFIG_HOME` → temp dir (per test) | Real `~/.config` |
| Git global config | `GIT_CONFIG_GLOBAL` → temp file (TestMain) | Real `~/.gitconfig` (credential helper) |
| System crontab | `WS_CRONTAB_FILE` → temp file (per test) | Live cron jobs on the host |
| Password store | `PASSWORD_STORE_DIR` → temp dir (per test) | Real `~/.password-store` |
| Editor binary | Fake `code` shim in `PATH` (TestMain) | VS Code not invoked during tests |

> **Convention:** Any test that invokes `ws init`, `ws cron`, `ws secret`, or
> any other RW command **must** call `testSetXDG(t)` at the top. Docker gives
> process isolation; `testSetXDG` gives test-level isolation. Both are needed.

This means:
- **No test can clobber your `ws git-credential-helper setup`.** Git global
  config writes go to an isolated temp file, not your real `~/.gitconfig`.
- **No test touches your live crontab.** `WS_CRONTAB_FILE` redirects all
  crontab reads/writes to an isolated temp file.
- **No test needs network access or real credentials.**
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
  - Install Go 1.26+ via official tarball.
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

### 12.2) Path display and input: absolute out, cwd-relative in

The authoritative rule is **Path Rules** in `spec.md`. This section is the contributor checklist.

**Output — every path `ws` prints is a literal, cleaned, absolute path.** That covers stdout text, `--json` payloads, plan/dry-run lines, action IDs, prompts, and stderr errors. There are no workspace-relative paths, no `~`-shortening, and no basename standing in for a path.

```
⎇  /home/user/Workspace/Data/bruno  main DIRTY
⎇  /home/user/Workspace/Experiments/ws-tool  master DIRTY
⎇  /home/user/.password-store  master CLEAN ↑19 ↓0
```

**Rules:**

1. **One helper.** Render every path through `style.AbsPath(workspace, p)`. It joins relative paths onto the workspace and runs `filepath.Clean`. It does not resolve symlinks and does not tilde-shorten. Call sites never format paths ad hoc.
2. **One line per item.** Absolute paths are already `cd`-ready, so the old "short name + muted full path" two-line pattern is gone. Identifiers such as scratch names, log tags, capture locations and cron job names may still appear. When the command acts on the file behind that name, print the file's absolute path too.
3. **JSON is output too.** Path fields in `--json` data are absolute (envelope `schema: 2`). Internal structs may keep workspace-relative paths where rule matching needs them (ignore/secret violations, repo discovery). Convert at render/marshal time, never by changing what the matcher sees.
4. **Not paths:** `.megaignore` rule patterns, config keys, git refs/URLs, and pass entry names (`git/<host>`). Print these verbatim.
5. **Tree views** (`ws ignore tree`) print one absolute header. The rows beneath it are tree nodes, not paths.

**Input — every path argument resolves the same way:** absolute as is, `~` expanded, anything else relative to the **current working directory**. Then clean it and compare absolute to absolute.

1. **No workspace-relative fallback for CLI input.** If a command today "tries cwd, then tries the workspace", delete the second branch.
2. **Prefix filters match whole components.** `--path /ws/Da` must not match `/ws/Data`.
3. **Round-trip test.** Any path copied from `ws` output must be accepted by any `ws` command, from any directory. Add a test when you touch a command that both prints and accepts paths.
4. **Config values are different.** Relative paths in `config.json` resolve against `<workspace>`. When writing CLI input into config, resolve it with the input rule first. Then store it workspace-relative if it is inside the workspace, absolute otherwise.

### 12.3) Positional targeting: fleet commands should support single-item operation

Fleet commands (`ws repo sync`, `ws repo fetch`, `ws repo pull`) operate on all discovered repos by default. This is correct for the common case. But the user frequently wants to act on a single repo — especially after `ws repo scan` shows one repo that needs attention.

**Rules:**

1. **Fleet commands should accept an optional positional repo path** as a filter. It is resolved like any path argument (absolute, `~`, or relative to the cwd) and matched against discovered repos as an absolute path. Pasting the path from `ws repo scan` output always works. `ws repo sync Data/bruno` works only when run from the workspace root.

2. **Positional targeting and `--path` filtering are complementary, not redundant.** `--path` is a prefix filter (all repos under a directory, whole-component match). A positional arg is an exact match (one specific repo). Both can coexist.

3. **Tab completion for positional repo args** must offer the absolute paths of discovered repos, including `ws/dotfiles` and the pass store. This is what makes the feature ergonomic: the user types `ws repo sync /home/user/Workspace/D<TAB>` and gets `/home/user/Workspace/Data/bruno`.

4. **This pattern may extend to other fleet-style commands** in the future (e.g. `ws dotfile fix <path>`). The same principle applies: if a command operates on a list and the user commonly wants to target one item, accept a positional filter.

### 12.4) Global flag registration: every subcommand must parse global flags

Global flags (`--json`, `--quiet`, `--verbose`, `--no-color`, `--dry-run`) are pre-parsed from the raw args in `parseGlobalFlags`. But subcommands that create their own `flag.FlagSet` must also call `registerGlobalFlags(fs, &globals)` — otherwise a global flag placed after the subcommand position (e.g. `ws cron ls --json`) is treated as an unknown flag and the command errors out.

**Rule:** Every `flag.NewFlagSet` in a command handler must be followed by `registerGlobalFlags(fs, &globals)`. Subcommands that skip `FlagSet` creation entirely (parsing args manually) must not reject recognized global flags.

### 12.5) Dead code in the completion table is a bug, not tech debt

A stale entry in `commandFlags()` or `completers` is not harmless dead code — it is a user-facing lie. The shell will suggest a flag or subcommand that does not exist, or fail to suggest one that does. Treat stale completion entries with the same severity as a broken command handler.

---

## 13) TUI (`ws tui`)

The TUI is the long-term front end. It and the CLI call the same `internal/`
packages. Logic both need lives in `internal/`, not `cmd/`, so they cannot
drift — e.g. `workspace.ResolvePaths`, `repo.SpecialRepos`/`DescribeSync`,
`dotfile.AutoSync`, `secret.ResolveGitToken` and the `secret fix` mutations,
`cron.AddActions`/`RemoveActions`, `trash.Record*Provisions`, and the Action
Plan types in `internal/plan`.

Built on Charm v2: `charm.land/bubbletea/v2`, `charm.land/bubbles/v2`,
`charm.land/lipgloss/v2`.

### Layout

| Package | Role |
| --- | --- |
| `internal/tui/app` | Root model. Owns screens, routes messages, handles global keys, lays out header / body / notice / footer, shows the setup screen when the workspace is not initialized. |
| `internal/tui/nav` | Contract between app and screens: `Screen` enum, `Component` interface, `ResizeMsg`, `UpMsg`, `GotoMsg`, `RefreshMsg`, `ReloadEnvMsg`, `NoticeMsg`. |
| `internal/tui/keys` | **Every key binding** (`keys.go`). App handles `Global`; each screen and dialog handles its own map. |
| `internal/tui/screens/<name>` | One package per screen: dashboard, repos, dotfiles, scratch, logs, capture, ignore, secrets, system, setup. Unexported state; outputs are exported `LoadedMsg` types. |
| `internal/tui/confirm` | The Action Plan checklist: review (toggle) → running (one `tea.Cmd` per action) → done. Emits `DoneMsg{Result}`. |
| `internal/tui/prompt`, `choose` | Text input with ghost-panel suggestions (fixed list or async `Completer`); single-choice list. |
| `internal/tui/modal` | Holds a screen's one open dialog, tagged with a typed step; `Match` routes the dialog's result back. |
| `internal/tui/handover` | Suspends the TUI to give a program the terminal (`Run`), launches GUI programs (`Launch`), runs CLI wizards (`WS` + `Pause`/`Paged`), opens editors (`Editor`). |
| `internal/tui/listview`, `layout`, `theme`, `format`, `complete` | Load-state-aware table; column sizing from width; lipgloss styles (16-color, matches `internal/style`); cell formatting; path completion. |
| `internal/tui/env` | Workspace context resolved at startup (paths, config, dirs). Reloaded via `nav.ReloadEnvMsg` after init/reset. |
| `internal/tui/tuitest` | Snapshot (golden file) and fits-in-terminal assertions. |

(`internal/tui/ghostinput.go` etc. are raw-ANSI prompt widgets used by the CLI.)

### Rules

**Architecture.** Model holds state; `Update` changes it; `tea.Cmd`s do async
work; `View` renders state and has no side effects. Never block `Update`;
never do IO in `View`. Loaders and plan actions run inside commands —
including validation that needs the filesystem (do it in the action and let
the checklist show the failure).

**Message routing.** Key presses go to the global map first, then to the
active screen only. While a screen has a dialog open (`CapturesKeys`), every
key except ctrl+c goes to it, so typing `q` or `2` into a prompt works. All
other messages are broadcast to every screen; that is how the dashboard
summarizes the others (it observes their `LoadedMsg`).

**Write actions.** Every mutation goes through a `plan.Plan` shown in the
`confirm` checklist — one action per independently meaningful mutation
(§9). Pre-check what the CLI would do by default (`WithChecked`); start
destructive plans (reset) unchecked. After `DoneMsg`, notify with
`confirm.Summary` and reload.

**Dialogs.** A screen keeps one `modal.Model`. Open a dialog with a typed
`step` constant; in `Update`, first `if st, ok := m.modal.Match(msg)` to
handle results, then forward to the modal while `Open()`. Results carry the
dialog's ID, so stale results are ignored.

**Handover.** Interactive programs (`ws log start`'s recorded shell,
terminal editors) and the CLI's multi-step wizards run via `handover`, with a
typed tag so the owning screen refreshes on `handover.DoneMsg`. Handed to the
CLI today: `init`, `restore`, `dotfile git setup`, `dotfile migrate`,
`secret setup`, `git-credential-helper setup|status|disconnect`,
`ignore tree|ls|edit`, `config view`, `log start`.

**Navigation.** Screens are `nav.Screen` constants, never strings. `esc`
always moves one level up: a dialog closes first; a screen with nested levels
(repos list → detail) handles esc itself; at its top level it returns
`nav.Up` and the app goes to the dashboard. At the dashboard, esc does
nothing (`q` / `ctrl+c` quit).

**Layout.** No absolute coordinates. The app sends each screen a
`nav.ResizeMsg` with the body's size on every resize; screens and dialogs
derive everything from it. The frame is clipped to the terminal; below 20×6 a
"terminal too small" notice is shown.

**Stale results.** A screen that can reload while a load is in flight tags
loads with a generation counter and drops stale results (see `repos`).

### Changing a component

Before: identify its state (the `Model` struct), inputs (messages it handles in
`Update`), outputs (exported msgs / `nav.*` commands it returns), key bindings
(its map in `keys.go`), and rendering region (the `ResizeMsg` size).

After:

```bash
make test                                         # all tests, incl. TUI snapshots
UPDATE_SNAPSHOTS=1 TMPDIR=$PWD/tmp GOCACHE=$PWD/.gocache GOTMPDIR=$PWD/.gotmp \
  go test ./internal/tui/...                      # accept intended snapshot changes
git diff internal/tui/**/testdata                 # review them like code
```

Snapshots live in `internal/tui/app/testdata/*.golden` (ANSI stripped,
100×30 unless noted) and cover every screen plus open dialogs. `TestResize`
renders every screen and several dialogs from 120×40 down to 1×1 and fails if
anything overflows; `TestResizeRestoresLayout` checks shrinking and restoring
is lossless. `TestScratchNewEndToEnd` drives prompt → checklist → apply
against a real temp workspace with every integration (HOME, git config, pass,
crontab) redirected — copy its `hermetic` helper for new end-to-end tests.
