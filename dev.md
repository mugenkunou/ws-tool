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
TMPDIR=$HOME/tmp GOCACHE=$HOME/.gocache GOTMPDIR=$HOME/gotmp go test -v -run TestFoo ./cmd/...
```

### Coverage run (host)

```bash
TMPDIR=$HOME/tmp GOCACHE=$HOME/.gocache GOTMPDIR=$HOME/gotmp go test -cover ./...
```

> **Why the env vars?** This machine mounts `/tmp` with `noexec`. Go needs to
> execute temp artifacts during compilation and testing. The Makefile sets
> `TMPDIR`, `GOCACHE`, and `GOTMPDIR` to home-directory directories
> (`~/tmp`, `~/.gocache`, `~/gotmp`, created if missing) automatically.
> When running `go test` directly (for extra flags), you **must** set these
> yourself. Nothing is written inside the repo.

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

### Naming: the command grammar

Every command is `ws <noun> <verb> [targets…]`. spec.md Part 3 has the rules, the nouns,
the closed verb glossary (§3.3), and every command (§3.4). Before you add or rename one:

1. **Use a glossary verb with exactly its meaning**, or a domain word its users already
   know (git's `fetch`, the trash's `restore`). If neither fits, extend the glossary in
   spec.md in the same change, and say why in the commit.
2. **No umbrella verbs:** never `scan`, `fix`, `reset`, `setup`, `manage`, `do`. Name what
   changes.
3. **`ls` lists the noun's rows** (its TUI screen, its `fields.go`). Listing is not a
   verdict: it exits 0.
4. **Targets are arguments.** No targets means every row, unless the command needs one.
5. **Renames keep the old name as a hidden alias**, kept in the alias table in `cmd/`. A
   hidden alias is absent from help and completion. It prints the rename hint on stderr
   when a person types it, and stays silent when called as plumbing.
6. **Give the command a TUI home in the same change:** a screen key named after the verb,
   or a `:` command (spec.md §6.5), plus its entry in `internal/tui/app/coverage.go`.

### RO vs RW classification

Every command is either **read-only (RO)** or **read-write (RW)**. If it writes to disk, config, manifest, or system state → RW.

- **RO commands** receive `(args, globals, stdout, stderr)` — no stdin, no prompts.
- **RW commands** receive `(args, globals, stdin, stdout, stderr)` — stdin is threaded for interactive prompts.

### RW exemptions

Some RW commands are exempt from the Plan → Confirm → Execute pattern because their writes are non-destructive and time-sensitive. These commands append-only and never modify existing data:

- `ws session record`: hands the terminal to a recorded shell, and leaving the shell ends it.
- `ws session stop`: stops a recording. There is nothing meaningful to confirm.
- `ws repo fetch` and `ws repo ls --fetch`: they change only remote-tracking refs, never a
  branch or the working tree.
- `ws cron run`: crontab lines call it, so the command is the confirmation.
- The `edit` commands (`dotfile edit`, `ignore edit`, `config edit`): the editor is the
  confirmation, and `ignore edit` and `config edit` validate before saving.

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
| Per-file operations (init, dotfile add, dotfile unlink) | One action per file |
| Fleet operations (repo fetch/pull/push/save/run) | One action per repo |
| Cleanup operations (session prune, trash prune) | One action per item removed |
| Single-mutation commands (repo root add) | One action total |

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
TMPDIR=$HOME/tmp go test ./internal/cron/... ./cmd/... -run TestCron -v -count=1
```

### Smoke-test the CLI

Use a throwaway crontab: `export WS_CRONTAB_FILE=$HOME/tmp/crontab.test`. Never test
against your real crontab.

```bash
# 1. Jobs and presets, with schedule, installed, last run, exit code, next run
ws cron ls

# 2. Preview an install (writes nothing)
ws cron add mega-sync --dry-run

# 3. Install a job (a `ws cron run <job>` line plus an @reboot line, in a ws block)
ws cron add mega-sync

# 4. Run it now, then read its output
ws cron run mega-sync
ws cron log mega-sync

# 5. Presets
ws cron add sync --dry-run   # mega-sync + dotfile-backup + pass-backup

# 6. Uninstall
ws cron rm mega-sync
```

