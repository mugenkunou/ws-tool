# `ws` — Your Workspace, Under Control

**One binary. One workspace. Zero scattered files.**

`ws` is a workspace management CLI for Linux. It manages your dotfiles, scans for sync hygiene violations, records terminal sessions, herds your Git repos, and makes sure you can restore a fresh machine from a single synced folder.

```text
ws scan
Summary
──────────────────────────────────────────────────────
Workspace    ~/Workspace              1,842 files · 4.3 GB
Ignore       0 critical · 1 warning   bloat/depth/project-meta
Secret       0 critical · 0 warning   0 files matched
Dotfiles     0 critical · 0 warning   5 registered
Log          recording active         312 MB / 500 MB cap
Trash        ok                       setup complete
──────────────────────────────────────────────────────
```

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

| Area | Problem | `ws` solution |
| --- | --- | --- |
| **Dotfiles** | Configs scattered across `~/`, `/etc/`, `~/.config/` | `ws dotfile add ~/.bashrc` — moves the original into workspace, symlinks back |
| **Sync hygiene** | 200 MB log dumps and `node_modules/` bloating your cloud | `ws ignore scan` finds bloat; `ws ignore fix` quarantines it |
| **Secrets** | Accidentally syncing `password=hunter2` to the cloud | `ws secret scan` catches it; `ws secret fix` lets you exclude or allowlist |
| **Terminal sessions** | "What was that command I ran last Tuesday?" | `ws log start` records everything via PTY. Search it later with `ws log search` |
| **Git repos** | 6 repos, 3 need push, 1 is on a detached HEAD | `ws repo scan` shows fleet status; `ws repo pull` / `ws repo push` in one shot |
| **Scratch dirs** | Debug sessions and investigations scattered across `/tmp` | `ws scratch new` — named directory in `~/Scratch`, opens in VS Code instantly |
| **Machine restore** | New laptop, two days of setup | `ws restore` — guided wizard, working machine in minutes |
| **Soft delete** | `rm -rf` regrets | `ws trash setup` configures your shell, VS Code, and file explorer to soft-delete |

---

## Quick Start

### Install

```bash
# From source (requires Go ≥ 1.23)
git clone https://github.com/mugenkunou/ws-tool.git
cd ws-tool
make build

# Copy the binary anywhere in your PATH
sudo cp ws /usr/local/bin/
```

### Initialize

```bash
ws init
```

This creates `ws/config.json`, `ws/manifest.json`, and `.megaignore` in your workspace root. It also walks you through configuring soft-delete on your machine.

### Capture Your First Dotfile

```bash
ws dotfile add ~/.bashrc
# ✔ Moved    ~/.bashrc → ~/Workspace/ws/dotfiles/bashrc
# ✔ Linked   ~/.bashrc → ~/Workspace/ws/dotfiles/bashrc
```

Your `.bashrc` now lives in the workspace (synced). The system path is a symlink pointing back. Edit either one — they're the same file.

### Scan for Problems

```bash
ws scan
```

One command. Checks everything: bloated files, secrets, broken dotfile symlinks, sync hygiene, trash setup. Fix them interactively:

```bash
ws fix
```

---

## Commands at a Glance

<details>
<summary><strong>🏗️ Setup & Restore</strong></summary>

| Command | What it does |
| --- | --- |
| `ws init` | Scaffold a workspace (creates `ws/`, `.megaignore`, configures trash) |
| `ws restore` | Guided full-machine restore wizard — init → trash → dotfiles → scan → fix |
| `ws trash setup` | Configure soft-delete for shell `rm`, VS Code, and file explorer |
| `ws trash status` | Check if soft-delete is active on this machine |

</details>

<details>
<summary><strong>🔍 Scan & Fix</strong></summary>

| Command | What it does |
| --- | --- |
| `ws scan` | Aggregate health check across all subsystems |
| `ws fix` | Walk through all violations interactively |
| `ws search <query>` | Grep the entire workspace — text, filenames, logs |

</details>

<details>
<summary><strong>🔗 Dotfiles</strong></summary>

| Command | What it does |
| --- | --- |
| `ws dotfile add <path>` | Capture a system file into `ws/dotfiles/`, symlink back |
| `ws dotfile rm <path>` | Restore file to system path, unregister |
| `ws dotfile ls` | List all managed dotfiles |
| `ws dotfile scan` | Verify all symlinks are intact |
| `ws dotfile fix` | Recreate broken/missing symlinks (the restore command) |
| `ws dotfile git connect` | Optional: configure Git backup for dotfile versioning |
| `ws dotfile git status` | Show dotfile Git backup health |

