# Plan 0003: the `ws symlink` noun

**For:** the agent who changes the code. You start with no context, so read this whole
file, the ADR and the catalog before touching anything.

**Goal:** implement [ADR 0003](../adr/0003-symlink-noun-mirrored-storage-and-built-in-catalog.md).
The data for the catalog is in [0003/catalog.md](0003/catalog.md). When this plan and the ADR
disagree, the ADR wins. Report the disagreement instead of choosing silently. If something
cannot be built as written, stop and report it.

**Where things are:** `HANDBOOK.md` is the spec (it was `spec.md`; the user renamed it and
that rename is staged). The earlier command-grammar plan and its per-noun code notes were
deleted from the working tree but are in history: `git show HEAD:docs/plans/0002-command-grammar.md`
lists file and line locations for the earlier code. Their line numbers are old, so use them
for *where*, then read the code.

---

## 0. Ground rules (read first)

1. **Tests run in Docker only: `make test`.** Never run bare `go test` on the host. On
   2026-10-16 a host `go test ./internal/workspace` deleted the user's real
   `~/.local/state/ws-tool/trash-setup.json`. Some packages touch real HOME paths (crontab,
   XDG trash, `~/.bashrc`, git config). For a quick host build use
   `TMPDIR=$HOME/tmp GOCACHE=$HOME/.gocache GOTMPDIR=$HOME/gotmp go build ./...` and the same
   environment for `go vet ./...`. `/tmp` is mounted noexec on this machine. If Docker is not
   available, say so and rely on build, vet and the TUI exception below.
2. **TUI snapshots** are the one allowed host test run, because `internal/tui/...` tests
   isolate HOME: `UPDATE_SNAPSHOTS=1 TMPDIR=$HOME/tmp GOCACHE=$HOME/.gocache GOTMPDIR=$HOME/gotmp go test ./internal/tui/...`.
   Read every golden diff as if it were code.
3. **Never break the user's live machine.**
   - Do not replace `~/.local/bin/ws` and do not run `make install`.
   - The live crontab runs `ws cron run mega-sync`. Git calls
     `ws git-credential-helper get`. `~/.bashrc` runs `eval "$(ws completions bash)"`. All
     must keep working silently, including after your changes.
   - Never run a ws command against the real crontab, git config, `~/.bashrc`, trash, `$HOME`
     or workspace. Use a temp `--workspace` and temp `HOME`, `WS_CRONTAB_FILE`,
     `GIT_CONFIG_GLOBAL`, `XDG_DATA_HOME` and `XDG_STATE_HOME`, as the tests do. Detection
     reads `$HOME`, so every test and manual check sets `HOME` to a temp dir.
4. **The git index is not yours.** `git status` shows about 69 changes staged by the user
   (the `spec.md` to `HANDBOOK.md` rename, deleted `docs/`, `dev.md`, `PHILOSOPHY.md`, and edits
   in `cmd/` and `internal/`). Do not run `git add`, `git commit`, `git stash`, `git reset`
   or `git checkout`. Edit files in the working tree and leave committing to the user. At the
   end of each step, report which files you changed. The new files under `docs/` are
   untracked on purpose.
5. **New directories:** after adding one under `internal/` or `cmd/`, run
   `git status --short --ignored internal cmd`. The `.gitignore` is a large generic template
   and once silently dropped `internal/tui/env`.
6. **Every step leaves `go build ./...`, `go vet ./...` and `make test` green.** Do not start
   the next step on a red tree.
7. **Do not rewrite what you do not need to.** Rename, re-route, delete and add. Follow the
   conventions already in the code: `cmd/plan.go` action plans, `renderList`, the `resource`
   schema, `reslist.Action` tables for the TUI, global flags, exit codes, `--json` schema 3,
   and the prompt and `--yes` conventions (read `HANDBOOK.md` §4 first).
8. **Do not edit** the ADRs, `ws.todo` or anything under `~`. Docs are step 7.

## 1. Read first