### What to verify

| Check | Expected |
| --- | --- |
| `ws cron ls` | the built-in jobs (mega-sync, dotfile-backup, pass-backup, session-prune) and presets; `installed` is yes/no |
| `ws cron add <job> --dry-run` | the plan; the crontab is unchanged |
| `ws cron add <job>` | a `# >>> ws` block with the schedule line and the `@reboot` line, and `configs/crontab` updated to match |
| `ws cron run dotfile-sync` | still runs (old job name, from existing crontab lines), silently |
| `ws cron rm <job>` | the block is gone from the crontab and from `configs/crontab` |

---

## 11) Troubleshooting quick hits

- **`go: toolchain not available`**
  - Install Go 1.26+ via official tarball.
- **Build/tests fail with `permission denied` or `exec format error`**
  - `/tmp` is mounted with `noexec`. Use `make build` / `make test` — the
    Makefile sets `TMPDIR`, `GOCACHE`, `GOTMPDIR` to home-directory dirs
    (`~/tmp`, `~/.gocache`, `~/gotmp`).
  - Never run bare `go test ./...` or `go build .`.
- **`ws` command not found**
  - Ensure `/usr/local/bin` is in `PATH`.
- **Formatting drift in PRs**
  - Run `make fmt` before commit.
- **Credential helper points to wrong binary after tests**
  - This should no longer happen — `TestMain` isolates `GIT_CONFIG_GLOBAL`.
  - If it does happen: `ws credential enable` (from the installed `ws`).
  - Never run `./ws credential enable`: it points git at the repo-local dev binary
    instead of the installed one.

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

4. **Commands with dynamic positional arguments must have a `resolve` function.** If a subcommand accepts a repo name, a scratch ID, a dotfile name, or any value that can be enumerated at tab-time, the `completers` entry needs a `resolve` function that loads the relevant data from `completionCtx`. A completer with `subcommands` but no `resolve` is only half-wired.

5. **`completionCtx` must carry data for every dynamic completion.** When a new `resolve` function needs workspace state (repo list, tag list, etc.), add the field to `completionCtx` and load it in `loadCompletionCtx`. Do not inline filesystem calls inside resolvers — the context loader is the single best-effort loading point.

**Enforcement:** `TestCompletersOfferEverySubcommand` checks that completion offers every subcommand the help lists, and that it never offers a hidden alias. Flags are still checked by hand: run `ws completion enable && exec bash` and tab through every subcommand and flag you touched.

### 12.2) Path display and input: absolute out, cwd-relative in

The authoritative rule is **Path Rules** in `spec.md`. This section is the contributor checklist.

**Output — every path `ws` prints is a literal, cleaned, absolute path.** That covers stdout text, `--json` payloads, plan/dry-run lines, action IDs, prompts, and stderr errors. There are no workspace-relative paths, no `~`-shortening, and no basename standing in for a path.

```
⎇  /home/user/Workspace/Data/bruno  main DIRTY
⎇  /home/user/Workspace/Experiments/ws-tool  master DIRTY
```

**Rules:**

1. **One helper.** Render every path through `style.AbsPath(workspace, p)`. It joins relative paths onto the workspace and runs `filepath.Clean`. It does not resolve symlinks and does not tilde-shorten. Call sites never format paths ad hoc.
2. **One line per item.** Absolute paths are already `cd`-ready, so the old "short name + muted full path" two-line pattern is gone. Identifiers such as scratch names, log tags and cron job names may still appear. When the command acts on the file behind that name, print the file's absolute path too.
3. **JSON is output too.** Path fields in `--json` data are absolute (envelope `schema: 3`). Internal structs may keep workspace-relative paths where rule matching needs them (ignore/secret violations, repo discovery). Convert at render/marshal time, never by changing what the matcher sees.
4. **Not paths:** `.megaignore` rule patterns, config keys, git refs/URLs, and pass entry names (`git/<host>`). Print these verbatim.
5. **Tree views** (`ws ignore tree`) print one absolute header. The rows beneath it are tree nodes, not paths.

