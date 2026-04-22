# `ws` — Workspace Manager

`ws` is a workspace management tool for Linux. It ships today as a single static CLI binary, but the core architecture — data model, config resolution, violation detection, dependency delegation — is interface-agnostic. This document is the authoritative spec: it covers the core design, the CLI reference, and the companion services that extend `ws` beyond the terminal.

The spec is organized in five parts:

1. **Core Architecture** — Design constraints, data model, config resolution, violation taxonomy. Interface-agnostic.
2. **Technical Design Decisions** — Implementation choices, trade-offs, and rationale behind key subsystems. Interface-agnostic.
3. **Dependencies & Environment** — Runtime tools, build chain, ecosystem software.
4. **CLI Reference** — Flags, exit codes, command catalog, output examples.
5. **Beyond CLI** — Daemon, TUI, desktop notifications, and guided workflows that build on the CLI foundation.

---

## Design Constraints

- **Single binary.** No Python, no Node, no shell deps beyond a POSIX shell for `ws log`.
- **Statically compiled.** Copy the binary to any machine and it works.
- **Follow Linux standards** Can reuse existing linux commands like `ls` or `grep` if needed. Pipe compatiblity with other linux commands.
- **Zero attack surface:** Avoid the use of 3rd party libraries completely. Reduce the attack surface to bare minimum.
- **Workspace-source metadata.** `config.json` and `manifest.json` live inside `<workspace>/ws/` and are synced with the workspace.
- **Scratch by default.** The binary never writes runtime output to the workspace. Only structured metadata (indexes, command extracts), configs, and essentials state goes to sync.
- **Soft-delete first.** `ws` uses `rm` for delete paths, and provides machine setup so common delete flows behave as soft delete.
- **Read/write separation.** Read-only commands are non-interactive and pipe-safe. Write commands are always interactive and support `--dry-run`. There is no `--force` flag. Exemptions: `ws log start --quiet-start`, `ws log stop`.
- **Private remote only for dotfiles.** Dotfiles contain secrets by nature (SSH keys, kubeconfigs, proxy rules). When dotfile Git versioning is enabled, `ws` enforces that the remote repository is private. This is not configurable — there is no flag to bypass it.
- **JSON output available.** Every command supports `--json` for piping and scripting.

---

### Read/Write Separation (Detailed)

Every command in `ws` is classified as **read-only (RO)** or **read-write (RW)**. This classification drives interactivity, piping, and safety behavior.

#### RO Commands (non-interactive, pipe-safe)

RO commands never modify the workspace, config, manifest, or system state. They:
- Produce deterministic output to stdout.
- Never prompt for user input.
- Work correctly when piped (`ws repo ls | wc -l`, `ws ignore scan --json | jq`).
- Support `--json` for machine-readable output.