| File | Why |
| --- | --- |
| `docs/adr/0003-…md`, `docs/plans/0003/catalog.md` | the decisions and the catalog data |
| `HANDBOOK.md` §2.2, §2.3, §3 (nouns, §3.4 commands, §3.5 renames), §4, the previous chapter of this noun in §5, §6 | the current spec you will update |
| the previous package of this noun under `internal/` | `Add`, `TakeLive`, `Relink`, `LinkTo`, `Unlink`, `Scan`, `classify.go`, `expand.go`, `sudo.go`, `fields.go` |
| the previous command file of this noun, `cmd/backup.go`, `cmd/aliases.go`, `cmd/commands.go`, `cmd/complete.go`, `cmd/help.go`, `cmd/plan.go`, `cmd/list.go`, `cmd/root.go` | dispatch, aliases, plans, lists |
| `internal/wsconfig/wsconfig.go` | `WsConfig`, `Link`, `Default`, schema and load |
| `internal/workspace/init.go`, `internal/ignore/defaults.go` | layout directories |
| `internal/cron/{cron,jobs}.go` | built-in jobs and presets |
| the previous TUI screen package, `internal/tui/{keys,nav,app,handover}`, `internal/tui/screens/dashboard/dashboard.go` | the screen, keys, routing, coverage, Dashboard |
| `cmd/pass.go`, `internal/secret/pass.go`, `internal/tui/screens/secrets/secrets.go` | pass uses `backup`, and prints a copy tip |

## 2. What changes, on one page

- **Noun:** `symlink`: verbs `ls`, `add`, `link`, `unlink`. The previous noun and its verbs
  are gone, with no aliases.
- **Storage:** `symlinks/home/<path under $HOME>` and `symlinks/system/<absolute path without
  the leading slash>`. One function, `MirrorPath`, computes it.
- **`ws.json`:** key `symlinks` replaces `links`. The backup settings key and `class` are gone, and
  old keys are rejected as unknown fields.
  Every entry is written with `~` for `$HOME`.
- **Catalog and detection:** `internal/symlink/catalog.go` and `detect.go`. Statuses: `ok`,
  `found`, `untracked`, `orphan`, `missing`, `broken`, `drifted`.
- **Removed:** backup (CLI, TUI, cron, config, internal files), `migrate`, `edit`, `--class`,
  `--to`.
- **Surfaces:** CLI `ls`/`add` (checklist), TUI screen, Dashboard line, `ws init` layout,
  layout directory, shell completion, help, docs.

## 3. The new CLI (this is the whole surface)

```
ws symlink ls [--filter k=v]… [--sort f] [--wide] [--json]
ws symlink add                              checklist of found items (needs a terminal)
ws symlink add <host-path> [<ws-path>]      move into the workspace and link back
ws symlink add --app <name>                 everything found for one app
ws symlink add <folder> [--dir | --files]   folder as one entry, or one entry per file
ws symlink link [<host-path>…]              create or repair links from ws.json
ws symlink unlink <host-path>… | --all      restore real content, remove the entry
```

Global flags are unchanged (`--dry-run`, `--json`, `--quiet`, `--workspace`, …). `--dry-run` prints
the plan and changes nothing, as it does today. `--class` and `--to` typed: exit 1 with
`--class/--to were removed: pass the workspace path as a second argument`. `symlink edit`, `symlink
backup`, `symlink git`, `symlink migrate` typed: unknown subcommand, with a one-line hint.

### `add <host-path> [<ws-path>]`

`<host-path>` accepts `~`. `<ws-path>` is workspace-relative or absolute and must be inside the
workspace, not inside `.git`, and not already an entry's original. Default `<ws-path>` is
`MirrorPath(host)`. Behaviour is decided by what exists:

| Host path | `<ws-path>` | Result |
| --- | --- | --- |
| real file or folder | free | Move into the workspace, link back, register. The plan line shows both paths before anything moves. |
| real, and already an entry (`drifted`) | the entry's own | Take the live content: it replaces the workspace copy (the old copy goes to the trash), link again. Keeps today's `TakeLive` rules. |
| real | exists, not the entry's own | Error: the destination exists. Exit 1. Say to remove it or choose another path. |
| link into the workspace, not in `ws.json` (`untracked`) | none, or the link's target | Register with the link's target as the original. Move nothing. |
| link into the workspace | differs from the target | Error. |
| link pointing outside the workspace | any | Error: the path is a link to somewhere else. |
| missing | exists | Link out and register (the old `link <path> --to <ws-path>`). |
| missing | missing | Error: nothing to add. |