**Input — every path argument resolves the same way:** absolute as is, `~` expanded, anything else relative to the **current working directory**. Then clean it and compare absolute to absolute.

1. **No workspace-relative fallback for CLI input.** If a command today "tries cwd, then tries the workspace", delete the second branch.
2. **Prefix filters match whole components.** `--path /ws/Da` must not match `/ws/Data`.
3. **Round-trip test.** Any path copied from `ws` output must be accepted by any `ws` command, from any directory. Add a test when you touch a command that both prints and accepts paths.
4. **Config values are different.** Relative paths in `ws.json` resolve against `<workspace>`. When writing CLI input into config, resolve it with the input rule first. Then store it workspace-relative if it is inside the workspace, absolute otherwise.

### 12.3) Positional targeting: fleet commands should support single-item operation

Fleet commands (`ws repo fetch|pull|push|save|run`, `ws dotfile link`) operate on every row by default. But people often want to act on one, especially after `ws repo ls` shows the one repo that needs attention.

**Rules:**

1. **Every command that acts on rows accepts positional targets** (spec.md §3.1 rule 5). A path target is resolved like any path argument (absolute, `~`, or relative to the cwd), and matched as an absolute path. Pasting a path from `ws repo ls` output always works. `ws repo save data/bruno` works only when run from the workspace root.

2. **Positional targets and `--path` are complementary.** `--path` is a prefix filter (every repo under a directory, matching whole components). A positional target is an exact match. Both can be used together.

3. **Tab completion for positional targets** offers the absolute paths of the rows (discovered repos, dotfile system paths), or their names (scratch, sessions, cron jobs, trash items). Typing `ws repo save /home/user/Workspace/d<TAB>` gives `/home/user/Workspace/data/bruno`.

4. **The TUI passes its rows the same way.** A key acts on the marked rows, else on the cursor row or the filtered rows (by its scope, §13). The CLI command with those rows as targets does the same thing.

### 12.4) Global flag registration: every subcommand must parse global flags

Global flags (`--json`, `--quiet`, `--verbose`, `--no-color`, `--dry-run`) are pre-parsed from the raw args in `parseGlobalFlags`. But subcommands that create their own `flag.FlagSet` must also call `registerGlobalFlags(fs, &globals)` — otherwise a global flag placed after the subcommand position (e.g. `ws cron ls --json`) is treated as an unknown flag and the command errors out.

**Rule:** Every `flag.NewFlagSet` in a command handler must be followed by `registerGlobalFlags(fs, &globals)`. Subcommands that skip `FlagSet` creation entirely (parsing args manually) must not reject recognized global flags.

### 12.5) Dead code in the completion table is a bug, not tech debt

A stale entry in `commandFlags()` or `completers` is not harmless dead code — it is a user-facing lie. The shell will suggest a flag or subcommand that does not exist, or fail to suggest one that does. Treat stale completion entries with the same severity as a broken command handler.

### 12.6) One grammar: help, completion, aliases

The grammar (spec.md Part 3, and §9 above) is enforced by tests, not review:

1. **Every subcommand has its own help.** `ws <noun> <verb> --help` prints that command's
   usage, arguments, own flags, and (for `ls`) its field names. `ws <noun> --help` lists the
   verbs with one line each. A group help that only prints `<a|b|c>` is a bug.
2. **Help, completion, the alias table and the TUI coverage table agree.** Tests read the
   help's verbs and check: each verb is in the glossary; completion offers exactly those
   verbs; no hidden alias appears in help or completion; every verb has a coverage entry.
3. **Hidden aliases are data, in one table.** Each entry maps an old command line prefix to
   the new one, and is marked as either *typed* (it prints the rename hint) or *plumbing*
   (silent: crontab lines, git, shell hooks). A test runs every alias and checks it reaches
   the same handler as its target.
4. **JSON command names are `<noun>.<verb>`** (`dotfile.backup.push`), and they match the
   command the user typed after alias resolution.

---

## 13) TUI (`ws tui`)

