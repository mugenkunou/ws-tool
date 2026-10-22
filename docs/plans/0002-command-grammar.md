# Plan 0002: implement the command grammar

**For:** the agent who changes the code. You start with no context, so read this whole
file before touching anything.

**Goal:** make the CLI and the TUI match [spec.md](../../spec.md): Part 3 (grammar), Part 4
(CLI conventions), Part 5 (every command) and Part 6 (TUI), as decided in
[ADR 0002](../adr/0002-one-command-grammar-for-cli-and-tui.md). The spec is the target. When
the spec and this plan disagree, the spec wins. Report the disagreement rather than
choosing silently.

**Deadline:** the user demos the TUI at KubeCon in the week of 2026-10-26. The TUI (phases
C–D) is what will be seen. If time runs short, finish the TUI and the CLI pieces it needs
first, then the rest of phase B.

**Code notes:** `docs/plans/0002/notes-*.md` has one file per noun group. They hold file:line
locations, today's behaviour, and the tests each command touches. They were written
before the user's last round of decisions, and each starts with a list of what changed
since. Use them for *where*, and the spec for *what*. Their line numbers are against
`b77d3c8`. After that, `0d08e4e` changed `internal/ignore/engine.go`, `cmd/ignore*.go` and
`internal/tui/screens/ignore/ignore.go` (warnings under an always-sync directory are muted,
also pinned by `internal/ignore/harbor_view_test.go`), so recheck lines there.

---

## 0. Ground rules (read first)

1. **Tests run in Docker only: `make test`.** Never run bare `go test` on the host. On
   2026-10-16 a host `go test ./internal/workspace` deleted the user's real
   `~/.local/state/ws-tool/trash-setup.json`. Some packages still touch real HOME paths
   (crontab, XDG trash, `~/.bashrc`, git config). For a quick host build, use
   `TMPDIR=$HOME/tmp GOCACHE=$HOME/.gocache GOTMPDIR=$HOME/gotmp go build ./...` and the same
   env for `go vet ./...`. `/tmp` is mounted noexec on this machine.
2. **Snapshots:** accept intended changes with
   `UPDATE_SNAPSHOTS=1 TMPDIR=$HOME/tmp GOCACHE=$HOME/.gocache GOTMPDIR=$HOME/gotmp go test ./internal/tui/...`.
   That host run is the one exception to rule 1, because `internal/tui/...` tests isolate HOME.
   Read every golden diff as if it were code.
3. **Never break the user's live machine.**
   - The installed binary is `~/.local/bin/ws`. Don't replace it, and don't run
     `make install`.
   - The live crontab runs `/home/siva-14414/.local/bin/ws cron run mega-sync` and
     `… cron run dotfile-sync`. Git calls `ws git-credential-helper get`. `~/.bashrc` runs
     `eval "$(ws completions bash)"`. All of these must keep working, silently.
   - Never run a ws command against the real crontab, git config, `~/.bashrc`, trash or
     workspace. Use a temp `--workspace`, `WS_CRONTAB_FILE`, `GIT_CONFIG_GLOBAL`,
     `XDG_DATA_HOME` and `XDG_STATE_HOME`, as the tests do.
4. **New directories:** after adding any directory under `internal/` or `cmd/`, run
   `git status --short --ignored internal cmd`. The `.gitignore` is a large generic
   template, and it once silently dropped `internal/tui/env` from two releases.
5. **Small commits, one per step below.** Use the repo's style (`area: what changed`, then
   a body explaining why), and end each with your harness's attribution trailer. Never push
   or tag: the user does that.
6. **No code beyond this plan and the spec.** If something in the spec can't be built as
   written, stop and report it.
7. **Docs are done.** spec.md, dev.md, README.md, PHILOSOPHY.md and the ADRs describe the
   target. Change them only to fix a mistake, and say so in the commit. At the end, remove
   the sentence in spec.md's intro that says the code still uses old names.

## 1. Read first

| File | Why |
| --- | --- |
| spec.md §3 | nouns, the closed verb glossary, every command (§3.4), renames and removals (§3.5) |
| spec.md §4 | global flags, per-command help, lists, JSON schema 3, paths, exit codes (2 is retired, 130 is a cancelled prompt), RO/RW, the editor (§4.10) |
| spec.md §5 | behaviour, flags, output, JSON and exit codes per command. The "Was:" lines map old to new |
| spec.md §6 | TUI: global keys, screen keys, scopes, bottom status line, command bar, dialogs |
| dev.md §9, §12.6, §13 | the rules, and the tests that enforce them |
| `internal/tui/keys/keys.go` | every binding today |
| `internal/tui/reslist/{reslist,actions}.go` | the shared list, scopes, `Run` |
| `internal/tui/app/{app,coverage}.go` | routing, footer, CLI↔TUI coverage |
| `cmd/root.go`, `cmd/complete.go`, `cmd/help.go`, `cmd/plan.go` | dispatch, global flags, completion, help, action plans |

Today's code works and is well tested. Don't rewrite what you don't need to: rename,
re-route and add. Most TUI actions already come from tables (`reslist.Action{Key, Scope,
Do}`), so most remapping is editing `keys.go` and those tables.

## 2. What changes, on one page

