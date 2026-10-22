> **Read this first.** These notes were written on 2026-10-22, before the user's last round of
> decisions. Where they disagree with spec.md, **spec.md wins**. Use them for file:line
> locations and today's behaviour. Changed since:
>
> - `scratch grep` is removed, and `scratch search` aliases `scratch ls --filter`.
> - `scratch tag` has no suggestions: tags are human labels. `--rm` removes one. With no tags, ws prompts on a terminal and otherwise gives a usage error. The notes' suggestion design is void.
> - Editor: `ws.json` `editor` (default empty → `$VISUAL` → `$EDITOR` → `vim`) replaces `scratch.editor_cmd`, which is **no longer read**.
> - Sessions `:grep` narrows the list. It is not a results view.

# Implementation notes: `scratch` and `session` (replaces `log`)

Spec chapter: spec.md. Line numbers are against `master` at b77d3c8
(with the uncommitted ignore changes in the tree). Tests are not HOME-safe on the host.
Run them in Docker only, because `scratch rm` soft-deletes into the real XDG trash.

## Decisions made in the spec (all marked `(decision)` there)

1. **Scratch name resolution** (`open`, `tag`, `rm`): an exact name wins, else a
   case-insensitive substring that matches exactly one directory. Zero or several matches
   is exit 1 with the candidates listed. Today `open`/`tag` take the first alphabetical
   substring match, and `scratch.Delete` **deletes every directory whose name contains the
   argument** (`internal/scratch/scratch.go:141-152`). The TUI's delete uses the same
   function, so deleting `foo` also deletes `foo-2` and `myfoo`.
2. **A missing required name** (`open`, `tag`, `rm`): the ghost prompt stays on a TTY
   (the `wsopen` shell function needs it). Without a TTY it is a usage error (exit 1).
   Today the non-TTY fallback reads a line from stdin.
3. **`scratch new [name]`** takes one argument. Today `strings.Join(fs.Args(), " ")`
   (`cmd/scratch.go:70`) makes names with spaces, and it would swallow trailing flags (see
   the flag-parsing risk below).
4. **`scratch add`**: a directory keeps its base name. A file is wrapped in a new
   directory named after the file without its last extension (`capture.pcap` →
   `<root>/capture/capture.pcap`), because `scratch.List` lists only directories. A name
   that would be empty keeps the full name. Collisions get `-2`, `-3` (`uniqueName`). No
   date suffix, no `.ws-meta.json`. Refused before any move: the path does not exist; it
   is the scratch root or inside it; it is an ancestor of the scratch root or the workspace
   root; it is `ws.json`; it is a `links[].original`. Symlinks move as links. Cross-device
   moves copy, then remove.
5. **`scratch tag`**: `--rm <tag>` (repeatable) removes. Comma-separated arguments split.
   Explicit tags make one plan action per directory. No tags makes one action per
   suggested tag (the old `--auto`), which `--quiet`/`--json` accept and `--dry-run` only
   lists. The vocabulary (`scratch.tags`) only grows. TUI prompt: `-tag` removes, the
   prompt is pre-filled with the suggestions when one row is selected, and an empty submit
   opens the per-tag suggestion checklist. `GhostMultiInput` is no longer used.
6. **`scratch grep`**: content only, per matching line, literal case-insensitive
   substring (arguments joined by spaces, the same as `session grep`). Skips `.git`,
   `.ws-meta.json`, binary files and files over 512 KB. Drops today's 50-files-per-directory
   cap (`search.go:249`). Lines are cut at 120 characters, as today.
7. **The editor launch** in the CLI follows the TUI's `handover.Editor`: split
   `editor_cmd` into fields, and run terminal editors (`handover.terminalEditors`) in the
   foreground. Today `exec.Command(*editor, path).Start()` (`cmd/scratch.go:118, 546,
   558`) breaks `code -n` and leaves `vim` without a terminal.
8. **The active-recording marker** moves from `<workspace>/data/ws-log/active.json`
   (synced by MEGA, because `data` is a layout harbor block: `internal/ignore/defaults.go:76`)
   to `wsconfig.StateDir()/session-active.json`. The wsconfig package comment
   (`internal/wsconfig/wsconfig.go:7`) already claims "log-active.json" is machine-local.
   Reason: on machine B, a synced marker blocks `record`, and `stop` SIGTERMs a PID from
   machine A. A legacy marker at the old path is finalized by `session stop` **without
   signalling**, then deleted. `ls`/`record` ignore the legacy file. **The head chapter's
   §2.1 machine-local row should list it.**