The TUI is the long-term front end. It and the CLI call the same `internal/`
packages. Logic both need lives in `internal/`, not `cmd/`, so they cannot
drift — e.g. `workspace.ResolvePaths`, `repo.DescribeSync`,
`dotfile.AutoSync`, `secret.ResolveGitToken` and the `secret allow|store` mutations,
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
| `internal/tui/screens/<name>` | One package per screen: dashboard, repos, dotfiles, scratch, sessions, ignore, secrets, cron, trash, setup. Unexported state; outputs are exported `LoadedMsg` types. |
| `internal/tui/cmdbar` | The `:` command bar: a one-line input drawn in place of the footer, with inline completion from the active screen's commands, the screen names, and the global commands. It emits `nav.CommandMsg`. |
| `internal/tui/confirm` | The Action Plan checklist: review (toggle) → running (one `tea.Cmd` per action) → done. Emits `DoneMsg{Result}`. |
| `internal/tui/prompt`, `choose` | Text input with ghost-panel suggestions (fixed list or async `Completer`); single-choice list. |
| `internal/tui/modal` | Holds a screen's one open dialog, tagged with a typed step; `Match` routes the dialog's result back. |
| `internal/tui/handover` | Suspends the TUI to give a program the terminal (`Run`), launches GUI programs (`Launch`), runs CLI wizards (`WS` + `Pause`/`Paged`), opens editors (`Editor`). |
| `internal/tui/selection`, `clip` | Mouse drag selection over the rendered frame; clipboard copy (OSC 52 + native tool). |
| `internal/tui/reslist` | The list every list screen is: a `listview` over typed items described by a `resource.Schema`, with filter (`/`), sort (`>` `<`), k9s-style marks (space, `ctrl+space` range, `ctrl+\` clear, `ctrl+a` all/none), describe (`d`), the row as JSON (`y`), and a status line reserved at the bottom built in. Screens declare their actions with a `Scope`, and their `:` commands. `reslist.Run` hands each one its rows. |
| `internal/tui/listview`, `layout`, `theme`, `format`, `complete` | Load-state-aware table; column sizing from width; lipgloss styles (16-color, matches `internal/style`); cell formatting; path completion. |
| `internal/tui/env` | Workspace context resolved at startup (paths, config, dirs). Reloaded via `nav.ReloadEnvMsg` after init/reset. |
| `internal/tui/tuitest` | Snapshot (golden file) and fits-in-terminal assertions. |

(`internal/tui/ghostinput.go` etc. are raw-ANSI prompt widgets used by the CLI.)

### One description per list

Each listed domain declares its records once, in its domain package
(`fields.go`; `trash/items.go` keeps the loader beside it):
a `resource.Schema` (`internal/resource`) of fields — key (the JSON key and
the `--sort` / `--filter` name), title, raw value, display text — plus the
default columns. The CLI list command (`renderList` in `cmd/list.go`) and the
TUI screen (`reslist`) both render that schema, so a field added there shows
up in `--json`, `--wide`, the TUI detail pane, and both filters at once.
Presentation stays with the caller: `reslist.Col` adds widths and colour.
See docs/adr/0001-tui-list-screens-from-shared-resource-descriptions.md.

**Actions take rows; they never pick them.** Each list screen declares an
`actions` table of `reslist.Action{Key, Scope, Do}` and dispatches keys with
`reslist.Run`, which calls `Do` with the rows the scope covers:

| Scope | Rows | Use for |
| --- | --- | --- |
| `Global` | none | `a` add/new, wizards, prompts not about a row, prune (all rows, like the CLI) |
| `Cursor` | the cursor row | `enter`, `e`, a prompt about one row (`m` on Secrets, `s` store) |
| `Selected` | marked rows, else the cursor row | `ctrl+d`, `t` tag, `i` ignore, `m` mute, `a` allow, `r` restore |
| `All` | marked rows, else every row shown (filtered) | fetch, pull, save, run, link |

An `All` action that receives every row may use the whole-subsystem
operation (`dotfile.Reset` for `unlink --all`, `trash.Empty`); otherwise it
acts on just the rows. The status line names the keys that will act on the
marks or the filter, with their labels.

**`:` commands are actions too.** Each screen declares its `commands` (the CLI
words after the noun, such as `backup push` or `root rm`), each with a scope and a
`Do` that also receives the typed arguments. The command bar completes from
them, and `?` lists them. A key and a command for the same verb share one `Do`.

Parity is tested, not reviewed:

- `cmd` `TestCLICoverage`: every CLI subcommand (read from its help) has an
  entry in `internal/tui/app/coverage.go`: a screen key, a `:` command, the
  screen's list, or a stated reason it is CLI-only. A new command fails until
  that is decided.
- `app` `TestCoverageKeysExist`: each entry's key or `:` command is one its
  screen offers.
- `app` `TestListScreensShareListKeys`: every list screen is a `reslist`, and
  none binds the global keys (`enter esc : / ? q ctrl+r d y e a ctrl+d space
  ctrl+space ctrl+\ ctrl+a > <` and movement) to another verb. Secrets' `a`
  allow is the one listed exception.
- `app` `TestNoShiftLetterActions`: no binding on any screen or dialog uses an
  uppercase letter, except `G` for movement.
- `app` `TestNoKeyTwiceOnAScreen`: no key appears in two bindings of one
  screen, movement included. That catches a `g` push hiding `g` top.
- `app` `TestEveryListKeyDeclaresItsRows`: every key on a list screen
  (except movement, back, refresh) is declared with a scope.
- `app` `TestMarkingMovesNoRow`: marking, filtering and sorting never change
  the screen line a row is drawn on, because the status line is always
  reserved.
- `app` `TestFooterFits`: at 100×30, every screen's footer shows all of its
  own keys without an ellipsis, and the global part is never cut.
- `app` `TestNoKeyNamesInText`: no string literal outside `keys.go` names a
  key ("press G"). Messages take the key from the binding's help.
- `cmd` `TestCompletersOfferEverySubcommand`: shell completion offers every
  subcommand the help lists, and no hidden alias.

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
(§9). Pre-check what the CLI would do by default (`WithChecked`), and start
destructive plans (`dotfile unlink --all`, `trash empty`) unchecked. A choice
ws must not make for the user (rebase or merge for a diverged repo) is asked
in a `choose` dialog before the checklist, never guessed and never a second
key. After `DoneMsg`, notify with `confirm.Summary` and reload.

**Dialogs.** A screen keeps one `modal.Model`. Open a dialog with a typed
`step` constant; in `Update`, first `if st, ok := m.modal.Match(msg)` to
handle results, then forward to the modal while `Open()`. Results carry the
dialog's ID, so stale results are ignored.

**Handover.** Interactive programs (`ws session record`'s recorded shell,
terminal editors) and the CLI's multi-step wizards run via `handover`, with a
typed tag so the owning screen refreshes on `handover.DoneMsg`. Handed to the
CLI: `init` (setup screen), `repo configure`, `dotfile backup enable`, `pass init`,
`pass backup enable`, `credential enable|disable`. Paged: `ignore tree`,
`credential status`, `config show`. sudo dotfile actions run in the terminal. Editors: `ignore
edit`, `dotfile edit`, `config edit`. Everything else is native (spec.md §6.8).

**Key grammar** (spec.md §6.3 and §6.5; every binding is declared in `keys.go`):

- **Global keys** mean the same on every screen: `enter` open · `esc` up one level ·
  `:` command bar · `/` filter · `?` help · `q`/`ctrl+c` quit · `tab`/`shift+tab`, `]`/`[`,
  `1`–`9` screens · `ctrl+r` refresh · `d` describe · `y` JSON · `e` edit · `a` add/new ·
  `ctrl+d` delete/remove (always through the checklist) · `space`/`ctrl+space`/`ctrl+\`/`ctrl+a`
  marks · `>`/`<` sort · `↑↓ j k g G pgup pgdn ctrl+b ctrl+f` movement.
- **Screen keys** are lowercase and mnemonic: usually the first letter of the verb the
  footer shows (`f` fetch, `i` ignore, `b` back up). A letter keeps one meaning on a
  screen, but may mean different verbs on different screens (`r`: run, record, restore).
- **No shift+letter actions** (only `G`, for movement). **No second key for a variant**: ask
  in a chooser (rebase or merge) or use a `:` command.
- **Rare actions and wizards are `:` commands**, never letters.
- **Text never names a key.** Empty states, notices and help take the key from the
  binding (`k.Add.Help().Key`), so a remap can't leave a stale "press G" behind.
- Global bindings never take arrows, hjkl, enter, esc or space:
  `TestGlobalKeysLeavePageKeysAlone` enforces it. The tab bar shows each jump key as a
  faint superscript (`Repos²`).

**Layout never shifts.** A list's status line (filter input, filter summary, sort,
marks) is always reserved at the bottom of the list's area, so typing a filter or marking
a row moves no row. The command bar replaces the footer line while it is open, and
toasts draw over the footer's last line.

**Dashboard.** The workspace's home. A "Needs attention" list (errors, then
warnings, then info; ↑↓) above an overview grid with one card per area (all
four arrows; ↑ from the top row returns to the list). It derives everything
from other screens' `LoadedMsg`. It has no actions of its own: `enter` on a
problem sends the target screen a `Focus*Msg` carrying a filter expression
(Dotfiles: `status!=ok`) or a row to select, alongside `nav.Goto`, and the user
acts there with that screen's key. Add an attention item by extending the
area's derivation function in `screens/dashboard`, with the filter that shows
exactly its rows.

**Mouse.** The app captures the mouse (cell-motion mode) so a drag selects
text and release copies it: `selection` extracts the dragged cells from the
rendered frame (terminal-style, emoji-width aware) and draws the highlight;
`clip.Copy` emits OSC 52 and also pipes to `wl-copy`/`xclip`/`xsel` when
present. A toast ("📋 Copied …") replaces the footer's last line for 2.5 s,
so showing it never shifts the layout. Because capture stops terminals from
turning the wheel into arrow keys, the app maps wheel ↑/↓ to arrow presses.
Shift+drag still uses the terminal's own selection; `WS_NO_MOUSE=1` turns
capture off. Tests clear `DISPLAY`/`WAYLAND_DISPLAY` so they never touch the
real clipboard.

**Emoji and fun cues.** Decoration only, via `theme.Icon` (off with
`WS_NO_EMOJI=1` and on `TERM=linux`). Use only Emoji_Presentation characters
(always 2 cells, no U+FE0F variation selector: not ⚠️ ☀️ ⚙️) so widths stay
exact. Animations read `nav.FrameMsg`, which the app broadcasts every 150 ms;
never read the clock in `View` — use `env.Now()` in `Update`.

**Navigation.** Screens are `nav.Screen` constants, never strings. `esc`
always moves one level up: a dialog closes first, then an active filter is
cleared, then the marks. A screen with nested levels (repos list → detail)
handles esc itself. At its top level it returns `nav.Up`, and the app goes to
the dashboard. At the dashboard, esc does nothing (`q` / `ctrl+c` quit).

**Command bar.** `:` is a global key handled by the app. The bar resolves `:<screen>`
(a unique prefix of the noun or the title), `:<noun> <verb…>` (switch, then run), and
`:<verb…>` (the active screen's command). It sends the screen a `nav.CommandMsg{Words,
Args}`, and the screen runs the matching command through `reslist.Run`, so its rows
and scope rules are the same as for keys.

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
UPDATE_SNAPSHOTS=1 TMPDIR=$HOME/tmp GOCACHE=$HOME/.gocache GOTMPDIR=$HOME/gotmp \
  go test ./internal/tui/...                      # accept intended snapshot changes
git diff internal/tui/**/testdata                 # review them like code
```

Snapshots live in `internal/tui/app/testdata/*.golden` (ANSI stripped,
100×30 unless noted) and cover every screen plus open dialogs. `TestResize`
renders every screen and several dialogs from 120×40 down to 1×1 and fails if
anything overflows; `TestResizeRestoresLayout` checks shrinking and restoring
is lossless. Tests pin `env.Clock` and `theme.SetEmoji(true)`; tests call
`loadAll()` rather than `Init()` (which also starts the animation timer).
`TestScratchNewEndToEnd` drives prompt → checklist → apply
against a real temp workspace with every integration (HOME, git config, pass,
crontab) redirected — copy its `hermetic` helper for new end-to-end tests.