Sudo is detected as today (`sudo.go`: the path is outside `$HOME`, or cannot be written).
Folder rules: a catalog folder (a catalog entry with `kind=folder` whose path equals the host
path) is always one entry. An unknown folder keeps today's behaviour (a per-file pick list).
`--dir` forces one entry. `--files` takes every file in the folder as its own entry without
asking. A path on the never-suggested list can still be added by hand: the list governs
suggestions, not what the user may do.

### `add` with no arguments

Needs a terminal. With no terminal it prints the found list (as `ls --filter status=found`)
and exits 0, changing nothing. It shows only what `Detect` found, grouped by app:

```
Found on this machine, not symlinked yet:
  [x] 1  git       ~/.gitconfig, ~/.gitignore_global
  [x] 2  vscode    settings.json, keybindings.json, snippets/ (3 items)
  [ ] 3  ssh       ~/.ssh/id_rsa + .pub                         🔑 secret
  [ ] 4  hosts     /etc/hosts                                    🔒 sudo
Stored under symlinks/ at the same path as on this machine.
Enter = checked ones · all · none · 1,3
```

One row per app for its non-secret, non-sudo items (checked). Every secret or sudo item is its
own row (unchecked). A private key and its `.pub` are one row. Answer grammar is exactly
`Enter`, `all`, `none`, or a comma list of numbers. Anything else re-asks. `q` or ctrl-c
cancels with exit 130. After the answer, show the same per-item plan lines as `add <path>` and
run them. `--app <name>` skips the question and adds every non-secret, non-sudo item found
for that app, and it exits 1 if the app has nothing found.

### `link [<host-path>…]`

Workspace copy wins. With no paths: every entry whose status is `missing`, `broken` or
(wrong target) that can be repaired; skip `drifted` (a real file stands there) and print
`drifted: ws symlink add <path> keeps the live file`. With paths: those entries only. A path
with no entry whose `MirrorPath` exists in the workspace (`orphan`) is registered and linked.
Sudo entries link through the existing sudo path and are skipped, with a message, when sudo
is unavailable. Linking never overwrites a real file.

### `unlink <host-path>… | --all`

Exactly today's `Unlink`/`reset` behaviour: restore the real content at the host path, remove
the entry, and keep the workspace copy only when shared (not ws-owned). `ws-owned` now means
the original is under `symlinks/`, the only owned root.

### `ls`

Default rows: every entry in `ws.json` plus any entry in a problem status (`untracked`,
`orphan`, `broken`, `missing`, `drifted`). `found` rows are not shown by default: `ls` ends
with the footer `N known configs found, not symlinked: ws symlink add`, written only when
N > 0 and when not `--json` or `--quiet`. `--filter status=found` shows them. Fields:
`system`, `original`, `workspace_path`, `app` (catalog label, blank for custom), `status`,
`message`, `dir`, `sudo`. Remove `class` and the secret field. `ls` runs the `$HOME` walk.
Exit 0 whatever it finds. `--json` is schema 3, command `symlink.ls`.

## 4. Behaviour of detection (`internal/symlink`)

`Detect(opts)` is a pure function of its inputs (the filesystem, `HOME`, the `ws.json` entries,
the workspace path, and the GOOS), keeps no state and writes nothing. It returns one record per
entry with `Status`, `App`, `Secret`, `Sudo`, `Kind` and `Message`. `opts.Walk` turns the
`$HOME` walk on. The Dashboard calls it without the walk (about 130 `lstat` calls), `ls` with it.

1. **Registered entries.** For each `ws.json` entry (expand `~`):
   - host is a link whose resolved target is the entry's workspace original and the original
     exists: `ok`.
   - host is a link, its target is wrong or missing: `broken`.
   - host missing: `missing`.
   - host is a real file or folder: `drifted`.