9. **`session record`** has no plan (head §4.7 says so, which overrides today's
   one-action plan). No TTY means exit 1 and nothing is created (today: "metadata-only
   mode", `cmd/log.go:87-95`, which leaves a dangling active session). A stale marker
   (PID not alive) is finalized, then recording starts. An existing tag gets `-2`
   (script(1) truncates `-O` files, so a re-used tag loses the old transcript today). A tag
   may not contain `/` or start with `.` (today `Start` does no validation, so
   `--tag ../../x` escapes `data/ws-log`). `--quiet` = no banner, no summary.
   `--quiet-start` is a hidden synonym. `--json` prints its envelope after the shell ends.
   `--dry-run` prints the directory.
10. **No raw input log.** Drop `script -I stdin.log` (`cmd/log.go:122`). Today two writers
    append to `stdin.log`: script's raw PTY input (escape codes, **and passwords typed at
    sudo/ssh prompts**, synced to MEGA) and the history hook. That is why the commands view
    and the `commands` count are noisy. After the change, `stdin.log` holds only hook
    lines.
11. **Indicator** `● ws:rec ` (red dot). With colour off it is `(*) ws:rec `, which is
    §7.2's ASCII fallback for `●`, so one dedupe check (`*"ws:rec"*`) covers both. Keep the
    env var `WS_LOG_RECORDING=1` unchanged (prompt themes may test it).
12. **`session show`** prints `stdout.log` only, with escapes and `\r` stripped.
    `--commands` prints `stdin.log`. Today `wslog.Show` "full" mode concatenates stdin.log
    and stdout.log (`internal/log/log.go:195-203`).
13. **`session grep`** searches only `stdin.log`/`stdout.log`, with escapes stripped.
    Today the TUI uses `internal/search.Run` over the whole log dir, which also hits
    `meta.json`, `active.json`, `.ws-init.sh` and `.zshrc`. TUI `:grep` keeps a results
    view (Session/Line/Text). `enter` opens the viewer scrolled to the line, which is new
    (today it opens at the top).
14. **`session rm` and `session prune` soft-delete** to the trash (§7.3 says only the trash
    commands delete permanently). Today `wslog.Remove` uses `os.RemoveAll`
    (`internal/log/log.go:249`).
15. **`session prune` default** = cap: if the total size (all sessions, the active one
    included) is over `cap_mb`, delete the oldest finished sessions until it is under. A
    cap of 0 or less means no cap. `--all` with `--older-than` is exit 1. Today a bare
    `prune` removes nothing (`cmd/log.go:276-296`), so the nightly `log-prune` cron job is
    a no-op although its description says it enforces `log.cap_mb`.
16. **`ws.json` `log` → `session`** without bumping `wsconfig.CurrentSchema` (a bump makes
    older binaries on other machines refuse to load: `wsconfig.go:256`). The old binary
    ignores the unknown `session` key and falls back to defaults, which is acceptable.

## Shell integrations: what references `ws log` / scratch

- **Nothing in any rc file calls `ws log`.** I grepped every writer:
  - `ws completions install` (`cmd/completions.go:424-426`) writes
    `eval "$(ws completions bash|zsh)"`. The generated script (`completions.go:78-224`)
    defines `_ws_completions` (calls `ws __complete`) and **`wsopen`**, which calls
    `ws scratch open --print-path "$@"`. `scratch open` and `--print-path` keep their
    names, so `wsopen` keeps working. Keep `--print-path`'s stdout contract (path only;
    status and prompt on stderr). `TestScratchOpenPrintPath` covers it.
  - `ws trash enable` writes `alias rm='ws-trash-rm'` (`internal/trash/trash.go:543-548`).
    No ws call.
  - The recorded shell's init (`cmd/log.go:398-502`) is generated per session into the
    session dir. It calls no `ws` command. The `_ws_log_*` function names are internal to
    it, and renaming them to `_ws_rec_*` is optional.
