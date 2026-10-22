> **Read this first.** These notes were written on 2026-10-22, before the user's last round of
> decisions. Where they disagree with spec.md, **spec.md wins**. Use them for file:line
> locations and today's behaviour. Changed since:
>
> - `repo sync` is removed. In its place: `repo push` (push only) and `repo save` (`git add -A`, commit `ws save <date time> (<host>)`, pull, push). The notes' `sync` sections are now code locations for `save`.
> - Diverged repos are never auto-merged. `pull` and `save` ask `[r]ebase/[m]erge/[s]kip` per repo (a chooser in the TUI). `--rebase`/`--merge` answer ahead of time, and without a terminal the repo is skipped. There is **no checklist rebase toggle**: ignore every note about `confirm.WithOption`.
> - `repo configure` is new (an interactive fix for identity, upstream, default branch and credentials), and so is the hygiene check `credentials`.
> - The `repo-sync` cron job is retired.
> - TUI keys: `s` = save, `c` = configure, `:push`. There's no `S`, `P` or rebase toggle.

# Implementation notes: `ws repo` (Repos screen)

Line numbers refer to the tree at b77d3c8 plus the uncommitted changes that existed on
2026-10-22. Run tests in Docker only: host `go test` is not HOME-safe.

## Decisions taken in spec.md (review these first)

1. **`issues` counts only `warn` and `error` findings.** Today it is `len(e.Findings)`
   (`internal/repo/fields.go:53`), so the two `info` identity findings that every repo with a
   global identity gets inflate it. `findings` still lists every check id. The TUI `Summary.Findings`
   (`repos.go:75`) and the Dashboard's "N hygiene findings" (`dashboard.go:397`, `:421`) should
   use the same count.
2. **`repo fetch` and `repo ls --fetch` have no plan and no prompt.** They change only
   remote-tracking refs. Add this to the dev.md "RW exemptions" list, which today names
   only `session record|stop`.
3. **`repo ls --fetch` exits 3** when some fetch failed (the list still prints). Fetch failures
   go to stderr as `ws: fetch failed: <path>: <err>`; today they go to stdout with a badge.
   `--dry-run` skips the fetch.
4. **`repo fetch` exits 0, 3 when some fail, 1 when all fail.** Today it returns 3 on any
   failure (`cmd/repo.go:850`). `--dry-run` prints `Would fetch <path>` and does nothing; today
   `--dry-run` is ignored and the fetch runs.
5. **`repo doctor` becomes a typed hidden alias of `repo ls`.** Today it is a tombstone that
   exits 1 (`cmd/repo.go:262-264`).
6. **`repo show` never fetches.** The TUI detail shows only the selected repo's last fetch
   failure. Today it lists every fleet failure under "Last fetch failures"
   (`repos.go:808-813`).
7. **`repo pull` leaves out repos with a scan error, a detached HEAD or no upstream**, and
   prints `Skipped: <path> (<reason>)`. Today every repo gets an action, and git then fails
   on it.
8. **`repo pull --rebase` aborts a failed rebase** (`git rebase --abort`), as `SyncOne`
   already does. Today `PullAll` (`internal/repo/repo.go:220`) leaves the repo mid-rebase.
9. **`repo sync` skips a repo whose fetch failed** (reason `fetch failed: <err>`). Today the
   fetch result is discarded (`cmd/repo.go:393`, `:401`; `repos.go:255`), and sync acts on
   stale counts.
10. **`repo run` takes `[repo…]` before `--`.** Without `--`, every positional is the
    command, as today. The command output is now printed: today the CLI drops it entirely,
    because `RunAll`'s `Output` is never shown (`cmd/repo.go:536-542`).
11. **`repo root add` refuses a directory outside the workspace.** `Discover` has always
    skipped repos outside the workspace (`internal/repo/repo.go:157-160`), so such a root
    finds nothing. Today `add-root` accepts it and stores it as an absolute path.
12. **`repo root rm` refuses to remove the last root.** An empty list means `["."]`
    (`Discover`, `repo.go:105-107`), so removing the last narrow root would widen discovery
    to the whole workspace.