2. **Catalog items.** For each catalog row matching `runtime.GOOS` (`any` matches), expand
   `~` and globs. An item is `found` when its host path exists as a real file or folder (not
   a link), is not already an entry's system path, and is not inside a folder entry. A host
   path that is a link to somewhere else outside the workspace is skipped silently. Glob
   `~/.ssh/id_*` excludes `*.pub` and `*.ws-backup`. Pairs: a private key and its `.pub` share
   one record group so the checklist can show them as one row.
3. **Untracked (only when `Walk`).** Walk `$HOME` to depth 4, never following links, skipping
   `.cache`, `.git`, `node_modules`, `.local/share`, `.npm`, `.cargo/registry`, `.rustup`,
   `.var`, `snap`, `.Trash`, `.local/share/Trash`, and any folder that is itself a mount of the
   workspace. Catalog host paths are also checked directly regardless of depth, and so are
   the catalog's `/etc` files. A link whose resolved target is inside the workspace and is
   not an entry's system path is `untracked`. Stop after 200,000 visited nodes and set a
   note on the result; never hang.
4. **Orphans.** Walk `symlinks/`. A file whose path is not equal to or inside any entry's
   original, and whose host path (reverse of `MirrorPath`) has no entry, is `orphan`. A node
   that matches the mirror of a catalog folder is one orphan and is not descended into.
5. **Never-suggested list.** Every catalog row is checked against the list in `catalog.md` by
   a test, so `Detect` needs no runtime check for suggestions.

`MirrorPath(host string, home string) string` rules: a path inside `home` becomes
`symlinks/home/<relative>`; any other absolute path becomes `symlinks/system/<path without
leading slash>`. Clean the path first. Reverse is `HostPath(original, home)`. Paths with spaces
and a trailing slash must round-trip. Reject a path with `..` after cleaning.

## 5. Steps

Do them in order. Each ends green (rule 6) and with a short report to the user: files changed,
tests added, anything the plan did not cover.

### Step 1: remove what is going away (shrink first)

1. Delete the git backup code of the previous noun (`git.go`, `autosync.go`, their tests) and
   every auto-sync call in its command file.
2. Remove the backup, git, migrate and edit verbs and their flags from the command file,
   `commands.go`, `help.go`, `complete.go`, `aliases.go`.
3. `cmd/backup.go` was a runner shared with pass. Keep it for `pass backup` only:
   drop `noun`, `hasAutos`, `AutoCommit`, `AutoPush` and the other noun's `backupSpec`, keep the
   pass spec. `internal/backup` and `internal/repo/visibility.go` stay untouched. Rewrite any
   wording that compares pass backup to the removed backup in code comments and help.
4. `internal/wsconfig`: delete the backup settings types. A `ws.json` that still has their
   key is rejected as an unknown field (`Validate`). Add a test with an old file that
   contains a `links` key.
5. Cron: remove the backup job and its preset entry from `internal/cron`.
6. TUI: remove `b` and `:backup` from the previous screen and its key table, and from help.
7. Remove `--class` and `--to` from `add` and `link` (typed: the error in §3). Keep the internals
   until step 4; only the flags go now.
8. Update tests that referenced any of this. Do not weaken a test: delete it only when the
   feature is gone, and say which.

### Step 2: rename the noun, the config key and the layout

1. Rename the previous package under `internal/` to `internal/symlink`, the TUI screen
   package to `symlinks`, its `nav` screen constant to `ScreenSymlinks`, its command files
   to `cmd/symlink*.go`, resource ids, plan ids (`symlink-add-…`), JSON
   commands (`symlink.ls`, …), cron and Dashboard strings, and `style.go` mentions.
2. `cmd/aliases.go`: no alias is kept for the previous noun. Its words are unknown commands.
3. `wsconfig`: rename `Links []Link` (`json:"links"`) to `Symlinks []Link` (`json:"symlinks"`).
   A file with a `links` key is rejected as an unknown field. Drop `class` handling. `Link.System` is stored with `~` for
   paths inside `$HOME` and expanded on read (`wsconfig` helper `ExpandHome` or the existing
   one). Absolute entries still load. Keep `original`, `dir` and `sudo` keys as they are.