- **Flag for the workspace/completion agent:** the rc line runs `ws completions <shell>`
  on every shell start. When `completions` becomes a hidden alias of `completion`, that
  alias **must be silent**, or every new terminal prints the rename line. It is a shell
  integration (grammar rule 5). Rewriting the rc line on `completion enable` is the clean
  fix.
- `WS_LOG_RECORDING=1` (`cmd/log.go:413-428`) is the only env surface. It is kept.

## `ws scratch`: per command

Shared: `cmd/scratch.go:runScratch` (one big switch). Update `scratchHelp` (`:19-30`) to
`<ls|new|add|open|tag|grep|rm>`. Use `parseInterspersed` (`cmd/root.go:220`, currently
**unused anywhere**) so that `ws scratch new foo --no-open` works. Today Go's `flag` stops
at the first positional, so the test `scratch new demo-scratch --no-open`
(`cmd/log_scratch_test.go:63`) creates `"demo-scratch --no-open"`… and tries to launch
`code`. Add a shared `resolveScratch(root, name) (Entry, error)` that implements decision 1,
in `internal/scratch` (for example `scratch.Resolve`).

### `scratch ls`
- Today: `cmd/scratch.go:125-142`, `scratch.List` (`internal/scratch/scratch.go:85`),
  `scratch.Schema` (`fields.go`). JSON `scratch.ls`. The legacy `--sort size` mapping
  (`:134`) stays.
- Change: none beyond the envelope schema 3. Optional: `age` is a `time.Duration` (JSON
  nanoseconds), so consider a `modified_at` timestamp field (not in the spec).

### `scratch new`
- Today: `cmd/scratch.go:56-124`. Plan description `"Create scratch directory"` has no
  path (`:102`), and the path is computed inside Execute.
- Change: compute the final path first (`scratch.New` with `DryRun: true`, or export
  `uniqueName`), and describe the action as `Create <abs path>`. Take one positional
  name. Launch the editor through a shared helper that mirrors `handover.Editor` (split
  fields, foreground for terminal editors). `--json` still skips the editor (`:110-112`).
- TUI `openNew` (`internal/tui/screens/scratch/scratch.go:409-425`) describes
  `"Create scratch directory " + name + " in ~/Scratch"`. Align it with the absolute path.
- Tests: `TestScratchCommandsFlow` checks for `"Create scratch"` in the output
  (`log_scratch_test.go:66`). Update it. `TestScratchNewEndToEnd`
  (`internal/tui/app/app_test.go:686`) checks `"Create scratch directory e2e-demo"`.

### `scratch add` (new)
- Today: nothing in `cmd`. The Ignore screen's `moveAction`
  (`internal/tui/screens/ignore/ignore.go:479-492`) and `ignore fix [v]`
  (`cmd/ignore.go:303-320`) do `os.Rename(src, <root>/<base>)`. That has no collision
  handling (a file silently replaces a same-named file), no EXDEV fallback, and puts a
  bare file in the root, where `scratch ls` never shows it.
- Change: add `scratch.Add(AddOptions{RootDir, Path, DryRun}) (AddResult{Source, Path},
  error)` in `internal/scratch` with decision 4. For the EXDEV fallback, reuse
  `trash.renameOrMove` (`internal/trash/trash.go:114`, unexported) or
  `dotfile.movePath` (`internal/dotfile/dotfile.go:859`). Export one shared mover rather
  than writing a third. The refusal list needs `ws.json` `links[]` (pass the loaded cfg) and
  the workspace root. The CLI case `add`: one action per path, JSON `scratch.add`
  `{moved, actions}`. The Ignore agent should switch `s` to call `scratch.Add`, so one
  implementation serves both.
- Tests: new unit tests in `internal/scratch` (dir, file wrap, collision, refusals,
  symlink). A cmd test for the plan and JSON.
- Risk: the default root `~/Scratch` and `~/Workspace` are usually on the same
  filesystem, but a custom root may not be.

### `scratch open`
- Today: `cmd/scratch.go:471-569`, with a substring first-match (`:527-533`).
- Change: use `resolveScratch`. Editor launch per decision 7. Keep `--print-path`
  exactly (stdout = path only, the TUI prompt on stderr, `:484-487`).
