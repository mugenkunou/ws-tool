> **Read this first.** These notes were written on 2026-10-22, before the user's last round of
> decisions. Where they disagree with spec.md, **spec.md wins**. Use them for file:line
> locations and today's behaviour. Changed since:
>
> - Jobs: `repo-sync` is retired too, and `pass-backup` is new. The `sync` preset is mega-sync, dotfile-backup, pass-backup.
> - `init` writes the **complete** `ws.json`, and **fills in the missing keys** of an existing one, keeping its values. That reverses the notes' "never touches an existing ws.json". `ws.json` always has every key (spec §2.3).
> - Config in the TUI is `:config show` / `:config edit`, from any screen. There's no Dashboard `c`/`e`.
> - Editor rule: `ws.json` `editor` → `$VISUAL` → `$EDITOR` → `vim`.
> - The setup screen key is `enter`.

# Implementation notes: cron, trash, workspace commands

Spec chapter: spec.md. Repo:
`/home/siva-14414/Workspace/experiments/ws-tool`. All paths below are relative to the repo.
Do not run `go test` on the host. Use Docker (see memory "Tests in Docker only"). The trash
tests have deleted real trash state before.

## Decisions made in the spec (each is marked `(decision)` there)

Cron
1. Old job names (`dotfile-sync`, `log-prune`) are aliases. Their crontab blocks, `cron.state`
   records and `cron.log` lines count as the new job's, so last run, missed-run checks and the
   log carry over.
2. Retired jobs (`secret-scan`, `ignore-scan`): `cron add` rejects them, and `cron rm` removes
   their blocks. `cron run` logs a skip line and exits 0, so old crontab lines don't fail. `cron ls`
   shows a `retired` row while a block remains. The `maintenance` preset stays, with only
   `session-prune`.
3. `cron run <job>` always runs. The new flag `--if-due` does the half-interval check, and
   `cron add` writes `--if-due` on both crontab lines. Today every run checks, so a manual run or
   the TUI's `r` could silently skip.
4. Crontab lines carry `--workspace <abs>`, because cron has no `$WS_WORKSPACE`. The `mega-sync`
   lines also carry `--display <d>`. Today `cron add --display` is parsed and then ignored.
5. `cron add` and `cron rm` write `configs/crontab` as well as the live crontab, with one plan
   action per job that covers both files. This fixes the known bug.
6. `cron apply` regenerates the blocks of known jobs for this machine instead of copying their
   text. It removes undeclared ws blocks, adds the missing declared non-ws lines once, skips
   retired or unknown blocks, and runs as a plan.
7. `cron ls` `state` is one of `crontab n/a`, `retired`, `outdated`, `undeclared`, `not applied`,
   `installed`, `not installed`, or "n of m installed" for presets. The JSON has `installed` and
   `declared` bools. `next_run` is filled only for installed jobs, and its text is `overdue` when
   past. The default columns gain `next_run`. Rows come jobs first, then presets, then retired.
8. `cron run` captures the job's output into `cron.log` (and tees it to stdout and stderr unless
   `--json` is given). When the log is over 5 MB, it trims it to the last 2.5 MB.
9. `cron run` exits 0 or 1. The job's real code goes to `cron.state` and to the JSON
   `exit_code`.
10. `cron log [job…]`: the job is optional (none means all), several are allowed, presets expand,
    and `--lines 0` shows all.
11. `cron.ls` JSON `data` is the plain record array, with the `jobs` wrapper dropped.
12. `cron add` and `cron rm` need at least one target.

Trash
13. The trash root is always `$XDG_DATA_HOME/Trash` (default `~/.local/share/Trash`).
    `--root-dir` is removed from every trash command, and `trash-setup.json` is no longer read.
14. An `<item>` argument is a name, or an original path (it contains `/` or starts with `~`; the
    newest match wins). A target that matches nothing aborts the whole command with exit 1.
15. `restore` never overwrites (it checks with `Lstat`) and creates missing parents with mode
    0755. It resolves a relative `Path=` against the trash's parent dir, and skips items without
    a `Path=`. When two targets share a destination, the first wins. Exit is 0, 3 or 1.
16. `restore` and `rm` need at least one target. The TUI's `:empty` ignores marks and the filter.
17. `prune`: `--older-than` is required and must be positive. There is one action per item.
    Items without a `DeletionDate` are never pruned.
18. Integrations are checked against the live system:
    - `shell-rm`: the script plus a guarded alias line
      `command -v ws-trash-rm >/dev/null 2>&1 && alias rm='ws-trash-rm'`. The line goes into each
      existing file among `~/.bashrc` and `~/.zshrc`, or into the `$SHELL` rc file, created, if
      neither exists.
    - `vscode`: on when `files.enableTrash` is not `false`. `enable` flips an explicit `false`
      with a text edit.
    - `file-explorer`: on when `files/` and `info/` exist. `enable` creates them with mode 0700.
    - `disable` removes only `shell-rm`, plus the legacy state file.
19. No trash command needs an initialized workspace.
20. `trash status` always exits 0 (exit 2 is retired). It has a new flat JSON shape.

Workspace
21. `init` is idempotent. It creates only what is missing and never touches an existing `ws.json`.
    It generates `.megaignore` from `ws.json`'s ignore rules, and writes `ws.json` at the resolved
    config path (`--config`, else `$WS_CONFIG`). It runs as a plan with at most three actions.
22. `config show` adds `config` (the absolute path of `ws.json`) and resolves `links[].original`
    to an absolute path. `--defaults` keeps the stored form and needs no workspace.
23. `config edit` edits a copy (`.ws.json.edit`) and validates it after the editor exits: JSON, the
    schema range, and no unknown keys except legacy keys ws still reads. It writes only when the
    copy is valid, changed, and `ws.json` was not changed meanwhile. It keeps the copy on failure,
    regenerates `.megaignore` when the ignore rules changed, and takes `--editor`.
24. `completion enable` rewrites a block that holds the old `ws completions …` line.
    `completions bash|zsh|fish` stays a silent alias, because rc files call it.
    `completions install|uninstall` print the stderr line.
25. `ws help <noun>` prints that noun's help.
26. `version` JSON: `data.schema` becomes `data.config_schema`.
27. `ws reset` and `ws restore` print a one-line pointer and exit 1.
28. The global `--manifest` flag is removed.
29. TUI: Dashboard `c` is `config show` and `e` is `config edit`; `O` and `R` are removed. Cron `r`
    confirms with a one-action checklist, then runs in the background.

## Where the matrix and the code disagree (also in the hand-off)