13. **TUI `:root rm` with no directory** opens a checklist of every root, none ticked
    (the same pattern as the Sessions remove checklist, `logs.go:497`).
14. **JSON shapes:** `pull` and `sync` report `skipped: [{path, reason}]` in place of the
    `warnings: ["path (reason)"]` strings. `sync` always includes `plans`. `run` adds
    `results: [{path, exit_code, output}]`. `show` is `{status, findings}`. `ls` keeps the old
    scan keys (`statuses`, `findings`, `fetch_warnings`).
15. **`--json` keeps the plan's exit code** for pull, sync and run. Today these return
    `writeJSON`'s 0 even when actions failed (`cmd/repo.go:349-351`, `:487-489`, `:546-548`).
16. **`--dry-run` for pull and run prints the plan's actions** (`[dry-run] Pull …`).
    Today it prints `Would pull N repositories.` (`cmd/repo.go:324-330`, `:521-527`).

Positional targets stay an **exact** match of a repo path (dev.md §12.3 rule 2), now with
several targets allowed.

## Cross-cutting pieces this group depends on

- **Alias table** (dev.md §12.6.3). Repo entries, all typed (they print the hint):
  - `repo scan` → `repo ls --fetch`. Keep a hidden `--no-fetch` bool on `ls` that cancels
    `--fetch`, so `repo scan --no-fetch` works through a plain prefix rewrite. Hint:
    `ws: "repo scan" is now "repo ls --fetch"`.
  - `repo doctor` → `repo ls`
  - `repo add-root` → `repo root add`
  - `repo ls-roots` → `repo root ls`
  - Plumbing (silent): `repo sync --quiet` from the `repo-sync` cron job
    (`internal/cron/jobs.go:46`). Its name does not change, so it needs no entry.
- **JSON names:** `repo.ls`, `repo.show`, `repo.fetch`, `repo.pull`, `repo.sync`, `repo.run`,
  `repo.root.add`, `repo.root.ls`, `repo.root.rm`. Today: `repo.scan`, `repo.add-root`,
  `repo.ls-roots`. Plan IDs: `pull-<path>`, `sync-<path>`, `run-<path>`, `root-add-<path>`
  (was `add-root-<path>`), `root-rm-<path>`.
- **Nested verbs** (`repo root add|ls|rm`; the same shape as `dotfile backup …` and
  `pass backup …`). Three things only read `top sub` today:
  - `cmd/parity_test.go` (`subcommands`, `cliCommands`)
  - `internal/tui/app/coverage.go` (`Coverage` has no field for a `:` command)
  - `cmd/complete.go` (two-level `completers`)

  They all need a way to name a third level, and `Coverage` needs a `Cmd string` (for
  example `":root ls"`) so that `TestCLICoverage` can accept `:` commands.
- **Per-verb help** (dev.md §12.6.1). `runRepo` prints the group help for any `--help`
  (`cmd/repo.go:121-123`). Each verb needs its own `cmdHelp`, plus `ws repo root --help`. The
  `--path` help text is wrong today ("workspace subpath", `cmd/repo.go:22`, `:40`): it is a
  path argument resolved against the cwd.
- **Exit 2 removal:** the only exit 2 in this group is `renderRepoScan`
  (`cmd/repo.go:726-736`).
- **TUI `ctrl+r` refresh:** `keys.refresh()` is `r` (`keys.go:749-751`). Moving it frees `r`
  for run. `TestEveryListKeyDeclaresItsRows` exempts `"r"` (`coverage_test.go:79`); that
  must become `"ctrl+r"`, or it will hide an undeclared `r`.
- **`:` command bar** (dev.md §13 names `internal/tui/cmdbar` and `nav.CommandMsg`; neither
  exists yet). Repos declares `root ls` and `root rm [dir]`.

## Per command

### `repo ls` (merges old `ls` and `scan`)