- Tests: `TestScratchOpenSubstringMatch` stays valid (`debug` is a unique substring).
  `TestScratchOpenNotFound` expects the `"scratch entry not found"` text, so keep that
  wording or update the test. Add a test for an ambiguous match.

### `scratch tag`
- Today: `cmd/scratch.go:214-391`. With `--auto`, it builds a plan (`:279-329`, command
  `scratch.tag.auto`) of one action per suggestion. Without flags, it runs the
  `GhostMultiInput` (`:331-391`) and **writes directly with no plan**. Tag args are joined
  into the *name* (`:227`).
- Change: the first positional is the name and the rest are tags (split on commas). Add a
  `--rm` repeatable string flag (a `stringSliceFlag` exists in `cmd/list.go`). Explicit
  tags give one action per directory. No tags give the `AutoTag` suggestions as one
  action per tag. JSON `scratch.tag` `{name, path, tags, added, removed, actions}` (the
  `scratch.tag.auto` command name goes away). Keep `--auto` as a hidden no-op flag that
  prints `ws: "scratch tag --auto" is now "scratch tag <name>"`. Tag writes: load the meta,
  apply, `SaveMeta`, `MergeTags` + `SaveTags` (the same as the TUI `tagAction`,
  `scratch.go:473-494`). Removal needs a new helper (filter `meta.Tags`).
  `internal/tui/ghostmulti.go` (+ test) becomes unused, so delete it or leave it to the
  cleanup pass.
- TUI: `promptTag` (`scratch.go:318-343`) pre-fills the suggestions for one row (run
  `AutoTag` async before opening the prompt, or open the prompt and fill it on arrival).
  It parses `-tag` for removal, and an empty submit runs `suggestTags` → `openAutoTag`
  (both exist: `:156-180`, `:456-469`). Remove the `AutoTag` key and action (`:284-286`).
- Goldens: `scratch_tag_prompt.golden` (the hint text changes. Its fixture paths do not
  exist, so `AutoTag` returns nothing and the prompt stays "n" as typed). Tests:
  `internal/scratch/meta_test.go` `TestAutoTag*` stay. Add cmd tests for `--rm`, explicit
  tags, and suggestions under `--json`.

### `scratch grep`
- Today: `scratch search` (`cmd/scratch.go:393-469`), `scratch.Search`
  (`internal/scratch/search.go`): it scores tag 3, name 2, content 1, returns one result
  per directory, and keeps only the first content snippet.
- Change: replace it with `scratch.Grep(GrepOptions{RootDir, Text, Max})
  []GrepMatch{Name, Path, Line, Text}` per decision 6. Delete `Search`/`matchEntry`/
  `matchAllIn` (keep `stripDateSuffix` if anything else uses it; today only
  `matchEntry` does). The CLI case `grep`: required text, `--max`, JSON `scratch.grep`.
  Hidden alias `search` → `grep` with the stderr line. Drop the interactive ghost mode
  (`:403-435`).
- TUI: per spec-90, `:grep <text>` **narrows the list** to the directories with a match. It
  needs a reslist hook to restrict rows to a set of IDs (`Entry.Path`), with `grep <text>`
  on the status line and esc clearing it. Remove the `levelSearch` results level
  (`scratch.go:53-57, 78-80, 103-109, 128-133, 149-154, 220-227, 345-364, 514-525,
  532-535`) and the `Search` key.
- Tests: `meta_test.go` `TestSearchByTag/ByName/ByContent/MaxResults` (`:138-225`)
  become Grep tests. The by-tag and by-name cases move to `ls --filter` tests.

### `scratch rm`
- Today: `cmd/scratch.go:143-213`, one action for the whole name, then
  `scratch.Delete(Name)`, which deletes all substring matches (`trash.SoftDelete`).
- Change: accept several names, resolve each (decision 1, all before the plan), one
  action per directory with size, and delete by **path** (change `DeleteOptions` to take
  `Path`, or add `scratch.Remove(path)`). The TUI `openDelete`
  (`scratch.go:496-512`) must pass `e.Path`, not `e.Name`.
- Tests: `internal/scratch/scratch_test.go:TestNewListDelete` deletes `"proxy-auth"`
  while the real name is `proxy-auth.<YYYY-MM>`. That still works via the unique
  substring. `cmd/log_scratch_test.go` scratch rm cases stay. They soft-delete into
  `XDG_DATA_HOME`, so keep `testSetXDG`.

