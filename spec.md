# `ws` — Workspace Manager

`ws` manages one directory, `~/Workspace`, which a sync client (MEGA) keeps in the
cloud, and everything a developer machine needs to be rebuilt from it: dotfiles, git
repos, secrets and the pass store, sync rules, scratch directories, recorded terminal
sessions, cron jobs, and the trash. It ships as one static binary with two front ends
over the same `internal/` packages: the CLI (`ws <noun> <verb>`) and a full-screen TUI
(`ws tui`).

This document is the authoritative spec. It describes the intended behaviour. The
command grammar in Part 3 was adopted on 2026-10-22 (see
[docs/adr/0002](docs/adr/0002-one-command-grammar-for-cli-and-tui.md)). The old command
names still work as hidden aliases, listed in §3.5.

Contents:

1. Principles
2. Workspace and state
3. Command grammar: the nouns, the verbs, every command
4. CLI conventions: flags, lists, JSON, paths, exit codes, write commands
5. Command reference, one chapter per noun
6. TUI
7. Design notes
8. Dependencies

---

## 1. Principles

- **One binary.** A static Go binary. The core and the CLI use only the standard
  library. The TUI uses Charm v2 (Bubble Tea, Bubbles, Lip Gloss). The only runtime
  dependencies are the system tools listed in Part 8.
- **The workspace carries the state.** Durable state lives in `<workspace>/ws.json`, which
  syncs with the workspace. Sync the folder to a new machine and ws knows your dotfiles,
  rules, allowlist and tags. Machine-local state (cron run state, trash integration
  state) lives in the XDG state directory and is never synced.
- **Offload, don't reimplement.** `git`, `script(1)`, `pass`, `crontab` and the coreutils do
  the work. ws orchestrates them, keeps state, and reports.
- **Read/write separation.** Read commands never prompt and are pipe-safe. Write commands
  build a plan of discrete actions, confirm each one, and support `--dry-run`. There is no
  `--force`.
- **Soft-delete first.** Everything ws deletes goes to the XDG trash and can be restored.
- **Private remotes only.** Dotfile and pass-store backups push only to private remotes.
  There is no flag to bypass this.
- **One grammar.** Every command is `ws <noun> <verb>`, and every verb means one thing
  (Part 3). The TUI uses the same nouns as screens and the same verbs as keys and `:`
  commands.
- **One description per list.** Each list is described once (its fields), and the CLI
  `ls`, its `--json`, and its TUI screen are all rendered from that description.
- **JSON everywhere.** Every command supports `--json` with a versioned envelope.

---

## 2. Workspace and state

### 2.1 Where things live

| What | Where | Synced |
| --- | --- | --- |
| Workspace root | `--workspace` → `$WS_WORKSPACE` → `~/Workspace` | — |
| State file | `--config` → `$WS_CONFIG` → `<workspace>/ws.json` | yes |
| Sync rules | `<workspace>/.megaignore`, generated from `ws.json` `ignore_rules` | yes |
| Dotfile originals | `<workspace>/configs/dotfiles/`, or `<workspace>/secrets/dotfiles/` for files that look like secrets, or wherever `--to` put them | yes |
| Dotfile backup repo | `<workspace>/configs/dotfiles` (a git repo, once `ws dotfile backup enable` runs) | yes |
| Recorded sessions | `<workspace>/data/ws-log/<tag>/` | yes |
| Scratch directories | `scratch.root_dir` (default `~/Scratch`), outside the workspace | no |
| Machine-local state | `$XDG_STATE_HOME/ws-tool/` (default `~/.local/state/ws-tool/`): cron run state and logs, trash integration state, the running recording's marker (`session-active.json`) | no |
| Trash | `$XDG_DATA_HOME/Trash` (default `~/.local/share/Trash`), shared with the desktop file manager | no |
| Pass store | `$PASSWORD_STORE_DIR` → `~/.password-store` | via its own git remote |

No other config file exists. The workspace root is never stored in a file: it comes from
the flag, the environment, or the default.

### 2.2 Layout created by `ws init`

```text
<workspace>/
├── ws.json              # ws state (see §2.3)
├── .megaignore          # generated sync rules
├── notes/
├── experiments/
├── artifacts/
├── references/
├── scripts/
│   └── lib/
├── agent/
│   └── skills/
├── configs/
│   └── dotfiles/        # dotfile originals; also the backup git repo
├── secrets/
│   ├── dotfiles/        # dotfile originals that look like secrets
│   └── pass/
├── data/
│   └── ws-log/          # recorded sessions
└── archive/
```