</details>

<details>
<summary><strong>📼 Session Recording</strong></summary>

| Command | What it does |
| --- | --- |
| `ws log start` | Start a PTY-recorded session (● ws:log prompt indicator) |
| `ws log stop` | End the current recording |
| `ws log ls` | List all sessions with size, duration, command count |
| `ws log show <tag>` | View a session — merged, commands-only, or output-only |
| `ws log search <query>` | Search across all recorded sessions |
| `ws log scan` | Log subsystem health (storage, cap pressure) |
| `ws log prune` | Delete old sessions |
| `ws log setup-terminal` | Auto-record in every new terminal tab |

</details>

<details>
<summary><strong>📦 Git Fleet</strong></summary>

| Command | What it does |
| --- | --- |
| `ws repo ls` | Discover Git repos under workspace |
| `ws repo scan` | Fleet status: dirty, ahead/behind, detached |
| `ws repo fetch` | `git fetch --all --prune` across the fleet |
| `ws repo pull` | Interactive fleet pull (ff-only or rebase) |
| `ws repo push` | Interactive fleet push |
| `ws repo run -- <cmd>` | Run any command in each repo root |
| `ws repo fix` | Reconcile repo state from current workspace |

</details>

<details>
<summary><strong>🛡️ Sync Hygiene</strong></summary>

| Command | What it does |
| --- | --- |
| `ws ignore check <path>` | Will this file sync? Shows the matching rule |
| `ws ignore ls` | List all excluded files (pipe-safe, greppable) |
| `ws ignore tree` | Visual directory tree with sync/ignored status |
| `ws ignore scan` | Find bloat, excessive depth, and build artifacts |
| `ws ignore fix` | Fix violations: move to scratch, add rules, delete build output |
| `ws ignore generate` | Create or merge `.megaignore` from the built-in template |
| `ws ignore edit` | Open `.megaignore` in your editor with syntax validation |

</details>

<details>
<summary><strong>🔒 Secrets</strong></summary>

| Command | What it does |
| --- | --- |
| `ws secret scan` | Find exposed secrets (`password=`, `API_KEY=`, private keys) |
| `ws secret fix` | View context, exclude file, or allowlist false positives |

</details>

<details>
<summary><strong>⚡ Scratch & Context</strong></summary>

| Command | What it does |
| --- | --- |
| `ws scratch new [name]` | Create a named scratch directory, open in VS Code |
| `ws scratch ls` | List scratch directories with age, size, items |
| `ws scratch prune` | Remove old scratch directories |
| `ws context init <task>` | Create a `.ws-context/` sidecar for agent-assisted dev |

</details>

<details>
<summary><strong>⚙️ Config & Meta</strong></summary>

| Command | What it does |
| --- | --- |
| `ws version` | Binary version, schema versions, platform info |
| `ws config view` | Dump the fully resolved config (shows source of each value) |
| `ws config defaults` | Print the built-in default config as valid JSON |
| `ws completions <shell>` | Generate shell completions (bash/zsh/fish) |
| `ws tui` | Full-screen interactive dashboard |
| `ws notify start/stop/status` | Background notification daemon for real-time alerts |

</details>

---

## How Dotfile Management Works

Most dotfile managers make you maintain a git repo with a weird bare-repo trick or GNU Stow. `ws` keeps it simpler:

```text
┌──── ORIGINALS (in ~/Workspace/ws/dotfiles/) ────────────────┐
│                                                              │
│  ssh/       bashrc       daemon.json       kubeconfig        │
│  vscode-settings.json    megaignore                          │
│                                                              │
└───────────────┬──────────────────────┬───────────────────────┘
                │       SYMLINKS       │
                ▼                      ▼
   ~/.ssh/ → ws/dotfiles/ssh    ~/.bashrc → ws/dotfiles/bashrc
   ~/.kube/config               /etc/docker/daemon.json  (sudo)
```

**Originals live in the workspace.** System paths are symlinks pointing back. The workspace syncs via MEGA. You get automatic cloud backup of every config file, and `ws dotfile fix` recreates all symlinks on a fresh machine.

Optional: enable `dotfile.git.enabled` for versioned backup with auto-commit and optional auto-push.

---

## The Sync Firewall: `.megaignore`

MEGA uses `.megaignore` to decide what syncs. `ws` ships a battle-tested template with 50+ rules that block the usual suspects:

