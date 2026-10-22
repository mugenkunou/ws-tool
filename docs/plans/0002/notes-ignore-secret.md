> **Read this first.** These notes were written on 2026-10-22, before the user's last round of
> decisions. Where they disagree with spec.md, **spec.md wins**. Use them for file:line
> locations and today's behaviour. Changed since:
>
> - `ignore ls` lists **ignored paths** with their rule, and `ignore ls --muted` the muted warnings. The warnings list (the old `scan`) is **`ignore lint`**. There is no `--ignored` flag. Where the notes say `ignore ls`, read `ignore lint`.
> - **Mute is warnings-only** (the user's decision). `ignore mute` adds to a new `ws.json` `ignore.muted` list and never writes a harbor or touches `.megaignore`. Legacy user safe harbors stay always-sync, so their warnings stay muted. There is no migration. Notes that equate mute with a safe harbor are void.
> - `ignore add` refuses dotfile originals.
> - `pass backup` mirrors `dotfile backup` exactly, through one shared helper: the same verbs, flags (`--remote`, `--pass-entry`) and `status` layout, `push` commits anything pending, and `disable` clears the remote and removes the new `pass-backup` cron job. There is a new `ws.json` `pass.pass_entry`.
> - `pass status` gets a gentle "workspace copy" info line and tip, and the Dashboard shows a matching advice row.
> - TUI Ignore has three views (Warnings = lint, Muted, Ignored), cycled by `v`. `u` unmutes in Muted. `ctrl+d` is `ignore rm` in Ignored.

# Implementation notes: `ignore`, `secret`, `pass`, `credential`

Spec chapter: spec.md. Code facts below were read on 2026-10-22 from the
working tree, which includes uncommitted edits to `cmd/ignore.go`, `cmd/ignore_extras.go`,
`internal/ignore/engine.go` and `internal/tui/screens/ignore/ignore.go` (`EvalResult.OverriddenBy`,
"already suppressed" wording). Build on those edits; do not revert them.

## Decisions (marked `(decision)` in the spec, plus smaller ones)

| # | Decision | Why |
| --- | --- | --- |
| D1 | **User ignore rules are evaluated last**: built-in excludes → built-in muted dirs → user muted dirs → user excludes → `-s:*`. Today user excludes come *before* every harbor (`internal/ignore/rules.go` `BuildEngine`, `GenerateMegaignore`), so `ignore add` on anything under `artifacts/ configs/ secrets/ data/ archive/` or a muted dir is a silent no-op. | `ignore add` (and the Secrets `i` key, which mostly hits files in `configs/`/`secrets/`) must do what it says. |
| D2 | `ignore ls`, `check` and `tree` warn once on stderr when `<workspace>/.megaignore` differs from `GenerateMegaignore(ws.json rules)`; every RW ignore command rewrites it. | After D1 (or any ws upgrade that changes the template) ws's view and MEGA's file diverge until the next rewrite. `ws init` writes `.megaignore` only if missing (`internal/workspace/init.go:89`). |
| D3 | `ignore ls` (default) omits muted rows and prints their count on stderr; `--muted` shows only them; `--ignored` is a separate row type; `--muted` + `--ignored` is an error. | One row set per view, like the TUI's two levels. |
| D4 | Record field `in_safe_harbor` is renamed `muted` (`internal/ignore/fields.go`, `scan.go` `Violation.InSafeHarbor`). `ignore check` JSON `safe_harbor` → `muted`, and gains `rule_source` (`builtin`/`user`/``). | Schema 3 breaks names anyway; the grammar word is "mute". `rule_source` is needed by `ignore rm` and the check text. |
| D5 | `project-meta` findings under a muted dir are muted (INFO). Today `detectProjectMeta` (`scan.go`) never sets `InSafeHarbor`, so they stay WARNING and cannot be muted. | Consistency; otherwise a Go project in `artifacts/` warns forever. |
| D6 | `ignore add` needs an existing path inside the workspace (glossary: `add` = existing thing). No targets = the path of every unmuted `ignore ls` row (old TUI `X`). Already-ignored paths get no action. | Glossary, rule 4. |
| D7 | `ignore rm` removes only the `ignore_rules.exclude` entry equal to the path. A wider user rule (parent dir, glob) → error naming it. A **built-in rule → error** with two hints: `ignore mute <dir>` or `suppress_defaults` via `ignore edit`. Path need not exist. | `rm` deletes what ws owns; never edit built-ins per path; never widen silently. |
| D8 | `ignore mute` refuses the workspace root and non-directories; already muted (built-in, self, parent) → no action. `ignore unmute` removes the exact `<dir>/**` entry only; muted by a parent → error; **built-in muted dir → refused** (only `suppress_harbors` in `ignore edit`). | Muting the root would force-sync everything (`+g:**`). Turning off `configs/**` would stop `.git` in `configs/dotfiles` from syncing. |
| D9 | `ignore check` exits 0 for an ignored path (was 2); a path outside the workspace is exit 1 (today `ignore.Check` returns a zero `EvalResult` for `../x` and the CLI prints IGNORED with an empty rule). | Rule 6; bug. |
| D10 | `ignore edit` with unchanged rules = nothing to do (today it always saves ws.json and rewrites `.megaignore`), unless `.megaignore` is stale (D2). | Plan semantics. |
| D11 | `secret ls` never writes `ws.json`. Stale-anchor pruning (today a side effect of `runSecretScan`, `cmd/secret.go:135-154`) moves to `secret allow` as its own last action. | RO commands never change anything (§4.7). |
| D12 | `secret ls --json` `data` is the plain records array (was `data.violations` plus `skipped_dirs`, `pass`, `pass_audit`, `git_credential_helper`, `credential_helper_value`). | Same shape as every other `ls`; the extras belong to `pass status` / `pass audit` / `credential status`. |
| D13 | `secret show` = the record + 5 context lines each side (TUI uses 5, CLI fix used 3); the target must be a current finding. | One view in both front ends. |
| D14 | `secret allow|store|show` targets must be current findings; all targets are validated before any plan is built; an invalid one exits 1 with nothing changed. Same for `ignore add|mute|rm`, `secret mute|unmute`. | Predictable; no half-applied argument lists. |
| D15 | `secret store`: no prompt in the CLI (entry = argument or suggestion); **never overwrites an existing pass entry** (today `InsertEntry` uses `pass insert -f`); always allows the finding afterwards (batch `--secret-mode pass` did not; the interactive `[p]` and the TUI did). One action, because allow must not run when insert fails. | Least surprise; no `--force` exists. |
| D16 | `secret unmute` (new) matches `secret.skip_dirs` entries in any written form (relative, absolute, `~`), resolved via `secret.MergeSkipDirs` logic. | Config values may be hand-written. |
| D17 | `--skip-dir` stays on `secret ls` (one-run mute, not saved). | Existing feature and tests. |
| D18 | `pass …` commands do not require an initialized workspace (today `runSecret` calls `requireWorkspaceInitialized` before `setup/status/git`). | The store is per machine. |
| D19 | `pass init` = `pass init <gpg-id>` + `pass git init`. The remote prompt moves to `pass backup enable`. `secret setup --git-remote <url>` (hidden alias) appends the remote action. | Matches §3.4 of the head chapter. |
| D20 | `pass backup enable` without `--remote` and no remote: prompt; `--quiet`/`--json` → exit 1 `--remote <url> is required`. It sets the URL with `set-url` when `origin` exists (today `secret.AddGitRemote` always runs `remote add`, which fails on an existing origin although `runSecretGit` prints "Updating existing remote"). Applies the §7.4 private-remote check, new for pass. | Bug + head §7.4. |
| D21 | `pass backup disable` removes the `origin` remote (`pass git remote remove origin`); keeps store, entries, history and the remote repository. No remote → nothing to do. | Nothing pushes the pass store automatically, so removing the remote is the only meaningful "off". Note: `dotfile backup disable` turns off auto-commit/push and keeps the remote; both mean "no more backups". |
| D22 | `pass backup status` adds `ahead`/`behind` (from the tracking ref, never fetches) and `-n` (default 10; old `secret git log -n` default 20). `commits` is an array of `"<hash> <subject>"`. | Matrix says ahead/behind. |
| D23 | `pass backup push` becomes a one-action plan (today `runSecretGit` "push" runs directly, no plan, no `--dry-run`). | §4.7. |
| D24 | `credential disable` with nothing to do exits 0 (today exit 1, `cmd/credential.go:482`). The wrapper script is left in place. | §4.6 "nothing to do" = 0. |
| D25 | `credential enable` "create pass entry" actions need a terminal; without one they fail (today `runPassInsertInteractive` attaches `os.Stdin` even under `--json`/`--quiet`, which can hang or store garbage). | Safe. |
| D26 | `ws credential get|store|erase` keep working **silently** (today `credential` is a full alias of `git-credential-helper`, `cmd/root.go:89`; `TestCredentialLegacyAlias` covers `credential get`). Unknown ops under `git-credential-helper` stay silent exit 0 when stderr is not a terminal. | Older git configs may say `!ws credential`. |
| D27 | Interactive `ignore fix` and `secret fix` are gone. Typed, they print `ws: "ignore fix" is gone: use ws ignore add | ignore mute | scratch add, or rm` on stderr and exit 1. `secret fix --secret-mode allowlist|exclude|pass` stays a hidden alias for the batch equivalent (table below), with the usual one stderr line. | There is no single successor for an interactive loop; scripts may use the batch modes. |
| D28 | TUI Ignore: `s` (scratch add) works on every row type (today only bloat). The muted view offers `u`, `i`, `s`, `ctrl+d`, `v`; `m` only on the live view. | Every row is a path. |
| D29 | Old `ignore ls` forms keep working hidden: `--path <dir>` / `--rule <r>` imply `--ignored` (`--rule` becomes `--filter rule=<r>`, exact match semantics may differ: note it); `--tree [--depth n]` → `ignore tree`; a positional path → `ignore check`. | The name `ignore ls` is reused, so only the old *flags* can be aliased. |

## Cross-cutting changes

### Rule order and `.megaignore` (D1, D2)

- `internal/ignore/rules.go` `BuildEngine`: move step 2 (user excludes) after step 4 (user
  harbors). `GenerateMegaignore`: same order; section titles become `# Built-in rules`,
  `# Built-in muted directories`, `# Muted directories (ws ignore mute)`,
  `# Ignored paths (ws ignore add)`, `# Never sync symlinks`. Keep the header ("Generated by ws
  — do not edit manually").
- `internal/ignore/engine.go`: give `Rule` a `Source` (`"builtin"`/`"user"`) set in
  `BuildEngine`, and put it on `EvalResult` (`RuleSource`, JSON `rule_source`). Rename
  `EvalResult.SafeHarbor` → `Muted` (JSON `muted`). With D1, when `Muted` is true, `Rule ==
  OverriddenBy` always, so `OverriddenBy` can be dropped or kept internal.
- New `ignore.Stale(megaignorePath string, rules UserRules) (bool, error)`: compares file bytes
  with `GenerateMegaignore(rules)`. Call it from `ignore ls|check|tree` (stderr warning) and from
  `ignore edit` (adds the rewrite action).
- Every RW ignore action calls one helper, e.g. `ignore.Regenerate(workspace, configPath)`
  (today duplicated: `cmd/ignore.go:340-352`, `cmd/ignore_extras.go:574`, `cmd/secret.go:484-488,620-623`,
  `internal/tui/screens/ignore/ignore.go:416 regenerate`, `internal/tui/screens/secrets/secrets.go:470-474`).
- `internal/wsconfig/wsconfig.go`: add `RemoveIgnoreExclude(configPath, pattern) (bool, error)`.
  `AddIgnoreSafeHarbor` still rejects only `"ws"`/`"ws/**"` (old layout); replace with the
  built-in check that lives in the dead `ignore.AddUserSafeHarbor` (`rules.go:111`), i.e. reject
  `ignore.LayoutHarborBlocks` patterns, and reject `""`/`"**"`.
- Tests: `internal/ignore/rules_test.go` `TestGenerateMegaignore`, `TestBuildEngineUserExclude`,
  `TestBuildEngineUserSafeHarbor`; add a test "user exclude inside `configs/` and inside a user
  harbor is ignored"; `internal/ignore/scan_test.go` `TestEngineEvaluateLastMatchWins`,
  `TestEvaluateSafeHarborFlag`, `TestScanSafeHarborDowngradesToInfo`;
  `internal/ignore/harbor_view_test.go` (field rename).
- **Risk:** existing `ignore_rules.exclude` entries that were no-ops start working at the next
  rewrite. The likely ones came from `secret fix --secret-mode exclude` / `[a]` / TUI `e` on
  files under `configs/` or `secrets/` (note text `"secret: excluded by ws secret fix"`). Those
  files will stop syncing to MEGA, including dotfile originals under `configs/dotfiles/` and
  `secrets/dotfiles/` (they then never reach other machines). Suggest: the first rewrite after
  upgrade prints the user excludes that became effective; mention in release notes.

### Muted field and rows (D3, D4, D5)

- `internal/ignore/scan.go`: rename `Violation.InSafeHarbor` → `Muted` (JSON `muted`);
  `detectProjectMeta`/`includePath` must evaluate the candidate and set `Muted` + severity INFO
  when it is under a harbor.
- `internal/ignore/fields.go` `Schema`: key `in_safe_harbor` → `muted`, title "Muted"; doc
  comment `ws ignore scan` → `ws ignore ls`. Add `IgnoredSchema` (fields `path`, `size_bytes`,
  `rule`; columns size, path, rule) and an `ignore.ListIgnored(engine, workspace)` function
  moved out of `cmd/ignore_extras.go` `runIgnoreList` (today an ad-hoc `row` struct, so
  `--filter/--sort/--wide` don't apply).
- `cmd/violations.go` `printIgnoreViolationsSplit`, `printSecretViolations`: delete; `ls` uses
  `renderList` (`cmd/list.go`).

### Shared operations (CLI and TUI call the same code)

Today the TUI re-implements every fix (`excludeAction`, `harborAction`, `unsuppressAction`,
`moveAction`, `deleteAction`, `allowAction`, `passAction`). Put the plan builders in the
domain packages so both front ends get identical actions, IDs and descriptions:

- `internal/ignore`: `AddPlan(paths)`, `RmPlan(paths)`, `MutePlan(dirs)`, `UnmutePlan(dirs)`.
- `internal/secret`: `AllowPlan(anchors)`, `MutePlan(dirs)`, `UnmutePlan(dirs)`,
  `StorePlan(anchor, entry)`, plus `ParseAnchor(workspace, arg) (rel string, line int, err)`:
  resolve the file part per §4.5, split at the **last** `:`; `FindingAt(findings, rel, line)`;
  `RemoveSkipDir(configPath, dir)`; `PruneAllowlist` stays, plus a writer
  `wsconfig.SetAllowlist`.
- `internal/secret` (or a new `internal/credential`): move `isWsHelper`, `helperPathStatus`,
  `gitConfigGet` from `cmd/credential.go` so `pass status`, `credential status` and the TUI
  header share one check. Today `cmd/secret.go:280` and `secrets.go:133` use
  `strings.Contains(v, "ws")`, which matches unrelated helpers.
- `internal/secret/pass.go`: add `SetGitRemote(url)` (add or set-url), `RemoveGitRemote()`,
  `GitAheadBehind() (ahead, behind int, ok bool)`; `GitLog` returns `[]string`; update
  `AuditPassStore` messages (`ws secret setup` → `ws pass init`; `pass git init` → `ws pass
  backup enable`; `pass git remote add origin <url>` → `ws pass backup enable --remote <url>`).
- Action IDs must be absolute paths (§4.5). Today the TUI uses relative ones
  (`"exclude-" + rel`, `"harbor-" + dir`, `"move-" + v.Path`).

### Aliases (hidden; one stderr line `ws: "<old>" is now "<new>"`)

| Old | New | Notes |
| --- | --- | --- |
| `ignore scan [--expand-suppressed\|--expand-harbors]` | `ignore ls` | expand flags accepted and ignored; the line suggests `--muted`. Exit 0 (was 2). |
| `ignore ls --path/--rule/--tree/<path>` | `ignore ls --ignored` / `ignore tree` / `ignore check` | D29 |
| `ignore tree --depth n`, `--path`, positional level | `ignore tree -L n [dir]` | silent flags are fine (not a rename of the command) |
| `ignore fix` | — | D27: stderr list, exit 1 |
| `secret scan [--skip-dir] [--pass]` | `secret ls` (`--pass`: line names `pass audit`) | exit 0 (was 2) |
| `secret fix` (interactive) | — | D27 |
| `secret fix --secret-mode allowlist` | `secret allow` | |
| `secret fix --secret-mode exclude` | `ignore add <file of each finding>` | |
| `secret fix --secret-mode pass` | `secret store <each finding>` | one plan, one action per finding |
| `secret setup [--git-remote <url>]` | `pass init` (+ `pass backup enable --remote`) | D19 |
| `secret status` | `pass status` | |
| `secret git remote <url>` | `pass backup enable --remote <url>` | |
| `secret git status`, `secret git log [-n]` | `pass backup status [-n]` | |
| `secret git push` | `pass backup push` | |
| `git-credential-helper setup\|status\|disconnect` | `credential enable\|status\|disable` | |
| `credential setup\|status\|disconnect` | same as above | `credential` used to alias the whole helper |
| `git-credential-helper get\|store\|erase`, `credential get\|store\|erase` | unchanged | **silent** (D26) |

Risk: the cron scripts `ws … secret scan --quiet` / `ws … ignore scan --quiet`
(`internal/cron/jobs.go:56,66`) are "called by other programs" but would print the alias line
into cron logs. Suggest the alias line is suppressed by `--quiet` (head-level decision).

### CLI equivalents of the old interactive choices

`ignore fix` (`cmd/ignore.go` `runIgnoreFixInteractive`):

| Old choice | Now |
| --- | --- |
| `[m]egaignore` | `ws ignore add <path>` |
| `[s]uppress <parent>` | `ws ignore mute <parent dir>` |
| `[v]` move to scratch (bloat only; `os.Rename` to `scratch.root_dir/<base>`) | `ws scratch add <path>` |
| `[d]elete` (`os.RemoveAll`, permanent) | shell `rm <path>` (trash once `ws trash enable` ran); TUI `ctrl+d` (always trash) |
| `[k]skip`, `[q]uit` | nothing / answer `n` / `q` in the plan prompt |
| `--dry-run` | `--dry-run` on each command |
| TUI `X` (megaignore all) | `ws ignore add` (no targets) |

`secret fix` (`cmd/secret.go` `runSecretFixInteractive`, `runSecretFixBatch`):

| Old choice | Now |
| --- | --- |
| `[v]iew` | `ws secret show <file:line>` |
| `[p]ass` | `ws secret store <file:line> [entry]` |
| `[a]dd .megaignore` | `ws ignore add <file>` |
| `[l]` allowlist | `ws secret allow <file:line>` |
| `[d]ir-skip` | `ws secret mute <dir>` |
| `[s]kip`, `[q]uit` | nothing |
| `--secret-mode allowlist` | `ws secret allow` |
| `--secret-mode exclude` | `ws secret ls --json \| jq -r '.data[].path' \| sort -u \| xargs ws ignore add` |
| `--secret-mode pass` | `ws secret store` per finding |
| `--skip-dir <d>` | `ws secret ls --skip-dir <d>` (one run) or `ws secret mute <d>` |

### Help, completion, parity

- `cmd/root.go` `printHelpStyled` (line ~495): drop `git-credential-helper` and the
  "Alias for git-credential-helper" row; add `pass` ("The pass password store") and
  `credential` ("ws as git's credential helper"); `ignore` → "What MEGA syncs: rules, muted
  directories, warnings"; `secret` → "Secret findings in workspace files".
- `cmd/root.go` `Execute` switch: add `case "pass"` (new `cmd/pass.go`, `runPass`); split `case
  "credential", "git-credential-helper"` into `runCredential` (enable/disable/status + silent
  plumbing ops) and `runGitCredentialHelper` (plumbing + aliases).
- Help structs: `ignoreHelp`, `secretHelp` rewritten; new `passHelp` (with the `backup`
  sub-noun), `credentialHelp` (user commands only). Remove `secretFixHelp`, `secretSetupHelp`,
  `secretGitHelp`. Today `ws secret fix --help` prints the `secret` help because `runSecret`
  checks `hasHelpArg(args)` before dispatch (`cmd/secret.go:52`): per-verb help must be
  checked after dispatch.
- `cmd/complete.go`: `topLevelCommands` add `pass`, `credential`, remove `git-credential-helper`;
  `completers`: `ignore` → `ls add rm mute unmute check tree edit`, `secret` → `ls show allow
  mute unmute store`, `pass` → `init status audit backup` (+ `backup` → `enable disable status
  push`), `credential` → `enable disable status`. Dynamic targets: `ignore rm` from
  `ignore_rules.exclude` (absolute) + files; `ignore unmute` from `safe_harbors` (absolute dirs);
  `secret unmute` from `secret.skip_dirs`; `secret allow|show|store` from current findings as
  absolute `file:line`. **Risk:** finding completion runs a full scan per TAB; cache it or
  only complete once the file part matches a file. `commandFlags` (`case "ignore"`,
  `"secret"`, `"git-credential-helper"`): new flags `--muted --ignored --filter --sort --wide`
  (ls), `-L` (tree), `--editor` (edit), `--skip-dir` (secret ls), `--remote` (pass backup
  enable), `-n` (pass backup status), `--dry-run` on RW.
- `cmd/parity_test.go`: `commandHelps` add `pass`, `credential`; drop `git-credential-helper`
  special case in `TestCompletersOfferEverySubcommand`; `cliCommands`/`subcommands` only
  understand `top sub` and must learn sub-nouns (`pass backup …`, shared with `dotfile
  backup` and `repo root`).
- Tests: `cmd/complete_test.go` (lines 91, 121, 169 `ignore scan --`, 205, 501);
  `cmd/root_test.go:153-173` (help rows); `cmd/credential_test.go`
  `TestCredentialHelperHelpOutput` ("User commands"/"Git plumbing"),
  `TestCredentialHelperInTopLevelHelp`, `TestCredentialAliasNoArgs` (expects git-credential-helper
  usage from `ws credential`), `TestCredentialNoArgs*`.

### Cron (built-in jobs dropped)

- `internal/cron/jobs.go`: delete `secretScanJob`, `ignoreScanJob`. `internal/cron/cron.go`:
  `Builtins` drop `"secret-scan"`, `"ignore-scan"`; `Presets["maintenance"]` =
  `{"ignore-scan","log-prune"}` → session-prune only (coordinate with the cron agent's
  rename). `cmd/complete.go` `cronJobNames`. Tests: `cmd/cron_test.go:54`,
  `internal/cron/cron_test.go:107`.
- Risk: existing crontab lines `ws cron run secret-scan|ignore-scan` (cron agent decides what
  `run` does with a dropped job). Their old "fail on findings" signal (exit 2) is gone anyway.

### Dead code to delete

- `internal/ignore/fix.go` (`Fix`, `AddRules`, `readLines`, `FixOptions`, `FixResult`: only
  `cmd/ignore.go:195` uses `FixResult`) and `internal/ignore/fix_test.go`.
- `internal/ignore/rules.go`: `UserRulesPath`, `LoadUserRules`, `SaveUserRules`,
  `AddUserExclude`, `AddUserSafeHarbor` (the old `ws/ignore.json` file) and their tests in
  `rules_test.go` (`TestUserRulesLoadSaveRoundTrip`, `TestLoadUserRulesMissingFileReturnsDefault`,
  `TestAddUserExcludeDeduplicate`). Fix stale comments (`rules.go:12`, `defaults.go:5`,
  `cmd/ignore.go:25` "ws/ignore.json").
- `internal/secret/fix.go` `FixMode`, `FixOptions`, `FixResult`; `internal/secret/fixops.go`
  deprecated `AllowlistMap`, `AddToAllowlist`, `TrackPassEntry`; header comment "Mutations
  behind `ws secret fix`".
- `cmd/ignore.go` `ignoreScanExit`, `runIgnoreFixInteractive`; `cmd/secret.go`
  `secretScanResult`, `runSecretFix`, `runSecretFixBatch`, `runSecretFixInteractive`,
  `trackPassEntry`, `isInSkippedDir`, `gitConfigValue`; `promptChoice` if nothing else uses it
  (also used in `cmd/plan.go`, keep).

## Per command

### `ignore ls`

- Today: `cmd/ignore.go` `runIgnore` case `"scan"` (warnings, split printer, exit 2 via
  `ignoreScanExit`); `cmd/ignore_extras.go` `runIgnoreList` (excluded files, `--tree`,
  `--depth`, `--path`, `--rule`, positional → check).
- Change: new `case "ls"` with `--muted`, `--ignored`, list flags; `renderList(…, "ignore.ls",
  ignore.Schema …)` filtered by `Muted`; stderr muted count; `--ignored` → `IgnoredSchema`.
  Stale warning (D2). Exit 0.
- Tests: `cmd/ignore_dotfile_restore_test.go` `TestIgnoreExtraCommands` (`ignore ls` now
  warnings; `ls --tree --depth 1` alias). New: default vs `--muted` vs `--ignored` records;
  `--muted --ignored` error; project-meta muted (D5).
- Risk: `ws ignore ls` and `ws ignore ls --json` change meaning with no possible alias; scripts
  that grepped the excluded list get warnings.

### `ignore add`

- Today: `wsconfig.AddIgnoreExclude` called from `runIgnoreFixInteractive` `[m]`,
  `runSecretFixBatch` "exclude", `runSecretFixInteractive` `[a]`, TUI `excludeAction` (both
  screens), `openExcludeAll`.
- Change: `ignore.AddPlan`; target resolution with `wsconfig.ExpandUserPath`, must exist and
  be within the workspace (`repo.IsWithin`), store `filepath.ToSlash(rel)`; skip already
  ignored (engine `Evaluate`). Note: a path whose name contains `*` becomes a glob in
  `userPatternToRule`; refuse such names or escape them.
- Tests: new `cmd/ignore_add_test.go`: adds, regenerates, inside `configs/` takes effect (D1),
  no targets = all unmuted warnings, invalid target → exit 1 and ws.json untouched.

### `ignore rm` (new)

- Change: `ignore.RmPlan` + `wsconfig.RemoveIgnoreExclude`; classify via `Evaluate` +
  `RuleSource` (D7). Exit 1 for the refusal cases.
- Tests: exact entry removed; parent rule → exit 1 naming it; built-in (`-g:*.csv`) → exit 1
  with both hints; still-ignored-after note.

### `ignore mute` / `ignore unmute`

- Today: `wsconfig.AddIgnoreSafeHarbor` / `RemoveIgnoreSafeHarbor` from `runIgnoreFixInteractive`
  `[s]` and TUI `harborAction`/`unsuppressAction`. The TUI `unsuppressAction` guesses
  `parent(row)/**`, so it silently does nothing when the harbor is an ancestor or built in.
- Change: `ignore.MutePlan`/`UnmutePlan`. Unmute from a row: use `Evaluate(row).Rule` (the
  harbor that matched) and its `RuleSource`.
- Tests: mute stores `<dir>/**` and regenerates; root refused; file refused; already muted by
  parent → no action; unmute built-in refused; unmute via parent → exit 1.
- Risk (spec states it): muting force-syncs what built-in rules exclude, e.g. `.git`, `.venv`,
  `node_modules` under the muted dir start uploading to MEGA.

### `ignore check`

- Today: `cmd/ignore_extras.go` `runIgnoreCheckPath` (exit 2 for ignored, JSON `safe_harbor`).
- Change: exit 0 (D9); outside workspace → 1; text "Reason:" lines per spec; JSON keys
  `path, included, rule, rule_source, muted, size_bytes`; "File size" → "Size".
- Tests: `cmd/ignore_check_tree_test.go` `TestIgnoreCheckExitCodeIgnored` (2 → 0),
  `TestIgnoreCheckJSON`, `TestIgnoreCheckIgnored`; `cmd/paths_test.go:156-185`.

### `ignore tree`

- Today: `runIgnore` case `"tree"` + `runIgnoreTreeView`. Already matches; keep `--depth`,
  `--path`, 2nd positional as hidden; add the D2 warning and "outside workspace" stays exit 1.
- Tests: `TestIgnoreTree*` unchanged except flag spelling if you switch them to `-L`.

### `ignore edit`

- Today: `runIgnoreEdit` (temp file `<workspace>/.ignore-edit.json`, always saves).
- Change: compare before/after (and staleness), build a one-action plan (D10); `--dry-run`
  prints it. JSON `ignore.edit` keeps `stats`.
- Tests: `TestIgnoreExtraCommands` `--editor true` → nothing to do, exit 0.

### `secret ls`

- Today: `cmd/secret.go` `runSecretScan` (prunes allowlist, prints pass + credential sections,
  exit 2 with findings).
- Change: `renderList(…, "secret.ls", secret.Schema …)`; drop pruning (D11), health sections,
  `--pass`. Keep `--skip-dir` via `skipDirFlagsToRel`/`mergeSkipDirs`.
- Tests: `cmd/ignore_secret_test.go` (exit 2 → 0), `cmd/ignore_secret_skip_dir_test.go`
  (all four: exit codes, `skipped_dirs` key gone → assert the vendor finding is absent),
  `cmd/paths_test.go:190-231` (`data.violations` → `data`).
- Risk: `--json` records include `snippet`, i.e. the secret itself (as today).

### `secret show` (new as a command)

- Today: `secret.GetFileContext` used by `[v]` (3 lines) and TUI `viewContext` (5 lines).
- Change: `cmd/secret.go` `case "show"`; `ParseAnchor` + `FindingAt`; JSON `context`.

### `secret allow`

- Today: `wsconfig.AddToAllowlist` from batch/interactive fix and TUI `allowAction`.
- Change: `secret.AllowPlan`; prune action from `secret.PruneAllowlist` on an allowlist-free scan.
- Tests: rewrite `TestSecretFixAllowlistBatch`, `TestSecretFixInteractiveAllowlist`,
  `TestSecretFixJSON`, `TestSecretFixDryRun`, `TestSecretFixNoViolations` in
  `cmd/ignore_secret_fix_test.go`; add stale-drop action test (from `PruneAllowlist` tests).

### `secret mute` / `secret unmute`

- Today: `secret.AddSkipDir` (`fixops.go`) from `[d]` and TUI `stepSkipDirInput`. No remove.
- Change: `MutePlan` with "already muted by parent" check (`matchesSkipDir` logic), new
  `RemoveSkipDir` (D16).

### `secret store`

- Today: `secret.SuggestPassEntry`, `ExtractSecretValue`, `InsertEntry` (`pass insert -f` /
  `-m`) from `[p]`, batch "pass", TUI `passAction`.
- Change: `StorePlan`: check `secret.CheckPass().Initialized`; `PassEntryExists(entry)` →
  fail (D15); insert without `-f` (still via stdin); then `AddToAllowlist`. JSON `data.entry`.
  Final hint text.
- Tests: no pass on the test image, so test the plan building with a fake (inject the insert
  function) and the refusal paths (no value, existing entry, store not initialized).

### `pass init`

- Today: `cmd/secret.go` `runSecretSetup` (GPG pick via `promptLine`, actions `pass-init`,
  `pass-git-init`, `pass-git-remote`, remote prompt).
- Change: new `cmd/pass.go` `runPassInit` without the remote part; no workspace required
  (D18). Hidden `--git-remote` on the `secret setup` alias path.
- Tests: `TestSecretSetupAlreadySetUp` → `pass init`.

### `pass status`

- Today: `runSecretStatus` (requires workspace; credential check by substring).
- Change: `runPassStatus`; shared helper check; hints per spec. Keep JSON keys.

### `pass audit`

- Today: `secret.AuditPassStore` via `secret scan --pass` and TUI `audit()`.
- Change: `runPassAudit`; message texts. Tests: `internal/secret/pass_test.go`
  `TestAuditPassStore*` (message strings, if asserted).

### `pass backup enable | disable | status | push`

- Today: `runSecretGit` (`remote`, `status`, `log`, `push`); `secret.AddGitRemote`,
  `GitRemoteURL`, `GitLog`, `GitPush`.
- Change: `runPassBackup` with the four verbs; D20–D23; private check with
  `repo.CheckRepoVisibility(url, secret.ResolveGitToken(url, ""))` as `cmd/dotfile.go:451` does.
- Risk: the §7.4 rule "refuse when visibility cannot be determined" on push would block
  existing pass backups to self-hosted remotes (`CheckRepoVisibility` returns `Checked:false`
  for unknown hosts). Today pass pushes never check.

### `credential enable | disable | status`

- Today: `cmd/credential.go` `runCredentialSetup`, `runCredentialDisconnect`,
  `runCredentialStatus`.
- Change: rename to `runCredentialEnable/Disable/Status`; D24, D25; messages that name
  `ws secret setup` (`credential.go:116, 316`) → `ws pass init`, and `ws git-credential-helper
  setup` (`354, 368, 373`, `cmd/secret.go:313`) → `ws credential enable`. Plan `Command`
  strings → `credential.enable` etc.
- Tests: `TestCredentialDisconnectNotConnected` (exit 1 → 0, new name), help tests above.
- Risk: the wrapper script (`installCredentialWrapper`) and existing gitconfigs call `ws
  git-credential-helper`; that name must never change. `credential.useHttpPath` removal on
  disable may undo a setting the user made themselves (it is its own action, so it can be
  skipped).

## TUI

### Keys (`internal/tui/keys/keys.go`)

- `ListMap.Detail` is `i` today (`keys.go:710`, "all fields"); it must move to the global `d`
  before Ignore and Secrets can bind `i`. `MarkAll` `A` → `ctrl+a`, refresh `r` → `ctrl+r`
  (global changes, other chapters).
- `IgnoreMap`: remove `Exclude a`, `ExcludeAll X`, `Check c`, `ViewSuppressed H`, `Tree t`,
  `List l`, `EditRules E`, `Delete d`, `Move m`, `Suppress s`, `Refresh r`. New: `Ignore i`
  ("ignore"), `Mute m` ("mute dir"), `Scratch s` ("move to scratch"), `Delete ctrl+d` ("delete",
  global binding), `Edit e` ("edit rules"), `Muted v` ("muted" / "back"), `Unmute u` ("unmute").
  Short help (100 columns): `i ignore • m mute dir • s scratch • ctrl+d delete • v muted`.
- `SecretsMap`: remove `Allowlist l`, `AllowAll L`, `Exclude e`, `StorePass p`, `SkipDir D`,
  `Audit a`, `PassPush g`, `PassLog v`, `PassRemote o`, `Setup S`, `CredSetup C`,
  `CredStatus c`, `CredDisconnect X`, `Refresh r`. Keep `View enter` (help "show"). New:
  `Allow a`, `Ignore i`, `Mute m`, `Store s`. Short help: `enter show • a allow • i ignore file •
  m mute dir • s store in pass`.
- Package doc (keys.go:1-25) still describes `r`, `i`, `A`.

### Ignore screen (`internal/tui/screens/ignore/ignore.go`)

- Levels: `levelLive` / `levelSuppressed` → `levelWarnings` / `levelMuted`; `split()` on
  `Violation.Muted`. `v` toggles; `esc` in the muted view returns (today `Back` does this).
- `liveActions`: `i` → `ignore.AddPlan` (Selected); `m` → `ignore.MutePlan` of each row's
  parent (Selected; a top-level row is left out with a notice); `s` → the shared `scratch add`
  plan (Selected, every row type; today `moveAction` does `os.Rename` to
  `ScratchDir/<base>` and only for bloat); `ctrl+d` → `trash.SoftDelete` per row (Selected;
  **today `deleteAction` uses `os.RemoveAll`, ignore.go:508, a permanent delete**); `e` →
  `handover.WS(…, "ignore", "edit")` (scope none); `v` → muted level (scope none).
- `suppActions` → muted-level actions: `u` → `ignore.UnmutePlan` from each row's matching
  harbor rule (built-in rows left out, notice), plus `i`, `s`, `ctrl+d`, `v` (D28).
- "Already muted" must not be a failed action: the working-tree change in `harborAction` returns
  `errors.New("already suppressed")`. Instead leave such rows out of the checklist at build time
  (`Evaluate(dir).Muted`) and say so in the notice.
- `:` commands: `ls --ignored` → `handover.Paged(handover.WS(…, "ignore", "ls", "--ignored"))`
  (today key `l` runs plain `ignore ls`); `rm <path>` → `ignore.RmPlan` checklist; `check <path>`
  → existing `check()` notice (today behind `c` + prompt); `tree [dir]` → paged handover (today `t`).
- `check()` texts: align with the CLI reasons (`IGNORED … rule `-g:*.csv` (built-in)`,
  `SYNCED … muted directory `+g:archive/**``, `SYNCED … no rule matches`).
- `statusLine()`: `"%d violation(s)"` → `"%d warning(s)"`; `"%d suppressed (H to view)"` →
  `"%d muted (v to view)"`; muted level `"Suppressed warnings · n item(s) · u unsuppress · esc
  back"` → `"Muted warnings · n · u unmute · v back"`. Build key labels from the keymap
  (`m.keys.Muted.Help().Key`), never literals.
- `severityStyle`: label `"suppressed"` → `"muted"`. Empty-state text "No suppressed warnings."
  → "No muted warnings."; "Squeaky clean — no sync hygiene violations." → "… no sync warnings."
- `KeyMap()`: the muted level must show its own keys (golden `ignore_harbors_shown.golden`
  shows the live level's `a add to .megaignore • s suppress warning …` footer there today).
- `LoadedMsg.Actionable()` → `Unmuted()` (used by `dashboard.go` `ignoreArea`).
- Plan `Command: "ignore.fix"` (`openFixMulti`, `openExcludeAll`) → the real command name per
  plan (`ignore.add`, `ignore.mute`, `ignore.unmute`, `ignore.rm`, `scratch.add`).
- Package doc comment (lines 1-6).

### Secrets screen (`internal/tui/screens/secrets/secrets.go`)

- `actions`: `enter` → `viewContext` (keep 5 lines, title `abs:line`); `a` → `secret.AllowPlan`
  (Selected; drop `AllowAll L`); `i` → `ignore.AddPlan` of the rows' files, deduplicated (today
  `excludeAction` with note `"secret: excluded by ws secret fix"`: drop the note); `m` → prompt
  (Cursor) pre-filled with the finding's directory, then `secret.MutePlan`; `s` →
  `promptStorePass` (Cursor) then `secret.StorePlan` (existing entry fails, D15).
- `:` commands: `unmute <dir>` (checklist); `pass init` → `m.wizard("pass", "init")`; `pass
  status` → detail view; `pass audit` → detail view (`audit()`); `pass backup enable` →
  `m.wizard("pass", "backup", "enable")`; `pass backup disable` → checklist; `pass backup status`
  → detail view (replace `passLog()` with status + ahead/behind + commits); `pass backup push` →
  checklist (`stepPassGit`); `credential enable|disable` → `m.wizard("credential", …)`;
  `credential status` → paged handover. Today these are `S`, `a`, `v`, `o`, `g`, `C`, `c`, `X`,
  and they call `secret setup` / `git-credential-helper …`, which become aliases.
- Remove steps `stepRemoteURL` (the URL prompt moves into the `pass backup enable` wizard).
- Notices with hardcoded keys (all to `:` commands): `secrets.go:316` and `:330` "Pass store is
  not git-backed — press S to run setup" → "Pass store has no git: run :pass backup enable";
  `:322` "Pass store has no git remote — press o to set one" → "Pass store has no remote: run
  :pass backup enable --remote <url>"; `:371` "Pass store not initialized — press S to run
  setup" → "Pass store not initialized: run :pass init". Prompt hint `:307` "Saved to
  secret.skip_dirs in config." → "Muted directories are not scanned (secret.skip_dirs)."
- `Load()`: credential detection `strings.Contains(v, "ws") || …git-credential-helper`
  (`:131-134`) → the shared helper status. Header (`healthLine`) is the `pass status` summary;
  keep its format.
- Plan `Command: "secret.fix"` (`confirmOne`, `confirmEach`) → `secret.allow`, `ignore.add`,
  `secret.mute`, `secret.store`, `pass.backup.push`, …
- Package doc comment (lines 1-8).

### Other screens and shared pieces

- `internal/tui/screens/dashboard/dashboard.go` `ignoreArea`: "violation" → "warning",
  `"%d in safe harbors"` → `"%d muted"`, issue `"sync hygiene violation (bloat, depth, build
  artifacts)"` → `"sync warning (bloat, depth, build output)"`. `secretsArea` unchanged.
- Out of my group but same pattern: `internal/tui/screens/dotfiles/dotfiles.go:231,617,620`
  "press G to set up / run setup" (Dotfiles agent).
- `internal/tui/app/coverage.go`: replace the `ignore …`, `secret …` and
  `git-credential-helper …` entries with: `ignore ls` (screen), `ignore add` (`i`),
  `ignore rm` (`:rm`), `ignore mute` (`m`), `ignore unmute` (`u`), `ignore check` (`:check`),
  `ignore tree` (`:tree`), `ignore edit` (`e`); `secret ls` (screen), `secret show` (enter),
  `secret allow` (`a`), `secret mute` (`m`), `secret unmute` (`:unmute`), `secret store` (`s`);
  `pass init|status|audit|backup …` (`:pass …`; status also the header); `credential
  enable|disable|status` (`:credential …`); `git-credential-helper get|store|erase` (plumbing
  note). `Coverage` has only `Key` or `Note`, so it needs a field for `:` commands (TUI agent);
  `TestCoverageKeysExist` (`internal/tui/app/coverage_test.go:26`) must accept them.
- Goldens (`internal/tui/app/testdata`, driven by `app_test.go:233-237`): `ignore.golden`
  (status line, column label "muted", footer), `ignore_harbors_shown.golden` → keys `"6","v"`
  (rename `ignore_muted_shown`), `ignore_marked_exclude.golden` → `"6","space","down","space","i"`
  (rename `ignore_marked_ignore`), `secrets.golden` (footer), `secrets_detail.golden` → `"7","d"`
  (describe moved off `i`). `app_test.go:97-104` fixture uses `Violation{…}` fields
  (`InSafeHarbor` rename).

## Risks (summary)

1. D1 turns old no-op excludes into real ones; files under `configs/`/`secrets/` (including
   dotfile originals) may stop syncing after the first `.megaignore` rewrite.
2. Until something rewrites `.megaignore`, MEGA follows the old order; D2 warns.
3. `ws ignore ls` / `ws ignore ls --json` change meaning without an alias.
4. Exit code 2 is gone from `ignore scan`, `ignore check`, `secret scan`, `secret fix`; any
   script or cron alert that keyed on it goes quiet.
5. `ignore mute` force-syncs what built-in rules exclude (`.git`, `.venv`, `node_modules`) under
   the directory. The glossary says mute only stops warnings; the spec states both effects.
6. `ws credential …` used to alias the whole helper; plumbing (`get|store|erase`) must stay
   silent under both names, and `git-credential-helper` must keep its name forever (wrapper
   script, gitconfigs).
7. The private-remote check (§7.4) is new for the pass store and may refuse remotes that push
   fine today.
8. `secret store` changes from overwrite (`pass insert -f`) to refuse-if-exists.
9. Completion of `file:line` targets is a full secret scan per TAB.
10. The `secrets/` block is scanned like any other synced directory, so secret-class dotfile
    originals in `secrets/dotfiles/` show as findings until `secret mute` or `allow`. Not changed
    here; worth a product decision (for example, `ws init` seeding `secret.skip_dirs`).