- Today: `cmd/repo.go:154-182` (`ls`: paths only, through `renderRepoList` at `:689`; JSON is
  `[{"path"}]`). `:183-261` (`scan`: fetch, `Scan`, `Filter`, `Doctor`, `applyList`,
  `renderRepoScan` at `:719` with its custom badge layout and exit 2).
- Change: a single `ls` case that does the following:
  1. `Discover`.
  2. Resolve targets (new multi-target `targetRepos`, below).
  3. Fetch the targets if `--fetch`.
  4. `Scan`, then `Filter`.
  5. `Doctor` with `--check`.
  6. `repo.Entries`, then `renderListIn(globals, "repo.ls", …)`, with JSON
     `{"statuses": Records, "findings": […], "fetch_warnings"?}`. `renderListIn` can't carry
     the extra keys, so build the map by hand after `applyList`, as `scan` does today
     (`:251-261`, `:738-747`).

  The text output is the schema table (`schema.WriteTable`). Drop `renderRepoScan`, the
  `Hygiene:` footer and the `--verbose` info hiding. Keep `validateHygieneChecks` (`:706`).
  Register `--fetch`, the hidden `--no-fetch`, `--check`, the filter flags and the list flags.
- `internal/repo/fields.go`: make `issues` count `Severity >= SeverityWarn` (decision 1).
  Update the `Entry` doc comment ("the record `ws repo scan` reports", `:10`).
- Remove `isExternalRepo` (`cmd/repo.go:116-118`) and its call sites (`:227`, `:387`, `:398`).
  `Discover` never returns a repo outside the workspace, so the check is dead.