`ws init` is idempotent. It creates what is missing, fills in any keys missing from an
existing `ws.json` (keeping its values), and leaves every other existing file alone.

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
  "dotfile": {
    "git": { "remote_url": "", "branch": "main", "auto_commit": true, "auto_push": true, "pass_entry": "" }
  },
  "secret": { "allowlist": [], "skip_dirs": [] },
  "pass": { "pass_entry": "" },
  "ignore": { "warn_size_mb": 1, "crit_size_mb": 10, "max_depth": 6, "muted": [] },
  "ignore_rules": { "schema": 1, "exclude": [], "safe_harbors": [], "suppress_defaults": [], "suppress_harbors": [] },
  "session": { "cap_mb": 500, "max_session_mb": 100 },
  "links": []
}
```

| Key | Meaning | Written by |
| --- | --- | --- |
| `editor` | the editor every `edit` and `open` command uses; empty means `$VISUAL`, then `$EDITOR`, then `vim` (§4.10) | hand |
| `scratch.root_dir` | where scratch dirs live | hand |
| `scratch.tags` | tags used so far, offered as completions by `scratch tag` | `scratch tag` |
| `repo.roots` | directories searched for git repos | `repo root add\|rm` |
| `repo.exclude_dirs` | directories never searched | hand |
| `repo.max_parallel` | reserved: fleet commands run one repo at a time today | hand |
| `dotfile.git.*` | the dotfile backup remote and its behaviour | `dotfile backup enable\|disable` |
| `secret.allowlist` | findings marked as false positives (`file:line` anchors) | `secret allow`, `secret store` |
| `secret.skip_dirs` | directories secret scanning skips (muted) | `secret mute\|unmute` |
| `pass.pass_entry` | the pass entry with the API token for the pass backup's visibility check | `pass backup enable` |
| `ignore.*` | thresholds for sync warnings | hand |
| `ignore_rules.exclude` | paths ignored by sync (user rules, on top of the built-in template) | `ignore add\|rm` |
| `ignore.muted` | paths whose sync warnings are hidden; they keep syncing | `ignore mute\|unmute` |
| `ignore_rules.safe_harbors` | always-sync directories (`<dir>/**`), made by the old suppress | `ignore edit` |
| `session.cap_mb`, `max_session_mb` | total cap for recordings; per-session limit | hand |
| `links[]` | managed dotfiles: `system`, `original` (workspace-relative), `dir`, `sudo` | `dotfile add\|link --to\|unlink` |

Renamed keys are still read, and only the new name is written: `session` (was `log`).
`scratch.editor_cmd` and `links[].class` are no longer read or written. Set `editor` to
keep a GUI editor such as `code`.

### 2.4 Resolution order

```text
flag  →  environment variable  →  ws.json  →  built-in default
```

The workspace root resolves first, then the state file inside it, then every other value.

---

## 3. Command grammar

### 3.1 Rules

1. **One shape.** `ws <noun> <verb> [targets…] [flags]`. Nouns are singular. Two nouns
   have sub-nouns: `repo root` and `backup` (under `dotfile` and `pass`).
2. **One noun per TUI screen.** `ws <noun> ls` prints exactly the rows of that screen, with
   the same fields as its `--json` records. The Ignore screen also has a view for
   `ignore lint`, and opens on it.
3. **One meaning per verb.** The glossary (§3.3) is closed. A new command uses a glossary
   verb, or a domain word that its users already know (git's `fetch`, the trash's
   `restore`). It never reuses a verb with a different meaning.
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
| `dotfile` | system files whose originals live in the workspace, symlinked back | Dotfiles |
| `scratch` | throwaway directories under `scratch.root_dir` | Scratch |
| `session` | recorded terminal sessions | Sessions |
| `ignore` | what MEGA syncs: ignore rules, muted directories, sync warnings | Ignore |
| `secret` | secret findings in workspace files | Secrets |
| `pass` | the pass(1) password store | Secrets (header, `:pass …`) |
| `credential` | ws as git's credential helper, backed by pass | Secrets (header, `:credential …`) |
| `cron` | ws-managed cron jobs | Cron |
| `trash` | the XDG trash and the soft-delete integrations | Trash |
| `config` | `ws.json` | `:config …` |
| `completion` | shell completion | — |

Workspace commands without a noun: `ws init`, `ws tui`, `ws version`, `ws help`. Bare `ws`
prints help.

### 3.3 Verb glossary

| Verb | Means | Used by |
| --- | --- | --- |
| `ls` | list the noun's rows | every noun |
| `show` | one row in full | repo, secret, session, config |
| `add` | put an existing thing under ws management | dotfile, scratch, ignore, cron, repo root |
| `new` | create a fresh thing | scratch |
| `rm` | delete something ws owns, soft-deleting files | scratch, session, trash, cron, ignore, repo root |
| `edit` | open a file in your editor (§4.10) | dotfile, ignore, config |
| `open` | open a directory in your editor (§4.10) | scratch |
| `enable` / `disable` | switch an integration on or off | trash, credential, completion, backup |
| `status` | the health of an integration (not a list of rows) | trash, credential, pass, backup |
| `mute` / `unmute` | hide / show warnings for a path and everything under it; nothing else changes | ignore, secret |
| `prune` | delete rows by age (`--older-than`) or by the configured size cap | session, trash |
| `grep` | search inside contents | session |
| `run` | execute now | repo (a command in each repo), cron (a job) |
| `save` | stage and commit everything with a timestamped message, pull, push | repo |
| `push` | git push, nothing else | repo, backup |
| `lint` | report what isn't hygienic | ignore |
| `configure` | ask for what a setup is missing, then set it | repo |
| `apply` | write the declared state into the live system | cron |
| `fetch`, `pull` | git fetch; git pull (fast-forward, or the rebase or merge you choose) | repo |
| `link`, `unlink` | point a system path at its workspace copy (the workspace copy wins; also repairs); put the real file back and stop managing it | dotfile (`add` is its other direction: the live file wins) |
| `tag` | label a row | scratch |
| `record`, `stop` | record a shell session; stop the recording | session |
| `check`, `tree` | will this path sync?; browse the tree with sync status | ignore |
| `allow`, `store` | mark a finding as a false positive; copy its value into pass | secret |
| `init`, `audit` | create; inspect entries for problems | pass (and `ws init`) |
| `restore`, `empty` | put a trashed item back; delete everything in the trash | trash |
| `log` | recent output of a job | cron |

`completion bash|zsh|fish` names a shell, not a verb. It is the one exception to the
glossary.

### 3.4 Every command

RO commands never change anything. RW commands build an action plan (§4.7).

```text
ws repo ls [repo…] [--fetch]              RO  repos with status and hygiene findings
ws repo show <repo>                       RO  one repo in full
ws repo fetch [repo…]                     RW  git fetch
ws repo pull [repo…] [--rebase|--merge]   RW  git pull; asks rebase or merge where diverged
ws repo push [repo…]                      RW  git push, nothing else
ws repo save [repo…] [-m msg] [--rebase|--merge]  RW  stage, commit (timestamped), pull, push
ws repo configure [repo…]                 RW  ask for missing identity, upstream, default branch, credentials
ws repo run [repo…] -- <cmd…>             RW  run a command in each repo
ws repo root add <dir>                    RW  add a discovery root
ws repo root ls                           RO  list discovery roots
ws repo root rm <dir>                     RW  remove a discovery root

ws dotfile ls                             RO  managed dotfiles with link health
ws dotfile add <path…> [--to <ws-path>] [--dir]  RW  take live files into the workspace, symlink back (new, or again)
ws dotfile link [path…]                   RW  point paths at their workspace copies (all broken ones by default)
ws dotfile link <path> --to <ws-path>     RW  manage a path as a link to a file already in the workspace
ws dotfile unlink <path…> | --all         RW  put the real file back, stop managing it
ws dotfile edit <path>                    RW  open the workspace copy in your editor
ws dotfile backup enable [--remote <url>] RW  set up the private git backup
ws dotfile backup disable                 RW  stop backing up
ws dotfile backup status                  RO  remote, ahead/behind, uncommitted, recent commits
ws dotfile backup push                    RW  commit pending changes and push

ws scratch ls                             RO  scratch directories
ws scratch new [name]                     RW  create one and open it
ws scratch add <path…>                    RW  move an existing path into scratch
ws scratch open <name>                    RO  open one in the editor
ws scratch tag <name> [tag…] [--rm tag]   RW  label one (tags are for you to read)
ws scratch rm <name…>                     RW  soft-delete

ws session ls                             RO  recorded sessions
ws session record [tag]                   RW  record a shell session
ws session stop                           RW  stop the running recording
ws session show <tag> [--commands]        RO  print a transcript
ws session grep <text>                    RO  search transcripts
ws session rm <tag…>                      RW  delete sessions
ws session prune [--older-than <age>|--all] RW delete old sessions, or enforce the size cap

ws ignore ls [--muted]                    RO  every path that doesn't sync, and its rule; --muted: muted warnings
ws ignore lint [path…]                    RO  check synced files and warn about the unhygienic ones
ws ignore add <path…>                     RW  stop syncing a path
ws ignore rm <path…>                      RW  sync a path again
ws ignore mute <path…>                    RW  hide a path's warnings (it keeps syncing)
ws ignore unmute <path…>                  RW  show them again
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
ws cron add <job…>                        RW  install jobs or presets
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

ws config show [--defaults]               RO  resolved config, or the built-in defaults
ws config edit                            RW  edit ws.json in your editor

ws completion bash|zsh|fish               RO  print a completion script
ws completion enable [--shell s]          RW  load completion from your shell rc file
ws completion disable [--shell s]         RW  remove it

ws init                                   RW  create the workspace layout
ws tui                                    RO  the full-screen interface
ws version                                RO  version, schema and platform
ws help [noun [verb…]]                    RO  help
```

### 3.5 Renamed and removed commands

Every old name below still works as a hidden alias (§3.1 rule 6), except the interactive
`ignore fix` and `secret fix`. Those have no single successor: typed, they print the
commands that replace them and exit 1.

| Old | New |
| --- | --- |
| `repo scan` | `repo ls` (add `--fetch` to fetch first) |
| `repo add-root`, `repo ls-roots` | `repo root add`, `repo root ls` |
| `dotfile scan` | `dotfile ls` |
| `dotfile rm`, `dotfile reset` | `dotfile unlink`, `dotfile unlink --all` |
| `dotfile fix` | `dotfile link` |
| `dotfile sync <path>` | `dotfile add <path>` |
| `dotfile link <path> <ws-path>` | `dotfile link <path> --to <ws-path>` |
| `dotfile git setup\|enable\|connect\|remote` | `dotfile backup enable [--remote]` |
| `dotfile git disconnect` | `dotfile backup disable` |
| `dotfile git status\|log` | `dotfile backup status` |
| `dotfile git push` | `dotfile backup push` |
| `scratch search` | `scratch ls --filter` (names and tags) |
| `log start\|stop\|ls\|rm\|prune` | `session record\|stop\|ls\|rm\|prune` |
| `ignore scan` | `ignore lint` |
| `ignore scan --expand-suppressed` | `ignore ls --muted` |
| `repo doctor` | `repo ls` |
| `config --defaults` | `config show --defaults` |
| `repo sync` | `repo save` |
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

Built-in cron jobs: `dotfile-sync` is now `dotfile-backup` and `log-prune` is now
`session-prune`. The old names keep running from existing crontab lines. `pass-backup` is
new. `secret-scan`, `ignore-scan` and `repo-sync` are retired: nothing commits your work
unattended.

Removed, no replacement: `ws reset`, `ws restore`, `ws capture`, `ws search`, `ws scratch grep`,
`ws ignore generate`, `ws dotfile migrate` (a one-off legacy split; hidden until deleted),
the `--manifest` global flag, `dotfile add|link --class` (use `--to`; typed, it is an error
that says so), and exit code `2`.

---

## 4. CLI conventions

### 4.1 Global flags

```text
-w, --workspace <dir>   workspace root (default: $WS_WORKSPACE, then ~/Workspace)
-c, --config <file>     state file (default: $WS_CONFIG, then <workspace>/ws.json)
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
| `--filter <expr>` | keep rows where `key=text` (the field contains text), `key!=text` (it does not), or bare `text` (any field contains it). Case-insensitive. Repeat it, and all must match. The TUI's `/` takes the same expressions. |
| `--sort <field[,field]>` | order rows; `-field` descends |
| `--wide` | show every field as a column |
| `--json` | one record per row, keyed by field name, with every field |

An unknown field is an error that names the valid ones. Listing is not a verdict: `ls`
exits 0 whether or not the rows show problems.

### 4.4 JSON

Every `--json` response has an envelope:

```json
{ "ws_version": "0.20.0", "schema": 3, "command": "dotfile.link", "data": { } }
```

`command` is `<noun>.<verb>` (`repo.root.add`, `dotfile.backup.push`). The workspace
commands without a noun use their name: `init`, `version`. Write commands
include `data.actions`, the status of each plan action. `schema` increments on breaking
changes to `data`.

History: `1` initial. `2` every path field is absolute. `3` command names follow the
grammar in Part 3, and `repo ls`, `ignore lint` and `secret ls` return the records the old
`scan` commands returned.

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
  refs and URLs, pass entry names, and names of ws-managed items (scratch names, session
  tags, cron jobs). When a command acts on the file behind a name, it prints the file's
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
- Tab completion offers absolute paths for ws-known items (repos, dotfiles), and the
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
  independently meaningful mutation** (one per file, repo, session, job). The test: would
  someone want to accept this one but skip the next? If so, they are separate actions.
- Each action is confirmed on its own: `y` (default, Enter) do it, `n` skip it, `a` accept
  all the rest, `q` skip all the rest. EOF on stdin counts as `y`.
- `--dry-run` prints the plan and stops. `--quiet` and `--json` accept every action.
- A failed action never stops the others. The exit code is 0, 1 or 3 by outcome (§4.6),
  and `--json` includes every action's status.
- Destructive plans (`dotfile unlink --all`, `trash empty`) start with every action
  unchecked in the TUI, and the CLI asks per action as usual.

Exemptions, which run without a plan:

- `session record` (it hands the terminal to a recorded shell, and leaving the shell ends
  it) and `session stop`;
- `repo fetch` and `repo ls --fetch`, which change only remote-tracking refs, never a branch
  or the working tree;
- `cron run`, which crontab lines call, so the command itself is the confirmation;
- the `edit` commands (`dotfile edit`, `ignore edit`, `config edit`), where the editor is the
  confirmation. `ignore edit` and `config edit` validate before they save.

### 4.8 Completion

Shell completion (`ws completion …`) offers exactly the commands, flags and dynamic
targets this spec documents: repo paths, dotfile system paths, scratch names, session
tags, cron jobs, trash items, `file:line` findings. It never offers hidden aliases. Tests
keep completion and help in step (see dev.md §12).

### 4.9 Environment variables

| Variable | Effect |
| --- | --- |
| `WS_WORKSPACE` | workspace root (after `--workspace`) |
| `WS_CONFIG` | state file (after `--config`) |
| `NO_COLOR` | no colors or icons ([no-color.org](https://no-color.org)) |
| `VISUAL`, `EDITOR` | your editor, when `ws.json` `editor` is empty (§4.10) |
| `PASSWORD_STORE_DIR` | pass store location |
| `XDG_STATE_HOME`, `XDG_DATA_HOME` | machine-local state and trash locations |
| `WS_NO_MOUSE` | TUI: leave the mouse to the terminal |
| `WS_NO_EMOJI` | TUI: no emoji |
| `WS_CRONTAB_FILE` | tests: a file that stands in for the user's crontab |

### 4.10 Editor

Every command that opens an editor (`dotfile edit`, `ignore edit`, `config edit`,
`scratch new`, `scratch open`, and `e` in the TUI) uses the same one:

1. `ws.json` `editor`, when it is set (it may carry arguments: `code -w`, `nvim -p`);
2. else `$VISUAL`;
3. else `$EDITOR`;
4. else **`vim`**.

A terminal editor (`vim`, `nvim`, `vi`, `nano`, `emacs -nw`, `hx`, …) takes over the
terminal until it exits. In the TUI, the TUI suspends while it runs. Any other editor (a
GUI such as `code`) is started in the background. When the editor cannot start, ws says
which one it tried and prints the path. The old `scratch.editor_cmd` is not read. To keep
using a GUI editor, set `editor` (for example `"code"`).

---

## 5. Command reference

One chapter per noun, in the TUI's tab order. Each chapter opens with a table of its
commands, and each command lists what it does, its flags, output, JSON, exit codes, its TUI
key, and what it was called before.

- **Was:** names the old command. Old names keep working as hidden aliases (§3.5).
- **(new)** marks a command or behaviour that the code doesn't have yet.
- **(decision)** marks a choice this spec made where the old code or spec left the
  question open, or got it wrong. "(decision, by the user)" marks one the user made.

### `ws repo`

A repo is a git repository inside the workspace. ws does not keep a list of repos. Every
command discovers them again by walking the discovery roots in `ws.json` `repo.roots`. Each
row pairs the repo's git status (branch, dirty, ahead/behind its upstream) with its hygiene
findings. The Repos screen shows the same rows.

| Command | RO/RW | Does | TUI |
| --- | --- | --- | --- |
| `ws repo ls [repo…] [--fetch]` | RO | List status and hygiene findings; `--fetch` fetches first | screen |
| `ws repo show <repo>` | RO | Show one repo in full: status, upstream, every finding | `enter` |
| `ws repo fetch [repo…]` | RW | `git fetch --all --prune` in each repo | `f` |
| `ws repo pull [repo…] [--rebase \| --merge]` | RW | Pull each repo; asks rebase or merge where it has diverged | `p` |
| `ws repo push [repo…]` | RW | Push each repo's commits to its upstream | `:push` |
| `ws repo save [repo…] [-m <msg>] [--rebase \| --merge]` | RW | Stage and commit everything with a timestamped message, pull, push | `s` |
| `ws repo configure [repo…]` | RW | Ask for whatever a repo's git setup is missing (identity, upstream, default branch, credentials) and set it | `c` |
| `ws repo run [repo…] -- <cmd…>` | RW | Run a command in each repo | `r` |
| `ws repo root add <dir>` | RW | Add a discovery root | `a` |
| `ws repo root ls` | RO | List the discovery roots | `:root ls` |
| `ws repo root rm <dir>` | RW | Remove a discovery root (new) | `:root rm` |

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

**Filter flags.** `ls`, `fetch`, `pull`, `push`, `save`, `configure` and `run` take these. Each flag narrows the
targets further, and all of them must match. A target that a filter removes is left out
quietly. Filters read the repo state as of the last fetch, except in `pull`, `save` and
`ls --fetch`, which fetch first and filter afterwards. With `--dry-run`, nothing fetches:
the plan comes from the last fetch, and says how old it is.

| Flag | Default | Meaning |
| --- | --- | --- |
| `--path <dir>` | none | Only repos at or under `<dir>` (path rules; whole path components) |
| `--dirty` | off | Only repos with uncommitted changes (untracked files count) |
| `--ahead` | off | Only repos ahead of their upstream |
| `--behind` | off | Only repos behind their upstream |
| `--detached` | off | Only repos on a detached HEAD |

**Git.** Commands run one repo at a time. Every git command that ws itself runs uses
`GIT_TERMINAL_PROMPT=0`, so git never asks for credentials. A repo that needs them fails
with git's own message.

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
| `credentials` | info | the remote is HTTPS and git has no credential helper (new) |

Every finding's detail ends with its fix: `ws repo configure <repo>`.

**Diverged repos.** When a repo and its upstream have both moved, a pull must rebase or
merge. ws never chooses for you. `pull` and `save` ask once per diverged repo:

```text
/home/user/Workspace/data/bruno has diverged (2 local, 1 remote commit).
Pull with [r]ebase, [m]erge, or [s]kip this repo?
```

`--rebase` or `--merge` answers for every repo ahead of time. Without a terminal (cron,
pipes, `--quiet`, `--json`), and without one of those flags, a diverged repo is skipped
with the reason `diverged: choose --rebase or --merge`. A fast-forward needs no choice. A
failed rebase or merge is aborted, so no repo is left half-way, and the action fails.

#### `ws repo ls`

```text
ws repo ls [repo…] [--fetch] [--check <id>]… [filter flags] [--sort f] [--filter expr] [--wide]
```

This lists the selected repos with their status and hygiene findings, one row per repo. By
default it reads only local state and touches no repo. With `--fetch`, it first runs
`git fetch --all --prune` in every target repo, then reads status, then applies the filter
flags. A failed fetch prints `ws: fetch failed: <path>: <git error>` on stderr. The repo
still appears in the list, with its counts from the last successful fetch. A fetch has no
plan and no prompt (decision) because it changes only remote-tracking refs. Hygiene checks
run last, so `fetch-staleness` reflects the fetch. `--dry-run` skips the fetch. With no
targets, it lists every discovered repo.

| Flag | Default | Meaning |
| --- | --- | --- |
| `--fetch` | off | Fetch each target repo before reading its status |
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
and `data` holds the records the old `repo scan` returned:
`{"statuses": [row…], "findings": [{"repo", "check", "severity", "detail"}…], "fetch_warnings": ["<path>: <error>"…]}`.
`findings` includes `info` findings and is `[]` when there are none. `fetch_warnings` is
present only after `--fetch` when a fetch failed.

Exit: `0`, including when repos are dirty or have findings. `1` on a bad flag, an unknown
check, an unknown target, or no `git` in PATH. `3` when `--fetch` could not fetch some repo
(the list still prints) (decision).

TUI: the Repos screen is this list. It opens without fetching, `f` fetches, and `ctrl+r`
reads status again without fetching. The status line sums the fleet:
`4 repos · 1 dirty · 1 ahead · 1 behind · 1 issue`, plus `N fetch failed` after a fetch.

Was: `repo scan` (hidden alias for `repo ls --fetch`; `repo scan --no-fetch` maps to
`repo ls`) and `repo doctor` (hidden alias for `repo ls`; it used to exit 1) (decision). The
old `repo ls`, which printed only paths, is replaced. The old `--verbose` behavior is gone:
every finding is in the row, and `repo show` prints them all.

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

TUI: `enter` on a row opens this view as the detail level. `esc` goes back. The TUI adds one
line, `Last fetch  failed: <error>`, when the last fetch of this repo during the session
failed (decision: the TUI shows only this repo's failure, not the whole fleet's). `d` shows
every field of the row, and `y` shows the row as JSON.

Was: new (the TUI detail level existed already).

#### `ws repo fetch`

```text
ws repo fetch [repo…] [filter flags]
```

This runs `git fetch --all --prune` in each selected repo. It updates only remote-tracking
refs. Branches and the working tree stay as they are. There is no plan and no prompt
(decision: a documented exemption, like `ls --fetch`). `--dry-run` prints
`Would fetch <path>` for each repo and fetches nothing. With no targets, it fetches every
discovered repo.

Output: one line per repo, `✔ <path> fetched` or `✘ <path> failed: <git error>`. In
`--json` the command is `repo.fetch`, and `data` is `[{"path", "success", "error"}…]`.

Exit: `0` when every fetch succeeds, `3` when some fail, `1` when all fail (decision).

TUI: `f` fetches the marked repos, else every repo the filter shows, then reads status
again. The status line shows `Fetching N repos…` while it runs. Failures are counted in the
status line and shown in each repo's detail.

Was: `repo fetch` (unchanged). `repo pull` and `repo save` fetch for themselves.

#### `ws repo pull`

```text
ws repo pull [repo…] [--rebase | --merge] [filter flags]
```

This brings each selected repo up to date with its upstream. ws fetches first, then plans
one action per repo that is behind. A repo that is only behind is fast-forwarded
(`git pull --ff-only`). A diverged repo is pulled with the strategy you choose for it (see
"Diverged repos"). Repos that cannot be pulled are left out, and ws prints
`Skipped: <path> (<reason>)` for each: `error: <git error>`, `detached HEAD`,
`no upstream (ws repo configure sets one)`, `fetch failed: <git error>`, or
`diverged: choose --rebase or --merge`. Uncommitted changes stay as they are. If git
refuses because they would be overwritten, the action fails with git's message. With no
targets, it covers every discovered repo.

| Flag | Default | Meaning |
| --- | --- | --- |
| `--rebase` | — | rebase diverged repos, without asking |
| `--merge` | — | merge diverged repos, without asking |
| filter flags | — | See "Selecting repos" |

Plan: `Pull <path> (3 behind, fast-forward)` or `Pull <path> (diverged, rebase)`, one per
repo, pre-checked. `--dry-run` asks and prints the plan, planned from the last fetch (it
fetches nothing). In `--json` the command is `repo.pull`, and `data` is `{"actions": […], "skipped": [{"path", "reason"}…]}`.

Exit: `0`, `1` (every action failed, or a bad flag or target), `3` (some failed).

TUI: `p` fetches the marked repos (else every repo the filter shows), asks rebase or merge
for each diverged one in a chooser, then opens the checklist `Pull N repos`.

Was: `repo pull --rebase` (rebase for every repo, never asked). `--ff-only` was the default
and still is. The TUI key `P` is gone.

#### `ws repo push`

```text
ws repo push [repo…] [filter flags]
```

(new) This pushes each selected repo's commits to its upstream. It does nothing else: it
does not commit, fetch or pull. ws plans one action per repo that is ahead of its upstream,
`Push <path> (2 ahead)`. Repos that cannot be pushed are skipped with a reason:
`behind or diverged: pull first (or ws repo save)`, `no upstream (ws repo configure sets
one)`, `detached HEAD`, `nothing to push`. Uncommitted changes are not pushed, and the
command says so per repo (`3 uncommitted changes stay local`). With no targets, it covers
every discovered repo. ws never force-pushes.

Plan: one action per repo, pre-checked. In `--json` the command is `repo.push`, and `data` is
`{"actions": […], "skipped": [{"path", "reason"}…]}`.

Exit: `0`, `1`, `3`.

TUI: `:push` acts on the marked repos, else every repo the filter shows. The checklist
lists the repos that are ahead.

Was: new. The old `repo sync` pushed as part of a pull-commit-push sequence.

#### `ws repo save`

```text
ws repo save [repo…] [-m <message>] [--rebase | --merge] [filter flags]
```

(new) This is for repos you edit in one place, such as `data/bruno`: one command saves
your work and takes it to the remote. For each selected repo, in order:

1. **Commit.** If the repo has changes, ws stages everything (`git add -A`, untracked files
   included) and commits with a timestamped message,
   `ws save 2026-10-22 14:05 (laptop)` (the local time and the host name), or with `-m`.
2. **Pull.** ws fetches. If the upstream has moved, a fast-forward or the strategy you chose
   brings the repo up to date (see "Diverged repos").
3. **Push.** If the repo is now ahead, ws pushes.

A repo with nothing to commit, pull or push gets no action. Repos are skipped, with a
reason, on a detached HEAD, without an upstream (`ws repo configure sets one`), on a
failed fetch, or when diverged without a choice. The commit stays when a later step fails,
so no work is lost, and the message names the step that failed:
`pull failed: <git error>` or `push failed: <git error>`.

| Flag | Default | Meaning |
| --- | --- | --- |
| `-m <message>` | `ws save <date time> (<host>)` | the commit message |
| `--rebase`, `--merge` | ask | the strategy for diverged repos |
| filter flags | — | See "Selecting repos" |

Plan: one action per repo, pre-checked, naming every step:
`Save /home/user/Workspace/data/bruno: commit 3 changes, pull (fast-forward), push`.
`--dry-run` asks and prints the plan, planned from the last fetch. In `--json` the command
is `repo.save`, and
`data` is `{"actions": […], "skipped": [{"path", "reason"}…]}`.

Exit: `0`, `1`, `3`.

TUI: `s` fetches the marked repos (else every repo the filter shows), asks rebase or merge
for each diverged one, then opens the checklist `Save N repos`.

Was: new. The old `repo sync` committed and pushed with a fixed message, and chose merge
for you. The `repo-sync` cron job is retired with it (decision: nothing commits your work
unattended).

#### `ws repo configure`

```text
ws repo configure [repo…] [filter flags]
```

(new) This fixes each selected repo's hygiene findings by asking you for what is missing,
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

Was: new. Before, hygiene findings had no fix in ws.

#### `ws repo run`

```text
ws repo run [repo…] [filter flags] -- <cmd> [arg…]
```

This runs a command in each selected repo, with the repo root as its working directory.
Everything after `--` is the command, and the arguments before it are repo targets. Without
`--`, every positional argument is the command and there are no targets (decision; this is
how it worked before). The CLI runs the command directly, not through a shell. Use
`sh -c '…'` for pipes. Stdin is not connected. ws captures stdout and stderr together. The
plan has one action per repo, `Run in <path>`. The CLI prompts for each one, and the TUI
pre-checks all of them. A non-zero exit fails that action. With no targets, the command runs
in every discovered repo. Without a command, ws prints `usage: ws repo run [repo…] -- <cmd…>`
and exits 1.

| Flag | Default | Meaning |
| --- | --- | --- |
| filter flags | — | See "Selecting repos" |

Output: after each action line, the command's output, indented (decision: it used to be
dropped).

```text
$ ws repo run --dirty -- git status --short
  ✔ Run in /home/user/Workspace/notes/second-brain
      M index.md
```

`--dry-run` prints the actions and runs nothing. In `--json` the command is `repo.run`, and
`data` is
`{"command": [arg…], "actions": [{"id", "status", "error"}…], "results": [{"path", "exit_code", "output"}…]}`.

Exit: `0`, `1`, or `3`, as for `pull`.

TUI: `r` asks for a command line and runs it with `sh -c`. The prompt applies to the marked
repos, else to every repo the filter shows. A checklist follows, where repos can be
unticked. When it finishes, the output view shows `✔` or `✘ (exit N)` and the output for
each repo that ran, failures included. `esc` goes back to the list.

Was: `repo run` (unchanged). The TUI key was `x`.

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

TUI: `a` asks for a directory, with directory completion and a hint listing the current
roots. A one-action checklist follows. Afterwards the list is read again over the new
roots.

Was: `repo add-root` (hidden alias).

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

Was: `repo ls-roots` (hidden alias; its numbered list is gone).

#### `ws repo root rm`

```text
ws repo root rm <dir>
```

(new) This removes a discovery root. `<dir>` follows the path rules. ws compares it to each
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

Was: new.

---

### `ws dotfile`

A dotfile is a system path, such as `/home/user/.bashrc` or `/etc/hosts`, that is a symlink
to a file kept in the workspace. The rows are the `links[]` entries in `ws.json`, each
checked against the filesystem for link health, and the Dotfiles screen shows them.
`ws dotfile backup` versions `configs/dotfiles/` in git and pushes it to a private remote.

**Two directions.** When both a live file and a workspace copy exist, the verb decides
which one wins:

- `add`: the **live file** goes into the workspace. This starts managing a new path, or,
  for an entry whose link something overwrote, takes the live file as the new workspace
  copy.
- `link`: the **workspace copy** goes out to the system path, as a symlink. This repairs
  broken and overwritten links, and puts every link back on a new machine. With `--to`,
  it starts managing a path as a link to a file already in the workspace.
- `unlink` reverses both: it puts a real file back and stops managing the path.

| Command | RO/RW | Does | TUI |
| --- | --- | --- | --- |
| `ws dotfile ls` | RO | managed dotfiles with link health | screen |
| `ws dotfile add <path…> [--to <ws-path>] [--dir]` | RW | take live files into the workspace and link them back | `a` |
| `ws dotfile link [path…]` | RW | point paths at their workspace copies (all broken or overwritten ones when none are given) | `l` |
| `ws dotfile link <path> --to <ws-path>` | RW | manage a path as a link to a file already in the workspace | `:link` |
| `ws dotfile unlink <path…> \| --all` | RW | put a real file back at the system path, stop managing it | `ctrl+d`, `:unlink --all` |
| `ws dotfile edit <path>` | RW | open the workspace copy in `$EDITOR` | `e` |
| `ws dotfile backup enable [--remote <url>]` | RW | set up the private git backup | `:backup enable` |
| `ws dotfile backup disable` | RW | stop backing up | `:backup disable` |
| `ws dotfile backup status` | RO | remote, ahead/behind, uncommitted, recent commits | `:backup status` |
| `ws dotfile backup push` | RW | commit pending changes and push | `b` |

#### Terms

- **Entry.** One `links[]` record: `system` (absolute system path), `original` (the
  workspace copy, workspace-relative), `dir` (the entry is one whole directory), `sudo`
  (the system side needs root). There is no class: where the copy lives says what it is.
  (decision, by the user: `--to` replaced `--class`.)
- **Workspace copy.** The file or directory `original` names. The system path is a
  symlink to its absolute path.
- **Owned copy.** A workspace copy under `configs/dotfiles/` or `secrets/dotfiles/` that no
  other entry points at. `add` makes these, and ws may move or replace them. Any other
  workspace copy is **shared**: a `link` target such as `agent/AGENTS.md`, or a copy that two
  entries point at. ws never moves or deletes a shared copy.
- **Storage path.** Where `add` puts a file. ws takes the system path relative to `$HOME`
  (or to `/` outside home), drops the leading dot of its first component (a bare `.`
  becomes `dot`), and puts it under `secrets/dotfiles/` when the file looks like a secret,
  else under `configs/dotfiles/`. `--to` puts it anywhere in the workspace instead.

  | System path | Workspace copy |
  | --- | --- |
  | `/home/user/.bashrc` | `/home/user/Workspace/configs/dotfiles/bashrc` |
  | `/home/user/.config/Code/User/settings.json` | `/home/user/Workspace/configs/dotfiles/config/Code/User/settings.json` |
  | `/home/user/.kube/config` (secret) | `/home/user/Workspace/secrets/dotfiles/kube/config` |
  | `/etc/docker/daemon.json` | `/home/user/Workspace/configs/dotfiles/etc/docker/daemon.json` |

- **Sudo entries.** `add` and `link --to` set `sudo` when the system path, or its nearest
  existing parent, is owned by root. Every system-side change for such an entry (move,
  symlink, copy, remove, mkdir) runs through `sudo`, which may ask for a password on the
  terminal. Without a terminal (cron), the action fails unless sudo needs no password. The
  TUI hands actions on sudo entries to the CLI in the terminal (§6.8), so sudo can prompt.
  (decision)
- **Targets.** Path arguments follow §4.5 and must match an entry's system path. A target
  that matches no entry is an error, and ws exits 1 before changing anything.
- **`.ws-backup`.** When ws must put a symlink where something else now stands, it first
  renames that thing to `<path>.ws-backup`, or `<path>.ws-backup.1`, `.2`, … (the first
  free name). It never overwrites an earlier backup and never deletes the live file.
- **Auto-commit.** After `add`, `link --to` or `unlink` changes at least one entry, ws
  commits the backup repo when backup is enabled and `auto_commit` is on, then pushes when
  `auto_push` is on (see `ws dotfile backup`). One commit covers the whole plan, even when
  some of its actions failed. (decision: before, any failure skipped the commit.) A failed
  commit or push prints a warning and does not change the exit code.

#### `ws dotfile ls`

```text
ws dotfile ls [--filter <expr>] [--sort <fields>] [--wide] [--json]
```

Lists every entry with its link health. It reads `links[]` and inspects each system path
(lstat and readlink only). It changes nothing and never needs sudo. `ls` takes no targets:
use `--filter status!=ok` to list only the problems.

| Field | Meaning |
| --- | --- |
| `system` | the system path |
| `workspace_path` | absolute path of the workspace copy |
| `original` | the workspace copy as stored in `ws.json` (workspace-relative) |
| `status` | `ok`, `broken` or `overwritten` |
| `message` | why the entry is not `ok` |
| `dir` | the entry is one whole directory |
| `sudo` | system-side changes need root |

Default columns: `system`, `workspace_path`, `status`, `sudo`. (decision:
`workspace_path` replaces `original` as a column, because printed paths are absolute.)

| Status | Message | Meaning |
| --- | --- | --- |
| `ok` | | the symlink points at the workspace copy, and the copy exists |
| `broken` | `system path is missing` | nothing is at the system path |
| `broken` | `symlink target is missing` | the link is right, but the workspace copy is gone |
| `broken` | `failed to read symlink target` | the symlink cannot be read |
| `overwritten` | `system path is no longer a symlink` | a real file or directory replaced the link |
| `overwritten` | `symlink points to an unexpected target` | the symlink points somewhere else |

`link` repairs every status except a missing workspace copy (the workspace copy wins).
`add` on an overwritten entry keeps the live file instead.

Output:

```text
SYSTEM PATH            WORKSPACE COPY                                   STATUS       SUDO
/home/user/.bashrc     /home/user/Workspace/configs/dotfiles/bashrc     ok
/home/user/.gitconfig  /home/user/Workspace/configs/dotfiles/gitconfig  broken
/etc/hosts             /home/user/Workspace/configs/dotfiles/etc/hosts  overwritten  yes
```

`--json`: `dotfile.ls`. `data` is the list of records, with every field above.

Exit: 0, also when entries are broken. 1 when `ws.json` cannot be read.

TUI: the Dotfiles screen. Status shows as `✔ ok`, `✘ broken` or `⚠ overwritten`, and rows
whose copy is under `secrets/` are highlighted. `d` shows every field.

Was: `dotfile ls`, and `dotfile scan` (hidden alias; it no longer exits 2).

#### `ws dotfile add`

```text
ws dotfile add <path…> [--to <ws-path>] [--dir] [--dry-run]
```

Takes the live file at each path into the workspace and leaves a symlink at the system path
pointing at the workspace copy. At least one path is required. What happens depends on the
path:

| The path is | `add` |
| --- | --- |
| not managed yet | moves it into the workspace and records a new entry (below) |
| an **overwritten** entry: a real file replaced the link | keeps the live file: it becomes the new workspace copy, the old copy goes to the trash, and the link is restored (below, "Taking a live file again") |
| an `ok` entry | nothing to do; reported as already managed |
| a `broken` entry with nothing at the system path | nothing to take; reported with the hint `ws dotfile link <path>` |

**A new path:**

- **A file** becomes one action: move it to its storage path (creating parent
  directories), symlink the system path to it, and record the entry.
- **A directory without `--dir`** is expanded, and ws asks which entries to manage. A
  directory with 20 files or fewer lists each file. A larger one lists its immediate
  children, with file counts and sizes, so you choose per subdirectory. Entries that look
  like config are selected by default. Entries that look like app state (`cache`, `logs`,
  `history`, `sessions`, binaries, directories over 100 files or 10 MB) or like secrets are
  not. Press Enter for the defaults, or type `all`, `none`, or numbers (`1,3,4`). Each
  chosen file, and each regular file inside a chosen subdirectory, becomes one action.
  Symlinks, sockets and other special files are skipped. `--quiet` and `--json` take the
  defaults.
- **`--dir`** moves the directory whole as one entry (`dir: true`), for trees that must stay
  together, such as `/home/user/.password-store`. Every path must then be a directory.
- **Where it goes.** A file that looks like a secret goes under `secrets/dotfiles/`, and
  anything else under `configs/dotfiles/`. A secret is a name like `id_ed25519`,
  `credentials`, `*.pem`, `*.key`, `*.gpg` or `*_token`, or a PEM private key in the first
  512 bytes. A `--dir` entry is judged by the directory's name alone. `--to` overrides it.
- **Secret warning.** Unless `--to` or `--dir` is given, ws lists the files that
  look like secrets before the plan and asks once, `Track anyway?`. Answering no drops
  them from the plan.
- **`--to <ws-path>`** (new) stores the workspace copy at `<ws-path>` instead of the storage
  path. It takes exactly one path. `<ws-path>` follows §4.5, must lie inside the workspace,
  and must not exist yet. To link to a file that already exists in the workspace, use
  `ws dotfile link <path> --to <ws-path>`.

An action fails when the path does not exist, is `$HOME` itself, or when its storage path
(or `--to` path) already exists. It also fails when the path is itself a symlink (add
the file it points to instead), or lies inside the workspace (a system path is outside it; to link a file the workspace
already has, use `ws dotfile link <path> --to <ws-path>`).
(decision: both of these are new checks.) `--dir` on a file is an error before the plan.

**Taking a live file again** (the old `dotfile sync`). For an overwritten
entry, the action does this:

1. The old workspace copy moves to the trash. (decision: before, it was deleted.)
2. The live file moves into its place, keeping the entry's `original`. (decision: before,
   the location was detected again, which could move a secret into `configs/dotfiles/`.)
3. The system path becomes a symlink to it again.

It fails, and changes nothing, when the system path is a symlink to somewhere else
(`link` it, or replace it with a file first), when the system path is a directory or the
entry is a directory entry (files only), or when the workspace copy is shared, made by
`link --to` such as `agent/AGENTS.md`. In that case, copy the live file over it yourself,
then run `link`. (decision: before, taking a healthy entry deleted its only copy. Now an
`ok` entry gets no action at all.)

Every action is pre-checked. Auto-commit runs after the plan.

Output:

```text
$ ws dotfile add ~/.bashrc ~/.ssh/id_ed25519
▲ 1 file(s) look like secrets:
  /home/user/.ssh/id_ed25519
Track anyway? [Y/n] y
Add /home/user/.bashrc → /home/user/Workspace/configs/dotfiles/bashrc [y/n/a/q] y
  ✔ Add /home/user/.bashrc → /home/user/Workspace/configs/dotfiles/bashrc
Add /home/user/.ssh/id_ed25519 → /home/user/Workspace/secrets/dotfiles/ssh/id_ed25519 (secret) [y/n/a/q] y
  ✔ Add /home/user/.ssh/id_ed25519 → /home/user/Workspace/secrets/dotfiles/ssh/id_ed25519 (secret)
```

An overwritten entry reads `Add /home/user/.bashrc again (the live file replaces
/home/user/Workspace/configs/dotfiles/bashrc; the old copy goes to the trash)`.

`--json`: `dotfile.add`, with `data.actions`.

Exit: 0, 1 or 3. Entries that are already managed, or broken with nothing to take, are
reported and don't change the exit code.

TUI: `a`.

- **With marked rows**, it adds those rows: the overwritten ones become actions, and the
  others are listed in the checklist's note.
- **Without marks**, it opens a path prompt with path completion. When the cursor row is
  overwritten, the prompt is pre-filled with that row's system path, so `a` then `enter`
  keeps its live file. Otherwise the prompt starts at `~/`.

A new file becomes a one-action checklist. A directory becomes a checklist of its entries:
config-like ones are pre-checked, and secrets (`⚠ looks like a secret`) and state
(`likely state`) start unchecked. For `--dir` or `--to`, use `:add <path…> [flags]`. (decision)

Was: `dotfile add` (one path only), and `dotfile sync <path>` for an overwritten entry
(hidden alias for `dotfile add <path>`).

#### `ws dotfile link`

```text
ws dotfile link [path…] [--dry-run]
ws dotfile link <path> --to <ws-path> [--dry-run]
```

Points system paths at their workspace copies. The workspace copy always wins: anything
else standing at the system path is kept as `.ws-backup`, never deleted.

**Without `--to`**, it repairs entries. With no paths, it takes every entry that is not
`ok`. This is the restore step on a new machine, once the workspace has synced. With paths,
it takes those entries, and reports the `ok` ones as already linked. Each entry is one
action:

- If nothing is at the system path, ws creates the missing parent directories and the
  symlink.
- If something else is there (a real file or directory, or a symlink to somewhere else),
  ws renames it to `.ws-backup`, then creates the symlink. To keep the live file instead,
  use `ws dotfile add <path>`.

An entry whose workspace copy is missing cannot be linked. ws leaves its system path alone,
prints the missing path, and counts the entry as failed. Restore the copy (for example with
`ws trash restore`), or `unlink` the entry. (decision: such entries are reported, not
offered as actions.) Repairing changes nothing in the backup repo, so it does not commit.

**With `--to <ws-path>`**, it starts managing `<path>` as a symlink to `<ws-path>`, a file or
directory that already exists in the workspace, and records the entry. Nothing moves into
the workspace. Use it for files the workspace keeps for another reason, such as
`/home/user/Workspace/agent/AGENTS.md` linked from `/home/user/.claude/CLAUDE.md`.

- It takes exactly one path, which must not be an entry yet.
- `<ws-path>` follows §4.5 like every path argument (absolute, `~`, or relative to the
  current directory) and must lie inside the workspace. ws stores it workspace-relative in
  `original`. (decision: before, it was relative to the workspace root.)
- If `<path>` is already a symlink to `<ws-path>`, ws only records the entry. If anything else
  is there, it is renamed to `.ws-backup` first, as when repairing. (decision: before, `link`
  refused. The workspace copy winning is what `link` means.) Missing parent directories are
  created.
- A directory `<ws-path>` makes a directory entry (`dir: true`).
- A system path that needs root gets a sudo entry, and the symlink is created with sudo.
- Auto-commit runs after it, because `ws.json` changed.

Every action is pre-checked.

Output:

```text
$ ws dotfile link
Link /home/user/.gitconfig → /home/user/Workspace/configs/dotfiles/gitconfig [y/n/a/q] y
  ✔ Link /home/user/.gitconfig → /home/user/Workspace/configs/dotfiles/gitconfig
Link /etc/hosts → /home/user/Workspace/configs/dotfiles/etc/hosts (the live file moves to /etc/hosts.ws-backup) [y/n/a/q] y
  ✔ Link /etc/hosts → /home/user/Workspace/configs/dotfiles/etc/hosts (the live file moves to /etc/hosts.ws-backup)
✖ /home/user/.npmrc: workspace copy /home/user/Workspace/configs/dotfiles/npmrc is missing

$ ws dotfile link ~/.claude/CLAUDE.md --to ~/Workspace/agent/AGENTS.md
Link /home/user/.claude/CLAUDE.md → /home/user/Workspace/agent/AGENTS.md (new entry) [y/n/a/q] y
  ✔ Link /home/user/.claude/CLAUDE.md → /home/user/Workspace/agent/AGENTS.md (new entry)
```

With nothing to repair, it prints `All dotfile links are ok.`

`--json`: `dotfile.link`, with `data.actions`. Entries with a missing copy appear there with
status `failed`. With `--to`, it also has `data.system` and `data.workspace_path`.

Exit: 0, 1 or 3.

TUI: `l` repairs the shown rows (the marked rows, else every row the filter shows). The
Dashboard's "dotfile links broken or overwritten" opens Dotfiles filtered to `status!=ok`,
so `l` links exactly those. `:link <path> --to <ws-path>` makes a new entry. Relative paths
resolve against the directory `ws tui` was started in.

Was: `dotfile fix` (hidden alias for `dotfile link`), and `dotfile link <path> <ws-path>`.
The old two-path form still works: a second argument inside the workspace can't be a
dotfile system path, so ws reads it as `--to` and prints the rename hint.

#### `ws dotfile unlink`

```text
ws dotfile unlink <path…> [--dry-run]
ws dotfile unlink --all [--dry-run]
```

Stops managing entries and leaves a real file at each system path. `--all` takes every
entry. Paths or `--all` are required, not both.

Each entry is one action:

1. If the system path holds anything other than the entry's own symlink (a real file or
   directory that replaced the link, or a symlink to somewhere else), it is renamed to
   `.ws-backup` first. (decision: before, it was deleted.)
2. The entry's symlink is removed.
3. The content goes back:
   - An **owned copy** moves back to the system path. Directories this leaves empty under
     `configs/dotfiles/` or `secrets/dotfiles/` are removed.
   - A **shared** copy (made by `link`, such as `agent/AGENTS.md`) is **copied** to the
     system path, recursively for a directory, with its modes. The original stays in the
     workspace. (decision: before, it was moved out of the workspace.)
   - When the workspace copy is missing, nothing is restored. ws removes only a dangling
     symlink at the system path.
4. The entry is removed from `links[]`.

The CLI asks per action as usual. In the TUI, `ctrl+d` pre-checks the rows you chose, and
`:unlink --all` starts with every action unchecked (§4.7). A failed entry stays
registered. Auto-commit runs after the plan.

Output:

```text
$ ws dotfile unlink ~/.bashrc ~/.claude/CLAUDE.md
Unlink /home/user/.bashrc (move /home/user/Workspace/configs/dotfiles/bashrc back) [y/n/a/q] y
  ✔ Unlink /home/user/.bashrc (move /home/user/Workspace/configs/dotfiles/bashrc back)
Unlink /home/user/.claude/CLAUDE.md (copy /home/user/Workspace/agent/AGENTS.md there; the original stays) [y/n/a/q] y
  ✔ Unlink /home/user/.claude/CLAUDE.md (copy /home/user/Workspace/agent/AGENTS.md there; the original stays)
```

An overwritten entry adds `; the live file moves to /home/user/.bashrc.ws-backup` to its
line, and a missing copy reads `Unregister /home/user/.npmrc (workspace copy missing;
nothing to restore)`.

`--json`: `dotfile.unlink`, with `data.actions`.

Exit: 0, 1 or 3.

TUI: `ctrl+d` (selected rows), `:unlink --all`.

Was: `dotfile rm <path>` (one path), `dotfile reset` (now `--all`; it ran as one action).

#### `ws dotfile edit`

```text
ws dotfile edit <path>
```

Opens the workspace copy of one entry in your editor (§4.10; default `vim`). You name the
system path. ws opens the
entry's `workspace_path`, which for a `link` entry is the linked original, such as
`/home/user/Workspace/agent/AGENTS.md`. Because ws opens the workspace copy, you edit the
managed file even when the link is broken.

- A directory entry opens the directory itself. vim, VS Code and most editors can browse
  it. (decision)
- An overwritten entry prints a warning first: the live file is a separate file, so your
  edits reach the system only after `ws dotfile link`. To keep the live file's content,
  `ws dotfile add` it first.
- A missing workspace copy is an error.
- If you cannot write the copy (a sudo entry's file stays owned by root after the move),
  ws prints a warning. It does not run the editor with sudo.

edit runs no plan, because ws itself changes nothing. It does not commit either: the next
auto-commit or `backup push` includes the change.

`--json`: `dotfile.edit`, with `data.system` and `data.workspace_path`, printed after the
editor exits.

Exit: 0 when the editor exits 0. 1 when the path is not an entry, the workspace copy is
missing, or the editor fails.

TUI: `e` (cursor row). A terminal editor takes over the terminal, and a GUI editor starts
beside the TUI (§6.8).

Was: new.

#### `ws dotfile backup`

The backup is a git repository at `<workspace>/configs/dotfiles`. The originals under
`configs/dotfiles/` are its work tree. Each commit also includes a fresh copy of `ws.json` at
`configs/dotfiles/ws.json`, so the remote alone holds the entry list. The originals
in `secrets/dotfiles/` (and any `--to` location outside `configs/dotfiles/`) are not in this repo. They travel only with the workspace
sync.

Settings, under `dotfile.git` in `ws.json`:

| Key | Default | Meaning |
| --- | --- | --- |
| `remote_url` | none | the private remote. Backup is enabled while it is set. |
| `branch` | `main` | the branch ws commits to and pushes |
| `auto_commit` | `true` | commit after `add`, `link --to` and `unlink` |
| `auto_push` | `true` | push after each of those commits |
| `pass_entry` | none | the pass entry holding an API token for the visibility check (default `git/<host>`) |

`auto_commit` and `auto_push` control only the commits and pushes that follow `add`,
`link --to` and `unlink`. `backup push`, and the `dotfile-backup` cron job that runs
it, are explicit requests: they commit and push whatever those keys say.

**Private remotes only.** Before `backup enable` saves a remote, and before every push
(`backup push` and each auto-push), ws asks the git host's API whether the repository is
private. (decision: before, auto-push skipped the check.) It knows GitHub, GitLab and
Bitbucket, from HTTPS or SSH URLs. If the API reports the repository public, ws refuses
with `ws requires dotfile Git remotes to be private repositories`. If an authenticated lookup finds no repository, ws refuses with
`repository not found`. If visibility cannot be determined (an unknown or self-hosted host,
an unauthenticated lookup that returns 404, a network error), ws prints a warning and goes
on. No flag or setting skips the check. The API token comes from pass at run time:
`pass_entry` if set, else `git/<host>`. ws never stores it. git authenticates the push
itself, through your git credential setup. `ws credential enable` makes git read the same
pass entries.

ws never force-pushes and never rewrites history. If the remote rejects a push (it has
commits this repo lacks), the push fails and the commit stays local. When ws creates the
repo, it sets the commit identity `ws <ws@localhost>` in the repo's local git config.

##### `ws dotfile backup enable`

```text
ws dotfile backup enable [--remote <url>] [--pass-entry <entry>] [--dry-run]
```

Turns the backup on. A remote is required. (decision: there is no local-only mode.) ws
takes the URL from `--remote`, else the current `remote_url`, else the repo's existing
`origin`, else a prompt: `Remote URL of a PRIVATE repository`. If there is still no URL (a
blank answer, or `--quiet` or `--json` without `--remote`), it exits 1. Then the visibility
check runs, and a refused remote exits 1 before any change.

| Action | Planned when |
| --- | --- |
| `Initialize git in /home/user/Workspace/configs/dotfiles (branch main)` | it is not a repo yet |
| `Set the backup remote to <url>` (the repo's `origin`, `remote_url`, and `pass_entry` if given) | the URL or pass entry changes |
| `Turn on auto-commit and auto-push` | either is off |

Every action is pre-checked. If backup is already enabled with nothing to change, ws says
so and exits 0. enable does not commit or push (decision). It ends with two hints:
`ws dotfile backup push` pushes now, and `ws cron add dotfile-backup` pushes every 30
minutes.

| Flag | Default | Meaning |
| --- | --- | --- |
| `--remote <url>` | see above | the private remote |
| `--pass-entry <entry>` | `git/<host>` | the pass entry with an API token for the visibility check. ws stores the name in `pass_entry`, never the token. (decision) |

`--json`: `dotfile.backup.enable`, with `data.actions`, `data.remote_url`, and
`data.visibility` (`provider`, `private`, `checked`, `warning`, `error`).

Exit: 0, 1 or 3.

TUI: `:backup enable [--remote <url>]` hands the terminal to this command, because it may
prompt (§6.8).

Was: `dotfile git setup` (a blank remote was allowed), `dotfile git remote <url>
[--pass-entry]` (the pass entry was ignored), and `dotfile git enable|connect` (which only
printed "use setup").

##### `ws dotfile backup disable`

```text
ws dotfile backup disable [--dry-run]
```

Turns the backup off.

| Action | Planned when |
| --- | --- |
| `Remove the backup remote <url>` (clears `remote_url` and the repo's `origin`) | backup is enabled |
| `Remove the dotfile-backup cron job` | the job is installed (decision) |

Both are pre-checked. The repository and its history stay in `configs/dotfiles/`.
`auto_commit` and `auto_push` keep their values for the next `enable`. If commits are not
pushed yet, ws says how many, and they stay local. When backup is not enabled, ws says so
and exits 0.

`--json`: `dotfile.backup.disable`, with `data.actions`.

Exit: 0, 1 or 3.

TUI: `:backup disable` opens the checklist.

Was: `dotfile git disconnect`, which turned off auto-commit and auto-push and kept the
remote.

##### `ws dotfile backup status`

```text
ws dotfile backup status [-n <count>]
```

Reports the backup's state. It does not contact the remote: ahead and behind compare with
the remote branch as of the last push or fetch.

| Flag | Default | Meaning |
| --- | --- | --- |
| `-n <count>` | `20` | recent commits to show (decision: the old `git log -n`) |

Output:

```text
Backup        enabled
Repository    /home/user/Workspace/configs/dotfiles
Remote        https://github.com/user/dotfiles.git
Branch        main
Ahead/behind  ↑2 ↓0
Auto-commit   on
Auto-push     off
Last commit   2026-10-21 18:02:11 +0530  dotfile add: /home/user/.bashrc

Uncommitted
  M bashrc

Recent commits
  3f2a1c9 dotfile add: /home/user/.bashrc
  9b07e1d dotfile unlink: /home/user/.npmrc
```

When backup is off it prints `Backup  disabled` and the hint
`ws dotfile backup enable --remote <url>`. If `configs/dotfiles` is not a repo yet, it
says so.

`--json`: `dotfile.backup.status`. `data` has `enabled`, `initialized`, `repo_path`,
`remote_url`, `branch`, `ahead`, `behind`, `auto_commit`, `auto_push`, `pass_entry`,
`uncommitted` (porcelain lines), `last_commit`, and `commits` (`hash`, `subject`).

Exit: 0, whatever the state. 1 when `ws.json` cannot be read.

TUI: `:backup status` opens a scrolling view, and `esc` returns to the list.

Was: `dotfile git status`, and `dotfile git log [-n]`.

##### `ws dotfile backup push`

```text
ws dotfile backup push [-m <message>] [--dry-run]
```

Commits every pending change in `configs/dotfiles/`, with a fresh copy of `ws.json`, and
pushes the branch to the remote. It ignores `auto_commit` and `auto_push`, because you
asked for it. Backup must be enabled.

One action, `Commit pending changes and push main to <url>`, pre-checked. It copies
`ws.json` into the repo, stages everything, commits if anything changed, runs the
visibility check, and pushes with `git push -u origin <branch>`. If the push fails, the
commit stays local.

| Flag | Default | Meaning |
| --- | --- | --- |
| `-m <message>` | `ws dotfile backup push` | the commit message |

Output:

```text
$ ws dotfile backup push
Commit pending changes and push main to https://github.com/user/dotfiles.git [y/n/a/q] y
  ✔ Commit pending changes and push main to https://github.com/user/dotfiles.git
```

`--json`: `dotfile.backup.push`, with `data.actions`, `data.committed`, `data.pushed`,
`data.remote_url`, `data.branch`.

Exit: 0. 1 when backup is not enabled, the remote is refused, or the commit or push fails.

TUI: `b` opens the one-action checklist.

Cron: the `dotfile-backup` job runs `ws --workspace <workspace> dotfile backup push
--quiet` every 30 minutes. Crontab lines that still call `ws cron run dotfile-sync` run the
same job, silently.

Was: `dotfile git push [-m] [--pass-entry]`. `--pass-entry` was ignored, and the alias
still accepts it.

#### Hidden: `ws dotfile migrate`

Not part of the grammar. It splits legacy directory entries (a directory copy without
`dir: true`) into one entry per file, and it stays hidden until it is deleted. The TUI no
longer offers it.

---

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

Was: unchanged.

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

Was: unchanged.

#### `ws scratch add` (new)

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
root or the workspace root; the workspace's `ws.json`; and a dotfile original that
`ws.json` `links[]` points to (`ws dotfile unlink` it first). A symlink is moved as the
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

Was: the "move to scratch" choice of `ignore fix` (`[v]`) and the Ignore screen's `m`.

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

Was: unchanged.

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

Was: `scratch tag` with content-based suggestions and `--auto`, both gone. `--auto` is
accepted and ignored.

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

Was: unchanged. It used to delete every directory whose name contained the argument.
Now a name must resolve to one directory.

---

### `ws session`

A session is a recorded terminal session: a shell that ws starts under `script(1)`,
recording everything the terminal shows (the **transcript**) and every command line you
run in it (the **commands**). Each row is a directory under
`<workspace>/data/ws-log/<tag>/`, which syncs with the workspace. The directory holds
`meta.json` (tag, start, end, duration), `stdout.log` (the transcript) and `stdin.log`
(the commands, one per line). The directory name stays `ws-log`. Only the noun changed,
because "log" also meant cron output and git history. The Sessions screen shows the rows.

Settings live under `session` in `ws.json`: `cap_mb` (default 500) is the total size the
`prune` default keeps the sessions under, and `max_session_mb` (default 100) ends a
recording that grows past it. ws still reads the old `log` key, and writes `session`.

Only one recording runs at a time on a machine. ws records it in a machine-local marker,
`$XDG_STATE_HOME/ws-tool/session-active.json`, with its tag, start time and recorder PID
(decision: the marker used to be `data/ws-log/active.json`, which synced to other
machines). The `active` field is true only for the recording running on this machine.

| Command | RO/RW | Does | TUI |
| --- | --- | --- | --- |
| `ws session ls` | RO | list recorded sessions | screen |
| `ws session show <tag> [--commands]` | RO | print a transcript, or only its commands | `enter` (`c` toggles commands) |
| `ws session record [tag]` | RW | start a recorded shell; leaving it ends the recording | `r` |
| `ws session stop` | RW | end the recording running on this machine | `:stop` |
| `ws session grep <text>` | RO | search transcripts and commands | `:grep` |
| `ws session rm <tag…>` | RW | soft-delete sessions to the trash | `ctrl+d` |
| `ws session prune [--older-than <age>] [--all]` | RW | delete old sessions; by default, enforce `session.cap_mb` | `p` |

#### `ws session ls`

```text
ws session ls [--filter <expr>] [--sort <fields>] [--wide] [--json]
```

Lists every session directory that has a `meta.json`, newest first.

| Field | Column | Meaning |
| --- | --- | --- |
| `tag` | Tag | session name (the directory name) |
| `started_at` | Started | start time |
| `ended_at` | Ended (`--wide`) | end time; empty while recording |
| `duration_sec` | Duration | length; `—` while recording |
| `commands` | Cmds | lines in `stdin.log` |
| `size_bytes` | Size | `stdin.log` plus `stdout.log` |
| `active` | State | `recording` (on this machine) or `done` |

```text
$ ws session ls
Tag            Started           Duration  Cmds  Size     State
deploy-fix     2026-10-22 14:05  —         12    40.0 KB  recording
rsync-backup   2026-10-20 09:30  31m 05s   7     8.0 KB   done
```

JSON: `session.ls`, one record per session with the fields above.

Exit: 0, or 1 for an unknown field.

TUI: the Sessions screen. The recording row's state is drawn as `● recording`.

Was: `log ls`.

#### `ws session show` (new)

```text
ws session show <tag> [--commands]
```

Prints a session's transcript (`stdout.log`) with terminal escape sequences and carriage
returns removed, so it reads as plain text in a pager or a file. With `--commands`, it
prints the commands file (`stdin.log`) instead: one command line per line, in the order
they ran. A session that is still recording shows what has been written so far. `<tag>`
must be an exact tag.

| Flag | Default | Meaning |
| --- | --- | --- |
| `--commands` | off | print only the commands |

```text
$ ws session show rsync-backup --commands
rsync -avz --delete ~/Workspace/ backup:/data/
df -h /data
```

JSON: `session.show`, with `tag`, `path` (the file printed), `commands` (bool) and `text`.

Exit: 0, or 1 if the tag does not exist.

TUI: `enter` opens the transcript in a scrolling view. `c` toggles commands only, and `esc`
returns to the list.

Was: new in the CLI. The TUI viewer existed. Its full view used to show the commands file
followed by the transcript. It now shows only the transcript, which already contains
every command as it was echoed.

#### `ws session record`

```text
ws session record [tag] [--no-prompt] [--quiet] [--dry-run]
```

Starts a recorded shell and blocks until it ends. Like `stop`, it runs without a plan
(§4.7). `tag` names the session. The default is the local time, `YYYY-MM-DD-HHMM`. A tag
must not contain `/` or start with `.`. If the tag already exists, ws appends `-2`,
`-3`, … instead of overwriting it (decision). The tag is printed.

ws refuses to start (exit 1, nothing created) when stdin or stdout is not a terminal,
when `script(1)` from util-linux ≥ 2.35 is missing (the error names the package), or when
a recording is already running on this machine. A marker whose PID is no longer running
is stale. ws finalizes that session and starts the new one (decision).

**The recorded shell.** ws creates the session directory and runs
`script -q --flush -e -O <dir>/stdout.log -c <shell>`. The shell is `$SHELL` when it is
bash or zsh. Any other shell gets bash. The shell first loads your normal rc file
(`~/.bashrc`, or `~/.zshrc`), then two hooks that run before each prompt:

- **Command capture.** The last history entry is appended to `stdin.log` when it is new.
  Commands typed inside programs the shell runs (`ssh`, `python`, `psql`) appear only in
  the transcript. ws does not log raw keyboard input, so passwords typed at prompts that
  do not echo are never recorded (decision).
- **Prompt indicator.** `● ws:rec ` (a red dot) is prepended to `PS1` (bash, through
  `PROMPT_COMMAND`) or `PROMPT` (zsh, through `precmd`). Your own prompt is kept, and the
  hook re-adds the indicator if your prompt code rewrites `PS1`. With colour off, the
  indicator is `(*) ws:rec `. `--no-prompt` leaves the prompt alone and still captures
  commands. Nothing is written to your rc files. The indicator ends with the shell.

The shell's environment has `WS_LOG_RECORDING=1`, for prompt themes that want their own
cue. The name is kept so that existing prompt configurations keep working.

**Ending.** The recording ends when the shell exits (`exit`, Ctrl-D), when
`ws session stop` runs, or when `stdin.log` and `stdout.log` together reach
`session.max_session_mb` (checked every 5 seconds; ws sends the recorder SIGTERM). ws
then writes the end time and duration into `meta.json`, removes the marker, and prints a
summary.

| Flag | Default | Meaning |
| --- | --- | --- |
| `--no-prompt` | off | do not add the indicator |
| `--quiet` | off | no banner and no summary (errors still print) |

```text
$ ws session record deploy-fix

✔ Recording deploy-fix
  Transcript  /home/user/Workspace/data/ws-log/deploy-fix/stdout.log
  Commands    /home/user/Workspace/data/ws-log/deploy-fix/stdin.log
  End         type exit or press Ctrl-D

● ws:rec user@laptop:~/Workspace$ kubectl rollout status deploy/api
…
● ws:rec user@laptop:~/Workspace$ exit

✔ Session ended
  Tag         deploy-fix
  Duration    14m 22s
  Transcript  /home/user/Workspace/data/ws-log/deploy-fix/stdout.log  (19.0 KB)
  Commands    /home/user/Workspace/data/ws-log/deploy-fix/stdin.log  (12 commands)
```

`--dry-run` prints the session directory it would create and starts nothing.

JSON: `session.record` prints its envelope after the shell ends, in place of the summary.
It has `tag`, `session_dir`, `stdin_path`, `stdout_path` and `duration_sec`.

Exit: 0 once a recording has ended, whatever the shell's own exit status. 1 if it could
not start.

TUI: `r` asks for an optional tag (blank for the timestamp), then hands the terminal to
`ws session record <tag> --quiet` (§6.8). The screen reloads when the shell exits. If a
recording is already running, `r` shows a notice instead.

Was: `log start [--tag <tag>]`. `ws log start --tag <tag>` still runs
`session record <tag>`, and `--quiet-start` still works and means `--quiet`. The
indicator was `● ws:log`, or `(rec)` without colour.

#### `ws session stop`

```text
ws session stop [--dry-run]
```

Ends the recording running on this machine, from any terminal: ws sends the recorder
SIGTERM, waits up to 3 seconds for it to exit, then finalizes `meta.json` and removes the
marker. It runs without a plan and without a prompt (§4.7). Run inside the recorded shell,
it ends that shell. With no recording running, it prints `No active recording session.`
and exits 0.

A session still marked active at the old synced location, `data/ws-log/active.json`, is
finalized without signalling any process, and the file is removed (decision).

```text
$ ws session stop
✔ Session ended
  Tag         deploy-fix
  Duration    14m 22s
  Transcript  /home/user/Workspace/data/ws-log/deploy-fix/stdout.log  (19.0 KB)
  Commands    /home/user/Workspace/data/ws-log/deploy-fix/stdin.log  (12 commands)
```

JSON: `session.stop`, with `tag`, `duration_sec` and `stopped`.

Exit: 0 (including when nothing was recording), or 1 if the metadata could not be
written.

TUI: `:stop` opens a one-action checklist (the TUI confirms every change, §6.4).

Was: `log stop`.

#### `ws session grep` (new)

```text
ws session grep <text> [--max <n>]
```

Searches the transcript and the commands of every session. `<text>` is a literal,
case-insensitive substring, and several arguments are joined with single spaces. Escape
sequences and carriage returns are removed before matching, as in `show`. Only
`stdout.log` and `stdin.log` are searched (decision). Sessions are searched newest first,
the commands file before the transcript.

| Flag | Default | Meaning |
| --- | --- | --- |
| `--max <n>` | 0 | stop after n matches (0: no limit) |

```text
$ ws session grep rsync
/home/user/Workspace/data/ws-log/rsync-backup/stdin.log:1: rsync -avz --delete ~/Workspace/ backup:/data/
/home/user/Workspace/data/ws-log/rsync-backup/stdout.log:3: rsync -avz --delete ~/Workspace/ backup:/data/
```

JSON: `session.grep`, a list of matches with `tag`, `path`, `line` and `text`.

Exit: 0, whether or not anything matched (`No matches.`). 1 if `<text>` is missing.

TUI: `:grep <text>` narrows the list to the sessions that have a match, and the status
line shows `grep <text>`. `enter` opens a session's viewer at its first matching
line (in commands-only mode when the match is in the commands file). `esc` clears the
narrowing.

Was: new in the CLI. The TUI search (`F`) existed.

#### `ws session rm`

```text
ws session rm <tag…> [--dry-run]
```

Moves sessions to the XDG trash (§7.3). Tags must match exactly. If a tag does not exist
or names the recording in progress, nothing is deleted (exit 1). The plan has one action
per session, pre-checked: `Delete /home/user/Workspace/data/ws-log/rsync-backup (8.0 KB)`.

JSON: `session.rm`, with `actions`.

Exit: 0, 1, or 3.

TUI: `ctrl+d` on the selected rows. The recording row is left out of the checklist, with
a notice.

Was: `log rm`. It used to delete permanently.

#### `ws session prune`

```text
ws session prune [--older-than <age> | --all] [--dry-run]
```

Deletes finished sessions to the XDG trash, oldest first. The recording in progress is
never pruned. Which sessions are pruned:

- **No flags:** enforce the cap. If all sessions together (the recording included) take
  more than `session.cap_mb`, the oldest finished sessions are pruned until the total is
  at or under the cap. With `cap_mb` 0 or less, there is no cap and nothing is pruned.
- **`--older-than <age>`:** sessions that started longer ago than `<age>`. Ages are
  `30d`, `12h` or `2w`, and a bare integer means days (the shared age format). The cap is
  not applied.
- **`--all`:** every finished session. Using it together with `--older-than` is an error
  (exit 1) (decision).

The plan has one action per session, pre-checked:
`Delete /home/user/Workspace/data/ws-log/2026-08-02-0930 (89.0 MB, 81d old)`. With
nothing to prune, ws says why (`Nothing to prune: 312 MB of the 500 MB cap.`) and exits 0.
The built-in cron job `session-prune` runs `ws session prune --quiet` nightly at 02:00.

| Flag | Default | Meaning |
| --- | --- | --- |
| `--older-than <age>` | — | prune by age instead of by the cap |
| `--all` | off | prune every finished session |

JSON: `session.prune`, with `actions`, `storage_bytes` (before pruning) and `cap_mb`.

Exit: 0, 1, or 3.

TUI: `p` asks for an age (blank enforces the cap), then shows the checklist of the sessions
it would prune. It covers every session, like the CLI, whatever is marked or filtered.

Was: `log prune`. It used to remove nothing without `--older-than` or `--all`, so the
nightly `log-prune` job did nothing. It used to delete permanently.

---

### `ws ignore`

`ignore` decides what MEGA syncs. The rules are a built-in template plus your own rules in
`ws.json` (`ignore_rules`), and ws generates `<workspace>/.megaignore` from them. The rows
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
| `ws ignore unmute <path…>` | RW | show its warnings again | `u` (muted view) |
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
| 1 | built-in excludes: OS junk, dot-files, build directories, compiled output, logs, lock files, archives, datasets | the ws binary, minus `ignore_rules.suppress_defaults` | `-:.*`, `-:node_modules`, `-g:*.log`, `-g:*.csv` |
| 2 | built-in always-sync directories: the layout blocks | the ws binary, minus `ignore_rules.suppress_harbors` | `+g:artifacts/**`, `configs/**`, `secrets/**`, `data/**`, `archive/**` |
| 3 | your always-sync directories | `ignore_rules.safe_harbors` (made by the old suppress; edit with `ignore edit`) | `+g:experiments/ml/**` |
| 4 | ignored paths | `ignore_rules.exclude` (`ignore add`) | `-:experiments/run/dump.bin` |
| 5 | never sync symlinks | fixed | `-s:*` |

- **Always-sync directories** sync everything below them, including what rule 1 excludes
  (`.git`, `node_modules`, `*.log`). The layout blocks are built in. Your own come from the
  old suppress and are edited with `ws ignore edit`. No command creates new ones.
- **Muting changes no rule.** `ignore mute` only hides warnings (see below).
- **Your ignored paths come last, so they always win** (decision): `ignore add` works inside
  a muted directory and inside the layout blocks (`configs/`, `secrets/`, …).
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
| `bloat` | WARNING | a file is larger than `ignore.warn_size_mb` (default 1) |
| `depth` | WARNING | a file or directory is more than `ignore.max_depth` (default 6) levels below the workspace |
| `project-meta` | WARNING | build output next to a project marker: `bin/` beside `go.mod`; `dist/`, `build/`, `node_modules/` beside `package.json` |

A warning is **muted** when its path is muted with `ignore mute` (itself or a parent), or
lies under an always-sync directory (any path an always-sync rule is the reason it syncs, as
`internal/ignore/engine.go` decides since 0d08e4e). A muted warning has severity INFO and
`muted` true, and appears only in `ignore ls --muted`. This includes `project-meta`
(decision).

#### `ws ignore ls`

```text
ws ignore ls [--muted] [--filter <expr>] [--sort <fields>] [--wide] [--json]
```

Lists every path that doesn't sync, with the rule that keeps it out, one file per line, so
it pipes into `grep`. Files inside an ignored directory are listed too. This answers "what
is MEGA leaving out?"

With `--muted`, it lists the muted warnings instead: everything you muted with
`ignore mute`, plus the warnings under always-sync directories. `ignore ls` thus shows both
kinds of decision you made: what doesn't sync, and what you chose not to hear about.

| Flag | Default | Meaning |
| --- | --- | --- |
| `--muted` | off | the muted warnings instead of the ignored paths |
| `--filter`, `--sort`, `--wide` | — | as in §4.3 |

Ignored-path fields: `path`, `size_bytes`, `rule`, `rule_source` (`builtin` or `user`).
Warning fields (with `--muted`): see `ignore lint`.

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

Was: `ignore ls` (same meaning, a flat list). `--muted` replaces `ignore scan
--expand-suppressed`. The old flags `--path`, `--rule` and `--tree` still work, hidden.

#### `ws ignore lint`

```text
ws ignore lint [path…] [--filter <expr>] [--sort <fields>] [--wide] [--json]
```

Checks the files that sync and warns about the ones that aren't hygienic: too big, too deep,
or build output (the types in **Warnings**). Muted warnings are left out. When there are
some, it prints their count on stderr: `47 muted warnings (2.3 GB): ws ignore ls --muted`.
With paths, it checks only under them. It changes nothing and exits 0, warnings or not.

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

Was: `ignore scan` (hidden alias).

#### `ws ignore add`

```text
ws ignore add <path…> [--dry-run]
```

Stops syncing paths. Each path resolves as in §4.5, must exist, and must be inside the
workspace (not the workspace itself). ws stores it workspace-relative in `ws.json`
`ignore_rules.exclude` (`{"pattern": "experiments/run/dump.bin"}`), and `.megaignore` then
has `-:experiments/run/dump.bin`. An ignored directory is ignored with everything in it. ws refuses a dotfile original (a
`links[]` workspace copy), because it must sync for `ws dotfile link` to work on your other
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

Was: part of `ignore fix`, and `secret fix --secret-mode exclude`.

#### `ws ignore rm`

```text
ws ignore rm <path…> [--dry-run]
```

(new) Syncs paths again by removing their entries from `ignore_rules.exclude`. It needs at
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

Was: new.

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
parent, or an always-sync directory. At least one path is required.

Plan: one action per path, pre-checked: `Mute /home/user/Workspace/experiments/ml (it keeps
syncing)`.

JSON: `ignore.mute`, `data.actions`.

Exit: 0, 1, 3.

TUI: `m` in the Warnings view mutes the selected rows' paths. Their rows move to the Muted
view.

Was: "suppress" (the TUI's `s`, and the `[s]` choice of `ignore fix`). Suppress also made
the directory sync everything below it. Mute does not (decision, by the user). Directories
suppressed before keep both effects with no migration: their always-sync rule stays in
`ignore_rules.safe_harbors`, and warnings under an always-sync directory are muted anyway.
To hear about one again, remove its rule with `ws ignore edit`.

#### `ws ignore unmute`

```text
ws ignore unmute <path…> [--dry-run]
```

Removes paths from `ignore.muted`, and their warnings return to `ignore lint`. Syncing is not
touched.

- **Muted by a parent entry:** ws names the parent and changes nothing. Unmute the parent.
- **Under an always-sync directory** (`artifacts`, `configs`, `secrets`, `data`, `archive`,
  or your own from the old suppress): its warnings stay muted, because that directory
  syncs everything by design, and ws says so. To change it, edit `safe_harbors` or
  `suppress_harbors` with `ws ignore edit`. (decision)
- **Not muted:** nothing to do.

Plan: one action per path, pre-checked: `Unmute /home/user/Workspace/experiments/ml`.

JSON: `ignore.unmute`, `data.actions`.

Exit: 0, 1, 3.

TUI: `u` in the Muted view unmutes the entry that mutes each selected row. Rows muted by an
always-sync directory are skipped, and the notice says so.

Was: the TUI's "remove suppress rule", which also stopped the directory syncing everything.

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
  Reason: always-sync directory `+g:archive/**` (built-in)
  Size: 230.0 MB
```

The reason is one of: an ignore rule (`built-in` or `ws.json`), an always-sync directory
(`built-in` or `ws.json`), or `no rule matches`. Muting never changes the answer.

JSON: `ignore.check`. Keys: `path`, `included`, `rule` (`<default>` when no rule matches),
`rule_source` (`builtin`, `user`, or empty), `muted`, and `size_bytes` for files.

Exit: 0 whether the path syncs or not; 1 when the path is missing or outside the workspace.

TUI: `:check <path>` answers in a notice.

Was: same name. It used to exit 2 for an ignored path. The old `ignore ls <path>` was the
same command.

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

Was: same name, and `ignore ls --tree`. `--depth n`, `--path <dir>` and a second positional
level still work, hidden.

#### `ws ignore edit`

```text
ws ignore edit [--editor <cmd>]
```

Writes `ignore_rules` to `<workspace>/.ignore-edit.json` and opens it in `--editor`, else
your editor (§4.10). It holds five keys:

| Key | Meaning |
| --- | --- |
| `exclude` | ignored paths (`ignore add`), `{"pattern", "note"}` |
| `safe_harbors` | your always-sync directories (from the old suppress), `{"pattern", "note"}` |
| `suppress_defaults` | built-in exclude patterns to turn off, such as `"*.csv"` (edit only) |
| `suppress_harbors` | built-in always-sync directories to turn off, such as `"data/**"` (edit only) |
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

Was: same name.

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

Lists the findings that are not allowed. It never writes `ws.json` (decision: it used to prune
stale anchors as a side effect).

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

JSON: `secret.ls`. `data` is the array of records (decision: the old `data.violations`).
The old extra keys moved: `pass` to `pass status`, `pass_audit` to `pass audit`,
`git_credential_helper` to `credential status`. `skipped_dirs` is `secret.skip_dirs` in
`ws config show`.

Exit: 0 with or without findings; 1 on bad flags, an unknown field, or a `--skip-dir`
outside the workspace.

TUI: the screen.

Was: `secret scan`. Its `--pass` is now `ws pass audit`.

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

Was: new as a command.

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

Was: `secret fix --secret-mode allowlist`.

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

Was: the skip-dir choice of `secret fix`.

#### `ws secret unmute`

```text
ws secret unmute <dir…> [--dry-run]
```

(new) Removes directories from `secret.skip_dirs`, however the entry is written there
(relative, absolute or `~`). If the directory is muted by a parent entry, ws names the parent
and changes nothing. A directory that is not muted means nothing to do.

Plan: one action per directory, pre-checked: `Unmute /home/user/Workspace/references/vendor in secret scans`.

JSON: `secret.unmute`, `data.actions`.

Exit: 0, 1, 3.

TUI: `:unmute <dir>`.

Was: new.

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

Was: `secret fix --secret-mode pass`, which did not allow the finding afterwards.

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

Was: `secret setup`. Its `--git-remote <url>` still works, hidden, and adds the
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
  Tip: ws dotfile add ~/.password-store --dir --to ~/Workspace/secrets/dotfiles/password-store
```

JSON: `pass.status`. Keys: `gpg_available`, `pass_installed`, `initialized`, `store_path`,
`entry_count`, `git_backed`, `git_remote`, `remote_url`, `cred_helper`, `cred_helper_value`.

Exit: 0. A missing piece is reported, not an error.

TUI: the Secrets header shows it on one line (`✔ pass  ✔ store (42 entries)  ✔ git  ▲ no
remote  ✔ credential helper`). `:pass status` opens the full report as a view.

Was: `secret status`, and the pass and credential sections of `secret scan`.

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

Was: `secret scan --pass`.

#### `ws pass backup`

The pass store is a git repository (`pass git …`). `ws pass backup` pushes it to a private
remote, and works exactly like `ws dotfile backup`: the same four verbs, flags, output
layout, private-remote rule (§7.4) and cron job. Only the repository differs. pass commits
every change itself, so there is no auto-commit setting. Pushes happen through
`backup push`, or the `pass-backup` cron job every 30 minutes.

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
and exits 0. enable does not push. It ends with the same two hints as dotfile backup:
`ws pass backup push` pushes now, and `ws cron add pass-backup` pushes every 30 minutes.

| Flag | Default | Meaning |
| --- | --- | --- |
| `--remote <url>` | see above | the private remote |
| `--pass-entry <entry>` | `git/<host>` | the pass entry with an API token for the visibility check |

JSON: `pass.backup.enable`, with `data.actions`, `data.remote_url`, and `data.visibility`.

Exit: 0, 1, 3.

TUI: `:pass backup enable [--remote <url>]` hands over the terminal (it may prompt).

Was: `secret git remote <url>`.

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

Was: new.

##### `ws pass backup status`

```text
ws pass backup status [-n <count>]
```

Reports the backup's state, in the same layout as `ws dotfile backup status`. It does not
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
`commits` (`hash`, `subject`), the same keys as `dotfile.backup.status` plus `entry_count`.

Exit: 0, whatever the state.

TUI: `:pass backup status` opens a scrolling view; `esc` returns.

Was: `secret git status` and `secret git log [-n]`.

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

Cron: the `pass-backup` job (new) runs `ws pass backup push --quiet` every 30 minutes. It is
part of the `sync` preset.

Was: `secret git push`.

#### A second copy in the workspace

The pass store syncs only through its git remote. Keeping it inside the workspace adds a
second copy, which MEGA syncs with everything else:

```text
ws dotfile add ~/.password-store --dir --to ~/Workspace/secrets/dotfiles/password-store
```

The store and its git history move into `secrets/dotfiles/password-store`, an always-sync
directory, and `~/.password-store` becomes a symlink to it, so `pass` works as before.

ws suggests this gently, and only where you'd look anyway:

- `pass status` ends with an info line, `ℹ workspace copy: none`, and the command above.
  When the store is already a dotfile link into the workspace, the line reads
  `✔ workspace copy  /home/user/Workspace/secrets/dotfiles/password-store`.
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

Was: `git-credential-helper setup`.

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
with exit 0 (decision; it used to exit 1).

JSON: `credential.disable`, `data.actions`.

Exit: 0, 1, 3.

TUI: `:credential disable` hands over the terminal.

Was: `git-credential-helper disconnect`.

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

Was: `git-credential-helper status`.

#### Plumbing: `ws git-credential-helper get|store|erase`

These are what git runs. They are hidden and unchanged, never print anything except the
answer, and need no workspace. `get` reads git's request (`protocol=`, `host=`, `path=`) on
stdin and prints `username=` and `password=` by the lookup above. `store` and `erase` read
stdin and do nothing: entries are managed with `ws secret store`, `ws credential enable` or
`pass insert`. Any other operation exits 0 silently. `ws credential get|store|erase` does the
same, for git configs written by older versions (decision).

---

### `ws cron`

Scheduled ws jobs. A row is a built-in job or a preset. Its state comes from three places:
the live user crontab (`crontab -l`), the declared crontab `<workspace>/configs/crontab`
(synced with the workspace), and the run history in `$XDG_STATE_HOME/ws-tool/cron.state`
(default `~/.local/state/ws-tool/`). The Cron screen shows the same rows.

| Command | RO/RW | Does | TUI |
| --- | --- | --- | --- |
| `ws cron ls` | RO | jobs and presets: schedule, state, last run, exit, next run | screen |
| `ws cron add <job\|preset…>` | RW | install in the live crontab and declare in `configs/crontab` | `a` |
| `ws cron rm <job\|preset…>` | RW | remove from the live crontab and from `configs/crontab` | `ctrl+d` |
| `ws cron run <job>` | RW | run the job now and record the result | `r` |
| `ws cron log [job…] [--lines n]` | RO | recent lines of the cron log | enter |
| `ws cron apply` | RW | make the live crontab match `configs/crontab` | `:apply` |

**Jobs.** Each job runs one command in the workspace. `<ws>` is the absolute path of the ws
binary and `<W>` the absolute workspace path.

| Job | Schedule | Interval | Runs |
| --- | --- | --- | --- |
| `mega-sync` | `*/30 * * * *` | 30m | restarts `megasync` for a 5-minute sync window, using `DISPLAY` |
| `dotfile-backup` | `*/30 * * * *` | 30m | `<ws> --workspace <W> dotfile backup push --quiet` |
| `pass-backup` | `*/30 * * * *` | 30m | `<ws> --workspace <W> pass backup push --quiet` (new) |
| `session-prune` | `0 2 * * *` | 24h | `<ws> --workspace <W> session prune --quiet` |

**Presets.** `sync` = `mega-sync`, `dotfile-backup`, `pass-backup`. `maintenance` = `session-prune`
(decision: the preset stays, with the one job it has left).

**Old and retired names.** `dotfile-sync` is now `dotfile-backup` and `log-prune` is now
`session-prune`. Old names are hidden aliases everywhere a job name is accepted. `cron run` takes
them silently, because crontab lines call it. `add`, `rm` and `log` print the stderr line. A crontab block,
state record or log line under an old name counts as the new job's (decision), so history and
missed-run checks carry over. `secret-scan`, `ignore-scan` and `repo-sync` are retired (`repo-sync` because nothing should commit your work unattended; use `ws repo save`): `cron add` rejects
them, `cron rm` still removes their blocks, and `cron run` skips them (see below).

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

Lists every built-in job, then every preset, then every retired job that still has a block in
either crontab, each group sorted by name. It reads the live crontab, `configs/crontab` and
`cron.state`, and changes nothing. It replaces `cron status`.

| Field | Meaning |
| --- | --- |
| `name` | job or preset name |
| `schedule` | cron expression; text `preset` for presets |
| `state` | see below |
| `installed` | a block for the job is in the live crontab (bool) |
| `declared` | a block for the job is in `configs/crontab` (bool) |
| `last_run` | time of the last recorded run (RFC 3339; text "12m ago") |
| `last_exit` | exit code of the last run |
| `next_run` | last run + interval, for installed jobs only; text "in 18m", or `overdue` when past |
| `description` | one line; presets: `installs: a, b` |
| `preset` | bool |
| `members` | jobs a preset installs |

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

A preset shows `installed`, `not installed`, or `2 of 3 installed`. Default columns: `name`,
`schedule`, `state`, `last_run`, `next_run`, `description` (decision: `next_run` added).

```text
$ ws cron ls
 Job             Schedule      State          Last run          Next run  Description
 dotfile-backup  */30 * * * *  outdated       12m ago (exit 1)  in 18m    Commit and push the dotfile backup
 mega-sync       */30 * * * *  installed      12m ago           in 18m    Restart megasync for a 5-minute sync window
 pass-backup     */30 * * * *  not installed                              Push the pass store backup
 session-prune   0 2 * * *     not installed                              Prune sessions over the size cap
 maintenance     preset        not installed                              installs: session-prune
 sync            preset        2 of 3 installed                           installs: mega-sync, dotfile-backup, pass-backup
