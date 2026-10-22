# `ws` — Your Workspace, Under Control

**One binary. One workspace. Zero scattered files.**

`ws` is a workspace manager for Linux, with a CLI and a full-screen TUI. It manages your dotfiles, keeps junk out of your cloud sync, records terminal sessions, herds your Git repos, and lets you rebuild a fresh machine from a single synced folder.

---

## The Problem

Your digital life is scattered:

- `~/.ssh/config` — one place. `~/.bashrc` — another. VS Code settings — who knows.
- That one `rsync` command that took 30 minutes to get right? Gone. Lost in terminal scrollback.
- A 200 MB log dump is silently eating your cloud sync quota.
- You got a new laptop. Recreating your setup takes *two days*.

`ws` fixes this. It gives you a single `~/Workspace` directory that syncs via [MEGA](https://mega.io), and a CLI that keeps it clean, searchable, and ready for the day you lose everything.

---

## What It Does

| Area | Problem | `ws` |
| --- | --- | --- |
| **Dotfiles** | Configs scattered across `~/`, `/etc/`, `~/.config/` | `ws dotfile add ~/.bashrc` moves the original into the workspace and symlinks it back. On a new machine, `ws dotfile link` puts every link back. |
| **Sync hygiene** | 200 MB log dumps and `node_modules/` eating your cloud quota | `ws ignore lint` warns about what shouldn't sync. `ws ignore add` stops syncing it, and `ws ignore mute` silences a warning you mean to keep. |
| **Secrets** | `password=hunter2` about to sync to the cloud | `ws secret ls` finds it. `ws secret store` moves the value into `pass`, and `ws secret allow` marks a false positive. |
| **Git credentials** | HTTPS tokens for several git hosts | `ws credential enable` makes git read them from `pass` |
| **Terminal sessions** | "What was that command I ran last Tuesday?" | `ws session record` records a shell. Find it later with `ws session grep`. |
| **Git repos** | 6 repos: 3 need a push, 1 is on a detached HEAD | `ws repo ls` shows the fleet. `ws repo save` commits, pulls and pushes in one go (and asks before rebasing or merging). `ws repo configure` fixes missing name, email and upstream. |
| **Scratch dirs** | Debug sessions littered across `/tmp` | `ws scratch new` makes a named directory in `~/Scratch` and opens it in your editor |
| **Soft delete** | `rm -rf` regrets | everything ws deletes goes to the trash. `ws trash enable` makes your shell's `rm` and VS Code do the same, and `ws trash restore` puts things back. |
| **Cron jobs** | MEGA sync and dotfile and pass backups, all by hand | `ws cron add sync` installs managed jobs with logging and missed-run recovery |

Everything is also in a full-screen TUI: `ws tui`.

---

## Quick Start

### Install

```bash
# One-liner (requires Go ≥ 1.26)
go install github.com/mugenkunou/ws-tool@latest
sudo mv "$(go env GOPATH)/bin/ws-tool" /usr/local/bin/ws

# Or from source
git clone https://github.com/mugenkunou/ws-tool.git
cd ws-tool
make build
sudo cp ws /usr/local/bin/
```

### Initialize

```bash
ws init
```

This creates the workspace layout, `ws.json` (ws's state, synced with the workspace) and
`.megaignore`. Running it again only adds what's missing, including any `ws.json` keys an older version
left out.

### Manage your first dotfile

```bash
ws dotfile add ~/.bashrc
# Add /home/user/.bashrc → /home/user/Workspace/configs/dotfiles/bashrc [y/n/a/q] y
#   ✔ Add /home/user/.bashrc → /home/user/Workspace/configs/dotfiles/bashrc
```

Your `.bashrc` now lives in the workspace and syncs. The system path is a symlink to it,
so editing either one edits the same file.

---

## Commands at a Glance

Every command is `ws <noun> <verb>`, and each verb means the same thing everywhere
(`ls` lists, `add` brings something under ws, `rm` deletes, `mute` silences warnings,
`prune --older-than 30d` clears old rows). Without targets, a command acts on every row.
The full grammar is in [spec.md](spec.md) Part 3.

<details>
<summary><strong>📦 Repos</strong></summary>

| Command | What it does |
| --- | --- |
| `ws repo ls [--fetch]` | Every repo: branch, dirty, ahead/behind, hygiene findings |
| `ws repo show <repo>` | One repo in full |
| `ws repo fetch [repo…]` | Fetch |
| `ws repo pull [repo…]` | Pull: fast-forward, or the rebase or merge you choose for a diverged repo |
| `ws repo push [repo…]` | Push, nothing else |
| `ws repo save [repo…]` | Commit everything with a timestamped message, pull, push: for repos you edit in one place |
| `ws repo configure [repo…]` | Asks for a missing name, email, upstream or default branch, and can set up `ws credential` |
| `ws repo run -- <cmd>` | Run a command in each repo |
| `ws repo root add \| ls \| rm` | Manage the directories searched for repos |

</details>

<details>
<summary><strong>🔗 Dotfiles</strong></summary>

| Command | What it does |
| --- | --- |
| `ws dotfile ls` | Managed dotfiles and their link health |
| `ws dotfile add <path>` | Move a file into the workspace and symlink it back. On a managed path that something overwrote, keep the live file instead. |
| `ws dotfile link [path…]` | Point paths back at their workspace copies: repairs broken links, and is the new-machine step |
| `ws dotfile link <path> --to <ws-path>` | Symlink a system path to a file already in the workspace |
| `ws dotfile unlink <path> \| --all` | Put the real file back and stop managing it |
| `ws dotfile edit <path>` | Edit the workspace copy |
| `ws dotfile backup enable \| disable \| status \| push` | Versioned backup to a private git remote |

</details>

<details>
<summary><strong>🧪 Scratch</strong></summary>

| Command | What it does |
| --- | --- |
| `ws scratch ls` | Scratch directories with age, size, items, tags |
| `ws scratch new [name]` | Create one and open it in your editor |
| `ws scratch add <path>` | Move an existing file or directory into scratch |
| `ws scratch open <name>` | Open one (`wsopen` also `cd`s into it) |
| `ws scratch tag <name> [tag…]` | Label one (`--rm` removes a tag) |
| `ws scratch rm <name>` | Delete (to trash) |

</details>

<details>
<summary><strong>📼 Sessions</strong></summary>

| Command | What it does |
| --- | --- |
| `ws session record [tag]` | Record a shell session (`● ws:rec` in the prompt) |
| `ws session stop` | Stop the recording |
| `ws session ls` | Sessions with duration, size, command count |
| `ws session show <tag> [--commands]` | Print a transcript, or just its commands |
| `ws session grep <text>` | Search every transcript |
| `ws session rm <tag>` | Delete a session |
| `ws session prune [--older-than 30d]` | Delete old sessions, or enforce the size cap |

</details>

<details>
<summary><strong>🧹 Sync hygiene</strong></summary>

| Command | What it does |
| --- | --- |
| `ws ignore lint` | Warn about synced files that shouldn't be: bloat, excessive depth, build artifacts |
| `ws ignore ls` | Every path that doesn't sync, and the rule that keeps it out |
| `ws ignore ls --muted` | The warnings you muted |
| `ws ignore add \| rm <path>` | Stop syncing a path; sync it again |
| `ws ignore mute \| unmute <path>` | Hide or show a path's warnings (it keeps syncing) |
| `ws ignore check <path>` | Will this path sync, and which rule decides? |
| `ws ignore tree [dir]` | The tree with sync status |
| `ws ignore edit` | Edit the rules |

</details>

<details>
<summary><strong>🔐 Secrets, pass, credentials</strong></summary>

| Command | What it does |
| --- | --- |
| `ws secret ls` | Possible secrets in workspace files |
| `ws secret show <file:line>` | A finding in context |
| `ws secret allow <file:line>` | Mark a false positive |
| `ws secret store <file:line> [entry]` | Copy the value into `pass` |
| `ws secret mute \| unmute <dir>` | Stop or resume scanning a directory |
| `ws pass init \| status \| audit` | Create the pass store; its health; entries with problems |
| `ws pass backup enable \| disable \| status \| push` | The store's private git backup, like `dotfile backup` |
| `ws credential enable \| disable \| status` | ws (backed by `pass`) as git's credential helper |

</details>

<details>
<summary><strong>⏰ Cron</strong></summary>

| Command | What it does |
| --- | --- |
| `ws cron ls` | Jobs with schedule, last run, exit code, next run |
| `ws cron add \| rm <job>` | Install or uninstall jobs and presets |
| `ws cron run <job>` | Run a job now (crontab lines call this too) |
| `ws cron log <job>` | Its recent output |
| `ws cron apply` | Install `configs/crontab` into your live crontab |

Built-in jobs: `mega-sync`, `dotfile-backup`, `pass-backup`, `session-prune`.
Presets: `sync` (mega-sync, dotfile-backup, pass-backup) and `maintenance` (session-prune).

</details>

<details>
<summary><strong>🚮 Trash</strong></summary>

| Command | What it does |
| --- | --- |
| `ws trash ls` | Trashed items, where they came from, size, age |
| `ws trash restore <item>` | Put an item back |
| `ws trash rm <item>` | Delete an item forever |
| `ws trash empty` | Delete everything forever |
| `ws trash prune --older-than 30d` | Delete items trashed before then |
| `ws trash enable \| disable \| status` | Soft-delete for shell `rm`, VS Code and the file explorer |

</details>

<details>
<summary><strong>⚙️ Workspace</strong></summary>

| Command | What it does |
| --- | --- |
| `ws init` | Create the workspace layout, `ws.json` and `.megaignore` |
| `ws tui` | The full-screen interface |
| `ws config show [--defaults] \| edit` | The resolved config; edit `ws.json` |
| `ws completion bash \| zsh \| fish \| enable \| disable` | Shell completion |
| `ws version`, `ws help` | |

</details>

---

## The TUI

`ws tui` has one screen per noun (Repos, Dotfiles, Scratch, Sessions, Ignore, Secrets,
Cron, Trash), plus a Dashboard that lists what needs attention. It follows k9s:

| Key | Does |
| --- | --- |
| `enter` · `esc` | open · back (clears a filter or marks first) |
| `:` | command bar, using the CLI's words: `:backup push`, `:root rm ~/old`, `:dot` jumps to Dotfiles |
| `/` · `>` `<` | filter (`status!=ok`) · sort |
| `d` · `y` | describe the row · the row as JSON (what `--json` prints) |
| `a` · `e` · `ctrl+d` | add · edit · delete (always through a checklist) |
| `space` · `ctrl+space` · `ctrl+a` · `ctrl+\` | mark · mark a range · mark all · clear marks |
| `ctrl+r` · `?` · `q` | refresh · help · quit |

Each screen adds a few lowercase keys named after their verbs: `f` fetch, `p` pull, `s` save
on Repos; `l` link, `b` back up on Dotfiles. Keys act on the marked rows. Without marks, a
fleet action (fetch, pull, save, link…) takes every row the filter shows, and any other key
takes the cursor row. Nothing changes without a checklist you
confirm.

---

## How Dotfile Management Works

```text
┌──── ORIGINALS (in ~/Workspace/configs/dotfiles/ and secrets/dotfiles/) ┐
│  bashrc     gitconfig     ssh/config     kubeconfig     daemon.json     │
└───────────────┬──────────────────────────────┬─────────────────────────┘
                │           SYMLINKS           │
                ▼                              ▼
   ~/.bashrc → configs/dotfiles/bashrc     /etc/docker/daemon.json  (sudo)
```

**Originals live in the workspace**, and the system paths are symlinks to them. The
workspace syncs through MEGA, so every config file is backed up, and on a fresh machine
`ws dotfile link` recreates every link. Files that look like secrets go to
`secrets/dotfiles/`.

Optional: `ws dotfile backup enable` adds versioned backup to a **private** git remote,
committing and pushing after each change. ws refuses public remotes.

---

## The Sync Firewall: `.megaignore`

MEGA reads `.megaignore` to decide what syncs. ws generates it from a built-in template,
which blocks build artifacts, archives, large datasets, logs and lock files, plus your
own rules from `ws ignore add`. Directories you `ws ignore mute` keep syncing and stop
producing warnings.

```bash
ws ignore lint                    # what probably shouldn't sync
ws ignore add ~/Workspace/experiments/model.bin
ws ignore check ~/Workspace/experiments/model.bin
```

---

## Design Principles

| Principle | What it means |
| --- | --- |
| **Single binary** | No Python, no Node. Copy it to any Linux box. |
| **Minimal dependencies** | The core and the CLI are pure Go stdlib. The TUI uses Charm's Bubble Tea, Bubbles and Lip Gloss. |
| **Offload, don't reimplement** | `git`, `script(1)`, `pass`, `crontab`: ws orchestrates them |
| **One grammar** | `ws <noun> <verb>`, one meaning per verb, the same nouns and verbs in the TUI |
| **Read/write separation** | Read commands never prompt and are pipe-safe. Write commands confirm each action and support `--dry-run`. |
| **`--json` everywhere** | A stable, versioned envelope |
| **One list, two front ends** | `ws <noun> ls` and its TUI screen show the same rows, fields, `--filter` and `--sort` |
| **Soft-delete first** | Everything ws deletes can be restored from the trash |
| **The workspace carries the state** | `ws.json` syncs with the workspace. Sync the folder, and the tool's state comes along. |

Full philosophy: [PHILOSOPHY.md](PHILOSOPHY.md).

### Read/Write Separation

Every command is **RO** (read-only) or **RW** (read-write), as listed in [spec.md](spec.md) §3.4.

- **RO** (`ls`, `show`, `status`, `check`, `tree`, `grep`, `log`, `audit`) never prompt and
  produce deterministic output.
- **RW** commands build a plan of discrete actions and confirm each one (`git add -p` style):

| Key | Effect |
| --- | --- |
| `y` (default, Enter) | Do this action |
| `n` | Skip it, continue |
| `a` | Accept all the rest |
| `q` | Skip all the rest |

`--dry-run` prints the plan. `--quiet` and `--json` accept every action. There is no `--force`.

---

## Exit Codes

| Code | Meaning |
| --- | --- |
| `0` | Success, including "nothing to do" |
| `1` | Error: bad input, missing workspace, permission denied |
| `3` | Partial: some actions succeeded, some failed |
| `130` | A prompt was cancelled with Ctrl-C |

Listing problems is not an error: `ws <noun> ls` and `ws ignore lint` exit 0.

---

## Global Flags

```text
--workspace, -w   Workspace root       (default: $WS_WORKSPACE, then ~/Workspace)
--config, -c      State file           (default: $WS_CONFIG, then <workspace>/ws.json)
--quiet, -q       Errors only; accept every action
--verbose         Show internal decisions
--json            Machine-readable output
--dry-run         Preview actions, change nothing
--no-color        No colors or Unicode (also: NO_COLOR)
```

### Environment Variables

| Variable | Description |
| --- | --- |
| `WS_WORKSPACE` | Workspace root (`--workspace` wins) |
| `WS_CONFIG` | State file (`--config` wins) |
| `NO_COLOR` | No colors or icons ([no-color.org](https://no-color.org)) |
| `WS_NO_MOUSE`, `WS_NO_EMOJI` | TUI: leave the mouse to the terminal; no emoji |

---

## Workspace Layout

```text
~/Workspace/
├── ws.json              # ws state (synced)
├── .megaignore          # generated sync rules
├── notes/  experiments/  artifacts/  references/  archive/
├── scripts/lib/
├── agent/skills/
├── configs/
│   ├── dotfiles/        # dotfile originals; also the backup git repo
│   └── crontab          # declared cron jobs (ws cron apply)
├── secrets/
│   ├── dotfiles/        # dotfile originals that look like secrets
│   └── pass/
└── data/
    └── ws-log/          # recorded sessions
```

Machine-local state (cron run state, trash integration) lives in `~/.local/state/ws-tool/`,
scratch directories in `~/Scratch`, and the trash in `~/.local/share/Trash`. None of these
sync.

---

## Restore a Machine in 5 Minutes

You lost your laptop, or got a new one:

1. **Install MEGA**, sign in, and sync `~/Workspace`.
2. **Install `ws`**: copy the binary to `/usr/local/bin/`.
3. **Put it back together:**

```bash
ws init              # adds anything missing, including ws.json keys; keeps your values
ws dotfile link      # every dotfile link
ws trash enable      # soft-delete for rm, VS Code, the file explorer
ws credential enable # git reads tokens from pass
ws cron apply        # your declared cron jobs
```

Your SSH config, bash setup, Kubernetes context and VS Code settings are back where they
belong.

---

## Build from Source

```bash
git clone https://github.com/mugenkunou/ws-tool.git
cd ws-tool
make build
```

Requires Go ≥ 1.26. That's it. No `npm install`. No virtualenv. No cmake. One command.

If your environment restricts executable temp dirs (e.g. `/tmp` is `noexec`),
the Makefile already uses home-directory temp/cache dirs and creates them if
missing:

```
~/tmp           # TMPDIR
~/.gocache      # GOCACHE
~/gotmp         # GOTMPDIR
```

---

## Docs

| Document | What's in it |
| --- | --- |
| [spec.md](spec.md) | The authoritative spec: the command grammar, every command, the TUI, the state file |
| [dev.md](dev.md) | Building, testing, and the rules for adding commands and TUI screens |
| [PHILOSOPHY.md](PHILOSOPHY.md) | The Workspace Factors, a methodology for workspace management |
| [docs/adr/](docs/adr/) | Architecture decisions |

---

## License

[MIT](LICENSE)