4. Layout: `ws init` creates `symlinks/` (keep `configs/` and `secrets/` themselves). Add `symlinks` to the
   layout. **Sync rule (decided by the owner):** the built-in `-:.*` exclude would drop every
   dot-file under `symlinks/` (`.gitconfig`, `.ssh/`, `.config/`), so `symlinks/**` is the one
   built-in included directory. The ignore rework removed the other built-in includes, and
   `HANDBOOK.md` says "None are built in", so read the current `internal/ignore` code first.
   Add a single fixed include rule `+g:symlinks/**` in the same position as the user's
   `ignore_rules.include` rules (after the built-in excludes, before the user's ignored paths),
   so a user's ignored path still wins. It is one constant, not a list. It is not stored in
   `ws.json` and the user cannot suppress it. Every consumer of the rules must see it:
   `.megaignore` generation, `ignore check`, `ignore tree`, `ignore lint` (warnings under it
   are muted, like any included directory) and `secret ls`. Add tests: `ignore check
   symlinks/home/.gitconfig` says it syncs and names the rule, and a user `ignore add` inside
   `symlinks/` still excludes. Amend `HANDBOOK.md` (the "None are built in" sentence and the
   rules table) to say `symlinks/**` is the only built-in one. Update every test that lists the
   layout or the include rules.
5. Ownership: `ownedRoots` is `symlinks` alone.
6. Replace the copy-tip in `cmd/pass.go` (and its mirror in the secrets TUI screen) with
   `ws symlink add ~/.password-store --dir`. `MirrorPath` makes that land in
   `symlinks/home/.password-store`. Keep the explanation that pass syncs through its own
   remote and that a copy in the workspace is a second sync path.
7. Add `MirrorPath` and `HostPath` with table tests (§4).

### Step 3: catalog and detection

1. `internal/symlink/catalog.go`: the Go slice from `catalog.md` (`App`, `Path`, `Kind`,
   `Secret`, `Sudo`, `OS`). One struct, one slice, no init-time file reads. Keep entries
   sorted by app, then path. `M`-confidence rows get a one-line comment.
2. `internal/symlink/detect.go`: `Detect` as described in §4, plus the `Status` constants.
   Take `fs`, `home`, `goos` through an options struct so tests use a temp `HOME` and can
   fake the OS.
3. Catalog tests: no duplicate path per OS; every path starts with `~/` or `/`; globs compile;
   no row equals, sits inside or is a parent of a never-suggested path (encode that list in the
   test, copy it from `catalog.md`); `~/.ssh/id_*` never matches a `.pub`; every secret row
   that is a file is not `Dir`; no folder row is one of the mixed folders; `/etc` rows have
   `Sudo`; every `os` is `any`, `linux` or `mac`.
4. Detect tests in a temp `HOME` and workspace, one case per status and per rule in §4,
   including: a link elsewhere is silent; a catalog folder found once; a file inside a folder
   entry is not `found`; a private key and its `.pub` pair; the walk skips `.cache`; the walk
   cap; an orphan file; reverse mapping with spaces.

### Step 4: `add`, `link`, `unlink` on the new rules

1. Rewrite `Add`, `TakeLive`, `LinkTo`, `Relink`, `Unlink` into `Add`, `Link`, `Unlink` per
   §3. Keep the move-and-link mechanics, trash handling, rollback and sudo logic that exist
   today, and their tests. Remove the `class` classification (`classify.go`) and storage-path
   guessing; `MirrorPath` replaces them. Delete `ConfigsRoot` and `SecretsRoot` use for new
   entries.
2. Implement the case table in §3. One test per row, plus `--dry-run` (nothing changes) and
   rollback on a failed move.
3. `add --dir`, `--files`, catalog folder rule, and the unknown-folder pick list.
4. **Secret scan check.** Put a private key under `symlinks/home/.ssh/` in a temp workspace
   and run `secret ls`. If it flags the key (or any file the catalog lists as secret), stop
   and report to the user with the finding and the two options (skip `symlinks/` in
   `secret ls` by default, or leave it flagged and let `secret allow` handle it). Do not choose.
   Continue with the other steps while you wait; the answer only changes defaults.
5. Update `cmd` tests (the symlink command tests, the ignore restore test, others) to the
   new words and behaviour.

### Step 5: the CLI surface

1. `ls`: statuses, the fields in §3, the footer, `--filter status=found`, `--sort`, `--wide`.
   Register the schema in `internal/resource` so the TUI and `ls` share it.
2. `add` with no arguments (the checklist and its answer grammar), `--app`, non-terminal
   behaviour, cancel exit 130.
3. `link` repair rules and orphan registration.
4. Shell completion (`complete.go`): the noun, verbs, flags, host paths, and `--app` values
   from the catalog's app labels. Help text for the noun and each verb, 80 columns, with
   the examples in §3.
5. Tests: checklist grammar (valid, invalid re-ask, cancel), grouping and unchecked rows,
   `--app`, no-terminal output, `ls` default rows and footer, `--json` shape, completion.

### Step 6: TUI and Dashboard

1. The "Symlinks" screen shows exactly the rows `ls` prints (the parity tests in
   `cmd/parity_test.go` and `internal/tui/app/coverage.go` enforce this; keep them green).
2. `a` with nothing marked opens the same checklist as `add` with no arguments, using the
   existing dialog or checklist components. With marks, `a` keeps today's meaning for the
   marked rows. Remove the screen's `e` edit action. Per-row keys follow the
   verbs: `a` add, `l` link, `u` unlink; check `keys.go` for conflicts and follow the
   existing rule (lowercase, named after the verb).
3. Dashboard: area text `N symlinks managed`; a problem line for broken, missing or drifted
   entries; and an info line `N known configs found, not symlinked` when N > 0 (no `$HOME`
   walk on the Dashboard). Sort into the Dashboard's existing severity order.
4. `:` command bar words follow the CLI (`:add <host> [<ws>]`, `:link`, `:unlink`).
5. Regenerate the golden snapshots (rule 2) and read every diff.

### Step 7: docs

Update `HANDBOOK.md` and `README.md` to the new design. Remove every statement that describes
backup, auto-commit, `migrate`, `edit`, `--class`, `--to`, or the `configs/secrets` split for
links. At least:

- §2.2 layout: add `symlinks/` with `home/` and `system/`, and drop the "also the backup git
  repo" note.
- §2.3 and the `ws.json` key tables: `symlinks` (with the entry fields). Say unknown keys,
  such as `links`, are rejected.
- §3: nouns, command list, and the removed list.
- §5: replace the previous chapter with a `symlink` chapter written from §3 and §4 of this
  plan (mirror path, statuses, case table, checklist, discovery, never-suggested list).
- §6 TUI: screen, keys and Dashboard lines. The cron chapter: the job list and presets.
- The pass chapter: rewrite any comparison with the removed backup to describe `pass backup` on its own, and the
  copy-tip. The ignore chapter: the include rules.
- `README.md`: the command rows and the layout tree.
- Search for leftovers of the previous noun and its storage folders in `HANDBOOK.md`,
  `README.md`, `cmd`, `internal`, `scripts` and `.github`: none may remain. Also check
  `scripts/` (the demo) for old commands.

### Step 8: final checks

1. `go build ./...`, `go vet ./...`, `gofmt -l .` (empty), `make test` green.
2. In a fresh temp workspace and temp `HOME` that holds a `.gitconfig`, a `.ssh/id_rsa` and a
   `.ssh/id_rsa.pub`, a `.config/Code/User/settings.json` and an `/etc`-style file you can
   fake via the options struct, run through the whole flow with the built binary:
   `ws init`, `ws symlink ls` (footer), `ws symlink add` (answer `Enter`), `ws symlink ls`,
   delete a host link then `ws symlink link`, replace a link with a real file then
   `ws symlink add <path>`, make an untracked link by hand then `add` it, `ws symlink unlink --all`.
   Paste the transcript in the report.
3. Verify a `ws.json` with a `links` key is rejected with an `unknown field` error.
4. Verify `ws completions bash` still works.
5. Final report: what changed per step, files touched, tests added, every place you deviated
   from this plan and why, the secret-scan finding from step 4, and anything left undone.
