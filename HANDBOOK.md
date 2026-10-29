# `ws` Handbook

`ws` manages one directory, `~/Workspace`, which a sync client (MEGA, Zoho WorkDrive, or any
folder-sync tool) keeps in the cloud, and everything a developer machine needs to be rebuilt
from it: symlinked configs, git repos, secrets and the pass store, sync rules, scratch directories,
cron jobs, and the trash, plus a front end for the terminal sessions agent-tmux records. It ships as one static binary with two
front ends over the same `internal/` packages: the CLI (`ws <noun> <verb>`) and a full-screen
TUI (`ws tui`).

This handbook is the one reference for ws. It holds the philosophy behind it (Part 1), the
authoritative spec of the intended behaviour (Parts 2 to 8), and the developer guide (Part 9).
Section numbers are stable: code comments cite them as `spec §x.y`.

## Contents

- [1. Principles](#1-principles)
  - [1.1 How ws is built](#11-how-ws-is-built)
  - [1.2 The Workspace Factors](#12-the-workspace-factors)
- [2. Workspace and state](#2-workspace-and-state)
  - [2.1 Where things live](#21-where-things-live)
  - [2.2 Layout created by `ws init`](#22-layout-created-by-ws-init)
  - [2.3 `ws.json`](#23-wsjson)
  - [2.4 Resolution order](#24-resolution-order)
  - [2.5 Restoring a machine](#25-restoring-a-machine)
  - [2.6 Sync clients](#26-sync-clients)
- [3. Command grammar](#3-command-grammar)
  - [3.1 Rules](#31-rules)
  - [3.2 Nouns](#32-nouns)
  - [3.3 Verb glossary](#33-verb-glossary)
  - [3.4 Every command](#34-every-command)
  - [3.5 Renamed and removed commands](#35-renamed-and-removed-commands)
- [4. CLI conventions](#4-cli-conventions)
  - [4.1 Global flags](#41-global-flags)
  - [4.2 Help](#42-help)
  - [4.3 Lists](#43-lists)
  - [4.4 JSON](#44-json)
  - [4.5 Paths](#45-paths)
  - [4.6 Exit codes](#46-exit-codes)
  - [4.7 Read and write commands](#47-read-and-write-commands)
  - [4.8 Completion](#48-completion)
  - [4.9 Environment variables](#49-environment-variables)
  - [4.10 Editor](#410-editor)
- [5. Command reference](#5-command-reference)
  - [`ws repo`](#ws-repo)
  - [`ws symlink`](#ws-symlink)
  - [`ws skill`](#ws-skill)
  - [`ws scratch`](#ws-scratch)
  - [`ws session`](#ws-session)
  - [`ws ignore`](#ws-ignore)
  - [`ws secret`](#ws-secret)
  - [`ws pass`](#ws-pass)
  - [`ws credential`](#ws-credential)
  - [`ws cron`](#ws-cron)
  - [`ws trash`](#ws-trash)
  - [`ws sync`](#ws-sync)
  - [Workspace commands](#workspace-commands)
- [6. TUI](#6-tui)
  - [6.1 Frame](#61-frame)
  - [6.2 Screens](#62-screens)
  - [6.3 Global keys](#63-global-keys)
  - [6.4 Lists](#64-lists)
  - [6.5 Screen keys](#65-screen-keys)
  - [6.6 Command bar](#66-command-bar)
  - [6.7 Dialogs](#67-dialogs)
  - [6.8 Handing over the terminal](#68-handing-over-the-terminal)
  - [6.9 Parity](#69-parity)
  - [6.10 Cues](#610-cues)
- [7. Design notes](#7-design-notes)
  - [7.1 No single failure breaks a batch](#71-no-single-failure-breaks-a-batch)
  - [7.2 Output styling](#72-output-styling)
  - [7.3 Deletion: one shared trash](#73-deletion-one-shared-trash)
  - [7.4 Private remotes and credentials](#74-private-remotes-and-credentials)
  - [7.5 One description per list](#75-one-description-per-list)
  - [7.6 One grammar for CLI and TUI](#76-one-grammar-for-cli-and-tui)
  - [7.7 Sync clients](#77-sync-clients)
- [8. Dependencies](#8-dependencies)
  - [8.1 Programs ws runs](#81-programs-ws-runs)
  - [8.2 Build](#82-build)
  - [8.3 Around ws](#83-around-ws)
- [9. Developer guide](#9-developer-guide)
  - [9.1 Machine setup](#91-machine-setup)
  - [9.2 Clone, build, run, deploy](#92-clone-build-run-deploy)
  - [9.3 Test suite](#93-test-suite)
  - [9.4 Daily flow](#94-daily-flow)
  - [9.5 Release pipeline](#95-release-pipeline)
  - [9.6 Adding a command](#96-adding-a-command)
  - [9.7 The command table: help, completion, aliases, coverage](#97-the-command-table-help-completion-aliases-coverage)
  - [9.8 Paths in code](#98-paths-in-code)
  - [9.9 Positional targets](#99-positional-targets)
  - [9.10 TUI development](#910-tui-development)
  - [9.11 Testing `ws cron` by hand](#911-testing-ws-cron-by-hand)
  - [9.12 Troubleshooting](#912-troubleshooting)

---

## 1. Principles

### 1.1 How ws is built

- **One binary.** A static Go binary. The core and the CLI use only the standard
  library. The TUI uses Charm v2 (Bubble Tea, Bubbles, Lip Gloss). The only runtime
  dependencies are the system tools listed in Part 8.
- **The workspace carries the state.** Durable state lives in `<workspace>/ws.json`, which
  syncs with the workspace. Sync the folder to a new machine and ws knows your symlinks,
  rules, allowlist and tags. Machine-local state (cron run state, trash integration state)
  lives in the XDG state directory and is never synced.
- **Offload, don't reimplement.** `git`, `pass`, `crontab`, agent-tmux and the coreutils do
  the work. ws orchestrates them, keeps state, and reports.
- **One grammar.** Every command is `ws <noun> <verb>`, and every verb means one thing
  (Part 3). The TUI uses the same nouns as screens and the same verbs as keys and `:`
  commands (§7.6).
- **One description per list.** Each list is described once (its fields), and the CLI
  `ls`, its `--json`, and its TUI screen are all rendered from that description (§7.5).
- **JSON everywhere.** Every command supports `--json` with a versioned envelope (§4.4).

Read/write separation, soft delete and private remotes are principles too. They come from
the workspace factors below (IV, IX and X).

### 1.2 The Workspace Factors

The workspace is organised by a small methodology, inspired by the
[Twelve-Factor App](https://12factor.net). Its governing idea: **flat hierarchy first.**
Every factor exists to reduce capture friction, improve searchability, and keep recovery
fast. Each factor below names the ws feature that carries it.

| # | Factor | Core idea | ws feature |
| --- | --- | --- | --- |
| I | Single source of truth | One directory, everything that matters | the workspace, `ws init` |
| II | Friction kills memory | Capture fast; search finds it later | flat layout, `ws scratch` |
| III | Searchability is survival | If you can't grep it, it doesn't exist | `ws session grep` (agent-tmux), descriptive names |
| IV | Originals in, symlinks out | Real files in sync, pointers at system paths | `ws symlink` |
| V | Flat hierarchy first | Separate only when truly needed | the `ws init` layout |
| VI | Git and sync are complementary | Sync moves files, git moves history | `ws repo`, `ws pass backup` |
| VII | Sync scripts, quarantine state | Scripts sync, log dumps don't | `ws scratch`, `ws ignore` |
| VIII | Sync with intent | Some files don't deserve sync | `ws ignore` |
| IX | Always soft delete | Delete means a recoverable move to the trash | `ws trash` |
| X | Per-action consent | Confirm each mutation, not the batch | the action plan (§4.7) |
| XI | Pin the clipboard | The clipboard forgets, your workspace shouldn't | a habit; `ws scratch` |
| XII | Sync is one-way | The laptop is the truth, the cloud copy is a mirror | `ws ignore`, `ws sync` |

#### I. Single source of truth

*Everything that matters lives in one directory. Anywhere else, it won't be synced.*

Config files are **accumulated state**: SSH proxy-jump rules that took a week to work out,
Docker daemon settings tuned for one network, Kubernetes contexts. They are small and
costly to rebuild. Every config gets synced, with no exception for "I haven't used it in
months": syncing 2 KB costs nothing, and recreating it from memory costs without bound.

`~/Workspace` removes the scattered-files problem (`~/.ssh/config` here, `~/.bashrc`
there, screenshots in `~/Pictures`, scripts in `/tmp`). It is one tree that syncs as a
unit, laid out so that a fresh Linux machine can be restored with few steps (§2.5). It is
designed for the day you lose everything.

#### II. Friction kills memory

*If it's hard to write down, it won't get written down.*

The bottleneck is capture, not retrieval. The moment you hear a useful command in a chat
thread, see a URL to an internal tool, or find the config flag that finally works is the
moment to record it. Optimise for write speed:

- Don't agonise over which subdirectory. Drop it in the closest relevant folder.
- Don't invent a naming convention. `notes.md`, `stuff.md` and `2026-03.md` are all fine.
- Don't limit the format. A screenshot, a pasted message, a raw URL, a one-liner with no
  context: something is always better than nothing.
- Don't create a subfolder for three files. Flat is fast.

If the workspace makes you think "where should this go?" for more than five seconds, the
workspace has failed.

#### III. Searchability is survival

*If you can't find it, you don't have it.*

Three layers of search work together: **textual** (`grep -rn rsync` across scripts),
**contextual** (GPU setup lives in `experiments/`, certificates in `artifacts/`), and
**semantic** ("the script that checks whether nodes are reachable" is
`check-node-status.sh`). Name things descriptively and put them where you would look
first. `ws session grep` (agent-tmux) searches recorded terminal sessions.

*Eyes open:* grep reads text only. PDFs, screenshots and binary datasets are invisible to
it, so descriptive directory and file names are their only index.

#### IV. Originals in, symlinks out

*The real file lives in the sync folder. Anywhere else gets a pointer.*

Config files that the OS expects at fixed paths (`~/.ssh/config`, `~/.bashrc`,
`/etc/docker/daemon.json`) keep their **originals** under `symlinks/`, at the same path they
have on the machine (`symlinks/home/.bashrc`, `symlinks/system/etc/docker/daemon.json`). The
host paths are symlinks pointing in. `ws symlink add` moves the original into the workspace
and leaves the symlink behind.

Never the reverse. If the original stays at `~/.bashrc` and a symlink into the workspace
syncs, a non-synced location is the source of truth. One bad `apt upgrade` replaces the real
file, and sync is left with a dangling link or a default config.

```text
┌──────────── ORIGINALS (in ~/Workspace/symlinks/) ────────────┐
│  home/.bashrc    home/.gitconfig    system/etc/docker/daemon.json │
└───────────────┬──────────────────────────────┬────────────────┘
                │           SYMLINKS           │
                ▼                              ▼
   ~/.bashrc → symlinks/home/.bashrc     /etc/docker/daemon.json  (sudo)
```

**The storage path is a function of the host path.** ws never asks where to put a file.
`~/.config/Code/User/settings.json` is stored at `symlinks/home/.config/Code/User/settings.json`
and `/etc/hosts` at `symlinks/system/etc/hosts`. Two apps with a file of the same name never
collide, and any file under `symlinks/` maps back to its host path by removing the prefix. To
store something elsewhere, give two paths: `ws symlink add <host-path> <ws-path>`.

**ws knows what is worth keeping.** A catalog compiled into the binary lists about 140
well-known configs (git, ssh, shells, editors, terminals, cloud CLIs, AI tools). ws checks it
silently and reports only what exists on this machine and is not symlinked yet. `ws symlink add`
with no arguments shows that list as a checklist. Folders that mix config with state
(`~/.ssh`, `~/.claude`, `~/.gnupg`) are never listed whole, only their safe children.

**Secrets may be tracked.** Private keys and credential files sometimes have to follow you.
They live in the same tree as everything else and start unchecked in the checklist. Rotation
and access control stay your responsibility.

*Eyes open:* a change to the symlinked file changes the original in sync, and sync is the only
protection: there is no backup repository. A user who wants history can run `git init` in
`symlinks/`, and `ws repo` will see it.

#### V. Flat hierarchy first

*Separate only when truly needed.*

A work laptop is also the primary laptop: tech resources, personal notes, résumés and side
projects all need to persist. Uploading to a personal cloud by hand is friction that kills
the habit. So work and personal files live side by side in the same top-level folders.
Capture speed and retrieval speed matter more than folder purity.

- **Default to top-level placement.** When unsure, use the nearest top-level domain
  (`artifacts/`, `experiments/`, `notes/`, `data/`). Symlinked configs are managed under
  `symlinks/`.
- **Delay taxonomy.** Create a subfolder after repeated pressure, not in anticipation.
- **Optimise for future search, not present neatness.** A slightly messy flat tree beats a
  perfectly nested one nobody can navigate.
- **Separate only for real boundaries:** compliance, access control, handoff packaging, or
  large-volume isolation. Aesthetics alone is not a reason.

Ambiguous items (a résumé? interview prep? incident notes?) go where they are most useful,
not where they are most "correct".

#### VI. Git and sync are complementary

*The sync client moves files. Git moves history. Some repos deserve both.*

By default, git-backed repos live outside the workspace (for example `~/Repositories`) and
synced files live in `~/Workspace` without git. Reality has exceptions:

- **Work repos** need sync because access to the remote can vanish overnight (layoff,
  credential revocation, VPN loss). If the laptop is lost too, the code is gone.
- **Personal knowledge repos** (`notes/second-brain/`, `data/bruno/`, `experiments/blog/`)
  deserve redundant backup: git for versioning, the sync client for instant availability
  even if the git host is down.

For these, both coexist. `.git/` stays out of the sync (the built-in `-:.*` rule), so the
client syncs the working tree while git tracks history on its own. `ws repo` finds such
repos and runs git in them (`ws repo git`).

#### VII. Sync scripts, quarantine state

*Ad-hoc scripts deserve sync. Ad-hoc state doesn't.*

A debugging script, a 30-minute `rsync` invocation, the `jq` pipeline that finally parsed
the broken JSON: these are **knowledge**, and they sync. The **state** they produce (200 MB
log dumps, container images, downloaded binaries, intermediate CSVs) is transient. It goes
to `~/Scratch/`, which is not synced, or is excluded by the sync rules.

```text
$ ws scratch new proxy-timeout
✔ Created   ~/Scratch/proxy-timeout.2026-03/
✔ Opening   VS Code → ~/Scratch/proxy-timeout.2026-03/

# When you find scripts or notes worth keeping, copy them in:
$ cp ~/Scratch/proxy-timeout.2026-03/debug.sh ~/Workspace/experiments/
```

```text
┌────────────────────────────────────┐
│  ~/Workspace/...   ← SYNCED        │
│    scripts, notes, findings        │
│    small configs, analysis         │
│                                    │
│  ~/Scratch/...     ← NOT SYNCED    │
│    log dumps, binaries, blobs      │
│    anything re-downloadable        │
└────────────────────────────────────┘
```

#### VIII. Sync with intent

*Keep sync lean. Quarantine large blobs. Avoid deep nesting.*

Don't sync large artifacts, especially reproducible ones. If a large blob must sync (a
custom-built binary, an internal VPN client `.deb`, a critical dataset), do it
consciously. Deeply nested trees (`.git`, `.venv/`, `node_modules/`, `__pycache__`) cost
the sync client scan time, so exclude them.

The ignore rules are a sync firewall. ws ships a built-in template of OS junk, dot-files,
temp files, build directories, compiled output, logs, lock files, archives and large datasets, and
adds your own rules on top (`ws ignore add`). §5 `ws ignore` has the rule order and the
template's categories. `ws ignore lint` warns about files that sync but probably
shouldn't: bloat, excessive depth, build output.

#### IX. Always soft delete

*Delete should mean reversible by default.*

Accidental deletion is a high-cost failure, and recovery should not need backups,
forensic tools or panic. Everything ws deletes goes to the XDG trash (§7.3), and
`ws trash enable` extends that to the places you delete from:

- terminal `rm` routes to the trash;
- VS Code's delete uses the system trash;
- the file explorer's delete goes to the trash, with no permanent-delete shortcut.

Permanent deletion is an explicit, deliberate act (`trash rm`, `trash empty`,
`trash prune`). Routine cleanup is always recoverable.

#### X. Per-action consent

*Confirm each mutation, not the batch.*

A single "Proceed? Y/n" before N operations is a rubber stamp: the user sees a summary,
presses Enter out of habit, and regrets it when one of the N was wrong. ws adopts the
`git add -p` model: **plan, present, confirm each, execute.** The command builds a plan
of actions, each with a description and an effect. Each action is offered on its own
(`y/n/a/q`, §4.7), and every outcome (done, skipped, failed) is recorded and appears in
`--json`.

Why per action:

- **Precision.** `ws repo git pull` across 12 repos lets you skip the one with
  uncommitted changes.
- **Partial success.** If action 3 of 5 fails, actions 1 and 2 are already done and 4 and
  5 are still offered. No all-or-nothing rollback is needed.
- **Muscle memory.** The `y/n/a/q` vocabulary is `git add -p`'s.

The granularity rule: **one action per independently meaningful mutation.** The test is
"would a user ever say yes to this and no to the next?" If so, they are separate actions.
Read commands never prompt, and there is no `--force`.

#### XI. Pin the clipboard

*The clipboard forgets. Your workspace shouldn't.*

Valuable information flows past you constantly: a chat message with a procedure, a wiki
diagram, a colleague's tip about a config flag, an error message that took hours to
reproduce. You recognise its value in the moment, then copy something else and it is gone.
The clipboard is a single-slot buffer and the choke point of every digital knowledge
transfer. The failure is not recognising value. It is failing to persist it before the
next copy.

So the fix is a habit: the moment something is worth keeping, paste it into the workspace
(a daily note in `notes/`, a scratch file, a screenshot in the artifacts folder). Factor II
applies: the closest relevant file is the right file.

- **The content already exists.** The clipboard has it, a URL points to it, a file holds
  it. Grab it from the source instead of retyping.
- **Persisting must be instantaneous.** If saving means leaving your context or deciding
  where it belongs, it won't happen.
- **Images are first-class.** Screenshots, diagrams and UI walkthroughs are valid records.
  Name or place them so they can be found again.

This is not a note-taking system. It is a net for knowledge that would otherwise be lost to
clipboard amnesia.

#### XII. Sync is one-way

*The machine you work on is the truth. The cloud copy is a mirror.*

`~/Workspace` on the machine you work on is the only source of truth. Sync goes one way,
laptop to cloud. The cloud copy is a backup: you never edit it. The only reverse direction
is a deliberate restore onto a new or empty machine (§2.5), not continuous two-way sync.

Two-way sync needs conflict detection that the clients and backends can't give reliably.
rclone's Zoho WorkDrive backend has no modtimes or hashes, and the WorkDrive client
(TrueSync) maps the whole account, not one folder. When conflict detection fails, a silent overwrite
loses work. One-way keeps the copy predictable and lets ws own exactly what goes out: its
ignore rules decide (Factor VIII), and symlinks never sync (`ws ignore lint` warns about
them).

`ws sync push` mirrors `~/Workspace` into a folder the sync client watches (for example
`~/ZohoWorkDrive/My Folders/Workspace`), and `ws sync restore` is the reverse, for a fresh
machine (§5 `ws sync`).

---

## 2. Workspace and state

### 2.1 Where things live

| What | Where | Synced |
| --- | --- | --- |
| Workspace root | `--workspace` → `$WS_WORKSPACE` → `~/Workspace` | — |
| State file | `<workspace>/ws.json` | yes |
| Sync rules | `<workspace>/.megaignore`, generated from `ws.json` `ignore_rules` (MEGA's format, §2.6) | yes |
| Symlink originals | `<workspace>/symlinks/home/<path under $HOME>` and `<workspace>/symlinks/system/<absolute path>`, or the workspace path you gave as the second argument of `ws symlink add` | yes |
| Scratch directories | `scratch.root_dir` (default `~/Scratch`), outside the workspace | no |
| Machine-local state | `$XDG_STATE_HOME/ws-tool/` (default `~/.local/state/ws-tool/`): cron run state and logs, trash integration state, `ws sync` state (`sync/`) | no |
| Trash | `$XDG_DATA_HOME/Trash` (default `~/.local/share/Trash`), shared with the desktop file manager | no |
| Built-in skills | `$XDG_DATA_HOME/ws/skills/<name>/` (default `~/.local/share/ws/skills`), extracted from the binary, read-only | no |
| Pass store | `$PASSWORD_STORE_DIR` → `~/.password-store` | via its own git remote |

No other config file exists. The workspace root is never stored in a file: it comes from
the flag, the environment, or the default.

### 2.2 Layout created by `ws init`

```text
<workspace>/
├── ws.json              # ws state (see §2.3)
├── .megaignore          # generated sync rules (MEGA's format)
├── notes/
├── experiments/
├── artifacts/
├── references/
├── scripts/
│   └── lib/
├── agent/
│   └── skills/          # your skills: one folder each, with a SKILL.md; `ws skill install` links them
├── configs/
├── symlinks/
│   ├── home/            # originals of paths under $HOME, at the same relative path
│   └── system/          # originals of other paths (/etc/hosts is system/etc/hosts)
├── data/
└── archive/
```

`ws init` is idempotent. It creates what is missing, fills in any keys missing from an
existing `ws.json` (keeping its values), and leaves every other existing file alone. The
pass store is not part of the layout: it syncs through its own git remote (see
`ws pass`, "A second copy in the workspace").

`ws layout` prints this list with each folder's purpose, and the homes outside the workspace
(temp, cache, session, scratch) as this machine resolves them. The purposes live in one Go
table that `ws init` creates from, so skills and scripts ask ws instead of copying the list.
`ws env` prints the homes as `export` lines that `scripts/lib/common.sh` evals (§5 Workspace
commands).

`symlinks/**` is the one built-in included directory (§5 `ws ignore`): the mirrored tree
keeps host names such as `.gitconfig`, `.ssh/` and `.config/`, and the built-in `-:.*`
exclude would drop all of them.

### 2.3 `ws.json`

ws reads and writes `ws.json`. You can edit it by hand (`ws config edit`). Relative paths
inside it resolve against `<workspace>` (config values have no current directory, see §4.5).

**`ws.json` always holds every key.** `ws init` writes the complete file, with every
setting at its default or empty value, so the file documents itself. Every write
writes every key, in a stable order. Nothing is left out because it is empty or still
the default. A file missing keys (written by an older ws, or trimmed by hand) still loads,
with defaults filling the gaps, and the next write fills them in. Hand edits must be
valid JSON. `ws config edit` checks before saving.

```json
{
  "schema": 1,
  "editor": "",
  "scratch": { "root_dir": "~/Scratch", "tags": [] },
  "repo": { "roots": ["."], "exclude_dirs": ["node_modules", ".venv"], "max_parallel": 8 },
  "secret": { "allowlist": [], "skip_dirs": [] },
  "pass": { "pass_entry": "" },
  "ignore": { "warn_size_mb": 5, "crit_size_mb": 10, "max_depth": 6, "muted": [] },
  "ignore_rules": { "schema": 1, "exclude": [], "include": [], "suppress_defaults": [] },
  "trash": { "warn_size_mb": 10240 },
  "sync": { "mirror": { "target": "" }, "exclude": [], "max_delete_pct": 20 },
  "symlinks": []
}
```

| Key | Meaning | Written by |
| --- | --- | --- |
| `editor` | the editor every `edit` and `open` command uses; empty means `$VISUAL`, then `$EDITOR`, then `vim` (§4.10) | hand |
| `scratch.root_dir` | where scratch dirs live | hand |
| `scratch.tags` | tags used so far, offered as completions by `scratch tag` | `scratch tag` |
| `repo.roots` | directories searched for git repos | `repo root add\|rm` |
| `repo.exclude_dirs` | directories never searched | hand |
| `repo.max_parallel` | reserved: fleet commands run one repo at a time | hand |
| `secret.allowlist` | findings marked as false positives (`file:line` anchors) | `secret allow`, `secret store` |
| `secret.skip_dirs` | directories secret scanning skips (muted) | `secret mute\|unmute` |
| `pass.pass_entry` | the pass entry with the API token for the pass backup's visibility check | `pass backup enable` |
| `ignore.*` | thresholds for sync warnings | hand |
| `ignore_rules.exclude` | paths ignored by sync (user rules, on top of the built-in template) | `ignore add\|rm` |
| `ignore.muted` | paths whose sync warnings are hidden; they keep syncing | `ignore mute\|unmute` |
| `ignore_rules.include` | included directories (`<dir>/**`) | `ignore edit` |
| `trash.warn_size_mb` | trash size in MB above which `trash status` warns (default 10240, i.e. 10 GiB) | hand |
| `sync.mirror.target` | the mirror folder `ws sync push` fills (a folder the sync client watches); no default, and `ws sync` refuses while it is empty | hand |
| `sync.exclude` | extra paths kept out of the mirror only, same pattern syntax as `ignore_rules.exclude`; empty by default | hand |
| `sync.max_delete_pct` | delete guard: a push that would delete more than this percentage of the mirror's files (and more than 10 files) asks first (default 20) | hand |
| `symlinks[]` | managed symlinks: `system` (the host path, written with `~` for `$HOME`), `original` (workspace-relative), `dir`, `sudo` | `symlink add\|link\|unlink` |

To use a GUI editor such as `code`, set `editor`.

**Unknown keys.** A `ws.json` with a key ws does not know, such as `links`, is rejected
with an `unknown field` error. An absolute `system` path still loads; ws writes `~` for
paths inside `$HOME`.

### 2.4 Resolution order

```text
flag  →  environment variable  →  ws.json  →  built-in default
```

The workspace root resolves first, then the state file inside it, then every other value.

---

### 2.5 Restoring a machine

On a new machine, or after losing one:

1. Install the sync client, sign in, and sync `~/Workspace` (§2.6). With a mirror client
   such as WorkDrive, the mirror folder arrives instead.
2. Install `ws` (§9.2).
3. Mirror clients only: `ws sync restore <mirror-dir>` copies the mirror into `~/Workspace`
   (§5 `ws sync`).
4. Put it back together:

```bash
ws init              # adds anything missing, including ws.json keys; keeps your values
ws symlink link      # every symlink, from the workspace copies
ws trash enable      # soft-delete for rm, VS Code, the file explorer
ws credential enable # git reads tokens from pass
ws cron apply        # your declared cron jobs
ws completion enable # shell completion
```

Your SSH config, shell setup, Kubernetes context and editor settings are back where they
belong. Machine setup stays separate from `ws init` on purpose: `init` only makes the
workspace a ws workspace, and each integration is a command you run once per machine.

Steps 1 and 3 are the only time data flows from the cloud copy to a machine (Factor XII). Once
the machine is set up, it pushes; it does not pull.

### 2.6 Sync clients

ws does not sync. A sync client keeps `<workspace>` identical on every machine, and ws
works with whichever you use: MEGA, Zoho WorkDrive, or any folder-sync tool that syncs a
directory tree.

What ws needs from the client: that `ws.json`, the symlink originals and the rest of the
workspace reach the other machines. What ws does for you depends on the client:

| | MEGA | Zoho WorkDrive, other clients |
| --- | --- | --- |
| Exclusion rules | ws generates `<workspace>/.megaignore` (§5 `ws ignore`), which MEGA reads | WorkDrive does not read `.megaignore`, and its app (TrueSync) mounts the whole account as one drive, with no folder of yours to sync. `ws sync push` copies the workspace one way into `sync.mirror.target` inside that folder, applying ws's rules while it copies (§5 `ws sync`). Other clients: configure exclusions in the client, or use `ws sync` the same way |
| `ws ignore ls`, `check`, `tree`, `lint` | evaluate the rules ws holds in `ws.json` | the same: they report what ws's rules would exclude, whichever client runs |
| `mega-sync` cron job | restarts MEGAsync for a 5-minute window every 30 minutes (§5 `ws cron`) | not needed when the client syncs by itself. Install `pass-backup` alone |
| `sync-push` cron job | not needed | runs `ws sync push` every 30 minutes, for a mirror client such as WorkDrive |

With a client other than MEGA, `ws ignore lint` still finds files that should not sync
(bloat, excessive depth, build output), and `ws ignore ls` shows the rules to copy into the
client's own exclusion settings.

Whatever the client, run it one way: workspace to cloud (Factor XII). Avoid two-way modes
such as `rclone bisync`.

---

## 3. Command grammar

### 3.1 Rules

1. **One shape.** `ws <noun> <verb> [targets…] [flags]`. Nouns are singular. Two nouns
   have sub-nouns: `repo root` and `backup` (under `pass`).
2. **One noun per TUI screen.** `ws <noun> ls` prints exactly the rows of that screen, with
   the same fields as its `--json` records. The Ignore screen also has a view for
   `ignore lint`, and opens on it.
3. **One meaning per verb.** The glossary (§3.3) is closed. A new command uses a glossary
   verb, or a domain word that its users already know (`git` for git command lines, the
   trash's `restore`). It never reuses a verb with a different meaning.
4. **No umbrella verbs.** There is no `scan`, `fix`, `reset` or `setup`. A command says what
   it changes: `link`, `allow`, `mute`, `unlink --all`.
5. **Targets are arguments.** Paths, names or `file:line` anchors. Without targets, a command
   acts on every row (in the TUI: the marked rows, else the filtered rows), except where its
   section says a target is required.
6. **Old names keep working.** Renamed commands remain as hidden aliases (§3.5). They are
   absent from help, completion and docs. Typed by a person, they print one line on
   stderr: `ws: "trash purge" is now "trash prune"`. Called by other programs (crontab
   lines, git, shell integrations), they stay silent.

### 3.2 Nouns

| Noun | What it manages | TUI screen |
| --- | --- | --- |
| `repo` | git repositories found under `repo.roots` | Repos |
| `symlink` | host files and folders whose originals live in the workspace, symlinked back | Symlinks |
| `skill` | agent skills, built into ws or yours in `agent/skills/`, linked into the coding tools on this machine | Skills |
| `scratch` | throwaway directories under `scratch.root_dir` | Scratch |
| `session` | terminal sessions and their recordings, owned by agent-tmux: `ws session …` is `agent-tmux …` | Sessions |
| `ignore` | what the sync client syncs: ignore rules, muted directories, sync warnings | Ignore |
| `secret` | secret findings in workspace files | Secrets |
| `pass` | the pass(1) password store | Secrets (header, `:pass …`) |
| `credential` | ws as git's credential helper, backed by pass | Secrets (header, `:credential …`) |
| `cron` | ws-managed cron jobs | Cron |
| `sync` | the one-way mirror of the workspace for folder-sync clients (`sync.mirror.target`) | — (CLI and cron only) |
| `trash` | the XDG trash and the soft-delete integrations | Trash |
| `config` | `ws.json` | `:config …` |
| `completion` | shell completion | — |

Workspace commands without a noun: `ws init`, `ws layout`, `ws env`, `ws tui`, `ws version`, `ws help`. Bare `ws`
prints help.

### 3.3 Verb glossary

| Verb | Means | Used by |
| --- | --- | --- |
| `ls` | list the noun's rows | every noun |
| `show` | one row in full | repo, secret, config, skill |
| `add` | put an existing thing under ws management | symlink, scratch, ignore, cron, repo root |
| `new` | create a fresh thing | scratch |
| `rm` | delete something ws owns, soft-deleting files | scratch, trash, cron, ignore, repo root |
| `edit` | open a file in your editor (§4.10) | ignore, config |
| `open` | open a directory in your editor (§4.10) | scratch |
| `enable` / `disable` | switch an integration on or off | trash, credential, completion, pass backup |
| `status` | the health of an integration (not a list of rows) | trash, credential, pass, backup, sync |
| `mute` / `unmute` | hide / show warnings for a path and everything under it; nothing else changes | ignore, secret |
| `prune` | delete rows by age (`--older-than`) | trash |
| `run` | execute now | repo (a command in each repo), cron (a job) |
| `git` | run a git command line, as typed, in each repo | repo |
| `push` | git push, nothing else; for `sync`, copy the workspace into the mirror | pass backup, sync |
| `lint` | report what isn't hygienic | ignore |
| `configure` | ask for what a setup is missing, then set it | repo |
| `apply` | write the declared state into the live system | cron |
| `link`, `unlink` | point a host path at its workspace copy (the workspace copy wins; also repairs); put the real content back and stop managing it | symlink (`add` is its other direction: the live file wins) |
| `tag` | label a row | scratch |
| `check`, `tree` | will this path sync? (skill: what drifted from this ws); browse the tree with sync status | ignore, skill (`check`) |
| `allow`, `store` | mark a finding as a false positive; copy its value into pass | secret |
| `init`, `audit` | create; inspect entries for problems | pass (and `ws init`) |
| `restore`, `empty` | put a trashed item back (sync: copy the mirror into the workspace); delete everything in the trash | trash, sync (`restore`) |
| `log` | recent output of a job | cron |
| `scan`, `install`, `uninstall` | which skills each tool has; link a skill into one tool; remove that link | skill |
| `eject` | copy a built-in into the workspace; from then on it is yours | skill |

`completion bash|zsh|fish` names a shell, not a verb. It is the one exception to the
glossary. `ws session` has no verbs of its own: the words after it are agent-tmux's
(`ls`, `create`, `show`, `grep`, …), and agent-tmux's own grammar governs them.

### 3.4 Every command

RO commands never change anything. RW commands build an action plan (§4.7).

```text
ws repo ls [repo…]                        RO  repos with status and hygiene findings
ws repo show <repo>                       RO  one repo in full
ws repo git [repo…] -- <git args…>        RW  run a git command in each repo
ws repo run [repo…] -- <cmd…>             RW  run any command in each repo
ws repo configure [repo…]                 RW  ask for missing identity, upstream, default branch, credentials
ws repo root add <dir>                    RW  add a discovery root
ws repo root ls                           RO  list discovery roots
ws repo root rm <dir>                     RW  remove a discovery root

ws symlink ls                             RO  managed symlinks, problems, and what was found
ws symlink add                            RW  checklist of known configs found on this machine (needs a terminal)
ws symlink add <host-path> [<ws-path>]    RW  move into the workspace and link back; <ws-path> picks where
ws symlink add --app <name>               RW  everything found for one app, without asking
ws symlink add <folder> [--dir|--files]   RW  a folder as one entry, or one entry per file
ws symlink link [<host-path>…]            RW  make or repair links from the workspace copies
ws symlink unlink <host-path…> | --all    RW  put the real content back, stop managing it

ws skill scan [--tool <name>]             RO  both shelves, built-in and yours, against every tool
ws skill show <skill> [--path]            RO  one skill: shelf, version, folder, tools
ws skill install [<skill>…] [--tool <t>]  RW  link skills into one tool; no names: a picklist
ws skill install --dev[=<dir>] [<skill>…] RW  point built-ins at a checkout, so edits are live
ws skill install --providers [--tool <t>] RW  copy the skills that tool binaries ship (agent-tmux) into agent/skills/
ws skill uninstall [<skill>…] [--tool <t>] RW  remove skill links from one tool; no names: a picklist
ws skill eject <skill>                    RW  copy a built-in into agent/skills/ to make it yours
ws skill check                            RO  skills and scripts that drifted from this ws

ws scratch ls                             RO  scratch directories
ws scratch new [name]                     RW  create one and open it
ws scratch add <path…>                    RW  move an existing path into scratch
ws scratch open <name>                    RO  open one in the editor
ws scratch tag <name> [tag…] [--rm tag]   RW  label one (tags are for you to read)
ws scratch rm <name…>                     RW  soft-delete

ws session <agent-tmux args…>             —   agent-tmux, run with the same arguments (exec)

ws ignore ls [--muted]                    RO  every path that doesn't sync, and its rule; --muted: muted warnings
ws ignore lint [path…]                    RO  check synced files and warn about the unhygienic ones
ws ignore add <path…>                     RW  stop syncing a path
ws ignore rm <path…>                      RW  sync a path again
ws ignore mute <path…>                    RW  hide a path's warnings (it keeps syncing)
ws ignore unmute <path…> | --stale         RW  show them again; --stale: drop muted paths that no longer exist
ws ignore check <path>                    RO  will this path sync, and which rule decides?
ws ignore tree [dir] [-L n]               RO  browse the tree with sync status
ws ignore edit                            RW  edit the rules in your editor

ws secret ls                              RO  secret findings
ws secret show <file:line>                RO  a finding in context
ws secret allow <file:line…>              RW  mark findings as false positives
ws secret mute <dir…>                     RW  stop scanning a directory
ws secret unmute <dir…>                   RW  scan it again
ws secret store <file:line> [entry]       RW  copy the value into pass and allow the finding

ws pass init                              RW  create the pass store (GPG key, git)
ws pass status                            RO  store, git, remote and credential-helper health
ws pass audit                             RO  entries with problems
ws pass backup enable [--remote <url>]    RW  set the private remote
ws pass backup disable                    RW  remove the remote
ws pass backup status                     RO  remote, ahead/behind, recent commits
ws pass backup push                       RW  push the store

ws credential enable                      RW  make git use ws (pass) for credentials
ws credential disable                     RW  stop it
ws credential status                      RO  helper config, pass entries, remote coverage

ws cron ls                                RO  jobs: schedule, installed, last run, exit, next run
ws cron add <job…>                        RW  install jobs
ws cron rm <job…>                         RW  uninstall them
ws cron run <job> [--if-due]              RW  run a job now (crontab lines add --if-due)
ws cron log [job…] [--lines n]            RO  recent output of jobs (all when none named)
ws cron apply                             RW  install configs/crontab into the live crontab

ws trash ls                               RO  trashed items
ws trash restore <item…>                  RW  put items back where they were
ws trash rm <item…>                       RW  delete items forever
ws trash empty                            RW  delete everything forever
ws trash prune --older-than <age>         RW  delete items trashed before then
ws trash enable [flags]                   RW  make shell rm, VS Code and the file explorer soft-delete
ws trash disable                          RW  undo that
ws trash status                           RO  integrations and trash size

ws sync push                              RW  mirror the workspace one way into sync.mirror.target
ws sync status                            RO  target, last push, stamp, pending changes and deletions
ws sync restore [<mirror-dir>]            RW  copy the mirror into the workspace (fresh machine)

ws config show [--defaults]               RO  resolved config, or the built-in defaults
ws config edit                            RW  edit ws.json in your editor

ws completion bash|zsh|fish               RO  print a completion script
ws completion enable [--shell s]          RW  load completion from your shell rc file
ws completion disable [--shell s]         RW  remove it

ws init                                   RW  create the workspace layout
ws layout                                 RO  workspace folders and the homes outside it, with their purpose
ws env                                    RO  export lines for WS_ROOT, WS_TMP, WS_CACHE, WS_RUN, WS_SCRATCH
ws tui                                    RO  the full-screen interface
ws version                                RO  version, schema and platform
ws help [noun [verb…]]                    RO  help
```

### 3.5 Renamed and removed commands

Every old name below still works as a hidden alias (§3.1 rule 6), except the interactive
`ignore fix` and `secret fix`, and the git verbs `repo fetch`, `pull`, `push`, `save` and
`sync`. Those have no single successor: typed, they print the commands that replace them
and exit 1. (decision: `ws repo git pull` is plain git, with no fetch first and no
rebase-or-merge question, so an alias would promise the old behaviour.)

| Old | New |
| --- | --- |
| `repo scan` | `repo ls` (it no longer fetches; `ws repo git fetch` does) |
| `repo add-root`, `repo ls-roots` | `repo root add`, `repo root ls` |
| `scratch search` | `scratch ls --filter` (names and tags) |
| `ignore scan` | `ignore lint` |
| `ignore scan --expand-suppressed` | `ignore ls --muted` |
| `repo doctor` | `repo ls` |
| `config --defaults` | `config show --defaults` |
| `repo fetch`, `repo pull`, `repo push` | `repo git fetch`, `repo git pull`, `repo git push` |
| `repo save`, `repo sync` | `repo git commit -am <msg>`, then `repo git pull`, `repo git push` |
| `ignore fix` | `ignore add`, `ignore mute`, `scratch add`, or shell `rm` |
| `secret scan` | `secret ls` |
| `secret scan --pass` | `pass audit` |
| `secret fix` | `secret allow`, `ignore add`, `secret mute`, `secret store` |
| `secret setup`, `secret status` | `pass init`, `pass status` |
| `secret git remote\|status\|log\|push` | `pass backup enable --remote\|status\|status\|push` |
| `git-credential-helper setup\|status\|disconnect`, `credential …` | `credential enable\|status\|disable` |
| `cron list`, `cron remove`, `cron status` | `cron ls`, `cron rm`, `cron ls` |
| `trash setup` | `trash enable` |
| `trash purge --older-than <days>` | `trash prune --older-than <age>` |
| `config view`, `config defaults` | `config show`, `config show --defaults` |
| `completions …`, `install`, `uninstall` | `completion …`, `enable`, `disable` |

Plumbing that other programs call keeps its name and stays out of help:
`ws git-credential-helper get|store|erase` (git), `ws cron run <job>` (crontab), and the
shell integration hooks. Old names that existing rc files and crontab lines contain are
silent aliases: `ws completions bash|zsh|fish` (the `eval` line that
`completions install` wrote runs on every shell start) and the old cron job names.

Built-in cron jobs: `secret-scan`, `ignore-scan`, `repo-sync` and `zoho-sync` are retired: nothing
commits your work unattended, and Zoho's TrueSync client needs no restart window. `ws cron run` of one exits 0 silently and does nothing, so a
crontab line written earlier keeps working; `ws cron rm` removes such a line, and
`ws cron add` is an error that says the job was removed.

`ws session record`, `stop`, `ls`, `show`, `grep` and `rm` are gone with ws's own recorder:
`ws session` now runs agent-tmux, whose verbs differ.
The `session` object in `ws.json` (`root_dir`, `max_session_mb`) is no longer read. Delete it
by hand: `config edit` rejects it as an unknown key, and the next write drops it. Old
recordings stay where they were; nothing moves or deletes them.

Removed, no replacement: `ws reset`, `ws restore`, `ws capture`, `ws search`, `ws scratch grep`,
`ws ignore generate`, `ws symlink migrate`, `ws symlink edit`, `ws symlink backup` and
`ws symlink git` (the symlink git backup, with its auto-commit and auto-push), the `--manifest` global flag, `--class` and `--to` (typed, they are an error
that says to pass the workspace path as a second argument), and exit code `2`.

---

## 4. CLI conventions

### 4.1 Global flags

```text
-w, --workspace <dir>   workspace root (default: $WS_WORKSPACE, then ~/Workspace)
-q, --quiet             errors only; write commands accept every action
    --verbose           internal decisions on stderr ([verbose] lines)
    --json              JSON on stdout; write commands accept every action
    --dry-run           print the plan, change nothing (ignored by read commands)
    --no-color          no ANSI colors or Unicode icons (also NO_COLOR, TERM=dumb, non-TTY)
-h, --help              help for the command
```

Every subcommand accepts the global flags anywhere on the line (`ws cron ls --json`).

### 4.2 Help

- `ws <noun> --help` (or `ws help <noun>`) lists the noun's verbs, one line each, then the
  flags shared by them.
- `ws <noun> <verb> --help` prints that command's usage, its arguments, its own flags, and,
  for `ls`, the field names that `--filter`, `--sort` and `--wide` accept.
- Help never mentions hidden aliases.

### 4.3 Lists

Every `ls` prints the rows described by its noun's field list (`internal/<domain>/fields.go`).
The TUI screen for that noun renders the same fields.

| Flag | Meaning |
| --- | --- |
| `--filter <expr>` | keep rows where `key=text` (the field contains text; a field with a fixed set of values, such as `status`, must equal it), `key!=text` (it does not), or bare `text` (any field contains it). Case-insensitive. Repeat it, and all must match. The TUI's `/` takes the same expressions. |
| `--sort <field[,field]>` | order rows; `-field` descends |
| `--wide` | show every field as a column |
| `--json` | one record per row, keyed by field name, with every field |

An unknown field is an error that names the valid ones. Listing is not a verdict: `ls`
exits 0 whether or not the rows show problems.

### 4.4 JSON

Every `--json` response has an envelope:

```json
{ "ws_version": "0.20.0", "schema": 3, "command": "symlink.link", "data": { } }
```

`command` is `<noun>.<verb>` (`repo.root.add`, `pass.backup.push`). The workspace
commands without a noun use their name: `init`, `version`. Write commands
include `data.actions`, the status of each plan action. `schema` increments on breaking
changes to `data`.

History: `1` initial. `2` every path field is absolute. `3` command names follow the
grammar in Part 3.

### 4.5 Paths

These rules apply to every command and override any example in this document.

**Output: every path ws prints is absolute and cleaned.** That covers text, JSON, plan
lines, action IDs, prompts and errors.

```text
/home/user/Workspace/data/bruno          ✔
data/bruno, ~/Workspace/data/bruno, ./x  ✗
```

- One form, whatever the origin: workspace items, system paths, scratch, trash, config.
- One line per item, because an absolute path is already `cd`-ready.
- A location is `/abs/path/file:42`.
- Cleaned (`filepath.Clean`), not resolved: symlinks are shown as the path ws acted on.
- Everything goes through one helper (`style.AbsPath`). Internal data may stay
  workspace-relative for rule matching, and is converted when rendered.
- Not paths, printed verbatim: rule patterns (`node_modules/`, `dir/**`), config keys, git
  refs and URLs, pass entry names, and names of ws-managed items (scratch names, cron
  jobs). When a command acts on the file behind a name, it prints the file's
  absolute path too.
- `ws ignore tree` prints its absolute root once, as the header, and tree nodes below it.
- The TUI may shorten paths to fit a column (`~/.bashrc`). The `d` describe pane and `y`
  JSON show them in full.

**Input: every path argument resolves the same way.** Absolute is used as is. `~` expands
to `$HOME`. Anything else is relative to the **current directory**, never to the workspace.
Then the path is cleaned.

- A path that resolves to nothing fails, and the error shows the absolute path it tried.
- Matching is absolute to absolute. Prefix filters (`--path`) match whole components:
  `--path ~/Workspace/da` does not match `~/Workspace/data`.
- Round trip: any path copied from ws output is valid input to any ws command, from any
  directory.
- Tab completion offers absolute paths for ws-known items (repos, symlinks), and the
  shell's own file completion elsewhere.

**Config values are the exception.** `ws.json` has no current directory, so relative paths
in it resolve against `<workspace>`. When a command writes CLI input into config
(`repo root add`, `secret mute`, `ignore add`), it resolves the input by the rules above,
then stores it workspace-relative if it is inside the workspace, absolute otherwise.

### 4.6 Exit codes

| Code | Meaning |
| --- | --- |
| `0` | success, including "nothing to do" and every action skipped by choice |
| `1` | error: bad input, missing workspace, permission denied, every action failed |
| `3` | partial: some actions succeeded, some failed |
| `130` | a prompt was cancelled with Ctrl-C |

Code `2` ("violations found") is retired. Finding problems is not an error: `ls` shows
them, and the exit code stays 0.

### 4.7 Read and write commands

Every command is read-only (RO) or read-write (RW), as listed in §3.4. RO verbs are `ls`,
`show`, `status`, `check`, `tree`, `lint`, `grep`, `log`, `audit`, and `open` (it only
launches an editor). Everything else is RW.

**RO commands** never change the workspace, `ws.json`, or the system. They never prompt,
except to ask for a missing required name on a terminal (`scratch open`). Their output is
deterministic, and they are safe in pipes (`ws repo ls --json | jq`).

**RW commands** use the action plan:

- Before changing anything, the command builds a plan of discrete actions: **one per
  independently meaningful mutation** (one per file, repo, job). The test: would
  someone want to accept this one but skip the next? If so, they are separate actions.
- Each action is confirmed on its own: `y` (default, Enter) do it, `n` skip it, `a` accept
  all the rest, `q` skip all the rest. EOF on stdin counts as `y`.
- `--dry-run` prints the plan and stops. `--quiet` and `--json` accept every action.
  The exception is `ws sync`: its safety questions (the delete guard, a foreign stamp,
  a restore over an existing workspace) are refused under them, so cron never forces one.
- A failed action never stops the others. The exit code is 0, 1 or 3 by outcome (§4.6),
  and `--json` includes every action's status.
- Destructive plans (`symlink unlink --all`, `trash empty`) start with every action
  unchecked in the TUI, and the CLI asks per action as usual.

Exemptions, which run without a plan:

- `repo git` and `repo run` on one repo at a terminal, which hand the terminal to the
  command, so the line you typed is the confirmation;
- `cron run`, which crontab lines call, so the command itself is the confirmation;
- the `edit` commands (`ignore edit`, `config edit`), where the editor is the
  confirmation. `ignore edit` and `config edit` validate before they save.

### 4.8 Completion

Shell completion (`ws completion …`) offers exactly the commands, flags and dynamic
targets this spec documents: repo paths, symlink host paths, scratch names, cron jobs,
trash items, `file:line` findings. It never offers hidden aliases. Tests keep completion
and help in step (§9.7). `ws session <words>` is completed by `agent-tmux __complete
<words>`: its candidates (`value` or `value<TAB>description` lines) become ws's, values
only. Without agent-tmux there are none.

### 4.9 Environment variables

| Variable | Effect |
| --- | --- |
| `WS_WORKSPACE` | workspace root (after `--workspace`) |
| `NO_COLOR` | no colors or icons ([no-color.org](https://no-color.org)) |
| `VISUAL`, `EDITOR` | your editor, when `ws.json` `editor` is empty (§4.10) |
| `PASSWORD_STORE_DIR` | pass store location |
| `XDG_STATE_HOME`, `XDG_DATA_HOME` | machine-local state and trash locations |
| `TMPDIR`, `XDG_CACHE_HOME`, `XDG_RUNTIME_DIR` | `ws env` and `ws layout`: the temp, cache and session homes; `XDG_CACHE_HOME`: the agent-tmux release cache |
| `WS_NO_VERSION_CHECK` | `1`: no agent-tmux version nudge (the TUI and `ws version` never ask GitHub) |
| `WS_NO_MOUSE` | TUI: leave the mouse to the terminal |
| `WS_NO_EMOJI` | TUI: no emoji |
| `WS_CRONTAB_FILE` | tests: a file that stands in for the user's crontab |
| `WS_SYMLINK_ROOT` | tests: a directory put in front of the catalog's `/etc` paths, so detection never reads the real ones |

### 4.10 Editor

Every command that opens an editor (`ignore edit`, `config edit`,
`scratch new`, `scratch open`, and `e` in the TUI) uses the same one:

1. `ws.json` `editor`, when it is set (it may carry arguments: `code -w`, `nvim -p`);
2. else `$VISUAL`;
3. else `$EDITOR`;
4. else **`vim`**.

A terminal editor (`vim`, `nvim`, `vi`, `nano`, `emacs -nw`, `hx`, …) takes over the
terminal until it exits. In the TUI, the TUI suspends while it runs. Any other editor (a
GUI such as `code`) is started in the background. When the editor cannot start, ws says
which one it tried and prints the path. To use a GUI editor, set `editor` (for
example `"code"`).

---

## 5. Command reference

One chapter per noun, in the TUI's tab order. Each chapter opens with a table of its
commands, and each command lists what it does, its flags, output, JSON, exit codes and its TUI
key. Old command names are hidden aliases, listed in §3.5.

**(decision)** marks a choice this spec made where the answer was not obvious.
"(decision, by the user)" marks one the user made.

### `ws repo`

A repo is a git repository inside the workspace. ws does not keep a list of repos. Every
command discovers them again by walking the discovery roots in `ws.json` `repo.roots`. Each
row pairs the repo's git status (branch, dirty, ahead/behind its upstream) with its hygiene
findings. The Repos screen shows the same rows.

ws adds discovery, status, hygiene and fleet scope. The verbs are git's own: ws has no
`pull`, `push` or `save` of its own, and `ws repo git` runs any git command line in the
selected repos (decision, by the user: the frequent commands come forward through the
TUI's prompt history, not through dedicated verbs or keys).

| Command | RO/RW | Does | TUI |
| --- | --- | --- | --- |
| `ws repo ls [repo…]` | RO | List status and hygiene findings | screen |
| `ws repo show <repo>` | RO | Show one repo in full: status, upstream, every finding | `enter` |
| `ws repo git [repo…] -- <git args…>` | RW | Run a git command in each repo | `!`, `:git` |
| `ws repo run [repo…] -- <cmd…>` | RW | Run any command in each repo | `!` with `git` erased, `:run` |
| `ws repo configure [repo…]` | RW | Ask for whatever a repo's git setup is missing (identity, upstream, default branch, credentials) and set it | `c`, `:configure` |
| `ws repo root add <dir>` | RW | Add a discovery root | `:root add` |
| `ws repo root ls` | RO | List the discovery roots | `:root ls` |
| `ws repo root rm <dir>` | RW | Remove a discovery root | `:root rm` |

#### Selecting repos

These rules apply to every `ws repo` command that acts on repos.

**Discovery.** ws walks each root in `repo.roots`. A root is stored workspace-relative and
resolves against the workspace. Older entries may be absolute or start with `~`. An empty
list means the workspace itself (default `["."]`). Every directory that contains a `.git`
directory is a repo. Nested repos are listed separately. Repos outside the workspace are
never listed. Directories in `repo.exclude_dirs` are skipped, along with everything under
them. These are paths, either workspace-relative or absolute, and they are not matched by
name. Repos are listed by absolute path in path order.

**Targets.** Each `[repo…]` argument is a path: absolute, `~`, or relative to the current
directory, never to the workspace. ws cleans it and compares it to the discovered repo
paths. It must equal one of them exactly. A path inside a repo is not a target, and neither is a
directory that holds repos (use `--path` for that). ws checks every target before it does any
work. If one matches no repo, it prints `ws: no repo at <abs path>` and exits `1`. Repeated
targets count once, and output keeps path order. With no targets, the command covers every
discovered repo.

**Filter flags.** `ls`, `git`, `run` and `configure` take these. Each flag narrows the
targets further, and all of them must match. A target that a filter removes is left out
quietly. Filters read the repo state as of the last fetch (`ws repo git fetch` updates it).

| Flag | Default | Meaning |
| --- | --- | --- |
| `--path <dir>` | none | Only repos at or under `<dir>` (path rules; whole path components) |
| `--dirty` | off | Only repos with uncommitted changes (untracked files count) |
| `--ahead` | off | Only repos ahead of their upstream |
| `--behind` | off | Only repos behind their upstream |
| `--detached` | off | Only repos on a detached HEAD |

**Git.** Commands run one repo at a time. Every git command that ws itself runs (status,
hygiene, `configure`) uses `GIT_TERMINAL_PROMPT=0`, so git never asks for credentials. A
repo that needs them fails with git's own message. What `ws repo git` and `ws repo run`
run follows their own rules (see `ws repo git`).

**Row fields** (`internal/repo/fields.go`; the keys for `--sort`, `--filter`, `--wide` and
`--json`):

| Key | Value |
| --- | --- |
| `path` | absolute repo path |
| `state` | `error` (git status failed), `dirty`, `no upstream`, or `clean`, checked in that order |
| `ahead`, `behind` | commits ahead of / behind the upstream (0 without one) |
| `issues` | number of `warn` and `error` findings; `info` findings are not counted (decision) |
| `branch` | current branch; `(detached)` in text on a detached HEAD |
| `detached`, `dirty`, `has_upstream` | booleans |
| `findings` | the check ids of every finding (text: `check: detail; …`) |
| `error` | git's error text when status failed |

The default columns are `path state ahead behind issues branch`. `--wide` shows every field.

**Hygiene checks** (`internal/repo/doctor.go`). Dirty state is not a check. The `state`
field reports it.

| Check | Severity | Finding |
| --- | --- | --- |
| `identity` | warn | `user.name` or `user.email` is not set, locally or globally |
| `identity` | info | it is set only globally (the detail names the global value) |
| `upstream` | warn | the current branch has no tracking upstream |
| `default-branch` | info | `init.defaultBranch` is unset and `origin/HEAD` does not resolve |
| `fetch-staleness` | info | `.git/FETCH_HEAD` is missing or older than 14 days |
| `credentials` | info | the remote is HTTPS and git has no credential helper |

Every finding's detail ends with its fix: `ws repo configure <repo>`.

#### `ws repo ls`

```text
ws repo ls [repo…] [--check <id>]… [filter flags] [--sort f] [--filter expr] [--wide]
```

This lists the selected repos with their status and hygiene findings, one row per repo. It
reads only local state and touches no repo: the ahead and behind counts are as of the last
fetch (decision: fetching is `ws repo git fetch`, so `ls` never needs the network). With no
targets, it lists every discovered repo.

| Flag | Default | Meaning |
| --- | --- | --- |
| `--check <id>` | all | Run only this hygiene check (repeatable). An unknown id is an error that lists the valid ones. |
| filter flags | — | See "Selecting repos" |
| `--sort`, `--filter`, `--wide` | — | The shared list flags, over the row fields |

Output:

```text
$ ws repo ls
REPO                                     STATE        ↑  ↓  ISSUES  BRANCH
/home/user/Workspace/data/bruno          clean        0  1          master
/home/user/Workspace/experiments/blog    no upstream  0  0  1       main
/home/user/Workspace/notes/second-brain  dirty        2  0          main
```

If nothing matches, it prints `No repositories found.` In `--json` the command is `repo.ls`,
and `data` holds the records:
`{"statuses": [row…], "findings": [{"repo", "check", "severity", "detail"}…]}`.
`findings` includes `info` findings and is `[]` when there are none.

Exit: `0`, including when repos are dirty or have findings. `1` on a bad flag, an unknown
check, an unknown target, or no `git` in PATH.

TUI: the Repos screen is this list. `ctrl+r` reads status again, and so does the end of
every command run from the screen. The status line sums the fleet:
`4 repos · 1 dirty · 1 ahead · 1 behind · 1 issue`.

#### `ws repo show`

```text
ws repo show <repo>
```

This shows one repo in full. `<repo>` is required and follows the target rules. ws reads the
repo's status and runs every hygiene check on it. It never fetches.

Output:

```text
$ ws repo show /home/user/Workspace/experiments/blog
Path      /home/user/Workspace/experiments/blog
Branch    main
State     no upstream
Upstream  none
Hygiene
  warn  upstream        current branch has no tracking upstream
  info  default-branch  init.defaultBranch not set and origin/HEAD not resolved
```

`Upstream` is `↑<ahead> ↓<behind>` when the branch tracks one, else `none`. `Branch` reads
`detached HEAD` on a detached HEAD. An `Error` line appears only when git status failed.
With no findings, the section reads `Hygiene  none`. In `--json` the command is
`repo.show`, and `data` is `{"status": row, "findings": [finding…]}` (the same shapes as
`repo ls`).

Exit: `0`. `1` when `<repo>` is missing, more than one is given, or it is not a discovered
repo.

TUI: `enter` on a row opens this view as the detail level. `esc` goes back. In it, `!` and
`c` act on this one repo. `d` shows every field of the row, and `y` shows the row as JSON.

#### `ws repo configure`

```text
ws repo configure [repo…] [filter flags]
```

This fixes each selected repo's hygiene findings by asking you for what is missing,
then setting it. It needs a terminal. With no targets, it covers every discovered repo
that has a finding. ws asks only the questions a repo needs, repo by repo:

| Finding | Question | Sets |
| --- | --- | --- |
| `identity` (missing) | `Name:` and `Email:`, typed by you with nothing suggested; then `For all repos (global) or only this one? [g/l]`, where the default is global when no global identity exists | `user.name`, `user.email` (`--global` or in the repo) |
| `upstream` | if `origin` has the branch: `Track origin/main? [Y/n]`. If not: `Push main to origin and track it? [y/N]`. Without a remote: `Remote URL (blank to skip):` | `branch.<b>.remote`/`merge`, or `git push -u`, or `git remote add origin` |
| `default-branch` | `Default branch for new repos [main]:` | `init.defaultBranch` (global) |
| `credentials` | `Use ws (pass) as git's credential helper? [y/N]` | runs `ws credential enable` |
| `fetch-staleness` | `Fetch now? [Y/n]` | `git fetch` |

A global setting is asked once, and every repo then gets it. The answers become a plan,
one action per setting, which is confirmed as usual (`y/n/a/q`). A blank answer skips that
setting. `--dry-run` asks the questions and prints the plan. With `--quiet` or `--json`, or
without a terminal, it exits 1: `ws repo configure needs a terminal`. It has no JSON output.

Exit: `0`, `1`, `3`.

TUI: `c` hands the terminal to `ws repo configure` for the marked repos, else the cursor
repo (§6.8), and the screen refreshes afterwards.

#### `ws repo git`

```text
ws repo git [repo…] [-y] [filter flags] -- <git args…>
ws repo git [-y] [filter flags] <git args…>
```

This runs `git <git args…>` in each selected repo, with the repo root as its working
directory. ws adds nothing to the command: `ws repo git pull --rebase` is `git pull
--rebase`, in every repo. With no targets, it runs in every discovered repo.

**Arguments.** With `--`, the words before it are repo targets and ws flags, in any order,
and everything after it goes to git as typed (a second `--`, as in `log -- <path>`, stays
git's). Without `--`, ws's flags come first, and the first other word starts the git
command, so git keeps its own flags (`ws repo git log --oneline -3`) and there are no
targets (decision: the flag-first form is the short one for the whole fleet). A path is a
target only before `--`. Without git args, ws prints
`usage: ws repo git [repo…] -- <git args…>` and exits 1.

| Flag | Default | Meaning |
| --- | --- | --- |
| `-y`, `--yes` | off | run in every repo without asking |
| filter flags | — | See "Selecting repos" |

**One repo on a terminal.** When the targets and filters leave one repo, and stdin and
stdout are a terminal (not `--json`, `--quiet` or `--dry-run`), git runs on the terminal
as if you had typed it in the repo: its own output, pager, editor and prompts work, so
`commit`, `rebase -i` and `add -p` behave as usual. There is no plan and no prompt; the
line you typed is the confirmation. Exit: `0` when git exits 0, else `1`.

**Several repos, or no terminal.** The plan has one action per repo, `Run in <path>`,
confirmed as usual (`y/n/a/q`; `a` takes the rest, `-y` asks nothing). Each repo runs in
turn with stdin not connected and stdout and stderr captured together. git gets
`GIT_TERMINAL_PROMPT=0`, so it never asks for credentials, and `GIT_EDITOR=true`, so an
editor it would open accepts what is there: a merge or rebase goes on with its default
message, and `commit` without `-m` aborts on the empty message instead of waiting
(decision). A non-zero exit fails that action.

Output: after each action line, the command's output, indented (decision).

```text
$ ws repo git --dirty -y status --short
  ✔ Run in /home/user/Workspace/notes/second-brain
      M index.md
```

`--dry-run` prints the actions and runs nothing. In `--json` the command is `repo.git`, and
`data` is
`{"command": [arg…], "actions": [{"id", "status", "error"}…], "results": [{"path", "exit_code", "output"}…]}`.

Exit: `0`, `1` (every action failed, or a bad flag or target), or `3` (some failed).

TUI: `!` opens the command prompt pre-filled with `git `, for the marked repos, else every
repo the filter shows; in a repo's detail, for that repo. `:git <args…>` runs without the
prompt. The prompt:

- `↑` and `↓` step through the lines run from the screen before, the most recent first.
- Its suggestions start with the lines run before that begin with what is typed, the
  most run first, so `tab` completes the command you use most. Then come git subcommands
  after `git `, and after the subcommand the branches, tags and remotes of the repos it is
  for.
- The history is machine-local: `$XDG_STATE_HOME/ws-tool/repo-history.json`, at most 200
  lines, the least recently run dropped first.

The line runs with `sh -c` in each repo root. One repo gets the terminal (§6.8), and the
screen waits for `enter` before it redraws. Several repos go through the checklist, where
repos can be unticked; then the output view shows `✔` or `✘ (exit N)` and the output for
each repo that ran. `esc` goes back to the list, which is read again.

#### `ws repo run`

```text
ws repo run [repo…] [-y] [filter flags] -- <cmd> [arg…]
ws repo run [-y] [filter flags] <cmd> [arg…]
```

This is `ws repo git` for any command: the same arguments, rules and output, without the
`git` in front. The CLI runs the command directly, not through a shell. Use `sh -c '…'` for
pipes. Without a command, ws prints `usage: ws repo run [repo…] -- <cmd…>` and exits 1. In
`--json` the command is `repo.run`.

TUI: erase the `git ` in the `!` prompt, or type `:run <cmd…>` (bare `:run` opens the
prompt empty).

#### `ws repo root add`

```text
ws repo root add <dir>
```

This adds a discovery root. `<dir>` follows the path rules. It must be an existing
directory inside the workspace (decision: discovery never lists repos outside the
workspace, so an outside root would find nothing). If it is already a root (compared after
resolving), ws prints `already a repo root: <abs path>` and exits 1. The plan has one action,
`Add repo root <abs path>`, which appends the directory to `ws.json` `repo.roots`. The value
is stored workspace-relative, for example `experiments`.

Output: the prompt line. `--dry-run` prints the action. In `--json` the command is
`repo.root.add`, and `data` is `{"path": "<abs path>", "actions": […]}`.

Exit: `0`. `1` on a missing, non-directory, outside or duplicate path, or a failed write.

TUI: `:root add <dir>` opens the one-action checklist. Bare `:root add` asks for a directory,
with directory completion and a hint listing the current roots. Afterwards the list is
read again over the new roots.

#### `ws repo root ls`

```text
ws repo root ls
```

This prints the discovery roots that are in effect, one absolute path per line, in
`ws.json` order. When `repo.roots` is empty, it prints the workspace path, which is the
default.

Output: `/home/user/Workspace` for the default. In `--json` the command is `repo.root.ls`,
and `data` is `{"roots": ["<abs path>"…]}`.

Exit: `0`. `1` when `ws.json` cannot be read.

TUI: `:root ls` shows the roots in a read-only view. `esc` goes back.

#### `ws repo root rm`

```text
ws repo root rm <dir>
```

This removes a discovery root. `<dir>` follows the path rules. ws compares it to each
`repo.roots` entry after resolving that entry. If none matches, it prints
`not a repo root: <abs path>` and exits 1. The plan has one action,
`Remove repo root <abs path>`, which deletes every matching entry from `ws.json`. No
directory or repo on disk is touched. ws refuses to remove the last root with
`cannot remove the last repo root; add another first` and exits 1 (decision: an empty list
falls back to the whole workspace, so the rm would widen discovery instead of narrowing
it).

Output: the prompt line. `--dry-run` prints the action. In `--json` the command is
`repo.root.rm`, and `data` is `{"path": "<abs path>", "actions": […]}`.

Exit: `0`. `1` on an unknown root, the last root, or a failed write.

TUI: `:root rm <dir>` opens a one-action checklist. `:root rm` with no directory opens a
checklist of every root with none ticked (decision).

---

### `ws symlink`

A symlink entry is a host path, such as `/home/user/.bashrc`, `/home/user/.config/nvim` or
`/etc/hosts`, that is a symlink to a file or folder kept in the workspace. The rows are the
`symlinks[]` entries in `ws.json`, checked against the filesystem, plus the things ws finds
around them (below). The Symlinks screen shows them.

**Two directions.** When both a live file and a workspace copy exist, the verb decides which
one wins:

- `add`: the **live file** goes into the workspace. This starts managing a new path, or, for
  an entry that a tool replaced with a real file (`drifted`), takes the live file as the new
  workspace copy.
- `link`: the **workspace copy** goes out to the host path, as a symlink. This repairs
  missing and broken links, and puts every link back on a new machine.
- `unlink` reverses both: it puts the real content back and stops managing the path.

| Command | RO/RW | Does | TUI |
| --- | --- | --- | --- |
| `ws symlink ls` | RO | managed symlinks, problems, and what was found | screen |
| `ws symlink add` | RW | checklist of known configs found on this machine (needs a terminal) | `a` |
| `ws symlink add <host-path> [<ws-path>]` | RW | move into the workspace and link back; `<ws-path>` picks where | `:add` |
| `ws symlink add --app <name>` | RW | everything found for one app, without asking | |
| `ws symlink add <folder> [--dir\|--files]` | RW | a folder as one entry, or one entry per file | `:add … --dir` |
| `ws symlink link [<host-path>…]` | RW | make or repair links from the workspace copies | `l`, `:link` |
| `ws symlink unlink <host-path…> \| --all` | RW | put the real content back, stop managing it | `ctrl+d`, `:unlink --all` |

#### Terms

- **Entry.** One `symlinks[]` record: `system` (the host path; `~` stands for `$HOME` in the
  file), `original` (the workspace copy, workspace-relative), `dir` (the entry is one whole
  folder), `sudo` (the host side needs root). There is no class: where the copy lives says
  what it is.
- **Workspace copy.** The file or folder `original` names. The host path is a symlink to its
  absolute path.
- **Mirror path.** Where `add` puts a file when you give no second path. It is a pure
  function of the host path: a path inside `$HOME` goes to `symlinks/home/<path under $HOME>`,
  any other absolute path to `symlinks/system/<path without the leading slash>`. The path is
  cleaned first, a `..` in it is an error, and the reverse (`HostPath`) gives the host path
  back, spaces and all.

  | Host path | Workspace copy |
  | --- | --- |
  | `/home/user/.bashrc` | `/home/user/Workspace/symlinks/home/.bashrc` |
  | `/home/user/.config/Code/User/settings.json` | `/home/user/Workspace/symlinks/home/.config/Code/User/settings.json` |
  | `/home/user/.kube/config` | `/home/user/Workspace/symlinks/home/.kube/config` |
  | `/etc/docker/daemon.json` | `/home/user/Workspace/symlinks/system/etc/docker/daemon.json` |

- **Owned copy.** A workspace copy under `symlinks/` that no other entry points at. `add` makes these, and ws may move or replace them. Any other workspace copy
  is **shared**: one you chose with `add <host-path> <ws-path>`, such as `agent/AGENTS.md`,
  or a copy that two entries point at. ws never moves or deletes a shared copy.
- **Sudo entries.** `add` sets `sudo` when the host path, or its nearest existing parent, is
  owned by root. Every host-side change for such an entry (move, symlink, copy, remove,
  mkdir) runs through `sudo`, which may ask for a password on the terminal. Without a
  terminal (cron), the action fails unless sudo needs no password. `link` skips a sudo entry,
  with a message, when `sudo` is not installed. The TUI hands actions on sudo entries to the
  CLI in the terminal (§6.8), so sudo can prompt.
- **Targets.** Path arguments follow §4.5. `unlink` needs paths that match an entry's host
  path; a path that matches none is an error, and ws exits 1 before changing anything.
- **`.ws-backup`.** When ws must put a symlink where something else now stands, it first
  renames that thing to `<path>.ws-backup`, or `<path>.ws-backup.1`, `.2`, … (the first free
  name). It never overwrites an earlier backup and never deletes the live file.
- **No history of its own.** ws keeps no git backup of `symlinks/`. MEGA (or your sync
  client) is the protection. To keep history, run `git init` in `symlinks/`; `ws repo` will
  see it.

#### The catalog and the seven statuses

ws holds a catalog of about 140 well-known configs in the binary (`internal/symlink/catalog.go`,
the data in `docs/plans/0003/catalog.md`): the host path or glob, file or folder, a secret
flag, a sudo flag, the OS (`any`, `linux` or `mac`) and an app label. It is never written to
a file and you cannot edit it. It is a source of suggestions, not of state: every symlink you
have is in `ws.json`, whether it came from the catalog or from a path you typed.

ws checks the catalog silently and reports only what exists on this machine and is not
managed. Mixed folders (`~/.ssh`, `~/.claude`, `~/.config/Code`, `~/.gnupg`, `~/.docker`)
are never listed whole, because they hold known_hosts, history and credentials: the catalog
lists their safe children, and a test fails if a row names anything on the never-suggested
list (`docs/plans/0003/catalog.md`). A path on that list can still be added by hand: the list
governs suggestions, not what you may do. There is no heuristic scan: anything outside the
catalog goes through `add <path>`.

Every row of `ls` has one status. ws keeps no state between runs: it compares four things
each time, `ws.json`, the files under `symlinks/`, the catalog paths on the host, and (for
`ls` only) a bounded walk of `$HOME`.

| Status | Meaning | Command |
| --- | --- | --- |
| `ok` | in `ws.json`, linked, and the workspace copy exists | none |
| `found` | a catalog item exists here and is not managed | `add` |
| `untracked` | a host link points into the workspace but is not in `ws.json` | `add <host-path>` registers it, moves nothing |
| `orphan` | content sits under `symlinks/` with no entry in `ws.json` | `link <host-path>` |
| `missing` | in `ws.json`, nothing at the host path (a new machine) | `link` |
| `broken` | the host link points at something that is gone, or elsewhere | `link` |
| `drifted` | a tool replaced the link with a real file | `add <host-path>` takes the live file |

How each is decided:

1. **Entries.** A host path that is a link to the entry's workspace copy, with the copy
   present, is `ok`. A link with a wrong or missing target is `broken`. Nothing there is
   `missing`. A real file or folder there is `drifted`.
2. **Found.** Each catalog row for this OS has its `~` and globs expanded. An item is
   `found` when it exists as a real file or folder (not a link), is not already an entry,
   and is not inside a folder entry. A link to somewhere outside the workspace is skipped
   silently. `~/.ssh/id_*` never matches a `.pub` or a `.ws-backup`, and a private key and
   its `.pub` travel as one group.
3. **Untracked** (only when the walk is on). The walk goes four levels under `$HOME`, never
   follows links, skips `.cache`, `.git`, `node_modules`, `.local/share`, `.npm`,
   `.cargo/registry`, `.rustup`, `.var`, `snap`, the trash and any mount of the workspace,
   and stops after 200,000 nodes with a note. Catalog paths are also checked directly, at any
   depth. A link whose target is inside the workspace and that no entry covers is `untracked`.
4. **Orphans.** A file under `symlinks/` that no entry's original equals or contains, and
   whose host path (the mirror reversed) has no entry. A folder that mirrors a catalog folder
   is one orphan and is not entered.

Links that sit inside the workspace (`data/bruno/temp/environments`) are out of scope: the
sync client keeps them.

Tools that rewrite their file on every save (`btop.conf`, `htoprc`) replace the symlink with a
real file. They are not in the catalog. `drifted` shows it, and `add <host-path>` adopts the
live content and links it again.

#### `ws symlink ls`

```text
ws symlink ls [--filter <expr>] [--sort <fields>] [--wide] [--json]
```

Lists every entry, plus every row in a problem status (`untracked`, `orphan`, `broken`,
`missing`, `drifted`). It reads `ws.json` and the filesystem (lstat and readlink, the
`$HOME` walk, and a walk of `symlinks/`), changes nothing and never needs sudo. `ls` takes no
targets.

`found` rows are not shown by default. `ls` ends with the footer
`N known configs found, not symlinked: ws symlink add`, written only when N is above 0 and
neither `--json` nor `--quiet` is set. `--filter status=found` shows them.

| Field | Meaning |
| --- | --- |
| `system` | the host path |
| `original` | the workspace copy as stored in `ws.json` (workspace-relative), or where it would go |
| `workspace_path` | absolute path of the workspace copy |
| `app` | the catalog label; blank for a path you added yourself |
| `status` | one of the seven statuses |
| `message` | why the row is not `ok` |
| `dir` | the row is one whole folder |
| `sudo` | host-side changes need root |

Default columns: `system`, `workspace_path`, `app`, `status`, `sudo`.

Output:

```text
HOST PATH              WORKSPACE COPY                                      APP     STATUS   SUDO
/home/user/.bashrc     /home/user/Workspace/symlinks/home/.bashrc          bash    ok
/home/user/.gitconfig  /home/user/Workspace/symlinks/home/.gitconfig       git     broken
/etc/hosts             /home/user/Workspace/symlinks/system/etc/hosts      hosts   drifted  yes
4 known configs found, not symlinked: ws symlink add
```

`--json`: schema 3, command `symlink.ls`. `data` is the list of records, with every field
above.

Exit: 0 whatever it finds. 1 when `ws.json` cannot be read.

TUI: the Symlinks screen shows exactly these rows. Status shows as `✔ ok`, `✘ broken` or
`⚠` with the status for the others. `d` shows every field.

#### `ws symlink add`

```text
ws symlink add
ws symlink add <host-path> [<ws-path>] [--dir | --files] [--dry-run]
ws symlink add --app <name>
```

Takes the live content at a host path into the workspace and leaves a symlink behind.
`<host-path>` accepts `~`. `<ws-path>` is workspace-relative or absolute and must lie inside
the workspace, not inside `.git`, and not already be an entry's original. Without it the
copy goes to the mirror path. What happens depends on what exists:

| Host path | `<ws-path>` | Result |
| --- | --- | --- |
| a real file or folder | free | moves it into the workspace, links back, records the entry; the plan line shows both paths before anything moves |
| a real file, and already an entry (`drifted`) | the entry's own, or none | takes the live content: it replaces the workspace copy (the old copy goes to the trash), and the link is made again |
| real | exists, not the entry's own | error: the destination exists; remove it or choose another path (exit 1) |
| a link into the workspace, not in `ws.json` (`untracked`) | none, or the link's target | records the entry with the link's target as the original; moves nothing |
| a link into the workspace | differs from the target | error |
| a link pointing outside the workspace | any | error: the path is a link to somewhere else |
| missing | exists | links out and records the entry |
| missing | missing | error: nothing to add |

Folders: a catalog folder (`~/.claude/commands`, `~/.config/nvim`) is always one entry. `--dir`
forces one entry for any folder (`~/.password-store`). `--files` takes every file in the
folder as its own entry without asking. An unknown folder without either lists its files and
asks which to take (Enter for everything but catalog secrets, `all`, `none`, or numbers like
`1,3`; `--quiet` and `--json` take the defaults). A second path goes with a file or `--dir`,
not with `--files` or the file list. A path on the never-suggested list can still be added.

There is no secret question at add time: a private key goes to `symlinks/home/.ssh/` like any
other file. `secret ls` scans `symlinks/` like the rest of the workspace, so a private key
shows up as a finding there. Mark each expected key with `ws secret allow <file:line>`;
nothing under `symlinks/` is skipped, so a real leak in a config file still shows.

A path that cannot be added (the destination exists, the path is a link elsewhere, nothing
is there) is reported on stderr and counts as a failed action, so the exit code follows the
plan (1 when all failed, 3 when some did) and the other paths still go ahead. Sudo is
detected as described under Terms. `--dry-run` prints the plan and changes nothing. A failed
step puts back what it moved, so the host path is never left empty.

**With no arguments**, `add` needs a terminal. It shows what was found, grouped by app:

```text
Found on this machine, not symlinked yet:
  [x] 1  git       ~/.gitconfig, ~/.gitignore_global
  [x] 2  vscode    ~/.config/Code/User/settings.json, ~/.config/Code/User/keybindings.json, … (5 items)
  [ ] 3  ssh       ~/.ssh/id_rsa + .pub                         🔑 secret
  [ ] 4  hosts     /etc/hosts                                   🔒 sudo
Stored under symlinks/ at the same path as on this machine.
Enter = checked ones · all · none · 1,3 · q quits :
```

One row per app holds its items that are neither secret nor sudo (checked). Every secret or
sudo item is its own row (unchecked), and a private key with its `.pub` is one row. The answer
is exactly `Enter`, `all`, `none`, or a comma list of numbers; anything else asks again. `q`,
or the input ending, cancels with exit 130. After the answer ws prints the same plan lines
as `add <host-path>` and runs them without asking each again. Without a terminal (stdin is not
one, or `--json` or `--quiet` is set) it prints the found rows, as
`ls --filter status=found`, and changes nothing.

**`--app <name>`** skips the question and adds every non-secret, non-sudo item found for that
app, asking per action as usual. It exits 1 when the app has nothing found (and says how many
secret or sudo items were left for `add <host-path>`), or when the name is not a catalog
label. It takes no paths.

Output:

```text
$ ws symlink add ~/.bashrc
Add /home/user/.bashrc → /home/user/Workspace/symlinks/home/.bashrc [y/n/a/q] y
  ✔ Add /home/user/.bashrc → /home/user/Workspace/symlinks/home/.bashrc

$ ws symlink add ~/Screenshots artifacts/screenshots --dir
Add directory /home/user/Screenshots → /home/user/Workspace/artifacts/screenshots (whole) [y/n/a/q] y
  ✔ Add directory /home/user/Screenshots → /home/user/Workspace/artifacts/screenshots (whole)
```

The other plan lines are
`Add <host> again (the live file replaces <ws>; the old copy goes to the trash)`,
`Register <host> (already a link to <ws>; nothing moves)`,
`Link <host> → <ws> (the workspace copy exists; nothing moves)` and
`Add directory <host> → <ws> (whole)`. A drifted folder entry, or one whose copy is shared,
cannot take the live content: ws says so and changes nothing.

`--json`: `symlink.add`, with `data.actions`.

Exit: 0, 1 or 3. Entries that are already managed are reported and do not change the exit code.

TUI: `a`.

- **With marked rows**, it takes the drifted ones (the live file wins) and registers the
  untracked ones. The others are listed in a note.
- **Without marks**, it opens the same checklist as the CLI, from the same detection. Sudo
  rows are listed, unchecked, and fail with a pointer to the CLI.
- `:add <host-path> [<ws-path>] [--dir]` is the two-path form.

#### `ws symlink link`

```text
ws symlink link [<host-path>…] [--dry-run]
```

Points host paths at their workspace copies. The workspace copy wins, and ws never overwrites
a real file.

With no paths, `link` takes every entry whose status is `missing` or `broken`. This is the
restore step on a new machine, once the workspace has synced. With paths, it takes those
entries, and reports the `ok` ones as already linked. A path with no entry whose mirror path
exists in the workspace (an `orphan`) is registered and linked. A path that is neither is an
error, and nothing changes. Each entry is one action:

- If nothing is at the host path, ws creates the missing parent directories and the symlink.
- If a symlink to somewhere else is there, ws renames it to `.ws-backup`, then creates the
  symlink.
- A real file or folder (`drifted`) is skipped, with
  `drifted: ws symlink add <path> keeps the live file`. To make the workspace copy win,
  move the live file away first.
- An entry whose workspace copy is missing cannot be linked. ws leaves its host path alone,
  prints the missing path, and counts the entry as failed. Restore the copy (for example
  with `ws trash restore`), or `unlink` the entry.
- A sudo entry is skipped, with a message, when `sudo` is not installed.

Every action is pre-checked.

Output:

```text
$ ws symlink link
Link /home/user/.gitconfig → /home/user/Workspace/symlinks/home/.gitconfig [y/n/a/q] y
  ✔ Link /home/user/.gitconfig → /home/user/Workspace/symlinks/home/.gitconfig
Link /etc/hosts → /home/user/Workspace/symlinks/system/etc/hosts (what is there moves to /etc/hosts.ws-backup) [y/n/a/q] y
  ✔ Link /etc/hosts → /home/user/Workspace/symlinks/system/etc/hosts (what is there moves to /etc/hosts.ws-backup)
✖ /home/user/.npmrc: workspace copy /home/user/Workspace/symlinks/home/.npmrc is missing

$ ws symlink link ~/.zshrc
Register and link /home/user/.zshrc → /home/user/Workspace/symlinks/home/.zshrc [y/n/a/q] y
```

With nothing to repair, it prints `Nothing to link.`

`--json`: `symlink.link`, with `data.actions`. Entries with a missing copy appear there with
status `failed`.

Exit: 0, 1 or 3.

TUI: `l` repairs the missing and broken rows and registers orphans among the shown rows (the
marked rows, else every row the filter shows). A drifted row is left for `a`. `:link [<path>…]`.

#### `ws symlink unlink`

```text
ws symlink unlink <host-path…> [--dry-run]
ws symlink unlink --all [--dry-run]
```

Stops managing entries and leaves the real content at each host path. `--all` takes every
entry. Paths or `--all` are required, not both.

Each entry is one action:

1. If the host path holds anything other than the entry's own symlink (a real file or
   folder that replaced the link, or a symlink to somewhere else), it is renamed to
   `.ws-backup` first.
2. The entry's symlink is removed.
3. The content goes back:
   - An **owned copy** moves back to the host path. Directories this leaves empty under
     `symlinks/` are removed.
   - A **shared** copy is **copied** to the host path, recursively for a folder, with its
     modes. The original stays in the workspace.
   - When the workspace copy is missing, nothing is restored. ws removes only a dangling
     symlink at the host path.
4. The entry is removed from `symlinks[]`.

The CLI asks per action as usual. In the TUI, `ctrl+d` pre-checks the rows you chose, and
`:unlink --all` starts with every action unchecked (§4.7). A failed entry stays registered.

Output:

```text
$ ws symlink unlink ~/.bashrc ~/.claude/CLAUDE.md
Unlink /home/user/.bashrc (move /home/user/Workspace/symlinks/home/.bashrc back) [y/n/a/q] y
  ✔ Unlink /home/user/.bashrc (move /home/user/Workspace/symlinks/home/.bashrc back)
Unlink /home/user/.claude/CLAUDE.md (copy /home/user/Workspace/agent/AGENTS.md there; the original stays) [y/n/a/q] y
  ✔ Unlink /home/user/.claude/CLAUDE.md (copy /home/user/Workspace/agent/AGENTS.md there; the original stays)
```

A drifted entry adds `; the live file moves to /home/user/.bashrc.ws-backup` to its line, and
a missing copy reads `Unregister /home/user/.npmrc (workspace copy missing; nothing to
restore)`.

`--json`: `symlink.unlink`, with `data.actions`.

Exit: 0, 1 or 3.

TUI: `ctrl+d` (selected rows), `:unlink --all`.

#### Removed and renamed

`--class` and `--to` typed: exit 1 with `--class/--to were removed: pass the
workspace path as a second argument`. `symlink edit`, `symlink backup`, `symlink git` and
`symlink migrate` are unknown subcommands, with a one-line hint.

### `ws skill`

Skills are the folders of instructions that coding tools such as Claude Code or Codex load
on demand. A skill is a folder, or a symlink to one, that holds a `SKILL.md` with `name` and
`description` frontmatter. ws keeps skills on two shelves and links them into the skills
folder of each tool that should have them:

- **Built-in.** Skills that ship inside the ws binary (today `workspace-write` and
  `workspace-run`). ws extracts them, read-only (files `0444`), to
  `$XDG_DATA_HOME/ws/skills/<name>/` (default `~/.local/share/ws/skills`), and tool links point
  at that copy. Every `ws skill` command and `ws init` refresh it when the binary's version or
  content differs from what is there, so a new ws updates every tool with no reinstall. Only
  ws writes them; to change one, `eject` it.
- **Yours.** Your own skills, in `<workspace>/agent/skills/<name>/`. A skill that is a symlink
  works too: `agent/skills/<name>` may point anywhere.

A skill of yours with a built-in's name is listed with a warning, and the built-in wins:
`install` links the built-in. An ejected copy (it holds a `.ws-ejected` file) replaces the
built-in instead.

ws knows the tools from a **built-in table**. It is in the binary only and cannot be edited.
Each row is the tool's config folder (a tool counts as **detected** when it exists, because a
fresh install has no `skills/` folder yet) and the skills folder the tool reads:

| Tool | Skills folder | | Tool | Skills folder |
| --- | --- | --- | --- | --- |
| `amp` | `~/.config/agents/skills` | | `junie` * | `~/.junie/skills` |
| `claude` | `~/.claude/skills` | | `kimi` | `~/.kimi-code/skills` |
| `cline` | `~/.cline/skills` | | `kiro` | `~/.kiro/skills` |
| `codex` * | `~/.codex/skills` | | `opencode` | `~/.config/opencode/skills` |
| `copilot` | `~/.copilot/skills` | | `roo` | `~/.roo/skills` |
| `cursor` | `~/.cursor/skills` | | `windsurf` | `~/.codeium/windsurf/skills` |
| `gemini` | `~/.gemini/skills` | | | |

\* The tool's own documentation does not confirm this path (Codex documents only
`~/.agents/skills`; Junie's user-level path is not published). ws marks it in `scan`.

ws does not use the shared `~/.agents/skills` folder: Claude Code does not read it (tested
with Claude Code 2.1.310), so every tool gets its own link. Some tools also read
`~/.claude/skills` (Cursor, Copilot, OpenCode); installing into both can show a skill twice.
ws does not track that. `scan` reports each tool's folder and nothing else, and you choose.

| Command | RO/RW | Does | TUI |
| --- | --- | --- | --- |
| `ws skill scan [--tool <name>]` | RO | a grid of both shelves against every known tool | screen |
| `ws skill show <skill> [--path]` | RO | one skill: shelf, version, folder, tools | the screen's row |
| `ws skill install [<skill>…] [--tool <name>] [--dev[=<dir>] \| --providers]` | RW | link skills into one tool; `--providers` copies the skills tool binaries ship | `i`, `:install` |
| `ws skill uninstall [<skill>…] [--tool <name>]` | RW | remove the links ws made in one tool | `u`, `:uninstall` |
| `ws skill eject <skill>` | RW | copy a built-in into `agent/skills/`; it is yours from then on | — |
| `ws skill check` | RO | skills and scripts that drifted from this ws | — |

#### `ws skill scan`

```text
ws skill scan [--tool <name>]
```

It runs no tool and changes nothing but the managed copy of the built-ins (the refresh every
`ws skill` command does). It shows two sections: **Built-in**, with the ws version (or `dev`)
of each, and **Yours**, where an ejected copy says `ejected from <version>` and a name clash
shows a warning instead of cells. For each skill and each known tool it checks one path: `<skills folder>/<skill name>`. If anything is there (a symlink, even a broken
one, or a real folder), the cell is `✔`. Otherwise it is `·` (`-` with `--no-color`). Nothing
else is reported: no duplicate check, no judgment. A cell is `·` both when the skill is not
installed and when the tool is not installed. The header and the footer tell the tools apart: a
detected tool is bold, a tool that is not detected is dim and listed under `○ not detected`
(`( )` with `--no-color`). A skill whose link target is gone shows `(broken link)`. An entry in
`agent/skills/` without a `SKILL.md`, and loose files, are not skills and are not listed.

```text
$ ws skill scan
Built-in (ws v0.16)       amp  claude  cline  codex  copilot  cursor  gemini  ...
  workspace-run    dev    ·    ✔       ·      ·      ·        ·       ·
  workspace-write  v0.16  ·    ✔       ·      ·      ✔        ·       ·
Yours (~/Workspace/agent/skills)
  review-pr               ·    ✔       ·      ·      ·        ·       ·
  sql-style               ·    ·       ·      ·      ·        ·       ·
  workspace-write  ▲ same name as a built-in; the built-in wins

● detected: claude cursor
○ not detected: amp cline codex* copilot gemini ...
✔ installed   · not installed (the tool or the skill)
* path not confirmed by the tool's own docs
```

`--tool` shows one column. `--json`: `skill.scan`, with `data.version`, `data.managed` (the
built-ins' folder), `data.root` (`agent/skills/`), `data.tools[]` (`name`, `detected`,
`unverified`) and `data.skills[]` (`skill`, `shelf` (`built-in` or `yours`), `broken`,
`version`, `dev`, `ejected_from`, `shadowed`, `tools[]` of `tool` and `installed`).

Exit: 0, or 1 on a usage error or an unknown tool.

#### `ws skill show`

```text
ws skill show <skill> [--path]
```

One skill: its shelf, its version (the ws version for a built-in, `dev` with the checkout it
links to, or where an ejected copy came from), its folder and the tools that have it. On a
name clash it shows the built-in and names the shadowed folder. `--path` prints only the
folder, for both shelves, so a skill can reach its sibling files:
`$(ws skill show workspace-write --path)/write-rules.md`. A built-in's folder is the managed
copy, also in dev mode.

```text
$ ws skill show workspace-write
workspace-write
  shelf              built-in
  version            v0.16
  path               ~/.local/share/ws/skills/workspace-write
  tools              claude copilot
```

`--json`: `skill.show`, with `data.name`, `shelf`, `version`, `dev`, `ejected_from`, `path`,
`tools[]` and `shadowed`. Exit: 0, or 1 on an unknown skill.

#### `ws skill install`

```text
ws skill install [<skill>…] [--tool <name>]
ws skill install --dev[=<dir>] [<skill>…] [--tool <name>]
```

Links each skill into **one** tool's skills folder: a symlink `<skills folder>/<name>` to
`<workspace>/agent/skills/<name>`, or for a built-in to its managed copy, created with the
folder if it is missing. To install into two tools, run it twice. Without `--tool`, ws lists the known tools
(detected ones first) and asks for one. Without skill names, it lists the skills of both
shelves not yet in that tool, each labeled `built-in` or `yours`, and asks which; `all` or
`1,3` is accepted. Both prompts need a terminal; with
`--json` or `--quiet` a missing `--tool` or name is a usage error.

ws never replaces anything. If the path already exists (a real folder, another link), that
action fails and the rest still run. A skill whose target is gone cannot be installed. A tool
that is not detected can still be chosen; the plan line says it will create the folder. A
skill whose link is already in place is reported and skipped.

`--dev` is for working on the built-ins. It makes the managed `<name>` a symlink to
`<dir>/skills/<name>`, so edits in the checkout are live in every tool linked to it, and
refreshes leave it alone. `<dir>` defaults to the checkout ws was built from (`make build`
stamps it); `--dev=<dir>` names another. Without names it takes every built-in; with `--tool`
it also links them into that tool. A plain `install` of a built-in in dev mode switches it
back to the copy in the binary.

```text
$ ws skill install review-pr --tool claude
Link review-pr → ~/.claude/skills/review-pr [y/n/a/q] y
  ✔ Link review-pr → ~/.claude/skills/review-pr
```

`--providers` is for tool binaries that ship their own skill. ws keeps a short, explicit list of
them (today `agent-tmux`); it never scans `PATH`. For each one found on `PATH`, ws runs
`<bin> skill` (5 s timeout), reads `name` from the SKILL.md frontmatter and writes the output to
`agent/skills/<name>/SKILL.md`, with a `.ws-provider` file that holds the binary's name. The copy
is a skill of yours, so `scan` lists it under Yours (`from <bin>`) and `install` links it like any
other; `--providers --tool <name>` copies and links in one step. It writes only new copies and
copies that differ, and only folders that carry `.ws-provider`: a skill of yours with the same
name is never touched and is reported. A provider that fails or times out is reported and the
others still run (exit 1 at the end). Run it again after upgrading the binary; `ws skill check`
tells you when to.

`--json`: `skill.install`, with `data.actions`. Exit: 0, 1 or 3.

#### `ws skill uninstall`

```text
ws skill uninstall [<skill>…] [--tool <name>]
```

Removes links from one tool. It removes only a symlink ws made: one whose own target is inside
`agent/skills/` or the built-ins' managed folder, or is a built-in's dev checkout (one hop is
read: where `agent/skills/<name>` itself points does not matter).
A real folder, or a link that points elsewhere, is left alone and the action fails. The skill
in the workspace is never touched. Without `--tool`, ws lists the tools that hold at least one
of its links; without names, it lists those links.

`--json`: `skill.uninstall`, with `data.actions`. Exit: 0, 1 or 3.

#### `ws skill eject`

```text
ws skill eject <skill>
```

Copies a built-in into `agent/skills/<name>`, writable, with a `.ws-ejected` file that holds
the ws version, and repoints the tool links to the managed copy at the workspace copy. From
then on the skill is yours: ws stops updating it, and `scan` lists it under Yours as `ejected
from <version>`. It fails when `agent/skills/<name>` already exists.

```text
$ ws skill eject workspace-write
Copy built-in workspace-write → ~/Workspace/agent/skills/workspace-write (yours from now on; ws stops updating it); repoint claude, copilot at it [y/n/a/q] y
  ✔ Copy built-in workspace-write → ~/Workspace/agent/skills/workspace-write (…)
```

`--json`: `skill.eject`, with `data.actions`. Exit: 0, 1 or 3.

#### `ws skill check`

```text
ws skill check
```

Read-only. A drift check over both shelves and `<workspace>/scripts`:

| Check | Level |
| --- | --- |
| every `ws <noun> <verb>` in a code span or block of a skill's `.md` files is a real command | error |
| every backticked workspace path in a skill of yours (first part a layout folder, no `<`, `*` or `$`) exists | error |
| each top-level script in `scripts/` starts with a header comment (after the shebang) | error |
| no top-level script mentions `/tmp` (use `$WS_TMP`) | error |
| a skill of yours whose frontmatter `metadata: ws-layout: "<n>"` differs from `ws.json`'s `schema` | warning |
| a fallback in `scripts/lib/common.sh` (`WS_TMP=${TMPDIR:-$HOME/tmp}`, …) that disagrees with `ws env` | warning |
| a skill of yours with a built-in's name | warning |
| a provider copy (`.ws-provider`) that differs from the installed binary's `<bin> skill` output; fix: `ws skill install --providers` | warning |
| a provider copy whose binary is not on `PATH`; fix: install the binary, or remove the copy's folder | warning |
| a provider whose `<bin> skill` fails, times out or prints no named skill, or whose skill name clashes with one of yours | warning |

```text
$ ws skill check
✖ ~/.local/share/ws/skills/workspace-write/SKILL.md:5  `ws skill frobnicate` is not a ws command
✖ scripts/bad.sh:2  mentions /tmp; use $WS_TMP
▲ scripts/lib/common.sh:2  WS_CACHE falls back to /home/user/.cache/wsx; ws env says /home/user/.cache/ws

2 errors, 1 warning
```

`--json`: `skill.check`, with `data.findings[]` (`level`, `shelf`, `skill`, `file`, `line`,
`message`), `data.errors` and `data.warnings`. Exit: 1 when there is an error, else 0.

### `ws scratch`

Scratch directories are throwaway working directories for debugging, investigations and
one-off tasks. Each row is a directory directly under the **scratch root**,
`scratch.root_dir` in `ws.json` (default `~/Scratch`). `~` expands to `$HOME`, and a
relative value resolves against the workspace root. ws creates the root on first use. By
default it is outside the workspace, so scratch is never synced. Files directly in the
root are not rows. Tags live in each directory's `.ws-meta.json`. Every tag ever added is
also kept in `scratch.tags`, the vocabulary that completion and the TUI offer. The
Scratch screen shows the rows.

**Names.** A `<name>` argument is matched against the directory names. An exact name
wins. Otherwise a case-insensitive substring that matches exactly one directory is used,
so `dns` finds `dns-debug.2026-09`. A name that matches nothing, or matches several
directories, is an error that lists the candidates (exit 1) (decision). When a required
name is omitted and stdin and stdout are a terminal, ws asks for it. The prompt lists
the existing names as you type, and `tab` completes the first match. Without a terminal,
a missing name is a usage error (exit 1). Ctrl-C at the prompt exits 130.

| Command | RO/RW | Does | TUI |
| --- | --- | --- | --- |
| `ws scratch ls` | RO | list scratch directories | screen |
| `ws scratch new [name]` | RW | create a directory and open it in the editor | `a` |
| `ws scratch add <path…>` | RW | move existing files or directories into scratch | `:add` (and `s` on Ignore) |
| `ws scratch open <name>` | RO | open a directory in your editor | `enter` |
| `ws scratch tag <name> [tag…]` | RW | add or remove tags | `t` |
| `ws scratch rm <name…>` | RW | soft-delete directories to the trash | `ctrl+d` |

#### `ws scratch ls`

```text
ws scratch ls [--filter <expr>] [--sort <fields>] [--wide] [--json]
```

Lists every directory under the scratch root, newest first. `age` is the time since the
directory's last modification. `items` counts the files and directories inside it,
recursively. To find directories by name or tag, use `--filter`.

| Field | Column | Meaning |
| --- | --- | --- |
| `name` | Name | directory name |
| `path` | Path (`--wide`) | absolute path |
| `age` | Age | time since last modification |
| `size_bytes` | Size | total size of the files inside |
| `items` | Items | files and directories inside |
| `tags` | Tags | tags from `.ws-meta.json` |

```text
$ ws scratch ls --filter tags=dns
Name                  Age  Size    Items  Tags
dns-debug.2026-09     3h   2.0 KB  4      dns, network
```

`--sort size` is still accepted and means `--sort -size_bytes`.

JSON: `scratch.ls`, one record per directory with the fields above.

Exit: 0, or 1 for an unknown field in `--filter`/`--sort` or an unreadable root.

TUI: the Scratch screen. `/` takes the same filter expressions.

#### `ws scratch new`

```text
ws scratch new [name] [--no-date] [--no-open] [--editor <cmd>] [--dry-run]
```

Creates `<root>/<name>.<YYYY-MM>` and opens it in the editor. The month suffix is
skipped with `--no-date`. If the name is taken, ws appends `-2`, `-3`, and so on. With no
name on a terminal, ws asks for one and lists the existing names, so you can pick a
distinct one. `tab` does not complete here. An empty answer, or no name without a
terminal, generates `scratch-<n>`. `name` is one argument, so quote a name that contains
spaces (decision). The new directory gets a `.ws-meta.json` with its creation time and no
tags.

The plan has one action, `Create /home/user/Scratch/<final name>`, pre-checked. After
it runs, ws opens the directory in your editor (§4.10; default `vim`), or `--editor`. A
terminal editor runs in the foreground, any other in the background. If the editor cannot
start, ws prints a warning and the path, and still exits 0.

| Flag | Default | Meaning |
| --- | --- | --- |
| `--no-date` | off | no `.YYYY-MM` suffix |
| `--no-open` | off | do not start the editor |
| `--editor <cmd>` | §4.10 | editor for this run |

```text
$ ws scratch new proxy-auth --no-open
Create /home/user/Scratch/proxy-auth.2026-10 [y/n/a/q] : y
  ✔ Create /home/user/Scratch/proxy-auth.2026-10
  /home/user/Scratch/proxy-auth.2026-10
```

JSON: `scratch.new`, with `result` (`path`, `created`, `dry_run`) and `actions`. `--json`
does not start the editor.

Exit: 0 or 1. 130 if the name prompt is cancelled.

TUI: `a` asks for a name (existing names are offered), shows the one-action checklist,
then opens the editor.

#### `ws scratch add`

```text
ws scratch add <path…> [--dry-run]
```

Moves existing files or directories into the scratch root, so they leave the workspace
(and stop syncing) but are kept and can be found with `scratch ls`. Paths follow the
input rules in §4.5. Nothing is copied: each path is moved (renamed, or copied and then
removed when the scratch root is on another filesystem).

- **A directory** keeps its name: `<root>/<base name>`.
- **A file** goes into a new directory named after the file without its last extension,
  because scratch rows are directories (decision):
  `/home/user/Workspace/experiments/net/capture.pcap` →
  `/home/user/Scratch/capture/capture.pcap`. A name that would be empty (`.env`) keeps
  the full file name.
- **Name collisions** append `-2`, `-3`, … to the destination directory, as `new` does.
  An existing scratch directory is never merged into or overwritten.
- No date suffix is added, and no `.ws-meta.json` is written. Tags are written by the
  first `scratch tag`.

The plan has one action per path, pre-checked: `Move <source> → <destination>`.
These paths stop the command before anything moves (exit 1) (decision): a path that does
not exist; the scratch root or anything inside it; a directory that contains the scratch
root or the workspace root; the workspace's `ws.json`; and a symlink original that
`ws.json` `symlinks[]` points to (`ws symlink unlink` it first). A symlink is moved as the
link itself, like `mv`.

```text
$ ws scratch add experiments/net/capture.pcap
Move /home/user/Workspace/experiments/net/capture.pcap → /home/user/Scratch/capture/capture.pcap [y/n/a/q] : y
  ✔ Move /home/user/Workspace/experiments/net/capture.pcap → /home/user/Scratch/capture/capture.pcap
```

JSON: `scratch.add`, with `moved` (`source`, `path` per executed move) and `actions`.

Exit: 0, 1, or 3.

TUI: `:add <path…>` on Scratch, with path completion. On the Ignore screen, `s` runs the
same move for the selected warnings.

#### `ws scratch open`

```text
ws scratch open <name> [--editor <cmd>] [--print-path]
```

Resolves `<name>` (see **Names**) and opens the directory in your editor (§4.10) or
`--editor`, with the same foreground and background rule as `new`. If the editor cannot
start, ws prints a warning and the path, and exits 0. `open` changes nothing.

| Flag | Default | Meaning |
| --- | --- | --- |
| `--editor <cmd>` | §4.10 | editor for this run |
| `--print-path` | off | print only the resolved path on stdout. Status lines and the name prompt go to stderr. |

`--print-path` exists for the `wsopen` shell function that `ws completion` installs. It
runs `cd "$(ws scratch open --print-path "$@")"`, so `wsopen dns` opens the directory and
moves the shell into it.

```text
$ ws scratch open dns
✔ Opening   vim → /home/user/Scratch/dns-debug.2026-09
```

JSON: `scratch.open`, with `name` and `path`. `--json` does not start the editor.

Exit: 0, or 1 when the name resolves to no directory or to several. 130 if the prompt is
cancelled.

TUI: `enter` on a row.

#### `ws scratch tag`

```text
ws scratch tag <name> [tag…] [--rm <tag>]… [--dry-run]
```

Changes the tags of one scratch directory. Tags are labels for you to read in
`scratch ls`. ws never derives them or searches by them, though `ls --filter tags=dns` still
narrows a list. Tags are trimmed and lowercased. A comma-separated argument (`k8s,dns`)
counts as several tags. The tags not yet on the directory are added, and each `--rm` tag
that it has is removed. The plan has one action for the directory, pre-checked:
`Tag dns-debug.2026-09: +dns +network −old`. If nothing changes, ws prints `No change.` and
exits 0.

With no tags and no `--rm`, ws asks for them on a terminal. Tags used before (`scratch.tags`)
are offered as completions, and nothing is suggested from the directory's contents
(decision, by the user: tags are a human label). Without a terminal, this is a usage error
(exit 1).

| Flag | Default | Meaning |
| --- | --- | --- |
| `--rm <tag>` | — | remove this tag; repeatable |

JSON: `scratch.tag`, with `name`, `path`, `tags` (after the change), `added`, `removed`
and `actions`.

Exit: 0, 1, or 3.

TUI: `t` on the selected rows opens a prompt for tags, comma-separated, where `-tag` removes
one. `tab` completes from tags used before, and the hint shows the current tags. Submitting
shows a checklist with one action per directory.

Compatibility: `--auto` is accepted and ignored.

#### `ws scratch rm`

```text
ws scratch rm <name…> [--dry-run]
```

Moves scratch directories to the XDG trash (§7.3), where `ws trash restore` can bring
them back. Each `<name>` resolves to exactly one directory (see **Names**). If any name
matches nothing, or matches several directories, nothing is deleted (exit 1). The plan
has one action per directory, pre-checked: `Delete /home/user/Scratch/dns-debug.2026-09
(2.0 KB)`.

JSON: `scratch.rm`, with `actions`.

Exit: 0, 1, or 3. 130 if the name prompt is cancelled.

TUI: `ctrl+d` on the selected rows, through the checklist.

---

### `ws session`

Terminal sessions belong to [agent-tmux](https://github.com/mugenkunou/agent-tmux): it
runs them in tmux, records each one from the start, and lists, shows, searches and deletes
the recordings. ws has no session code of its own. `ws session <args…>` replaces itself
with `agent-tmux <args…>` (exec), so the terminal and signals reach agent-tmux directly.
Every word after `session` is agent-tmux's, flags included: `ws session --help` is
agent-tmux's help, and `ws session ls --json` is its JSON. ws global flags typed before
`session` are ignored. Recordings live where agent-tmux keeps them; ws never reads them.

```text
ws session <agent-tmux args…>
```

```text
ws session ls                    live sessions and recordings
ws session create web            start one and attach
ws session grep rsync            search every recording
ws session show web              page the recording
```

Without agent-tmux on PATH, `ws session` prints how to install it and exits 127:

```text
download: https://github.com/mugenkunou/agent-tmux/releases/latest
or:       go install github.com/mugenkunou/agent-tmux/cmd/agent-tmux@latest
```

Completion of `ws session <words>` is `agent-tmux __complete <words>`, so it offers exactly
what agent-tmux would (§4.8). Without agent-tmux it offers nothing.

**Version nudge.** `ws version` and the TUI (the Sessions screen, and a Dashboard row) say
`agent-tmux <tag> is available`, with the two install lines, when GitHub's latest
agent-tmux release (`api.github.com/repos/mugenkunou/agent-tmux/releases/latest`,
`tag_name`) is newer than `agent-tmux version --json`. Versions compare as semver; a `dev`
build, or anything else unparseable, never nudges. The answer is cached for 24 hours in
`$XDG_CACHE_HOME/ws/agent-tmux-release.json` (default `~/.cache/ws/`): `{"checked_at",
"etag", "tag_name"}`. After that the check sends `If-None-Match` with the cached ETag, and
a `304` keeps the cached tag for another day. The TUI checks in the background; every
check gives up after 3 seconds and fails silently. `WS_NO_VERSION_CHECK=1` turns it off.
`ws session` never checks.

TUI: the Sessions screen (§6.5).

---

### `ws ignore`

`ignore` decides what the sync client leaves out. The rules are a built-in template plus your
own rules in `ws.json` (`ignore_rules`), and ws generates `<workspace>/.megaignore` from them,
the file MEGA reads. With another client (§2.6), the same rules still drive `ignore ls`,
`lint`, `check` and `tree`, and you copy the rules you want into the client's own
exclusion settings. The rows
are the paths that don't sync, and the rule that keeps each one out. `ignore lint` checks the
paths that do sync and warns about the unhygienic ones: too big, too deep, or build output.
The Ignore screen has three views: Warnings (`lint`, where it opens), Muted and Ignored.

| Command | RO/RW | Does | TUI |
| --- | --- | --- | --- |
| `ws ignore ls` | RO | every path that doesn't sync, and the rule | Ignored view |
| `ws ignore ls --muted` | RO | the muted warnings | Muted view |
| `ws ignore lint [path…]` | RO | check synced files and warn about the unhygienic ones | Warnings view (the default) |
| `ws ignore add <path…>` | RW | stop syncing paths | `i` |
| `ws ignore rm <path…>` | RW | sync paths again | `:rm` |
| `ws ignore mute <path…>` | RW | hide a path's warnings; it keeps syncing | `m` |
| `ws ignore unmute <path…> \| --stale` | RW | show its warnings again; `--stale`: drop muted paths that no longer exist | `u` (muted view) |
| `ws ignore check <path>` | RO | will this path sync, and which rule decides | `:check` |
| `ws ignore tree [dir] [-L n]` | RO | the tree with sync status | `:tree` |
| `ws ignore edit` | RW | edit the rules in `$EDITOR` | `e` |
| `ws scratch add <path…>` | RW | move a path out of the workspace into scratch (see `ws scratch`) | `s` |
| shell `rm <path>` | — | delete a path (it goes to the trash once `ws trash enable` has run) | `ctrl+d` (to the trash) |

#### Rules

MEGA reads `.megaignore` top to bottom, and the last rule that matches a path decides. A
path that no rule matches syncs. ws writes the rules in this order:

| # | Rules | Source | Example |
| --- | --- | --- | --- |
| 1 | built-in excludes: OS junk, dot-files, temp files, build directories, compiled output, logs, lock files, archives, datasets | the ws binary, minus `ignore_rules.suppress_defaults` | `-:.*`, `-:node_modules`, `-g:*.log`, `-g:*.csv` |
| 2 | the built-in included directory `symlinks/**`, then your included directories | fixed in the ws binary, then `ignore_rules.include` (edit with `ignore edit`) | `+g:symlinks/**`, `+g:experiments/ml/**` |
| 3 | ignored paths | `ignore_rules.exclude` (`ignore add`) | `-:experiments/run/dump.bin` |
| 4 | never sync symlinks | fixed | `-s:*` |

- **Included directories** sync everything below them, including what rule 1 excludes
  (`.git`, `node_modules`, `*.log`). The only built-in one is `symlinks/**` (decision): the
  mirrored tree of `ws symlink` keeps host names (`.gitconfig`, `.ssh/`, `.config/`), and the
  built-in `-:.*` would drop them all. It is fixed: not in `ws.json`, and `suppress_defaults`
  cannot turn it off. You add your own with `ws ignore edit`. No other command creates them.
- **Muting changes no rule.** `ignore mute` only hides warnings (see below).
- **Your ignored paths come last, so they always win** (decision): `ignore add` works inside
  a muted directory and inside an included directory.
- `.megaignore` is generated. Every RW `ignore` command rewrites it, so hand edits are lost.
  Edit the rules with `ws ignore edit`. When `.megaignore` differs from what `ws.json`
  generates (after a ws upgrade, or a hand edit), `ignore ls`, `lint`, `check` and `tree` print one
  warning on stderr, and the next RW `ignore` command rewrites it (decision).
- ws evaluates the rules from `ws.json`, the same rules it writes into `.megaignore`.
  `ignore ls`, `ignore lint`, `ignore check`, `ignore tree` and `secret ls` all use them.

**Warnings.** `ignore lint` walks every path that syncs. It never enters an ignored path.

| Type | Severity | When |
| --- | --- | --- |
| `bloat` | CRITICAL | a file is larger than `ignore.crit_size_mb` (default 10) |
| `bloat` | WARNING | a file is larger than `ignore.warn_size_mb` (default 5) |
| `depth` | WARNING | a file or directory is more than `ignore.max_depth` (default 6) levels below the workspace |
| `project-meta` | WARNING | build output next to a project marker: `bin/` beside `go.mod`; `dist/`, `build/`, `node_modules/` beside `package.json` |

A warning is **muted** when its path is muted with `ignore mute` (itself or a parent), or
lies under an included directory (any path an include rule is the reason it syncs). A muted warning has severity INFO and
`muted` true, and appears only in `ignore ls --muted`. This includes `project-meta`
(decision).

#### `ws ignore ls`

```text
ws ignore ls [--muted] [--filter <expr>] [--sort <fields>] [--wide] [--json]
```

Lists every path that doesn't sync, with the rule that keeps it out, one file per line, so
it pipes into `grep`. Files inside an ignored directory are listed too. This answers "what
is the sync client leaving out?"

With `--muted`, it lists the muted warnings instead: everything you muted with
`ignore mute`, plus the warnings under included directories. `ignore ls` thus shows both
kinds of decision you made: what doesn't sync, and what you chose not to hear about.
A muted path that no longer exists is listed too, as a row of type `stale-mute` (INFO,
muted, message `muted path no longer exists`); `--filter type=stale-mute` shows only those.

| Flag | Default | Meaning |
| --- | --- | --- |
| `--muted` | off | the muted warnings instead of the ignored paths |
| `--filter`, `--sort`, `--wide` | — | as in §4.3 |

Ignored-path fields: `path`, `size_bytes`, `rule`, `rule_source` (`builtin` or `user`).
Warning fields (with `--muted`): see `ignore lint`.

Without `--muted`, when `ignore.muted` names paths that no longer exist, it prints one line
on stderr: `2 muted paths no longer exist: ws ignore unmute --stale` (`--quiet` hides it).

```text
$ ws ignore ls --filter rule=csv
SIZE      PATH                                                RULE
114.0 MB  /home/user/Workspace/experiments/run/metrics.csv    -g:*.csv
```

JSON: `ignore.ls`. `data` is the array of records.

Exit: 0; 1 on bad flags or an unknown field.

TUI: the Ignored view (`ignore ls`) and the Muted view (`ignore ls --muted`). `v` cycles
Warnings → Muted → Ignored. In the Ignored view, `ctrl+d` stops ignoring the selected
paths (`ignore rm`).

Compatibility: the flags `--path`, `--rule` and `--tree` still work, hidden.

#### `ws ignore lint`

```text
ws ignore lint [path…] [--filter <expr>] [--sort <fields>] [--wide] [--json]
```

Checks the files that sync and warns about the ones that aren't hygienic: too big, too deep,
or build output (the types in **Warnings**). Muted warnings are left out. When there are
some, it prints their count on stderr: `47 muted warnings (2.3 GB): ws ignore ls --muted`.
With paths, it checks only under them. It changes nothing and exits 0, warnings or not.
When `ignore.muted` names paths that no longer exist, it also prints
`1 muted path no longer exists: ws ignore unmute --stale` on stderr (`--quiet` hides it).

Fields: `severity`, `type`, `size_bytes` (Size; shown as `7 lvl` on depth rows), `path`,
`depth`, `group`, `message`, `muted`. Default columns: severity, type, size, path.

```text
$ ws ignore lint
SEVERITY  TYPE          SIZE      PATH
CRITICAL  bloat         220.0 MB  /home/user/Workspace/experiments/web/dump.bin
WARNING   depth         7 lvl     /home/user/Workspace/notes/a/b/c/d/e/f/g
WARNING   project-meta            /home/user/Workspace/experiments/app/dist
```

Each warning has a fix: `ignore add` (stop syncing it), `ignore mute` (keep syncing it,
stop warning), `scratch add` (move it out of the workspace), or deleting it.

JSON: `ignore.lint`. `data` is the array of records.

Exit: 0; 1 on bad flags, an unknown field, or a path outside the workspace.

TUI: the Warnings view, where the Ignore screen opens. Its keys are `i`, `m`, `s` and `ctrl+d`.

#### `ws ignore add`

```text
ws ignore add <path…> [--dry-run]
```

Stops syncing paths. Each path resolves as in §4.5, must exist, and must be inside the
workspace (not the workspace itself). ws stores it workspace-relative in `ws.json`
`ignore_rules.exclude` (`{"pattern": "experiments/run/dump.bin"}`), and `.megaignore` then
has `-:experiments/run/dump.bin`. An ignored directory is ignored with everything in it. ws refuses a symlink original (a
`symlinks[]` workspace copy), because it must sync for `ws symlink link` to work on your other
machines (decision).

At least one path is required (decision: a bulk ignore must be explicit). In the TUI,
mark the rows first.

Plan: one action per path, pre-checked:
`Ignore /home/user/Workspace/experiments/run/dump.bin (ws.json ignore_rules.exclude)`. Each
action also rewrites `.megaignore`. A path that is already ignored gets no action, and ws
prints the rule that ignores it.

Targets are checked first. If one is invalid (missing, or outside the workspace), ws prints
why and exits 1 without changing anything.

JSON: `ignore.add`, `data.actions`.

Exit: 0, 1, 3.

TUI: `i` on the selected rows of the Warnings or Muted view. On Secrets, `i` ignores the file
of each selected finding.

#### `ws ignore rm`

```text
ws ignore rm <path…> [--dry-run]
```

Syncs paths again by removing their entries from `ignore_rules.exclude`. It needs at
least one path. The path does not have to exist, so a rule for a deleted file can go too.

`rm` removes only the entry that equals the path. For any other reason a path is ignored,
`rm` changes nothing and says why:

- **Ignored by a wider rule of yours** (a parent directory, or a pattern with `*`): ws names
  the rule. Remove that one, or change it with `ws ignore edit`.
- **Ignored by a built-in rule** (decision): ws does not edit built-in rules for one path. It
  names the rule (`-g:*.csv`) and the way to sync the path: turn the rule off everywhere
  by listing its pattern in `suppress_defaults` (`ws ignore edit`). Muting changes nothing
  about syncing, so it doesn't help here.
- **Not ignored:** nothing to do.

Plan: one action per path, pre-checked:
`Stop ignoring /home/user/Workspace/experiments/run (remove "experiments/run" from ignore_rules.exclude)`.
If another rule still ignores the path afterwards, the plan line says so. Each action also
rewrites `.megaignore`.

JSON: `ignore.rm`, `data.actions`.

Exit: 0, 1 (including a path that cannot be un-ignored this way), 3.

TUI: `ctrl+d` in the Ignored view, on the selected paths, and `:rm <path>` from any view.

#### `ws ignore mute`

```text
ws ignore mute <path…> [--dry-run]
```

Hides warnings. ws stores each path, workspace-relative, in `ws.json` `ignore.muted`. A
muted file's warnings, and every warning under a muted directory, leave `ignore lint` and
appear in `ignore ls --muted` (the TUI's muted view). **Syncing does not change.** No rule
is added, `.megaignore` is not rewritten, and the path syncs exactly as before.

Each target must exist inside the workspace and must not be the workspace itself. A path
that is already muted gets no action, and ws says what mutes it: the path itself, a muted
parent, or an included directory. At least one path is required.

Plan: one action per path, pre-checked: `Mute /home/user/Workspace/experiments/ml (it keeps
syncing)`.

JSON: `ignore.mute`, `data.actions`.

Exit: 0, 1, 3.

TUI: `m` in the Warnings view mutes the selected rows' paths. Their rows move to the Muted
view.

Muting only hides warnings: it never changes what syncs (decision, by the user). A directory
with an include rule in `ignore_rules.include` keeps syncing everything below it, and
its warnings are muted anyway. To hear about one again, remove its rule with `ws ignore edit`.

#### `ws ignore unmute`

```text
ws ignore unmute <path…> [--dry-run]
ws ignore unmute --stale [--dry-run]
```

Removes paths from `ignore.muted`, and their warnings return to `ignore lint`. Syncing is not
touched.

`--stale` takes no paths. It removes every `ignore.muted` entry whose path no longer exists
(deleted, moved, or under a file that replaced its folder), in any written form. Scans
(`ignore lint`, `ignore ls`) never remove entries, since a path can be gone for a while (a
branch switch, `make clean`) and should be muted again when it returns; they only hint.
An entry is stale only when the path is reported as not existing. Any other error (permissions,
an unreadable or unmounted workspace) leaves it alone. Nothing stale: `No stale muted paths:
nothing to do`.

- **Muted by a parent entry:** ws names the parent and changes nothing. Unmute the parent.
- **Under an included directory** (one of yours): its warnings stay muted, because that
  directory syncs everything by design, and ws says so. To change it, edit `include` with
  `ws ignore edit`. (decision)
- **Not muted:** nothing to do.

Plan: one action per path, pre-checked: `Unmute /home/user/Workspace/experiments/ml`. With
`--stale`: `Unmute /home/user/Workspace/experiments/old (no longer exists)`.

JSON: `ignore.unmute`, `data.actions`.

Exit: 0, 1, 3.

TUI: `u` in the Muted view unmutes the entry that mutes each selected row. Rows muted by an
included directory are skipped, and the notice says so. Stale entries are rows of the Muted
view (type `stale-mute`), so `u` on them is `ignore unmute --stale` for the rows you pick. The
Dashboard's Ignore card counts them (`2 stale mutes`).

#### `ws ignore check`

```text
ws ignore check <path>
```

Reports whether a path syncs, and the rule that decides. The path must exist and be inside
the workspace.

```text
$ ws ignore check ~/Workspace/experiments/run/metrics.csv
✘ IGNORED  /home/user/Workspace/experiments/run/metrics.csv
  Reason: rule `-g:*.csv` (built-in)
  Size: 114.0 MB

$ ws ignore check ~/Workspace/archive/incident-2026.tar.gz
✔ SYNCED  /home/user/Workspace/archive/incident-2026.tar.gz
  Reason: included directory `+g:archive/**` (built-in)
  Size: 230.0 MB
```

The reason is one of: an ignore rule (`built-in` or `ws.json`), an included directory
(`built-in` or `ws.json`), or `no rule matches`. Muting never changes the answer.

JSON: `ignore.check`. Keys: `path`, `included`, `rule` (`<default>` when no rule matches),
`rule_source` (`builtin`, `user`, or empty), `muted`, and `size_bytes` for files.

Exit: 0 whether the path syncs or not; 1 when the path is missing or outside the workspace.

TUI: `:check <path>` answers in a notice.

#### `ws ignore tree`

```text
ws ignore tree [dir] [-L n]
```

Prints the tree under `dir` (default: the workspace root), which must be inside the
workspace. The header is the absolute root, and every node has an icon: `✔` synced, `✘`
ignored, `◐` a synced directory with ignored entries inside it (with their count). Ignored
directories are shown but not expanded. A summary line ends it: `38 files excluded · 812 MB
not synced`, or `All files synced`.

| Flag | Default | Meaning |
| --- | --- | --- |
| `-L n` | unlimited | show n levels below `dir` |

JSON: `ignore.tree`. One record per entry within the depth: `path`, `status` (`synced`,
`ignored`, `partial`), `rule`, `is_dir`, `size_bytes`, `depth`.

Exit: 0; 1 when `dir` is missing or outside the workspace.

TUI: `:tree [dir]`, paged.

#### `ws ignore edit`

```text
ws ignore edit [--editor <cmd>]
```

Writes `ignore_rules` to `<workspace>/.ignore-edit.json` and opens it in `--editor`, else
your editor (§4.10). It holds four keys:

| Key | Meaning |
| --- | --- |
| `exclude` | ignored paths (`ignore add`), `{"pattern", "note"}` |
| `include` | your included directories, `{"pattern", "note"}` |
| `suppress_defaults` | built-in exclude patterns to turn off, such as `"*.csv"` (edit only) |
| `schema` | `1` |

A pattern with `*` is a glob (`-g:` / `+g:`); anything else is a path.

When the editor exits, ws reads the file back. Invalid JSON is an error: `ws.json` stays as it
was, and the exit code is 1. If the rules changed (or `.megaignore` is out of date), the plan
has one action: `Save ignore_rules to /home/user/Workspace/ws.json and rewrite
/home/user/Workspace/.megaignore`. Unchanged rules mean nothing to do. The temporary file
is removed either way.

JSON: `ignore.edit`, `data.actions` and `data.stats` (rule counts: built-in, user).

Exit: 0, 1.

TUI: `e` hands the terminal to the editor.

---

### `ws secret`

`secret` lists secret findings: lines in synced workspace files that look like credentials.
ws scans every file that syncs (it skips what the ignore rules exclude and the directories
muted with `secret mute`), and reports at most one finding per line. The Secrets screen
shows them, under a header with the `pass` and `credential` health.

| Command | RO/RW | Does | TUI |
| --- | --- | --- | --- |
| `ws secret ls` | RO | the findings | screen |
| `ws secret show <file:line>` | RO | a finding in context | `enter` |
| `ws secret allow <file:line…>` | RW | mark findings as false positives | `a` |
| `ws ignore add <file…>` | RW | stop syncing the file (see `ws ignore`) | `i` |
| `ws secret mute <dir…>` | RW | stop scanning a directory | `m` |
| `ws secret unmute <dir…>` | RW | scan it again | `:unmute` |
| `ws secret store <file:line> [entry]` | RW | copy the value into pass, then allow the finding | `s` |

#### Findings and anchors

- **What matches:** a keyword (`password`, `api_key`, `secret`, `token`, `auth`,
  `access_key`, `client_secret`, `connection_string`, in any case, with `_`, `-` or nothing between the words) followed by `:` or `=` and
  a value; a private key header (`-----BEGIN … PRIVATE KEY-----`); a provider token
  (`ghp_…`, `AKIA…`, `xoxb-…`, `sk-…`).
- **Not a finding:** comment lines (`#`, `//`, `--`, `/*`, `*`, `;`, `rem`); placeholder
  values (`<…>`, `${…}`, `{{…}}`, `changeme`, `example`, `xxx`, `your_password`, `TODO`, …);
  values of 8 or more characters with low entropy or a repeating pattern; lines that are not
  valid UTF-8 or contain NUL. Every finding is CRITICAL.
- **Anchors.** A finding is named by its location, `file:line`. As an argument, the file part
  resolves like any path (§4.5): `/home/user/Workspace/configs/myapp/app.properties:3`,
  `./app.properties:3`. ws stores it workspace-relative: `configs/myapp/app.properties:3`.
- **An allow covers a line number, not its content.** It survives edits that keep the
  finding on the same line. When lines are inserted or removed above it, the finding moves,
  appears again at its new line, and needs another `secret allow`. The old anchor is then
  stale, and the next `secret allow` drops it. If an allowed line later holds a different
  secret, the allow still hides it.

#### `ws secret ls`

```text
ws secret ls [--skip-dir <dir>]… [--filter <expr>] [--sort <fields>] [--wide] [--json]
```

Lists the findings that are not allowed. It never writes `ws.json` (decision: listing has no side effects).

| Flag | Default | Meaning |
| --- | --- | --- |
| `--skip-dir <dir>` | none | also skip this directory, for this run only (repeatable) |
| `--filter`, `--sort`, `--wide` | — | as in §4.3 |

Fields: `severity`, `location` (`/abs/file:line`), `snippet` (Match, the trimmed line), `path`,
`line`, `type`, `group`, `message`. Default columns: severity, location, snippet.

```text
$ ws secret ls
SEVERITY  LOCATION                                              MATCH
CRITICAL  /home/user/Workspace/configs/myapp/app.properties:3   password=hunter2
CRITICAL  /home/user/Workspace/notes/deploy.md:14               token: ghp_x8Kq…
```

JSON: `secret.ls`. `data` is the array of records (decision). The pass and credential data
are in `pass status`, `pass audit` and `credential status`. `skipped_dirs` is `secret.skip_dirs` in
`ws config show`.

Exit: 0 with or without findings; 1 on bad flags, an unknown field, or a `--skip-dir`
outside the workspace.

TUI: the screen.

#### `ws secret show`

```text
ws secret show <file:line>
```

Prints one finding in full: its fields, then 5 lines before and after it, with the match
marked.

```text
$ ws secret show ~/Workspace/configs/myapp/app.properties:3
/home/user/Workspace/configs/myapp/app.properties:3  CRITICAL  potential secret detected
     1  # app settings
     2  user=admin
 →   3  password=hunter2
     4  port=8080
```

The target must be a current finding. An allowed line, or a line with no match, exits 1 with
`no finding at /home/user/Workspace/configs/myapp/app.properties:3`.

JSON: `secret.show`. The record's fields plus `context`: `[{"number", "content", "is_match"}]`.

Exit: 0, 1.

TUI: `enter` opens the same view; `esc` returns.

#### `ws secret allow`

```text
ws secret allow <file:line…> [--dry-run]
```

Marks findings as false positives by adding their anchors to `ws.json` `secret.allowlist`.
Allowed findings no longer appear in `secret ls`. At least one target is required
(decision: allowlisting every secret at once must never happen by accident, least of all
under `--quiet`).

Each target must be a current finding. If one is not, ws says so and exits 1 without changing
anything.

Plan: one action per finding, pre-checked:
`Allow /home/user/Workspace/configs/myapp/app.properties:3 (false positive)`. When the
allowlist holds stale anchors (no pattern matches that line any more), one more action comes
last: `Drop 2 stale anchors from secret.allowlist`.

JSON: `secret.allow`, `data.actions`.

Exit: 0, 1, 3.

TUI: `a` on the selected rows. Secrets has nothing to add, so here `a` means allow.

#### `ws secret mute`

```text
ws secret mute <dir…> [--dry-run]
```

Adds directories to `ws.json` `secret.skip_dirs`, stored workspace-relative. `secret ls` does
not scan muted directories. Like `ignore mute`, this does not change what syncs.

Each target must be an existing directory inside the workspace, and not the workspace itself.
A directory that is already muted, by itself or by a parent, gets no action.

Plan: one action per directory, pre-checked: `Mute /home/user/Workspace/references/vendor in secret scans`.

JSON: `secret.mute`, `data.actions`.

Exit: 0, 1, 3.

TUI: `m` prompts for a directory, pre-filled with the cursor finding's directory.

#### `ws secret unmute`

```text
ws secret unmute <dir…> [--dry-run]
```

Removes directories from `secret.skip_dirs`, however the entry is written there
(relative, absolute or `~`). If the directory is muted by a parent entry, ws names the parent
and changes nothing. A directory that is not muted means nothing to do.

Plan: one action per directory, pre-checked: `Unmute /home/user/Workspace/references/vendor in secret scans`.

JSON: `secret.unmute`, `data.actions`.

Exit: 0, 1, 3.

TUI: `:unmute <dir>`.

#### `ws secret store`

```text
ws secret store <file:line> [entry] [--dry-run]
```

Copies a finding's value into pass, then allows the finding. It needs an initialized store
(`ws pass init`) and exactly one target, which must be a current finding.

- **The value** is the text after the keyword and its `:` or `=`, without surrounding quotes
  or a trailing `#` or `//` comment. A finding without a key and value (a private key
  header, a bare provider token) has no value to read: ws exits 1, and you store it with
  `pass insert <entry>`.
- **The entry name** is `entry`, or else a suggestion: the file's parent directory, the file
  name without its extension, and the keyword. For example, `configs/myapp/app.properties`
  with `password=` gives `myapp/app/password`. Spaces, `:` and `\` become `_`, and leading
  dots are dropped.
- **An existing entry is never overwritten** (decision). The action fails, and you pick
  another name.

Plan: one action, pre-checked:
`Store the value of /home/user/Workspace/configs/myapp/app.properties:3 in pass as myapp/app/password, then allow it`.
The value goes to `pass insert` on stdin, never on a command line.

The file is not rewritten. The secret stays in it until you change it, so ws ends with a hint:
replace the value with `$(pass show myapp/app/password)` or an environment variable, and rotate
the credential if the file has already synced.

JSON: `secret.store`. `data.actions` and `data.entry`.

Exit: 0, 1.

TUI: `s` prompts for the entry name, pre-filled with the suggestion, then shows the checklist.

---

### `ws pass`

`pass` is the pass(1) password store on this machine (`$PASSWORD_STORE_DIR`, else
`~/.password-store`). It holds the values that `secret store` takes out of files, and the git
tokens that `ws credential` serves. It has no rows: its health is the Secrets header, and
`:pass …` runs its commands on the Secrets screen. ws runs `pass` and `gpg` to do the work.
The `pass` commands work without an initialized workspace (decision), because the store
belongs to the machine.

| Command | RO/RW | Does | TUI |
| --- | --- | --- | --- |
| `ws pass init` | RW | create the store with a GPG key, and turn on git versioning | `:pass init` |
| `ws pass status` | RO | gpg, pass, store, git, remote, credential helper | Secrets header, `:pass status` |
| `ws pass audit` | RO | problems with the store and its entries | `:pass audit` |
| `ws pass backup enable [--remote <url>]` | RW | set up the private git backup | `:pass backup enable` |
| `ws pass backup disable` | RW | stop backing up | `:pass backup disable` |
| `ws pass backup status` | RO | remote, ahead/behind, recent commits | `:pass backup status` |
| `ws pass backup push` | RW | commit anything pending and push | `:pass backup push` |

#### `ws pass init`

```text
ws pass init [--dry-run]
```

Creates the store. If `gpg` or `pass` is missing, ws exits 1 and names the package (`gnupg`,
`pass`). If there is no GPG secret key, ws exits 1 and suggests `gpg --full-generate-key`.
With one key, ws uses it. With several, ws lists them and asks for a number; `--quiet` and
`--json` take the first and print which.

Plan, each action only when needed, pre-checked:

1. `Initialize the pass store /home/user/.password-store with GPG key 3AA5C34371567BD2` (`pass init <id>`)
2. `Turn on git versioning for the pass store` (`pass git init`)

A store that is initialized and git-backed means nothing to do. `pass init` does not ask for
a remote; that is `ws pass backup enable --remote <url>`.

JSON: `pass.init`. `data.actions`, and `data.pass` (the `pass status` keys).

Exit: 0, 1, 3.

TUI: `:pass init` hands over the terminal.

Compatibility: `--git-remote <url>` still works, hidden, and adds the
`pass backup enable` action.

#### `ws pass status`

```text
ws pass status [--json]
```

Reports, one line each: gpg available, pass installed, store initialized (path and entry
count), git-backed, remote (its URL, or `no remote: secrets live only on this machine`), and
the credential helper (`connected`, `stale` or `not connected`, by the same check as
`credential status`). A hint for the first missing piece ends it: install the package,
`ws pass init`, `ws pass backup enable --remote <url>`, or `ws credential enable`.

```text
$ ws pass status
✔ gpg available
✔ pass installed
✔ store initialized  /home/user/.password-store, 42 entries
✔ git-backed
▲ no remote          secrets live only on this machine
✔ credential helper  connected
  Next: ws pass backup enable --remote <url>   (use a private repository)
ℹ workspace copy     none: the store syncs only through its git remote
  Tip: ws symlink add ~/.password-store --dir
```

JSON: `pass.status`. Keys: `gpg_available`, `pass_installed`, `initialized`, `store_path`,
`entry_count`, `git_backed`, `git_remote`, `remote_url`, `cred_helper`, `cred_helper_value`.

Exit: 0. A missing piece is reported, not an error.

TUI: the Secrets header shows it on one line (`✔ pass  ✔ store (42 entries)  ✔ git  ▲ no
remote  ✔ credential helper`). `:pass status` opens the full report as a view.

#### `ws pass audit`

```text
ws pass audit [--json]
```

Inspects the store without decrypting anything (it reads file names and sizes only). It
reports: the store is not initialized, not git-backed, has no remote, or is empty; and each
entry under `git/` whose file is under 450 bytes, which likely holds a token without the
`username:` line that the credential helper uses.

```text
$ ws pass audit
▲ git/github.com  entry may lack metadata: add `username: <user>` on line 2
```

JSON: `pass.audit`. `data.findings`: `[{"entry", "message"}]` (`entry` is empty for store-wide
findings).

Exit: 0 with or without findings.

TUI: `:pass audit` opens the findings as a view; `esc` returns.

#### `ws pass backup`

The pass store is a git repository (`pass git …`). `ws pass backup` pushes it to a private
remote with four verbs: `enable`, `disable`, `status` and `push`. pass commits every change
itself, so there is no auto-commit setting. Pushes happen through `backup push`, or the
`pass-backup` cron job every 30 minutes.

**Private remotes only.** Before `backup enable` saves a remote, and before every push
(`backup push` and the cron job), ws asks the git host's API whether the repository is
private (§7.4). It knows GitHub, GitLab and Bitbucket, from HTTPS or SSH URLs. If the API
reports the repository public, ws refuses. If an authenticated lookup finds no repository,
ws refuses with `repository not found`. If visibility cannot be determined (an unknown or
self-hosted host, an unauthenticated lookup that returns 404, a network error), ws prints a
warning and goes on. No flag or setting skips the check. The API token comes from pass at
run time: `pass_entry` if set, else `git/<host>`. ws never stores it. git authenticates the
push itself, through your git credential setup. `ws credential enable` makes git read the
same pass entries.

ws never force-pushes and never rewrites history. If the remote rejects a push (it has
commits this repo lacks), the push fails and the commit stays local.

Settings, under `pass` in `ws.json`:

| Key | Default | Meaning |
| --- | --- | --- |
| `pass_entry` | none | the pass entry holding an API token for the visibility check (default `git/<host>`) |

##### `ws pass backup enable`

```text
ws pass backup enable [--remote <url>] [--pass-entry <entry>] [--dry-run]
```

Turns the backup on. It needs an initialized store (`ws pass init`). A remote is required.
ws takes the URL from `--remote`, else the store's existing `origin`, else a prompt:
`Remote URL of a PRIVATE repository`. If there is still no URL (a blank answer, or
`--quiet`/`--json` without `--remote`), it exits 1. Then the visibility check runs, and a
refused remote exits 1 before any change.

| Action | Planned when |
| --- | --- |
| `Turn on git versioning for the pass store` (`pass git init`) | it is not a repo yet |
| `Set the backup remote to <url>` (the store's `origin`, and `pass_entry` if given) | the URL or pass entry changes |

Every action is pre-checked. If backup is already enabled with nothing to change, ws says so
and exits 0. enable does not push. It ends with two hints:
`ws pass backup push` pushes now, and `ws cron add pass-backup` pushes every 30 minutes.

| Flag | Default | Meaning |
| --- | --- | --- |
| `--remote <url>` | see above | the private remote |
| `--pass-entry <entry>` | `git/<host>` | the pass entry with an API token for the visibility check |

JSON: `pass.backup.enable`, with `data.actions`, `data.remote_url`, and `data.visibility`.

Exit: 0, 1, 3.

TUI: `:pass backup enable [--remote <url>]` hands over the terminal (it may prompt).

##### `ws pass backup disable`

```text
ws pass backup disable [--dry-run]
```

Turns the backup off.

| Action | Planned when |
| --- | --- |
| `Remove the backup remote <url>` (the store's `origin`) | a remote is set |
| `Remove the pass-backup cron job` | the job is installed |

Both are pre-checked. The store, its entries and its history stay, and the remote repository
itself is not touched. If commits are not pushed yet, ws says how many, and they stay local.
When backup is not enabled, ws says so and exits 0.

JSON: `pass.backup.disable`, `data.actions`.

Exit: 0, 1, 3.

TUI: `:pass backup disable` opens the checklist.

##### `ws pass backup status`

```text
ws pass backup status [-n <count>]
```

Reports the backup's state. It does not
contact the remote: ahead and behind compare with the remote branch as of the last push or
fetch.

| Flag | Default | Meaning |
| --- | --- | --- |
| `-n <count>` | `20` | recent commits to show |

```text
Backup        enabled
Repository    /home/user/.password-store  (42 entries)
Remote        git@github.com:user/pass.git
Branch        main
Ahead/behind  ↑3 ↓0
Last commit   2026-10-21 18:02:11 +0530  Add given password for git/github.com to store.

Recent commits
  4be91d0 Add given password for git/github.com to store.
  77c2a1e Edit password for aws/prod using editor.
```

When backup is off, it prints `Backup  disabled` and the hint
`ws pass backup enable --remote <url>`.

JSON: `pass.backup.status`. `data` has `enabled`, `initialized`, `repo_path`, `entry_count`,
`remote_url`, `branch`, `ahead`, `behind`, `pass_entry`, `uncommitted`, `last_commit` and
`commits` (`hash`, `subject`).

Exit: 0, whatever the state.

TUI: `:pass backup status` opens a scrolling view; `esc` returns.

##### `ws pass backup push`

```text
ws pass backup push [-m <message>] [--dry-run]
```

Commits anything pending in the store (normally nothing, because pass commits each change),
runs the visibility check, and pushes the branch with `git push -u origin <branch>`. If the
push fails, the commit stays local. Backup must be enabled.

One action, pre-checked: `Commit pending changes and push main to git@github.com:user/pass.git`.

| Flag | Default | Meaning |
| --- | --- | --- |
| `-m <message>` | `ws pass backup push` | the commit message, if there is anything to commit |

JSON: `pass.backup.push`, with `data.actions`, `data.committed`, `data.pushed`,
`data.remote_url`, `data.branch`.

Exit: 0. 1 when backup is not enabled, the remote is refused, or the commit or push fails.

TUI: `:pass backup push` opens the checklist.

Cron: the `pass-backup` job runs `ws pass backup push --quiet` every 30 minutes.

#### A second copy in the workspace

The pass store syncs only through its git remote. Keeping it inside the workspace adds a
second copy, which the sync client syncs with everything else:

```text
ws symlink add ~/.password-store --dir
```

The store and its git history move into `symlinks/home/.password-store`, and
`~/.password-store` becomes a symlink to it, so `pass` works as before. The pass store is on
the catalog's never-suggested list, so `ws symlink add` never offers it: this is a command
you type on purpose, and it is a second sync path next to the store's own remote.

ws suggests this gently, and only where you'd look anyway:

- `pass status` ends with an info line, `ℹ workspace copy: none`, and the command above.
  When the store is already a symlink into the workspace, the line reads
  `✔ workspace copy  /home/user/Workspace/symlinks/home/.password-store`.
- `pass init` and `pass backup enable` end with the same one-line tip.
- The Secrets header ends with a dim `ℹ no workspace copy`.
- The Dashboard lists it as advice: last, at info level, never as a warning (§6.5). `enter`
  pre-fills the command in the command bar.

It is never an error or a warning, it never changes an exit code, and `pass audit` doesn't
report it. (decision, by the user: advise without shouting.)

---

### `ws credential`

`credential` is ws acting as git's credential helper: when git needs HTTPS credentials, it
asks ws, and ws answers from pass. It has no rows: its health shows in the Secrets header,
and `:credential …` runs its commands on the Secrets screen. It works without a workspace.
In a workspace, `enable` and `status` also look at the remotes of the workspace repos.

| Command | RO/RW | Does | TUI |
| --- | --- | --- | --- |
| `ws credential enable` | RW | make git use ws (pass) for credentials | `:credential enable` |
| `ws credential disable` | RW | stop it | `:credential disable` |
| `ws credential status` | RO | helper config, pass entries, remote coverage | Secrets header, `:credential status` |

#### Lookup

When git runs the helper, ws tries these pass entries in order and uses the first one that
exists:

1. `git/<host>/<owner>/<repo>`, a repo token. git sends the repo path only when
   `credential.useHttpPath` is true (globally, or for that URL).
2. `git/<host>`, a host token shared by every repo on that host.

Line 1 of the entry is the password or token. A later `username:`, `user:` or `login:` line
gives the username. If nothing matches, ws prints nothing, and git tries its next helper or
prompts. SSH remotes never ask a credential helper.

#### `ws credential enable`

```text
ws credential enable [--dry-run]
```

It needs an initialized pass store (`ws pass init`), and exits 1 without one.

Plan, each action only when needed, pre-checked:

1. `Install the helper wrapper /home/user/.local/bin/ws-credential-helper`. It is a two-line
   `sh` script that runs `ws git-credential-helper "$@"`, so git's config survives moving the
   ws binary.
2. `Set global credential.helper to '!/home/user/.local/bin/ws-credential-helper'` (or update a
   stale ws value).
3. `Set global credential.useHttpPath = true`.
4. One per workspace repo whose local `credential.helper` is a stale ws value:
   `Update credential.helper in /home/user/Workspace/experiments/api`.
5. One per host whose HTTPS remotes resolve to no entry:
   `Create pass entry git/gitlab.work.com (host token for 2 remotes)`. It runs `pass insert`,
   which asks for the token on the terminal. Without a terminal, the action fails (decision).

Already enabled, with every remote covered, means nothing to do.

JSON: `credential.enable`, `data.actions`.

Exit: 0, 1, 3.

TUI: `:credential enable` hands over the terminal.

#### `ws credential disable`

```text
ws credential disable [--dry-run]
```

Plan, each action only when needed, pre-checked:

1. `Remove credential.helper from global git config` (when it is ws).
2. `Remove credential.useHttpPath from global git config` (when it is true; skip it to keep
   the setting).
3. One per workspace repo with a ws `credential.helper` in its local config:
   `Remove credential.helper from /home/user/Workspace/experiments/api (local config)`.

The wrapper script stays (it does nothing on its own). Not enabled means nothing to do,
with exit 0 (decision).

JSON: `credential.disable`, `data.actions`.

Exit: 0, 1, 3.

TUI: `:credential disable` hands over the terminal.

#### `ws credential status`

```text
ws credential status [--json]
```

Reports the pass store health; the global helper (`connected`, `stale` when the wrapper or
binary is missing or written in an old broken form, or `disconnected`) and its value; repos
with a local `credential.helper`; and, for each workspace remote, the entry it resolves to
(host or repo token), `no credential`, or `credential helper not used` for SSH. It also lists
repo tokens that git never asks for because `credential.useHttpPath` is off. When something is
missing, the last line names `ws credential enable`.

JSON: `credential.status`. Keys: `connected`, `helper`, `pass` (the `pass status` health),
`remotes` (`host`, `path`, `transport`, `helper_used`, `pass_entry`, `scope`, `exists`,
`ignored_entry`), `local_overrides` (`repo_path`, `helper`, `is_ws`).

Exit: 0.

TUI: the Secrets header shows `credential helper` connected or not. `:credential status` shows
the full report, paged.

#### Plumbing: `ws git-credential-helper get|store|erase`

These are what git runs. They are hidden and unchanged, never print anything except the
answer, and need no workspace. `get` reads git's request (`protocol=`, `host=`, `path=`) on
stdin and prints `username=` and `password=` by the lookup above. `store` and `erase` read
stdin and do nothing: entries are managed with `ws secret store`, `ws credential enable` or
`pass insert`. Any other operation exits 0 silently. `ws credential get|store|erase` does the
same, for git configs written by older versions (decision).

---

### `ws cron`

Scheduled ws jobs. A row is a built-in job. Its state comes from three places:
the live user crontab (`crontab -l`), the declared crontab `<workspace>/configs/crontab`
(synced with the workspace), and the run history in `$XDG_STATE_HOME/ws-tool/cron.state`
(default `~/.local/state/ws-tool/`). The Cron screen shows the same rows.

| Command | RO/RW | Does | TUI |
| --- | --- | --- | --- |
| `ws cron ls` | RO | jobs: schedule, state, last run, exit, next run | screen |
| `ws cron add <job…>` | RW | install in the live crontab and declare in `configs/crontab` | `a` |
| `ws cron rm <job…>` | RW | remove from the live crontab and from `configs/crontab` | `ctrl+d` |
| `ws cron run <job>` | RW | run the job now and record the result | `r` |
| `ws cron log [job…] [--lines n]` | RO | recent lines of the cron log | enter |
| `ws cron apply` | RW | make the live crontab match `configs/crontab` | `:apply` |

**Jobs.** Each job runs one command in the workspace. `<ws>` is the absolute path of the ws
binary and `<W>` the absolute workspace path.

| Job | Schedule | Interval | Runs |
| --- | --- | --- | --- |
| `mega-sync` | `*/30 * * * *` | 30m | MEGA only: restarts `megasync` for a 5-minute sync window, using `DISPLAY` |
| `pass-backup` | `*/30 * * * *` | 30m | `<ws> --workspace <W> pass backup push --quiet` |
| `sync-push` | `*/30 * * * *` | 30m | `<ws> --workspace <W> sync push --quiet` (§5 `ws sync`) |

Each job is installed on its own. `mega-sync` suits MEGA; with another sync client,
install `pass-backup` alone, plus `sync-push` for a mirror client such as WorkDrive (§2.6).

**Retired names.** `secret-scan`, `ignore-scan`, `repo-sync` (nothing should commit your
work unattended; run `ws repo git` yourself) and `zoho-sync` (Zoho's TrueSync client runs from login autostart; the old Sync client it restarted is discontinued) are retired. `cron add` rejects them (`cron job
"repo-sync" was removed`), `cron rm` still removes their blocks, and `cron run` skips
them silently and exits 0, so a crontab line written earlier keeps working (see below).

**Crontab block.** `cron add` writes one marked block per job, the same in both files:

```text
# >>> ws:mega-sync >>>
SHELL=/usr/bin/bash
*/30 * * * * /home/user/.local/bin/ws --workspace /home/user/Workspace cron run --if-due --display :1 mega-sync
@reboot /home/user/.local/bin/ws --workspace /home/user/Workspace cron run --if-due --display :1 mega-sync
# <<< ws:mega-sync <<<
```

ws owns everything between `# >>> ws:<name> >>>` and `# <<< ws:<name> <<<` for any `<name>`.
Lines outside the markers are never changed. The lines carry `--workspace` so a job does not
depend on `$WS_WORKSPACE`, which cron does not set (decision). Only `mega-sync` carries
`--display`.

**Missed-run recovery.** Both lines pass `--if-due`. A job is due when no run is recorded, or when
the last run is older than half the interval. Otherwise the line logs a skip and exits 0. The
`@reboot` line runs a due job at boot, which catches up on slots missed while the machine was off.
The scheduled line skips when a run happened moments before, for example at boot. `@reboot` fires at boot
only, not after resume from suspend. After a suspend, the next scheduled line runs the job
because it is due.

**`configs/crontab`** is the declared crontab. `cron add` and `cron rm` keep it in step with the
live crontab. `cron apply` installs it on a machine, for example a new machine whose workspace
arrived through sync. It may also hold lines that ws does not own, such as `CRON_TZ=UTC`.

**Files.** `cron.state` holds one JSON record per finished run: `{"job":"mega-sync","ts":"2026-10-22T07:05:01Z","exit":0}`.
`cron.log` holds timestamped lines: `2026-10-22T07:00:01Z [mega-sync] Starting`. Both are in
`$XDG_STATE_HOME/ws-tool/` and are machine-local.

**Preflight.** Before any command reads or writes the live crontab, ws checks that `crontab` exists
and the user may use it. If it fails, `add`, `rm` and `apply` exit 1 with a fix, for example
"add siva to /etc/cron.allow". `ls` shows `crontab n/a` instead.

#### `ws cron ls`

```text
ws cron ls [--sort <field>] [--filter <expr>] [--wide] [--json]
```

Lists every built-in job, then every retired job that still has a block in
either crontab, each group sorted by name. It reads the live crontab, `configs/crontab` and
`cron.state`, and changes nothing. It replaces `cron status`.

| Field | Meaning |
| --- | --- |
| `name` | job name |
| `schedule` | cron expression |
| `state` | see below |
| `installed` | a block for the job is in the live crontab (bool) |
| `declared` | a block for the job is in `configs/crontab` (bool) |
| `last_run` | time of the last recorded run (RFC 3339; text "12m ago") |
| `last_exit` | exit code of the last run |
| `next_run` | last run + interval, for installed jobs only; text "in 18m", or `overdue` when past |
| `description` | one line |

`state` (decision) is the first match:

| State | Means | Fix |
| --- | --- | --- |
| `crontab n/a` | preflight failed | see the stderr line |
| `retired` | the job no longer exists but its block remains | `ws cron rm <name>` |
| `outdated` | the live block uses an old job name, or lacks `--if-due` | `ws cron add <job>` |
| `undeclared` | in the live crontab, not in `configs/crontab` | `ws cron add <job>` |
| `not applied` | in `configs/crontab`, not in the live crontab | `ws cron apply` |
| `installed` | in both, current form | — |
| `not installed` | in neither | `ws cron add <job>` |

Default columns: `name`,
`schedule`, `state`, `last_run`, `next_run`, `description` (decision: `next_run` added).

```text
$ ws cron ls
 Job             Schedule      State          Last run          Next run  Description
 mega-sync       */30 * * * *  installed      12m ago           in 18m    Restart megasync for a 5-minute sync window
 pass-backup     */30 * * * *  not installed                              Push the pass store backup
```

`--json`: command `cron.ls`. `data` is the array of records with the field keys above (decision: no
`jobs` wrapper, the same shape as every other `ls`).

Exit: 0. 1 only for bad flags. An unreadable crontab is not an error: rows show `crontab n/a`
and stderr gets one line with the reason.

TUI: the Cron screen list. Its header reads "2 jobs installed · 1 failing". `d` shows every field.

#### `ws cron add`

```text
ws cron add <job…> [--display <d>] [--dry-run]
```

Installs each named job: it writes the job's block into the live crontab and into
`configs/crontab`, creating that file if missing (decision: both files stay in step). Each job is handled once. It needs at least one
target. An unknown or retired name exits 1 before any change.

Plan: one action per job that needs a change. Each action covers both files (decision), so the
two cannot drift apart. All actions are pre-checked. A job that is already `installed` gets no
action and is reported as "already installed". An `outdated`, `undeclared` or `not applied` job is
written fresh, and a block under its old name is removed in both files. Lines outside ws blocks
are untouched.

| Flag | Default | Meaning |
| --- | --- | --- |
| `--display <d>` | `$DISPLAY`, else `:1` | X display written into the `mega-sync` line; ignored for other jobs |
| `--dry-run` | false | print the plan; change nothing |

```text
$ ws cron add mega-sync pass-backup
  Install mega-sync (*/30 * * * * + @reboot) in the crontab and /home/user/Workspace/configs/crontab  [y/n/a/q] a
  ✔ Install mega-sync (*/30 * * * * + @reboot) in the crontab and /home/user/Workspace/configs/crontab
  ✔ Install pass-backup (*/30 * * * * + @reboot) in the crontab and /home/user/Workspace/configs/crontab
✔ 2 cron jobs installed.
```

`--json`: command `cron.add`; data `jobs` (`name`, `schedule`), `actions`.

Exit: 0, 1, 3.

TUI: `a` acts on the marked rows, else the cursor row, and opens the same checklist after the preflight.

#### `ws cron rm`

```text
ws cron rm <job…> [--dry-run]
```

Removes each named job's block, under its current and old names, from the live crontab and from
`configs/crontab`. It also deletes the job's legacy wrapper script
`$XDG_STATE_HOME/ws-tool/cron-jobs/<name>.sh` if one is there. It accepts retired names. It needs at least one target. Run history and log lines are kept.

Plan: one action per job that has a block in either file, pre-checked. A job with no block is
reported as "not installed" and gets no action.

| Flag | Default | Meaning |
| --- | --- | --- |
| `--dry-run` | false | print the plan; change nothing |

`--json`: command `cron.rm`; data `removed` (names), `actions`.

Exit: 0, 1, 3.

TUI: `ctrl+d` acts on the marked rows, else the cursor row, and opens the checklist.

#### `ws cron run`

```text
ws cron run <job> [--if-due] [--display <d>] [--dry-run]
```

Runs one job now in the workspace and records the result. Crontab lines call it, so it never
prompts. The command is the confirmation.

1. With `--if-due`, it skips when the job's last recorded run, under any of its names, is
   newer than half its interval. It logs `Skipping: last run 4m ago (< half-interval 15m)` and exits 0.
   Without `--if-due`, the job always runs (decision: `run` means execute now).
2. It logs `Starting` and runs the job's command with bash. Each line of the job's stdout and
   stderr goes to `cron.log` as `<time> [<job>] <line>` (decision: the output is kept), and
   also to ws's own stdout and stderr unless `--json` is given.
3. It appends a record to `cron.state` and logs `Done (exit 0)` or `Failed (exit 1)`.

Before writing, if `cron.log` is over 5 MB, ws keeps its last 2.5 MB. An old job name runs
silently as the new job. A retired name logs
`Skipping: secret-scan is no longer a ws job; run ws cron rm secret-scan` and exits 0, so old
crontab lines do not fail (decision).

| Flag | Default | Meaning |
| --- | --- | --- |
| `--if-due` | false | run only when the job is due (used by ws's crontab lines) |
| `--display <d>` | `$DISPLAY`, else `:1` | X display for `mega-sync` |
| `--dry-run` | false | print the job's command; run and record nothing |

`--json`: command `cron.run`; data `job`, `exit_code` (the job's own code), `skipped` (bool).

Exit: 0 when the job exited 0 or was skipped. 1 when the job failed or the name is unknown
(decision: the job's own code is kept in `cron.state`, in `cron ls` `last_exit`, and in the JSON
`exit_code`, not passed through).

TUI: `r` acts on the marked rows, else the cursor row. A checklist asks "Run mega-sync now",
one action per job. The job then runs in
the background with its output going only to `cron.log`. A notice reports the exit code, and the
row refreshes.

#### `ws cron log`

```text
ws cron log [job…] [--lines <n>]
```

Prints the last `n` lines of `cron.log` for the named jobs, including lines under their old names,
or for all jobs when none is named (decision: the job stays optional, per rule 4).
It reads only `cron.log`.

| Flag | Default | Meaning |
| --- | --- | --- |
| `--lines <n>` | 20 | lines to show; 0 shows all |

```text
$ ws cron log pass-backup --lines 3
2026-10-22T07:00:01Z [pass-backup] Starting
2026-10-22T07:00:02Z [pass-backup] error: failed to push some refs to 'origin'
2026-10-22T07:00:02Z [pass-backup] Failed (exit 1)
```

`--json`: command `cron.log`; data `jobs` (names, empty for all), `lines`.

Exit: 0, including for an empty log ("No cron log entries."). 1 for an unknown job.

TUI: enter on a job row opens its log: the last 200 lines, scrolled to the end. esc goes back.

#### `ws cron apply`

```text
ws cron apply [--dry-run]
```

Writes the declared state into the live crontab. It reads `configs/crontab` and does three things:

- It installs every declared block of a built-in job (old names count) as `cron add` would write it
  on this machine: this binary, this workspace, the built-in schedule, and `$DISPLAY` or `:1` for
  `mega-sync` (decision: blocks are regenerated, not copied, so a workspace synced from another
  machine gets correct paths).
- It removes live ws blocks that are not declared.
- It appends declared lines outside ws blocks, such as `CRON_TZ=UTC`, that the live crontab lacks.
  Each line is added once.

Live lines outside ws blocks are kept. Declared blocks of retired or unknown jobs are skipped and
reported. `configs/crontab` itself is not changed.

Plan: one action per job installed or replaced, one per block removed, and one for the added lines.
All are pre-checked. Blocks that already match get no action. When there is nothing to do, it
prints "The crontab already matches /home/user/Workspace/configs/crontab."

| Flag | Default | Meaning |
| --- | --- | --- |
| `--dry-run` | false | print the plan; change nothing |

`--json`: command `cron.apply`; data `installed`, `removed`, `unchanged`, `skipped` (names),
`lines_added` (count), `actions`.

Exit: 0, 1, 3. A missing `configs/crontab` exits 1:
"/home/user/Workspace/configs/crontab not found; ws cron add <job> creates it".

TUI: `:apply` opens the same checklist.

### `ws trash`

The trash is the user's XDG trash: `$XDG_DATA_HOME/Trash`, default `~/.local/share/Trash`. It uses
the freedesktop layout, with items in `files/<name>` and their records in
`info/<name>.trashinfo`. ws always uses this directory (decision: no other trash root). ws's own
deletes go there, and so do the shell `rm` integration, VS Code and the desktop file manager, so
one list shows them all. A row is a top-level entry of `files/`. The Trash screen lists the rows,
and its header shows `trash status`.

**How ws soft-deletes.** ws moves the path to `files/<base name>`, adding `.2`, `.3`, … on a
collision, as gio does. It writes `info/<name>.trashinfo` with `Path=` (the absolute original
path, percent-encoded) and `DeletionDate=` (local time, `2026-10-22T11:25:03`). Across
filesystems it copies, then deletes. This is what `rm` means in other ws commands. No trash
command does it.

**Item arguments** (decision). An `<item>` is the item's name (the `name` column), or the path it
was deleted from. An argument that contains `/` or starts with `~` is a path. It is resolved by the
path rules and matches the most recently deleted item with that original path. Any argument that
matches nothing is an error, and nothing runs.

None of these commands needs an initialized workspace (decision).

| Command | RO/RW | Does | TUI |
| --- | --- | --- | --- |
| `ws trash ls` | RO | trashed items, newest first | screen |
| `ws trash restore <item…>` | RW | move items back to their original paths | `r` |
| `ws trash rm <item…>` | RW | delete items forever | `ctrl+d` |
| `ws trash empty` | RW | delete every item forever | `:empty` |
| `ws trash prune --older-than <age>` | RW | delete forever the items trashed before the age | `p` |
| `ws trash enable [--no-shell-rm] [--no-vscode] [--no-file-explorer]` | RW | turn on soft-delete integrations | `:enable` |
| `ws trash disable` | RW | turn off the shell `rm` integration | `:disable` |
| `ws trash status` | RO | integrations, trash location and totals | screen header |

`status` reports the machine: which integrations route deletes into the trash, where the trash
is, and how big it is. It is one record, not a list. `ls` lists what is in the trash.

#### `ws trash ls`

```text
ws trash ls [--sort <field>] [--filter <expr>] [--wide] [--json]
```

Lists the top-level entries of `files/`, newest first. The `.trashinfo` gives each entry's
original path and deletion date. An entry without one shows no original path, and its date falls
back to the file's mtime. A missing trash directory lists nothing.

| Field | Meaning |
| --- | --- |
| `name` | entry name under `files/` (text: `build/` for a directory) |
| `original_path` | where it was deleted from (absolute) |
| `size_bytes` | total size |
| `files` | files inside (a file counts as 1) |
| `deleted_at` | deletion time (text "3h ago") |
| `dir` | bool |
| `path` | absolute path under `files/` |

Default columns: `name`, `original_path`, `size_bytes`, `deleted_at`.

```text
$ ws trash ls
 Name          Deleted from                                    Size     Deleted
 old-notes.md  /home/user/Workspace/notes/old-notes.md         2.0 KB   3h ago
 build/        /home/user/Workspace/experiments/demo/build     3.0 MB   2d ago
```

`--json`: command `trash.ls`; data is the array of records.

Exit: 0. 1 for bad flags or an unreadable trash directory.

TUI: the Trash screen list.

#### `ws trash restore`

```text
ws trash restore <item…> [--dry-run]
```

Puts each item back at the original path in its `.trashinfo`. It needs at least one target.
It follows the freedesktop trash spec:

- `Path=` is percent-decoded. A relative `Path=` is resolved against the directory that holds the
  trash directory.
- An item with no `.trashinfo` or no `Path=` cannot be restored. It is reported and skipped.
- ws never overwrites. If anything exists at the original path, including a dangling symlink, the
  item is reported and skipped. When two targets share an original path, the first one named is
  restored and the other is skipped. In the TUI the newest goes first.
- Missing parent directories are created with mode 0755, and the action names them (decision).
- ws moves `files/<name>` to the original path, copying across filesystems. Then it deletes
  `info/<name>.trashinfo` and the item's line in `directorysizes`, if there is one. If the move
  fails, both stay.

Plan: one action per restorable item, pre-checked. Skipped items are listed before the prompts.

| Flag | Default | Meaning |
| --- | --- | --- |
| `--dry-run` | false | print the plan; change nothing |

```text
$ ws trash restore old-notes.md build
  ! build: /home/user/Workspace/experiments/demo/build already exists; skipped
  Restore old-notes.md to /home/user/Workspace/notes/old-notes.md  [y/n/a/q] y
  ✔ Restore old-notes.md to /home/user/Workspace/notes/old-notes.md
```

`--json`: command `trash.restore`; data `restored` (`name`, `path`), `skipped` (`name`, `path`,
`reason`), `actions`.

Exit: 0 when every target was restored. 3 when some were restored and others were skipped or
failed. 1 when none was restored.

TUI: `r` acts on the marked rows, else the cursor row, and opens the checklist. Skipped rows appear in its note.

#### `ws trash rm`

```text
ws trash rm <item…> [--dry-run]
```

Deletes each item forever: `files/<name>`, its `.trashinfo`, and its `directorysizes` line. It needs
at least one target. Use `trash empty` for everything. Unlike `rm` elsewhere in ws, this delete
cannot be undone.

Plan: one action per item, pre-checked, for example
"Permanently delete build/ (3.0 MB), deleted from /home/user/Workspace/experiments/demo/build".

| Flag | Default | Meaning |
| --- | --- | --- |
| `--dry-run` | false | print the plan; change nothing |

`--json`: command `trash.rm`; data `removed` (names), `actions`.

Exit: 0, 1, 3.

TUI: `ctrl+d` acts on the marked rows, else the cursor row.

#### `ws trash empty`

```text
ws trash empty [--dry-run]
```

Deletes every item forever. It removes every entry in `files/` and `info/` (orphan `.trashinfo`
files included) and the `directorysizes` file. It keeps the `files/` and `info/` directories and
does not touch other directories such as `expunged/`. It takes no targets.

Plan: one action, "Permanently delete 12 items (3.0 MB) in /home/user/.local/share/Trash", pre-checked.
An empty trash gets no action: "Trash is empty."

| Flag | Default | Meaning |
| --- | --- | --- |
| `--dry-run` | false | print the plan; change nothing |

`--json`: command `trash.empty`; data `items`, `files`, `size_bytes`, `actions`.

Exit: 0, 1.

TUI: `:empty` always covers the whole trash and ignores marks and the filter (decision). To delete
only some rows, use `ctrl+d`.

#### `ws trash prune`

```text
ws trash prune --older-than <age> [--dry-run]
```

Deletes forever every item whose `DeletionDate` is older than `<age>`. The age is measured from
the `.trashinfo`, not the file's mtime. Items without a `DeletionDate` are never pruned, and the
output reports how many were skipped. Trash has no size cap in ws.json, so `--older-than` is
required.

Plan: one action per matched item, oldest first, pre-checked (decision).

| Flag | Default | Meaning |
| --- | --- | --- |
| `--older-than <age>` | required | `30d`, `12h`, `2w`; a bare number means days; must be positive |
| `--dry-run` | false | print the plan; change nothing |

`--json`: command `trash.prune`; data `matched` (`name`, `size_bytes`, `deleted_at`), `kept`,
`skipped`, `size_bytes`, `actions`.

Exit: 0, 1, 3. A missing or invalid `--older-than` exits 1.

TUI: `p` prompts for the age in the same format, then opens a checklist of the matched items.
Like the CLI, it covers the whole trash, whatever is marked or filtered.

#### `ws trash enable`

```text
ws trash enable [--no-shell-rm] [--no-vscode] [--no-file-explorer] [--dry-run]
```

Turns on the integrations that route deletes into the trash (decision for all three):

| Integration | On when | `enable` does |
| --- | --- | --- |
| `shell-rm` | `~/.local/bin/ws-trash-rm` is executable and an rc file has ws's alias line | writes the script, which moves each argument into the trash with a `.trashinfo`, and adds `command -v ws-trash-rm >/dev/null 2>&1 && alias rm='ws-trash-rm'` to each existing file among `~/.bashrc` and `~/.zshrc`, or creates the rc file for `$SHELL` if neither exists |
| `vscode` | VS Code's user `settings.json` does not set `files.enableTrash` to `false` | changes an explicit `false` to `true`, editing only that value |
| `file-explorer` | the trash's `files/` and `info/` exist | creates them with mode 0700 |

Plan: one action per integration that is off, pre-checked. A `--no-…` flag leaves its integration
out. When all are on, it prints "Soft-delete is already on." The guarded alias line keeps `rm`
working on a machine whose rc file arrived through sync but which has no `ws-trash-rm`.

| Flag | Default | Meaning |
| --- | --- | --- |
| `--no-shell-rm` | false | leave the shell integration out |
| `--no-vscode` | false | leave VS Code out |
| `--no-file-explorer` | false | leave the trash directories out |
| `--dry-run` | false | print the plan; change nothing |

`--json`: command `trash.enable`; data `enabled` (integration names), `actions`.

Exit: 0, 1, 3.

TUI: `:enable` opens the checklist with every integration that is off. Unticking one replaces the
`--no-…` flags.

#### `ws trash disable`

```text
ws trash disable [--dry-run]
```

Turns off the shell integration. It removes ws's alias line, in both its old unguarded form and its guarded form, from `~/.bashrc` and `~/.zshrc`,
then deletes `~/.local/bin/ws-trash-rm`. It also deletes the legacy state file
`$XDG_STATE_HOME/ws-tool/trash-setup.json` if present. It leaves the trash contents and
directories alone, and VS Code's setting too. Those are the system's defaults, and ws keeps
soft-deleting either way (decision).

Plan: one action for the shell integration (alias lines first, then the script) and one for the
legacy state file. Both are pre-checked.

| Flag | Default | Meaning |
| --- | --- | --- |
| `--dry-run` | false | print the plan; change nothing |

`--json`: command `trash.disable`; data `actions`.

Exit: 0, 1, 3.

TUI: `:disable`.

#### `ws trash status`

```text
ws trash status [--json]
```

Reports each integration from the live system, not from a state file (decision), along with the
trash directory and its totals. The size warning is `trash.warn_size_mb` (default 10 GiB); outside an initialized workspace the default applies.

```text
$ ws trash status
  Trash root  /home/user/.local/share/Trash

  ✔ shell-rm       rm moves files to the trash (/home/user/.bashrc, /home/user/.zshrc)
  ✔ vscode         VS Code deletes to the trash
  ✘ file-explorer  /home/user/.local/share/Trash/info is missing; run ws trash enable

  Items 12 · Files 340 · Size 3.0 MB · Warns at 1.0 GB
```

`--json`: command `trash.status`; data `root`, `integrations` (`name`, `enabled`, `detail`),
`items`, `files`, `size_bytes`, `warn_size_bytes`, `over_limit`.

Exit: 0. 1 when the trash cannot be read. Integrations that are off and a trash over the warning
size are reported, not a failure (exit 2 is retired).

TUI: the Trash screen header. It shows the same lines, with "over the limit" when the trash
passes 1 GB.

### `ws sync`

A one-way mirror for sync clients that can only sync their own folder (Zoho WorkDrive TrueSync,
OneDrive, a USB stick). `ws sync push` copies the workspace into `sync.mirror.target`, a
folder the client watches, and the client uploads it (Factor XII). The mirror is ws's: you
never edit it. MEGA does not need this noun (§2.6).

There is no TUI screen: `sync` is CLI and cron only (§6.9). Every command needs `rsync` on
`PATH`.

| Command | RO/RW | Does |
| --- | --- | --- |
| `ws sync push` | RW | mirror the workspace one way into `sync.mirror.target` |
| `ws sync status` | RO | target, last push, stamp state, pending changes and deletions |
| `ws sync restore [<mirror-dir>]` | RW | copy the mirror into the workspace, on a fresh machine |

Settings, under `sync` in `ws.json` (§2.3):

| Key | Default | Meaning |
| --- | --- | --- |
| `mirror.target` | none | the mirror folder, for example `~/ZohoWorkDrive/My Folders/Workspace` |
| `exclude` | `[]` | extra paths kept out of the mirror only; the pattern syntax of `ignore_rules.exclude` |
| `max_delete_pct` | `20` | the delete guard's threshold, in percent of the mirror's files |

**Checks**, before push and status. An empty `sync.mirror.target` is refused with a hint to
set it in `ws.json` (`ws config edit`). The target's parent must exist, so a moved or
reinstalled client never gets a stray folder. The target and the workspace must not
contain each other. `rsync` must be on `PATH`.

**What goes out.** ws's own ignore engine (§5 `ws ignore`) plus `sync.exclude` decides,
not rsync's rule syntax: ws walks the workspace and writes the exact list of synced paths
as an rsync filter (`--from0`). Symbolic links are skipped, and a warning line counts them
(`ws ignore lint` names them). File modes are not carried, and there is no manifest.

**The stamp.** `.ws-sync-stamp` in the mirror root holds `{push_id, host, time}`, and is
protected from deletion. The machine keeps its own record in
`$XDG_STATE_HOME/ws-tool/sync/` (`last-push.json`, plus `lock`, `filter` and rsync's
`tmp/`). Before each push:

| Mirror stamp | Means | Push |
| --- | --- | --- |
| matches `last-push.json` | normal | pushes |
| missing, but a last push exists | the mirror was wiped, locally or from the cloud | warns loudly ("mirror was wiped"), then pushes everything |
| another `push_id`, or a stamp with no local record | foreign: another machine pushes here | asks; refused under `--quiet`/`--json` |
| missing, no last push | first push | pushes |

**The delete guard.** A push that would delete more than `max_delete_pct`% of the mirror's
files, and more than 10 files, asks first. Like the foreign stamp, it is refused under
`--quiet`/`--json`, so cron never mass-deletes. There is no `--force` (§4.7).

**One push at a time.** The lock is `flock`. A second run prints that another push is
running and exits 0, so overlapping cron runs are harmless.

#### `ws sync push`

```text
ws sync push [--dry-run] [--quiet]
```

Runs the checks, reads the stamp, writes the filter, and does an rsync dry run to count
changes and deletions (the delete guard). Then it copies:

```bash
rsync -rt --update --delete --delete-excluded --from0 \
      --filter="P /.ws-sync-stamp" --filter="merge <state>/sync/filter" \
      --temp-dir=<state>/sync/tmp  <workspace>/  <target>/
```

`-rt`: no links, no modes, times kept so unchanged files are skipped. `--update`: a file whose
mirror copy is newer than the workspace file is skipped too. Virtual-drive clients (Zoho
TrueSync) stamp each upload with the upload time instead of the time rsync sets, so without
`--update` every file would look changed on every push. A file you replace with an older one
(`cp -p` of an old version) is not pushed until it is touched. `--delete-excluded`:
files deleted or newly ignored in the workspace leave the mirror. The temp dir is outside
the mirror, so the client never sees half-written files. Empty directories are mirrored.
Finally it writes the stamp and `last-push.json`, and reports the changed, deleted, file
and byte counts.

`--dry-run` shows the counts and the changes, and changes nothing. `--quiet` prints only
errors and warnings.

Exit: 0, including a busy lock. 1 on a failed check, a refusal, or an rsync error.

Cron: the `sync-push` job runs `ws sync push --quiet` every 30 minutes. It is not installed
by default: `ws cron add sync-push`.

#### `ws sync status`

```text
ws sync status [--json]
```

Shows the target, the last push (time, files, bytes, changed, deleted), the stamp state
(the table above), and what is pending: changes and deletions from an rsync dry run.
Changes nothing.

JSON: `sync.status`.

Exit: 0, whatever the state. 1 on a failed check.

#### `ws sync restore`

```text
ws sync restore [<mirror-dir>] [--dry-run]
```

The one way back (Factor XII, §2.5): copies the mirror into the workspace with rsync. It
never deletes anything in the workspace and skips the stamp. `<mirror-dir>` defaults to
`sync.mirror.target` when `ws.json` exists; on a fresh machine, pass it.

If the workspace already has a `ws.json`, the restore is one action you confirm (`y`/`n`),
and it is refused under `--quiet`/`--json`. Afterwards ws adopts the mirror's stamp as its
own last push, so this machine's next push is recognised rather than foreign. It ends with
two reminders: executable bits are not restored (`chmod +x` your scripts), and
`ws symlink link` puts the symlinks back.

Exit: 0, 1.

### Workspace commands

Commands about the workspace itself and the ws binary. There is no TUI screen of their own. The
command bar covers `config` (`:config show`, `:config edit`), and the setup screen covers `init`.

| Command | RO/RW | Does | TUI |
| --- | --- | --- | --- |
| `ws init` | RW | create the workspace layout, `ws.json` and `.megaignore` | setup screen `enter` |
| `ws layout` | RO | print the workspace folders and the homes outside it, with their purpose | — |
| `ws env` | RO | print the paths scripts use, as shell `export` lines | — |
| `ws config show [--defaults]` | RO | print the resolved `ws.json`, or the built-in defaults | `:config show` |
| `ws config edit` | RW | open `ws.json` in `$EDITOR`, validate, save | `:config edit` |
| `ws completion bash\|zsh\|fish` | RO | print the completion script | — |
| `ws completion enable [--shell s]` | RW | add completion to the shell rc file | — |
| `ws completion disable [--shell s]` | RW | remove it | — |
| `ws tui` | RO | open the terminal UI | — |
| `ws version [--short]` | RO | print version information | — |
| `ws help [noun [verb…]]` | RO | print help | `?` |

Global flags, accepted before or after the command:

| Flag | Meaning |
| --- | --- |
| `-w, --workspace <dir>` | workspace root (default `$WS_WORKSPACE`, else `~/Workspace`) |
| `-q, --quiet` | errors only; accepts plans without prompting |
| `--verbose` | show internal decisions on stderr |
| `--json` | the JSON envelope; accepts plans without prompting |
| `--dry-run` | print plans; change nothing |
| `--no-color` | no color or unicode (also `NO_COLOR`) |
| `-h, --help` | help for the command |

#### `ws init`

```text
ws init [--workspace <dir>] [--dry-run]
```

Makes a directory a ws workspace. The workspace is `--workspace`, else `$WS_WORKSPACE`, else
`~/Workspace`. `ws.json` is written to `<workspace>/ws.json`. It creates, when missing:

- the layout: `notes/ experiments/ artifacts/ references/ scripts/ scripts/lib/ agent/ agent/skills/
  configs/ symlinks/ data/ archive/`
- `ws.json`, complete: every key, at its default or empty value (`ws config show --defaults`),
  so the file documents every setting (§2.3)
- `<workspace>/.megaignore`, generated from `ws.json`'s ignore rules

It is idempotent (decision). On an existing workspace, it creates only what is missing. An
existing `ws.json` is never replaced. If it lacks keys (written by an older ws), init fills
them in with their defaults and keeps every value already set. An existing `.megaignore` is
left alone. This covers a workspace that arrived through sync with its `ws.json` but without some directories. Machine setup is separate:
`ws symlink link`, `ws trash enable`, `ws cron apply`, `ws completion enable`.

Init also refreshes the managed copy of the built-in skills (§`ws skill`). At the end of an
interactive run (stdin is a terminal, no `--json` or `--quiet`) it offers to link the
built-ins into the detected tools: `Install the built-in skills (workspace-run,
workspace-write) into claude, copilot? [y/N]`, default no. Otherwise it prints a one-line hint
(`ws skill install`). It says nothing when every built-in is already in every detected tool.

Plan: up to three actions, all pre-checked. They are: create the missing directories (one action
that lists them), write `ws.json` (or `Fill in 7 missing keys in /home/user/Workspace/ws.json;
values you set are kept`), and write `.megaignore`. When nothing is missing, it prints
"Workspace already initialized: /home/user/Workspace".

| Flag | Default | Meaning |
| --- | --- | --- |
| `-w, --workspace <dir>` | `$WS_WORKSPACE`, else `~/Workspace` | workspace root |
| `--dry-run` | false | print the plan; change nothing |

`--json`: command `init`; data `workspace`, `config`, `created` (absolute paths), `existing`
(absolute paths), `actions`.

Exit: 0, 1, 3.

TUI: until the workspace is initialized, the TUI shows the setup screen. `enter` hands the terminal to
`ws init` with the TUI's `--workspace`.

#### `ws layout`

```text
ws layout [--json]
```

Prints every folder of the layout (§2.2) with a one-line purpose, then the homes outside the
workspace with the variable `ws env` exports for each. It is the one place these facts live:
skills and scripts ask `ws layout` rather than copy them. A folder that does not exist yet is
marked `(missing; ws init creates it)`. It needs no initialized workspace; without `ws.json`,
scratch is the default `~/Scratch`.

```text
$ ws layout
Workspace /home/user/Workspace
  /home/user/Workspace/notes         the user's own notes; agents read them and write elsewhere
  /home/user/Workspace/experiments   code projects (git repos too) and hand-written or multi-step scripts
  …
  /home/user/Workspace/archive       put away for good, at the same relative path

Homes outside the workspace (ws env exports them)
  /home/user/tmp                     WS_TMP      temp: scripts make their private temp dir (WS_TMPD) here
  /home/user/.cache/ws               WS_CACHE    cache: regenerable caches, one <system>-<instance>/ each
  /run/user/1000                     WS_RUN      session: short-lived runtime and session files
  /home/user/Scratch                 WS_SCRATCH  scratch: throwaway work and captures (ws scratch)
```

`--json`: command `layout`; data `workspace`, `folders` (`path`, `rel`, `purpose`, `exists`) and
`homes` (`name`, `var`, `path`, `purpose`).

Exit: 0. 1 when `ws.json` is invalid.

#### `ws env`

```text
ws env [--json]
```

Prints one `export NAME='value'` line per path scripts use, quoted for any POSIX shell, so
`eval "$(ws env)"` sets them. `scripts/lib/common.sh` does exactly that when `ws` is on `PATH`,
and computes the same values itself where it is not (a remote host):

| Variable | Value |
| --- | --- |
| `WS_ROOT` | the workspace (`--workspace` → `$WS_WORKSPACE` → `~/Workspace`) |
| `WS_TMP` | `${TMPDIR:-$HOME/tmp}`; common.sh makes the script's private `WS_TMPD` in it |
| `WS_CACHE` | `${XDG_CACHE_HOME:-$HOME/.cache}/ws` |
| `WS_RUN` | `${XDG_RUNTIME_DIR:-$HOME/.local/state}` |
| `WS_SCRATCH` | `ws.json` `scratch.root_dir`, resolved (default `~/Scratch`) |

An empty variable counts as unset, as with the shell's `:-`. The lines go to stdout even with
`--quiet`. It needs no initialized workspace.

```text
$ ws env
export WS_ROOT='/home/user/Workspace'
export WS_TMP='/home/user/tmp'
export WS_CACHE='/home/user/.cache/ws'
export WS_RUN='/run/user/1000'
export WS_SCRATCH='/home/user/Scratch'
```

`--json`: command `env`; data is an object of the same names and values.

Exit: 0. 1 when `ws.json` is invalid; common.sh then falls back to its own values.

#### `ws config show`

```text
ws config show [--defaults] [--json]
```

Prints `ws.json` as ws resolves it, as indented JSON. It adds `workspace` (the absolute workspace
root) and `config` (the absolute path of `ws.json`, decision). Every path-valued setting is shown
absolute, with `~` expanded and relative values joined to the workspace root: `scratch.root_dir`,
`repo.roots`, `repo.exclude_dirs`, `secret.skip_dirs`, and `links[].original` (decision: shown absolute, like every printed path). Other values are shown as stored. It needs an initialized
workspace.

With `--defaults`, it prints the built-in defaults in stored form instead (`"root_dir": "~/Scratch"`,
`"roots": ["."]`), so the output can be written to a `ws.json`. This is the one documented
exception to absolute output. It needs no workspace.

| Flag | Default | Meaning |
| --- | --- | --- |
| `--defaults` | false | print the built-in defaults, in stored form |

```text
$ ws config show
{
  "schema": 1,
  "editor": "",
  "scratch": { "root_dir": "/home/user/Scratch", "tags": [] },
  "repo": { "roots": ["/home/user/Workspace"], "exclude_dirs": ["/home/user/Workspace/node_modules", "/home/user/Workspace/.venv"], "max_parallel": 8 },
  …
  "workspace": "/home/user/Workspace",
  "config": "/home/user/Workspace/ws.json"
}
```

`--json`: command `config.show`; data is the same object.

Exit: 0. 1 when the workspace is not initialized or `ws.json` is invalid.

TUI: `:config show`, from any screen, shows it in the pager.

#### `ws config edit`

```text
ws config edit [--editor <cmd>]
```

Opens `ws.json` for editing and saves it only if the result is valid (decision):

1. It copies `ws.json` to `<dir of ws.json>/.ws.json.edit` and opens the copy with `--editor`,
   else your editor (§4.10). This is the same as every other `edit` command.
2. After the editor exits, it validates the copy. The copy must be JSON, its `schema` must be from
   1 to the version this build writes, and it must have no unknown keys.
3. If the copy is valid and changed, it replaces `ws.json`. If the ignore rules changed, it
   regenerates `.megaignore`, as `ws ignore edit` does. Then it deletes the copy.

If the copy is invalid, or `ws.json` changed on disk while the editor was open, `ws.json` stays as
it was. The copy is kept, and the error names its path. An unchanged copy is deleted, and the
command reports "No changes."

| Flag | Default | Meaning |
| --- | --- | --- |
| `--editor <cmd>` | §4.10 | editor command |

`--json`: command `config.edit`; data `config`, `changed` (bool), `megaignore_regenerated` (bool).

Exit: 0. 1 for an invalid edit, a concurrent change, or an editor failure.

TUI: `:config edit`, from any screen, hands the terminal to `ws config edit`.

#### `ws completion`

```text
ws completion bash|zsh|fish
ws completion enable [--shell bash|zsh|fish] [--dry-run]
ws completion disable [--shell bash|zsh|fish] [--dry-run]
```

`ws completion <shell>` prints a completion script for that shell to stdout. The script asks the
hidden `ws __complete` for candidates at tab time, so it never goes stale. Candidates are the
commands, verbs and flags in help (never hidden aliases), job names, trash item names, and paths
where a path is expected. The script also defines the `wsopen [name]` shell function, which opens
a scratch directory and `cd`s into it through `ws scratch open --print-path`.

`enable` adds a marked block to the shell's rc file:

| Shell | File | Line |
| --- | --- | --- |
| bash | `~/.bashrc` | `eval "$(ws completion bash)"` |
| zsh | `~/.zshrc` | `eval "$(ws completion zsh)"` |
| fish | `$XDG_CONFIG_HOME/fish/conf.d/ws.fish` | `ws completion fish \| source` |

The block sits between `# >>> ws shell completion >>>` and `# <<< ws shell completion <<<`. If the
block already holds the current line, there is nothing to do. If it holds an older line, such as
`ws completions bash`, `enable` rewrites it (decision). Unmarked lines that load ws completion are
removed. `disable` removes the block. The shell is `--shell`, else the base name of `$SHELL`. An
unsupported shell exits 1. Plan: one action, pre-checked.

| Flag | Default | Meaning |
| --- | --- | --- |
| `--shell <s>` | from `$SHELL` | `bash`, `zsh` or `fish` (`enable`, `disable`) |
| `--dry-run` | false | print the plan; change nothing |

`--json`: commands `completion.bash`, `completion.zsh`, `completion.fish` (data `shell`,
`script`); `completion.enable`, `completion.disable` (data `shell`, `path`, `actions`).

Exit: 0, 1. An unknown shell exits 1.

TUI: none. These are shell integrations.

#### `ws tui`

```text
ws tui
```

Opens the interactive terminal UI on the Dashboard. The screens are Dashboard, Repos, Symlinks,
Scratch, Sessions, Ignore, Secrets, Cron, Trash and Skills, reached with 1–9, 0 and tab. It honours
`--workspace`. If the workspace is not initialized, it opens the setup screen. stdin
and stdout must be a terminal.

Exit: 0 on quit. 1 when stdin or stdout is not a terminal ("ws tui requires an interactive
terminal.") or the UI fails.

#### `ws version`

```text
ws version [--short] [--json]
```

Prints the ws version, the `ws.json` schema this build writes, the platform and the Go version.
`--short` prints only the version. It needs no workspace.

```text
$ ws version
ws v0.15.0
  ws.json schema  1
  Platform        linux/amd64
  Go version      go1.26.1
```

`--json`: command `version`; data `version`, `config_schema`, `platform`, `go_version` (decision:
named `config_schema` so it is not confused with the envelope's `schema`).

Exit: 0.

#### `ws help`

```text
ws help [noun [verb…]]
ws <noun> [verb…] --help
ws <noun>
ws
```

Bare `ws` and `ws help` print the overview: the usage line, the nouns, the workspace commands and
the global flags. `ws help <noun>` (decision), `ws <noun> --help`
and `ws <noun>` with no verb print that noun's help: its verbs and flags. All of these go to stdout
and exit 0. Help never lists hidden aliases or plumbing (`__complete`, `git-credential-helper`).

```text
$ ws
ws — Workspace manager

Usage:
  ws <noun> <verb> [targets…] [flags]

Nouns:
  repo        Git repositories under the repo roots
  symlink     Files and folders kept in the workspace and linked back
  scratch     Scratch directories
  session     Terminal sessions and recordings (agent-tmux)
  ignore      MEGA sync rules and warnings
  secret      Secret findings in the workspace
  pass        The pass password store
  credential  Git credential helper backed by pass
  cron        Scheduled ws jobs
  trash       Trashed files and soft-delete integrations

Workspace:
  init        Create the workspace layout and ws.json
  config      Show or edit ws.json
  completion  Shell completion
  tui         Interactive terminal UI
  version     Print version information
  help        Show help
…global flags…
```

An unknown noun or verb prints `unknown command: <word>` and the usage line to stderr, and exits 1.

TUI: `?` on every screen.

#### Removed commands

`ws reset` and `ws restore` are gone. Each prints one line to stderr and exits 1 (decision):

- `ws: "reset" was removed; use ws symlink unlink --all, ws trash disable, ws cron rm <job>`
- `ws: "restore" was removed; on a new machine run ws init, ws symlink link, ws trash enable, ws cron apply`

---

## 6. TUI

`ws tui` is a full-screen interface over the same `internal/` packages as the CLI. It
follows k9s's conventions: one screen per noun, a few global keys that mean the same
thing everywhere, screen keys named after their verbs, marks for acting on many rows, and
a `:` command bar that takes the CLI's own words.

### 6.1 Frame

```text
ws  Dashboard¹  Repos²  Symlinks³  Scratch⁴  Sessions⁵  Ignore⁶  Secrets⁷  Cron⁸  Trash⁹  Skills⁰   tabs
────────────────────────────────────────────────────────────────────────────────────────   rule
 Host path          Workspace copy                       App     Status    Sudo           body
 ~/.bashrc          symlinks/home/.bashrc                bash    ✔ ok
 ~/.gitconfig       symlinks/home/.gitconfig             git     ✘ broken
 …
2 marked — l link · a add · ctrl+d unlink act on them · esc clears                         status line
✔ 2 done                                                                                    notice (until the next key)
a add • ctrl+d unlink • l link • space mark … • tab screens • : command • ? help • q quit   footer
```

- **Nothing moves.** A list keeps its status line reserved at the bottom of its area, even
  when it is empty, so typing a filter or marking a row never shifts rows. The command bar
  replaces the footer line while it is open. Toasts ("📋 Copied …") draw over the footer's
  last line.
- **Footer.** The screen's own keys come first. The global part (`tab screens`, `: command`,
  `? help`, `q quit`) is always shown in full. If the screen's part does not fit, it ends in
  `…`, and `?` shows everything. Each screen's keys are chosen so the footer fits at 100
  columns.
- **Sizes.** Every screen fits any size down to 20×6. Below that, the TUI shows "Terminal
  too small".

### 6.2 Screens

Tab order: Dashboard, Repos, Symlinks, Scratch, Sessions, Ignore, Secrets, Cron, Trash, Skills.
Every screen except the Dashboard is the list of its noun's rows, the same rows as
`ws <noun> ls`. `tab` and `shift+tab` switch screens; the active tab is a reversed block.

### 6.3 Global keys

These mean the same on every screen. No screen binds them to anything else.

| Key | Does |
| --- | --- |
| `enter` | open the row: drill in, view, or launch (each screen says what) |
| `esc` | one level up: close a dialog, then clear the filter, then clear the marks, then leave a detail view, then go to the Dashboard. Does nothing at the Dashboard. |
| `:` | the command bar (§6.6) |
| `/` | filter the list (same expressions as `--filter`) |
| `?` | full help in a centered popup, grouped: this screen, lists, general, `:` commands; `esc` or `?` closes it |
| `q`, `ctrl+c` | quit (`ctrl+c` always works, even inside a dialog) |
| `tab` / `shift+tab`, `]` / `[` | next / previous screen |
| `ctrl+r` | refresh this screen |
| `d` | describe: every field of the cursor row |
| `y` | the cursor row as JSON, exactly what `ws <noun> ls --json` prints for it |
| `e` | edit in your editor (§4.10, default `vim`), on screens that have something to edit |
| `a` | add or create a row, on screens that can |
| `ctrl+d` | delete or remove the selected rows, always through the checklist |
| `space` | mark or unmark the cursor row (the cursor does not move) |
| `ctrl+space` | mark the range from the nearest mark to the cursor |
| `ctrl+\` | clear all marks |
| `ctrl+a` | mark all shown rows, or clear them when all are marked |
| `>` / `<` | sort by the next field / reverse the sort |
| `↑` `↓` `j` `k` | move |
| `g` / `G`, `home` / `end` | first / last row |
| `pgup` / `pgdn`, `ctrl+b` / `ctrl+f` | page up / down |

There are no shift+letter actions. `G` is the only uppercase key, and it is movement. The
mouse wheel scrolls. Dragging selects text and copies it on release (OSC 52, plus
`wl-copy`/`xclip`/`xsel` when present). `shift`+drag uses the terminal's own selection.

### 6.4 Lists

Every list screen is the same component (`internal/tui/reslist`), so filtering, sorting,
describing, JSON and marks work identically everywhere.

**Which rows an action takes.** Every screen key declares one scope:

| Scope | Rows | For |
| --- | --- | --- |
| none | no rows | `a`, wizards, prompts that are not about a row |
| cursor | the cursor row | `enter`, `e`, prompts about one row |
| selected | the marked rows, else the cursor row | `ctrl+d`, `t`, `i`, `m`, `a` (allow), `r` (restore) |
| shown | the marked rows, else every row the filter shows | fleet actions: `!` (git), link |

A key with no rows to act on does nothing. Every action that changes something opens the
checklist (§6.7) first. Nothing applies directly from a key.

**Status line.** It sits at the bottom of the list and is always there:

```text
/status!=ok▏   key=text · key!=text · text   enter keep · esc cancel                       while typing
filter status!=ok (2 of 9) — l link acts on these 2 · sort Age ↓                            filtered
3 marked — ctrl+d unlink · l link · a add act on them · esc clears                         marked
```

Each key is shown with its label, never as a bare letter.

### 6.5 Screen keys

Screen keys are lowercase and mnemonic: usually the first letter of the verb the footer
shows (`i` ignore, `b` back up, `m` mute, `l` link). A letter keeps one meaning on a screen,
but may mean different verbs on different screens (`r` runs on Cron
and restores on Trash), as in k9s. Global keys never change meaning. The `:`
commands are listed with each screen. They are the CLI's words after the noun.

**Dashboard.** The workspace's home: a greeting, **Needs attention** (errors, then
warnings, then info), and an overview grid with one card per screen. `↑` `↓` move in the
list, and all four arrows move in the grid (`↑` from the top row returns to the list).
`enter` on a problem opens its screen with the first affected row selected (or filtered to
the affected rows, where one filter says it). `enter` on a card opens its screen. The Symlinks
card reads `N symlinks managed`. A problem line names the broken, missing or drifted entries,
and an info line, `N known configs found, not symlinked`, appears when N is above 0 (the
Dashboard does not run the `$HOME` walk for untracked links). The Dashboard has no actions of its own.

Some rows are advice, not problems. They are listed last, at info level, never in red or
yellow, and they never count towards the card's status. `enter` on such a row opens the
command bar pre-filled with the suggested command, which you can run, edit or dismiss with
`esc`. One example is a pass store outside the workspace: "Your pass store syncs only
through its own git remote. A copy in the workspace would sync with the sync client too." `enter` then
pre-fills `:symlink add ~/.password-store --dir`.

**Repos**

| Key | Does | Rows |
| --- | --- | --- |
| `!` | the command prompt, pre-filled with `git ` (`repo git`; erase `git ` for `repo run`): history on `↑` `↓`, the most-run line first in its suggestions. One repo gets the terminal; more go through the checklist, then the output view | shown |
| `c` | configure: hand the terminal to `ws repo configure` (identity, upstream, …) | selected |
| `enter` | `repo show`: status, upstream, hygiene findings. In it, `!` and `c` act on that repo | cursor |

There is no key per git verb (decision, by the user): `git pull`, `git push`, `git fetch`
are a line in the prompt, and the history puts the ones you run most first.

`:` commands: `git <args…>`, `run <cmd…>` (bare, either opens the prompt), `configure`,
`root add [<dir>]`, `root ls`, `root rm [<dir>]`.

**Symlinks**

| Key | Does | Rows |
| --- | --- | --- |
| `a` | add. With marks: take the marked rows (a drifted row's live file wins; an untracked link is registered). Without: the checklist of known configs found on this machine, the same as `ws symlink add` with no path | marked, else none |
| `ctrl+d` | unlink | selected |
| `l` | link: repair the missing and broken links and register orphans (the workspace copy wins); a drifted row is left for `a` | shown |

The rows are exactly those of `ws symlink ls` (the `found` rows are not shown; they are in the
checklist). Sudo rows are handed to the CLI in the terminal; in the checklist they are listed
unchecked and fail with a pointer to the CLI. (decision: unlink is `ctrl+d` like every other
destructive verb, §6.3, not `u`, which is already `unmute` on Ignore.)

`:` commands: `add <host-path> [<ws-path>] [--dir]`, `link [<path>…]`, `unlink --all`.

**Scratch**

| Key | Does | Rows |
| --- | --- | --- |
| `a` | new: prompt for a name | none |
| `enter` | open in your editor | cursor |
| `t` | tag: prompt for tags, comma-separated; `-tag` removes one; `tab` completes tags used before | selected |
| `ctrl+d` | delete (to trash) | selected |

`:` commands: `add <path>`.

**Sessions**

A client of agent-tmux. The list is `agent-tmux ls --json`: live sessions and ended
recordings together, with their state (`live`, `ended`) and recorder (`● rec` running,
`stopped`, `none` for a session an older agent-tmux started, blank when ended). Every key
runs one agent-tmux verb; `open`, `show` and `create` get the terminal, with the TUI
suspended (§6.8). Without agent-tmux the screen is greyed out and shows the two install
lines; refresh after installing it. When a newer agent-tmux is out, its line and the install
lines stand above the list (the version nudge, §5 `ws session`).

| Key | Does | Rows |
| --- | --- | --- |
| `enter` | open: attach to a live session (`agent-tmux open`); an ended one is shown | cursor |
| `v` | show: page the recording (`agent-tmux show`, which pages it in `less`) | cursor |
| `a` | new: prompt for a name, then `agent-tmux create <name>`, which attaches | none |
| `s` | grep: prompt for text, then list the matching lines (`agent-tmux grep --json`); `enter` shows one, `esc` returns | none |
| `x` | kill the live sessions, through a checklist; their recordings stay | selected |
| `ctrl+d` | delete: move ended recordings to the trash (`agent-tmux rm`), through a checklist; live ones are left out | selected |

`:` commands: `grep <text>` (the same as `s`).

The Dashboard card counts live and ended sessions. A live session whose recorder stopped is
a problem there (it opens the screen filtered to `recorder=stopped`), and the version nudge
is a row of its own.

**Ignore.** There are three views, and `v` cycles them: **Warnings** (`ignore lint`, where it
opens), **Muted** (`ignore ls --muted`) and **Ignored** (`ignore ls`). The header names the
view and the counts: `Warnings 12 · Muted 47 · Ignored 3,210`.

| Key | View | Does | Rows |
| --- | --- | --- | --- |
| `i` | Warnings, Muted | ignore the path: stop syncing it | selected |
| `m` | Warnings | mute: hide the row's warning; the path keeps syncing | selected |
| `s` | Warnings | move to scratch (`scratch add`) | selected |
| `ctrl+d` | Warnings | delete the path (to trash) | selected |
| `u` | Muted | unmute | selected |
| `ctrl+d` | Ignored | stop ignoring: sync it again (`ignore rm`) | selected |
| `e` | any | edit the rules | none |
| `v` | any | next view | none |

`ctrl+d` always removes the row from its view, and the checklist names exactly what happens
("Delete … (to trash)", or "Sync again: …").

`:` commands: `rm <path>`, `check <path>`, `tree [dir]` (paged), `lint <path>`.

**Secrets.** The header shows pass store, git, remote and credential-helper health. When
the pass store isn't in the workspace, the header ends with a dim `ℹ no workspace copy`.

| Key | Does | Rows |
| --- | --- | --- |
| `enter` | show the finding in context | cursor |
| `a` | allow (false positive) | selected |
| `i` | ignore the file (stop syncing it) | selected |
| `m` | mute a directory: prompt pre-filled with the file's directory | cursor |
| `s` | store the value in pass: prompt for the entry name | cursor |

`:` commands: `unmute <dir>`, `pass init`, `pass status`, `pass audit`, `pass backup enable`,
`pass backup disable`, `pass backup status`, `pass backup push`, `credential enable`,
`credential disable`, `credential status`.

Here `a` means allow, because Secrets has nothing to add. It's the one screen where a
global letter takes the screen's own verb, and the footer says "a allow".

**Cron**

| Key | Does | Rows |
| --- | --- | --- |
| `enter` | the job's recent output (`esc` returns) | cursor |
| `a` | add (install) | selected |
| `ctrl+d` | remove (uninstall) | selected |
| `r` | run now | selected |

`:` commands: `apply`.

**Trash.** The header shows integration status and trash size.

| Key | Does | Rows |
| --- | --- | --- |
| `r` | restore to the original path | selected |
| `ctrl+d` | delete forever | selected |
| `p` | prune: prompt for an age, then a checklist | all rows |

`:` commands: `empty`, `enable`, `disable`, `status`.

**Skills.** The header shows the skills folder, how many skills it holds, and which tools are
detected (`●`) and how many are not (`○`). A row is a skill of either shelf: its name, its
shelf (`built-in` or `yours`, with `dev`, `ejected from <version>` or a name clash in
parentheses), the tools that have something at its place, and its path. This list is `ws skill scan`. A skill whose link target is
gone is listed and cannot be installed.

| Key | Does | Rows |
| --- | --- | --- |
| `i` | install: choose one tool (detected first), then a checklist of links | selected |
| `u` | uninstall: choose one of the tools that hold ws's links, then a checklist | selected |

`:` commands: `install [<tool>]`, `uninstall [<tool>]`. A tool named in the command skips the
chooser. A row the CLI would skip (already installed, broken, or a link that is not ws's) is
named in the checklist's note.

### 6.6 Command bar

`:` opens a one-line command bar in place of the footer. It takes the CLI's words:

| Typed | Runs |
| --- | --- |
| `:<screen>` | switch screen. Any unique prefix of the noun or title works: `:rep`, `:dot`, `:sess`, `:dash` |
| `:<verb> [args]` | the active screen's command, the CLI words after the noun: `:link` on Symlinks is `ws symlink link` |
| `:<noun> <verb> [args]` | switch to the noun's screen and run it there: `:pass audit` |
| `:config show`, `:config edit` | workspace config |
| `:help`, `:q` | full help; quit |

While typing, the bar shows the matching completions after the cursor, dimmed. `tab`
completes the first one, `enter` runs, `esc` cancels. An unknown command shows a notice
with the closest match. Full help (`?`) lists every `:` command of the active screen.

A command runs the way its TUI counterpart does: natively (a prompt, the checklist, or a
view) when the screen has one, otherwise handed to the CLI (§6.8).

### 6.7 Dialogs

A screen has at most one dialog open. While it is open, every key except `ctrl+c` goes to
the dialog, so typing `q` or `2` into a prompt works.

| Dialog | Keys |
| --- | --- |
| Prompt | type; `tab` completes; `enter` submits; `esc` cancels |
| Chooser | `↑` `↓`, or an option's letter where it has one; `enter` chooses; `esc` cancels the whole command, so nothing runs |
| Checklist | `↑` `↓`; `space` toggles an action; `ctrl+a` toggles all; `enter` applies; `esc` cancels. While running, keys wait. When done, `enter` or `esc` closes. |

A choice ws must not make for you, such as the tool a skill is installed into, is asked in
a chooser before the checklist. It is never guessed and never a second key.

The checklist is the action plan (§4.7): one line per action, pre-checked as the CLI
would do them, except destructive plans (`unlink --all`, `trash empty`), which start
unchecked. It runs one action at a time, shows each result as it arrives, and ends with a
summary notice.

### 6.8 Handing over the terminal

Some commands need the terminal or are multi-step CLI wizards. The TUI suspends itself,
runs them, and resumes and refreshes the screen when they exit.

- **Interactive programs:** agent-tmux's `open`, `create` and `show` (the Sessions screen), and terminal editors for
  `e` and the `edit` commands, `config edit` included (your editor, §4.10, default `vim`).
  GUI editors (for example `code`) are launched without suspending.
- **A command line in one repo:** the Repos `!` prompt (or `:git`, `:run`) for one repo runs
  on the terminal, so git's pager, editor and prompts work. The terminal waits for `enter`
  before the TUI redraws.
- **CLI wizards:** `init` (the setup screen), `repo configure`,
  `pass init`, `pass backup enable`, `credential enable`, `credential disable`.
- **sudo:** symlink actions on sudo entries run in the terminal, so sudo can ask for a
  password.
- **Paged CLI output:** `ignore tree`, `credential status`, `config show`.

Everything else is native.

### 6.9 Parity

Every CLI command is reachable in the TUI as a key, a `:` command, or the screen's list.
The only exceptions are plumbing (`git-credential-helper get|store|erase`), shell output
(`completion …`), `sync …` (CLI and cron only), and `version`, `help` and `tui` themselves. A test enforces this (§9.10).

### 6.10 Cues

Emoji are decoration only, and `WS_NO_EMOJI=1` or `TERM=linux` turns them off. Colours
follow the CLI's semantic palette (§7.2). Marked rows are drawn wholly in the mark colour,
not prefixed.

---

## 7. Design notes

Decisions that cut across nouns. Decisions specific to one noun are in its chapter.

### 7.1 No single failure breaks a batch

A workspace has thousands of files, and at any moment some are broken symlinks, vanished
directories or unreadable. That is normal, and ws absorbs it.

- Walks (`ignore ls`, `secret ls`, `symlink ls`, `repo ls`) skip unreadable entries and
  return everything else, plus a warning listing what they skipped. `filepath.WalkDir`
  callbacks return `nil` on entry errors.
- Write commands run every action they can, and report per-action failures at the end.
- The exit code reflects the outcome (§4.6): a skipped unreadable file is a warning, not an
  error.

### 7.2 Output styling

All CLI styling lives in `internal/style` (raw ANSI, no dependencies). The TUI's
`internal/tui/theme` uses the same semantic roles.

| Role | Colour | For |
| --- | --- | --- |
| success | green | ok states, done actions |
| error | red | broken, failed, critical |
| warning | yellow | needs attention |
| info | cyan | paths, values |
| muted | dim | secondary information, hints |
| accent | blue | headers, command names |

Icons have ASCII fallbacks: `✔` `[ok]`, `✖` `[err]`, `▲` `[warn]`, `●` `(*)`, `→` `->`. Colour and
icons are off with `--no-color`, `NO_COLOR`, `TERM=dumb`, or when stdout is not a terminal.
`--json` is always plain.

### 7.3 Deletion: one shared trash

Everything ws deletes (`scratch rm`, the Ignore screen's `ctrl+d`) moves into
the XDG trash (`$XDG_DATA_HOME/Trash`): `files/` holds the item, and
`info/<name>.trashinfo` records its original path and deletion time
([freedesktop trash spec](https://specifications.freedesktop.org/trash-spec)). The
desktop file manager uses the same directory, so ws deletions appear in its trash view, and
`ws trash restore`, `ws trash empty` and the file manager act on the same items.
`ws trash enable` makes a plain shell `rm` and VS Code's delete use it too. ws's own
deletions use the trash whether or not that is enabled. The only permanent deletions are
`trash rm`, `trash empty` and `trash prune`.

### 7.4 Private remotes and credentials

The pass-store backup contains secrets by nature (passwords, tokens). ws therefore pushes it
only to a **private** remote:

- `backup enable` and every push to a backup remote check the remote's visibility through
  the git host's API, and refuse a public remote. That includes `backup push` and the
  `pass-backup` cron job.
- When visibility cannot be determined (an unknown host, no token, no network), ws prints a
  warning and pushes. Refusing would stop backups to self-hosted remotes.
- There is no flag to bypass the refusal of a public remote.
- The API check authenticates with the token in pass: the entry named in
  `pass.pass_entry`, defaulting to `git/<host>`. The push itself uses git's normal credentials.
  With `ws credential enable`, git's helper is ws itself, backed by the same pass entries.

### 7.5 One description per list

**Decision.** Each domain declares its **fields** and **actions** once. The CLI `ls` output,
its `--json` keys and its TUI columns are all derived from that one field list, and the
list screens are thin descriptions on top of one generic list component
(`internal/tui/reslist`) that provides filter, sort, marks, describe and JSON for every
screen.

**Why.** Without it, each domain is described twice: the CLI defined record shapes inline, and
each TUI screen defined its own columns, formatting, keys and actions. Nothing tied them
together, so they drifted (a `class` field existed in `ws symlink ls --json` long before
the TUI showed it), and every cross-cutting feature (multi-select, search, sort) had to be
written once per screen.

**How it works.**

- **Fields** live in `internal/<domain>/fields.go`: a `resource.Schema` whose fields carry a
  key (the JSON key and the `--filter`/`--sort` name), title, raw value, display text, a sort
  comparator and a kind (text, status, bool, size, time, path), plus the default columns.
  Columns show a subset. The detail pane (`d`) shows all of them, so "the TUI lacks a field the
  CLI has" cannot happen silently. A field added there appears in `--json`, `--wide`, the
  TUI columns, `d`, `y` and both filters at once.
- **Actions** declare a key and one **scope**, and `reslist.Run` hands them exactly the rows
  that scope covers. A screen never picks its own rows (the scopes are in §6.4). Marks were
  once an opt-in helper, and fleet actions kept reading every item: marking 3 repos and
  pressing fetch fetched all of them, and reset and empty ignored marks entirely. Scope is
  therefore part of the action's declaration, and a test fails for any list-screen key that
  does not declare one.
- **Marking follows k9s:** `space` marks the cursor row, `ctrl+space` marks a range, `ctrl+\`
  clears, `ctrl+a` marks all or none.
- **Items are typed and keyed by a stable ID**, not stored as pre-styled strings addressed by
  cursor index. Sorting compares raw values (styled strings contain ANSI codes), and marks and
  the cursor survive reload, filter and sort.
- **Plain Go structs and functions**, generic over the item type: no struct tags, reflection
  or config files. The description must not grow into its own mini-language.
- **Non-list screens stay custom.** The Dashboard implements `nav.Component` directly.
- **Every marked-items action goes through the checklist**; nothing that acts on a selection
  applies immediately.
- **Parity is enforced by tests, not review** (§9.10).

**Considered and rejected.** Keeping per-screen code and adding features one screen at a
time (eight implementations of every feature, and nothing prevents drift). Shared helpers
without a shared description (less duplication, but fields and actions are still defined
twice). Reflection or struct tags on domain types (hides formatting and sort logic, and
couples `internal/` domain types to presentation).

**Consequences.** A screen with unusual behaviour (a live tail, a nested detail) needs hooks
on the generic list. If those hooks become awkward, that is the signal the abstraction is
too rigid, not a reason to force the screen into it.

### 7.6 One grammar for CLI and TUI

**Decision.** The CLI and the TUI share one grammar (Part 3 and §6):

- **Shape:** `ws <noun> <verb> [targets…]`, singular nouns. Each noun is one TUI screen, and
  `ws <noun> ls` prints exactly that screen's rows. A TUI action is the CLI command with the
  rows as its targets, so the two cannot disagree about what a verb does.
- **A closed verb glossary** (§3.3). Each verb means one thing everywhere. Domain words win
  where the domain has them (`git` for git command lines, the trash's `restore` and `empty`,
  `link` and `unlink` for symlinks, `allow` for false positives, `mute` for warnings, `apply`
  for writing declared state into the live system). Where two copies can conflict, the verb
  names the direction: `symlink add` keeps the live file, `symlink link` keeps the workspace
  copy.
- **No umbrella verbs.** `scan`, `fix`, `reset` and `setup` do not exist. Each command names
  what it changes. Finding problems is not an error, so exit code `2` is retired.
- **Nouns that mean one thing:** `session` for terminal sessions (now agent-tmux's: `ws session` execs it), and `pass` and
  `credential` split out of `secret`.
- **Old names stay as hidden aliases** (§3.5). They print a one-line rename hint when a person
  types them, and stay silent when crontab, git or a shell hook calls them.
- **TUI keys follow k9s:** global keys that never change meaning, lowercase screen keys named
  after their verbs, no shift+letter actions, and rare actions in a `:` command bar that takes
  the CLI's own words (§6).
- **Layout never shifts.** The list status line (filter, sort, marks) is reserved at the
  bottom, and the command bar replaces the footer.

**Why.** The CLI grew one subcommand at a time, and the TUI took its labels and keys from it.
The result showed on every screen:

- One key meant up to five things: `x` was quick fix, run a command, fix links, stop
  recording and empty the trash, depending on the screen. `p` was pull, git push, move to
  pass, or purge.
- 21 actions used shift+letter keys, and two of them took the list's movement keys: on
  Secrets `g` pushed the pass store instead of going to the top, and on Symlinks `G` launched
  a wizard instead of going to the bottom.
- "sync" meant four things: git pull and push, adopting a live file, committing and pushing
  a config repo, and the sync client's own sync.
- `scan` and `fix` meant something different on every noun, and `fix` was an interactive loop
  with its own letters, which disagreed with the TUI. There was no rule for `ls` versus
  `scan`.
- The same idea took different formats (`trash purge --older-than 30` was days, another
  command's was a duration), and dead commands lingered in completion.

An earlier attempt gave every noun the same verbs. It made things worse, because generic
verbs were forced onto domains where they meant different things.

**Considered and rejected.** Fixing only the TUI keys (the keys come from the CLI verbs, so
inconsistent verbs keep reappearing as inconsistent keys). One generic verb set for every
noun. A global `f` fix key with a picker, and `scan` as the problem-finding verb (named
actions on marked rows are clearer, and `ls` already shows status). Renaming without aliases
(crontab lines, git and shell hooks would break). k9s's shift+column sort (column initials
collide, so `>` and `<` stay). Keeping `r` for refresh (refresh is `ctrl+r`, which frees `r`
for run, record and restore).

**Consequences.**

- The JSON envelope is `schema: 3` (§4.4).
- `secret-scan`, `ignore-scan` and `repo-sync` are removed, because
  they existed only to turn exit code 2 into a notification, or to commit work unattended.
- Commands that fill gaps: `trash restore`, `repo root rm`, `ignore rm`,
  `scratch add`, `config edit`, `secret show`.
- `repo git` runs a git command line as typed in each repo, and ws has no git verbs of its
  own (the earlier `repo fetch|pull|push|save` were removed). `repo configure` asks for what
  a repo's setup is missing.
- `ignore lint` reports sync warnings, and `ignore ls` lists what doesn't sync. `mute` only
  hides warnings and never changes what syncs.
- The editor is one setting (§4.10), and `ws.json` always holds every key (§2.3).
- Tests enforce the grammar (§9.7): help and completion list only documented commands, every
  verb is in the glossary, no screen rebinds a global key or uses a shift+letter action,
  every CLI command has a TUI key or `:` command, and marking a row moves no row.

### 7.7 Sync clients

ws never syncs anything itself. It assumes a client keeps `<workspace>` in step across
machines (§2.6). The pieces that know a particular client are few and named: the generated
`.megaignore` and the `mega-sync` cron job are MEGA's. `ws sync` serves clients that sync
only their own folder (WorkDrive): it copies the workspace into that folder and stays
client neutral itself. Everything else is client neutral.

---

## 8. Dependencies

### 8.1 Programs ws runs

ws does its file work natively in Go: walking, symlinks, moves, copies, sizes, hashing,
and matching. It runs these programs:

| Program | Package | Used by | Required |
| --- | --- | --- | --- |
| `git` | git | `repo …`, `pass backup …` | for those commands |
| `agent-tmux` | [agent-tmux](https://github.com/mugenkunou/agent-tmux) | `ws session …` (exec), the Sessions screen, `ws session` completion | for sessions |
| `pass`, `gpg` | pass, gnupg | `pass …`, `secret store`, `credential …`, backup tokens | for those commands |
| `crontab` | cron | `cron …` | for cron |
| `sh`, `bash` | — | `repo run`, cron job scripts, shell integrations | yes |
| `sudo` | sudo | symlinks whose host path needs root (`/etc/…`) | for those symlinks |
| your editor: `ws.json` `editor`, `$VISUAL`, `$EDITOR`, else `vim` | vim | `edit` commands, `scratch new\|open` | for editing (the path is printed if it can't start) |
| `$PAGER` / `less` | less | paged output | optional |
| `rsync` | rsync | `sync push\|status\|restore` | for `ws sync` |
| `megasync`, `pgrep`, `pkill` | MEGAsync, procps | the `mega-sync` cron job (MEGA only) | for that job |
| `wl-copy` / `xclip` / `xsel` | wl-clipboard / xclip / xsel | TUI copy (OSC 52 is always sent too) | optional |

A missing program fails only the command that needs it, with an error naming the program
and its package.

### 8.2 Build

| Tool | Version | Purpose |
| --- | --- | --- |
| `go` | ≥ 1.26 | build the binary |
| `make` | any | `make build`, `make test` (tests run in Docker; see §9.3) |
| `docker` | any | the isolated test run |

### 8.3 Around ws

- **A sync client** keeps `~/Workspace` in step: MEGA (MEGAsync), Zoho WorkDrive, or any
  folder-sync tool (§2.6). ws generates MEGA's `.megaignore` only.
- **pass** and **GnuPG** hold secrets and git tokens.
- Editors and apps whose settings ws manages as symlinks (VS Code and others) need nothing
  from ws.

---

## 9. Developer guide

This part takes you from a fresh Linux box to coding, testing and publishing. The rules for
adding commands and TUI screens are in §9.6 to §9.10.

### 9.1 Machine setup

**Operating-system packages.**

```bash
sudo apt update
sudo apt install -y \
  git make curl wget ca-certificates gnupg pass \
  coreutils findutils grep diffutils file util-linux sudo
sudo apt install -y jq        # optional but useful
```

ws delegates to standard Linux tools (`ln`, `find`, `grep`, `script`, `git`, …) instead of
reimplementing them (§1.1). Docker must also be installed and running to run the tests (§9.3).

**Go ≥ 1.26** (required by the Charm TUI libraries). Install the official tarball:

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

A distro package (`sudo apt install -y golang-go`) works only when it is new enough.

**VS Code** (optional). Extensions: `golang.go` (language tooling, tests, debug),
`eamodio.gitlens`, `streetsidesoftware.code-spell-checker`, `yzhang.markdown-all-in-one`,
`esbenp.prettier-vscode`. Install them with `code --install-extension <id>`. Suggested
`.vscode/settings.json`:

```json
{
  "go.useLanguageServer": true,
  "go.formatTool": "gofmt",
  "editor.formatOnSave": true,
  "go.testFlags": ["-v"],
  "files.insertFinalNewline": true
}
```

### 9.2 Clone, build, run, deploy

```bash
git clone https://github.com/mugenkunou/ws-tool.git
cd ws-tool
make fmt && make build         # binary: ./ws
./ws version
./ws init --dry-run            # run locally, changing nothing
```

To install: `make install` copies the binary to `~/.local/bin` (override with
`make install PREFIX=/usr/local`), or copy it yourself: `sudo cp ./ws /usr/local/bin/ws`.
Without a checkout, `go install github.com/mugenkunou/ws-tool@latest` builds it, and the
binary is named `ws-tool`: rename it to `ws`.

If your machine mounts `/tmp` with `noexec`, Go cannot run its temporary build artifacts
there. The Makefile therefore sets `TMPDIR`, `GOCACHE` and `GOTMPDIR` to `~/tmp`,
`~/.gocache` and `~/gotmp` and creates them when missing. Nothing is written inside the
repo.

### 9.3 Test suite

**Never run bare `go test`, `go build` or `go run`.** They fail on a `noexec` `/tmp`, and a
host `go test` once deleted a real `~/.local/state/ws-tool/trash-setup.json`. Use
`make test`, `make build` and `make fmt`.

**Default: Docker.** `make test` builds the image `ws-tool-test:local` from
`Dockerfile.test` on first run (it is cached until `Dockerfile.test` changes), then runs
`go test ./...` in a container with the source tree mounted read-only. The container cannot
see the host's crontab, credential helpers, XDG config, git config or password store.

```bash
make test                # Docker-isolated
make test LOCAL=1        # on the host, for CI (runners are already isolated) or when Docker is unavailable
make test-image          # build or rebuild the test image
make test-image-clean    # remove the image and stamp, forcing a full rebuild
```

Targeted, verbose and coverage runs go on the host, with the environment set by hand:

```bash
TMPDIR=$HOME/tmp GOCACHE=$HOME/.gocache GOTMPDIR=$HOME/gotmp go test -v -run TestFoo ./cmd/...
TMPDIR=$HOME/tmp GOCACHE=$HOME/.gocache GOTMPDIR=$HOME/gotmp go test -cover ./...
```

**Isolation has two independent layers, both always on.**

1. **Docker** (process level): the host's crontab, credential helpers, XDG config, password
   store and git config are unreachable for the whole test binary.
2. **`testSetXDG`** (test level): redirects `XDG_CONFIG_HOME`, `PASSWORD_STORE_DIR` and
   `WS_CRONTAB_FILE` to per-test temp dirs. `TestMain` isolates `GIT_CONFIG_GLOBAL` and the
   editor `code`.

| Scope | Mechanism | Protects |
| --- | --- | --- |
| XDG config | `XDG_CONFIG_HOME` → temp dir (per test) | real `~/.config` |
| Git global config | `GIT_CONFIG_GLOBAL` → temp file (`TestMain`) | real `~/.gitconfig` (credential helper) |
| System crontab | `WS_CRONTAB_FILE` → temp file (per test) | live cron jobs |
| Password store | `PASSWORD_STORE_DIR` → temp dir (per test) | real `~/.password-store` |
| Editor binary | fake `code` shim in `PATH` (`TestMain`) | VS Code is never launched |
| agent-tmux | `agenttmuxtest.Hide()` in `TestMain` takes it off `PATH` and sets `WS_NO_VERSION_CHECK=1`; a test that needs it calls `agenttmuxtest.Fake` (a script on `PATH` that prints canned output and logs its arguments) | the installed agent-tmux, its sessions and recordings, and GitHub |

**Convention:** any test that runs `ws init`, `ws cron`, `ws secret` or any other RW command
must call `testSetXDG(t)` first. No test needs network access or real credentials, and no
test can clobber your `ws credential enable` setup.

### 9.4 Daily flow

```bash
git pull --rebase
# ... edit ...
make fmt && make test && make build     # the quality gate; run it before every PR and release
git add . && git commit -m "feat: your change"
git push
```

### 9.5 Release pipeline

Pushing a semver tag (`v*`) triggers `.github/workflows/release.yml`, which runs the tests,
cross-compiles binaries (linux and darwin, amd64 and arm64), generates SHA-256 checksums and
creates a GitHub Release with every artifact. Every push and pull request runs
`.github/workflows/ci.yml`: a `gofmt` check, `go vet`, `go test -race` and a build.

**Secret scanning.** The repo is public, so a pre-push hook runs
[gitleaks](https://github.com/gitleaks/gitleaks) before anything leaves your machine.

```bash
go install github.com/zricethezav/gitleaks/v8@v8.21.2
make hooks                                  # copies scripts/pre-push into .git/hooks/
gitleaks detect --source . --verbose        # manual scan
```

**Version injection.** `appVersion` in `cmd/version.go` defaults to `dev` and is set at build
time with ldflags (`-s -w` strips debug symbols and local paths):

```bash
make build VERSION=v0.2.0
```

**Tagging a release.**

1. Everything is committed, and `make fmt && make test && make build` passes.
2. Pick the version: `git tag --sort=-v:refname | head -3`, then
   [semver](https://semver.org/): patch for fixes, minor for backward-compatible features,
   major for breaking changes. `git log v0.2.0..HEAD --oneline` shows what changed.
3. `make release-check`: gitleaks, `go vet` and race tests. Do not skip it.
4. `git tag -a v0.3.0 -m "v0.3.0"`: an annotated tag, with the message matching the version.
5. `git push origin master --follow-tags`: pushes the commits and the tag together. The
   pre-push hook runs gitleaks first.
6. Check GitHub → Releases for the artifacts.
7. If a change since the last tag touches a built-in skill (`skills/`) or anything skills rely on (a
   folder, a path, a command or a flag), edit the GitHub release notes and add a **Skills** line
   saying which skill line changed. The generated notes do not say it.

| Command | Purpose |
| --- | --- |
| `make build` | local build (version `dev`) |
| `make build VERSION=v0.2.0` | local build with a version |
| `make test` | run the tests |
| `make release-check` | the pre-release gate (gitleaks, vet, race tests) |
| `make hooks` | install the pre-push hook |
| `make clean` | remove the binary |

### 9.6 Adding a command

**Naming: the grammar.** Every command is `ws <noun> <verb> [targets…]` (Part 3). Before you
add or rename one:

1. **Use a glossary verb with exactly its meaning** (§3.3), or a domain word its users
   already know (`git`, the trash's `restore`). If neither fits, extend the glossary
   in this document in the same change, and say why in the commit.
2. **No umbrella verbs:** never `scan`, `fix`, `reset`, `setup`, `manage` or `do`. Name what
   changes.
3. **`ls` lists the noun's rows** (its TUI screen, its `fields.go`). Listing is not a
   verdict: it exits 0.
4. **Targets are arguments.** No targets means every row, unless the command needs one.
5. **A rename keeps the old name as a hidden alias** in the alias table (§9.7). A hidden
   alias is absent from help and completion, prints the rename hint on stderr when a person
   types it, and stays silent when called as plumbing.
6. **Give the command a TUI home in the same change:** a screen key named after the verb, or
   a `:` command (§6.5), plus its entry in `internal/tui/app/coverage.go`.

**RO or RW.** A command that writes to disk, config or system state is RW. RO handlers
receive `(args, globals, stdout, stderr)` with no stdin and no prompts. RW handlers also
receive `stdin`, for the per-action prompts.

**The action plan is required for every RW command.** Use `Plan` and `RunPlan` from
`cmd/plan.go`, not `confirm()`. The exemptions are listed in §4.7. A new exemption needs a
strong justification in this document.

```go
func runMyCommand(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
    // 1. Parse flags, validate inputs, gather state
    if *dryRun {
        globals.dryRun = true
    }

    // 2. Build the plan: one Action per discrete mutation
    plan := Plan{Command: "mycommand"}
    for _, item := range items {
        item := item // capture the loop variable
        plan.Actions = append(plan.Actions, Action{
            ID:          fmt.Sprintf("process-%s", item.Name),
            Description: fmt.Sprintf("Process %s", item.Name),
            Execute:     func() error { return processItem(item) },
        })
    }

    // 3. RunPlan handles dry-run, prompts and quiet/json auto-accept
    planResult := RunPlan(plan, stdin, stdout, globals)

    // 4. JSON output includes planResult.Actions for programmatic consumers
    if globals.json {
        return writeJSON(stdout, stderr, "mycommand", map[string]any{"actions": planResult.Actions})
    }

    // 5. The exit code follows the outcome
    return planResult.ExitCode()
}
```

**Granularity.** One action per independently meaningful mutation: would a user ever say
yes to this and no to the next?

| Command type | Granularity |
| --- | --- |
| Per-file operations (`init`, `symlink add`, `symlink unlink`) | one action per file |
| Fleet operations (`repo git\|run`) | one action per repo |
| Cleanup operations (`trash prune`) | one action per item removed |
| Single-mutation commands (`repo root add`) | one action in total |

**Testing RW commands.** Tests pass `strings.NewReader("y\n")` as stdin. `promptChoice`
returns the default `y` on EOF, so every action is accepted.

### 9.7 The command table: help, completion, aliases, coverage

The grammar is enforced by tests, not review. `cmd/commands.go` holds one table of commands
(`command{Path, Args, Summary, RO, Flags, Fields}`, one entry per line of §3.4). It is the
single source for help, shell completion and the TUI coverage test, so they cannot drift.

1. **Every subcommand has its own help.** `ws <noun> <verb> --help` prints the usage, the
   arguments, the command's own flags and, for `ls`, its field names. `ws <noun> --help`
   lists the verbs, one line each. A group help that only prints `<a|b|c>` is a bug.
2. **Help, completion, the alias table and the coverage table agree.** Tests read help's
   verbs and check that each is in the glossary, that completion offers exactly those verbs,
   that no hidden alias appears in help or completion, and that every verb has a coverage
   entry (`TestGlossaryVerbs`, `TestHelpEveryCommand`, `TestCLICoverage`,
   `TestCompletersOfferEverySubcommand`, `TestCompletionFlagsMatchHelp`).
3. **Hidden aliases are data, in one table** (`cmd/aliases.go`). Each entry maps an old
   command-line prefix to the new one and is marked *typed* (it prints the rename hint) or
   *plumbing* (silent: crontab lines, git, shell hooks). A test runs every alias and checks
   that it reaches the same handler as its target.
4. **JSON command names are `<noun>.<verb>`** (`pass.backup.push`) and match the command
   after alias resolution.
5. **Completion mirrors reality.** `topLevelCommands` is derived from the table, so it holds
   exactly the commands that help lists. A command that tab-completes but answers "unknown
   command" is worse than none. A stale entry in the table or in `resolvers` is a user-facing
   lie, so treat it like a broken handler.
   - `commandFlags()` reads the same table, so a flag is offered exactly when help lists it.
   - A subcommand that accepts a repo name, scratch name, symlink host path or anything else
     enumerable needs a `resolvers` entry (`cmd/complete.go`).
   - A resolver that needs workspace state gets a field in `completionCtx`, loaded in
     `loadCompletionCtx`. Never call the filesystem from a resolver: the loader is the one
     best-effort loading point.
   - After touching a command, run `ws completion enable && exec bash` and tab through the
     subcommands and flags you changed.
6. **Every subcommand parses global flags.** Global flags are pre-parsed in
   `parseGlobalFlags`, but a handler that creates its own `flag.FlagSet` must also call
   `registerGlobalFlags(fs, &globals)`, or `ws cron ls --json` fails as an unknown flag. A
   handler that parses arguments by hand must not reject recognised global flags.

### 9.8 Paths in code

The rules are §4.5. The contributor checklist:

1. **Output.** Render every path through `style.AbsPath(workspace, p)`: it joins relative
   paths onto the workspace and cleans them, without resolving symlinks or shortening to `~`.
   Never format a path ad hoc. That covers stdout, `--json`, plan and dry-run lines, action
   IDs, prompts and errors. Internal structs may keep workspace-relative paths where rule
   matching needs them (ignore and secret violations, repo discovery). Convert at render or
   marshal time, never by changing what the matcher sees.
2. **One line per item.** An absolute path is already `cd`-ready. Names (scratch names,
   cron jobs) may still appear, and when a command acts on the file behind a
   name, print its absolute path too.
3. **Input.** Absolute as is, `~` expanded, anything else relative to the current directory.
   There is no workspace-relative fallback for CLI input: delete a "try cwd, then try the
   workspace" branch when you find one. Prefix filters match whole components
   (`--path /ws/Da` must not match `/ws/Data`).
4. **Round trip.** Any path copied from ws output is accepted by any ws command from any
   directory. Add a test when you touch a command that both prints and accepts paths.
5. **Config values differ.** Relative paths in `ws.json` resolve against `<workspace>`. Resolve
   CLI input by the input rule first, then store it workspace-relative inside the workspace
   and absolute outside.

### 9.9 Positional targets

Every command that acts on rows accepts positional targets (§3.1 rule 5), so you can act on
one repo after `ws repo ls` shows which needs attention. A path target is matched as an
absolute path: `ws repo git data/bruno -- status` works only from the workspace root, and a path pasted
from `ws repo ls` always works. Targets and `--path` are complementary: `--path` is a prefix
filter and a target is an exact match, and both may be used together. Tab completion offers
the rows' absolute paths (repos, symlink host paths) or names (scratch, cron
jobs, trash items). The TUI passes its rows the same way: a key acts on the marked rows, else
on the cursor row or the filtered rows by its scope (§6.4), and the CLI command with those
rows as targets does the same thing.

### 9.10 TUI development

The TUI and the CLI call the same `internal/` packages. Logic that both need lives in
`internal/`, not `cmd/`, so they cannot drift (for example `workspace.ResolvePaths`,
`repo.DescribeSync`, `symlink.Detect`, `secret.ResolveGitToken` and the `secret allow|store`
mutations, `cron.AddActions` and `RemoveActions`, `trash.Record*Provisions`, and the action
plan types in `internal/plan`). The TUI is built on Charm v2: `charm.land/bubbletea/v2`,
`charm.land/bubbles/v2` and `charm.land/lipgloss/v2`. The user-facing behaviour is Part 6.

**Packages.**

| Package | Role |
| --- | --- |
| `internal/tui/app` | Root model. Owns the screens, routes messages, handles global keys, lays out header, body, notice and footer, and shows the setup screen when the workspace is not initialized. |
| `internal/tui/nav` | The contract between app and screens: the `Screen` enum, the `Component` interface, `ResizeMsg`, `UpMsg`, `GotoMsg`, `RefreshMsg`, `ReloadEnvMsg`, `NoticeMsg`, `CommandMsg`. |
| `internal/tui/keys` | **Every key binding** (`keys.go`). The app handles `Global`, and each screen and dialog handles its own map. |
| `internal/tui/screens/<name>` | One package per screen: dashboard, repos, symlinks, scratch, sessions, ignore, secrets, cron, trash, setup. State is unexported, and outputs are exported `LoadedMsg` types. |
| `internal/tui/cmdbar` | The `:` command bar: a one-line input drawn in place of the footer, with inline completion from the active screen's commands, the screen names and the global commands. It emits `nav.CommandMsg`. |
| `internal/tui/confirm` | The action-plan checklist: review (toggle), running (one `tea.Cmd` per action), done. Emits `DoneMsg{Result}`. |
| `internal/tui/prompt`, `choose` | Text input with ghost-panel suggestions (fixed list or async `Completer`), and a single-choice list. |
| `internal/tui/modal` | Holds a screen's one open dialog, tagged with a typed step. `Match` routes the dialog's result back. |
| `internal/tui/handover` | Suspends the TUI to give a program the terminal (`Run`), launches GUI programs (`Launch`), runs CLI wizards (`WS` with `Pause` or `Paged`), and opens editors (`Editor`). |
| `internal/tui/selection`, `clip` | Mouse drag selection over the rendered frame, and clipboard copy (OSC 52 plus a native tool). |
| `internal/tui/reslist` | The list every list screen is: a `listview` over typed items described by a `resource.Schema`, with filter, sort, marks, describe, JSON and the reserved status line built in. Screens declare their actions with a scope, and their `:` commands. `reslist.Run` hands each one its rows. |
| `internal/tui/listview`, `layout`, `theme`, `format`, `complete` | The load-state-aware table, column sizing from width, lipgloss styles (16-colour, matching `internal/style`), cell formatting, and path completion. |
| `internal/tui/env` | Workspace context resolved at startup (paths, config, dirs), reloaded with `nav.ReloadEnvMsg` after init. |
| `internal/tui/tuitest` | Snapshot (golden file) and fits-in-terminal assertions. |

(`internal/tui/ghostinput.go` and friends are raw-ANSI prompt widgets used by the CLI.)

**One description per list.** The decision is §7.5. In code, each listed domain declares its
records once in its domain package (`fields.go`, with `trash/items.go` keeping its loader
beside it). The CLI list command (`renderList` in `cmd/list.go`) and the TUI screen
(`reslist`) both render that schema. Presentation stays with the caller: `reslist.Col` adds
widths and colour. Each list screen declares an `actions` table of
`reslist.Action{Key, Scope, Do}` and dispatches keys with `reslist.Run`, which calls `Do` with
the rows the scope covers. The scopes are in §6.4. In code they are `Global`, `Cursor`,
`Selected` and `All`, and a screen never picks its own rows. An `All` action that receives
every row may use the whole-subsystem operation (`trash.Empty`), and otherwise acts on just the rows.

**`:` commands are actions too.** Each screen declares its `commands` (the CLI words after the
noun, such as `backup push` or `root rm`), each with a scope and a `Do` that also receives the
typed arguments. The command bar completes from them, and `?` lists them. A key and a command
for the same verb share one `Do`.

**Parity is tested, not reviewed.**

- `cmd` `TestCLICoverage`: every CLI subcommand (read from its help) has an entry in
  `internal/tui/app/coverage.go`: a screen key, a `:` command, the screen's list, or a stated
  reason it is CLI-only. A new command fails until that is decided.
- `app` `TestCoverageKeysExist`: each entry's key or `:` command is one its screen offers.
- `app` `TestListScreensShareListKeys`: every list screen is a `reslist`, and none binds a
  global key (`enter esc : / ? q ctrl+r d y e a ctrl+d space ctrl+space ctrl+\ ctrl+a > <` and
  movement) to another verb. Secrets' `a` (allow) is the one listed exception.
- `app` `TestNoShiftLetterActions`: no binding uses an uppercase letter, except `G` for
  movement.
- `app` `TestNoKeyTwiceOnAScreen`: no key appears in two bindings of one screen, movement
  included. That catches a `g` push hiding `g` top.
- `app` `TestEveryListKeyDeclaresItsRows`: every key on a list screen (except movement, back
  and refresh) is declared with a scope.
- `app` `TestMarkingMovesNoRow`: marking, filtering and sorting never change the screen line
  a row is drawn on.
- `app` `TestFooterFits`: at 100×30, every screen's footer shows all of its own keys without an
  ellipsis, and the global part is never cut.
- `app` `TestNoKeyNamesInText`: no string literal outside `keys.go` names a key ("press G").
  Messages take the key from the binding's help.
- `app` `TestGlobalKeysLeavePageKeysAlone`: global bindings never take arrows, hjkl, enter, esc
  or space.
- Per domain, the CLI `--json` keys equal the field keys.
- `cmd` `TestCompletersOfferEverySubcommand`: completion offers every subcommand help lists,
  and no hidden alias.

**Rules.**

- **Architecture.** The model holds state, `Update` changes it, `tea.Cmd`s do async work, and
  `View` renders state with no side effects. Never block `Update` and never do IO in `View`.
  Loaders and plan actions run inside commands, including validation that needs the
  filesystem (do it in the action and let the checklist show the failure).
- **Message routing.** Key presses go to the global map first, then to the active screen only.
  While a screen has a dialog open (`CapturesKeys`), every key except `ctrl+c` goes to it. All
  other messages are broadcast to every screen, which is how the Dashboard summarizes the
  others (it observes their `LoadedMsg`).
- **Write actions.** Every mutation goes through a `plan.Plan` shown in the `confirm`
  checklist, one action per independently meaningful mutation (§9.6). Pre-check what the CLI
  would do by default (`WithChecked`), and start destructive plans (`symlink unlink --all`,
  `trash empty`) unchecked. A choice ws must not make for the user (the tool a skill goes
  into) is asked in a `choose` dialog before the checklist, never guessed and never a
  second key. After `DoneMsg`, notify with `confirm.Summary` and reload.
- **Dialogs.** A screen keeps one `modal.Model`. Open a dialog with a typed `step` constant. In
  `Update`, first `if st, ok := m.modal.Match(msg)` to handle results, then forward to the modal
  while `Open()`. Results carry the dialog's ID, so stale results are ignored.
- **Handover.** Interactive programs (the recorded shell, terminal editors) and the CLI's
  multi-step wizards run through `handover` with a typed tag, so the owning screen refreshes on
  `handover.DoneMsg`. What is handed over is in §6.8.
- **Text never names a key.** Empty states, notices and help take the key from the binding
  (`k.Add.Help().Key`), so a remap cannot leave a stale "press G" behind.
- **Dashboard.** It derives everything from other screens' `LoadedMsg` and has no actions of its
  own. `enter` on a problem sends the target screen a `Focus*Msg` carrying a filter expression
  (Symlinks: a row to select) or a row to select, alongside `nav.Goto`. To add an attention item,
  extend the area's derivation function in `screens/dashboard`, with the filter that shows
  exactly its rows.
- **Mouse.** The app captures the mouse in cell-motion mode so a drag selects text and release
  copies it: `selection` extracts the dragged cells from the rendered frame (terminal-style,
  emoji-width aware) and draws the highlight, and `clip.Copy` emits OSC 52 and also pipes to
  `wl-copy`, `xclip` or `xsel` when present. A toast replaces the footer's last line for 2.5 s,
  so it never shifts the layout. Because capture stops terminals from turning the wheel into
  arrow keys, the app maps wheel up and down to arrow presses. `WS_NO_MOUSE=1` turns capture
  off. Tests clear `DISPLAY` and `WAYLAND_DISPLAY` so they never touch the real clipboard.
- **Emoji and cues.** Decoration only, through `theme.Icon` (off with `WS_NO_EMOJI=1` and on
  `TERM=linux`). Use only Emoji_Presentation characters (always 2 cells, no U+FE0F variation
  selector: not ⚠️ ☀️ ⚙️) so widths stay exact. Animations read `nav.FrameMsg`, which the app
  broadcasts every 150 ms. Never read the clock in `View`: use `env.Now()` in `Update`.
- **Navigation.** Screens are `nav.Screen` constants, never strings. `esc` always moves one
  level up (§6.3). A screen with nested levels (repos list, then detail) handles `esc` itself,
  and at its top level returns `nav.Up`.
- **Command bar.** `:` is a global key handled by the app. It resolves `:<screen>`, then
  `:<noun> <verb…>` (switch, then run), then `:<verb…>` (the active screen's command), and sends
  the screen a `nav.CommandMsg{Words, Args}`. The screen runs the matching command through
  `reslist.Run`, so its rows and scope rules are the same as for keys.
- **Layout.** No absolute coordinates. The app sends each screen a `nav.ResizeMsg` with the
  body's size on every resize, and screens and dialogs derive everything from it. The frame is
  clipped to the terminal.
- **Stale results.** A screen that can reload while a load is in flight tags loads with a
  generation counter and drops stale results (see `repos`).

**Changing a component.** Before: identify its state (the `Model` struct), inputs (the messages
`Update` handles), outputs (exported messages and `nav.*` commands), key bindings (its map in
`keys.go`) and rendering region (the `ResizeMsg` size). After:

```bash
make test                                         # all tests, including TUI snapshots
UPDATE_SNAPSHOTS=1 TMPDIR=$HOME/tmp GOCACHE=$HOME/.gocache GOTMPDIR=$HOME/gotmp \
  go test ./internal/tui/...                      # accept intended snapshot changes
git diff internal/tui/**/testdata                 # review them like code
```

Snapshots live in `internal/tui/app/testdata/*.golden` (ANSI stripped, 100×30 unless noted) and
cover every screen plus open dialogs. `TestResize` renders every screen and several dialogs
from 120×40 down to 1×1 and fails on any overflow, and `TestResizeRestoresLayout` checks that
shrinking and restoring is lossless. Tests pin `env.Clock` and `theme.SetEmoji(true)`, and call
`loadAll()` rather than `Init()` (which also starts the animation timer).
`TestScratchNewEndToEnd` drives prompt, checklist and apply against a real temp workspace with
every integration (HOME, git config, pass, crontab) redirected. Copy its `hermetic` helper for
new end-to-end tests.

### 9.11 Testing `ws cron` by hand

Unit and integration tests touch no crontab:

```bash
TMPDIR=$HOME/tmp go test ./internal/cron/... ./cmd/... -run TestCron -v -count=1
```

For a smoke test, point at a throwaway crontab, never your real one:
`export WS_CRONTAB_FILE=$HOME/tmp/crontab.test`.

```bash
ws cron ls                       # jobs: schedule, installed, last run, exit, next run
ws cron add mega-sync --dry-run  # preview an install; writes nothing
ws cron add mega-sync            # a block with the schedule line and the @reboot line
ws cron run mega-sync            # run it now
ws cron log mega-sync            # read its output
ws cron add mega-sync --dry-run  # show what would be installed
ws cron rm mega-sync             # uninstall
```

| Check | Expected |
| --- | --- |
| `ws cron ls` | the built-in jobs (`mega-sync`, `pass-backup`) with `installed` yes or no |
| `ws cron add <job> --dry-run` | the plan; the crontab is unchanged |
| `ws cron add <job>` | a `# >>> ws` block with the schedule line and the `@reboot` line, and `configs/crontab` updated to match |
| `ws cron run repo-sync` | exits 0 silently and does nothing (a retired name, from existing crontab lines) |
| `ws cron rm <job>` | the block is gone from the crontab and from `configs/crontab` |

### 9.12 Troubleshooting

- **`go: toolchain not available`.** Install Go 1.26 or newer from the official tarball (§9.1).
- **Build or tests fail with `permission denied` or `exec format error`.** `/tmp` is mounted
  `noexec`. Use `make build` and `make test`, which set `TMPDIR`, `GOCACHE` and `GOTMPDIR` to
  home-directory paths (§9.2). Never run bare `go test ./...` or `go build .`.
- **`ws` command not found.** Make sure `/usr/local/bin` (or `~/.local/bin`) is in `PATH`.
- **Formatting drift in a PR.** Run `make fmt` before committing.
- **The credential helper points at the wrong binary.** `TestMain` isolates
  `GIT_CONFIG_GLOBAL`, so tests should not cause this. If it happens, run `ws credential enable`
  from the installed `ws`. Never run `./ws credential enable`: it points git at the repo-local
  development binary.