- Matrix `cron log <job>` reads as a required target. The code and rule 4 make it optional (kept optional).
- Matrix `trash enable [flags]` and the old `--root-dir` contradict "trash is the XDG trash". Today
  `cmd/trash.go` hard-codes `~/.local/share/Trash` (it ignores `$XDG_DATA_HOME`), while
  `internal/trash.SoftDelete` honours `$XDG_DATA_HOME`. With that variable set, `ws trash ls`
  reads a different directory from the one ws deletes into.
- The matrix's `cron run` is a k9s-style trigger ("execute now"), but today's `cron run` skips
  within half an interval. Hence `--if-due`.
- The glossary says `rm` soft-deletes. `trash rm` is necessarily permanent. The spec says so.
- `trash disable` today removes only `trash-setup.json`. Its descriptions claim it removes the
  script, the aliases and the symlink (`internal/trash/reset.go:Reset`). `ws-trash-rm` and
  `alias rm=` stay after "disable".
- The VS Code "integration" is a no-op today. `Setup` records `vscode_delete: true`, and `GetStatus`
  echoes it back.
- The TUI prints `~`-relative paths: the Trash header (`m.env.Rel(st.RootDir)`) and the
  "Deleted from" column via `v.Path`. Golden `trash.golden` shows `~/.Trash` and
  `Notes/old-notes.md`. That conflicts with "every path ws prints is absolute". This is
  cross-cutting, and the TUI owner should decide.
- `keys.Table().HalfPageDown` is `ctrl+d` (`internal/tui/keys/keys.go:95`). It conflicts with
  `ctrl+d` delete, which needs the global key change (ctrl+b/ctrl+f).
- Refresh is `r` on every screen today (`keys.go:refresh()`, line 749). The new Cron `r` (run) and
  Trash `r` (restore) need refresh moved to `ctrl+r` first.

---

## cron

### Today