- **Build artifacts** — `node_modules/`, `__pycache__/`, `.venv/`, `*.class`, `*.pyc`
- **Archives & packages** — `*.tar.gz`, `*.deb`, `*.zip`, `*.jar`, `*.whl`
- **Large datasets** — `*.csv`, `*.parquet`, `*.orc`
- **Logs & crash dumps** — `*.log`, `hs_err_pid*`
- **Lock files** — `package-lock.json`, `yarn.lock`, `poetry.lock`, `go.sum`
- **Hidden dirs** — `.git/`, `.obsidian/`, `.venv/` (caught by `-:.*`)

**Safe harbors** override everything above. `ws/` always syncs (your metadata, dotfiles, session logs). Generate it with:

```bash
ws ignore generate          # fresh template
ws ignore generate --merge  # keep your custom rules, add missing template rules
```

---

## Design Principles

`ws` is opinionated. Here's why:

| Principle | What it means |
| --- | --- |
| **Single binary** | No Python, no Node, no runtime deps. Copy it to any Linux box and go. |
| **Zero third-party libraries** | Pure Go stdlib. Minimal attack surface. |
| **Offload, don't reimplement** | `ln`, `grep`, `find`, `diff`, `script(1)`, `git` — `ws` orchestrates battle-tested tools |
| **Read/write separation** | Read commands are non-interactive and pipe-safe. Write commands are always interactive with `--dry-run`. |
| **`--json` everywhere** | Every command supports `--json` for scripting. Stable schema envelope with version. |
| **Soft-delete first** | `rm` is the delete primitive. `ws trash setup` makes it route to Trash. |
| **Workspace-sourced metadata** | Config and manifest live inside the workspace. Sync the folder, get the tool state too. |

Full philosophy: [PHILOSOPHY.md](PHILOSOPHY.md) — a nine-factor methodology for workspace management, inspired by the Twelve-Factor App.

---

## Exit Codes

Scripts can branch on these without parsing output:

| Code | Meaning |
| --- | --- |
| `0` | Success — no issues |
| `1` | Error — bad input, crash, permission denied |
| `2` | Violations found — workspace is not clean |
| `3` | Partial success — some items succeeded, some failed |

---

## Global Flags

```text
--workspace, -w   Path to workspace root     (default: ~/Workspace)
--config, -c      Path to config file        (default: <workspace>/ws/config.json)
--quiet, -q       Errors only
--verbose         Show internal decisions
--json            Machine-readable output
--dry-run         Preview actions, no changes
--no-color        Disable colors and Unicode (also: NO_COLOR env var)
```

---

## Workspace Layout

```text
~/Workspace/
├── .megaignore                  # sync firewall (MEGA ignore rules)
├── ws/                          # ws metadata (always synced)
│   ├── config.json              # your settings (edit by hand)
│   ├── manifest.json            # ws-managed registry (don't hand-edit)
│   ├── dotfiles/                # dotfile originals (symlinked from system paths)
│   │   ├── bashrc
│   │   ├── ssh/
│   │   ├── kubeconfig
│   │   └── ...
│   ├── dotfiles-git/            # optional git repo for dotfile backup
│   ├── ws-log/                  # recorded terminal sessions
│   │   └── <tag>/
│   │       ├── stdin.log
│   │       └── stdout.log
│   └── ws-log-index.md          # session index/summary
├── Artifacts/                   # presentations, certs, documents
├── Experiments/                 # scripts, side projects, investigations
├── Notes/                       # daily notes, second brain, knowledge base
└── Data/                        # API collections, app data, datasets
```

---

## Restore a Machine in 5 Minutes

The whole point. You lost your laptop (or got a new one). Here's the play:

1. **Install MEGA** → sign in → sync `~/Workspace`
2. **Install `ws`** → copy the binary to `/usr/local/bin/`
3. **Run the wizard:**

```bash
ws restore
```

It walks you through: workspace init → trash setup → dotfile symlinks → scan → fix. Your SSH keys, bash config, Kubernetes context, VS Code settings — all back where they belong.

---

## Build from Source

```bash
git clone https://github.com/mugenkunou/ws-tool.git
cd ws-tool
go build -o ws .
```

Requires Go ≥ 1.23. That's it. No `npm install`. No virtualenv. No cmake. One command.

---

## Docs

| Document | What's in it |
| --- | --- |
| [spec.md](spec.md) | The authoritative spec — design decisions, full CLI reference, output examples, config schemas |
| [PHILOSOPHY.md](PHILOSOPHY.md) | The Workspace Factors — a nine-factor methodology for workspace management |

---

## License

[MIT](LICENSE)