| Command | Notes |
|---|---|
| `ws version` | |
| `ws config view`, `ws config --defaults`, `ws config dump` | |
| `ws ignore scan`, `ws ignore check`, `ws ignore ls`, `ws ignore tree` | |
| `ws secret scan` | |
| `ws secret status` | |
| `ws secret git status`, `ws secret git log`, `ws secret git remote` | |
| `ws git-credential-helper get`, `ws git-credential-helper store`, `ws git-credential-helper erase` | Git plumbing — called by git, not for direct use (no workspace required) |
| `ws git-credential-helper status` | Check credential helper config and pass entry coverage |
| `ws dotfile ls`, `ws dotfile scan` | |
| `ws dotfile git status` | |
| `ws repo ls`, `ws repo scan`, `ws repo fetch` | |
| `ws log ls` | |
| `ws scratch ls` | |
| `ws scratch search` | Search scratch directories by tag/name/content |
| `ws trash status` | |
| `ws notify status`, `ws notify test` | |
| `ws tui` | Interactive TUI (reads input but doesn't modify data) |
| `ws completions` | |

#### RW Commands (interactive, confirm-before-write)

RW commands modify state. They:
- **Build a `Plan` of discrete `Action`s** before executing anything.
- **Prompt per-action** with `y/n/a/q` keys (like `git add -p`): `y` = yes (default/Enter), `n` = skip this action, `a` = accept all remaining, `q` = quit (skip all remaining).
- Support `--dry-run` to preview the full plan without writing.
- Auto-accept all actions when `--quiet` or `--json` is set (non-interactive modes).
- Treat EOF on stdin as "yes" (accept), so piped/test usage works seamlessly.
- There is **no `--force` flag** — the only way to bypass prompts is `--quiet`.
- Multi-action commands (e.g., `ws repo pull` across N repos) present **one prompt per action**, not a single gate.

| Command | Actions |
|---|---|
| `ws init` | Per-file: config.json, manifest.json, .megaignore, ws/ dir |
| `ws reset` | Per-subsystem: dotfiles, context, trash, ws/ dir removal |
| `ws restore` | Per-step: trash-enable, dotfile-fix, ignore-generate |
| `ws dotfile add` | Single action per file |
| `ws dotfile rm` | Single action per file |
| `ws dotfile reset` | Single action wrapping subsystem reset |
| `ws dotfile fix` | Per-violation actions |
| `ws dotfile git setup` | Guided walk-through: init → remote → auto-push |
| `ws dotfile git push` | Commit pending + push to remote |
| `ws dotfile git disconnect` | Single action |
| `ws secret git push` | Push pass store to remote |
| `ws ignore fix` | Per-violation actions |
| `ws ignore generate` | Per-step: generate/merge, optional scan |
| `ws ignore edit` | Opens editor (inherently interactive) |
| `ws secret fix` | Per-violation actions |
| `ws repo pull` | Per-repo actions |
| `ws repo sync` | Per-repo actions |
| `ws repo run` | Per-repo actions |
| `ws log start` | Single action |
| `ws log stop` | Single action |
| `ws log prune` | Per-session actions |
| `ws log rm` | Single action |
| `ws scratch new` | Single action |
| `ws scratch prune` | Per-directory actions |
| `ws scratch rm` | Single action |
| `ws scratch tag` | Per-tag actions (interactive or auto) |
| `ws trash enable` | 2 actions: setup, record-provisions |
| `ws trash disable` | Single action wrapping subsystem reset |
| `ws notify start` | Single action |
| `ws notify stop` | Single action |
| `ws notify daemon` | Long-running: inotify + periodic scan loop (used by systemd ExecStart) |
| `ws context create` | Single action |
| `ws context rm` | Single action (or batch with `--all`) |
| `ws git-credential-helper setup` | Per-action: set git config, create missing pass entries |
| `ws git-credential-helper disconnect` | Per-action: unset credential.helper from git config |
| `ws completions install` | Single action |
| `ws completions uninstall` | Single action |

#### Implementation Contract

When adding a new command to `ws`:

1. **Classify it as RO or RW.** If it writes to disk, config, manifest, or system state → RW.
2. **RO commands** receive `(args, globals, stdout, stderr)` — no stdin.
3. **RW commands** receive `(args, globals, stdin, stdout, stderr)` — stdin is threaded from the entry point.
4. **RW commands must use the Action Plan pattern (`cmd/plan.go`):**
   - Build a `Plan` containing one or more `Action`s (each with an ID, Description, and Execute closure).
   - Call `RunPlan(plan, stdin, stdout, globals)` to execute.
   - `RunPlan` handles dry-run display, per-action `y/n/a/q` prompts, quiet/json auto-accept, and error reporting.
   - Multi-mutation commands **must** create one `Action` per discrete mutation (e.g., one per repo, one per file, one per session) — never gate multiple mutations behind a single confirm.
5. **RW commands must support `--dry-run`** to preview the plan without writing. Set `globals.dryRun = true` and `RunPlan` handles the rest.
6. **After `RunPlan`**, use `planResult.WasExecuted(id)`, `planResult.ExitCode()`, `planResult.HasFailures()`, etc. to drive post-execution output and return codes.
7. **JSON output**: include `planResult.Actions` in the JSON envelope alongside command-specific data for programmatic consumers.
8. **Tests** pass `strings.NewReader("y\n")` as stdin to auto-accept the first action prompt. `promptChoice` returns the default key ("y") on EOF, so all actions are accepted in test contexts.

##### Action Plan Pattern — Quick Reference

```go
plan := Plan{Command: "mycommand"}
plan.Actions = append(plan.Actions, Action{
    ID:          "do-something",
    Description: "Create the thing",
    Execute: func() error {
        return doSomething()
    },
})
planResult := RunPlan(plan, stdin, stdout, globals)

if globals.json {
    return writeJSON(stdout, stderr, "mycommand", map[string]any{
        "result":  res,
        "actions": planResult.Actions,
    })
}
return planResult.ExitCode()
```

Prompt vocabulary (interactive mode):
- `y` (default, Enter) — execute this action
- `n` — skip this action, continue to next
- `a` — accept all remaining actions without prompting
- `q` — quit, skip all remaining actions

Exit codes from `planResult.ExitCode()`:
- `0` — all succeeded or all skipped by user choice
- `1` — all failed or infrastructure error
- `3` — partial success (some executed, some failed)

---

## Config Files: `config.json` and `manifest.json`

Located inside the workspace:

- `<workspace>/ws/config.json`
- `<workspace>/ws/manifest.json`

- `config.json` is user-managed, stable configuration.
- `manifest.json` is ws-managed durable metadata required for portable restore (dotfile registry, allowlists, and similar metadata).

**Path convention:** All relative paths in config values are resolved against `<workspace>`. Absolute paths and `~`-prefixed paths are expanded at runtime (`~` → `$HOME`). `<workspace>`, `scratch.root_dir`, `repo.roots`, and `trash.root_dir` are user-provided (via config/env/flag). Derived paths: logs use `<workspace>/ws/ws-log`.

**Soft-delete integration model:** `ws` delete actions execute via `rm`. Machine-level setup (`ws trash setup`) configures shell/IDE/file-explorer delete flows to soft-delete, usually to `trash.root_dir` (default `~/.Trash`). `ws` does not maintain its own workspace trash store.

`config.json`:

```json
{
  "config_schema": 1,
  "workspace": "~/Workspace",
  "ignore": {
    "warn_size_mb": 1,
    "crit_size_mb": 10,
    "max_depth": 6,
    "template": "builtin"
  },
  "secret": {
    "enabled": true,
    "pass_nudge": true,
    "skip_dirs": []
  },
  "scratch": {
    "root_dir": "~/Scratch",
    "editor_cmd": "code",
    "name_suffix": "auto",
    "prune_after_days": 90
  },
  "trash": {
    "root_dir": "~/.Trash",
    "warn_size_mb": 1024,
    "setup": {
      "prompt_on_init": true,
      "shell_rm": true,
      "vscode_delete": true,
      "file_explorer_delete": true,
      "warn_if_unconfigured": true
    }
  },
  "log": {
    "cap_mb": 500
  },
  "search": {
    "default_context": 2,
    "max_results": 0
  },
  "dotfile": {
    "git": {
      "enabled": false,
      "remote_url": "",
      "auth_username": "",
      "pass_entry": "",
      "branch": "main",
      "auto_commit": true,
      "auto_push": true
    }
  },
  "repo": {
    "roots": ["."],
    "exclude_dirs": ["ws", "node_modules", ".venv"],
    "max_parallel": 8,
    "reconcile_on_read": true
  },
  "notify": {
    "enabled": true,
    "poll_interval_min": 10,
    "push_interval_min": 5,
    "events": ["dotfile", "secret", "bloat", "storage"]
  }
}
```

`manifest.json`:

```json
{
  "manifest_schema": 1,
  "dotfiles": [
    {
      "system": "~/.ssh",
      "name": "ssh",
      "sudo": false,
      "note": "SSH keys and proxy jump config"
    },
    {
      "system": "~/.bashrc",
      "name": "bashrc",
      "sudo": false
    },
    {
      "system": "/etc/docker/daemon.json",
      "name": "daemon.json",
      "sudo": true
    },
    {
      "system": "~/.kube/config",
      "name": "kubeconfig",
      "sudo": false
    },
    {
      "system": "~/.config/Code/User/settings.json",
      "name": "vscode-settings.json",
      "sudo": false
    }
  ],
  "secret": {
    "allowlist": [
      "configs/myapp/app.properties:14",
      "experiments/debug-auth.sh:3"
    ]
  },
  "repo": {
    "tracked": [
      {
        "path": "notes/second-brain",
        "branch": "main",
        "remote": "origin"
      },
      {
        "path": "data/bruno",
        "branch": "master",
        "remote": "origin"
      }
    ]
  }
}
```

`config_schema` and `manifest_schema` are top-level integers for forward-compatible migrations. The binary refuses files with unsupported higher schema versions and prints an upgrade prompt.

`trash.root_dir` is a machine-local preference used by setup workflows. It defaults to `~/.Trash` and should generally stay outside `<workspace>`.

---

## `ws` Directory Structure

`ws` keeps its synced control/state data under `<workspace>/ws/`.

Directory layout:

```text
<workspace>/
└── ws/
  ├── config.json         # user-managed configuration
  ├── manifest.json       # ws-managed durable metadata (dotfiles, allowlists)
  ├── provisions.json     # ws-managed provisioning ledger (external side-effects)
  ├── megaignore.state    # ws-managed canonical ignore state
  ├── repo.state          # ws-managed repo cache/index (workspace-only repos)
  ├── health.json         # ws-managed daemon scan results (runtime-only, excluded from sync)
  ├── notify.state        # ws-managed daemon lifecycle + dedup state
  ├── ws-log-index.md     # ws-managed log index/summary
  ├── dotfiles/            # ws-managed originals captured by ws dotfile add (also git repo when git versioning enabled)
  │   ├── bashrc
  │   ├── ssh/
  │   ├── kubeconfig
  │   └── ...
  └── ws-log/             # ws-managed session logs
    └── <tag>/
      ├── stdin.log
      └── stdout.log  # present when stdout capture is enabled
```

Ownership and edit policy:

- `config.json`: edit by hand (or override via flags/env).
- `manifest.json`: do not hand-edit; managed by `ws dotfile *`, `ws secret fix`, and `ws repo` reconciliation.
- `provisions.json`: do not hand-edit; managed by all RW commands that create external side-effects. Read by `ws reset` to reverse provisions.
- `repo.state`: generated/updated by `ws repo` commands. Safe to delete (recreated by reconcile scan).
- `dotfiles/`: managed by `ws dotfile add|rm`. When git versioning is enabled via `ws dotfile git setup`, this directory also serves as the git repo. Files here are the synced originals — system paths are symlinks pointing back. Safe to edit the contents (they *are* your dotfiles), but don't move or rename files manually; use `ws dotfile rm` instead.
- `megaignore.state`: generated/updated by `ws ignore` commands (canonical parsed/normalized ignore state).
- `health.json`: generated/updated by `ws notify daemon` after each scan. Runtime-only state — excluded from MEGA sync. Consumed by `ws tui` and `ws notify status`.
- `notify.state`: generated/updated by `ws notify start|stop|daemon`. Tracks daemon lifecycle, last-scan time, and known violations for notification deduplication.
- `ws-log-index.md`: generated/updated by `ws log` commands.
- `ws-log/`: created/rotated/pruned by `ws log start|ls|prune`.

Design intent:

- Keep all portable tool state colocated and synced with the workspace.
- Avoid hidden state outside the workspace for restore/reproducibility.
- Keep scratch output in `scratch.root_dir` (outside the workspace, never synced), while `ws log` remains in `ws/` by design.

---

## Resolution Order

```text
Flag  →  Environment variable  →  ws/config.json  →  Built-in default
 ↑ highest                                                ↑ lowest
```

The config file path resolves first (flag → env → `<workspace>/ws/config.json`). Then remaining values fill in left-to-right: flag wins over env, env wins over config, config wins over default. Durable operational metadata (dotfile registry, secret allowlists, tracked repos) is loaded from `<workspace>/ws/manifest.json`; repo cache/index data is loaded from `<workspace>/ws/repo.state`.

---

## Violation Types, Groups, and Remediation

`ws` classifies findings into groups. Each group is owned by a specific subsystem that detects and fixes it.

| Violation type | Group | Output subtype (`TYPE` column) | Primary detector | Fix command |
| --- | --- | --- | --- | --- |
| Oversized file | Ignore | `bloat` | `ws ignore scan` | `ws ignore fix` → move to scratch / add `.megaignore` rule |
| Excessive depth | Ignore | `depth` | `ws ignore scan` | `ws ignore fix` → add `.megaignore` rule |
| Project-meta artifact | Ignore | `project-meta` | `ws ignore scan` | `ws ignore fix` → add `.megaignore` rule / move to scratch |
| Secret pattern match | Secret | `secret` | `ws secret scan` | `ws secret fix` → view context / add `.megaignore` / allowlist |
| Broken dotfile symlink (`BROKEN`) | Dotfile | `BROKEN` | `ws dotfile scan` | `ws dotfile fix` |
| Overwritten dotfile symlink (`OVERWRITTEN`) | Dotfile | `OVERWRITTEN` | `ws dotfile scan` | `ws dotfile fix` |
| Trash size exceeds threshold | Trash | `trash-size` | `ws trash status` | `ws trash enable` → re-enable integrations |

---

## Technical Design Decisions

Implementation choices and rationale behind key subsystems. These designs are interface-agnostic — they apply regardless of whether `ws` is invoked via CLI, daemon, TUI, or API.

### Batch Resilience: No Single Failure Breaks the Chain

One bad file must never kill an entire operation.

A workspace contains thousands of files across dozens of projects. At any moment some of those files will be broken symlinks, vanished directories, permission-denied paths, partially written blobs, or race-condition ghosts that exist in a directory listing but are gone by the time you stat them. This is normal. The tool must absorb it.

**Hard rule:** When `ws` runs a batch operation — scanning, fixing, restoring, pruning, or any command that iterates over multiple items — a failure on any single item must **never** abort the entire operation. The operation continues, collects partial results, and reports what it could not process.

Policy:

- **Scan walks** (`ws ignore scan`, `ws secret scan`, `ws dotfile scan`) skip unreadable entries (broken symlinks, missing directories, permission errors) and continue. Results from healthy files are still returned.
- **Batch mutations** (`ws restore`, `ws prune`) process every item they can and report per-item failures at the end.
- **Exit codes** reflect the worst outcome across all items, not the first failure. Exit 0 = clean. Exit 2 = violations found. Exit 1 = infrastructure error (workspace not initialized, config unreadable). A skipped broken symlink is not exit 1.

Implementation rules:

- `filepath.WalkDir` callbacks return `nil` on entry errors, not the error itself.
- `d.Info()` and `os.Open()` errors inside walk callbacks are skipped, not propagated.
- Command orchestrators collect errors from subsystems into a warnings slice instead of returning early.
- Warnings are surfaced in the output (both text and JSON modes) so the user knows what was skipped.

The user sees everything that worked plus a clear list of what didn't. They never see a panic or a truncated output because one file in a 10,000-file tree was momentarily unreadable.

### Output Styling: Colors, Icons, and Formatting

`ws` uses ANSI colors and Unicode icons to make CLI output scannable at a glance. All styling is implemented in `internal/style/` — a zero-dependency package using raw ANSI escape sequences (no third-party color libraries). Every rendering function accepts a `noColor bool` parameter for testability and graceful degradation.

**Color palette (semantic roles):**

| Role | ANSI Code | Usage |
| --- | --- | --- |
| Success | Green (32) | OK states, completed actions, clean results |
| Error | Red (31) | Critical issues, failures, broken state |
| Warning | Yellow (33) | Warnings, attention needed, non-critical |
| Info | Cyan (36) | Paths, values, highlighted data |
| Muted | Dim (2) | Secondary info, metadata, hints |
| Accent | Blue (34) | Headers, command names, emphasis |
| Bold | Bold (1) | Section titles, key labels |
| Critical | Bold+Red (1;31) | CRITICAL severity badge |

**Icon vocabulary:**

| Icon | Plain fallback | Semantic meaning |
| --- | --- | --- |
| `✔` | `[ok]` | Success / completed |
| `✖` | `[err]` | Error / failure |
| `▲` | `[warn]` | Warning / attention |
| `●` | `(*)` | Active / recording |
| `→` | `->` | Direction / mapping |
| `ℹ` | `[i]` | Informational |
| `🔒` | `[lock]` | Secret / security |
| `⎇` | `[git]` | Git repository |

**Status badges:** Words like `OK`, `CRITICAL`, `WARNING`, `SYNCED`, `IGNORED`, `DIRTY`, `CLEAN` are rendered as colored badges — uppercase text painted in the severity's color. This makes scan output visually parseable without reading every word.

**Structural elements:**

- **Headers:** Bold title + dimmed divider line (`────────`). Used by `ws tui`, `ws restore`.
- **Key-value rows:** Bold left-aligned label (18-char column) + value. Used by `ws version`, `ws trash status`.
- **Result lines:** Icon + message (`✔ Workspace is clean.`, `▲ Violations found: 3`). Used by every command's success/failure path.
- **Severity counters:** `0 critical · 2 warning` with conditional coloring — zero counts are dimmed, non-zero counts use severity color.

**Color suppression:**

Color is automatically disabled when any of these conditions is true:

1. `--no-color` flag is passed.
2. `NO_COLOR` environment variable is set (per [no-color.org](https://no-color.org) convention).
3. `TERM=dumb` is set.

When color is disabled, Unicode icons fall back to ASCII text equivalents (`✔` → `[ok]`, `→` → `->`), and all ANSI escape codes are stripped. JSON output (`--json`) bypasses the style system entirely — it is always uncolored structured data.

**Design rationale:** Colored output reduces cognitive load for repeated operations like `ws repo scan` where the user needs to spot problems in a list. The icon + badge system creates a visual language: green check = safe, red cross = broken, yellow triangle = needs attention. The `internal/style/` package centralizes all visual decisions so adding a new command means calling `style.ResultSuccess()` instead of hand-crafting `fmt.Fprintf` with embedded escape codes.

### Session Recording: PTY Mode (Default)

`ws log` records sessions using PTY mode by default. It spawns a child shell via `script(1)` and captures both stdin and stdout.

| | PTY mode (default) |
| --- | --- |
| Mechanism | `script(1)` PTY wrapper |
| Captures | Commands + full terminal output |
| Shell impact | Spawns child shell |
| Dependencies | `script` (util-linux ≥ 2.35) |
| Overhead | Moderate (PTY layer) |
| Exit | `exit`, Ctrl-D, or `ws log stop` |

**Why PTY mode is the default:** It provides complete auditability for interactive work, including SSH sessions, full-screen editors, and pasted terminal input.

### Prompt Indicator `● ws:log`

Recording prepends `● ws:log` to the shell prompt (`PS1` in bash, `PROMPT` in zsh) as a compact visual cue that recording is active. The dot is colored red via ANSI escape (`\[\033[31m\]●\[\033[0m\] ws:log`) in supported terminals; in `--no-color` mode it falls back to `(rec)`.

- **PTY mode:** Sets `PS1="● ws:log $PS1"` in the child shell's environment. Disappears automatically when the child shell exits.

Design rationale: the red dot catches the eye instantly (like a camera recording indicator), while `ws:log` identifies the source without ambiguity. At 8 characters total it is compact enough for all-day recording without dominating the prompt.

Safety properties:

- **Ephemeral.** Only affects the current session. Nothing is written to `.bashrc` or `.zshrc`.
- **Non-destructive.** Prepends to the existing prompt, preserving the user's customizations.
- **Opt-out.** Pass `--no-prompt` to disable.

### Dotfile Severity Model

`ws dotfile scan` classifies dotfile symlink health into three states, sorted by impact:

| Status | Severity | Meaning | Impact |
| --- | --- | --- | --- |
| `BROKEN` | CRITICAL | Symlink target is missing — the system path points to nothing | Functionality completely lost. Program using this config will fail or fall back to defaults. |
| `OVERWRITTEN` | WARNING | System path is a real file, not a symlink — someone (or a package manager) replaced it | Workspace copy is stale. Edits go to the local file, not the synced workspace. Data divergence. |
| `OK` | — | Symlink resolves correctly and target exists | No action needed. |

### Deletion Model: Soft Delete via Machine Setup

`ws` executes delete operations via `rm`. It does not implement a separate trash engine. Instead, it helps configure the machine so common delete paths are soft-delete by default.

Rules:

1. `ws` delete paths (`ws dotfile rm`, project-meta delete actions, prune operations) call `rm`.

2. `ws trash setup` configures machine-local tooling so delete behaves as soft-delete (shell `rm`, VS Code delete, file explorer delete).

3. `trash.root_dir` (default `~/.Trash`) is a setup target for these integrations.

4. `ws trash status` warns when soft-delete setup is missing on the current machine.

Rationale: keep the binary simple and Linux-native while still enforcing safe deletion posture across everyday tools.

### Dotfile Git Versioning (Optional)

`ws dotfile` can optionally version `ws/dotfiles/` in a Git repository for backup and history.

- Default is off (`dotfile.git.enabled=false`) so existing workflows are unchanged.
- When enabled, successful dotfile write operations (`ws dotfile add`, `ws dotfile rm`, `ws dotfile fix`) stage + commit dotfile and registry changes.
- Auto-push defaults to on (`dotfile.git.auto_push=true`).
- When auto-push is enabled, network reachability is the only gate for remote sync: if online, push now; if offline, keep commits locally and retry on the next dotfile Git operation.

Local repository model:

1. `ws` manages the local dotfile Git repository under `<workspace>/ws/` (not user-controlled).
2. Users configure only remote connection metadata: remote URL, username, optional pass entry, and branch.
3. `ws` initializes and maintains the local repository lifecycle internally.

Credential policy (never plain text in config):

1. `ws` never stores git passwords/tokens in `ws/config.json`, `ws/manifest.json`, or any ws state file.
2. `ws` first uses native Git credential helpers when available.
3. If helper-based auth is unavailable, `ws` silently checks for Unix Password Store (`pass`) and uses `pass_entry` (or an auto-derived default entry).
4. If no helper/pass credential is available, `ws` prompts the user and keeps the secret in memory for that operation only.
5. Only non-secret metadata is persisted in config.

This follows an Obsidian-style approach: persist connection metadata, fetch secrets at runtime.

Private-repo enforcement (hard constraint — not configurable):

Dotfiles contain high-sensitivity material by nature: SSH private keys and proxy-jump configs, Kubernetes cluster credentials, Docker daemon settings, shell histories baked into `.bashrc`. Pushing these to a public repository is an irreversible secret leak. `ws` treats this as a design constraint, not a preference.

1. **On connect:** `ws dotfile git connect` queries the hosting provider API to verify the target repository is private. If the repo is public, connect is refused with an error — no override flag exists.
2. **On every push:** Before each push, `ws` re-checks visibility. Repositories can be flipped to public after initial setup; the pre-push check catches this.
3. **Known providers:** GitHub (`GET /repos/{owner}/{repo}` → `"private": true`), GitLab (`GET /projects/:id` → `"visibility": "private"`), Bitbucket (`GET /repositories/{workspace}/{repo}` → `"is_private": true`). SSH URLs are translated to API endpoints for the check.
4. **Unrecognized hosts (self-hosted):** If the provider has no known visibility API, `ws` prints a prominent warning that it cannot verify repository visibility, but allows the operation. The user accepts the risk for self-hosted infrastructure.
5. **Auth for the check:** The same credential resolution chain (git helper → `pass` → prompt) is used for API calls. On GitHub, an unauthenticated `GET /repos/{owner}/{repo}` returning 404 is ambiguous (private or nonexistent), so the authenticated check is preferred.
6. **No opt-out.** There is no config field, flag, or environment variable to disable this check. It is a design constraint on the same level as soft-delete-first.

Repository attach rules:

1. If the ws-managed local repo already exists, `ws` reuses it and continues.
2. If missing, `ws` initializes it automatically inside `<workspace>/ws/`.
3. The local repo path is internal and not accepted from user input.
4. Users can run dotfile management without Git for months and configure remote backup later without changing local dotfile layout.

Automatic merge-conflict handling (no commit loss):

1. Before push, `ws` runs `git fetch` for the configured remote/branch.
2. `ws` integrates remote changes using rebase-first (`git pull --rebase --autostash`).
3. If rebase reports conflicts, `ws` aborts rebase and retries with a non-destructive merge commit path.
4. If conflicts still remain, `ws` writes conflict markers, preserves both histories, and creates a timestamped safety branch (`ws/dotfile-conflict-<ts>`) before returning a conflict status.
5. `ws` never uses `git reset --hard` or force-push in this flow.

### Secret Allowlist: Line-Anchored Entries

Secret violations can be marked as known false positives via `ws secret fix`. Each allowlist entry is stored in `manifest.json` as `file:line` (e.g. `configs/myapp/app.properties:14`).

If the file changes and the pattern moves to a different line, the allowlist entry no longer matches and the violation reappears. This prevents stale allowlist entries from hiding new secrets — the user must re-confirm after any file change that shifts line numbers.

### Ignore Merge Logic

When merging `.megaignore` rules (`ws ignore generate --merge`), `ws` compares rules by their normalized form (ignoring comments and whitespace). Template rules that already exist in the current file are skipped. New template rules are appended in the template's section order. Custom rules (present in the current file but not in the template) are always preserved — they are never removed by a merge. The `-s:*` line is always placed last.

After any successful fix/merge/edit, `ws` refreshes `<workspace>/ws/megaignore.state` from the effective `<workspace>/.megaignore`.

### Safe Harbor Directories

The `.megaignore` template excludes aggressively — compiled output, logs, archives, datasets are all blocked globally. But some directories need to sync *everything*, regardless of extension. These are **safe harbors**: directories with `+:`/`+g:` include overrides placed *after* all exclude rules, exploiting megaignore's last-match-wins evaluation order.

The built-in template defines two safe harbors:

| Directory | Purpose | Why it needs override |
| --- | --- | --- |
| `ws/` | Tool metadata, session logs, config | `ws/ws-log/<tag>/stdin.log` would be excluded by `-g:*.log` without the override. |
| `Archive/` | Configs, credentials, machine state | Intentional blobs — a VPN client `.deb`, an incident log dump, a backup `.tar.gz` — are legitimate here. Factor I: "Every config gets synced. No exceptions." |

**Rule ordering is load-bearing.** The safe harbor includes *must* appear after all global excludes. If they were placed earlier (e.g. in the OS junk section), a later `-g:*.log` would re-exclude `ws/ws-log/tag/stdin.log`. The current placement — immediately before the `-s:*` sentinel — ensures they are the final word.

```text
# Evaluation for Archive/incident-2026.tar.gz:
#   1. -g:*.tar.gz          → EXCLUDED
#   2. +g:Archive/**         → INCLUDED  (last match wins)
#   Result: SYNCED ✔

# Evaluation for Experiments/junk.tar.gz:
#   1. -g:*.tar.gz          → EXCLUDED
#   2. (no include matches)
#   Result: EXCLUDED ✔
```

**Interaction with violation scanning:** `ws ignore scan` detects violations inside safe harbors but **downgrades their severity to INFO**. These items don't count as actionable violations: they don't trigger exit code 2, they don't appear in `ws ignore fix`, and they render in muted/dim styling. By default, safe harbor items are collapsed to a single summary line:

```text
Violations (3)
  CRITICAL  bloat          Experiments/dataset.bin  (420.0 MB exceeds critical threshold 10.0 MB)
  WARNING   depth          Projects/vendor/deep/pkg  (depth 14 exceeds max 6)
  WARNING   project-meta   Projects/app/node_modules  (Node project build artifact directory should be excluded)

Safe harbors (47 items, 2.3 GB)
  Use --expand-harbors to see details.
```

Pass `--expand-harbors` to list each safe harbor item individually. `ws ignore check` shows the safe harbor override in its output so the user knows *why* a `*.tar.gz` file is syncing (with a `[safe harbor]` annotation).

**Adding new safe harbors:** Users can add more safe harbor directories by appending `+:DirName` / `+g:DirName/**` in the safe harbors section of `.megaignore`. The `ws ignore edit` post-save validation warns if include rules appear before exclude rules (order-dependent correctness).

### Project-Meta Detection (Dynamic Artifact Discovery)

The static `.megaignore` template covers universally-safe exclusions — compiled extensions (`*.class`, `*.pyc`), logs, lock files, archives. But some artifact directories have names that are ambiguous without context: `bin/` could be compiled Java output or hand-curated shell scripts; `dist/` could be webpack output or a distribution source directory; `target/` could be Maven output or a deployment target. Adding these as static rules would cause false positives.

`ws ignore scan` solves this with **project-marker heuristics**: it detects the *type* of project in a directory and flags its expected build outputs as `project-meta` violations only when a confirming marker is present.

**Detection model:**

1. Walk the workspace for known project marker files.
2. For each marker, check if the associated build output directories/files exist.
3. If the output exists **and** is not already excluded by a `.megaignore` rule, emit a `project-meta` violation.

**Project-marker heuristic table:**

| Marker file(s) | Project type | Expected build outputs |
| --- | --- | --- |
| `*.java`, `pom.xml` | Java (Maven) | `target/`, `bin/`, `*.class` |
| `*.java`, `build.gradle` | Java (Gradle) | `build/`, `bin/`, `*.class` |
| `go.mod` | Go | `bin/` |
| `Cargo.toml` | Rust | `target/` |
| `Makefile`, `CMakeLists.txt` | C/C++ | `build/`, `*.o`, `*.so` |
| `Gemfile` | Ruby | `Gemfile.lock`, `.bundle/`, `_site/` (if Jekyll) |
| `package.json` | Node.js | `node_modules/`, `dist/`, `build/`, `package-lock.json`, `yarn.lock`, `pnpm-lock.yaml` |
| `pyproject.toml`, `setup.py`, `setup.cfg` | Python | `*.egg-info/`, `dist/`, `build/`, `*.pyc`, `__pycache__/` |
| `composer.json` | PHP | `vendor/`, `composer.lock` |
| `*.sln`, `*.csproj` | .NET/C# | `bin/`, `obj/` |

**Scope:** Detection walks only `<workspace>` (same roots used by other ignore commands). It does **not** descend into directories that are already excluded by `.megaignore` (e.g. `node_modules/` excluded by `-:node_modules` is never walked). Safe harbor directories (`Archive/`, `ws/`) are exempt from project-meta scanning — files there are intentionally synced regardless of type.

**Severity:** All `project-meta` violations are `WARNING` severity. They are never `CRITICAL` — the files are small and reproducible; the concern is sync noise, not data loss.

**Interaction with static rules:** If a build output is already covered by a static glob rule in `.megaignore` (e.g. `*.class` is caught by `-g:*.class`), no violation is emitted for that output. The heuristic only fires when the output would actually be synced.

**Design rationale:** This approach avoids polluting `.megaignore` with rules for every conceivable build tool. The static template catches extension-based artifacts universally. The dynamic scan catches directory-based artifacts contextually. Together they form a complete coverage model without false positives.

### Scratch Directory Management

`ws scratch` creates throwaway working directories outside the workspace for debug sessions, investigations, ad-hoc tasks, and infra problems. Scratch directories live under `scratch.root_dir` (default `~/Scratch`) — never inside `<workspace>`, never synced.

The command is optimized for capture speed (Philosophy Factor II): type a name, get a directory, land in VS Code. The user manually promotes valuable scripts and notes to the synced `<workspace>` when they're worth keeping — `ws` doesn't try to be smart about what deserves sync.

**Ghost suggestions:** When `ws scratch new` or `ws scratch rm` is invoked without a name argument on a TTY, a live ghost panel renders below the prompt line and updates on every keystroke. The panel lists existing directories filtered by the current input (date suffixes stripped for matching), capped at 5 rows with an overflow count. For `new`, the panel is display-only — Tab does nothing, because you are picking a name *distinct from* the ones shown. For `rm`, Tab completes the input to the first matched entry. Calling with a name argument (`ws scratch new proxy-debug`) bypasses the prompt entirely. On non-TTY (pipe, `--json`, test), falls back to a plain line read with no panel.

**Name resolution:** When `scratch.name_suffix` is `"auto"` (default), the final directory name is suffixed with `.YYYY-MM` (e.g. `proxy-auth-header.2026-04`). If the exact name already exists, `-2`, `-3` etc. are appended. Empty input generates `scratch-<short-hash>.YYYY-MM`.

**Editor launch:** After `mkdir -p`, `ws` runs the configured `scratch.editor_cmd` (default `code`) to open the new directory. If the command is not found, `ws` prints the path, skips the open, and exits 0 with a warning. Pass `--no-open` to skip the editor launch.

**Scratch metadata:** Each scratch directory gets a `.ws-meta.json` file seeded on creation (with `created` timestamp and empty `tags` array). Tags are added later via `ws scratch tag`, which presents a multi-value ghost input showing popular tags from the workspace tag collection (`<workspace>/ws/tags.json`). Enter commits a tag and loops; empty Enter finishes. Tab completes to the first matching suggestion. New tags are automatically added to the workspace collection for future suggestions.

**Auto-tagging:** `ws scratch tag --auto` scans files in the scratch directory and proposes tags based on heuristics: file extensions (`.sh` → `bash`, `.py` → `python`), known file names (`Dockerfile` → `docker`, `Makefile` → `make`), shebangs, and content patterns (`kubectl` → `k8s`, `systemctl` → `systemd`, `cgroup` → `cgroups`). Each suggestion is presented as an action in the standard plan/confirm flow.

**Scratch search:** `ws scratch search <query>` finds scratch directories matching by tag (score 3), directory name (score 2), or file content (score 1). Multi-token queries use AND logic. Without arguments, enters interactive ghost mode showing all scratch dirs with tags, filterable as you type. Supports `--json` and `--max`.

`ws scratch ls` shows tags inline when present: `my-debug.2026-04  age=2d  size=1.2M  items=7  [k8s, pid-limit]`.

`ws scratch` is a standalone utility.

### Repository Fleet Operations (Workspace-Only, Stateful with Reconciliation)

`ws repo` manages only Git repositories inside `<workspace>`, but it is stateful: repo metadata is cached in `ws/repo.state` and durable references are stored in `manifest.json`.

Every repo command that reads state performs a reconcile phase first whenever possible:

1. Scan current workspace roots (`repo.roots`) for live repos.
2. Match discovered repos against cached/tracked entries.
3. Auto-heal moved paths when Git identity still matches.
4. Mark missing entries as stale (warning), not fatal.
5. Add newly discovered repos to state when command scope includes them.

This makes state helpful for speed and UX while keeping commands grounded in current on-disk reality.

Read commands (`ws repo ls`, `ws repo scan`) are non-interactive and pipe-safe. Write commands (`ws repo pull`, `ws repo sync`, `ws repo run`) are interactive by default and support `--dry-run`.

### Context Sidecar: Local Git Exclusion

`ws context create` creates a `.ws-context/` directory alongside the project it supports. This directory holds task-scoped agent context (design notes, constraints, resources) that should stay close to the code but never be pushed to a remote.

Git exclusion uses `.git/info/exclude` — a per-clone ignore file that is never committed, never pushed, and leaves zero trace in the repository. This is preferred over `.gitignore` (which would be committed and expose `ws` conventions to collaborators) and over `.git/config` sparse rules (which serve a different purpose).

The dot prefix (`.ws-context/`) makes the directory hidden on Linux. Inside `<workspace>`, hidden files are already excluded from MEGA sync by the `-:.*` rule in `.megaignore`, so no additional MEGA configuration is needed. For repos outside `<workspace>` (e.g. `~/Repositories/`), MEGA is not involved at all.

Idempotency: `ws context create` can be run multiple times safely. The directory is created only if absent. The `.git/info/exclude` entry is appended only if not already present. If the project is not a git repo, the exclude step is silently skipped — but a later invocation after `git init` will retroactively add the exclude rule.

---

## Prerequisites & External Dependencies

`ws` embraces the Unix philosophy: delegate to existing tools instead of reimplementing them. The binary orchestrates, formats, and manages state — the heavy lifting is offloaded to battle-tested system commands. This keeps the codebase small, avoids reimplementation bugs, and aligns with the "follow Linux standards" constraint.

### Tier 1 — Required (ship with every Linux)

Core utilities that `ws` calls as subprocesses. Present on every standard Linux distribution via **coreutils**, **findutils**, **diffutils**, **grep**, **util-linux**, and **sudo**.

| Tool | Package | What `ws` offloads |
| --- | --- | --- |
| `ln` | coreutils | Symlink creation (`ln -s`) and forced overwrite (`ln -sf`) |
| `readlink` | coreutils | Resolve and verify symlink targets (`readlink -f`) |
| `cp` | coreutils | Copy files between workspace and system paths (`cp -a` preserves metadata) |
| `mv` | coreutils | Move files into workspace or to scratch |
| `rm` | coreutils | Delete primitive used by `ws` (`rm -rf`); soft-delete behavior comes from machine setup |
| `mkdir` | coreutils | Create directories recursively (`mkdir -p`) |
| `stat` | coreutils | File metadata: size, modification time, type detection |
| `du` | coreutils | Directory size calculation (`du -sh`, `du --summarize`) |
| `sha256sum` | coreutils | Content identity — detect whether system and workspace files have diverged without reading both into memory |
| `find` | findutils | File tree walking with depth limits (`-maxdepth`), size filters (`-size +10M`), name patterns (`-name '*.sh'`), type filters (`-type f`) |
| `grep` | grep | Text search with context (`grep -rn -C N`), regex pattern matching (`grep -E`), binary file detection (`grep -I`) |
| `diff` | diffutils | Unified diff output (`diff -u`) for file comparison during conflict resolution |
| `file` | file | MIME type detection — distinguish text from binary files, identify file formats without relying on extensions |
| `sudo` | sudo | Privileged symlink creation for system paths like `/etc/docker/daemon.json` |
| `sh` | POSIX shell | POSIX shell for subcommand execution |

### Tier 2 — Expected (standard on most Linux desktops)

Present on most Linux desktop installations. `ws` checks availability at the moment a feature needs the tool and prints an actionable error if missing.

| Tool | Package | What `ws` offloads |
| --- | --- | --- |
| `script` | util-linux ≥ 2.35 | **Session recording — the single biggest offload.** `script --log-in stdin.log --log-out stdout.log -q bash` handles the entire PTY layer: spawns a child shell, allocates a pseudo-terminal, records input and output to separate files, and passes through the terminal transparently. This eliminates the need to implement PTY splitting from scratch (~2-4 weeks of work). The `--echo` flag controls input echo, `--log-timing` enables replay, `--flush` ensures real-time writes, and `-e` returns the child's exit code. `ws` manages the session lifecycle (tag, scratch dir, index update) around `script`. |
| `git` | git | Repo discovery/fleet operations and optional dotfile Git versioning. Git remains the source of truth for branch/remotes/status in each `.git/` directory; `ws` orchestrates batching, commits, and optional pushes. |
| `less` | less | Paginated display. Invoked automatically when stdout is a TTY and output exceeds terminal height. `ws` pipes through `less -R` (preserve ANSI colors). Falls back to direct stdout if `less` is unavailable. |
| `vi` / `vim` | vim-minimal | Fallback editor when `$EDITOR` and `$VISUAL` are unset. The tool resolution order is `$EDITOR` → `$VISUAL` → `vi`. |
| `tput` | ncurses-bin | Terminal capability queries: column width (`tput cols`), color support (`tput colors`). `ws` falls back to 80 columns / no color if unavailable. |
| `pgrep` | procps | Terminal emulator detection by inspecting the process tree when `$TERM_PROGRAM` is unset. `pgrep -a` identifies running terminal emulators. |

### Tier 3 — Optional (enhanced features)

Not required for any core functionality. `ws` detects availability and unlocks additional capabilities or higher accuracy. Missing tools trigger an informational hint, never an error.

| Tool | Package | What it enables |
| --- | --- | --- |
| `mega-ls` | mega-cmd | **Ground-truth sync verification.** Cross-references local `.megaignore` evaluation against MEGA's actual sync state. Output shows both the local rule result and the MEGA-confirmed status. Without it, ignore checking is a best-effort approximation based on local rule parsing only. |
| `mega-exclude` | mega-cmd | Query MEGA's exclusion engine directly for a path. More accurate than local rule parsing for edge cases in MEGA's matching semantics. |
| `dconf` | dconf-cli | Read/write GNOME Terminal and Tilix profiles (both use `dconf`, not config files). Required only for these two terminal emulators. Kitty, Alacritty, and Konsole use file-based configs and don't need this. |
| `pass` | pass | Optional credential source for dotfile Git backup. `ws` reads credentials from password-store entries at runtime instead of config. |
| `jq` | jq | Not called by `ws`. Recommended for processing `--json` output in user scripts. Referenced in documentation examples (`ws ignore scan --json \| jq '.data.violations[]'`). |

### Delegation Map

Summary of what `ws` builds natively vs what it offloads, and why.

| Responsibility | Approach | Used by | Rationale |
| --- | --- | --- | --- |
| JSON config parsing | **Native** (stdlib JSON decoder) | All commands | No custom parser needed; low complexity and auditable behavior. |
| Dotfile registry state | **Native** (read/write `ws/manifest.json`) | `ws dotfile *` | Core state management — the registry is the heart of dotfile tracking and restore. |
| JSON output generation | **Native** | All commands (`--json`) | Controls the schema envelope. No template engine needed. |
| Interactive prompts | **Native** | Write commands | Custom rendering with defaults, color, quiet suppression. |
| Output formatting | **Native** | All commands | Tables, progress lines, ANSI colors, Unicode/ASCII toggle. |
| Exit code logic | **Native** | All commands | Structured exit codes (0/1/2/3) depend on command-specific analysis. |
| Symlink creation & verification | **Offload** → `ln`, `readlink` | `ws dotfile add`, `ws dotfile scan`, `ws dotfile fix` | Handles edge cases: existing targets, dangling links, relative vs absolute, permissions. |
| File operations | **Offload** → `cp -a`, `mv`, `rm -rf` | `ws dotfile add`, `ws dotfile rm`, `ws dotfile fix` | Cross-device moves, permission preservation, recursive operations. Soft-delete behavior depends on machine-level setup around `rm`. |
| File tree scanning | **Offload** → `find` | `ws ignore scan`, `ws scratch ls` | Depth limiting, size filtering, type filtering, name patterns. |
| Repo discovery + reconcile | **Offload** → `find`, `git rev-parse` | `ws repo ls`, `ws repo scan`, `ws repo *` | Reconciles cached repo state with live workspace before command execution. |
| Git fleet operations | **Offload** → `git` | `ws repo fetch`, `ws repo pull`, `ws repo sync`, `ws repo run` | Git owns per-repo state in `.git/`; `ws` coordinates fan-out execution and aggregate reporting. |
| Dotfile Git versioning | **Offload** → `git` | `ws dotfile git *`, `ws dotfile add`, `ws dotfile rm`, `ws dotfile fix` (when enabled) | Optional commit/push of dotfile registry and content for private backup history. Private-repo enforcement: `ws` verifies remote visibility via provider API on connect and before every push — public repos are rejected (hard constraint, not configurable). Credentials resolve in Obsidian-style order: git helper → `pass` → prompt. |
| Secret scanning | **Offload** → `grep -rn -E` | `ws secret scan` | `ws` provides patterns, `grep` does the scanning. |
| File comparison | **Offload** → `diff -u` | `ws dotfile add` (conflict), `ws dotfile fix` | Unified diff for conflict resolution. Battle-tested format. |
| File identity | **Offload** → `sha256sum` | `ws dotfile scan`, `ws dotfile add` | Fast content comparison without reading files into memory. |
| Binary/text detection | **Offload** → `file --mime-type` | `ws ignore scan` | MIME-based detection beats extension-based guessing. |
| Session recording (PTY default) | **Offload** → `script` | `ws log start` | Full PTY capture of stdin/stdout by default. Requires `script` from util-linux. |
| Paginated display | **Offload** → `less -R` | `ws log ls` | Standard Unix paging when stdout is a TTY. |
| Editor integration | **Offload** → `$EDITOR` / `$VISUAL` / `vi` | `ws ignore edit` | Standard editor resolution chain. |
| Directory size calculation | **Offload** → `du -sh` | `ws scratch ls` | Faster than manual accumulation, handles filesystem edge cases. |
| MEGA sync verification | **Offload** → `mega-ls` (optional) | `ws ignore check` | Ground-truth check that local rule parsing can't guarantee. |

### Build Dependencies

Required only if compiling `ws` from source. Not needed on machines that receive a pre-built binary.

| Tool | Version | Purpose | Install |
| --- | --- | --- | --- |
| `go` | ≥ 1.23 | Compile the `ws` binary (statically linked) | [go.dev/dl](https://go.dev/dl/) |
| `git` | any | Clone the source repo, embed build metadata | `sudo apt install git` |
| `make` | any | Build automation (optional — `go build` works alone) | `sudo apt install make` |

### MEGAsync (Desktop Sync Client)

The core of the workspace backup strategy. Syncs `~/Workspace` to MEGA cloud continuously.

```bash
# Ubuntu/Debian
wget https://mega.nz/linux/repo/xUbuntu_22.04/amd64/megasync-xUbuntu_22.04_amd64.deb
sudo dpkg -i megasync-xUbuntu_22.04_amd64.deb
sudo apt-get install -f
```

### mega-cmd

CLI companion to MEGAsync. Needed for `ws ignore check` ground-truth verification and direct sync control.

```bash
# Ubuntu/Debian
wget https://mega.nz/linux/repo/xUbuntu_22.04/amd64/megacmd-xUbuntu_22.04_amd64.deb
sudo dpkg -i megacmd-xUbuntu_22.04_amd64.deb
sudo apt-get install -f
```

Key commands: `mega-sync`, `mega-ls`, `mega-exclude`, `mega-login`.

### Secrets & Encryption

| Tool | Package | Purpose | Install |
| --- | --- | --- | --- |
| `gnupg` / `gpg` | gnupg | GPG key management — encrypt/decrypt, sign, import keys | `sudo apt install gnupg` |
| `pass` | pass | Unix password store — GPG-encrypted, git-backed secrets | `sudo apt install pass` |

### Desktop Applications

Not installed via `apt`. Separate downloads or managed through other channels.

| Application | Purpose | Install |
| --- | --- | --- |
| **VS Code** | Primary code editor. Settings managed via `ws dotfile`. | [code.visualstudio.com](https://code.visualstudio.com/) |
| **Obsidian** | Knowledge base / second brain. Vault at `notes/second-brain/`. | [obsidian.md](https://obsidian.md/) |
| **Bruno** | API collections (version-controlled). Data at `data/bruno/`. | [usebruno.com](https://www.usebruno.com/) |
| **Vikunja** | Task management. Data at `data/vikunja/`. | [vikunja.io](https://vikunja.io/) |

---

## Global Flags

```text
ws [command] [flags]

--workspace, -w   Path to sync root        (default: <workspace>)
--config,    -c   Path to config file      (default: <workspace>/ws/config.json)
--manifest       Path to manifest file      (default: <workspace>/ws/manifest.json)
--quiet,     -q   Errors only — suppress progress, summaries, and informational output
--verbose         Show internal decisions (path resolution, config loading, symlink checks)
--json            Output as JSON (all commands)
--dry-run         Preview actions, make no changes (silently ignored on read-only commands)
--no-color        Disable colored output and Unicode symbols (also honored via NO_COLOR env var)
```

---

## Flag Semantics

**`--quiet`** — Suppresses all output except errors and the final exit code. Progress lines, summaries, and informational messages are hidden. Errors are always printed to stderr. Useful for scripting where only the exit code matters.

**`--verbose`** — Prints internal decisions to stderr as `[verbose]`-prefixed lines. Useful for debugging path resolution, config loading, symlink verification, and permission checks. Can be combined with `--json` (verbose goes to stderr, JSON goes to stdout).

**`--dry-run`** — On write commands: shows the plan without executing. On read-only commands: silently ignored — the command runs normally since it already makes no changes.

**`--no-color`** — Disables ANSI color codes and replaces Unicode symbols (`✔ ⚠ ✗`) with ASCII equivalents (`[ok] [warn] [err]`). Automatically activated when stdout is not a TTY (e.g. piped to a file or another command). Also activated when the `NO_COLOR` environment variable is set to any non-empty value, per the [no-color.org](https://no-color.org) convention. The `--json` flag always produces uncolored, symbol-free output regardless of this setting.

**`--json`** — Every JSON response includes an envelope for schema stability:

```json
{
  "ws_version": "0.1.0",
  "schema": 1,
  "command": "scan",
  "data": { ... }
}
```

`ws_version` is the binary version. `schema` is an integer that increments on breaking changes to `data` structure. Downstream scripts should check `schema` and fail gracefully if it's higher than expected.

---

## Exit Codes

Every command returns a structured exit code. Scripts can branch on these without parsing output.

| Code | Meaning |
| --- | --- |
| `0` | Success — no issues |
| `1` | Error — bad input, crash, permission denied, missing config |
| `2` | Violations found — workspace is not clean |
| `3` | Partial success — some items succeeded, some skipped/failed |

---

## Command Summary

```text
ws version                           Print binary version, config/manifest schema, and platform

ws config view                       Dump resolved config from memory
ws config defaults                   Print default config as a valid ws/config.json file

ws init                              Scaffold a directory as a ws-compatible workspace
ws reset                             Reverse ws init — undo all provisions, remove ws/
ws restore                           Guided full-machine restore wizard

ws completions <shell>               Generate shell completions (bash/zsh/fish)
ws completions install               Install shell completions into shell rc/config
ws completions uninstall             Remove installed shell completions from shell rc/config

ws tui                               Interactive TUI dashboard

ws notify start                      Start the notification daemon
ws notify stop                       Stop the notification daemon
ws notify status                     Check daemon status
ws notify test                       Send a test notification
ws notify daemon                     Run daemon in foreground (used by systemd ExecStart)

ws trash setup                       Configure soft-delete integrations (rm, VS Code, file explorer)
ws trash disable                     Remove soft-delete integrations
ws trash status                      Check integration status and trash size

ws repo ls                           Discover git repos under workspace
ws repo scan                         Reconciled fleet status (branch, ahead/behind, dirty, stash)
ws repo fetch                        Fetch remotes across repos
ws repo pull                         Interactive fleet pull
ws repo sync                         Interactive fleet sync (pull/push per state)
ws repo run -- <cmd>                 Run command in each selected repo

ws context create <task>             Create a task-scoped context sidecar in the current repo/directory
ws context list                      List tracked context sidecars
ws context rm                        Remove a context sidecar (or all with --all)

ws dotfile add <system-path>          Capture a system file into ws/dotfiles/ + symlink back
ws dotfile rm <path>                 Restore file to system path, unregister
ws dotfile ls                        Show all registered dotfiles
ws dotfile scan                      Verify all dotfile symlinks (read-only)
ws dotfile fix                       Reconcile/enforce all dotfile symlinks from registry
ws dotfile reset                     Reset dotfile subsystem provisions
ws dotfile git remote <url>          Set/show git remote URL
ws dotfile git push                  Commit pending changes + push to remote
ws dotfile git log                   Show dotfile commit history
ws dotfile git status                Show dotfile git backup status
ws dotfile git setup                 Guided walk-through (init → remote → auto-push)
ws dotfile git disconnect            Disconnect dotfile git remote/config

ws log start                         Start a recorded session (PTY mode by default)
ws log stop                          Stop the current recording session
ws log ls                            List recorded sessions (tag, duration, size)
ws log prune                         Prune old sessions
ws log rm <tag>                      Remove one recorded session by tag

ws ignore check <path>               Test if a path would be synced
ws ignore ls                         List all excluded files (flat, pipe-safe)
ws ignore tree                       Browse workspace tree with sync status
ws ignore edit                       Edit .megaignore rules in $EDITOR
ws ignore scan                       Find bloat, depth, and project-meta violations
ws ignore fix                        Fix bloat, depth, and project-meta violations interactively
ws ignore generate                   Generate or merge .megaignore from built-in template

ws secret scan                       Scan workspace for exposed secrets
ws secret fix                        Resolve secret violations interactively
ws secret setup                      Setup/check Unix Password Store (pass)
ws secret status                     Show pass health, git state, and actionable warnings
ws secret git push                   Push pass store commits to remote
ws secret git log                    Show pass store commit history
ws secret git remote                 Show pass store git remote URL
ws secret git status                 Show pass store git status summary

ws git-credential-helper setup       Connect credential helper and create missing pass entries
ws git-credential-helper status      Check credential helper config and pass entry coverage
ws git-credential-helper disconnect  Remove ws credential helper from git config
ws git-credential-helper get         Look up credentials from pass (git plumbing — called by git)
ws git-credential-helper store       No-op (git plumbing — pass is managed separately)
ws git-credential-helper erase       No-op (git plumbing — pass is managed separately)

ws scratch new [name]                Create a named scratch directory, open in editor
ws scratch open [name]               Open an existing scratch directory in editor
ws scratch ls                        List scratch directories with age, size, items
ws scratch tag [name]                Add tags to a scratch directory
ws scratch search [query]            Search scratch directories by tag, name, or content
ws scratch prune                     Remove old scratch directories
ws scratch rm [name]                 Delete a scratch directory by name
```

---

## Commands

### `ws version`

Print binary version, config/manifest schema, platform, and build metadata. Useful for debugging, bug reports, and verifying that all machines run the same version.

```text
ws version [flags]

--short   Print only the semver string (e.g. "0.1.0")
```

**Output:**

```text
ws version

ws 0.1.0
  Config schema:   1
  Config path:     ~/Workspace/ws/config.json
  Manifest schema: 1
  Manifest path:   ~/Workspace/ws/manifest.json
  Platform:        linux/amd64
  Built:           2026-03-15T10:22:00Z
  Go version:      go1.23.1
```

**Output — short:**

```text
ws version --short
0.1.0
```

**Output — JSON:**

```json
{
  "ws_version": "0.1.0",
  "schema": 1,
  "command": "version",
  "data": {
    "version": "0.1.0",
    "config_schema": 1,
    "config_path": "~/Workspace/ws/config.json",
    "manifest_schema": 1,
    "manifest_path": "~/Workspace/ws/manifest.json",
    "platform": "linux/amd64",
    "built": "2026-03-15T10:22:00Z"
  }
}
```

---

### `ws init`

Scaffold a directory as a `ws`-compatible workspace. Creates the `ws/` metadata directory and populates it with default `config.json`, empty `manifest.json`, and a `.megaignore` file at the workspace root. Interactively guides the user through initial configuration.

`ws init` is primarily a scaffolding command — it does not apply dotfile symlinks. It creates workspace metadata (`ws/`, `.megaignore`) and optionally performs machine-level trash setup when enabled. After initialization, it points the user to subsystem-specific scan and fix commands for workspace hygiene.

**Init guard:** Every other `ws` command checks for the presence of `<workspace>/ws/config.json` before running. If the file does not exist, the command prints an error and exits:

```text
Error: workspace not initialized.
Run `ws init` to set up this directory as a ws workspace.
```

This applies to all commands except `ws version` and `ws init` itself.

```text
ws init [flags]

--workspace, -w   Path to workspace root (default: current directory, or ~/Workspace)
--dry-run         Preview all actions without applying
```

**What it does:**

| Step | Action | Interactive? |
| --- | --- | --- |
| 1. Workspace path | Confirm or set the workspace root directory | Prompt with default |
| 2. `ws/` directory | Create `<workspace>/ws/` | No |
| 3. `config.json` | Write default config (or skip if exists) | No |
| 4. `manifest.json` | Write empty manifest (or skip if exists) | No |
| 5. `.megaignore` | Generate from built-in template (or skip if exists) | Prompt: replace/merge/skip |
| 6. Trash setup | Configure machine soft-delete integrations | Prompt: yes/no + per integration |
| 7. Guidance | Print next steps | No |

**Output — fresh directory:**

```text
ws init
══════════════════════════════════════════════════════
 WORKSPACE INIT
══════════════════════════════════════════════════════

Workspace path: ~/Workspace  [Y/n]: ↵

── [1/3] Scaffolding ──────────────────────────────

✔ Created  ~/Workspace/ws/
✔ Created  ~/Workspace/ws/config.json       (default config)
✔ Created  ~/Workspace/ws/manifest.json     (empty registry)

── [2/3] Ignore rules ─────────────────────────────

✔ Generated ~/Workspace/.megaignore          (builtin template, 50 rules)

── [3/4] Trash setup ──────────────────────────────

Configure soft-delete on this machine? [Y/n]: ↵

  Shell rm wrapper/alias:     enable  [Y/n]: ↵
  VS Code delete-to-trash:    enable  [Y/n]: ↵
  File explorer soft-delete:  enable  [Y/n]: ↵

✔ Trash setup completed (root: ~/.Trash)

── [4/4] Done ─────────────────────────────────────

══════════════════════════════════════════════════════
 ~/Workspace is now a ws workspace
══════════════════════════════════════════════════════

Files created:
  ws/config.json       Edit config: ws config view
  ws/manifest.json     Managed by ws — do not hand-edit
  .megaignore           Edit rules:  ws ignore edit

Next steps:
  ws trash status        Verify soft-delete setup on this machine
  ws dotfile add <path>  Capture dotfiles into workspace management
  ws completions bash    Generate shell completions
```

**Output — MEGA-restored directory (`ws/` already exists with data):**

When `ws/config.json` and `ws/manifest.json` already exist (e.g. synced from another machine), `ws init` skips scaffolding and only fills in missing pieces. It never modifies existing config or manifest files.

```text
ws init
══════════════════════════════════════════════════════
 WORKSPACE INIT
══════════════════════════════════════════════════════

Workspace path: ~/Workspace  [Y/n]: ↵

── [1/3] Scaffolding ──────────────────────────────

✔ ws/config.json       already exists — skipped
✔ ws/manifest.json     already exists — skipped (5 dotfiles registered)

── [2/3] Ignore rules ─────────────────────────────

✔ .megaignore           already exists — skipped (22 rules)

── [3/4] Trash setup ──────────────────────────────

⚠ Soft-delete setup not detected on this machine.
Run `ws trash setup` now? [Y/n]: ↵
✔ Trash setup completed

── [4/4] Done ─────────────────────────────────────

══════════════════════════════════════════════════════
 ~/Workspace is now a ws workspace
══════════════════════════════════════════════════════

Detected 5 registered dotfiles in manifest.json.

Next steps:
  ws trash status        Verify soft-delete setup on this machine
  ws dotfile fix         Apply registered dotfile symlinks to this machine
```

**Output — already initialized (no changes needed):**

```text
ws init

Workspace ~/Workspace is already initialized. Nothing to do.

  ws trash status  Verify soft-delete setup on this machine
```

**Output — .megaignore conflict (exists but differs from template):**

```text
── [2/3] Ignore rules ─────────────────────────────

~/Workspace/.megaignore already exists (22 rules).
The built-in template has 50 rules.

[r] Replace with template (your custom rules will be lost)
[m] Merge — keep your rules, add missing template rules
[s] Skip — keep current file unchanged

: s

✔ .megaignore           kept as-is (22 rules)
```

**Output — dry-run:**

```text
ws init --dry-run

Would create:
  ~/Workspace/ws/
  ~/Workspace/ws/config.json       (default config)
  ~/Workspace/ws/manifest.json     (empty registry)
  ~/Workspace/.megaignore           (builtin template, 50 rules)

No changes made.
```

**Output — JSON:**

```json
{
  "ws_version": "0.1.0",
  "schema": 1,
  "command": "init",
  "data": {
    "workspace": "~/Workspace",
    "config": { "action": "created", "path": "~/Workspace/ws/config.json" },
    "manifest": { "action": "created", "path": "~/Workspace/ws/manifest.json" },
    "ignore": { "action": "created", "rules": 20, "path": "~/Workspace/.megaignore" },
    "trash_setup": {
      "root_dir": "~/.Trash",
      "shell_rm": "configured",
      "vscode_delete": "configured",
      "file_explorer_delete": "configured"
    }
  }
}
```

### `ws reset`

Reverse `ws init` — undo all external side-effects recorded in the provisioning ledger and remove the `ws/` directory. This is the nuclear teardown command: it removes symlinks, deletes provisioned files, strips config lines from shell rc files, and cleans up `.git/info/exclude` entries.

**Prerequisite:** The `ws/` directory must exist. If not, `ws reset` exits with an error.

**Provisioning ledger:** Every RW command that creates or modifies files outside `<workspace>/ws/` records an entry in `<workspace>/ws/provisions.json`. `ws reset` reads this ledger and undoes entries in reverse chronological order (LIFO).

```text
ws reset [flags]

--dry-run   Preview what would be undone without applying
```

**Provision entry types:**

| Type | Undo action |
| --- | --- |
| `file` | Delete the file |
| `dir` | Delete the directory (recursively) |
| `symlink` | Remove the symlink (does not delete the target) |
| `config_line` | Remove the exact line from the config file |
| `git_exclude` | Remove the line from `.git/info/exclude` |

**Safety:**
- Symlinks that have been overwritten by a real file are skipped (not deleted).
- Missing files/dirs are silently skipped.
- Each undo operation is idempotent.
- `--dry-run` previews all actions without writing.

**Output:**

```text
ws reset
══════════════════════════════════════════════════════
 WORKSPACE reset
══════════════════════════════════════════════════════

Provisions to undo: 5

  symlink    ~/.bashrc                  (remove symlink)
  symlink    ~/.ssh                     (remove symlink)
  config_line ~/.bashrc                 (remove line from .bashrc)
  config_line ~/.zshrc                  (remove line from .zshrc)
  file       ~/.local/bin/ws-trash-rm   (delete file)

This will also delete:
  ~/Workspace/ws/

reset workspace at ~/Workspace? [Y/n]: ↵

  ✔ ~/.local/bin/ws-trash-rm  (deleted)
  ✔ ~/.zshrc                  (line removed)
  ✔ ~/.bashrc                 (line removed)
  ✔ ~/.ssh                    (symlink removed)
  ✔ ~/.bashrc                 (symlink removed)

  ✔ ~/Workspace/ws/           (deleted)

──────────────────────────────────────────────────────
✔ Workspace reset.
```

**Contributor contract:** Every RW command that writes outside `<workspace>/ws/` must call `provision.Record()` after success. Commands that reverse their own work (e.g. `ws dotfile rm`) must call `provision.Remove()` to keep the ledger accurate.

---

### `ws trash`

Configure and validate machine-level soft-delete behavior.

```text
ws trash [subcommand]

ws trash setup [flags]
--root-dir <path>    Preferred trash root directory (default: ~/.Trash)
--no-shell-rm        Skip shell rm configuration
--no-vscode          Skip VS Code delete-to-trash configuration
--no-file-explorer   Skip file explorer delete configuration
--dry-run            Preview actions only

ws trash disable
ws trash status
```

`ws` delete paths continue to use `rm`; this command configures the environment so those deletes behave as soft-delete.

**Output — setup:**

```text
ws trash setup

Trash root: ~/.Trash

Shell rm integration:      ✔ configured
VS Code delete-to-trash:   ✔ configured
File explorer soft-delete: ✔ configured

Done. Soft-delete setup is active on this machine.
```

**Output — status (not configured):**

```text
ws trash status

Trash root: ~/.Trash

WARNING  shell-rm       not configured
WARNING  vscode-delete  not configured
WARNING  file-explorer  not configured

Run `ws trash setup` to configure soft-delete behavior.
```

**Output — status (configured, under threshold):**

```text
ws trash status

Trash root: ~/.Trash

  OK  shell-rm
  OK  vscode-delete
  OK  file-explorer

  Files            42
  Size             128 MB
  Threshold        1024 MB
```

**Output — status (configured, over threshold, exit 2):**

```text
ws trash status

Trash root: ~/.Trash

  OK  shell-rm
  OK  vscode-delete
  OK  file-explorer

  Files            3,291
  Size             2048 MB
  Threshold        1024 MB

▲ Trash size exceeds threshold (2048 MB > 1024 MB)
```

The threshold is configurable via `trash.warn_size_mb` in `config.json` (default: `1024` = 1 GB).

---

---

### Symlinks (Manual)

For general-purpose symlinks — linking workspace directories to external locations (e.g. `~/Repositories/bruno` → `data/bruno/`) — use `ln` directly. `ws` does not manage arbitrary symlinks. The `ws dotfile` command exists specifically for dotfile capture; everything else is a manual `ln -s`.

#### Quick reference

```bash
# Create a symlink (target must exist)
ln -s <target> <link-name>

# Example: link a repository into the workspace
ln -s ~/Workspace/Data/bruno ~/Repositories/bruno

# Example: link a workspace script to a PATH directory
ln -s ~/Workspace/Experiments/setup/install.sh ~/.local/bin/install.sh

# Example: link a workspace config where the OS expects it
sudo ln -s ~/Workspace/ws/dotfiles/daemon.json /etc/docker/daemon.json

# Verify a symlink (shows target)
readlink -f ~/.bashrc

# List symlinks in a directory
find ~/Workspace -maxdepth 2 -type l -ls

# Find all symlinks on the system pointing into the workspace
find / -type l -lname '*/Workspace/*' 2>/dev/null

# Remove a symlink (does NOT delete the target)
rm ~/.local/bin/install.sh

# Replace a symlink with a real copy of the file
cp --remove-destination "$(readlink -f ~/.bashrc)" ~/.bashrc
```

**Key rules:**

- `ln -s` takes `<target> <link-name>` — the link points *to* the target.
- Use absolute paths to avoid breakage when the working directory changes.
- `rm` on a symlink removes the pointer, never the target file.
- Use `sudo` for system paths like `/etc/`.
- Symlinks inside the workspace that point *outside* will appear as broken after a sync to a new machine. Keep originals inside the workspace, symlinks outside.

---

### `ws dotfile`

Solves: P2, P3

Captures system dotfiles into `<workspace>/ws/dotfiles/` and manages their symlinks. The original file is moved into the workspace under a non-user-controlled directory; the system path becomes a symlink pointing back. This ensures dotfiles are synced and survive a machine wipe.

The dotfile registry lives at `<workspace>/ws/manifest.json` (`dotfiles` array). All dotfile operations write to or read from this registry.

Optional: `ws dotfile` can use a Git repository for versioned backups of dotfiles and registry changes. This is disabled by default.

The local Git directory for dotfile backup is managed by `ws` under `<workspace>/ws/` and is not user-controlled.

Additional subcommands implemented by the CLI:

- `ws dotfile reset` — reset dotfile subsystem provisions.
- `ws dotfile git disconnect` — disconnect dotfile Git remote/config.

#### `ws dotfile add`

Capture a system file or directory into `ws/dotfiles/`. The file is moved in, and a symlink replaces it at the original location.

The storage name inside `ws/dotfiles/` is derived automatically from the system path: leading dots are stripped, path separators become flat names. For example `~/.ssh` → `ws/dotfiles/ssh/`, `~/.config/Code/User/settings.json` → `ws/dotfiles/vscode-settings.json`.

If the auto-derived name conflicts with an existing entry, the user is prompted for an alternative.

```text
ws dotfile add <system-path> [flags]

--sudo     Use sudo when creating the symlink at the system path.
--name     Override the auto-derived storage name inside ws/dotfiles/.
--dry-run  Preview all actions without applying.
```

When dotfile Git versioning is enabled, successful `add` operations auto-commit changes (and optionally auto-push).

**Interactive flow — simple dotfile:**

```text
ws dotfile add ~/.bashrc
──────────────────────────────────────────────────────
Capturing ~/.bashrc into workspace dotfile management.

Storage name: bashrc
System path:  ~/.bashrc  →  ~/Workspace/ws/dotfiles/bashrc

Plan:
  Move     ~/.bashrc  →  ~/Workspace/ws/dotfiles/bashrc
  Symlink  ~/.bashrc  →  ~/Workspace/ws/dotfiles/bashrc
  Register in manifest.json

Apply? [Y/n]: ↵

✔ Moved     ~/.bashrc  →  ~/Workspace/ws/dotfiles/bashrc
✔ Linked    ~/.bashrc  →  ~/Workspace/ws/dotfiles/bashrc
✔ Verified  readlink -f ~/.bashrc == ~/Workspace/ws/dotfiles/bashrc
✔ Registered in manifest.json
```

**Flow — directory (e.g. SSH):**

```text
ws dotfile add ~/.ssh
──────────────────────────────────────────────────────
Capturing ~/.ssh/ into workspace dotfile management.

Storage name: ssh/
System path:  ~/.ssh  →  ~/Workspace/ws/dotfiles/ssh/

Plan:
  Move     ~/.ssh/  →  ~/Workspace/ws/dotfiles/ssh/
  Symlink  ~/.ssh   →  ~/Workspace/ws/dotfiles/ssh/
  Register in manifest.json

Apply? [Y/n]: ↵

✔ Moved     ~/.ssh/  →  ~/Workspace/ws/dotfiles/ssh/
✔ Linked    ~/.ssh   →  ~/Workspace/ws/dotfiles/ssh/
✔ Verified  link resolves correctly
✔ Registered in manifest.json
```

**Flow — system path requiring sudo:**

```text
ws dotfile add /etc/docker/daemon.json --sudo
──────────────────────────────────────────────────────
Capturing /etc/docker/daemon.json into workspace dotfile management.

Storage name: daemon.json
System path:  /etc/docker/daemon.json  →  ~/Workspace/ws/dotfiles/daemon.json

Plan:
  Move     /etc/docker/daemon.json  →  ~/Workspace/ws/dotfiles/daemon.json  (sudo)
  Symlink  /etc/docker/daemon.json  →  ~/Workspace/ws/dotfiles/daemon.json  (sudo)
  Register in manifest.json

Apply? [Y/n]: ↵

✔ Moved     /etc/docker/daemon.json  →  ~/Workspace/ws/dotfiles/daemon.json  (sudo)
✔ Linked    /etc/docker/daemon.json  →  ~/Workspace/ws/dotfiles/daemon.json  (sudo)
✔ Verified  link resolves correctly
✔ Registered in manifest.json
```

**Flow — conflict (system file diverged from synced copy):**

This happens on a restored machine where `ws/dotfiles/daemon.json` was synced but the system also has a `/etc/docker/daemon.json` with different content.

```text
ws dotfile add /etc/docker/daemon.json --sudo
──────────────────────────────────────────────────────
CONFLICT  A file exists at both locations with different contents:

  System file:    /etc/docker/daemon.json       (modified 2026-03-14  2.1 KB)
  Workspace copy: ws/dotfiles/daemon.json       (modified 2026-01-12  1.8 KB)

[d] Diff both files
[o] Keep system    — overwrite workspace copy, then symlink
[w] Keep workspace — overwrite system file, then symlink
[b] Keep both      — backup system file to ws/dotfiles/daemon.json.bak, keep workspace, then symlink
[s] Skip

: d

--- ws/dotfiles/daemon.json     2026-01-12
+++ /etc/docker/daemon.json     2026-03-14
@@ -3,3 +3,5 @@
   "default-runtime": "nvidia",
+  "log-driver": "json-file",
+  "log-opts": { "max-size": "100m" },
+  "insecure-registries": ["registry.local:5000"]

[d] Diff  [o] Keep system  [w] Keep workspace  [b] Keep both  [s] Skip : o

✔ Copied    /etc/docker/daemon.json  →  ~/Workspace/ws/dotfiles/daemon.json
✔ Linked    /etc/docker/daemon.json  →  ~/Workspace/ws/dotfiles/daemon.json  (sudo)
✔ Verified  link resolves correctly
✔ Registered in manifest.json
```

#### `ws dotfile scan`

Verify every registered dotfile symlink is intact. Read-only — reports issues without making changes. Detects: `BROKEN`, `OVERWRITTEN`, and `OK` states. Results are sorted by severity.

See **Technical Design Decisions → Dotfile Severity Model** for the full severity table.

```text
ws dotfile scan
```

**Output:**

```text
ws dotfile scan — Dotfile Registry
Summary
──────────────────────────────────────────────────────
Registry     ~/Workspace/ws/manifest.json               5 dotfiles registered
Storage      ~/Workspace/ws/dotfiles/
Scanned      2026-03-29 09:14:22

BROKEN       1 critical                target missing
OVERWRITTEN  1 warning                 system file replaced symlink
OK           3                         no action needed
──────────────────────────────────────────────────────

Violations
──────────────────────────────────────────────────────
CRITICAL  BROKEN      ~/.config/Code/User/settings.json  →  ws/dotfiles/vscode-settings.json  [target missing]
WARNING   OVERWRITTEN /etc/docker/daemon.json                                                  [real file, not a symlink]
──────────────────────────────────────────────────────

Run `ws dotfile fix` to repair.
```

#### `ws dotfile ls`

Show all registered dotfiles from the `manifest.json` `dotfiles` registry. A quick view of what's managed.

```text
ws dotfile ls [flags]

--porcelain   Machine-readable output: tab-separated, no decorations
```

**Output:**

```text
ws dotfile ls — Registered Dotfiles
Registry: ~/Workspace/ws/manifest.json  (5 dotfiles)
Storage:  ~/Workspace/ws/dotfiles/
──────────────────────────────────────────────────────

  ~/.ssh                              →  ssh/                  SSH keys and proxy jump config
  ~/.bashrc                           →  bashrc
  /etc/docker/daemon.json             →  daemon.json           (sudo)
  ~/.kube/config                      →  kubeconfig
  ~/.config/Code/User/settings.json   →  vscode-settings.json

──────────────────────────────────────────────────────
5 dotfiles  (1 sudo)
```

**Output — porcelain:**

```text
ws dotfile ls --porcelain

~/.ssh	ssh	false	SSH keys and proxy jump config
~/.bashrc	bashrc	false	
/etc/docker/daemon.json	daemon.json	true	
~/.kube/config	kubeconfig	false	
~/.config/Code/User/settings.json	vscode-settings.json	false	
```

Columns: `system_path`, `dotfile_name`, `sudo`, `note` (tab-separated).

#### `ws dotfile fix`

Reconcile all registered dotfile symlinks to match the registry. Idempotent — safe on both new and existing machines. This is the primary restore command: after syncing the workspace to a fresh machine, `ws dotfile fix` recreates all symlinks.

```text
ws dotfile fix [flags]

--dry-run   Preview all actions without applying
--sudo      Allow dotfiles that require sudo without prompting per entry.
```

When dotfile Git versioning is enabled, successful `fix` operations commit any changed/repaired entries.

**Output:**

```text
ws dotfile fix — Dotfile Reconciliation
Registry: manifest.json  (5 dotfiles)
Storage:  ws/dotfiles/
──────────────────────────────────────────────────

Scanning...
  4 dotfiles to link
  1 dotfile to skip   (~/.config/Code/User/settings.json target missing in ws/dotfiles/)
  1 dotfile requires sudo

Apply? [Y/n]: ↵

[1/5]  ~/.ssh                →  ws/dotfiles/ssh/              ✔ created
[2/5]  ~/.bashrc             →  ws/dotfiles/bashrc             ✔ created
[3/5]  /etc/docker/...       →  ws/dotfiles/daemon.json        ✔ created  (sudo)
[4/5]  ~/.kube/config        →  ws/dotfiles/kubeconfig         ✔ created
[5/5]  ~/.config/Code/User/… →  ws/dotfiles/vscode-settings…   ⚠ skipped  [target missing]

──────────────────────────────────────────────────
Created: 4   Skipped: 1   Failed: 0
```

#### `ws dotfile git connect`

Configure remote backup for `ws`-managed dotfile Git versioning.

```text
ws dotfile git connect [flags]

--remote-url  Remote repository URL (required)
--username    Remote auth username (required)
--pass-entry  Optional pass entry path override (default: auto-derived)
--branch      Branch for push/status (default: main)
--auto-push   Enable push after each successful auto-commit (default: enabled)
--dry-run     Preview without writing config
```

Safety behavior:

- Local repo is always managed under `<workspace>/ws/` by `ws`.
- If the local repo exists, `ws` reuses it; if missing, `ws` initializes it automatically.
- User does not provide a local repo path.
- Secrets are never written to ws config; auth is prompted or read from `pass`.
- Auth resolution is automatic: git credential helper → `pass` (if available) → interactive prompt.
- If `pass` is used, `ws` reads `pass show <entry>` at runtime and uses the first line as the credential.
- With `--auto-push`, `ws` pushes whenever connectivity is available; offline periods only defer sync, never discard commits.
- **Private-repo enforcement:** Before saving config, `ws` verifies the remote repository is private via the hosting provider API. Public repos are rejected — no override exists. For unrecognized hosts (self-hosted), a warning is printed but the operation proceeds.

**Output — configure remote on existing ws-managed local repo:**

```text
ws dotfile git connect \
  --remote-url https://git.example.com/user/dotfiles-private.git \
  --username user

If required: Password: ********

Checking repository visibility…
✔ Repository is private

Detected ws-managed local git repository:
  Local:   ~/Workspace/ws/dotfiles-git
  Branch:  main
  Remote:  https://git.example.com/user/dotfiles-private.git

✔ Dotfile git remote configured
  auto_commit: enabled
  auto_push:   enabled

Saved to ws/config.json under dotfile.git.
```

**Output — rejected (public repo):**

```text
ws dotfile git connect \
  --remote-url https://github.com/user/my-dotfiles.git \
  --username user

If required: Password: ********

Checking repository visibility…
✘ Repository is PUBLIC

Dotfiles contain secrets (SSH keys, kubeconfigs, tokens).
Pushing to a public repository is an irreversible secret leak.
ws requires dotfile Git remotes to be private repositories.

Action: make the repository private, or create a new private repository.

Exit code: 1
```

**Output — warning (unrecognized host):**

```text
ws dotfile git connect \
  --remote-url https://git.internal.corp/user/dotfiles.git \
  --username user

If required: Password: ********

Checking repository visibility…
⚠ Unable to verify — unrecognized hosting provider
  ws cannot confirm that git.internal.corp repositories are private.
  Proceeding — ensure the repository is private before pushing.

✔ Dotfile git remote configured
```

**Output — first-time setup (local repo auto-created):**

```text
ws dotfile git connect --remote-url https://git.example.com/user/dotfiles-private.git --username user

If required: Password: ********

Checking repository visibility…
✔ Repository is private

No local dotfile git repo found.
✔ Initialized ws-managed local repository at ~/Workspace/ws/dotfiles-git
✔ Dotfile git remote configured
```

#### `ws dotfile git status`

Show Git backup status for dotfiles.

```text
ws dotfile git status
```

**Output:**

```text
ws dotfile git status

Git versioning: enabled
Local repo:     ~/Workspace/ws/dotfiles-git
Remote URL:     https://git.example.com/user/dotfiles-private.git
Username:       user
Branch:         main
Auto-commit:    enabled
Auto-push:      enabled

Working tree:   clean
Ahead/behind:   ↑2 ↓0
Last commit:    2026-04-03 11:20  ws(dotfile): capture ~/.bashrc
Pending sync:   0 commits queued (network: reachable)
```

#### `ws dotfile rm`

Unregister a dotfile. Copies the file from `ws/dotfiles/` back to the system path (replacing the symlink with a real file), then unregisters it. The file is removed from `ws/dotfiles/`.

Accepts either the system path or the dotfile name.

```text
ws dotfile rm <path> [flags]

--dry-run  Preview actions without applying
```

When dotfile Git versioning is enabled, successful `rm` operations auto-commit registry/content changes (and optionally auto-push).

**Output:**

```text
ws dotfile rm ~/.bashrc

Registered dotfile:
  system    ~/.bashrc
  stored    ~/Workspace/ws/dotfiles/bashrc

Plan:
  Copy       ~/Workspace/ws/dotfiles/bashrc  →  ~/.bashrc
  Delete     ~/Workspace/ws/dotfiles/bashrc
  Unregister from manifest.json

Apply? [Y/n]: ↵

✔ Copied    ~/Workspace/ws/dotfiles/bashrc  →  ~/.bashrc  (real file, not a symlink)
✔ Deleted   ~/Workspace/ws/dotfiles/bashrc
✔ Unregistered from manifest.json
```

---

### `ws scratch`

Create and manage throwaway working directories outside the workspace for debug sessions, investigations, and ad-hoc tasks. Scratch directories live under `scratch.root_dir` (default `~/Scratch`) — never inside `<workspace>`, never synced. See **Technical Design Decisions → Scratch Directory Management** for the design rationale.

`ws scratch` is a standalone utility.

Additional subcommands implemented by the CLI:

- `ws scratch rm [name]` — delete one scratch directory by name. Shows ghost panel if name is omitted.
- `ws scratch open [name]` — open an existing scratch directory in the configured editor. Shows ghost panel with Tab completion if name is omitted.
- `ws scratch tag [name]` — add tags to a scratch directory. Multi-tag ghost input with workspace tag suggestions.
- `ws scratch search [query]` — search scratch directories by tag/name/content. Interactive ghost mode if no query.

```text
ws scratch [subcommand]
```

#### `ws scratch new`

Create a named scratch directory and open it in the configured editor. When invoked without a name argument, shows an interactive ghost panel listing existing directories for context.

```text
ws scratch new [name] [flags]

--no-open         Create the directory but don't launch the editor
--editor <cmd>    Override scratch.editor_cmd for this invocation
--no-date         Skip the date suffix even if name_suffix is "auto"
--dry-run         Show what would be created, don't do it
```

**Output (interactive — no name argument):**

```text
ws scratch new
Name: █
  ▒CA-remove-CPU-limits.2026-04▒
  ▒CA-registry-migration.2026-04▒
  ▒ca-llm-pending▒
  ▒G9-reboot.2026-04▒
  ▒whitelist-conflict.2026-04▒

Name: CA-█
  ▒CA-remove-CPU-limits.2026-04▒
  ▒CA-registry-migration.2026-04▒

Name: CA-debug█
  ▒CA-remove-CPU-limits.2026-04▒
  ▒CA-registry-migration.2026-04▒

✔ Created   ~/Scratch/CA-debug.2026-04/
✔ Opening   VS Code → ~/Scratch/CA-debug.2026-04/
```

The ghost panel is display-only for `new` — Tab does not complete (you are picking a name distinct from those listed). Matching strips date suffixes: typing `CA` matches `CA-remove-CPU-limits.2026-04`. Bottom-of-terminal: `ws` reserves panel space before entering raw mode (emits blank lines to scroll the terminal up if needed), so the panel always fits regardless of cursor position.

**Output (inline name — skip prompt):**

```text
ws scratch new proxy-auth-header

✔ Created   ~/Scratch/proxy-auth-header.2026-04/
✔ Opening   VS Code → ~/Scratch/proxy-auth-header.2026-04/
```

**Edge cases:**

| Situation | Behavior |
| --- | --- |
| Name already exists (exact match with date suffix) | Appends `-2`, `-3`, etc. Prints a notice. |
| Empty input (user hits Enter with no name) | Generates `scratch-<short-hash>.YYYY-MM` and proceeds. |
| `scratch.root_dir` doesn't exist | Creates it automatically (`mkdir -p`). |
| `editor_cmd` not found | Prints path, skips open, exits 0 with warning. |
| `--no-open` flag | Creates dir, prints path, exits. Good for scripting. |
| Non-TTY stdin (pipe, `--json`, test) | Ghost panel skipped; reads name from stdin line. |
| Ctrl+C during prompt | Exits cleanly (code 130), no directory created. |

#### `ws scratch ls`

List all directories under `scratch.root_dir`. Non-interactive, pipe-safe.

```text
ws scratch ls [flags]

--sort <key>      Sort by: age (default), size, name
--json            Machine-readable output
```

**Output:**

```text
ws scratch ls

NAME                              AGE     SIZE    ITEMS
proxy-auth-header.2026-04         2h      —       0 files
proxy-timeout.2026-03             8d      312 MB  14 files
dns-resolution.2026-03            12d     48 MB   6 files
gpu-driver-debug.2026-02          34d     1.2 GB  23 files
                                          ──────
                                 4 dirs   1.5 GB
```

#### `ws scratch open`

Open an existing scratch directory in the configured editor. When no name is given, shows a live ghost panel with Tab completion (Tab fills the first match; Enter confirms). Resolves names with substring matching (case-insensitive).

```text
ws scratch open [name] [flags]

--editor <cmd>    Override scratch.editor_cmd for this invocation
```

**Output (interactive — no name argument):**

```text
ws scratch open
Open: █
  ░CA-remove-CPU-limits.2026-04░
  ░CA-registry-migration.2026-04░
  ░ca-llm-pending░
  ░G9-reboot.2026-04░
  ░whitelist-conflict.2026-04░

Open: CA-█     ← Tab → completes to first match
  ░CA-remove-CPU-limits.2026-04░
  ░CA-registry-migration.2026-04░

Open: CA-remove-CPU-limits.2026-04█

✔ Opening   code → ~/Scratch/CA-remove-CPU-limits.2026-04/
```

**Output (inline name — skip prompt):**

```text
ws scratch open proxy-auth-header

✔ Opening   code → ~/Scratch/proxy-auth-header.2026-04/
```

**Edge cases:**

| Situation | Behavior |
| --- | --- |
| Name not found | Prints error, exits 1. |
| `editor_cmd` not found | Prints path, skips open, exits 0 with warning. |
| Non-TTY stdin (pipe, `--json`, test) | Ghost panel skipped; reads name from stdin line. |
| `--json` | Emits `{"name": "...", "path": "..."}`, skips editor launch. |
| Ctrl+C during prompt | Exits cleanly (code 130). |

#### `ws scratch rm`

Delete a scratch directory by name. When no name is given, shows a live ghost panel with Tab completion (Tab fills the first match; Enter confirms).

```text
ws scratch rm [name] [flags]

--dry-run         Preview without deleting
```

**Output (interactive — no name argument):**

```text
ws scratch rm
Delete: █
  ░CA-remove-CPU-limits.2026-04░
  ░CA-registry-migration.2026-04░
  ░ca-llm-pending░
  ░G9-reboot.2026-04░
  ░whitelist-conflict.2026-04░

Delete: CA-█     ← Tab → completes to first match
  ░CA-remove-CPU-limits.2026-04░
  ░CA-registry-migration.2026-04░

Delete: CA-remove-CPU-limits.2026-04█

Delete scratch "CA-remove-CPU-limits.2026-04"? [Y/n/a/q]
✔ Deleted  ~/Scratch/CA-remove-CPU-limits.2026-04/
```

#### `ws scratch prune`

Remove old scratch directories interactively.

```text
ws scratch prune [flags]

--older-than <duration>   Only prune dirs older than this (e.g. 30d, 90d)
--all                     Prune all scratch directories
--name <pattern>          Prune dirs matching this name pattern
--dry-run                 Preview actions without applying
```

**Output:**

```text
ws scratch prune --older-than 90d

Will remove:
  cert-rotation.2026-01          94d   180 MB   8 files

Confirm? [y/N]: y
✔ Removed 180 MB from ~/Scratch/
```

Deletion goes through `rm` (soft-delete if `ws trash setup` was run — Philosophy Factor IX).

#### `ws scratch tag`

Add tags to a scratch directory. When no name is given, shows a ghost panel to pick a scratch dir (Tab-completable). Then enters a multi-tag ghost input: type a tag, Enter commits it and loops, empty Enter finishes. Ghost panel suggests popular tags from `<workspace>/ws/tags.json`, filtered as you type. New tags are automatically added to the workspace tag collection.

```text
ws scratch tag [name] [flags]

--auto            Suggest tags from file/content heuristics (plan/confirm per tag)
```

**Output (interactive):**

```text
ws scratch tag
Scratch: pid-limit█     ← Tab completes
Tag: [k8s] █
  ░cgroups░
  ░docker░
  ░networking░
  ░systemd░

Tag: [k8s] cgroups█

Tag: [k8s, cgroups] █   ← empty Enter finishes

Tagged pid-limit-debug.2026-04: [k8s, cgroups]
```

**Output (auto-tag):**

```text
ws scratch tag pid-limit-debug --auto

Add tag "bash" to pid-limit-debug.2026-04? [y/n/a/q] y
Add tag "k8s" to pid-limit-debug.2026-04? [y/n/a/q] y
Add tag "cgroups" to pid-limit-debug.2026-04? [y/n/a/q] y

Tagged pid-limit-debug.2026-04: [bash, k8s, cgroups]
```

Auto-tag heuristics:
- File extensions: `.sh` → `bash`, `.py` → `python`, `.go` → `go`, `.tf` → `terraform`
- Named files: `Dockerfile` → `docker`, `Makefile` → `make`
- Shebangs: `#!/bin/bash` → `bash`, `#!/usr/bin/env python` → `python`
- Content patterns: `kubectl` → `k8s`, `systemctl` → `systemd`, `cgroup` → `cgroups`, `docker` → `docker`, `iptables` → `networking`

#### `ws scratch search`

Search scratch directories by tag, name, or file content. Multi-token queries use AND logic. Results ranked: tag match (3) > name match (2) > content match (1). Without arguments, enters interactive ghost mode showing all scratch dirs with tags, filterable as you type.

```text
ws scratch search [query] [flags]

--max <n>         Limit results (0=unlimited)
--json            Machine-readable output
```

**Output (CLI):**

```text
ws scratch search "k8s pid"

pid-limit-debug.2026-04            match=tag     [k8s, pid-limit, cgroups]
```

**Output (interactive — no query):**

```text
ws scratch search
Search: k8s█
  ░pid-limit-debug.2026-04 [k8s, pid-limit, cgroups]░
  ░ca-registry.2026-04 [k8s, docker]░
```

---

### `ws log`

Solves: P5

A session recorder. Uses PTY recording by default via `script(1)`, capturing both stdin and stdout. Recording sessions show a `● ws:log` prompt indicator. Logs are stored in `<workspace>/ws/ws-log`.

Additional subcommand implemented by the CLI:

- `ws log rm <tag>` — remove one recorded session by tag.

See **Technical Design Decisions → Session Recording: PTY Mode (Default)** for architecture and trade-offs.

```text
ws log [subcommand]
```

#### `ws log start`

Begin a recorded session in PTY mode. Spawns a child shell via `script(1)`. Logs are written under `<workspace>/ws/ws-log`.

```text
ws log start [flags]

--tag             Human-readable label (default: auto timestamp YYYY-MM-DD-HHMM)
--quiet-start     Non-interactive start using default values
--no-prompt       Do not modify the shell prompt (disable the ● ws:log indicator)
```

**Output — default (PTY mode):**

```text
ws log start
Tag [2026-03-29-1422]: ↵

✔ Recording (PTY mode)
  Session: 2026-03-29-1422
  Stdin:   ~/Workspace/ws/ws-log/2026-03-29-1422/stdin.log
  Stdout:  ~/Workspace/ws/ws-log/2026-03-29-1422/stdout.log
  Exit:    type 'exit' or Ctrl-D to end

● ws:log user@laptop ~/Workspace $
● ws:log user@laptop ~/Workspace $ ls -la configs/
● ws:log user@laptop ~/Workspace $ git status
● ws:log user@laptop ~/Workspace $ exit

✔ Session ended
  Tag:      2026-03-29-1422
  Duration: 14m 22s
  Stdin:    ~/Workspace/ws/ws-log/2026-03-29-1422/stdin.log  (4 KB)
  Stdout:   ~/Workspace/ws/ws-log/2026-03-29-1422/stdout.log (19 KB)
```

**Output — quiet start:**

```text
ws log start --quiet-start
● ws:log user@laptop ~/Workspace $
```

No banner, no prompts. The `● ws:log` prefix is the only visual change.

**Output on exit:**

```text
ws log — Session ended
Tag:      prod-migration
Duration: 9m 14s
Stdin:    ~/Workspace/ws/ws-log/prod-migration/stdin.log   (18 KB  | 47 commands)
Index:    updated at ~/Workspace/ws/ws-log-index.md
```

The `● ws:log` prompt disappears automatically on session end. See **Technical Design Decisions → Prompt Indicator** for cleanup mechanics.

#### `ws log stop`

Stop the current recording session early (if supported by the active session process tracking). PTY sessions normally end by exiting the child shell (`exit` or Ctrl-D).

```text
ws log stop
```

**Output:**

```text
ws log stop

✔ Session ended
  Tag:      2026-03-29-1422
  Duration: 14m 22s
  Stdin:    ~/Workspace/ws/ws-log/2026-03-29-1422/stdin.log   (4 KB)
  Stdout:   ~/Workspace/ws/ws-log/2026-03-29-1422/stdout.log  (19 KB)
  Index:    updated at ~/Workspace/ws/ws-log-index.md

user@laptop ~/Workspace $
```

If no session is active, prints:

```text
ws log stop

No active recording session.
```

#### `ws log ls`

List all recorded sessions with storage summary. Shows each session's tag, command count, duration, and size. Storage totals are shown at the bottom.

```text
ws log ls [flags]

--since     Only show sessions from this date onward (YYYY-MM-DD)
--mini      Print a compact one-line summary (no individual sessions)
```

**Output:**

```text
ws log ls

Sessions: 48   Storage: 312 MB / 500 MB cap   Oldest: 2026-01-04
Location: ~/Workspace/ws/ws-log/
──────────────────────────────────────────────────────────────────

● 2026-03-29-1422                   47 cmds   9m       18 KB
  2026-03-28-0900   prod-migration   52 cmds   14m      24 KB
  2026-03-27-1100   ssl-debug        12 cmds   3m        4 KB
  2026-03-25-0830                    38 cmds   22m      12 KB
  2026-03-20-1400   sys-bench        91 cmds   45m      48 KB
  ...
  2026-01-04-0900                     8 cmds   2m        2 KB

──────────────────────────────────────────────────────────────────
48 sessions  |  312 MB total
Synced index: ~/Workspace/ws/ws-log-index.md  (12 KB)
```

The `●` marker indicates the currently active session.

#### `ws log prune`

Prune old sessions from `<workspace>/ws/ws-log`.

```text
ws log prune [--older-than <duration>] [--all]
```

**Output:**

```text
ws log prune --older-than 30d

Will remove 8 sessions (2026-01-04 to 2026-02-27):
  89 MB  2026-01-04-0900/
  44 MB  2026-01-15-1133/
  ...

Confirm? [y/N]: y
Removed: 133 MB from ~/Workspace/ws/ws-log/
```

---

### `ws search`

Solves: P6

Textual and contextual search across the entire workspace. Covers text files via indexed grep, filenames, and tags. PDF/binary files are indexed by filename and directory only.

```text
ws search <query> [flags]

--type     Filter by file type: note, script, config, pdf, image, all  (default: all)
--path     Restrict search to a subpath
--context  Show N lines of context around matches  (default: 2)
```

**Output:**

```text
ws search "rsync"
──────────────────────────────────────────────────────
[script]   experiments/mega-cron.sh:4
           # rsync is not used here but see debug-proxy.sh

[script]   experiments/debug-proxy.sh:22
           rsync -avz --delete --progress ~/Workspace/ remote-backup:/data/

[note]     notes/second-brain/2026-02-infra.md:34
           mega-sync is preferred over rsync for MEGA. rsync used for remote backup.

[log]      ws/ws-log/prod-migration/stdin.log:3
           rsync -avz --delete --exclude='.git' ~/Workspace/ remote:/backup/
──────────────────────────────────────────────────────
4 results across 4 files
```

---

### `ws repo`

Stateful Git fleet operations for repositories inside `<workspace>`. `ws repo` keeps a workspace-local repo state and reconciles it against live directories before stateful operations.

```text
ws repo [subcommand]
```

Common selection flags:

```text
--path          Restrict discovery to a workspace subpath
--dirty         Only repos with uncommitted changes
--ahead         Only repos ahead of upstream
--behind        Only repos behind upstream
--detached      Only repos in detached HEAD
--max-parallel  Max concurrent repo workers (default: repo.max_parallel)
```

Discovery behavior:

- roots from `repo.roots` (default: `.`)
- excludes from `repo.exclude_dirs`
- only repos under `<workspace>` are included

Reconcile behavior (default for stateful repo commands):

- load cached state from `ws/repo.state` and tracked refs from `manifest.json`
- scan live repos in current workspace scope
- auto-heal moved paths when repository identity matches
- mark missing repos as stale instead of failing the whole command
- append newly discovered repos to state when they are in scope

Stateful commands: `ws repo scan`, `ws repo fetch`, `ws repo pull`, `ws repo sync`, `ws repo run`.
These commands always attempt reconcile-first behavior unless explicitly disabled by config.
If state is missing or unreadable, commands fall back to live discovery for the current scope.

#### `ws repo ls`

Discover and list Git repos under the workspace.

```text
ws repo ls [flags]

--porcelain   tab-separated output: path, branch, dirty, ahead, behind
```

**Output:**

```text
ws repo ls

experiments/blog                    branch=main      dirty=no   ahead=0 behind=0
notes/second-brain                 branch=main      dirty=yes  ahead=2 behind=0
data/bruno                         branch=master    dirty=no   ahead=0 behind=1

3 repos discovered
```

#### `ws repo scan`

Fleet status view for discovered repos.

Before calculating status, `ws` fetches each repo (`git fetch --all --prune`) to ensure ahead/behind counts are current, then reconciles repo state from the current directory scope whenever possible. Use `--no-fetch` to skip the fetch phase for offline or faster scans. Fetch failures for individual repos are reported as warnings, not errors.

```text
ws repo scan [flags]

--no-fetch   Skip the automatic fetch before scanning (default: fetch enabled)
```

**Output:**

```text
ws repo scan
Summary
──────────────────────────────────────────────────────
Repos        3 discovered
Dirty        1
Ahead        1
Behind       1
Detached     0
──────────────────────────────────────────────────────

Details
──────────────────────────────────────────────────────
notes/second-brain   main    dirty   ↑2 ↓0   stash=1   last=2h
data/bruno           master  clean   ↑0 ↓1   stash=0   last=3d
experiments/blog     main    clean   ↑0 ↓0   stash=0   last=5h
──────────────────────────────────────────────────────
```

Exit behavior:

- `0` clean fleet
- `2` one or more repos need attention (`dirty`, `ahead`, `behind`, or `detached`)

#### `ws repo fetch`

Run `git fetch --all --prune` across selected repos.

Reconcile runs first, then fetch executes on the reconciled live set.

```text
ws repo fetch [flags]
```

**Output:**

```text
ws repo fetch

[1/3] notes/second-brain    ✔ fetched
[2/3] data/bruno            ✔ fetched
[3/3] experiments/blog      ✔ fetched

Fetched: 3   Failed: 0
```

#### `ws repo pull`

Interactive fleet pull. Default strategy is `git pull --ff-only`.

Reconcile runs first, so moved/newly-discovered repos in scope are included when possible.

```text
ws repo pull [flags]

--rebase     use `git pull --rebase` instead of `--ff-only`
--dry-run    preview planned pulls
```

**Output:**

```text
ws repo pull

Behind repos:
  data/bruno (master, behind 1)

Apply `git pull --ff-only` to 1 repo? [Y/n]: ↵

[1/1] data/bruno   ✔ updated (fast-forward)

Updated: 1   Skipped: 0   Failed: 0
```

#### `ws repo sync`

Interactive fleet sync. Inspects each repo's state and proposes the minimal action to bring it in sync with its upstream. One action per repo.

Reconcile runs first to avoid acting on stale paths. Fetches all repos before scanning to ensure accurate ahead/behind counts.

Per-repo state → action mapping:

| State | Action | Git operations |
| --- | --- | --- |
| Behind only | Pull (ff-only) | `git pull --ff-only` |
| Ahead only | Push | `git push` |
| Diverged | Pull + push | `git pull --no-rebase` (or `--rebase` with flag) → `git push` |
| Dirty + needs pull | Stash, pull, unstash | `git stash push` → pull → `git stash pop` |
| Up to date | *(skipped)* | — |
| Detached HEAD | *(skipped with warning)* | — |
| No upstream | *(skipped with warning)* | — |

Sync never commits. It only syncs already-committed work with the remote.

If merge/rebase conflicts occur, the operation is aborted cleanly (`git merge --abort` / `git rebase --abort`) and the repo is reported as failed. The repo is never left in a mid-merge/mid-rebase state.

If `git stash pop` encounters conflicts after a successful pull, the stash is preserved on the stack and a warning is emitted.

```text
ws repo sync [flags]

--rebase     use rebase for diverged repos (default: merge)
--dry-run    preview planned sync actions
```

**Output:**

```text
ws repo sync

  Pull       data/bruno                  (1 behind, ff)                [y/n/a/q] y
  ✔ Pulled (fast-forward)

  Push       notes/second-brain          (2 ahead)                     [y/n/a/q] y
  ✔ Pushed

  Pull+Push  experiments/blog            (1 ahead, 2 behind, merge)   [y/n/a/q] y
  ✔ Merged + pushed

  ▲ Skipped: tmp-branch (detached HEAD)

  3 synced · 1 skipped
```

#### `ws repo run`

Run an arbitrary command in each selected repo root.

Reconcile runs first so command fan-out targets current repo locations whenever possible.

```text
ws repo run -- <command...>

--dry-run    preview target repos and command only
```

**Output:**

```text
ws repo run -- git gc --auto

Command: git gc --auto
Targets: 3 repos

Run now? [Y/n]: ↵

[1/3] experiments/blog      ✔ exit 0
[2/3] notes/second-brain   ✔ exit 0
[3/3] data/bruno           ✔ exit 0

Succeeded: 3   Failed: 0
```

**Output:**

```text
ws repo run -- git gc --auto

Command: git gc --auto
Targets: 3 repos

Run now? [Y/n]: ↵

[1/3] experiments/blog      ✔ exit 0
[2/3] notes/second-brain   ✔ exit 0
[3/3] data/bruno           ✔ exit 0

Succeeded: 3   Failed: 0
```

---

### `ws context`

Create task-scoped context directories alongside a project. Designed for agent-assisted development: drop design notes, constraints, and resources next to the code so AI agents discover them naturally without path redirection.

The context sidecar lives at `.ws-context/` in the project root. Each task or feature gets its own subdirectory inside it. The parent `.ws-context/` is excluded from git via `.git/info/exclude` (local-only, never committed, zero trace in the repo). If the project is not a git repository, the exclude step is silently skipped; if the project becomes a git repo later, the next `ws context create` retroactively adds the exclude rule.

See **Technical Design Decisions → Context Sidecar: Local Git Exclusion** for rationale.

```text
ws context [subcommand]
```

#### `ws context create`

Create a new task context directory under `.ws-context/`. The task name is required and becomes the subdirectory name.

```text
ws context create <task> [flags]

--path       Project root to create context in (default: current directory)
--dry-run    Preview actions without applying
```

**Behavior:**

| Step | Action | Condition |
| --- | --- | --- |
| 1. Resolve project root | Use `--path` if given, else CWD | Always |
| 2. Create `.ws-context/<task>/` | `mkdir -p` | Directory does not exist |
| 3. Git exclude | Append `.ws-context/` to `.git/info/exclude` | CWD is inside a git repo AND entry not already present |

Step 3 uses `git rev-parse --show-toplevel` to locate the repo root and its `.git/info/exclude` file. If the command fails (not a git repo), step 3 is silently skipped.

**Output — first task in a git repo:**

```text
ws context create auth-redesign
──────────────────────────────────────────────────────
Project: ~/Repositories/my-service
Task:    auth-redesign

  Create  .ws-context/auth-redesign/
  Exclude .ws-context/ via .git/info/exclude

Apply? [Y/n]: ↵

✔ Created  .ws-context/auth-redesign/
✔ Updated  .git/info/exclude
```

**Output — second task (exclude already present):**

```text
ws context create rate-limiter
──────────────────────────────────────────────────────
Project: ~/Repositories/my-service
Task:    rate-limiter

  Create  .ws-context/rate-limiter/

Apply? [Y/n]: ↵

✔ Created  .ws-context/rate-limiter/
✔ .git/info/exclude already has .ws-context/ — skipped
```

**Output — task directory already exists:**

```text
ws context create auth-redesign

.ws-context/auth-redesign/ already exists. Nothing to create.
✔ .git/info/exclude already has .ws-context/ — skipped
```

When the task directory already exists but the project was not previously a git repo and now is, the exclude step runs:

```text
ws context create auth-redesign

.ws-context/auth-redesign/ already exists. Nothing to create.
✔ Updated  .git/info/exclude
```

**Output — not a git repo:**

```text
ws context create dns-investigation
──────────────────────────────────────────────────────
Project: ~/Workspace/Experiments/dns-debug
Task:    dns-investigation

  Create  .ws-context/dns-investigation/

Apply? [Y/n]: ↵

✔ Created  .ws-context/dns-investigation/
  (not a git repo — skipped exclude step)
```

**Output — dry-run:**

```text
ws context create auth-redesign --dry-run

Would create:
  ~/Repositories/my-service/.ws-context/auth-redesign/
Would append to .git/info/exclude:
  .ws-context/

No changes made.
```

**Output — JSON:**

```json
{
  "ws_version": "0.1.0",
  "schema": 1,
  "command": "context.create",
  "data": {
    "project": "~/Repositories/my-service",
    "task": "auth-redesign",
    "context_dir": ".ws-context/auth-redesign",
    "created": true,
    "git_repo": true,
    "git_exclude_updated": true
  }
}
```

**Resulting structure:**

```text
my-service/
├── .git/
│   └── info/
│       └── exclude          ← ".ws-context/" appended
├── .ws-context/
│   ├── auth-redesign/       ← task 1
│   └── rate-limiter/        ← task 2
├── src/
├── go.mod
└── README.md
```

#### `ws context rm`

Remove context sidecars. Use `--all` to remove all tracked context sidecars and reset context subsystem state.

```text
ws context rm [<task>] [flags]

--all        Remove all contexts
--path       Project root (default: current directory)
--dry-run    Preview actions without applying
```

---

### `ws ignore`

Solves: P7

Generate, validate, and audit the `.megaignore` file. Produces a battle-proof ignore file and lets you check whether a given file would be synced.

**Megaignore guard:** All `ws ignore` subcommands except `ws ignore generate` require `<workspace>/.megaignore` to exist. If missing, the command prints an error and exits:

```text
Error: .megaignore not found at ~/Workspace/.megaignore
Run `ws ignore generate` to create one from the built-in template.
```

```text
ws ignore [subcommand]
```

#### `ws ignore check`

Test whether a file path would be synced or ignored.

```text
ws ignore check <path>
```

**Output:**

```text
ws ignore check experiments/debug-proxy.sh

✔ SYNCED      experiments/debug-proxy.sh
  Reason: no matching exclude rule
  File size: 4 KB (below thresholds)
```

```text
ws ignore check artifacts/datasets/node-metrics.csv

✗ IGNORED     artifacts/datasets/node-metrics.csv
  Reason: rule `-g:*.csv` on line 31 of .megaignore
```

```text
ws ignore check archive/incident-2026.tar.gz

✔ SYNCED      archive/incident-2026.tar.gz
  Reason: safe harbor — Archive/ overrides `-g:*.tar.gz` (line 44)
  File size: 230 MB (above crit_size threshold — bloat warning will fire)
```

#### `ws ignore ls`

List all files currently excluded by `.megaignore` rules. Flat, one-path-per-line output — pipe-safe, greppable. When stdout is a TTY and output exceeds the terminal height, results are paginated via `less`. Each line shows the matched rule so you can trace *why* a file is excluded.

```text
ws ignore ls [flags]

--path     Restrict to a subpath
--rule     Filter to files matched by a specific rule (e.g. "-g:*.csv")
```

**Output:**

```text
ws ignore ls

IGNORED  114 MB   artifacts/datasets/node-metrics-2026.csv        -g:*.csv
IGNORED   44 MB   artifacts/datasets/city_temperature.csv         -g:*.csv
IGNORED    4 MB   artifacts/datasets/house_prices.csv             -g:*.csv
IGNORED    8 MB   artifacts/datasets/car_prices.orc                        -g:*.orc
IGNORED   10 MB   artifacts/datasets/weather.orc                           -g:*.orc
IGNORED  230 MB   experiments/proxy-debug/capture.tar.gz               -g:*.tar.gz
IGNORED   42 MB   notes/second-brain/.git/                             -:.*
IGNORED  180 MB   experiments/pip-madness/.venv/                       -:.*

38 files · 812 MB excluded
```

**Filtered by rule:**

```text
ws ignore ls --rule "-g:*.csv"

IGNORED  114 MB   artifacts/datasets/node-metrics-2026.csv        -g:*.csv
IGNORED   44 MB   artifacts/datasets/city_temperature.csv         -g:*.csv
IGNORED    4 MB   artifacts/datasets/house_prices.csv             -g:*.csv

3 files · 162 MB excluded
```

**Pipe usage:**

```bash
ws ignore ls | grep \.csv         # find excluded CSVs
ws ignore ls --json | jq ...      # machine-readable
```

#### `ws ignore tree`

Browse the workspace as a directory tree with a sync/ignored column on every entry. Directories where all children are excluded collapse to a single line.

```text
ws ignore tree [flags]

--path     Start from a subpath instead of workspace root
--depth    Limit tree depth (default: 1)
```

**Output (default depth=1):**

```text
ws ignore tree

~/Workspace/
├── ✔ configs/                      18 KB
├── ✔ ws-tool/                       4 KB
├── ◐ artifacts/                   682 MB   (5 files excluded)
├── ◐ experiments/                 410 MB   (2 files excluded)
├── ◐ notes/                        42 MB   (1 file excluded)
├── ✔ README.md                      2 KB
└── ✔ PHILOSOPHY.md                  1 KB

38 files excluded · 812 MB not synced
```

Legend: ✔ fully synced · ✗ excluded · ◐ partially excluded (has excluded children).

**Deeper traversal:**

```text
ws ignore tree --depth 3

~/Workspace/
├── artifacts/
│   └── datasets/
│       ├── ✗ node-metrics-2026.csv    114 MB   -g:*.csv
│       ├── ✗ city_temperature.csv      44 MB   -g:*.csv
│       ├── ✗ house_prices.csv           4 MB   -g:*.csv
│       ├── ✗ car_prices.orc               8 MB   -g:*.orc
│       ├── ✗ weather.orc                 10 MB   -g:*.orc
│       └── ✔ AgentCrewPrompt.txt          2 KB
├── configs/
│   ├── ✔ bashrc                       4 KB
│   ├── ✔ daemon.json                  2 KB
│   └── ✔ ssh/                        12 KB
├── experiments/
│   ├── proxy-debug/
│   │   └── ✗ capture.tar.gz           230 MB   -g:*.tar.gz
│   └── pip-madness/
│       └── ✗ .venv/                   180 MB   -:.*
└── notes/
    └── second-brain/
        └── ✗ .git/                     42 MB   -:.*

38 files excluded · 812 MB not synced
```

**Scoped to a subpath:**

```text
ws ignore tree --path artifacts/datasets

artifacts/datasets/
├── ✗ node-metrics-2026.csv    114 MB   -g:*.csv
├── ✗ city_temperature.csv      44 MB   -g:*.csv
├── ✗ house_prices.csv           4 MB   -g:*.csv
├── ✗ car_prices.orc               8 MB   -g:*.orc
├── ✗ weather.orc                 10 MB   -g:*.orc
└── ✔ AgentCrewPrompt.txt          2 KB

5 files excluded · 180 MB not synced
```

#### `ws ignore scan`

Scan the workspace and report all sync-hygiene violations: oversized files (bloat), excessive directory depth, and project-meta artifacts (build outputs detected via project-marker heuristics). This is the single place where file size thresholds, depth limits, and project-type artifact detection are checked.

Violations inside safe harbor directories are automatically downgraded to INFO severity. They don't count toward the violation total, don't trigger exit code 2, and are collapsed to a single summary line by default. Pass `--expand-harbors` to list each safe harbor item individually.

```text
ws ignore scan [flags]

--warn-size        File size threshold for warnings   (default: 1MB)
--crit-size        File size threshold for critical   (default: 10MB)
--depth            Max directory depth before warning (default: 6)
--no-meta          Skip project-meta artifact detection
--expand-harbors   Show safe harbor items individually instead of collapsed summary
```

**Output:**

```text
ws ignore scan — Sync Hygiene
Summary
──────────────────────────────────────────────────────
Rules loaded   50
Bloat          2 critical · 1 warning   oversized files
Depth          0 critical · 1 warning   excessive nesting
Project-meta   0 critical · 2 warning   build output dirs
Scanned        2026-03-29 09:14:22     profile: ignore
──────────────────────────────────────────────────────

Violations (6)
──────────────────────────────────────────────────────
CRITICAL  bloat           114 MB   artifacts/datasets/node-metrics-2026.csv
CRITICAL  bloat            48 MB   experiments/proxy-debug/tcpdump-output.pcap
WARNING   bloat            22 MB   artifacts/presentations/quarterly-review.pptx
WARNING   depth             8 lvl  experiments/k8s/cluster/namespaces/logs/app/debug.txt
WARNING   project-meta     24 KB   experiments/projects/java-demo/bin/  (marker: *.java)
WARNING   project-meta      4 KB   experiments/blog/.bundle/                    (marker: Gemfile)
──────────────────────────────────────────────────────

Safe harbors (47 items, 2.3 GB)
  Use --expand-harbors to see details.

Run `ws ignore fix` to resolve interactively.
```

**Output with `--expand-harbors`:**

```text
Safe harbors (47 items, 2.3 GB)
  INFO      bloat           420 MB   ws/ws-log/session-2026-03/stdout.log
  INFO      bloat           180 MB   Archive/vpn-client-v3.deb
  INFO      depth            12 lvl  Archive/backups/2026/Q1/host/etc/nginx/conf.d/upstream.conf
  ... (44 more)
```

#### `ws ignore fix`

Interactively resolve bloat, depth, and project-meta violations — the same violations shown by `ws ignore scan`. Does not touch the `.megaignore` template; use `ws ignore generate` for that.

```text
ws ignore fix [flags]

--dry-run        Preview actions without applying
```

```text
ws ignore fix

── Violations (6 found) ───────────────────────────

CRITICAL  bloat  114 MB  artifacts/datasets/node-metrics-2026.csv
Action? [m]ove to scratch  [a]dd to .megaignore  [s]kip : m

✔ Moved   → ~/Scratch/node-metrics-2026.csv

CRITICAL  bloat   48 MB  experiments/proxy-debug/tcpdump-output.pcap
Action? [m]ove to scratch  [a]dd to .megaignore  [s]kip : a

✔ Added rule to .megaignore: -g:*.pcap

WARNING   bloat   22 MB  artifacts/presentations/quarterly-review.pptx
Action? [m]ove to scratch  [a]dd to .megaignore  [s]kip : s

WARNING   depth    8 lvl  experiments/k8s/cluster/namespaces/logs/app/debug.txt
Action? [a]dd to .megaignore  [s]kip : a

✔ Added path exclude to .megaignore

WARNING   project-meta  24 KB  experiments/projects/java-demo/bin/  (marker: *.java)
Action? [a]dd to .megaignore  [d]elete  [s]kip : a
  Rule scope? [d]irectory (exclude this bin/)  [g]lobal (exclude all bin/ dirs)  : d

✔ Added rule to .megaignore: -:experiments/projects/java-demo/bin

WARNING   project-meta   4 KB  experiments/blog/.bundle/  (marker: Gemfile)
Action? [a]dd to .megaignore  [d]elete  [s]kip : s

──────────────────────────────────────────────────
Fixed: 4   Skipped: 2
```

| Action | Violation types | What it does |
| --- | --- | --- |
| `[m]ove to scratch` | bloat | Move the file to scratch directory (not synced). Removes it from the workspace. |
| `[a]dd to .megaignore` | bloat, depth, project-meta | Append an exclude rule for this file or pattern. File stays but MEGA stops syncing it. For `project-meta`, prompts for scope: exclude this specific path (`-:path`) or all directories with this name (`-:name`). |
| `[d]elete` | project-meta | Delete the build output (safe — it is reproducible). Not offered for bloat/depth since those files may not be reproducible. |
| `[s]kip` | all | Do nothing — move to the next violation. |

#### `ws ignore generate`

Generate or update the `.megaignore` file from the built-in template. Supports replacing, merging with existing rules, and scanning the workspace for additional rule suggestions.

The `.megaignore` is the sync firewall — this command is the safe way to create and maintain it.

```text
ws ignore generate [flags]

--merge          Merge with existing .megaignore instead of replacing
--scan           Scan the workspace and append suggested rules for detected file types
--dry-run        Show what would be generated without writing anything
```

Path behavior is fixed:

- Live ignore file: `<workspace>/.megaignore`
- Canonical state mirror: `<workspace>/ws/megaignore.state`

On fresh workspaces, `ws ignore generate` writes the default template to `<workspace>/.megaignore` and updates `<workspace>/ws/megaignore.state`.

**Built-in template:**

The template is embedded in the binary and covers common exclusions for a Linux workspace. It matches the rules documented in PHILOSOPHY.md.

```text
# .megaignore — MEGA sync ignore rules
# Generated by: ws ignore generate
# Template: builtin v1
#
# Syntax quick reference:
#   -:pattern      Exclude (this directory only)
#   -g:pattern     Exclude (this directory and all subdirectories)
#   +:pattern      Include (override a previous exclude)
#   +g:pattern     Include (this directory and all subdirectories)
#   -s:*           Sync everything not excluded (must be last line)

# ── OS junk ──────────────────────────────────────────
-:Thumbs.db
-:desktop.ini
-:~*
-g:*~                          # Editor backup files (vim file~, emacs file~)
-:.*                           # Hidden files/dirs (.git, .obsidian, .venv, etc.)

# ── Build artifact directories ────────────────────────
-:node_modules
-:__pycache__
-:.venv
-:venv
-:.jekyll-cache
-:_site
-:*.egg-info                   # Python setuptools metadata

# ── Compiled output (always reproducible) ────────────
-g:*.class
-g:*.pyc
-g:*.pyo
-g:*.pyd
-g:*.o
-g:*.obj
-g:*.so
-g:*.a
-g:*.dll
-g:*.dylib
-g:*.exe

# ── Logs and crash dumps (ephemeral) ────────────────
-g:*.log
-g:hs_err_pid*                 # JVM crash dumps

# ── Lock files (reproducible from manifests) ─────────
-g:Gemfile.lock
-g:package-lock.json
-g:yarn.lock
-g:poetry.lock
-g:pnpm-lock.yaml
-g:composer.lock
-g:go.sum

# ── Packages and archives (never sync) ───────────────
-g:*.tar
-g:*.tar.gz
-g:*.tgz
-g:*.deb
-g:*.rpm
-g:*.run
-g:*.zip
-g:*.rar
-g:*.7z
-g:*.jar
-g:*.war
-g:*.ear
-g:*.whl
-g:*.egg
-g:*.gem

# ── Large datasets (never sync) ─────────────────────
-g:*.csv
-g:*.orc
-g:*.parquet

# ── Safe harbors (override ALL excludes above) ──────
# Last-match-wins: these includes are placed after all
# excludes so they take unconditional precedence.
+:ws                           # ws metadata directory
+g:ws/**                       # ws metadata contents (logs, index, config)
+:Archive                      # Intentional sync — configs, creds, machine state
+g:Archive/**                  # Everything in Archive/ syncs regardless of type

# ── Sync everything else ────────────────────────────
-s:*
```

**Output — fresh generate (no existing .megaignore):**

```text
ws ignore generate

✔ Generated ~/Workspace/.megaignore (builtin template, 50 rules)

Run `ws ignore check <path>` to verify the effect on specific files.
Run `ws ignore scan` to find files that may still need exclusion.
```

**Output — existing .megaignore exists (without --merge):**

```text
ws ignore generate

⚠ ~/Workspace/.megaignore already exists (22 rules)

[r] Replace with built-in template (your custom rules will be lost)
[m] Merge — keep your rules, add missing template rules
[d] Diff — show differences between current file and template
[s] Skip

: d

--- current .megaignore   (22 rules)
+++ builtin template      (50 rules)
@@ -12,0 +13,4 @@
+# ── Build artifacts ──────────────────────────────────
+-:.venv
+-:venv
+-:.jekyll-cache
+-:_site
@@ -18,2 +23,0 @@
--g:*.pcap               (your custom rule — not in template)
--g:*.bin                (your custom rule — not in template)

[r] Replace  [m] Merge  [s] Skip : m

✔ Merged — 4 template rules added, 2 custom rules preserved
  Added:    -:.venv, -:venv, -:.jekyll-cache, -:_site
  Kept:     -g:*.pcap, -g:*.bin (your custom rules)
  Total:    26 rules
  Saved:    ~/Workspace/.megaignore
```

**Output — with --merge (explicit):**

```text
ws ignore generate --merge

Merging with existing .megaignore (22 rules)...

  Template rules already present:  46 of 50  (skipped — no duplicates)
  Template rules to add:           4
    -:.venv
    -:venv
    -:.jekyll-cache
    -:_site
  Your custom rules:               4  (preserved)

Apply? [Y/n]: ↵

✔ Merged — 26 rules total
  Saved: ~/Workspace/.megaignore
```

See **Technical Design Decisions → Ignore Merge Logic** for how rule normalization, deduplication, and custom rule preservation work.

**Output — with --scan:**

```text
ws ignore generate --merge --scan

Scanning ~/Workspace for file types...
  Found: 3 × .pcap, 12 × .bin, 847 × .py, 2 × .whl

Suggested rules based on workspace scan:
  -g:*.pcap               3 files, 96 MB total (binary, large)
  -g:*.bin                12 files, 18 MB total (binary)

  Already covered by template:
  -g:*.whl                2 files (template rule exists)
  -g:*.class              13 files (template rule exists)
  -g:*.log                15 files (template rule exists)

Project-meta artifacts detected:
  experiments/projects/java-demo/bin/    (marker: *.java, 24 KB)
  → Run `ws ignore scan` for full project-meta analysis

Add scan suggestions? [Y/n]: ↵

✔ Merged — 60 rules total (50 template + 2 scan suggestions + 8 existing custom)
  Saved: ~/Workspace/.megaignore
```

**Output — dry-run:**

```text
ws ignore generate --dry-run

Would generate ~/Workspace/.megaignore:
  Source: builtin template (50 rules)
  Action: create new file

No changes made.
```

#### `ws ignore edit`

Opens the `.megaignore` file in your editor for direct editing. The file is annotated with inline comments explaining the rule syntax, so you don't need to memorize the MEGA ignore format. After the editor closes, `ws` validates the modified file and reports any syntax errors.

```text
ws ignore edit [flags]

--editor   Override editor (default: $EDITOR, then $VISUAL, then vi)
```

The `.megaignore` file uses MEGA's ignore rule syntax. A quick reference is injected as comments at the top of the file if not already present:

```text
# .megaignore — MEGA sync ignore rules
# Syntax:
#   -:pattern      Exclude files matching pattern (this directory only)
#   -g:pattern     Exclude files matching pattern (this directory and all subdirectories)
#   -:dirname      Exclude directory by name
#   +:pattern      Include files matching pattern (override a previous exclude)
#   +g:pattern     Include files matching pattern (this directory and all subdirectories)
#   -s:*           Sync everything not excluded above (must be last rule)
#
# Rules are evaluated top-to-bottom. The last matching rule wins.
# Place -s:* as the final line to sync everything not explicitly excluded.

-:Thumbs.db
-:desktop.ini
-:.*
-:node_modules
-:__pycache__
-g:*.tar
-g:*.tar.gz
-g:*.csv
-g:*.pcap
+:ws
+g:ws/**
+:Archive
+g:Archive/**
-s:*
```

**Output:**

```text
ws ignore edit

Opening ~/Workspace/.megaignore in vim...

[editor session — user adds -g:*.bin, saves, exits]

✔ Saved .megaignore (14 rules)
  Added:   -g:*.bin
  Effect:  2 files would now be excluded:
             experiments/ssl-trace/ssl_keylog.bin   (2 MB)
             artifacts/debug/trace.bin              (400 KB)
```

**On syntax error:**

```text
ws ignore edit

[editor session — user writes invalid rule, saves, exits]

✗ Syntax error on line 8: "g:*.bin" — missing prefix (expected -g: or +g:)
  Re-open editor to fix? [Y/n]:
```

For non-interactive use (scripting), manipulate `.megaignore` directly — it's a plain text file. Use `ws ignore check <path>` to verify the effect of your changes.

If `.megaignore` is changed outside `ws`, the next `ws ignore` command re-parses it and refreshes `<workspace>/ws/megaignore.state`.

---

### `ws secret`

Scan for exposed secrets in the workspace. Detects patterns like `password=`, `API_KEY=`, private key headers (`-----BEGIN.*PRIVATE KEY-----`), and other high-confidence secret patterns. Uses `grep -rn -E` for scanning. Results are filtered against the allowlist in `manifest.json`.

```text
ws secret [subcommand]
```

#### `ws secret scan`

Read-only scan. Reports files containing secret patterns. Follows the same 2-section `Summary` + `Violations` structure as other scan commands. Secret violations show the file size (not the secret itself), the matched pattern, and the line number for context.

```text
ws secret scan [--skip-dir <dir>] [--pass]
```

**Flags:**

| Flag | Description |
| --- | --- |
| `--skip-dir <dir>` | Skip a directory from scanning. Repeatable. Additive with `secret.skip_dirs` in config. |
| `--pass` | Audit pass store health |

**Config — `secret.skip_dirs`:**

Directories listed in `config.json` under `secret.skip_dirs` are skipped during secret scanning. Paths are relative to workspace root, forward-slash separated, prefix-matched. No globs.

```json
"secret": {
  "enabled": true,
  "pass_nudge": true,
  "skip_dirs": ["vendor", "testdata", "fixtures/certs"]
}
```

**Output:**

```text
ws secret scan — Secret Detection
Summary
──────────────────────────────────────────────────────
Scanned      2026-03-29 09:14:22      profile: secret
Matched      2 files                  2 patterns
Allowlisted  0 entries                in manifest.json
──────────────────────────────────────────────────────

Violations
──────────────────────────────────────────────────────
CRITICAL  secret     2.4 KB  configs/myapp/app.properties (line 14: password=)
WARNING   secret     1.1 KB  experiments/debug-auth.sh   (line 3: API_KEY=)
──────────────────────────────────────────────────────

Run `ws secret fix` to resolve interactively.
```

**Output — clean (no secrets found):**

```text
ws secret scan — Secret Detection
Summary
──────────────────────────────────────────────────────
Scanned      2026-03-29 09:14:22      profile: secret
Matched      0 files                  0 patterns
Allowlisted  2 entries                in manifest.json
──────────────────────────────────────────────────────

Violations
──────────────────────────────────────────────────────
none
──────────────────────────────────────────────────────
```

#### `ws secret fix`

Interactively resolve secret violations. Walks through each finding and offers contextual actions. Designed to help the user make an informed decision without automatically modifying file contents.

```text
ws secret fix [flags]

--dry-run   Preview actions without applying
```

**Output:**

```text
ws secret fix
══════════════════════════════════════════════════════
 SECRET FIX
══════════════════════════════════════════════════════

CRITICAL  2.4 KB  configs/myapp/app.properties (line 14: password=)
Action? [v]iew context  [a]dd to .megaignore  [l]allowlist  [s]kip : v

  12 │ db.host=192.0.2.10
  13 │ db.user=app_svc
  14 │ db.password=s3cret_pass123          ← matched: password=
  15 │ db.port=5432
  16 │ db.pool_size=10

Action? [a]dd to .megaignore  [l]allowlist  [s]kip : a

✔ Added rule to .megaignore: -:app.properties
✔ File will be excluded from MEGA sync on next sync cycle
⚠ The file still exists in ~/Workspace — consider rotating the credential.

WARNING   1.1 KB  experiments/debug-auth.sh (line 3: API_KEY=)
Action? [v]iew context  [a]dd to .megaignore  [l]allowlist  [s]kip : l

✔ Added to secret allowlist: experiments/debug-auth.sh:3

══════════════════════════════════════════════════════
Fixed: 2   Skipped: 0
```

| Action | What it does |
| --- | --- |
| `[v]iew context` | Show 5 lines around the match (grep -C 2). Redisplays the action menu after viewing. |
| `[a]dd to .megaignore` | Append an exclude rule for this file. Prints a warning reminding the user to rotate the exposed credential. |
| `[l]allowlist` | Mark this file+pattern as a known false positive. Stored in `manifest.json` under `secret.allowlist`. Future scans skip this match. |
| `[d]ir-skip` | Skip the parent directory from future secret scans. Writes the directory to `config.json` under `secret.skip_dirs`. Remaining violations in the same directory are skipped in the current session. |
| `[s]kip` | Do nothing — move to the next violation. |

The allowlist is stored in `manifest.json` under `secret.allowlist` as `file:line` entries. See **Technical Design Decisions → Secret Allowlist: Line-Anchored Entries** for the anchoring and expiry design.

#### `ws secret setup`

Set up or validate Unix Password Store (`pass`) prerequisites used by secret workflows. For credential helper connection, use `ws git-credential-helper setup`.

```text
ws secret setup [--git-remote <url>]
```

---

### `ws git-credential-helper`

Git credential helper backed by Unix `pass`. Bridges the git credential protocol with the password store, eliminating the need for third-party helpers like `pass-git-helper` (Python, extra attack surface). Ships as part of the `ws` binary — zero additional dependencies.

```text
ws git-credential-helper <command>

User commands:
  setup        Connect credential helper and create missing pass entries
  status       Show credential helper config, pass health, and remote coverage
  disconnect   Remove ws credential helper from git config

Git plumbing (called by git — not for direct use):
  get          Look up credentials from pass
  store        No-op (pass is managed separately)
  erase        No-op (pass is managed separately)
```

The `get`, `store`, and `erase` operations do not require workspace initialization — git invokes them outside any workspace context. The `setup`, `status`, and `disconnect` commands accept global flags and benefit from workspace context for remote discovery. The legacy alias `ws credential` still works for backward compatibility with existing git configs.

#### `ws git-credential-helper setup`

Configure git to use `ws` as the credential helper and create missing pass entries for workspace git remotes. Uses the Action Plan pattern for per-action consent.

```text
ws git-credential-helper setup [--dry-run]
```

**Prerequisites:** `pass` must be installed and the pass store must be initialized (run `ws secret setup` first if needed).

**Actions:**

1. Set `credential.helper` in global git config to `!<ws-binary-path> git-credential-helper`
2. Set `credential.useHttpPath = true` so git sends the full repo path
3. Scan workspace repos for git remotes, check which `git/<host>` pass entries exist
4. Offer to create missing entries via interactive `pass insert`

**Output:**

```text
ws git-credential-helper setup
  [1/2] Set credential.helper to '!/usr/local/bin/ws git-credential-helper'
  Apply? [y/n/a/q] y
  ✔ Set credential.helper
  [2/2] Set credential.useHttpPath = true
  Apply? [y/n/a/q] y
  ✔ Set credential.useHttpPath

  Workspace git remotes:
    ✔ github.com → git/github.com (exists)
    ✖ gitlab.work.com → git/gitlab.work.com (missing)

  [1/1] Create pass entry git/gitlab.work.com
  Apply? [y/n/a/q] y
  Enter password for git/gitlab.work.com:
  ✔ Created git/gitlab.work.com

✔ git credential helper configured.

  Convention: store git credentials under git/<host> in pass.
  Entry format (standard pass):
    <password or token>
    username: your-username
```

#### `ws git-credential-helper status`

Check credential helper configuration, pass store health, and workspace remote pass entry coverage.

```text
ws git-credential-helper status
```

**Output:**

```text
ws git-credential-helper status
Git Credential Helper
──────────────────────────────────────────────────────
Status              connected
credential.helper   !/usr/local/bin/ws git-credential-helper

Pass Store
──────────────────────────────────────────────────────
  ✔  pass installed
  ✔  gpg available
  ✔  store initialized  (42 entries)

Workspace Remotes
──────────────────────────────────────────────────────
  ✔  github.com → git/github.com (exists)
  ✖  gitlab.work.com → git/gitlab.work.com (missing)

▲ 1 remote(s) missing pass entries — run `ws git-credential-helper setup`
```

#### `ws git-credential-helper disconnect`

Remove the ws credential helper from global git config.

```text
ws git-credential-helper disconnect [--dry-run]
```

**Output:**

```text
ws git-credential-helper disconnect
  [1/2] Remove credential.helper from global git config
  Apply? [y/n/a/q] y
  ✔ Remove credential.helper
  [2/2] Remove credential.useHttpPath from global git config
  Apply? [y/n/a/q] y
  ✔ Remove credential.useHttpPath

✔ Git credential helper disconnected.
```

#### `ws git-credential-helper get`

**Git plumbing — called by git, not for direct use.**

Look up credentials from the pass store. Called by git when it needs authentication. Reads the git credential protocol on stdin (`protocol=`, `host=`, `path=`), looks up the corresponding pass entry, and returns credentials on stdout.

```text
ws git-credential-helper get
```

**Lookup convention:** Credentials are stored under `git/<host>` in the pass store. For per-repo tokens (when `credential.useHttpPath = true`), the path is appended: `git/<host>/<path>`.

**Lookup order:**

1. `git/<host>/<path>` — if path is provided (stripped of trailing `.git`)
2. `git/<host>` — fallback to host-only entry
3. No match — exit silently (git tries the next helper or prompts)

**Pass entry format (standard pass convention):**

```text
<password or token>
username: your-username
url: https://example.com
```

Line 1 is the password. Lines 2+ are scanned for `username:`, `user:`, or `login:` (case-insensitive) to extract the username. Other metadata lines are ignored.

**Example pass store layout:**

```text
~/.password-store/git/
  github.com.gpg                         ← default token for github.com
  github.com/
    work-org/private-repo.gpg            ← specific token for this repo
  gitlab.work.com.gpg                    ← gitlab token
```

#### `ws git-credential-helper store`

**Git plumbing — called by git, not for direct use.**

No-op. Git calls this after successful authentication. The pass store is managed separately via `pass insert`, `ws secret fix`, or `ws git-credential-helper setup`.

#### `ws git-credential-helper erase`

**Git plumbing — called by git, not for direct use.**

No-op. Git calls this after failed authentication. Credential removal from the pass store is a manual operation.

**Security posture:**

- Read-only helper — never writes to the pass store during `get`
- Secrets never appear in logs, even with `--verbose`
- Silent failure on errors — lets git fall back to next helper or prompt
- No credential caching — `gpg-agent` handles GPG passphrase caching
- No stdin injection — mapping is convention-based (`git/<host>`), not configurable via git args

---

### `ws config`

View the active configuration. `ws` reads user settings from `ws/config.json` and writes durable operational metadata to `ws/manifest.json` and `ws/repo.state`. These files are stored inside the workspace for portable restore. To change config values, edit `<workspace>/ws/config.json` directly (or use command flags/env overrides). `<workspace>`, `scratch.root_dir`, `repo.roots`, and `trash.root_dir` are user-provided; log data is always stored in `<workspace>/ws/ws-log`. The dotfile registry is managed through `ws dotfile add` and `ws dotfile rm`; optional dotfile git backup settings are managed via `ws dotfile git connect`; repo state is managed through `ws repo` commands.

```text
ws config [subcommand]
```

#### `ws config view`

Dump the fully resolved configuration as `ws` sees it in memory. This is the result after applying the full resolution chain (flags → env vars → `config.json` → built-in defaults). Useful for debugging why a setting has an unexpected value — every value shown is the final, effective value. Output is valid JSON and can be redirected to a file.

```text
ws config view
```

**Output:**

```json
{
  "config_schema": 1,
  "source": "~/Workspace/ws/config.json",
  "workspace": { "value": "/home/user/Workspace", "source": "config.json" },
  "ignore": {
    "warn_size_mb": { "value": 1, "source": "default" },
    "crit_size_mb": { "value": 10, "source": "default" },
    "max_depth": { "value": 6, "source": "default" },
    "live_path": { "value": "/home/user/Workspace/.megaignore", "source": "derived(<workspace>)" },
    "state_path": { "value": "/home/user/Workspace/ws/megaignore.state", "source": "derived(<workspace>)" },
    "template": { "value": "builtin", "source": "default" }
  },
  "secret": {
    "enabled": { "value": true, "source": "default" },
    "pass_nudge": { "value": true, "source": "default" },
    "skip_dirs": { "value": [], "source": "default" }
  },
  "log": {
    "state_dir": { "value": "/home/user/Workspace/ws/ws-log", "source": "derived(<workspace>)" },
    "cap_mb": { "value": 500, "source": "config.json" },
    "index_file": { "value": "/home/user/Workspace/ws/ws-log-index.md", "source": "derived(<workspace>)" }
  },
  "scratch": {
    "root_dir": { "value": "/home/user/Scratch", "source": "config.json" },
    "editor_cmd": { "value": "code", "source": "default" },
    "name_suffix": { "value": "auto", "source": "default" },
    "prune_after_days": { "value": 90, "source": "default" }
  },
  "trash": {
    "root_dir": { "value": "/home/user/.Trash", "source": "default" },
    "warn_size_mb": { "value": 1024, "source": "default" },
    "setup": {
      "prompt_on_init": { "value": true, "source": "default" },
      "shell_rm": { "value": true, "source": "default" },
      "vscode_delete": { "value": true, "source": "default" },
      "file_explorer_delete": { "value": true, "source": "default" },
      "warn_if_unconfigured": { "value": true, "source": "default" }
    }
  },
  "search": {
    "default_context": { "value": 2, "source": "default" },
    "max_results": { "value": 0, "source": "default" }
  },
  "dotfile": {
    "git": {
      "enabled": { "value": false, "source": "default" },
      "local_repo_dir": { "value": "/home/user/Workspace/ws/dotfiles-git", "source": "derived(<workspace>)" },
      "remote_url": { "value": "", "source": "default" },
      "auth_username": { "value": "", "source": "default" },
      "pass_entry": { "value": "", "source": "default" },
      "branch": { "value": "main", "source": "default" },
      "auto_commit": { "value": true, "source": "default" },
      "auto_push": { "value": true, "source": "default" }
    }
  },
  "repo": {
    "roots": { "value": ["."], "source": "config.json" },
    "exclude_dirs": { "value": ["ws", "node_modules", ".venv"], "source": "config.json" },
    "max_parallel": { "value": 8, "source": "default" },
    "reconcile_on_read": { "value": true, "source": "default" },
    "state_path": { "value": "/home/user/Workspace/ws/repo.state", "source": "derived(<workspace>)" }
  },
  "notify": {
    "enabled": { "value": true, "source": "default" },
    "poll_interval_min": { "value": 10, "source": "default" },
    "events": { "value": ["dotfile", "secret", "bloat", "storage"], "source": "default" }
  },
  "manifest": {
    "path": "~/Workspace/ws/manifest.json",
    "manifest_schema": 1,
    "dotfiles_count": 5,
    "repo_tracked_count": 3
  }
}
```

All paths are shown fully expanded (tildes resolved, relative paths resolved against workspace root). Each value is annotated with its source: `config.json`, `env`, `flag`, or `default`. The `dotfiles` array in `manifest.json` is summarized as a count — use `ws dotfile ls` for the full registry.

**Output — with flag override:**

```json
{
  "config_schema": 1,
  "source": "~/Workspace/ws/config.json",
  "workspace": { "value": "/home/user/Workspace", "source": "config.json" }
}
```

**Output — JSON:**

```json
{
  "ws_version": "0.1.0",
  "schema": 1,
  "command": "config.view",
  "data": {
    "config_schema": 1,
    "source": "~/Workspace/ws/config.json",
    "workspace": { "value": "/home/user/Workspace", "source": "config.json" },
    "ignore": {
      "warn_size_mb": { "value": 1, "source": "default" },
      "crit_size_mb": { "value": 10, "source": "default" },
      "max_depth": { "value": 6, "source": "default" },
      "live_path": { "value": "/home/user/Workspace/.megaignore", "source": "derived(<workspace>)" },
      "state_path": { "value": "/home/user/Workspace/ws/megaignore.state", "source": "derived(<workspace>)" },
      "template": { "value": "builtin", "source": "default" }
    },
    "secret": {
      "enabled": { "value": true, "source": "default" },
      "pass_nudge": { "value": true, "source": "default" },
      "skip_dirs": { "value": [], "source": "default" }
    },
    "log": {
      "state_dir": { "value": "/home/user/Workspace/ws/ws-log", "source": "derived(<workspace>)" },
      "cap_mb": { "value": 500, "source": "config.json" },
      "index_file": { "value": "/home/user/Workspace/ws/ws-log-index.md", "source": "derived(<workspace>)" }
    },
    "scratch": {
      "root_dir": { "value": "/home/user/Scratch", "source": "config.json" },
      "editor_cmd": { "value": "code", "source": "default" },
      "name_suffix": { "value": "auto", "source": "default" },
      "prune_after_days": { "value": 90, "source": "default" }
    },
    "trash": {
      "root_dir": { "value": "/home/user/.Trash", "source": "default" },
      "warn_size_mb": { "value": 1024, "source": "default" },
      "setup": {
        "prompt_on_init": { "value": true, "source": "default" },
        "shell_rm": { "value": true, "source": "default" },
        "vscode_delete": { "value": true, "source": "default" },
        "file_explorer_delete": { "value": true, "source": "default" },
        "warn_if_unconfigured": { "value": true, "source": "default" }
      }
    },
    "search": {
      "default_context": { "value": 2, "source": "default" },
      "max_results": { "value": 0, "source": "default" }
    },
    "dotfile": {
      "git": {
        "enabled": { "value": false, "source": "default" },
        "local_repo_dir": { "value": "/home/user/Workspace/ws/dotfiles-git", "source": "derived(<workspace>)" },
        "remote_url": { "value": "", "source": "default" },
        "auth_username": { "value": "", "source": "default" },
        "pass_entry": { "value": "", "source": "default" },
        "branch": { "value": "main", "source": "default" },
        "auto_commit": { "value": true, "source": "default" },
        "auto_push": { "value": true, "source": "default" }
      }
    },
    "repo": {
      "roots": { "value": ["."], "source": "config.json" },
      "exclude_dirs": { "value": ["ws", "node_modules", ".venv"], "source": "config.json" },
      "max_parallel": { "value": 8, "source": "default" },
      "reconcile_on_read": { "value": true, "source": "default" },
      "state_path": { "value": "/home/user/Workspace/ws/repo.state", "source": "derived(<workspace>)" }
    },
    "notify": {
      "enabled": { "value": true, "source": "default" },
      "poll_interval_min": { "value": 10, "source": "default" },
      "events": { "value": ["dotfile", "secret", "bloat", "storage"], "source": "default" }
    },
    "manifest": {
      "path": "~/Workspace/ws/manifest.json",
      "manifest_schema": 1,
      "dotfiles_count": 5,
      "repo_tracked_count": 3
    }
  }
}
```

#### `ws config defaults`

Print the built-in default configuration as a valid `config.json` file. This is the config that `ws` would use if no `config.json` existed and no flags or env vars were set. Useful as a starting point when creating a new config from scratch, or for diffing against your current config to see what you've customized.

```text
ws config defaults
```

**Output:**

```json
{
  "config_schema": 1,
  "workspace": "~/Workspace",
  "ignore": {
    "warn_size_mb": 1,
    "crit_size_mb": 10,
    "max_depth": 6,
    "template": "builtin"
  },
  "secret": {
    "enabled": true,
    "pass_nudge": true,
    "skip_dirs": []
  },
  "scratch": {
    "root_dir": "~/Scratch",
    "editor_cmd": "code",
    "name_suffix": "auto",
    "prune_after_days": 90
  },
  "trash": {
    "root_dir": "~/.Trash",
    "warn_size_mb": 1024,
    "setup": {
      "prompt_on_init": true,
      "shell_rm": true,
      "vscode_delete": true,
      "file_explorer_delete": true,
      "warn_if_unconfigured": true
    }
  },
  "log": {
    "cap_mb": 500
  },
  "search": {
    "default_context": 2,
    "max_results": 0
  },
  "dotfile": {
    "git": {
      "enabled": false,
      "remote_url": "",
      "auth_username": "",
      "pass_entry": "",
      "branch": "main",
      "auto_commit": true,
      "auto_push": true
    }
  },
  "notify": {
    "enabled": true,
    "poll_interval_min": 10,
    "events": ["dotfile", "secret", "bloat", "storage"]
  },
  "repo": {
    "roots": ["."],
    "exclude_dirs": ["ws", "node_modules", ".venv"],
    "max_parallel": 8,
    "reconcile_on_read": true
  }
}
```

Output is always uncolored, valid JSON, and can be piped directly into a file:

```bash
ws config defaults > <workspace>/ws/config.json
```

The `dotfiles` registry is omitted — dotfiles are managed in `manifest.json` via `ws dotfile add` and `ws dotfile rm`, not by hand-editing defaults output.

---

### `ws restore`

Guided full-machine restore wizard. Chains the post-sync setup steps into a single interactive flow. Designed for the day you lose everything: sync the workspace to a fresh machine, run `ws restore`, walk through the prompts, and the machine is configured.

**Prerequisite:** The workspace must already be initialized (`ws/config.json` must exist). `ws restore` is for synced workspaces on new machines — not for first-time setup. If the workspace is not initialized, `ws restore` exits with an error and directs the user to run `ws init` first.

```text
ws restore [flags]

--dry-run   Preview all actions without applying
```

**Output — workspace not initialized:**

```text
ws restore
✖ Workspace is not initialized: ~/Workspace/ws/config.json

  ws restore only works on an already-initialized workspace.
  Run 'ws init' first to set up the workspace, then run 'ws restore'.
```

**Output — workspace initialized (normal flow):**

```text
ws restore
══════════════════════════════════════════════════════
 WORKSPACE RESTORE
══════════════════════════════════════════════════════

Steps: trash setup → dotfile fix → ignore generate → scan → fix

── [1/5] Trash setup ─────────────────────────────

✔ shell-rm integration configured
✔ vscode-delete integration configured
✔ file-explorer integration configured

── [2/5] Dotfiles ─────────────────────────────────

ws dotfile fix — Dotfile Reconciliation
Registry: manifest.json  (5 dotfiles)
Storage:  ws/dotfiles/
──────────────────────────────────────────────────

Scanning...
  5 dotfiles to link
  1 dotfile requires sudo

Apply? [Y/n]: ↵

[1/5]  ~/.ssh                → ws/dotfiles/ssh/               ✔ created
[2/5]  ~/.bashrc             → ws/dotfiles/bashrc              ✔ created
[3/5]  /etc/docker/...       → ws/dotfiles/daemon.json         ✔ created  (sudo)
[4/5]  ~/.kube/config        → ws/dotfiles/kubeconfig          ✔ created
[5/5]  ~/.config/Code/User/… → ws/dotfiles/vscode-settings…    ✔ created

Created: 5   Skipped: 0   Failed: 0

── [3/5] Ignore rules ─────────────────────────────

✔ .megaignore is up to date (22 rules, matches template)

── [4/5] Scan ─────────────────────────────────────

Ignore       0 critical · 0 warning   no bloat/depth/project-meta
Secret       0 critical · 0 warning   0 files matched
Dotfiles     0 critical · 0 warning   5 registered
Log          recording inactive        0 MB / 500 MB cap

── [5/5] Fix ──────────────────────────────────────

No violations found. Nothing to fix.

══════════════════════════════════════════════════════
 ✔ Restore complete
══════════════════════════════════════════════════════

Your machine is configured. Suggested next steps:
  ws completions bash    Generate shell completions
  ws tui                 Open the workspace dashboard
```

Each step is self-contained. If a step fails, the wizard reports the failure and continues to the next step. The user can re-run `ws restore` safely — every sub-step is idempotent.

---

### `ws notify`

Background notification daemon. Monitors workspace state via `inotify` and periodic scans, then pushes desktop notifications via `notify-send` when actionable events occur. Runs as a systemd user service.

```text
ws notify start                      Start the notification daemon
ws notify stop                       Stop the notification daemon
ws notify status                     Check daemon status
ws notify test                       Send a test notification
```

**Events that trigger notifications:**

| Event | Severity | Notification |
| --- | --- | --- |
| Dotfile symlink broken | Critical | `⚠ ws: ~/.bashrc symlink is broken — target missing` |
| Dotfile overwritten by package manager | Warning | `⚠ ws: /etc/docker/daemon.json was overwritten (real file, not symlink)` |
| Secret pattern detected after sync | Critical | `🔒 ws: new secret pattern found in experiments/debug-auth.sh` |
| Sync storage cap > 80% | Warning | `📦 ws: log storage at 420 MB / 500 MB (84%)` |
| Sync storage cap > 95% | Critical | `📦 ws: log storage at 485 MB / 500 MB — prune needed` |
| Bloat file detected after sync | Warning | `📁 ws: new 114 MB file detected — artifacts/datasets/node-metrics.csv` |

**Setup:**

```text
ws notify start

✔ Created   ~/.config/systemd/user/ws-notify.service
✔ Enabled   ws-notify.service
✔ Started   ws-notify.service

Notification daemon is running.
Mode: inotify + periodic scan
Notify via: notify-send

Test with: ws notify test
```

```text
ws notify status

● ws-notify.service — ws notification daemon
  Status:       active (running)
  Mode:         inotify on ~/Workspace + periodic scan (every 10 min)
  Last scan:    2026-03-29 09:14:22
  Last alert:   2026-03-29 08:42:10 (bloat: infra-review.pptx)
  Health file:  ~/Workspace/ws/health.json (2 min ago)
```

See **Beyond CLI → Notification Daemon Design** for implementation details.

---

### `ws completions`

Generate shell completion scripts for bash, zsh, or fish. Also supports install/uninstall helpers that manage shell rc/config files directly.

```text
ws completions <shell>
ws completions install [--shell <bash|zsh|fish>] [--dry-run]
ws completions uninstall [--shell <bash|zsh|fish>] [--dry-run]

Supported shells: bash, zsh, fish
```

**Output — bash:**

```text
ws completions bash

# Shell completion script printed to stdout.
# To install permanently:
#   ws completions bash > ~/.local/share/bash-completion/completions/ws
#
# To use in current session:
#   eval "$(ws completions bash)"

_ws_completions() {
    local cur prev commands
    cur="${COMP_WORDS[COMP_CWORD]}"
    prev="${COMP_WORDS[COMP_CWORD-1]}"
    commands="version scan fix link script log search ignore secret config completions"
    ...
}
complete -F _ws_completions ws
```

**Output — zsh:**

```text
ws completions zsh

# To install permanently:
#   ws completions zsh > ~/.zfunc/_ws
#   (ensure ~/.zfunc is in $fpath)
#
# To use in current session:
#   eval "$(ws completions zsh)"
```

**Output — fish:**

```text
ws completions fish

# To install permanently:
#   ws completions fish > ~/.config/fish/completions/ws.fish
```

The completion script covers all commands, subcommands, flags, and flag values. It is regenerated from the command tree at build time — no manual maintenance.

---

## Beyond CLI

The following features build on top of the CLI foundation. They reuse the same config, manifest, and violation model — no new data formats. Each one is an optional layer: the CLI works without them, they work because of the CLI.

---

### `ws tui`

Full-screen interactive terminal dashboard. Shows workspace health, violations, dotfile status, log sessions, and storage in a single view. Built with Go's native terminal capabilities (raw mode, ANSI escape sequences) — no third-party TUI libraries.

```text
ws tui
```

**Layout:**

```text
┌─ ws — ~/Workspace ──────────────────────────────────────────────────────┐
│                                                                         │
│  HEALTH          DOTFILES           LOG             STORAGE             │
│  ✔ 0 critical    5 registered       ● recording     Workspace  4.3 GB  │
│  ⚠ 1 warning     5 ok / 0 broken    tag: daily-29   Scratch    1.2 GB  │
│                                      42 min          Log        312 MB  │
│                                                                         │
├─ Violations (1) ────────────────────────────────────────────────────────┤
│                                                                         │
│  WARNING  bloat  22 MB  artifacts/presentations/quarterly-review.pptx  │
│                                                                         │
├─ Recent sessions ───────────────────────────────────────────────────────┤
│                                                                         │
│  daily-29   ● active     42 min     18 commands     12 MB              │
│  daily-28   2026-03-28   1h 14m     47 commands     28 MB              │
│  daily-27   2026-03-27   2h 03m     63 commands     34 MB              │
│                                                                         │
├─ Dotfiles ──────────────────────────────────────────────────────────────┤
│                                                                         │
│  ✔ ~/.ssh                → ssh/                                         │
│  ✔ ~/.bashrc             → bashrc                                       │
│  ✔ /etc/docker/daemon…   → daemon.json           (sudo)                │
│  ✔ ~/.kube/config        → kubeconfig                                   │
│  ✔ ~/.config/Code/User/… → vscode-settings.json                        │
│                                                                         │
├─ Keys ──────────────────────────────────────────────────────────────────┤
│  q quit   r refresh   s scan   f fix   d dotfiles   l logs   ? help    │
└─────────────────────────────────────────────────────────────────────────┘
```

**Key bindings:**

| Key | Action |
| --- | --- |
| `q` | Quit |
| `r` | Refresh all panels (re-runs scan) |
| `s` | Run `ws scan`, update violations panel |
| `f` | Run `ws fix` (drops to interactive CLI, returns to TUI after) |
| `d` | Focus dotfiles panel — show full `ws dotfile ls` view |
| `l` | Focus log panel — browse sessions, preview commands |
| `↑`/`↓` | Navigate within focused panel |
| `Enter` | Drill into selected item (show details / run action) |
| `?` | Show help overlay |

**Design constraints:**

- No third-party TUI libraries (no bubbletea, no tview). Raw ANSI escape sequences + Go's `os.Stdin` raw mode.
- TUI is read-heavy. Write actions (`f` for fix) drop to the normal interactive CLI and return to TUI when done.
- Panels refresh on launch. Manual `r` for subsequent refreshes. No background polling inside the TUI itself.
- Terminal size detection via `TIOCGWINSZ` ioctl. Graceful degradation if terminal is too small (show summary only).

---

### Notification Daemon Design

Implementation details for `ws notify` (command reference in CLI Reference above).

#### Detection: inotify + periodic scan

The daemon uses two complementary detection mechanisms:

1. **inotify watches** — real-time, event-driven:
   - Watches `<workspace>/ws/dotfiles/` for deletions and modifications (detects broken/overwritten dotfiles immediately).
   - Watches MEGA's sync state directory (`~/.local/share/data/Mega Limited/MEGAsync/`) for sync completion transitions. When sync goes from "syncing" → "up to date", the daemon triggers a full `ws scan --json --quiet` to catch new bloat, secrets, or depth violations that arrived via sync.

2. **Periodic scan** — fallback and catch-all:
   - Runs `ws scan --json --quiet` every N minutes (default: 10, configurable).
   - Catches anything inotify misses: storage cap pressure, secret patterns in new files, depth violations in new directories.
   - Interval is reset after every inotify-triggered scan to avoid redundant work.

#### State: `ws/health.json`

After every scan (inotify-triggered or periodic), the daemon writes the result to `ws/health.json`. This file is consumed by `ws tui` and shell prompt integrations for near-real-time workspace health without running scans themselves.

`ws/health.json` is excluded from MEGA sync via `.megaignore` (runtime-only state).

```json
{
  "timestamp": "2026-03-29T09:14:22Z",
  "trigger": "mega-sync",
  "summary": {
    "ignore": { "critical": 0, "warning": 1 },
    "secret": { "critical": 0, "warning": 0 },
    "dotfile": { "critical": 0, "warning": 0 },
    "log": { "active": true, "cap_percent": 62 },
    "trash": { "configured": false, "warnings": 2 }
  },
  "violations_count": 2,
  "violations": [
    {
      "group": "ignore",
      "type": "bloat",
      "severity": "warning",
      "size_mb": 22,
      "path": "artifacts/presentations/quarterly-review.pptx"
    },
    {
      "group": "trash",
      "type": "machine-setup",
      "severity": "warning",
      "path": "vscode",
      "message": "VS Code delete-to-trash is not configured"
    }
  ]
}
```

#### Deduplication: `ws/notify.state`

The daemon maintains `ws/notify.state` — a record of the last-notified violations. It compares each scan result against this state and only fires notifications for *new* or *changed* violations. Known issues are never re-notified until they change or are resolved and reappear.

#### Systemd integration

- Runs as `systemd --user` service (`ws-notify.service`).
- `ws notify start` generates and enables the systemd unit file at `~/.config/systemd/user/ws-notify.service`.
- `ws notify stop` stops and disables the unit.
- `ws notify status` queries `systemctl --user status ws-notify.service` and enriches it with last-scan and last-alert metadata from `ws/notify.state`.

#### Config keys (in `ws/config.json`)

```json
{
  "notify": {
    "enabled": true,
    "poll_interval_min": 10,
    "push_interval_min": 5,
    "events": ["dotfile", "secret", "bloat", "storage"]
  }
}
```

- `push_interval_min`: How often the daemon pushes dotfile and pass store git repos to their remotes (default 5). Acts as a safety net for failed at-commit pushes.

---