- Tests (`cmd/repo_test.go`):
  - `TestRepoLsAndScan`: `repo scan --check identity` → `repo ls --check identity`. The dirty
    case expects exit 2 (`:61`), now 0. The run dry-run check for `"Would run command"`
    (`:70`) becomes `[dry-run] Run in`.
  - `TestRepoScanNoFetch`: becomes plain `repo ls`, which no longer fetches by default.
  - `TestRepoScanHygieneCleanIdentity`, `…InlineWarnings` (exit 2, inline `[identity]`, the
    `Hygiene: 2 warning(s)` footer) and `…InfoHiddenUnlessVerbose`: rewrite them against the
    table's `issues` cell, `repo show` and `--json` `findings`.
  - `TestRepoScanUnknownCheck`: becomes `repo ls --check bogus`.
  - `TestRepoScanJSONIncludesFindings`: becomes `repo ls --json`, command `repo.ls`.
  - `TestRepoDoctorRemoved`: becomes an alias test (exit 0, plus the hint on stderr).
  - `TestRepoListsDotfilesRepoOnce`: still passes on the table, but its comment ("also a
    built-in repo", `:158-159`) is stale since f71b4f6.
- Add tests:
  - `repo scan` alias: hint on stderr, `--no-fetch` respected.
  - Several targets; an unknown target fails before any fetch.
  - `--fetch` against a broken remote: exit 3, failure line on stderr.
  - `ls` exits 0 when repos are dirty.
- Risk: scripts that read the old `repo ls` (one path per line) or its JSON `[{"path"}]` break.
  The replacement is `ws repo ls --json | jq -r '.data.statuses[].path'`. Nothing in the repo
  calls it (only README.md:135-142, dev.md, and the old spec mention it).

### Targets helper (shared by ls, show, fetch, pull, sync, run)

- Today: `targetRepo` (`cmd/repo.go:95-112`) allows one target, and it runs *after*
  `filterRepos` in ls, fetch and pull (`:173-181`, `:284-292`, `:315-323`). A target that a
  filter removes therefore reports "no repo at", which is wrong. `sync` resolves the target
  before filtering (`:373-380`). `run` ignores targets.
- Change: add `targetRepos(repos, args) ([]repo.Repository, error)`. It resolves every
  argument with `wsconfig.ExpandUserPath` plus `filepath.Clean`, matches it exactly, dedupes,
  keeps discovery order, and fails on the first unknown target (`ws: no repo at <abs>`).
  Resolve targets first and filter after, everywhere.
- Consider moving discover, then targets, then scan, then filter into `internal/repo`
  (`repo.Select(ws, roots, excludes, targets, FilterOptions)`), so the TUI and the CLI cannot
  drift (dev.md §13).

### `repo show` (new)

- Today: only the TUI detail exists, in `repos.go:769-815` (`detailContent`).
- Change: a new `show` case. It needs exactly one target. Then it runs `Scan` on that repo
  and `Doctor` with every check. Move the detail layout into one renderer in
  `internal/repo`, for example `repo.Detail(e Entry) []DetailLine{Label, Value}` plus the
  findings, which the CLI prints plain and the TUI styles. CLI and TUI then print the same
  lines (the b77d3c8 pattern). JSON `repo.show`: `{"status": Schema.Record(e), "findings": e.Findings}`.
- The TUI keeps `fetchErrors`, but as `map[path]string`. `detailContent` shows
  `Last fetch  failed: <err>` only for `e.Status.Path`. The status line count is
  `len(map)`.
- Goldens: `repos_detail.golden` should not change if the shared renderer keeps the labels.
- Add tests: text and JSON; a missing argument, two arguments, or an unknown argument exits 1.

### `repo fetch`

- Today: `cmd/repo.go:265-294`, `renderRepoFetch` (`:823-854`), `repo.FetchAll`
  (`repo.go:206`).
- Change:
  - multi-target, resolved before filtering;
  - `--dry-run` prints `Would fetch <path>` and returns 0;
  - exit 0, 3 or 1 (decision 4).

  JSON is unchanged (`[{"path", "success", "error"}]`).
- Add tests: `--dry-run` must not create `.git/FETCH_HEAD`; check the exit code for partial
  and total failure (`internal/repo/repo_test.go:62` `TestFetchAllNoRemotePartial` has a
  fixture to reuse).

### `repo pull`

- Today: `cmd/repo.go:295-352`. Every repo gets an action that calls `repo.PullAll`
  (`repo.go:220-236`). There is no skip and no rebase abort.
- Change:
  - Scan the targets and filter them.
  - Skip repos whose status has an error, a detached HEAD or no upstream. Factor the skip
    part of `PlanSync` (`repo.go:267-282`) into `repo.SkipReason(st RepoStatus) string`, and
    use it in both places.
  - The description becomes `Pull <path> (ff-only|rebase)`.
  - In `PullAll`, on error with `rebase`, run `git rebase --abort`, ignoring its error.
  - JSON is `{"actions", "skipped"}` with the real exit code.
  - Dry run goes through `RunPlan`'s dry-run path.
- Add type `repo.Skip{Path string \`json:"path"\`; Reason string \`json:"reason"\`}` for pull
  and sync.
- Add tests: a repo with no upstream is skipped with its reason; a rebase conflict leaves no
  `.git/rebase-merge`.

### `repo sync`

- Today: `cmd/repo.go:353-495`. The fetch loop (`:382-403`) ignores results; plans come from
  `repo.PlanSync`, `repo.DescribeSync` (`internal/repo/special.go`) and `repo.SyncOne`
  (`repo.go:323-402`). Dry-run has its own strategy formatting (`:436-466`, `rebaseOrMerge`
  at `:682`). The TUI duplicates this in `prepareSync` (`repos.go:244-271`).
- Change:
  - Move fetch, scan, filter, plan and skip into
    `repo.PrepareSync(ws, repos, FilterOptions) (plans []SyncPlan, skipped []Skip)`, used by
    both the CLI and the TUI. A failed fetch becomes a `Skip`.
  - Dry-run prints `DescribeSync` lines through `RunPlan`. Delete the custom block and
    `rebaseOrMerge`.
  - JSON is `{"actions", "plans", "skipped"}` with the real exit code.
  - Keep the progress line (text and non-quiet only).
  - Keep the auto-commit exactly as it is (`git add -A`; `ws: auto-sync commit`).
- Rename `internal/repo/special.go` to `describe.go`. It holds only `DescribeSync` now that
  f71b4f6 removed the special repos.
- Tests: `TestRepoSync` (`cmd/repo_test.go:85`) still passes, since dry-run output keeps
  `dry-run`. `internal/repo/repo_test.go` `TestPlanSyncSkips` and `TestSyncOne` are
  unchanged unless `SkipReason` moves. Add a test that a failed fetch is skipped.
- Risks:
  - The `repo-sync` cron job runs `ws --workspace <ws> repo sync --quiet` every 30 min
    (`internal/cron/jobs.go:40-47`). It must keep working silently, and `--quiet` must
    keep auto-accepting (`cmd/plan.go:60`).
  - Sync auto-commits every dirty repo with `git add -A`, including `configs/dotfiles` (the
    dotfile backup repo) and `secrets/pass` if it is a git repo inside the workspace. This
    overlaps with `dotfile backup push` and with pass's own commits. The behavior is
    unchanged, but the user should know. The old spec's "sync never commits" was wrong.

### `repo run`

- Today: `cmd/repo.go:496-549`. Every positional is the command; there are no targets, and
  the output is dropped. `repo.RunAll` (`repo.go:404-431`) captures stdout only on success
  and stderr only on failure.
- Change:
  - Split `subArgs` at the first literal `--` *before* `fs.Parse`. The left side holds the
    flags and targets, and the right side is the command. Go's `flag` consumes a leading
    `--`, so splitting after parsing mixes up `ws repo run -- git log -- file`. With no `--`
    in `subArgs`, keep today's behavior: after parsing, `fs.Args()` is the command.
  - `RunAll` captures stdout and stderr into one buffer. `OperationResult` gains
    `ExitCode int \`json:"exit_code"\``, with -1 when the command did not start, and keeps
    `Output` on failure too.
  - The CLI prints each action's output, indented, under its result line. The cleanest hook
    is an optional `Output func() string` on `plan.Action` (`internal/plan/plan.go:9`) that
    `RunPlan` prints after the `✔`/`✘` line (`cmd/plan.go:101-120`). The TUI ignores it,
    because it has the output view.
  - JSON is `{"command", "actions", "results"}` with the real exit code.
- Tests: `TestRunAll` (`internal/repo/repo_test.go:88`) changes for the new fields. Add CLI
  tests:
  - targets before `--`;
  - no `--` (compatibility);
  - output printed;
  - non-zero exit, giving 3 or 1.

### `repo root add|ls|rm`

- Today: `add-root` (`cmd/repo.go:550-642`), `ls-roots` (`:643-671`, prints
  `Configured repo roots:` and 0-based `N.` lines). The root resolution for every command is
  at `:140-147`. Config: `wsconfig.Repo.Roots` (`internal/wsconfig/wsconfig.go:57-61`),
  default `["."]` (`:117`, `:272-274`).
- Change:
  - A `root` case with its own sub-switch.
  - `add`:
    - Keep the stat and dir checks.
    - Add a check that the directory is inside the workspace (`repo.IsWithin`).
    - Always store it workspace-relative (`filepath.ToSlash(rel)`; `.` for the workspace
      itself).
    - Keep the duplicate check (compare resolved paths).
    - Plan action `Add repo root <abs>`, ID `root-add-<abs>`.
  - `ls`:
    - Print the resolved effective roots, absolute, one per line, with no header.
    - Print the workspace path when the list is empty.
    - JSON is `{"roots": […]}` (the key is unchanged).
  - `rm`:
    - Resolve the argument, then match it against each resolved entry.
    - Refuse when no entry matches, or when removing it would leave the list empty.
    - Plan action `Remove repo root <abs>` reloads `ws.json` and drops every matching entry
      before `wsconfig.Save`.
- Existing `ws.json` files may hold absolute roots outside the workspace (accepted until
  now). `root ls` prints them, and `root rm` can remove them; they still find nothing.
- Add tests:
  - add inside the workspace (the stored value is relative);
  - add outside the workspace is refused;
  - add a duplicate;
  - ls with the default and with an empty list;
  - rm a match, rm an unknown root, rm the last root;
  - the `add-root` and `ls-roots` alias hints.

### Completion (`cmd/complete.go`)

- `completers["repo"]` (`:63`): `ls, show, fetch, pull, sync, run, root`.
  `TestCompletersOfferEverySubcommand` compares this list with the help Usage.
- `completeRepo` (`:249-262`):
  - `ls`, `fetch`, `pull`, `sync`: offer repo paths at every position, minus the ones
    already given (today only the first position).
  - `show`: the first position only.
  - `run`: repo paths before `--`, `compDirectiveDefault` after it.
    `splitArgsForCompletion` (`:545-549`) drops the `--` marker, so keep it (for example,
    leave `--` in `positional`).
  - `root`: `add`, `ls`, `rm` at the first position. After `add`, return
    `compDirectiveFilterDirs`. After `rm`, offer the configured roots as absolute paths;
    this needs a new `completionCtx.repoRoots` filled in `loadCompletionCtx` (`:510-525`).
- `commandFlags` (`:448-465`):
  - `ls`: filter flags plus `--fetch --check --sort --filter --wide`;
  - `fetch`: filter flags plus `--dry-run`;
  - `pull`, `sync`: `--rebase --dry-run` plus filter flags;
  - `run`: `--dry-run` plus filter flags;
  - `root`: `--dry-run`;
  - drop the `scan` entry; leave `show` with no flags.
- `isStringFlag` (`:570`): add `check`. Offer `repo.HygieneChecks` as `--check` values, and
  `compDirectiveFilterDirs` for `--path` (`flagValueDirective`, `:121-134`).

### Coverage (`internal/tui/app/coverage.go:82-89`)

The entries become:

- `repo ls` (screen)
- `repo show` (`rp.Open`)
- `repo fetch` (`rp.Fetch`), `repo pull` (`rp.Pull`), `repo sync` (`rp.Sync`), `repo run`
  (`rp.Run`)
- `repo root add` (`rp.AddRoot`)
- `repo root ls` (`:root ls`), `repo root rm` (`:root rm`)

Drop `repo scan`, `repo add-root` and `repo ls-roots`. The old `ls-roots` entry (`rp.AddRoot`,
"the add-root prompt lists the roots") is replaced by `:root ls`.

## TUI: Repos screen (`internal/tui/screens/repos/repos.go`, `internal/tui/keys/keys.go`)

### Keys (`keys.ReposMap`, `keys.go:151-197`)

| Key | Today | Target |
| --- | --- | --- |
| `enter` | details | details (= `repo show`) |
| `f` | fetch | fetch |
| `p` | pull (ff-only) | pull: checklist with a rebase toggle |
| `P` | pull --rebase | **removed** |
| `s` | sync (merge) | sync: checklist with a rebase toggle |
| `S` | sync (rebase) | **removed** |
| `x` | run command | **removed**, now `r` |
| `r` | refresh | run (refresh is global `ctrl+r`) |
| `a` | add root | add root (= `repo root add`) |
| `:root ls` | none | read-only roots view |
| `:root rm [dir]` | none | remove-root checklist |

- Delete `PullRebase` and `SyncRebase` from `ReposMap`. Set `Run` to
  `key.WithKeys("r"), key.WithHelp("r", "run")`. Relabel: `p` "pull", `s` "sync", `a` "add
  root". `Refresh` comes from the global map.
- `ShortHelp`: `Open, Sync, Pull, Fetch, Run`. `FullHelp`: `{Open, Fetch, Back}`,
  `{Sync, Pull}`, `{Run, AddRoot}`.
- The `actions` table (`repos.go:286-318`): drop the `k.PullRebase` and `k.SyncRebase`
  rows, and the `rebase` parameter of the `pull`/`sync` closures. `k.Run` keeps
  `reslist.All`. The marks hint becomes `3 marked — f p s r act on them`.
- `RequestSyncMsg` (`:84-85`, `:425-432`, sent by the Dashboard fix at `dashboard.go:419`)
  opens the same sync checklist with rebase off. Its comment "(merge), as pressing s does"
  stays true.

### Rebase toggle: what `internal/tui/confirm` needs

Today `confirm.Model` (`confirm.go:56-70`) has a fixed title, note and plan. Toggling rebase
changes the title (`Pull 4 repos (ff-only)` / `(rebase)`), every description (`DescribeSync`
takes `rebase`), and every `Execute` closure (`PullAll(…, rebase)`, `SyncOptions{Rebase}`).
The checklist therefore has to rebuild its plan.

- Add:
  ```go
  // Option is one on/off switch shown under the title. Toggling it rebuilds
  // the title and the plan; the user's ticks carry over by action ID.
  type Option struct {
      Key   key.Binding // e.g. key.WithKeys("r"), key.WithHelp("r", "rebase")
      Label string      // "rebase"
      On    bool
      Build func(on bool) (title string, p plan.Plan)
  }
  func (m Model) WithOption(o Option) Model // calls o.Build(o.On) once
  ```
- `updateReview` (`confirm.go:149-184`): when `o.Key` matches, flip `On`, call `Build`, and
  replace `title` and `plan`. Then rebuild `checked`, `status` and `errs` to the new length:
  `checked` keeps the old value for an action ID that still exists, and new IDs are checked.
  Clamp `cursor`. The key does nothing in `phaseRunning` and `phaseDone`. One option per
  checklist is enough; don't build a list.
- `View` (`:240-282`): add one line after the note, `[x] rebase  r` or `[ ] rebase  r`, muted
  when off. Count it in `head`, so the scroll math (`avail`) stays right.
- `KeyMap` (`:347-355`): in review, include `o.Key` (footer `space toggle • a all/none •
  r rebase • enter apply • esc back`). This needs a small wrapper key map, because
  `keys.ConfirmMap` is shared by every checklist.
- The key must not collide with `ConfirmMap` (`up/k down/j space a enter esc`, `keys.go:611-621`).
  `r` is free. If the checklist's all/none moves from `a` to `ctrl+a` in the cross-cutting
  key change, nothing here changes.
- `DoneMsg` is unchanged: the `Execute` closures already carry the chosen mode. Repos does
  not remember the toggle; every pull or sync checklist opens with rebase off.
- `repos.go`: `openPull` (`:535-558`) becomes `pullBuilder(rows) func(bool) (string, plan.Plan)`.
  `openSync` (`:560-589`) builds from `msg.plans` and needs no refetch. Drop `rebase` from
  `syncPreparedMsg` (`:88-95`) and `prepareSync` (`:244`).
- Tests: `internal/tui/confirm/confirm_test.go` needs these cases:
  - toggling rebuilds the title and descriptions;
  - an unticked action stays unticked across a toggle;
  - the toggle is ignored while running;
  - `Execute` uses the new mode.

### Pull checklist skips

`openPull` should leave out rows with `Status.Error`, `Detached` or `!HasUpstream`, using
`repo.SkipReason`. It adds `WithNote("Skipped: …")`, as `openSync` does (`:584-586`).

### `:root` commands

- `:root ls`: show the roots at `levelOutput`, reusing `m.detail` like the run output, with a
  title `Repo roots` and one `m.env.Rel(root)` per line. When `repo.roots` is empty, show the
  workspace and mark it as the default.
- `:root rm <dir>`: resolve the argument as `openAddRoot` does (`:621-661`) and open a
  one-action checklist. Refuse an unknown root or the last root with `nav.Notify(LevelWarn, …)`
  before the checklist opens. With no argument, open a checklist of all roots, none ticked
  (`WithChecked(… false)`). The action's `Execute` reloads `ws.json` and refuses to leave
  the list empty.
- Roots state: today `m.roots` is copied from `env.RepoRoots` and changed optimistically.
  `openAddRoot` appends at `:658`, and `dialogResult` trims at `:509-512` and `:518-520`.
  With rm added, the trim-on-abort trick breaks, because it assumes the last element was just
  appended. Instead, after any root checklist finishes, reload the roots from `ws.json`
  (`wsconfig.Load(m.env.Paths.Config)`, resolved as `env.go:61-66` does). Do the same in
  `nav.ReloadEnvMsg`.
- `openAddRoot` gains the inside-workspace check (`nav.Notify(LevelError, "Not inside the
  workspace: …")`).

### Run (`r`)

- `promptRun` (`:344-359`) and `openRun` (`:591-617`) are unchanged apart from the key.
  `renderOutput` (`:674-698`) shows `✘ <path> (exit N)` and the output for failures too, once
  `RunAll` keeps `Output` and `ExitCode` on failure. Today a failed repo shows only its mark
  and path.

### Empty state

`repos.go:197` hardcodes `"No git repositories found. Press a to add a repo root."`. `a`
stays correct under the new grammar, but build the text from the key map, so the next
rename can't leave it stale:
`"No git repositories found. Press " + keys.Repos().AddRoot.Help().Key + " to add a repo root."`.
Other screens hardcode keys the same way, and their keys do change: Sessions
`logs.go:145` "Press s" (becomes `r`), and Scratch `scratch.go:127` "Press n" (becomes `a`).
Tell those groups.

### Detail

`refreshDetail` and `detailContent` (`:700-815`) use the shared `repo.Detail` renderer. The
fetch-failure section shows only the selected repo (decision 6).

### TUI goldens (`internal/tui/app/testdata/`, list in `app_test.go:215-221`, `:244`)

- `repos.golden`, `repos_marked.golden`, `repos_range_marked.golden` and
  `repos_marked_fetch.golden`:
  - footer `enter details • s sync • p pull • f fetch …`;
  - marks hint `3 marked — f p s r act on them`.
- `repos_pull_checklist.golden`: the fixture (`app_test.go:74-79`) has `Projects/old`
  detached and `~/.password-store` without an upstream. Both are now skipped, which gives
  `Pull 2 repos (ff-only)`, a `Skipped: …` note, the `[ ] rebase` line, and `r rebase` in
  the footer.
- `repos_filtered_pull.golden` (`state=clean` gives api and old): `Pull 1 repo (ff-only)`,
  with old skipped.
- `full_help.golden` lines 23-26: the `S`, `P` and `x` rows go, `r run` appears, and
  `r refresh` goes.
- `repos_detail.golden`: unchanged, if the shared renderer keeps the labels.
- The `~/.password-store` fixture row can no longer happen, because discovery stays inside
  the workspace. Replace it with an in-workspace repo that has no upstream (this changes
  `repos.golden` and the Dashboard goldens' counts only if the state changes; keep
  `no upstream`).

## Dead code to delete

- `cmd/repo.go`:
  - `isExternalRepo` (`:116-118`);
  - `renderRepoOperation` (`:856-886`, no caller);
  - the `doctor` tombstone (`:262-264`, replaced by the alias);
  - `renderRepoList` (`:689-703`);
  - `renderRepoScan` (`:719-821`);
  - `rebaseOrMerge` (`:682-687`).
- `internal/repo/repo.go`: `Reconcile`, `ReconcileSummary` and `TrackedRepo` (`:76-85`,
  `:433-464`). Only `TestReconcile` (`repo_test.go:167`) uses them; delete it too.
- `ws.json` `repo.max_parallel` (`wsconfig.go:60`, default 8) is read by nothing, and every
  repo command runs serially. Leave it in the schema or drop it; the spec does not mention it.

## Other observations (not changed by the spec)

- `repo.exclude_dirs` entries are workspace-relative *paths* (`workspaceRel`,
  `repo.go:19-45`; matched at `:142-150`). The defaults `node_modules` and `.venv`
  (`wsconfig.go:118`) therefore exclude only `<ws>/node_modules` and `<ws>/.venv`, not nested
  ones. The spec describes this as it is.
- `Discover` keeps walking inside a repo after it finds `.git` (`SkipDir` skips only the
  `.git` dir), so it also walks every repo's working tree. That is slow on big trees, but
  nested repos are found.
- The TUI prints workspace-relative paths (`env.Rel`); the CLI prints absolute paths.
- README.md:31 and :135-142 list `repo scan`, `add-root` and `ls-roots`. dev.md:498 and
  :645-653 describe targets; rule 2 there already says exact match.