## `ws session`: per command

Shared:
- Rename `cmd/log.go` → `cmd/session.go`, `runLog` → `runSession`, `logHelp` →
  `sessionHelp` (`ws session <ls|show|record|stop|grep|rm|prune>`). Keep
  `isTerminalWriter` (also used by `cmd/credential.go:67`). `ternary` (`log.go:374`) is
  dead.
- Recommended: rename the package `internal/log` → `internal/session` (package `log`
  shadows the stdlib; callers alias it `wslog`). Storage stays `data/ws-log` (§2.2;
  `internal/workspace/init.go:28`). `wslog.Scan` and `wslog.Prune` (`log.go:207, 255`)
  are unused outside tests, so fold the prune selection (cap/age/all) into one function
  that cmd and the TUI both call.
- Dispatch: `cmd/root.go:77` gets a `case "session"`, and `"log"` becomes a hidden alias
  that maps `start→record` (converting `--tag X` to the positional), `stop`, `ls`, `rm`,
  `prune`, with the stderr rename line. `printHelpStyled` (`root.go:492`) changes `log` →
  `session` ("Recorded terminal sessions"). `root_test.go:153` `allDispatchedCommands`
  and `parity_test.go:21` `commandHelps` change `"log"` → `"session"`.
- Config: `internal/wsconfig/wsconfig.go:45,90-94,137-140`: the field becomes
  `Session Session \`json:"session"\``. To read the old key, add `Log *Session
  \`json:"log,omitempty"\`` (or decode into a raw map in `Load`). If `session` is absent
  and `log` is present, copy it. On `Save`, write `session` only. Note that `Load` starts
  from `Default()`, so detect "absent" from the raw JSON, not from zero values. Update
  `config show --defaults` and the `paths_test`/`root_test` expectations that print the
  defaults. Fix the stale package comment (`:7`).