```

`--json`: command `cron.ls`. `data` is the array of records with the field keys above (decision: no
`jobs` wrapper, the same shape as every other `ls`).

Exit: 0. 1 only for bad flags. An unreadable crontab is not an error: rows show `crontab n/a`
and stderr gets one line with the reason.

TUI: the Cron screen list. Its header reads "2 jobs installed · 1 failing". `d` shows every field.

Was: `cron list`, `cron status [job]` (hidden aliases; `cron status <job>` is
`cron ls --filter name=<job>`).

#### `ws cron add`

```text
ws cron add <job|preset…> [--display <d>] [--dry-run]
```

Installs each named job: it writes the job's block into the live crontab and into
`configs/crontab`, creating that file if missing (decision: today `add` writes only the live
crontab). Presets expand to their members, and each job is handled once. It needs at least one
target. An unknown or retired name exits 1 before any change.

Plan: one action per job that needs a change. Each action covers both files (decision), so the
two cannot drift apart. All actions are pre-checked. A job that is already `installed` gets no
action and is reported as "already installed". An `outdated`, `undeclared` or `not applied` job is
written fresh, and a block under its old name is removed in both files. Lines outside ws blocks
are untouched.

| Flag | Default | Meaning |
| --- | --- | --- |
| `--display <d>` | `$DISPLAY`, else `:1` | X display written into the `mega-sync` lines; ignored for other jobs |
| `--dry-run` | false | print the plan; change nothing |

```text
$ ws cron add sync
  Install mega-sync (*/30 * * * * + @reboot) in the crontab and /home/user/Workspace/configs/crontab  [y/n/a/q] a
  ✔ Install mega-sync (*/30 * * * * + @reboot) in the crontab and /home/user/Workspace/configs/crontab
  ✔ Install dotfile-backup (*/30 * * * * + @reboot), replacing dotfile-sync, in the crontab and /home/user/Workspace/configs/crontab
  ✔ Install pass-backup (*/30 * * * * + @reboot) in the crontab and /home/user/Workspace/configs/crontab