- **Grammar:** `ws <noun> <verb>`. There is no `scan`, `fix`, `reset` or `setup`. Old names
  become hidden aliases (spec §3.5): typed ones print one stderr line, and plumbing stays
  silent. JSON envelope `schema: 3`, with command `<noun>.<verb>`. Exit code `2` is gone, and
  `ls`/`lint` exit 0 whatever they find.
- **repo:** `ls` (absorbs `scan`, adds `--fetch`), `show`, `fetch`, `pull` (fast-forward;
  diverged repos **ask** rebase/merge/skip; `--rebase`/`--merge`; skipped when it can't ask),
  `push` (new, push only), `save` (new: `git add -A`, commit `ws save <date time> (<host>)`,
  pull asking as above, push), `configure` (new, interactive: identity with no
  suggestions, upstream, `init.defaultBranch`, `ws credential enable`, fetch), `run`,
  `root add|ls|rm`. `sync` is removed (alias → `save`). New hygiene check `credentials`.
- **dotfile:** `ls`, `add [--to] [--dir]` (also takes the live file of an overwritten entry,
  the old `sync`), `link [path…]` (repair, the old `fix`), `link <path> --to <ws-path>` (the old
  `link`), `unlink <path…>|--all` (the old `rm`, `reset`), `edit` (new), `backup
  enable|disable|status|push` (the old `git …`). **`--class` and `links[].class` are removed.**
- **scratch:** `ls`, `new`, `add` (new), `open`, `tag` (labels only: no suggestions,
  `--rm`), `rm`. `grep` and `search` are gone.
- **session** (was `log`): `ls`, `record`, `stop`, `show` (new), `grep` (new), `rm`, `prune`.
- **ignore:** `ls` (paths that don't sync, plus the rule; `--muted` lists muted warnings),
  `lint` (the old `scan`), `add`, `rm` (new), `mute`/`unmute` (**warnings only, sync
  untouched**, stored in new `ignore.muted`), `check`, `tree`, `edit`.
- **secret:** `ls`, `show` (new), `allow`, `mute|unmute` (skip_dirs), `store`. `fix` is gone.
- **pass** (split out of secret): `init`, `status` (with a gentle "workspace copy" tip),
  `audit`, and `backup enable|disable|status|push`, **identical in UX to dotfile backup**.
- **credential:** `enable|disable|status`. `git-credential-helper get|store|erase` is
  unchanged plumbing.
- **cron:** `ls` (absorbs `status`), `add`, `rm` (both also write `configs/crontab`), `run
  [--if-due]`, `log [job…]`, `apply` (a plan). Jobs: `mega-sync`, `dotfile-backup` (was
  `dotfile-sync`), `pass-backup` (new), `session-prune` (was `log-prune`). Retired:
  `secret-scan`, `ignore-scan`, `repo-sync`.
- **trash:** `ls`, `restore` (new), `rm`, `empty`, `prune --older-than <age>` (was `purge`),
  `enable|disable|status`.
- **workspace:** `config show [--defaults]|edit` (edit is new), `completion …|enable|disable`,
  `init` (writes and completes a **full ws.json**), `tui`, `version`, `help`. Removed:
  `reset`, `restore`, `--manifest`.
- **ws.json:** always has every key. New keys: `editor`, `ignore.muted`, `pass.pass_entry`,
  `session` (was `log`). `scratch.editor_cmd` becomes `editor`. `links[].class` is dropped.
- **Editor:** `ws.json` `editor` → `$VISUAL` → `$EDITOR` → **`vim`**, for every edit and open.
- **TUI:** k9s-style global keys (spec §6.3: `:` command bar, `d` describe, `y` JSON,
  `ctrl+d` delete, `ctrl+a` mark all, `ctrl+r` refresh); lowercase screen keys named after
  their verbs; no shift+letter actions; a bottom status line that never moves rows; Logs is
  renamed Sessions; Ignore has three views; the Dashboard has no actions, and shows advice
  rows that pre-fill the command bar.

---

## 3. Phase A: CLI foundations (commit each step)

Every noun depends on these, so they come first.

### A1. One command table drives help, completion and coverage

Today each `cmd/<noun>.go` hand-writes its group help, so `ws repo sync --help` prints the
whole group. `cmd/complete.go` keeps its own subcommand lists, and `cmd/parity_test.go`
parses `Usage` strings.

- Add `cmd/commands.go` with `var commands = []command{…}`, one row per line of spec §3.4.
  Fields: `Path []string` (`{"repo","root","add"}`), `Args string`, `Summary string`,
  `RO bool`, `Flags []flagDoc`, and `Fields func() []string` for `ls` and `lint` (from the
  domain schema's `Keys()`).
- Rewrite `cmd/help.go` so `ws help [noun [verb…]]`, `ws <noun> --help` and
  `ws <noun> <verb…> --help` all render from the table, in the spec §4.2 format. Delete the
  per-file help builders once nothing calls them.
- `cmd/complete.go`: derive `topLevelCommands` and each completer's `subcommands` from the
  table, including sub-nouns (`repo root …`, `dotfile backup …`, `pass backup …`). Keep the
  `resolve` functions, and make `commandFlags()` read the table's flags. That also drops the
  stale `reset`/`restore`.
- Deferred until after KubeCon: one shared command list in `internal/` for the TUI's `:`
  bar. The TUI declares its `:` commands per screen for now (phase C).

Tests: `TestHelpEveryCommand` (every row's `--help` prints its own usage), and
`TestGlossaryVerbs` (every verb in the table is in spec §3.3; keep that list as a Go slice
next to the test, and exempt `completion bash|zsh|fish`, which are shell names). `TestCompletersOfferEverySubcommand` becomes table-driven and checks
that no alias is offered. `TestCLICoverage` reads the table instead of help text.

### A2. Hidden aliases

- Add `cmd/aliases.go`: `[]alias{Old, New []string; Plumbing bool; Args func([]string)
  []string}`, covering every row of spec §3.5. Some aliases rewrite arguments:
  `trash purge --older-than 30` → `--older-than 30d`; `secret scan --pass` → `pass audit`;
  `dotfile reset` → `dotfile unlink --all`; `dotfile link <p> <q>` → `--to <q>` when `<q>` is
  inside the workspace; `repo scan [--no-fetch]` → `repo ls [--fetch]`; `log start --tag t`
  → `session record t`; `scratch search q` → `scratch ls --filter q`.
- In `Execute` (`cmd/root.go`), after `parseGlobalFlags`, find the longest matching old
  prefix and rewrite the args before the `switch`. Unless the alias is plumbing, or
  `--quiet`/`--json` is set, print `ws: "<old>" is now "<new>"` on stderr.
- **Plumbing (silent):** `cron run <old job>`; `git-credential-helper get|store|erase`;
  `completions bash|zsh|fish` (rc files run it at every shell start); and anything the
  shell scripts written by `cmd/completions.go`, `cmd/log.go` and `internal/trash` invoke
  (grep them for `ws `).
- Typed `ignore fix` and `secret fix` have no single successor. They print the replacement
  commands and exit 1. The `--secret-mode` batch forms are real aliases
  (`allowlist` → `secret allow`, `exclude` → `ignore add`, `pass` → `secret store`).

Tests: `TestAliasesReachTheirTarget` (same output and JSON command as the target, on a temp
workspace), and `TestPlumbingAliasesAreSilent`.

### A3. JSON schema 3, exit codes

- The envelope `schema` becomes `3`, and every `writeJSON(…, name, …)` uses
  `<noun>.<verb>` after alias resolution. Grep `writeJSON(` and `writeJSONDryRun(`.
- **Must fix:** with `--json`, write commands return `writeJSON`'s 0 even when actions failed.
  `writeJSONDryRun` returns 0 (cmd/root.go:297), and every dotfile, repo and trash write
  command returns it (see the notes). Return `planResult.ExitCode()` after writing.
- Retire exit 2: grep `return 2`, `ignoreScanExit`, `renderRepoScan`. Keep 130 for a
  cancelled prompt (cmd/scratch.go uses it).

Test: `TestJSONCommandNames` runs each RO command with `--json` and checks `command`.

### A4. Shared helpers

- `internal/age`: `Parse(s) (time.Duration, error)` accepts `30d`, `12h` and `2w`, and a bare
  integer means days. Reject zero and negatives. Table tests.
- `internal/editor`: `Command(cfg) []string` resolves `ws.json` `editor` → `$VISUAL` →
  `$EDITOR` → `vim`. `IsTerminal(argv) bool` is true for vim, nvim, vi, nano, emacs -nw, hx and
  the like. Use it for `dotfile edit`, `ignore edit`, `config edit`, `scratch new|open`, and
  the TUI's `handover.Editor` (internal/tui/handover). Replace every `vi` and `code`
  fallback: grep `"vi"`, `EditorCmd`, `editor_cmd`.
- `flag` parsing: `parseInterspersed` exists but is unused, so a flag after a positional
  argument (`ws dotfile add ~/.x --dir`) fails. Use it, or an equivalent, in every
  subcommand.

### A5. ws.json: every key, always

- `internal/wsconfig`: remove `omitempty` from every field, so every write writes every key
  in a stable order. On load, keys missing from the file take their defaults
  (`wsconfig.Default()`), and the next save writes them.
- New keys: `Editor string` (`editor`), `Ignore.Muted []string` (`ignore.muted`),
  `Pass.PassEntry` (`pass.pass_entry`). Rename `Log` → `Session` (JSON `session`), and read
  the old `log` when `session` is absent. Write only the new names. Stop reading and writing
  `scratch.editor_cmd` (the default editor is vim now, and a user who wants `code` sets
  `editor`) and `Link.Class`.
- Remove the `--manifest` global flag (`cmd/root.go`: the `globalFlags.manifest` field, the
  registry entry, the `registerGlobalFlags` lookup) and every use.
- Tests: write → read round trip with every key present; a file missing keys loads, and
  the next write fills them; the old `log` and `scratch.editor_cmd` are read.

---

## 4. Phase B: CLI nouns (one commit per noun, in this order)

The order puts data-loss and security fixes first. Each step names the **must-fix** bugs
it closes. Regression-test every one. Details and locations are in the notes.

### B1. dotfile (notes: notes-dotfile.md)

- `cmd/dotfile.go`: `ls`, `add`, `link`, `unlink`, `edit`, `backup enable|disable|status|push`.
  `migrate` stays hidden.
- **`add`** (spec §5 dotfile add): the current `Add`, plus `--to <ws-path>` (exactly one
  path, inside the workspace, must not exist). On an **overwritten** entry it takes the live
  file. Write `dotfile.TakeLive`: the old copy goes to the trash (`trash.SoftDelete`), and the live
  file moves into place, keeping `original`. It refuses shared copies, directories, and
  symlinks pointing elsewhere. It does nothing for `ok` entries, and only hints for
  broken-with-nothing-there entries.
- **`link`**: without `--to`, it is `Fix` with `Only` (match `filepath.Clean` on both sides,
  bug B11); a displaced live file becomes `.ws-backup`, and a missing workspace copy is
  reported as failed. With `--to`, it is `Link`, with the displaced file renamed to
  `.ws-backup` instead of refused, `<ws-path>` resolved against the cwd, and sudo honoured
  (bug B5).
- **`unlink`**: `Remove`/`Reset`. A displaced live file becomes `.ws-backup` instead of being
  deleted, and a shared (link-made) copy is **copied** back instead of moved.
- **Remove `--class`** everywhere (flags, `wsconfig.Link.Class`, the `class` schema field
  in `internal/dotfile/fields.go`, the TUI column). A typed `--class` is an error that
  points to `--to`. Secret detection still picks
  `secrets/dotfiles/` over `configs/dotfiles/`.
- **`backup`**: map the old `git` subcommands as spec §5 says. `disable` clears the remote and
  removes the `dotfile-backup` cron job. Auto-push runs the visibility check. `GitSync`
  copies the resolved config path, not `<workspace>/ws.json` (bug B9).
- **Must fix:**
  - **B1:** `dotfile sync` on a healthy link deletes the only copy (dotfile.go:393, :431).
  - **B2:** `rm`/`reset` move link-made originals out of the workspace (dotfile.go:346,
    reset.go:109).
  - **B3:** `rm`/`reset` delete a live file that replaced a link (dotfile.go:329,
    reset.go:78).
  - **B6:** the token lookup asks pass for `git/git/<host>` (internal/secret/gittoken.go:8,
    credential.go:73-85). Call `tryPassEntry(entry)` directly.
  - **B8:** an authenticated 404 reads as "public" (internal/repo/visibility.go:135, 180,
    216). Check `vis.Error` first.
  - **B7:** the `--json` exit code (phase A3).
  - **B10:** `link --dry-run` runs the plan and then calls `Link` again (cmd/dotfile.go:1043).
- Aliases (A2): `scan`, `rm`, `reset`, `fix`, `sync`, `link <p> <ws-path>`, and
  `git setup|enable|connect|remote|disconnect|status|log|push`.

### B2. session, was log (notes: notes-scratch-session.md)

- Rename `cmd/log.go` to `cmd/session.go`. The noun is `session`, and `internal/log` may keep
  its package name.
- **Must fix (security):** drop `script -I` (`--log-in`). Today it writes raw keystrokes,
  including passwords typed at sudo and ssh prompts, into a synced file. Commands come from
  the shell's history hook instead (the notes have the bash and zsh hooks).
- **Must fix:** the active-recording marker moves from the synced
  `data/ws-log/active.json` to `$XDG_STATE_HOME/ws-tool/session-active.json`. An old marker is
  finalized without signalling any PID.
- **Must fix:** bare `prune` removes nothing, so the nightly job is a no-op. The default now
  enforces `session.cap_mb`. `--older-than` uses `internal/age`. `rm` and `prune` soft-delete.
- New: `show <tag> [--commands]` and `grep <text>` in the CLI. The prompt indicator becomes
  `● ws:rec`; keep `WS_LOG_RECORDING=1`.
- Aliases: `log start|stop|ls|rm|prune`, with `log start --tag t` → `session record t` and
  `--quiet-start` → `--quiet`.

### B3. scratch

- **Must fix:** `scratch.Delete` deletes **every** directory whose name contains the argument
  (in the TUI too). A name resolves to an exact match, else to one unique case-insensitive
  substring. Zero or several matches is an error listing the candidates.
- Remove `grep` and `search` (alias `search q` → `ls --filter q`). `tag`: no content-based
  suggestions; `--rm <tag>` removes one; with no tags, prompt on a terminal, else usage
  error; `--auto` is accepted and ignored. New: `add <path…>` (spec §5 scratch add).
- `new` and `open` use `internal/editor` (default `vim`).

### B4. trash (notes: notes-cron-trash-ws.md)

- New: `restore <item…>`, which never overwrites, creates missing parents, and exits 0/3/1.
  `purge` becomes `prune --older-than <age>`.
- Items are named by their `name` column or their original path.
- **Must fix:**
  - `trash disable` deletes only its state file, so the script and the rc alias stay
    (internal/trash/reset.go).
  - `cmd/trash.go` ignores `$XDG_DATA_HOME`, while soft-delete honours it.
  - The VS Code integration writes nothing effective. See the notes, and decide with the
    spec.
- **Test risk:** once `cmd/trash.go` reads `$XDG_DATA_HOME`, every `cmd` and `internal/trash`
  test must set it to a temp dir, or the tests can touch the real trash.

### B5. cron

- Jobs: `mega-sync`, `dotfile-backup` (alias `dotfile-sync`), `pass-backup` (new:
  `ws --workspace <W> pass backup push --quiet`, every 30 min), `session-prune` (alias
  `log-prune`). Retired: `secret-scan`, `ignore-scan`, `repo-sync`. `cron add` rejects
  them, `cron rm` removes their blocks, and `cron run` logs a skip and exits 0. Presets:
  `sync` = mega-sync, dotfile-backup, pass-backup; `maintenance` = session-prune.
- `cron run` always runs. The new `--if-due` does the half-interval skip, and `cron add`
  writes `--if-due` and `--workspace` on both lines (plus `--display` for mega-sync). A block,
  state record or log line under an old name counts as the new job's.
- **Must fix:**
  - `cron add`/`rm` write `configs/crontab` too, with one action per job covering both files.
  - `cron apply` re-appends non-ws lines on every run, and copies other machines' paths.
    Regenerate the blocks, and run it as a plan.
  - `cron add --display` is parsed and then ignored.
  - `TestCronLogJobFilter` passes without testing anything.
- `ls` folds in `status` (states per spec), and `log` takes zero or more jobs.

### B6. repo (notes: notes-repo.md)

- `ls` merges `scan` (no fetch by default; `--fetch`). `show` is new.
  `issues` counts warn+error findings only.
- **`pull`**: fetch first, then fast-forward when possible. For each diverged repo, ask
  `[r]ebase / [m]erge / [s]kip` on the terminal, before the plan. `--rebase`/`--merge`
  answer for all. Without a terminal and without a flag, skip with
  `diverged: choose --rebase or --merge`. A failed rebase or merge is aborted. (`PullAll`
  today leaves a failed rebase in place, internal/repo/repo.go:220.)
- **`push`** (new): push repos that are ahead. Skip the behind or diverged ones ("pull
  first, or ws repo save"), and repos with no upstream or a detached HEAD. Never force.
- **`save`** (new): per repo, `git add -A` and commit `ws save <YYYY-MM-DD HH:MM> (<host>)`
  (or `-m`), then fetch, pull (asking as above), push. One action per repo, naming its steps.
  The commit stays when a later step fails. Reuse what fits from `repo.PlanSync`/`SyncOne`,
  and delete the rest of sync. `sync` becomes an alias of `save`.
- **`configure`** (new, interactive, needs a terminal):
  - Per repo, ask only what its findings need: `Name:`/`Email:` typed with **no
    suggestions**, then global vs this repo.
  - Upstream: track `origin/<b>`, `push -u`, or add a remote.
  - `init.defaultBranch`, and `ws credential enable` for HTTPS remotes without a helper (the
    new `credentials` check). Offer a fetch for stale repos.
  - Global answers are asked once. The answers become a plan.
- `run [repo…] -- <cmd…>` now prints the output (today it's dropped, cmd/repo.go:536-542).
  `root add|ls|rm` replaces `add-root`/`ls-roots`, and `root rm` is new.
- Every hygiene finding's detail ends with `ws repo configure <repo>`.

### B7. ignore (notes: notes-ignore-secret.md)

- `ls` = ignored paths plus their rule (today's `ignore ls`); `ls --muted` = muted
  warnings; `lint [path…]` = today's `scan`, exit 0.
- **Mute is warnings-only (user decision):** `ignore mute <path…>` adds to `ignore.muted`
  and never touches `.megaignore`. Warnings for muted paths, or under always-sync
  directories (the built-in layout harbors and legacy `ignore_rules.safe_harbors`), are muted.
  There is no migration. Legacy harbors stay always-sync, so their warnings stay muted.
  `unmute` refuses paths under always-sync directories.
- `add` and `mute` require at least one path (no bulk by default), and `add` refuses dotfile
  originals (any `links[].original`). `rm` is new: it removes the
  exact user rule, and refuses with hints for wider or built-in rules.
- Rule order: user excludes are written last, so `ignore add` works inside always-sync
  directories (notes D1). **Before shipping, list the user's existing exclude rules that
  this activates.** Phase H does this on their machine; your code change must add a test.
- Remove the interactive `ignore fix` (A2 prints the replacements). The Ignore screen's
  delete (`ctrl+d`) soft-deletes via the trash. Today it is `os.RemoveAll`.

### B8. secret, pass, credential

- `secret ls` (today's `scan`, read-only; moving the stale-anchor pruning into
  `secret allow`), `show` (new), `allow` (at least one target is required, so a bare `allow`
  can never allowlist every secret), `mute|unmute` (`secret.skip_dirs`), `store` (never
  overwrites an existing pass entry; today `InsertEntry` uses `pass insert -f`). Remove the
  interactive `fix`.
- `pass init` (was `secret setup`), `pass status` (was `secret status`), with the
  **workspace copy** info line and tip (spec §5 pass, "A second copy in the workspace"), and
  `pass audit` (was `secret scan --pass`).
- **`pass backup` mirrors `dotfile backup` exactly.** Factor one helper (for example
  `internal/backup`) that both nouns call with their repo path and settings, so the
  verbs, flags, plan lines, `status` layout and JSON keys can't drift. `disable` clears the
  remote and removes the `pass-backup` job. The visibility check (spec §7.4) applies, and
  an undetermined result warns and continues.
- `credential enable|disable|status`. `git-credential-helper get|store|erase` stays
  byte-for-byte.

### B9. config, completion, init, version, help

- `config show [--defaults]`, and `config edit` (new): it edits a copy, validates it, and
  saves only if valid.
- `completion bash|zsh|fish|enable|disable`. `enable` rewrites an old `completions` rc
  block, and `completions bash|zsh|fish` stays a silent alias.
- `init` writes the **complete** `ws.json`. On an existing one, it fills in the missing keys
  and keeps the values ("Fill in N missing keys…").
- `version` JSON: `config_schema`. `reset` and `restore` print a pointer and exit 1.

---

## 5. Phase C: TUI foundations (commit each step)

Target: spec §6.1–6.4, §6.6, §6.7, and dev.md §13 "Key grammar". After each step, run the
snapshot tests and read the golden diffs.

### C1. `keys.go`

- Rewrite the package doc to match spec §6.3.
- `GlobalMap`: add `Command` (`:`) and `Refresh` (`ctrl+r`). Keep `Quit`, `ForceQuit`, `Help`,
  `NextTab`/`PrevTab` (`tab`/`]`, `shift+tab`/`[`) and `Screens` (1–9). The short help becomes
  `tab screens • : command • ? help • q quit`.
- `Table()` and `Detail()`: delete `HalfPageUp`/`HalfPageDown` (`ctrl+u`/`ctrl+d`), because
  `ctrl+d` is delete now. `PageUp` = `pgup`, `ctrl+b`; `PageDown` = `pgdown`, `ctrl+f`;
  `GotoTop` = `home`, `g`; `GotoBottom` = `end`, `G`.
- `ListMap`: `Detail` (`i`) becomes `Describe` (`d`). Add `JSON` (`y`). `MarkAll` moves from
  `A` to `ctrl+a`. Keep `/`, `>`, `<`, space, ctrl+space and ctrl+\. Add shared constructors
  `Add()` (`a`), `Edit()` (`e`) and `Delete()` (`ctrl+d`), which screens relabel with
  `key.WithHelp`.
- Delete every per-screen `r` refresh.
- Screen maps exactly as spec §6.5 (letters, help texts, order). `DashboardMap` keeps only
  movement and `Open`.
- `ConfirmMap`: `All` becomes `ctrl+a`.

### C2. `reslist`

- `d` opens the describe pane (today's `i`). `y` opens a JSON pane:
  `json.MarshalIndent(schema.Record(cursorItem))`. First check that no `Field.Value` in any
  `fields.go` depends on `resource.View` (only `Text` may). If one does, pass a
  `resource.CLIView()` schema for JSON.
- `ctrl+a` replaces `A`.
- **Status line at the bottom, always reserved (the user's explicit request).** `layout()`
  always gives the list `height-1` rows. `View()` renders the list, then the status line,
  which is blank when nothing is active, and the filter input while one is typed.
- The status text names keys with labels (`3 marked — ctrl+d unlink · l link act on them ·
  esc clears`), built from the declared actions' `Help()` and filtered by scope.
- Commands: `reslist.Command[M,T]{Name, Args, Help string; Scope Scope; Do func(M, []T,
  args []string) (M, tea.Cmd)}` and `RunCommand(…, nav.CommandMsg)`, using the same
  `Rows(scope)` as `Run`. A key and a command for one verb share a function.
- `SetFilter(expr)`, so the Dashboard can open a screen filtered.
- Screens with lines above the list (the Repos summary, the Secrets health line, the Ignore
  and Trash headers) keep them on top. Only the reslist status line goes to the bottom.

### C3. Dialogs

- `confirm`: `All` → `ctrl+a`. No option toggle: the old rebase toggle is gone.
- Diverged repos: before the pull/save checklist, the Repos screen opens a `choose` dialog
  per diverged repo (`rebase`, `merge`, `skip`, with the repo and its counts in the title),
  and passes the answers into the plan. Add per-option shortcut letters to `choose`
  (`Option.Key`: `r`, `m`, `s`). `esc` cancels the whole command, so nothing runs.

### C4. `nav`

- `ScreenLogs` → `ScreenSessions`, title "Sessions".
- `CommandMsg{Words []string; Args string}`, and an optional `Commander{ Commands()
  []CommandInfo }` (Name, Args, Help) so the app can complete and list `:` commands.
- `OpenCommandBarMsg{Prefill string}`, for the Dashboard's advice rows.

### C5. `internal/tui/cmdbar` (new package)

A one-line input drawn in place of the footer: `:` plus the input, plus the matching
completions after the cursor, dimmed and clipped to the width. `tab` completes the first
match, `enter` submits (`ResultMsg{ID, Line}`), `esc` cancels. The candidates are the screen
nouns and titles, the active screen's commands, `<noun> <command>` for every screen, and
`config show`, `config edit`, `help`, `q`, `quit`. Tests: completion, submit, cancel, clipping,
prefill.

### C6. `app`

- `:` opens the cmdbar, unless a dialog is open or a filter is being typed. While it's open,
  every key except `ctrl+c` goes to it.
- Parse the line (spec §6.6): `q`/`quit`; `help`; `config show` (paged) and `config edit`
  (editor); a unique prefix of a noun or title switches screen; `<noun> <words…>` switches,
  then sends `CommandMsg`; otherwise `CommandMsg` goes to the active screen. An unknown
  command shows a notice with the closest candidate.
- `ctrl+r`: broadcast `RefreshMsg` on the Dashboard; elsewhere, send it to the active
  screen only.
- Full help (`?`): four titled columns: screen keys, list keys, general keys, `:` commands.
  Render the titles with lipgloss, because bubbles' help has none. It must still fit at
  100×30.
- Footer: keep `shortFooter`'s rule (the global part always fits, the screen part is cut with
  `…`).

---

## 6. Phase D: TUI screens (one commit per screen)

For each screen, apply its spec §6.5 table: keys, scopes and `:` commands. Delete the old
keys. Replace every hardcoded key name in text ("Press a…", "press G…", "press S…", "H to
view") with the binding's `Help().Key`. Grep `[Pp]ress [A-Za-z]` and `[A-Z] to view`.

- **Dashboard:** remove `x` (quick fix), `c`, `O` and `R`, and delete `openReset`. `enter` on a
  problem sends the target screen `nav.Goto` plus a filter (Dotfiles `status!=ok`, Repos
  `state!=clean`, Ignore's Warnings view, …), or a row to select. Add the advice rows (info
  level, listed last, not counted in card status). The pass-store one sends
  `OpenCommandBarMsg{Prefill: "dotfile add ~/.password-store --dir --to
  ~/Workspace/secrets/dotfiles/password-store"}`. Remove the `Request*Msg` types that existed
  only for quick fix.
- **Repos:** `f p s r c a`, plus `:push`, `:root ls`, `:root rm`, `:ls --fetch`. `p`/`s` fetch, then
  open the diverged chooser (C3), then the checklist. `c` hands over to
  `ws repo configure <repos>`. Remove `P`, `S` and `x`.
- **Dotfiles:** `a` (marks: take the live files of the overwritten marked rows; no marks:
  prompt, pre-filled with the cursor row's path when it is overwritten), `l` (shown rows),
  `ctrl+d` unlink, `e` edit, `b` backup push, plus `:add`, `:link … --to …`,
  `:unlink --all`, `:backup enable|disable|status`. Remove the `class` column, and `x s p R
  G M v`. The old git view becomes the `:backup status` view.
- **Scratch:** `a`, `enter`, `t`, `ctrl+d`, `:add`. Remove `n o T F d`, and the search
  prompt.
- **Sessions** (rename the package `screens/logs` → `screens/sessions`): `enter` (viewer, `c`
  toggles commands), `r`, `p` (an age prompt, blank = cap; covers all rows, like the CLI), `ctrl+d`, `:stop`, `:grep`.
  Remove `s x F d P`.
- **Ignore:** three views, cycled by `v` (Warnings = lint, Muted = ls --muted, Ignored = ls),
  with keys per view per spec §6.5. `ctrl+d` in Warnings soft-deletes. `ctrl+d` in Ignored is
  `ignore rm`. Remove `a X c H t l E s(suppress) u(old)`.
- **Secrets:** `enter a i m s`, plus the `:pass …` and `:credential …` commands. Remove
  `l L e p D a(audit) g v o S C c X`. The health line gains the dim `ℹ no workspace copy`
  cue when it applies.
- **Cron:** `enter a ctrl+d r`, `:apply`. `r` runs `cron run <job>` (no `--if-due`) through a
  one-action checklist.
- **Trash:** `r` restore, `ctrl+d`, `p` (all rows, like the CLI), plus `:empty`, `:enable`, `:disable`, `:status`.
  Remove `e D x d`.
- `internal/tui/app/coverage.go`: give every spec §3.4 command a key, a `:` command, the
  screen list, or a reason. Add a `Cmd string` field for `:` commands, and support
  sub-nouns.

---

## 7. Phase E: tests that keep the grammar

Each must fail on today's code for the right reason. Check that before fixing the code.

| Test | Fails when |
| --- | --- |
| `TestListScreensShareListKeys` (update) | a screen binds a global key (spec §6.3) to another verb. Secrets' `a` (allow) is the one listed exception. |
| `TestNoShiftLetterActions` | any binding uses an uppercase letter other than `G` |
| `TestNoKeyTwiceOnAScreen` | a key appears in two bindings of one screen view, movement included (today: Secrets `g`, Dotfiles `G`) |
| `TestEveryListKeyDeclaresItsRows` (keep) | a list key has no scope. The exemption list changes `r` to `ctrl+r` (coverage_test.go:79-80) |
| `TestMarkingMovesNoRow` | after `space`, `/x`, `>` or `ctrl+a`, any row is drawn on a different line (compare frames) |
| `TestFooterFits` | at 100×30, a screen's own keys are cut, or the global part is |
| `TestNoKeyNamesInText` | a string literal outside `keys.go` and the tests names a key (`(?i)\bpress [a-z]\b`; scan the AST of `internal/tui/**`) |
| `TestCoverageKeysExist`, `TestCLICoverage` (update) | a command has no coverage, or the coverage names a key or `:` command its screen lacks |
| `TestCommandBarRoutes` | `:dot`, `:pass audit`, `:backup push` (on Dotfiles), `:q`, and a prefilled advice command don't reach the right place |
| `TestYEqualsCLIJSON` | a screen's `y` pane differs from `ws <noun> ls --json` for the same row |
| `TestDivergedAsks` | `repo pull`/`save` picks rebase or merge without being told, or fails to skip without a terminal |
| `TestWsJSONHasEveryKey` | `ws init` or any write leaves a key out |
| phase A tests | see A1–A5 |

Snapshots: every screen's golden changes. Rename `logs*.golden` to `sessions*.golden`. Add
goldens for:
- the bottom status line with marks, on each screen that has marks;
- the command bar, open with completions;
- the diverged chooser;
- the `y` JSON pane;
- the three Ignore views;
- a Dashboard advice row;
- full help.

`TestResize` and `TestResizeRestoresLayout` must pass for every new view, including the
command bar at 20×6.

## 8. Phase F: cleanup

- Delete the dead packages `internal/config`, `internal/manifest` and `internal/megaignore`.
  Nothing imports them; check with `grep -rn 'ws-tool/internal/<pkg>"'`. Leave
  `internal/provision`, `internal/search` and `internal/log` unless phase B leaves them
  unused.
- `dotfile migrate` stays hidden. Ask the user before deleting it.
- Remove the spec.md intro sentence about old names.
- Make sure every README command runs as written on a temp workspace.

## 9. Phase G: verification before you report done

1. `make fmt && make test` (Docker), all green. Paste the summary into your report.
2. `go vet ./...`, with the host env from rule 1.
3. **Fresh clone** (this has bitten the project before):
   `git clone . $HOME/tmp/ws-clone && cp Dockerfile.test $HOME/tmp/ws-clone/ && cd
   $HOME/tmp/ws-clone && touch .docker-image-stamp && mkdir -p tmp .gocache .gotmp && make
   test`. Then, in the original repo, `git status --short --ignored internal cmd` must show
   nothing new.
4. **CLI smoke run** on a throwaway workspace, never the real one:
   ```bash
   export T=$HOME/tmp/ws-smoke; rm -rf "$T"; mkdir -p "$T"
   export WS_WORKSPACE=$T/Workspace WS_CRONTAB_FILE=$T/crontab GIT_CONFIG_GLOBAL=$T/gitconfig \
          XDG_DATA_HOME=$T/data XDG_STATE_HOME=$T/state
   ./ws init --quiet && jq 'keys' "$WS_WORKSPACE/ws.json"        # every top-level key
   ./ws help; ./ws repo --help; ./ws repo root add --help         # per-command help
   ./ws dotfile ls --json | jq '.schema, .command'                # 3, "dotfile.ls"
   ./ws trash purge --older-than 30 --dry-run                      # prints the rename hint
   ./ws git-credential-helper erase </dev/null 2>&1 | wc -c        # 0: plumbing is silent
   ./ws completions bash >/dev/null 2>"$T/err"; wc -c <"$T/err"    # 0: silent alias
   ./ws ignore lint; echo "exit=$?"                                # 0 even with warnings
   ```
   Run every command in spec §3.4 at least once (`--dry-run` for RW commands), and every alias
   in §3.5.
5. **TUI walkthrough** at 100×30, then 80×24. Script it with tmux so the frames can be read
   (`tmux new-session -d -x 100 -y 30 './ws tui'`, `send-keys`, `capture-pane -p`). Use a
   workspace seeded with a broken and an overwritten dotfile link, a bloat file, a fake
   secret (`password=hunter2`), two git repos (one dirty, one diverged), a scratch dir and a
   trashed file. **Write the seeding script as `scripts/demo-workspace.sh`**, because the
   user wants it for the KubeCon demo. On each screen, check that:
   - the footer shows only lowercase screen keys and fits;
   - `space` marks a row, **no row moves**, and the status line at the bottom names the keys
     with labels;
   - `/`, `>`, `d`, `y`, `ctrl+a`, `ctrl+\` and `esc` behave as spec §6.3 says;
   - `:` opens the bar in the footer line, `tab` completes, `:dot` switches screens, and an
     unknown command shows a notice;
   - every screen key in spec §6.5 opens its prompt, chooser or checklist (then `esc`);
   - `s` on the diverged repo asks rebase or merge;
   - Dashboard `enter` opens the right screen, filtered, and the pass-store advice pre-fills
     the bar;
   - `ctrl+r` refreshes, and `r` does its screen's verb.
6. **Report:** the commits per phase, anything in the spec you couldn't build as written, the
   test summary, and two captured frames (Dotfiles with marks, and the command bar open).

## 10. Phase H: after shipping, on the user's machine (only with the user present)

The user asked for these to be done later. Don't do them yourself:

- Run `ws init` once, so `~/Workspace/ws.json` gets every key (values kept). Show the diff.
- Run `ws cron add mega-sync dotfile-backup`, so the live crontab lines get `--if-due` and
  `--workspace`, and `configs/crontab` matches.
- List the existing `ignore_rules.exclude` entries inside always-sync directories, which the
  new rule order activates. Confirm none are dotfile originals.
- Optionally, move the pass store into the workspace, per the tip.

## Appendix: code notes

| File | Covers |
| --- | --- |
| [0002/notes-repo.md](0002/notes-repo.md) | repo |
| [0002/notes-dotfile.md](0002/notes-dotfile.md) | dotfile, backup, bugs B1–B11 |
| [0002/notes-scratch-session.md](0002/notes-scratch-session.md) | scratch, session (log) |
| [0002/notes-ignore-secret.md](0002/notes-ignore-secret.md) | ignore, secret, pass, credential, decisions D1–D29 |
| [0002/notes-cron-trash-ws.md](0002/notes-cron-trash-ws.md) | cron, trash, init, config, completion, version, help |