- Marker (decision 8): `activeStatePath` (`internal/log/log.go:299`) → `StateDir()/
  session-active.json`. Store the tag, `started_at`, `pid`, and the session dir (absolute,
  so `stop` doesn't depend on `--workspace`). `List` must read the new marker for
  `Active`. Keep reading the legacy `logDir/active.json` in `Stop` only.
- TUI env: `internal/tui/env/env.go:62` `LogDir` → `SessionDir` (optional rename;
  `env_test.go:45`, `app_test.go:62`).

### `session ls`
- Today: `cmd/log.go:233-247`, `wslog.List`, `wslog.Schema` (`internal/log/fields.go`,
  `Name: "log session"`).
- Change: the command name is `session.ls`. Schema `Name: "session"` (it appears in
  "unknown session field …" errors). No field changes.

### `session show` (new in the CLI)
- Today: only `wslog.Show(logDir, tag, mode)` (`log.go:181-205`), used by the TUI
  (`screens/logs/logs.go:162-169`, which strips `ansi.Strip` and `\r`).
- Change: move the stripping into the domain package (it currently uses
  `github.com/charmbracelet/x/ansi` in the TUI. The CLI core must stay stdlib-only per
  head §1, so write a small stdlib CSI/OSC stripper in `internal/session` and use it in
  both). "Full" = stdout.log only (decision 12). Add the CLI case `show <tag>
  [--commands]`, JSON `session.show`.

### `session record`
- Today: `cmd/log.go:47-184` (`log start`): a one-action plan (`:64-75`); a `--json`
  return before the PTY (`:76-78`, so `--json` never records); the non-TTY metadata-only
  fallback (`:87-95`); `script -I stdin -O stdout -q --flush -e -c <shell>` (`:121-128`);
  `SetActivePID` (`:143`); the size watchdog (`:146-167`, `cfg.Log.MaxSessionMB`); finalize
  (`:174`).
- Change: positional tag, validation and `-2` suffix (in `Start`). No plan. Require a
  TTY on stdin and stdout. Drop `-I` (decision 10). Marker per decision 8, with a
  stale-PID check (`syscall.Kill(pid, 0)`). `cfg.Session.MaxSessionMB`. `--quiet` hides
  the banner and summary (they print to `stdout` directly today, `:106-114`, `:175-182`, so
  route them through `textOut`). `--quiet-start` becomes a hidden alias. `--json` writes
  the envelope after `Wait`. Banner labels become Transcript/Commands.
- Prompt hooks (`logShellInitContent`, `:436-502`): change the strings at `:460, :462,
  :489, :491` to `● ws:rec` / `(*) ws:rec` with the dedupe `*"ws:rec"*`.
- **Bug:** with `--no-prompt` under zsh, `logBuildShellInit` writes `.ws-init.sh` but
  starts zsh with `ZDOTDIR=<sessionDir>`, which reads `<sessionDir>/.zshrc`, and that file
  does not exist (`:407-416`). So zsh + `--no-prompt` captures no commands and does not
  load the user's `.zshrc`. Write `.zshrc` in that branch too. Also, with
  `ZDOTDIR=<sessionDir>` zsh skips the user's `~/.zshenv`, so source it from the generated
  `.zshrc` as well.
- The TUI handover (`screens/logs/logs.go:400-406`) changes to
  `handover.WS(paths, "session", "record", tag, "--quiet")` (omit the tag when blank).
- Tests: `cmd/log_scratch_test.go:TestLogCommandsFlow` relies on the non-TTY
  metadata-only start and checks `"Start recording"`. Rewrite it: seed a session with
  `session.Start` directly (it creates the dir and marker), then exercise `ls`, `show`,
  `stop`, `rm`. Test `record` without a TTY → exit 1, nothing created. `complete_test.go:165`
  (`log start --` flags) becomes `session record --`.

### `session stop`
- Today: `cmd/log.go:185-232`. It forces `autoAccept`, SIGTERMs `GetActivePID`, waits 3 s,
  and calls `wslog.Stop`. The TUI duplicates the same logic in `openStop`
  (`screens/logs/logs.go:433-456`).
- Change: move "signal, wait, finalize" into one domain function (for example
  `session.StopActive(stateDir, legacyLogDir)`) and call it from both. No plan in the CLI.
  Handle the legacy marker without signalling. JSON `session.stop`
  `{tag, duration_sec, stopped}`. `wslog.Remove`'s error text mentions `ws log stop`
  (`log.go:231`), so update it.

### `session grep` (new in the CLI)
- Today: only the TUI `searchLogs` → `internal/search.Run` (`screens/logs/logs.go:171-176`).
- Change: add `session.Grep(dir, text, max) []Match{Tag, Path, Line, Text}` over
  `stdin.log`/`stdout.log` with stripping. Add the CLI case `grep`. The TUI switches to it.
  After that, **`internal/search` has no callers** (it is the remains of `ws search`), so
  delete the package.

### `session rm`
- Today: `cmd/log.go:309-341`. One tag only (`fs.Args()[0]`; extra args are silently
  ignored). `os.RemoveAll` through `wslog.Remove`.
- Change: several tags, validated up front (existence, not active, no separators; the
  check at `:322-326` stays), one action each, `trash.SoftDelete`. TUI `openDelete`
  (`logs.go:459-475`): change the description from "Remove log session" to "Delete …".

### `session prune`
- Today: `cmd/log.go:248-308`. `parseDurationWithDays` (`:348-372`, which accepts `Nd`
  and Go durations, but not `2w` or a bare integer) and no cap logic.
- Change: use the **shared age parser** (coordinate with the trash agent: `30d`, `12h`,
  `2w`, a bare integer = days). Default cap selection per decision 15, oldest first.
  `--all`/`--older-than` exclusive. Soft-delete. JSON `session.prune`
  `{actions, storage_bytes, cap_mb}`. A "Nothing to prune" message with the totals.
- TUI `openPrune` (`logs.go:480-501`): today it lists every shown finished row, none
  pre-checked. New: open a prompt for the age first (blank = cap), then a checklist of
  only the selected sessions (from the shown rows), pre-checked.
- Cron: `internal/cron/jobs.go:70-78` `logPruneJob` → `sessionPruneJob`, name
  `session-prune`, body `ws --workspace <ws> session prune --quiet`, description "Delete
  the oldest sessions while data/ws-log is over session.cap_mb (nightly at 02:00)". The
  registry is at `internal/cron/cron.go:50`, preset `maintenance` at `:55`. The cron agent
  owns the old-name mapping. Existing crontab blocks are `# >>> ws:log-prune >>>` with
  `<ws> cron run log-prune` (`crontab.go:96`), and `cron.state` history is keyed by job
  name (`LastRun(statePath, job.Name)`), so the old name must resolve to the new job
  **and** keep its last-run history or marker removal. Tests: `cmd/cron_test.go:54, 109,
  181, 196, 244` use `log-prune`.

## TUI changes

### Screen rename Logs → Sessions
- `internal/tui/nav/nav.go:19` `ScreenLogs` → `ScreenSessions`. `:48` icon 📼 stays.
  `:74-75` title `"Logs"` → `"Sessions"`.
- `internal/tui/screens/logs/` → `screens/sessions/` (package `sessions`). Its doc
  comment (`:1-8`) mentions `ws log start`.
- `internal/tui/app/app.go:42,132`. `screens/dashboard/dashboard.go:44,80,171-172,464`
  (`logsArea` → `sessionsArea`; the card title comes from `Title()`).
- `internal/tui/keys/keys.go:291-333`: `LogsMap`/`Logs()` → `SessionsMap`/`Sessions()`.
- `internal/tui/app/coverage.go:27` (`lg := keys.Logs()`), `:76-80`. `coverage_test.go:69`.
  `app_test.go:28,93`, the golden case `{"logs", …}` at `:229-232` → `{"sessions", …}`,
  and the comment at `:455`.
- Goldens: `internal/tui/app/testdata/logs.golden` → `sessions.golden`. **Every golden
  with the tab bar** changes (`Logs⁵` → `Sessions⁵`, 4 columns wider): 35 of the 38 files
  in `internal/tui/app/testdata/` (all except `dotfiles_error`, `notice` and
  `not_initialized`), plus the dashboard grid cards. Regenerate in Docker with the update flag, and
  check that the tab bar still fits at the narrow sizes the tests use.

### Scratch keys (`keys.go:248-289`, `screens/scratch/scratch.go:270-297`)
| Key | Today | Target |
| --- | --- | --- |
| `n` | new | removed. `a` = new (global add) |
| `enter`, `o` | open | `enter` only |
| `t` | tag prompt | `t` (prompt pre-filled with suggestions, `-tag` removes, empty = suggestion checklist) |
| `T` | auto-tag | removed (folded into `t`) |
| `F` | search (results level) | removed. `:grep <text>` narrows the list |
| `d` | delete | removed (`d` = global describe). `ctrl+d` = delete |
| — | — | `:add <path…>` (path completion; checklist of moves) |

The empty state (`scratch.go:127`) `"No scratch directories yet. Press n to start one."`
becomes `"… Press a to start one."` The results header (`:534`) with `F new search` goes
away with the results level. Tests pressing `n` on Scratch (change to `a`): `app_test.go:230`
(`{"4","n","d","n"}` → `{"4","a","d","n"}`), `:556` `TestEscClosesDialogFirst`,
`:573` `TestDialogCapturesGlobalKeys`, `:682` `TestScratchNewEndToEnd`, and `:724`
`TestCancelChangesNothing`. The footer
in `scratch.golden` changes.

### Sessions keys (`keys.go:291-333`, `screens/logs/logs.go:303-342`)
| Key | Today | Target |
| --- | --- | --- |
| `enter` | view | `enter` (viewer; `c` toggles commands only, `esc` back) |
| `s` | start recording | removed. `r` = record |
| `x` | stop recording | removed. `:stop` (one-action checklist) |
| `F` | search | removed. `:grep <text>` (results view; `enter` opens the viewer at the line) |
| `d` | delete | removed (global describe). `ctrl+d` = delete |
| `P` | prune | removed. `p` = prune (age prompt, then checklist) |

The empty state (`logs.go:145`) `"No recorded sessions yet. Press s to start one."`
becomes `"… Press r to record one."` The results header (`:543`)
`"(enter views · F new search · esc back)"` becomes `"(enter views · esc back)"`. The
start prompt hint (`:315`) stays. Plan command names `log.stop`/`log.rm`/`log.prune`
(`:435, :461, :483`) → `session.*`. The footer in `sessions.golden` changes.

### Dependencies on the global TUI work
- `r` on Sessions requires refresh to move from `r` to `ctrl+r` (`keys.go:749-751`
  `refresh()`). `TestEveryListKeyDeclaresItsRows` exempts `"r"` as refresh
  (`coverage_test.go:80`), so change it to `"ctrl+r"`, or Sessions' `r` must declare a
  scope (Global) like any action.
- `:grep`, `:add`, `:stop` need the command bar (no command bar exists yet in
  `internal/tui`). Until it lands, `TestCoverageKeysExist` can't check `:` entries, so
  coverage needs a way to name a `:` command.
- Coverage (`internal/tui/app/coverage.go`): replace the `log *` entries with `session ls`
  (screen), `session show` (enter), `session record` (r), `session stop` (`:stop`),
  `session grep` (`:grep`), `session rm` (ctrl+d), `session prune` (p). Scratch: `ls`
  (screen), `new` (a), `add` (`:add`), `open` (enter), `tag` (t), `grep` (`:grep`), `rm`
  (ctrl+d). `TestCLICoverage` (`cmd/parity_test.go`) derives the CLI list from the help
  usage `<a|b|c>`, so the help strings must change in the same commit.

## Completion (`cmd/complete.go`)
- `topLevelCommands` (`:25-43`): add `session`, and drop `log` (hidden). Keep the list
  alphabetized.
- `completers` (`:62, :64`): `"session": {ls, show, record, stop, grep, rm, prune}` with
  `completeSession`, and `"scratch": {ls, new, add, open, tag, grep, rm}`.
- `completeLog` (`:234-246`) → `completeSession`: `show` completes the first arg from tags,
  `rm` completes every arg from tags, and the others complete nothing.
- `completeScratch` (`:265-282`): `open` and `tag` complete the first arg from names, `rm`
  completes every arg, `tag` completes args 2+ from `scratch.tags` (load the cfg in
  `loadCompletionCtx`), `add` uses file completion (default directive), and `grep`/`ls`
  complete nothing.
- `commandFlags` (`:354-376`): scratch `new` `--no-open --editor --no-date --dry-run`;
  `open` `--editor --print-path`; `ls` `--sort --filter --wide`; `tag` `--rm --dry-run`;
  `grep` `--max`; `add`/`rm` `--dry-run`. Session `record` `--no-prompt --dry-run`; `show`
  `--commands`; `grep` `--max`; `prune` `--older-than --all --dry-run`; `rm`/`stop`
  `--dry-run`. **Delete the stale entries** `log search` (`--tag --since`) and `log show`
  (`--commands-only --output-only --merged`): neither command exists today.
- `isStringFlag` (`:569-583`): add `max` and `rm`. `tag` and `since` can go once nothing
  uses them.
- `completionCtx` (`:69-78`): `logDir`/`logTags` → `sessionDir`/`sessionTags`.
- Tests: `complete_test.go:21` (wants `"log"`), `:96` (`log` subcommands), `:80-89` (scratch
  subcommands), `:161-167`.

## Risks
- **Crontab:** existing lines run `ws cron run log-prune`. They must keep working and stay
  silent (cron agent). Until the cap logic lands, renaming alone changes nothing because
  the job was a no-op.
- **Synced marker:** moving the active marker to the state dir changes what other
  machines see. Another machine's in-progress session shows `done` with `—` duration
  there, which is acceptable.
- **Soft-delete in prune:** pruning moves bytes from `data/ws-log` (synced) into the local
  trash. Disk use drops only after `trash prune`. That is intended (§7.3), but the cap no
  longer bounds local disk use on its own.
- **`script -I` removal** changes what old sessions look like next to new ones. Old
  `stdin.log` files keep their raw bytes. `show --commands` prints them as they are, after
  stripping.
- **Flag parsing:** `scratch` and `session` subcommands use `fs.Parse`, so flags after a
  positional are taken as arguments. Switch them to `parseInterspersed`. Scratch names that
  start with `-` then need `--`.
- **`scratch.Delete` semantics** change from "all substring matches" to one resolved path.
  Anything scripted with a broad substring will now error instead of deleting several
  directories. That is the point, but it is visible.
- **Tests and HOME:** the `scratch rm` tests soft-delete into `XDG_DATA_HOME/Trash`. Keep
  `testSetXDG`/`hermetic`, and run them in Docker only.