✔ 3 cron jobs installed.
```

`--json`: command `cron.add`; data `jobs` (`name`, `schedule`), `actions`.

Exit: 0, 1, 3.

TUI: `a` acts on the marked rows, else the cursor row, and opens the same checklist after the preflight.

Was: `cron add` (one target only). Old job names work with the stderr line.

#### `ws cron rm`

```text
ws cron rm <job|preset…> [--dry-run]
```

Removes each named job's block, under its current and old names, from the live crontab and from
`configs/crontab`. It also deletes the job's legacy wrapper script
`$XDG_STATE_HOME/ws-tool/cron-jobs/<name>.sh` if one is there. It accepts retired names. Presets
expand to their members. It needs at least one target. Run history and log lines are kept.

Plan: one action per job that has a block in either file, pre-checked. A job with no block is
reported as "not installed" and gets no action.

| Flag | Default | Meaning |
| --- | --- | --- |
| `--dry-run` | false | print the plan; change nothing |

`--json`: command `cron.rm`; data `removed` (names), `actions`.

Exit: 0, 1, 3.

TUI: `ctrl+d` acts on the marked rows, else the cursor row, and opens the checklist.

Was: `cron remove` (hidden alias).

#### `ws cron run`

```text
ws cron run <job> [--if-due] [--display <d>] [--dry-run]
```

Runs one job now in the workspace and records the result. Crontab lines call it, so it never
prompts. The command is the confirmation.

1. With `--if-due` (new), it skips when the job's last recorded run, under any of its names, is
   newer than half its interval. It logs `Skipping: last run 4m ago (< half-interval 15m)` and exits 0.
   Without `--if-due`, the job always runs (decision: `run` means execute now).
2. It logs `Starting` and runs the job's command with bash. Each line of the job's stdout and
   stderr goes to `cron.log` as `<time> [<job>] <line>` (decision: today the output is lost), and
   also to ws's own stdout and stderr unless `--json` is given.
3. It appends a record to `cron.state` and logs `Done (exit 0)` or `Failed (exit 1)`.

Before writing, if `cron.log` is over 5 MB, ws keeps its last 2.5 MB. An old job name runs
silently as the new job. A retired name logs
`Skipping: secret-scan is no longer a ws job; run ws cron rm secret-scan` and exits 0, so old
crontab lines do not fail (decision). Presets cannot be run.

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
row refreshes. On a preset row, a notice says to run a member job.

Was: `cron run` (always applied the half-interval skip).

#### `ws cron log`

```text
ws cron log [job…] [--lines <n>]
```

Prints the last `n` lines of `cron.log` for the named jobs, including lines under their old names,
or for all jobs when none is named (decision: the job stays optional, as today and per rule 4).
A preset stands for its members. It reads only `cron.log`.

| Flag | Default | Meaning |
| --- | --- | --- |
| `--lines <n>` | 20 | lines to show; 0 shows all |

```text
$ ws cron log dotfile-backup --lines 3
2026-10-22T07:00:01Z [dotfile-backup] Starting
2026-10-22T07:00:02Z [dotfile-backup] error: failed to push some refs to 'origin'
2026-10-22T07:00:02Z [dotfile-backup] Failed (exit 1)
```

`--json`: command `cron.log`; data `jobs` (names, empty for all), `lines`.

Exit: 0, including for an empty log ("No cron log entries."). 1 for an unknown job.

TUI: enter on a job row opens its log: the last 200 lines, scrolled to the end. esc goes back. On a
preset row, a notice says to open a member job.

Was: `cron log [job]`.

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

Was: `cron apply` (it copied blocks verbatim, never prompted, and re-appended non-ws lines on every run).

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

Was: `trash ls`; `--root-dir` is removed.

#### `ws trash restore`

```text
ws trash restore <item…> [--dry-run]
```

(new) Puts each item back at the original path in its `.trashinfo`. It needs at least one target.
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

Was: new.

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

Was: `trash rm <name>` (one target, names only).

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

Was: `trash empty`.

#### `ws trash prune`

```text
ws trash prune --older-than <age> [--dry-run]
```

Deletes forever every item whose `DeletionDate` is older than `<age>`. The age is measured from
the `.trashinfo`, not the file's mtime. Items without a `DeletionDate` are never pruned, and the
output reports how many were skipped. Trash has no size cap in ws.json, so `--older-than` is
required.

Plan: one action per matched item, oldest first, pre-checked (decision: today the CLI used one
action for all).

| Flag | Default | Meaning |
| --- | --- | --- |
| `--older-than <age>` | required | `30d`, `12h`, `2w`; a bare number means days; must be positive |
| `--dry-run` | false | print the plan; change nothing |

`--json`: command `trash.prune`; data `matched` (`name`, `size_bytes`, `deleted_at`), `kept`,
`skipped`, `size_bytes`, `actions`.

Exit: 0, 1, 3. A missing or invalid `--older-than` exits 1.

TUI: `p` prompts for the age in the same format, then opens a checklist of the matched items.
Like the CLI, it covers the whole trash, whatever is marked or filtered.

Was: `trash purge --older-than <days>` (whole days only; hidden alias).

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

Was: `trash setup` (hidden alias); `--root-dir` is removed.

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

Was: `trash disable` (JSON command was `trash.reset`).

#### `ws trash status`

```text
ws trash status [--json]
```

Reports each integration from the live system, not from a state file (decision), along with the
trash directory and its totals. The size warning is fixed at 1 GB.

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

Was: `trash status` (exited 2 when an integration was off or the trash was over the limit).

### Workspace commands

Commands about the workspace itself and the ws binary. There is no TUI screen of their own. The
command bar covers `config` (`:config show`, `:config edit`), and the setup screen covers `init`.

| Command | RO/RW | Does | TUI |
| --- | --- | --- | --- |
| `ws init` | RW | create the workspace layout, `ws.json` and `.megaignore` | setup screen `enter` |
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
| `-c, --config <file>` | `ws.json` (default `$WS_CONFIG`, else `<workspace>/ws.json`) |
| `-q, --quiet` | errors only; accepts plans without prompting |
| `--verbose` | show internal decisions on stderr |
| `--json` | the JSON envelope; accepts plans without prompting |
| `--dry-run` | print plans; change nothing |
| `--no-color` | no color or unicode (also `NO_COLOR`) |
| `-h, --help` | help for the command |

`--manifest` is removed (decision: a leftover that did nothing).

#### `ws init`

```text
ws init [--workspace <dir>] [--config <file>] [--dry-run]
```

Makes a directory a ws workspace. The workspace is `--workspace`, else `$WS_WORKSPACE`, else
`~/Workspace`. `ws.json` is written where every other command looks for it: `--config`, else
`$WS_CONFIG`, else `<workspace>/ws.json` (decision: today init always writes
`<workspace>/ws.json`). It creates, when missing:

- the layout: `notes/ experiments/ artifacts/ references/ scripts/ scripts/lib/ agent/ agent/skills/
  configs/dotfiles/ secrets/ secrets/dotfiles/ secrets/pass/ data/ data/ws-log/ archive/`
- `ws.json`, complete: every key, at its default or empty value (`ws config show --defaults`),
  so the file documents every setting (§2.3)
- `<workspace>/.megaignore`, generated from `ws.json`'s ignore rules

It is idempotent (decision). On an existing workspace, it creates only what is missing. An
existing `ws.json` is never replaced. If it lacks keys (written by an older ws), init fills
them in with their defaults and keeps every value already set. An existing `.megaignore` is
left alone. This covers a workspace that
arrived through sync with its `ws.json` but without some directories. Machine setup is separate:
`ws dotfile link`, `ws trash enable`, `ws cron apply`, `ws completion enable`.

Plan: up to three actions, all pre-checked. They are: create the missing directories (one action
that lists them), write `ws.json` (or `Fill in 7 missing keys in /home/user/Workspace/ws.json;
values you set are kept`), and write `.megaignore`. When nothing is missing, it prints
"Workspace already initialized: /home/user/Workspace".

| Flag | Default | Meaning |
| --- | --- | --- |
| `-w, --workspace <dir>` | `$WS_WORKSPACE`, else `~/Workspace` | workspace root |
| `-c, --config <file>` | `$WS_CONFIG`, else `<workspace>/ws.json` | where `ws.json` goes |
| `--dry-run` | false | print the plan; change nothing |

`--json`: command `init`; data `workspace`, `config`, `created` (absolute paths), `existing`
(absolute paths), `actions`.

Exit: 0, 1, 3.

TUI: until the workspace is initialized, the TUI shows the setup screen. `enter` hands the terminal to
`ws init` with the TUI's `--workspace` and `--config`.

Was: `ws init`.

#### `ws config show`

```text
ws config show [--defaults] [--json]
```

Prints `ws.json` as ws resolves it, as indented JSON. It adds `workspace` (the absolute workspace
root) and `config` (the absolute path of `ws.json`, decision). Every path-valued setting is shown
absolute, with `~` expanded and relative values joined to the workspace root: `scratch.root_dir`,
`repo.roots`, `repo.exclude_dirs`, `secret.skip_dirs`, and `links[].original` (decision: today the
link originals stay workspace-relative). Other values are shown as stored. It needs an initialized
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

Was: `config view`, `config defaults`, `config --defaults` (hidden aliases).

#### `ws config edit`

```text
ws config edit [--editor <cmd>]
```

(new) Opens `ws.json` for editing and saves it only if the result is valid (decision):

1. It copies `ws.json` to `<dir of ws.json>/.ws.json.edit` and opens the copy with `--editor`,
   else your editor (§4.10). This is the same as every other `edit` command.
2. After the editor exits, it validates the copy. The copy must be JSON, its `schema` must be from
   1 to the version this build writes, and it must have no unknown keys. Old key names that ws
   still reads, such as `log`, are accepted.
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

Was: new.

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

Was: `completions bash|zsh|fish`, `completions install`, `completions uninstall`. rc files call
`ws completions bash`, so that alias stays silent. `completions install|uninstall` print the
stderr line.

#### `ws tui`

```text
ws tui
```

Opens the interactive terminal UI on the Dashboard. The screens are Dashboard, Repos, Dotfiles,
Scratch, Sessions, Ignore, Secrets, Cron and Trash, reached with 1–9 and tab. It honours
`--workspace` and `--config`. If the workspace is not initialized, it opens the setup screen. stdin
and stdout must be a terminal.

Exit: 0 on quit. 1 when stdin or stdout is not a terminal ("ws tui requires an interactive
terminal.") or the UI fails.

Was: `ws tui`.

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
`schema` was renamed to `config_schema` so it is not confused with the envelope's `schema`).

Exit: 0.

Was: `ws version`.

#### `ws help`

```text
ws help [noun [verb…]]
ws <noun> [verb…] --help
ws <noun>
ws
```

Bare `ws` and `ws help` print the overview: the usage line, the nouns, the workspace commands and
the global flags. `ws help <noun>` (decision: today the argument is ignored), `ws <noun> --help`
and `ws <noun>` with no verb print that noun's help: its verbs and flags. All of these go to stdout
and exit 0. Help never lists hidden aliases or plumbing (`__complete`, `git-credential-helper`).

```text
$ ws
ws — Workspace manager