- `cmd/cron.go`: `cronHelp` (the usage lists `remove`, `list`, `status`), `runCron` (dispatch),
  `runCronAdd` (first arg only; `--display` is parsed and then ignored by `AddActions`),
  `runCronRm`, `runCronLs` (`renderListIn(... "jobs" ...)`), `runCronStatus` (its own text and JSON
  `cron.status`), `runCronRun` (returns the job's exit code), `runCronApply` (no plan, no prompt),
  `runCronLog`.
- `internal/cron/cron.go`: `Builtins`, `Presets` (`maintenance` = ignore-scan + log-prune), `AllNames`,
  `Resolve`, `DataDir`/`StateFilePath`/`LogFilePath` (all under `wsconfig.StateDir()`; the package
  comment still says `~/.local/share/ws-tool`), `JobsDir`/`ScriptPath` (legacy wrapper scripts).
- `internal/cron/jobs.go`: the six job definitions. `dotfileSyncJob` runs `dotfile git push --quiet`
  and `logPruneJob` runs `log prune --quiet`.
- `internal/cron/crontab.go`: `CronMarkerStart`/`End`, `ReadCrontab`/`WriteCrontab` (with the
  `WS_CRONTAB_FILE` test override), `HasJob`, and `AddJob`, which writes
  `<sched> <bin> cron run <name>` plus `@reboot …` with no `--workspace`, no `--if-due` and no
  `--display`. Also `RemoveJob` and `removeBlock`.
- `internal/cron/actions.go`: `WSBinary`, `DefaultDisplay`, `AddActions` (ignores `display` and
  `workspacePath`), `RemoveActions`.
- `internal/cron/run.go`: `RunJob` (unconditional half-interval skip; `runBash` wires
  `os.Stdin/Stdout/Stderr`, so job output never reaches `cron.log`), `logLine`, `appendState`.
- `internal/cron/apply.go`: `ApplyCrontab`, `parseWorkspaceCrontab`, `extractNonWSLines`,
  `extractWSBlocks`. Marker detection loops over `AllNames()`, so blocks for unknown or removed
  names count as user lines. Non-ws lines from the file are appended on every run, so repeated
  applies duplicate them. Blank lines are dropped from the live crontab.
- `internal/cron/preflight.go`: `Preflight` runs a real `crontab -l` even when `WS_CRONTAB_FILE` is set.
- `internal/cron/state.go`: `ReadState`, `LastRun`, `ReadLog` (filters on `[<job>]`).
- `internal/cron/fields.go`: `Job`, `Jobs`, `presetInstalled`, `Schema` (the key `installed`
  carries the state text).
- `internal/cron/script.go`: `GenerateScript`. It is dead code, used only by tests.
- The live crontab on this machine (read-only check):
  `/home/siva-14414/.local/bin/ws cron run mega-sync` and `… cron run dotfile-sync`, both
  `*/30` plus `@reboot`, inside `ws:mega-sync` and `ws:dotfile-sync` blocks. There is no
  `/home/siva-14414/Workspace/configs/crontab`. `cron.log` shows `[dotfile-sync] Failed (exit 1)`
  with no reason, because the output is not captured.

### Changes

Job registry (`internal/cron/cron.go`, `jobs.go`)
- Rename `dotfileSyncJob` to `dotfile-backup`. Its body becomes
  `<ws> --workspace <W> dotfile backup push --quiet`, the new name from the dotfile chapter. Calling
  the old `dotfile git push` would print the alias stderr line into every log.
- Rename `logPruneJob` to `session-prune`. Its body becomes `<ws> --workspace <W> session prune --quiet`.
- Delete `secretScanJob` and `ignoreScanJob`. Add `var Retired = []string{"secret-scan", "ignore-scan"}`.
- Add `var Aliases = map[string]string{"dotfile-sync": "dotfile-backup", "log-prune": "session-prune"}`.
  `Resolve(name)` returns the canonical job plus a flag saying whether an alias was used, so
  `cmd` can print `ws: "dotfile-sync" is now "dotfile-backup"`. `run` stays silent.
- Add `Names(job)`, which returns the canonical name plus its old names. `LastRun` and `ReadLog`
  match any of them.
- `Presets`: `sync` = mega-sync, dotfile-backup, repo-sync; `maintenance` = session-prune.
- Delete `script.go` (`GenerateScript`) and its tests. Keep `ScriptPath`/`JobsDir` only for the
  legacy-script cleanup in `rm`.

Crontab blocks (`crontab.go`, `apply.go`)
- One generic block parser: marker regexp `^# >>> ws:([a-z0-9-]+) >>>$`, used by `HasJob`,
  `removeBlock`, `extractNonWSLines`, `extractWSBlocks` and `parseWorkspaceCrontab`. Never
  loop over `AllNames()` again.
- `BlockFor(job, bin, workspace, display)` builds the text from the spec. It is shared by `add`
  and `apply`. Both lines read `<bin> --workspace <W> cron run --if-due [--display <d>] <job>`.
- `Outdated(blockName, blockText)`: the name is an alias, or a line lacks `cron run --if-due`.
  Ignore differences in the binary and workspace paths. `/home/siva-14414/tmp/wsux/ws` builds
  must not mark everything outdated.
- Make all reads and writes go through one function per file. The live crontab uses `ReadCrontab`
  and `WriteCrontab`, and the declared file uses `ReadDeclared(path)`/`WriteDeclared(path)`
  (atomic write, mode 0644, created with its parent).

`cron ls` (`fields.go`, `cmd/cron.go:runCronLs`)
- `Jobs(workspace string)` also reads `<workspace>/configs/crontab`. If no workspace resolves,
  every job reads as undeclared and the "undeclared" state is not used. The state is computed
  per the spec's table.
- Schema keys: `name`, `schedule`, `state`, `installed`, `declared`, `last_run`, `last_exit`,
  `next_run`, `description`, `preset`, `members`. Columns: `name`, `schedule`, `state`,
  `last_run`, `next_run`, `description`. `next_run` is empty when not installed, and its text is
  `overdue` when in the past.
- Retired rows: one per retired name that has a block in either file.
- `renderListIn(…, "jobs", …)` becomes `renderList` (plain array).
- Print the preflight error once on stderr when the crontab is unreadable. Exit 0.
- Delete `runCronStatus`. `status [job…]` becomes a hidden alias that prints the stderr line and
  runs `ls` with a `name` filter.

`cron add` / `cron rm` (`actions.go`, `cmd/cron.go`)
- Take all positional args (`fs.Args()`), not `remaining[0]`. Dedupe after preset expansion.
  No args: usage error, exit 1.
- `AddActions(jobs, bin, display, workspace, declaredPath)`: one action per job that is not
  `installed`. `Execute` removes any old-name block, then writes the block to the live crontab
  and to `configs/crontab`. The description names both files and "replacing <old>" when an old
  block exists.
- `RemoveActions(jobs, declaredPath)`: one action per job with a block in either file. `Execute`
  removes the canonical and old-name blocks from both files and deletes the legacy
  `cron-jobs/<name>.sh`.
- `rm` accepts `Retired` names (build a pseudo-job). `add` rejects them with
  `cron job "secret-scan" was removed`.
- Run `Preflight` for dry runs too, because the plan depends on the live crontab. Make
  `Preflight` return nil when `WS_CRONTAB_FILE` is set, so tests stay hermetic in Docker without a
  `crontab` binary.
- JSON: `cron.add` `{jobs, actions}`; `cron.rm` `{removed, actions}`.

`cron run` (`run.go`, `cmd/cron.go:runCronRun`)
- Add `RunOptions.IfDue bool`, `Stdout`/`Stderr io.Writer` and `DryRun`. The half-interval skip
  runs only when `IfDue` is set.
- Resolve the name: an alias runs silently as canonical. A retired name logs
  `Skipping: <name> is no longer a ws job; run ws cron rm <name>` and returns exit 0. A preset is
  an error.
- Capture output with `cmd.StdoutPipe`/`StderrPipe`. Prefix each line as `<ts> [<job>] <line>` in
  `cron.log`, and tee to the writers (`io.Discard` for `--json` and for the TUI). Do not connect
  `os.Stdin`.
- Before the first write, trim `cron.log` to its last 2.5 MB when it is over 5 MB (the same
  numbers as the old wrapper in `script.go`).
- `cmd`: return 0 or 1, not `result.ExitCode`. JSON `{job, exit_code, skipped}`.

`cron log` (`cmd/cron.go:runCronLog`, `state.go:ReadLog`)
- Accept `fs.Args()...`. Expand presets and aliases (an alias prints the stderr line). `ReadLog`
  takes a set of names. `--lines 0` means all. An unknown job exits 1.
- JSON `{jobs, lines}`.

`cron apply` (`apply.go`, `cmd/cron.go:runCronApply`)
- Rewrite `ApplyCrontab` into `ApplyActions(declaredPath, bin, workspace, display) ([]plan.Action, ApplyResult, error)`.
  - Declared block for a builtin or alias: install `BlockFor(...)` when the live block is missing,
    differs, or sits under an old name.
  - Live ws block not declared (any name): a remove action.
  - Declared non-ws lines missing from the live crontab (exact-line compare): one append action.
  - Declared retired or unknown blocks go into `Skipped`.
- Run it through `RunPlan` (today the command bypasses plans). Replace the string concatenation
  `workspacePath + "/configs/crontab"` (`cmd/cron.go:424`) with `filepath.Join`.
- Missing file: exit 1 with the hint from the spec (today's behavior, new message).
- JSON `{installed, removed, unchanged, skipped, lines_added, actions}`.

Help and completion
- `cronHelp.Usage`: `ws cron <ls|add|rm|run|log|apply>`. Hidden: `remove`, `list`, `status`.
  `parity_test.go:subcommands` reads this list, so aliases must not appear in it.
- `cmd/complete.go`: change `completers["cron"].subcommands` to `ls add rm run log apply`. Replace
  `cronJobNames()` (hard-coded, stale) with `cron.AllNames()`, which holds canonical jobs and
  presets only. `completeCron` offers names for `add rm run log`. In `commandFlags`, `cron add`
  gets `--display --dry-run`, `rm` and `apply` get `--dry-run`, `run` gets `--display --dry-run`
  (leave `--if-due` uncompleted), and `log` gets `--lines`.

### Tests and goldens (cron)

- `cmd/cron_test.go`:
  - `TestCronLs`: the expected names become mega-sync, dotfile-backup, repo-sync and
    session-prune. Assert that secret-scan and ignore-scan are absent.
  - `TestCronLsJSON`: `data` becomes an array.
  - `TestCronAddDryRun` and `TestCronAddPreflightAccessDenied` use `log-prune`. Switch them to
    `session-prune`, and add an alias case that asserts the stderr line.
  - `TestCronStatusNoJobs` and `TestCronStatusUnknownJob`: rewrite for the alias.
  - `TestCronLogJobFilter` is vacuous today. It writes `$HOME/.local/state/ws-tool/cron.log`, but
    `TestMain` sets `XDG_STATE_HOME`, so the file is never read, and the "no entries" message
    contains "mega-sync" anyway. Write the log under `$XDG_STATE_HOME/ws-tool/`.
  - `TestCronApplyDryRun`: expect plan output.
  - New tests:
    - add writes both files;
    - rm removes old-name blocks from both files;
    - apply is idempotent (run twice, no duplicate lines) and regenerates paths;
    - `run --if-due` skips and plain `run` runs;
    - `run dotfile-sync` is silent and records under `dotfile-backup`;
    - `run secret-scan` exits 0 and logs;
    - output is captured into `cron.log`;
    - `ls` states `outdated`, `undeclared` and `not applied`.
- `internal/cron/cron_test.go`: delete `TestGenerateScript_*`. Update the `TestResolve_preset`
  expectation (it follows `Presets`, so it is fine) and `TestAllBuiltins_*`. Add alias and retired
  tests, and a generic-marker test for `removeBlock`.
- `internal/cron/preflight_test.go`: add the `WS_CRONTAB_FILE` short-circuit.
- `internal/tui/app/testdata/cron.golden`: the footer (`enter log • a add • ctrl+d remove • r run …`)
  and the State column texts. The fixtures in `internal/tui/app/app_test.go:106-111`
  (`dotfile-push`, preset `all`) are fake and can stay. Set `Declared` so the rows read `installed`.

### Risks (cron)

- The live crontab calls `ws cron run mega-sync` and `ws cron run dotfile-sync` with no
  `--workspace` and no `--if-due`. Both must keep working silently. Until the user re-adds them,
  those lines run unconditionally: `@reboot` after a recent run will run again. That is harmless
  for these jobs, and `cron ls` shows `outdated`.
- `dotfile-sync` keeps failing (exit 1) until dotfile backup is set up. After the change, the
  reason appears in `cron.log`. Don't "fix" this by hiding errors.
- The job bodies call the other chapters' new names: `dotfile backup push`, `session prune`,
  `repo sync`. If those land later than cron, the alias stderr line goes into `cron.log`
  (cosmetic). If an alias is missing, the job fails.
- The TUI's `r` runs `mega-sync`, which takes 5 minutes (`sleep 300`). Quitting the TUI mid-run
  orphans the bash child, and no state record is written. Acceptable, but document it in the code.
- `promptChoice` returns the default `y` on EOF (`cmd/root.go:promptChoice`). Plans invoked from
  cron with no stdin auto-accept. That is fine today because jobs pass `--quiet`. Keep `--quiet` in
  every job body.

---

## trash

### Today

- `cmd/trash.go:runTrash` is one switch:
  - `enable|setup`: a single `trash-setup` action plus two no-op "record provisions" actions. The
    JSON calls `Setup` again in dry-run mode.
  - `disable`: JSON command `trash.reset`.
  - `empty`, `purge` (`fs.Int` days), `rm` (exactly one name, rejects `/`), `ls`.
  - `status` returns 2 on warnings (line 467).
  - All of them call `requireWorkspaceInitialized` first, though nothing reads `ws.json`.
    `cfgTrashRoot` is hard-coded `~/.local/share/Trash` (line 53), and so is the
    `trashWarnSizeMB` constant 1024.
  - Unused helpers: `boolWord`, `boolBadge`, `statusWord`. `formatAgeDays` and `pluralize` are
    used only here.
- `internal/trash/trash.go`:
  - `xdgTrashDir`, `SoftDelete` (falls back to `os.RemoveAll` when the trash dir can't be
    resolved, which is a silent permanent delete), `moveToTrash`, `uniqueTrashName`,
    `writeTrashInfo`, `renameOrMove`.
  - `Setup` (creates `files/` and `info/` with mode 0755 and writes `trash-setup.json`),
    `GetStatus` (state file AND live checks), `WarningCount`.
  - `Scan`, `Empty` (does not remove `directorysizes`).
  - `ensureShellRMIntegration`. It writes the script and the unguarded alias to both `~/.bashrc`
    and `~/.zshrc`, and `ensureLine` creates a missing `~/.zshrc`.
  - `shellRMConfigured`, `ensureFileExplorerIntegration`, `fileExplorerConfigured`.
- `internal/trash/items.go`: `Item`, `Items`, `readTrashInfo`, `Schema`.
- `internal/trash/purge.go`: `ListOlderThan`, `Purge`, `readDeletionDate`, `measure`.
- `internal/trash/reset.go:Reset` deletes only `trash-setup.json` (the bug above).
- `internal/trash/provisions.go`: no-op `RecordShellProvisions` and `RecordExplorerProvision`.
- This machine: `/home/siva-14414/.local/share/Trash` has `files/`, `info/` and `expunged/`.
  `/home/siva-14414/.local/state/ws-tool/trash-setup.json` exists. The alias sits in `~/.bashrc:159`
  and `~/.zshrc:5`, and the script is `~/.local/bin/ws-trash-rm`.

### Changes

- Root: add `trash.Root() (string, error)` (the exported `xdgTrashDir`). Remove every `rootHint`
  or `RootDir` parameter: `Items`, `ListOlderThan`, `Purge`, `Scan`, `Empty`, `Setup`,
  `GetStatus`. Delete `resolveRoot`, `expandPath` and `ErrXDGTrashDirExists`. Also delete the
  symlink branch of `ensureFileExplorerIntegration` (a custom root no longer exists). In
  `cmd/trash.go`, delete `cfgTrashRoot` and every `--root-dir` flag. In the TUI, delete the
  hard-coded `root()` in `internal/tui/screens/trash/trash.go:90`.
- Workspace guard: remove `requireWorkspaceInitialized` from `runTrash`.
- Item resolution: add `trash.Resolve(args []string, cwd string) ([]Item, error)` with the spec
  rules: a name, or an original path through the shared path resolver (`wsconfig.ExpandUserPath`),
  newest match first. It returns an error naming the unmatched args. Both `restore` and `rm` use
  it, and the TUI passes rows directly.
- `restore` (new): add `internal/trash/restore.go` with `RestorePlan(items) (actions []plan.Action, skipped []Skip)`.
  - Checks: no info or `Path=` means skip "no original path". `os.Lstat(dest)` succeeding means
    skip "exists". A destination already claimed earlier in the same call means skip "restored
    above".
  - Per action: `MkdirAll(parent, 0o755)` when missing, then `renameOrMove(files/<name>, dest)`,
    then remove `info/<name>.trashinfo`, then drop the item's line from `directorysizes`.
  - A relative `Path=` is joined to `filepath.Dir(root)`.
  - `cmd`: the `restore` case. Exit code: 0, 3 when some were skipped or failed, 1 when none was
    restored. JSON `{restored, skipped, actions}`.
- `rm`: take N args via `Resolve`, with one action per item. Share a `trash.Delete(item)` helper
  with the TUI (`internal/tui/screens/trash/trash.go:openDeleteItems` duplicates the logic). JSON
  `{removed, actions}`.
- `empty`: `Empty` also removes `directorysizes`. Keep `expunged/`. JSON `{items, files, size_bytes, actions}`.
- `prune`: rename `Purge`/`PurgeOptions`/`PurgeResult` to `Prune…` and `purge.go` to `prune.go`.
  `--older-than` takes the shared age parser (see cross-cutting). Build one action per matched
  item, oldest first. JSON `{matched, kept, skipped, size_bytes, actions}`. `purge` becomes a
  hidden alias that accepts the old int days (the new parser accepts a bare int anyway).
- `enable`: replace `Setup` with `EnableActions(opts) []plan.Action`, one action per integration
  that is off.
  - shell-rm: the script (unchanged body) plus the guarded alias line. Add it to each existing
    `~/.bashrc` and `~/.zshrc`. Create the `$SHELL` rc file only if neither exists.
  - vscode: the settings path is `$XDG_CONFIG_HOME/Code/User/settings.json` (default
    `~/.config/Code/User/settings.json`). If it contains `"files.enableTrash": false`, replace that
    literal by regexp, without a JSON re-encode, because the file is JSONC with comments. A
    missing file or key means the integration is on and no action is needed.
  - file-explorer: `MkdirAll(files|info, 0o700)`.
  - Stop writing `trash-setup.json`. Delete `provisions.go` and the two no-op actions.
- `disable`: replace `reset.go:Reset` with `DisableActions() []plan.Action`.
  - Action 1 (when shell-rm is on): remove the exact unguarded and guarded alias lines from
    `~/.bashrc` and `~/.zshrc`, then delete `~/.local/bin/ws-trash-rm`.
  - Action 2 (when the file exists): delete `trash-setup.json`.
  - JSON command `trash.disable`.
- `status`: `GetStatus()` returns `{Root, Integrations []Integration{Name, Enabled, Detail}, Items, Files, SizeBytes, WarnSizeBytes, OverLimit}`.
  Compute it from the live system only. `shellRMConfigured` must accept both alias forms. Exit 0
  always (delete `return 2`). Update the `cmd/trash.go:statusBadge` call sites. The dashboard's
  `WarningCount()` becomes a count of disabled integrations.
- Help: `trashHelp.Usage` is `ws trash <ls|restore|rm|empty|prune|enable|disable|status>`.
  `setup` and `purge` are hidden. Drop `SetupFlags`, or rename the section "Enable flags" with the
  three `--no-…` flags and `--dry-run`.
- Completion (`cmd/complete.go`):
  - `completers["trash"].subcommands`: `ls restore rm empty prune enable disable status`.
  - Add a `completeTrash` resolver that offers item names for `restore` and `rm` (from `trash.Items`).
  - `commandFlags`: `enable` gets the three `--no-…` flags and `--dry-run`, `prune` gets
    `--older-than --dry-run`, and `restore`, `rm` and `empty` get `--dry-run`.
  - Remove `--root-dir` from `isStringFlag` once no command uses it. Check other chapters first.

### Tests and goldens (trash)

- `cmd/trash_test.go`:
  - `TestTrashStatusAndSetupFromCLI` expects exit 2 and `WARNING`. It becomes exit 0 with
    off/on markers, and it uses `enable`. Add a `setup` alias case.
  - `TestTrashPurgeOlderThanFromCLI`: rename to prune, and test `30d`, `30` and `2w`.
  - New tests:
    - restore: round trip, collision skip, missing parent created, no-info skip, original-path argument, exit 3;
    - rm with several items, and an unknown item aborting with nothing deleted;
    - disable removes the alias and the script;
    - `--root-dir` is rejected.
- `internal/trash/trash_test.go`:
  - `TestSetupAndStatus`, `TestSetupDryRunDoesNotWriteState`, and the
    `TestFileExplorerIntegration*` tests (custom root and symlink) get rewritten or deleted.
  - The `Empty`/`Scan` tests lose the root arg.
- `internal/trash/purge_test.go`: rename the tests.
- Goldens in `internal/tui/app/testdata/`:
  - `trash.golden`: footer and header.
  - `trash_empty_checklist.golden`: the case `{"9","x"}` becomes `:empty` through the command bar.
  - `trash_purge_prompt.golden`: "Prune … older than" with the age hint.
  - `trash_marked_delete.golden`: `{"9","A","d"}` becomes `ctrl+a`, `ctrl+d`.
  - `trash_marked_empty.golden`: `{"9","space","x"}`. Delete it, because `:empty` ignores marks.
    Add `trash_marked_restore` instead (`space`, `r`).
- Fixture `app_test.go:113-120`: the `Status` struct shape changes.

### Risks (trash)

- Host safety. `cmd/testmain_test.go` leaves `XDG_DATA_HOME` unset on purpose: "trash tests set
  HOME". Once `cmd` honours `$XDG_DATA_HOME`, a host or CI with `XDG_DATA_HOME` set would point
  the `empty` and `prune` tests at the real trash. In `TestMain`, `os.Unsetenv("XDG_DATA_HOME")`,
  or set it to a temp dir, in `cmd`, `internal/trash` and `internal/tui/app` (`hermetic()` already
  sets it). Run in Docker only.
- The shell alias. `~/.bashrc` may be a ws-managed dotfile, a symlink into `configs/dotfiles/`.
  `ensureLine` follows the symlink, so the alias line syncs to other machines. The guarded line
  is what keeps `rm` from breaking there. `disable` must edit through the symlink too, which
  `os.WriteFile` does.
- The `ws-trash-rm` script ignores every flag and silently skips missing paths (`rm -rf x` trashes
  `x`, and a typo is not an error). The behavior is unchanged, but it belongs in the help text.
- VS Code `settings.json` may be a ws dotfile and is JSONC. Only a regexp edit of the one literal is
  safe.
- `SoftDelete`'s `os.RemoveAll` fallback (`trash.go:53`) turns a lookup failure into a permanent
  delete. Return the error instead. That is a behavior change for `scratch rm`; coordinate with the
  scratch chapter.

---

## Workspace commands

### Routing and globals (`cmd/root.go`)

- `Execute` switch (lines 61-99): add `completion` (and keep `completions` as an alias). Route
  `reset` and `restore` to a `removedCommand(name)` that prints the spec's pointer and returns 1.
  The `log` and `credential` cases belong to other chapters.
- `help` case: `ws help <noun>` dispatches to that noun with `--help`. Today `shouldShowHelp`
  handles only a single arg, and the `help` case ignores `commandArgs`.
- `printHelpStyled` (lines 474-520): the noun and workspace lists from the spec. Rename
  `completions` to `completion`, and drop the `git-credential-helper` and `credential` alias rows.
  Usage line: `ws <noun> <verb> [targets…] [flags]`.
- Remove `--manifest` from:
  - `globalFlags.manifest`, the `parseGlobalFlags` registry, `registerGlobalFlags` and
    `resolveGlobalPaths` (lines 23, 114, 192-194, 327);
  - `workspace.PathOverrides.Manifest` and `Paths.Manifest` (`internal/workspace/paths.go`);
  - `resolveWorkspacePaths`'s third return and every caller (`cmd/tui.go:276`,
    `requireWorkspaceInitialized`);
  - `cmd/complete.go`: `globalFlagCompletions`, `isStringFlag` and `flagValueDirective`;
  - `TestFlagValueCompletionManifest` in `cmd/complete_test.go`.

  `internal/tui/screens/dotfiles/dotfiles.go:438` passes `p.Manifest` to `dotfile.AutoSync`.
  Switch it to `p.Config` (coordinate with the dotfile chapter). Grep `Manifest` across `cmd` and
  `internal` before deleting.

### `init` (`cmd/version.go:runInit`, `internal/workspace/init.go:Init`)

- Today `runInit` returns early when `ws.json` exists ("To reinitialize, remove ws.json…"), so it
  never creates missing dirs. It ignores `--config` and `$WS_CONFIG`
  (`configPath := wsconfig.ConfigPath(resolvedWorkspace)`), while the TUI's setup handover passes
  `--config` (`internal/tui/handover.WS`). `--dry-run` prints one line.
- Change it to resolve paths with `resolveWorkspacePaths(globals)`. Its precedence already matches
  the spec, and it also drops init's local `-w` flag duplication.
- `workspace.InitActions(workspace, configPath) []plan.Action` returns at most three actions:
  - missing layout dirs (one action);
  - `ws.json` if missing;
  - `.megaignore` if missing. It uses `wsconfig.Load(configPath).IgnoreRules` when `ws.json`
    exists, else `ignore.DefaultUserRules()`.

  Run it through `RunPlan`. JSON `init` `{workspace, config, created, existing, actions}`. Nothing
  to do: "Workspace already initialized: <path>", exit 0.
- Move `runInit`, `initHelp`, `runVersion` and `versionHelp` out of `cmd/version.go` into
  `cmd/init.go` (optional tidy-up).
- Tests:
  - `internal/workspace/init_test.go`: test the action-based API, and add idempotence (a second
    run has no actions; a deleted dir is recreated).
  - `cmd/root_test.go:TestInitAndConfigView`: switch to `config show`.
  - Every `newTestWorkspace` passes `strings.NewReader("y\n")`. With three prompts, EOF after the
    first `y` defaults to `y` (`promptChoice`), so it still works. Passing `--quiet` would be more
    explicit.

### `config` (`cmd/root.go:runConfig`, lines 380-468)

- `configHelp.Usage`: `ws config <show|edit>`. Hidden: `view`, `defaults`, `--defaults` as a
  subcommand.
- `show`: the current `view` code. Add `Config: configPath` to `viewPayload`. Extend
  `wsconfig.WithAbsPaths` (`internal/wsconfig/wsconfig.go:394`) to resolve `Links[i].Original`
  for display only. `--defaults` is a flag on `show`, and it skips `requireWorkspaceInitialized`.
  The JSON command is `config.show` in both modes.
- `edit` (new): mirror `cmd/ignore_extras.go:runIgnoreEdit`, including the editor resolution and
  the `--editor` flag.
  - The copy is `filepath.Join(filepath.Dir(configPath), ".ws.json.edit")`. Validate it with a
    `json.Decoder` and `DisallowUnknownFields`, using a struct that also has the legacy keys the
    other chapters keep reading (`log` beside `session`), plus `wsconfig.Load`'s schema checks.
  - Compare the `ws.json` bytes before and after to detect a concurrent change.
  - If `IgnoreRules` changed, call `ignore.WriteMegaignore(<workspace>/.megaignore, rules)`.
  - Do not `defer os.Remove` the copy on failure (ignore edit does, which loses the edit).
- TUI: in `internal/tui/screens/dashboard/dashboard.go:217`, `handover.WS(…, "config", "view")`
  becomes `"config", "show"`. Add an `e` binding to `keys.DashboardMap` that runs
  `handover.Run(handover.WS(m.env.Paths, "config", "edit"), handoverWizard)`, and add a reload on
  return.
- Tests: `cmd/root_test.go`:
  - `TestConfigDefaultsFlag` (`config --defaults`): keep it as an alias test, and add a
    `config show --defaults` test.
  - `TestConfigNoArgsShowsHelp`: it expects "view", which becomes "show".
  - `TestConfigDumpIsRejected`: the message stays "unknown config subcommand"; align it with the
    shared "unknown command" wording if another chapter changes it.
  - New: `config edit` tests with the editor shim from `cmd/testmain_test.go:createEditorShim`
    (valid, invalid with the copy kept, unchanged, ignore rules changed).

### `completion` (`cmd/completions.go`, `cmd/complete.go`)

- Rename the file to `cmd/completion.go`, `runCompletions` to `runCompletion`, and
  `completionsHelp` to `completionHelp` (Usage `ws completion <bash|zsh|fish|enable|disable>`).
  Rename `install`/`uninstall` to `enable`/`disable`, and keep the old words as hidden aliases with
  the stderr line.
- Routing: `completions bash|zsh|fish` must stay silent. Every existing rc block runs
  `eval "$(ws completions bash)"` (for example `/home/siva-14414/.bashrc:157`), and a stderr line
  there would print on every new shell.
- `completionRCInfo`: the lines become `eval "$(ws completion bash)"`, `eval "$(ws completion zsh)"`
  and `ws completion fish | source`.
- Enable logic: if a marker block exists and its body differs from the current line, the action
  replaces the block (`removeMarkerBlock`, then `removeStaleCompletionLines`, then append). If
  the body is the same, there is no action. `isStaleCompletionLine` must match both
  `ws completions` and `ws completion `.
- Script headers: "generated by ws completion bash", and so on. The `wsopen` function calls
  `ws scratch open --print-path`. Keep that flag in the scratch chapter.
- JSON: `completion.bash|zsh|fish` `{shell, script}`; `completion.enable|disable`
  `{shell, path, actions}`. Today the JSON writes `already_installed` and similar text **and** a
  JSON envelope on the same stdout (`cmd/completions.go:244-249`, `315-320`, `325-330`): it prints
  text before checking `globals.json`. Print text only when not in JSON mode.
- `parseCompletionFlags` hand-parses flags. Switch it to a `flag.FlagSet` with `--shell` and
  `registerGlobalFlags`, like every other command.
- `cmd/complete.go`:
  - `topLevelCommands` (lines 25-43): drop `reset` and `restore`, and rename `completions` to
    `completion`. Other chapters rename `log` to `session` and add `pass` and `credential`.
  - `completers`: `"completion": {bash zsh fish enable disable}`, `"config": {show edit}`,
    plus the cron and trash entries above.
  - `commandFlags`: `completion enable|disable` get `--shell --dry-run`, `config show` gets
    `--defaults`, `config edit` gets `--editor`, and `version` keeps `--short`.
- Tests: `cmd/completions_test.go`:
  - The command names change (`TestCompletionsJSON` expects `"completions"`).
  - Install and uninstall become enable and disable.
  - `TestCompletionsInstallIntegration` uses `completions install --shell bash --dry-run`. Keep it
    as an alias test.
  - Add a test that rewrites an old-line block, and one asserting that `completions bash` stays
    silent (empty stderr).
- Also update `cmd/complete_test.go` (`TestCompleteTopLevel`, `TestCompleteSubcommands`,
  `TestCompleteCommandFlags`), `cmd/parity_test.go:commandHelps` (the `completion` key) and
  `cmd/root_test.go:allDispatchedCommands` (`completions` becomes `completion`).

### `tui`, `version`, `help`

- `cmd/tui.go`: update `tuiHelp.Description` (Logs becomes Sessions) and drop `Manifest` from
  `overrides`. There is no other change.
- `cmd/version.go`: in `versionData`, the `Schema` JSON tag becomes `config_schema`. Change the text
  label "Schema" to "ws.json schema". Tests: check `cmd/root_test.go:TestVersionJSON` for a
  `schema` field assertion.
- `cmd/help.go`: no change in mechanics. `SetupFlags` is used only by trash. Rename it or drop it
  with the trash help change.

---

## TUI changes

Prerequisites from other owners: the `:` command bar (no code exists yet; nothing in
`internal/tui` matches "command bar"), and the global key moves. Refresh goes from `r` to
`ctrl+r`, describe from `i` to `d`, mark-all from `A` to `ctrl+a`, and half-page from `ctrl+d` to
`ctrl+b`/`ctrl+f`. Cron and Trash depend on both.

### Cron screen (`internal/tui/screens/cron/cron.go`, `keys.CronMap` in `internal/tui/keys/keys.go:467-502`)

- Keys:
  - enter: `Log`, Scope Cursor (unchanged).
  - `a`: `Add`, Scope Selected. Help text becomes "add" instead of "install".
  - `ctrl+d`: `Remove`, Scope Selected. Today it is `d`; delete the `d` binding.
  - `r`: new `Run` action, Scope Cursor. It needs refresh on `ctrl+r` first.
  - `:apply`: registered with the command bar. It opens `ApplyActions` in a checklist after the
    preflight.
- Run: `r` shows a notice for a preset row. Otherwise it opens a one-action checklist with
  `plan.Plan{Command: "cron.run"}`.
  - `confirm` already runs each action in its own `tea.Cmd` (`internal/tui/confirm/confirm.go:10`),
    so `Execute` does not block `Update`. But the checklist would stay open until the job ends,
    which is 5 minutes for `mega-sync`.
  - So the action only records consent. When the `DoneMsg` for `stepRun` reports the action
    accepted, the screen closes the modal, notifies "Running mega-sync…", and returns a `tea.Cmd`.
    That command calls `wscron.RunJob(name, RunOptions{WorkspacePath, Stdout: io.Discard, Stderr: io.Discard})`
    and yields `ranMsg{job, exit}`, which becomes a `nav.Notify` plus a reload.
  - Keep a `running map[string]bool` so a second `r` on the same job shows a notice instead of
    starting a second run.
- `openChange` (line 321) passes `p.Workspace` and `configs/crontab` into the new
  `AddActions`/`RemoveActions` signatures.
- `LoadedMsg.Failing()` is unchanged. Optional: add the `outdated`/`undeclared` count to
  `statusLine()`.
- `Load`: `wscron.Jobs(e.Paths.Workspace)`.
- `ShortHelp`: `{Log, Add, Remove, Run}`. `FullHelp` adds Run and the `:apply` hint.

### Trash screen (`internal/tui/screens/trash/trash.go`, `keys.TrashMap` in `keys.go:504-542`)

- Remove `Enable` (`e`), `Disable` (`D`), `Empty` (`x`) and `DeleteItem` (`d`).
- Add `Restore` (`r`, Scope Selected) and `Delete` (`ctrl+d`, Scope Selected).
- `Purge` becomes `Prune` (`p`, Scope All). The prompt title is "Prune items older than". Use the
  shared age parser in `prompt.WithValidate`, and delete `parseDays`/`daysFrom`.
- Command bar: `:empty` opens `openEmpty` for every item. Delete the marked-rows branch at lines
  387-389. `:enable` opens `openEnable`, built from `trash.EnableActions`. Delete the note telling
  the user to use the CLI flags, because unticking replaces them. `:disable` opens
  `trash.DisableActions`.
- `openEnable` today includes the action "Record trash provisions (so `ws reset` can undo them)"
  (line 261). Delete it. `openDisable` uses plan command `trash.reset`, which becomes
  `trash.disable`.
- `openRestore(rows)`: `trash.RestorePlan(rows)`. Skipped rows go into the checklist note via
  `confirm.WithNote`. Rows are newest first, so the newest wins on a collision.
- Delete the `root()` helper and the `m.state.Status.RootDir` fallbacks, and use `trash.Root()`.
  The header comes from the new `Status` shape. Keep `RequestEnableMsg` and `RequestEmptyMsg`,
  which the dashboard's quick-fix uses (`dashboard.go:540, 545`).
- Header path: today `m.env.Rel(st.RootDir)`. See the path-rule note above.

### Dashboard (`internal/tui/screens/dashboard/dashboard.go`, `keys.DashboardMap` in `keys.go:101-149`)

- Delete `Restore` (`O`) and `Reset` (`R`) from `DashboardMap`, `Dashboard()` and `FullHelp`.
  Both use shift letters, which are banned. `O` also runs `ws restore`, which no longer exists
  (`dashboard.go:218-219`, a broken handover today).
- Delete `openReset` (lines 342-378), the `case key.Matches(k, m.keys.Reset)` branch (220-221),
  and `stepReset` (line 100), if no other step uses that iota. Remove the now-unused imports
  (`dotfile`, `wstrash`, `plan`, `os`, if nothing else uses them).
- `c` stays as `config show`. Add `e` for `config edit` (the global meaning of `e`). Put both in
  `FullHelp`.
- Area cards: `trashArea` uses `msg.Status.WarningCount()` and "soft-delete off (n of 3)". Keep
  it, computed from the new live `Status`. `cronArea` is unchanged.

### Coverage (`internal/tui/app/coverage.go`)

- `Coverage` needs a way to name a command-bar entry. Add `Cmd string // ":apply"`, and teach
  `TestCoverageKeysExist` (`coverage_test.go:26`) to check that the screen's command bar declares it.
- Replace lines 33-56 and 111-118 with these entries:

```go
{"init", nav.ScreenDashboard, none, "", "the TUI shows its setup screen (i) until the workspace is initialized"},
{"version", …none…, "prints the build version"}, {"help", …"the TUI has ? help"}, {"tui", …"starts this TUI"},
{"completion bash|zsh|fish|enable|disable" ×5, nav.ScreenDashboard, none, "", shell},
{"config show", nav.ScreenDashboard, d.Config, "", ""},
{"config edit", nav.ScreenDashboard, d.Edit, "", ""},
{"cron ls", nav.ScreenCron, none, "", ""}, {"cron add", …cr.Add}, {"cron rm", …cr.Remove},
{"cron run", …cr.Run}, {"cron log", …cr.Log}, {"cron apply", nav.ScreenCron, none, ":apply", ""},
{"trash ls", nav.ScreenTrash, none, "", ""}, {"trash restore", …tr.Restore}, {"trash rm", …tr.Delete},
{"trash empty", …none, ":empty"}, {"trash prune", …tr.Prune}, {"trash enable", …none, ":enable"},
{"trash disable", …none, ":disable"}, {"trash status", nav.ScreenTrash, none, "", "the screen header"},
```

- Delete `reset`, `restore`, `cron remove`, `cron list`, `cron status`, `trash setup`,
  `trash purge`, and `completions …`. `cmd/parity_test.go:TestCLICoverage` fails on any entry
  whose command is not in a help usage list, and hidden aliases are no longer listed.
- `coverage_test.go:79` `TestEveryListKeyDeclaresItsRows`: `exempt` contains `"r"` (refresh).
  Change it to `"ctrl+r"`, or `r` on Cron and Trash will be flagged once refresh moves. Run is
  declared, so after the move it passes either way.

### App tests and goldens (`internal/tui/app/app_test.go`)

- `TestSnapshots` (lines 206-253): delete `dashboard_reset_checklist` (line 214) and its golden.
  Update the trash cases (240-243) as listed under trash. Add `cron_run_checklist` (`8`, `r`) and
  `trash_restore_checklist` (`9`, `r`).
- `TestResize` flows (line 287): `{"1","R"}` becomes `{"8","r"}` or `{"9","r"}`, to keep a dialog
  in the resize sweep.
- `full_help.golden` (`2`, `?`) and `dashboard.golden` are affected only if global help or the
  dashboard footer shows the removed keys. The dashboard `ShortHelp` doesn't list `O`/`R`, so
  probably only `FullHelp` users are affected. Re-generate the goldens and review them.

## Stale `reset` / `restore` cleanup (all places)

- `cmd/complete.go:36-37`: `"reset"`, `"restore"` in `topLevelCommands`. Delete them.
  `cmd/complete_test.go:TestCompleteTopLevel` may count the entries.
- `internal/tui/app/coverage.go:34-35`: the `reset` and `restore` entries. Delete them.
- `internal/tui/keys/keys.go:114-115, 131-132, 148`: `DashboardMap.Restore` and `Reset`. Delete them.
- `internal/tui/screens/dashboard/dashboard.go:100, 218-221, 342-378`: `stepReset`, the
  `O`/`R` handlers and `openReset`. Delete them.
- `internal/tui/app/testdata/dashboard_reset_checklist.golden` and its case. Delete them.
- `internal/workspace/reset.go` (`Reset`, `Provisions`, `UndoActionLabel`) and
  `internal/workspace/reset_test.go`: nothing outside tests calls them. Delete both. That leaves
  `internal/provision` used only by `internal/manifest`, which is also a deletion candidate
  (outside this chapter; confirm with a grep).
- `internal/trash/reset.go` (`Reset`, `ResetOptions`, `ResetEntry`, `ResetResult`): replaced by
  `DisableActions`. Delete it. The callers are `cmd/trash.go:169`,
  `internal/tui/screens/trash/trash.go:281` and the dashboard's `openReset`.
- `internal/trash/provisions.go`: no-op shims. Delete them along with their calls
  (`cmd/trash.go:105-123`, `trash.go:260-268` in the TUI).
- Text mentioning "ws reset" or "ws restore": `internal/tui/screens/trash/trash.go:261`; the
  `GetStatus` reason string "run `ws trash setup`" (`internal/trash/trash.go:284`) becomes
  `ws trash enable`. Grep `ws reset`, `ws restore` and `trash setup` across `internal/` and `cmd/`.
- `cmd/root.go`: add the removed-command pointer for `reset` and `restore`.

## Cross-cutting dependencies

- Age parser. `cmd/log.go:parseDurationWithDays` doesn't accept a bare integer or `w`. Put one
  parser in a shared package, for example `internal/age.Parse`, accepting `<n>`, `<n>d`, `<n>h`
  and `<n>w`, positive only. Trash prune, session prune and both TUI prompts use it. Agree on the
  owner with the session chapter.
- JSON schema 3: `jsonSchema` (`cmd/root.go:274`) becomes 3. Command names: `cron.ls`, `cron.add`,
  `cron.rm`, `cron.run`, `cron.log`, `cron.apply`, `trash.ls`, `trash.restore`, `trash.rm`,
  `trash.empty`, `trash.prune`, `trash.enable`, `trash.disable`, `trash.status`, `init`,
  `config.show`, `config.edit`, `completion.bash|zsh|fish|enable|disable`, `version`.
- `ls` data shape: this chapter returns a plain record array for `cron.ls`, as `trash.ls`
  already does. Other chapters should match.
- Hidden-alias helper: one `alias(stderr, old, new)` printing `ws: "<old>" is now "<new>"`, with a
  `silent` variant for `cron run <old-job>` and `completions bash|zsh|fish`.