Usage:
  ws <noun> <verb> [targets…] [flags]

Nouns:
  repo        Git repositories under the repo roots
  dotfile     Dotfiles kept in the workspace and linked back
  scratch     Scratch directories
  session     Recorded terminal sessions
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

Was: `ws help`.

#### Removed commands

`ws reset` and `ws restore` are gone. Each prints one line to stderr and exits 1 (decision):

- `ws: "reset" was removed; use ws dotfile unlink --all, ws trash disable, ws cron rm <job>`
- `ws: "restore" was removed; on a new machine run ws init, ws dotfile link, ws trash enable, ws cron apply`

---

## 6. TUI

`ws tui` is a full-screen interface over the same `internal/` packages as the CLI. It
follows k9s's conventions: one screen per noun, a few global keys that mean the same
thing everywhere, screen keys named after their verbs, marks for acting on many rows, and
a `:` command bar that takes the CLI's own words.

### 6.1 Frame

```text
ws  Dashboard¹  Repos²  Dotfiles³  Scratch⁴  Sessions⁵  Ignore⁶  Secrets⁷  Cron⁸  Trash⁹   tabs
────────────────────────────────────────────────────────────────────────────────────────   rule
 System path        Workspace copy                       Status         Sudo                body
 ~/.bashrc          ~/Workspace/configs/dotfiles/bashrc    ✔ ok
 ~/.gitconfig       ~/Workspace/configs/dotfiles/gitconfig ✘ broken
 …
2 marked — l link · a add · ctrl+d unlink act on them · esc clears                         status line
✔ 2 done                                                                                    notice (until the next key)
a add • l link • ctrl+d unlink • e edit • b back up • tab screens • : command • ? help • q quit   footer
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

Tab order: Dashboard, Repos, Dotfiles, Scratch, Sessions, Ignore, Secrets, Cron, Trash.
Every screen except the Dashboard is the list of its noun's rows, the same rows as
`ws <noun> ls`. The tab bar shows each screen's jump digit as a superscript.

### 6.3 Global keys

These mean the same on every screen. No screen binds them to anything else.

| Key | Does |
| --- | --- |
| `enter` | open the row: drill in, view, or launch (each screen says what) |
| `esc` | one level up: close a dialog, then clear the filter, then clear the marks, then leave a detail view, then go to the Dashboard. Does nothing at the Dashboard. |
| `:` | the command bar (§6.6) |
| `/` | filter the list (same expressions as `--filter`) |
| `?` | full help, grouped: this screen, lists, general, `:` commands |
| `q`, `ctrl+c` | quit (`ctrl+c` always works, even inside a dialog) |
| `tab` / `shift+tab`, `]` / `[`, `1`–`9` | next / previous screen; jump to a screen |
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
| shown | the marked rows, else every row the filter shows | fleet actions: fetch, pull, save, run, link |

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
shows (`f` fetch, `i` ignore, `b` back up, `m` mute). A letter keeps one meaning on a screen,
but may mean different verbs on different screens (`r` runs on Repos and Cron, records on
Sessions, and restores on Trash), as in k9s. Global keys never change meaning. The `:`
commands are listed with each screen. They are the CLI's words after the noun.

**Dashboard.** The workspace's home: a greeting, **Needs attention** (errors, then
warnings, then info), and an overview grid with one card per screen. `↑` `↓` move in the
list, and all four arrows move in the grid (`↑` from the top row returns to the list).
`enter` on a problem opens its screen filtered to the affected rows (for example
Dotfiles with `status!=ok`), so the screen's own key acts on exactly those rows. `enter` on
a card opens its screen. The Dashboard has no actions of its own.

Some rows are advice, not problems. They are listed last, at info level, never in red or
yellow, and they never count towards the card's status. `enter` on such a row opens the
command bar pre-filled with the suggested command, which you can run, edit or dismiss with
`esc`. One example is a pass store outside the workspace: "Your pass store syncs only
through its own git remote. A copy in the workspace would sync with MEGA too." `enter` then
pre-fills `:dotfile add ~/.password-store --dir --to ~/Workspace/secrets/dotfiles/password-store`.

**Repos**

| Key | Does | Rows |
| --- | --- | --- |
| `enter` | `repo show`: status, upstream, hygiene findings, last fetch failure | cursor |
| `f` | fetch | shown |
| `p` | pull: fetch, ask rebase or merge for each diverged repo, then the checklist | shown |
| `s` | save: commit everything (timestamped), pull (asking where diverged), push | shown |
| `r` | run a command: prompt, checklist, then an output view | shown |
| `c` | configure: hand the terminal to `ws repo configure` (identity, upstream, …) | selected |
| `a` | add a discovery root (prompt with directory completion) | none |

`:` commands: `push`, `root ls`, `root rm <dir>`, `ls --fetch`.

**Dotfiles**

| Key | Does | Rows |
| --- | --- | --- |
| `a` | add. With marks: take the marked rows' live files (the overwritten ones). Without: prompt for a path, pre-filled with the cursor row's path when that row is overwritten; a directory expands to a checklist of its entries | marked, else none |
| `ctrl+d` | unlink | selected |
| `l` | link: repair the broken and overwritten links (the workspace copy wins) | shown |
| `e` | edit the workspace copy | cursor |
| `b` | back up now: commit pending changes and push | none |

`:` commands: `add <path…> [--dir] [--to <ws-path>]`, `link <path> --to <ws-path>`,
`unlink --all`, `backup enable`, `backup disable`,
`backup status` (a view: remote, ahead/behind, uncommitted, recent commits; `esc` returns).

**Scratch**

| Key | Does | Rows |
| --- | --- | --- |
| `a` | new: prompt for a name | none |
| `enter` | open in your editor | cursor |
| `t` | tag: prompt for tags, comma-separated; `-tag` removes one; `tab` completes tags used before | selected |
| `ctrl+d` | delete (to trash) | selected |

`:` commands: `add <path>`.

**Sessions**

| Key | Does | Rows |
| --- | --- | --- |
| `enter` | show the transcript (`c` toggles commands only; `esc` returns) | cursor |
| `r` | record: prompt for a tag, then hand the terminal to the recorded shell | none |
| `p` | prune: prompt for an age (blank enforces the size cap), then a checklist | all rows |
| `ctrl+d` | delete | selected |

`:` commands: `stop`, `grep <text>` (narrows the list to sessions with a match; `enter` opens
the viewer at the first match; `esc` clears it).

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

### 6.6 Command bar

`:` opens a one-line command bar in place of the footer. It takes the CLI's words:

| Typed | Runs |
| --- | --- |
| `:<screen>` | switch screen. Any unique prefix of the noun or title works: `:rep`, `:dot`, `:sess`, `:dash` |
| `:<verb> [args]` | the active screen's command, the CLI words after the noun: `:backup push` on Dotfiles is `ws dotfile backup push` |
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
| Chooser | `↑` `↓`, or an option's letter (`r` rebase, `m` merge, `s` skip); `enter` chooses; `esc` cancels the whole command, so nothing runs |
| Checklist | `↑` `↓`; `space` toggles an action; `ctrl+a` toggles all; `enter` applies; `esc` cancels. While running, keys wait. When done, `enter` or `esc` closes. |

A choice ws must not make for you, such as rebase or merge for a diverged repo, is asked in a
chooser before the checklist (`[r]ebase`, `[m]erge`, `[s]kip`), one repo at a time. It is
never guessed and never a second key.

The checklist is the action plan (§4.7): one line per action, pre-checked as the CLI
would do them, except destructive plans (`unlink --all`, `trash empty`), which start
unchecked. It runs one action at a time, shows each result as it arrives, and ends with a
summary notice.

### 6.8 Handing over the terminal

Some commands need the terminal or are multi-step CLI wizards. The TUI suspends itself,
runs them, and resumes and refreshes the screen when they exit.

- **Interactive programs:** `session record` (the recorded shell), and terminal editors for
  `e` and the `edit` commands, `config edit` included (your editor, §4.10, default `vim`).
  GUI editors (for example `code`) are launched without suspending.
- **CLI wizards:** `init` (the setup screen), `repo configure`, `dotfile backup enable`,
  `pass init`, `pass backup enable`, `credential enable`, `credential disable`.
- **sudo:** dotfile actions on sudo entries run in the terminal, so sudo can ask for a
  password.
- **Paged CLI output:** `ignore tree`, `credential status`, `config show`.

Everything else is native.

### 6.9 Parity

Every CLI command is reachable in the TUI as a key, a `:` command, or the screen's list.
The only exceptions are plumbing (`git-credential-helper get|store|erase`), shell output
(`completion …`), and `version`, `help` and `tui` themselves. A test enforces this (dev.md
§13).

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

- Walks (`ignore ls`, `secret ls`, `dotfile ls`, `repo ls`) skip unreadable entries and
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

Everything ws deletes (`scratch rm`, `session rm`, the Ignore screen's `ctrl+d`) moves into
the XDG trash (`$XDG_DATA_HOME/Trash`): `files/` holds the item, and
`info/<name>.trashinfo` records its original path and deletion time
([freedesktop trash spec](https://specifications.freedesktop.org/trash-spec)). The
desktop file manager uses the same directory, so ws deletions appear in its trash view, and
`ws trash restore`, `ws trash empty` and the file manager act on the same items.
`ws trash enable` makes a plain shell `rm` and VS Code's delete use it too. ws's own
deletions use the trash whether or not that is enabled. The only permanent deletions are
`trash rm`, `trash empty` and `trash prune`.

### 7.4 Private remotes and credentials

Dotfile and pass-store backups contain secrets by nature (SSH config, kubeconfigs, tokens).
ws therefore pushes them only to **private** remotes:

- `backup enable` and every push to a backup remote check the remote's visibility through
  the git host's API, and refuse a public remote. That includes `backup push`, the
  auto-push after a change, and the `dotfile-backup` and `pass-backup` cron jobs. The check
  is new for the pass store.
- When visibility cannot be determined (an unknown host, no token, no network), ws prints a
  warning and pushes. Refusing would stop backups to self-hosted remotes.
- There is no flag to bypass the refusal of a public remote.
- The API check authenticates with the token in pass: the entry named in
  `dotfile.git.pass_entry` for the dotfile backup and in `pass.pass_entry` for the pass
  store, each defaulting to `git/<host>`. The
  push itself uses git's normal credentials. With `ws credential enable`, git's helper is
  ws itself, backed by the same pass entries.

### 7.5 One description per list

Each noun's rows are described once, in `internal/<domain>/fields.go`. That description
holds the field key (the JSON key and the `--filter`/`--sort` name), title, raw value,
display text, and default columns. The CLI's `ls` and the TUI screen both render it, so a
field added there appears in `--json`, `--wide`, the TUI columns, `d` describe, `y` JSON,
and both filters at once. See [docs/adr/0001](docs/adr/0001-tui-list-screens-from-shared-resource-descriptions.md).

### 7.6 One grammar for CLI and TUI

The CLI verbs, the TUI keys and the TUI `:` commands are one vocabulary (Part 3, §6.5). A
TUI action is the CLI command with the rows as its targets, so the two cannot disagree
about what a verb does. See
[docs/adr/0002](docs/adr/0002-one-command-grammar-for-cli-and-tui.md).

---

## 8. Dependencies

### 8.1 Programs ws runs

ws does its file work natively in Go: walking, symlinks, moves, copies, sizes, hashing,
and matching. It runs these programs:

| Program | Package | Used by | Required |
| --- | --- | --- | --- |
| `git` | git | `repo …`, `dotfile backup …`, `pass backup …` | for those commands |
| `script` | util-linux ≥ 2.35 | `session record` (the PTY recording) | for recording |
| `pass`, `gpg` | pass, gnupg | `pass …`, `secret store`, `credential …`, backup tokens | for those commands |
| `crontab` | cron | `cron …` | for cron |
| `sh`, `bash` | — | `repo run`, cron job scripts, shell integrations | yes |
| `sudo` | sudo | dotfiles whose system path needs root (`/etc/…`) | for those dotfiles |
| your editor: `ws.json` `editor`, `$VISUAL`, `$EDITOR`, else `vim` | vim | `edit` commands, `scratch new\|open` | for editing (the path is printed if it can't start) |
| `$PAGER` / `less` | less | paged output | optional |
| `megasync`, `pgrep`, `pkill` | MEGAsync, procps | the `mega-sync` cron job | for that job |
| `wl-copy` / `xclip` / `xsel` | wl-clipboard / xclip / xsel | TUI copy (OSC 52 is always sent too) | optional |

A missing program fails only the command that needs it, with an error naming the program
and its package.

### 8.2 Build

| Tool | Version | Purpose |
| --- | --- | --- |
| `go` | ≥ 1.26 | build the binary |
| `make` | any | `make build`, `make test` (tests run in Docker; see dev.md) |
| `docker` | any | the isolated test run |

### 8.3 Around ws

- **MEGAsync** syncs `~/Workspace` to the cloud. ws generates its `.megaignore`.
- **pass** and **GnuPG** hold secrets and git tokens.
- Editors and apps whose settings ws manages as dotfiles (VS Code and others) need nothing
  from ws.
