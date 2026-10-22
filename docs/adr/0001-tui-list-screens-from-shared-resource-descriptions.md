---
status: accepted
---

# TUI list screens are built from shared resource descriptions

Each domain (dotfiles, repos, trash, …) is currently described twice: the CLI
defines its own record shapes inline in `cmd/` (e.g. `lsRecord` in
`cmd/dotfile.go`) and each TUI screen defines its own columns, row formatting,
keys and actions. Nothing ties the two together, so they drift — the dotfile
`class` field existed in `ws dotfile ls --json` long before the TUI showed it —
and any cross-cutting feature (multi-select, search, sort) would have to be
written once per screen. We decided that each domain declares its **fields**
and **actions** once, the CLI `ls` output, `--json` keys and TUI columns are
all derived from that one field list, and the eight list screens are thin
descriptions on top of one generic list component that provides search, sort,
marking and a full-field detail pane for every screen.

## Decisions within this

- **Fields** carry a key, title, raw value, display formatter, sort comparator
  and kind (text, status, bool, size, time, path). Columns show a subset; the
  detail pane shows all of them, so "the TUI lacks a field the CLI has" cannot
  happen silently.
- **Actions** declare a key and one scope, and `reslist.Run` hands them
  exactly the rows that scope covers — a screen never picks its own rows:
  - *Global* — no rows (add, new, setup, git push);
  - *Cursor* — the cursor row only (open, view, a prompt about one row);
  - *Selected* — the marked rows, else the cursor row (remove, delete, tag);
  - *All* — the marked rows, else every row shown, filter applied (fetch,
    pull, run, fix, reset, empty, purge).
  A first cut offered marks only as an opt-in helper; fleet actions kept
  reading every item, so marking 3 repos and pressing fetch fetched all of
  them, and reset / empty ignored marks entirely. Scope is therefore part of
  the action's declaration, and a test fails for any list-screen key that
  is not declared.
- **Marking follows k9s**: `space` marks the cursor row, `ctrl+space` marks
  the range from the nearest mark to the cursor, `ctrl+\` clears; `A` marks
  all/none. The footer always shows the mark keys when there is room.
- **Items are typed and keyed by a stable ID**, not stored as pre-styled
  `table.Row` strings addressed by cursor index. Sorting compares raw values
  (styled strings contain ANSI codes), and marks and the cursor survive
  reload, filter and sort.
- **Plain Go structs and functions**, generic over the item type — no struct
  tags, reflection or config files. The description must not grow into its own
  mini-language.
- **Non-list screens stay custom.** The dashboard, log viewer and dotfile git
  view keep implementing `nav.Component` directly; only list screens adopt the
  generic component.
- **Parity is enforced by tests**, not review: per domain, CLI `--json` keys
  must equal the field keys; every CLI subcommand must map to a TUI action, a
  handover, or an explicit CLI-only allowlist. Every list-screen key must be
  declared with a scope.

## Considered options

- **Keep per-screen code and add features screen by screen.** Rejected: eight
  implementations of every feature, and nothing prevents CLI/TUI drift.
- **Shared helpers only (a sort helper, a filter helper) without a shared
  description.** Rejected: reduces duplication but leaves fields and actions
  defined twice, which is the actual source of the discrepancies.
- **Reflection / struct tags on domain types.** Rejected: hides formatting and
  sort logic, and couples `internal/` domain types to presentation.

## Consequences

- Migration is incremental: dotfiles first (simplest; `ws dotfile ls` moves to
  the shared fields at the same time), then trash to prove marking and bulk
  actions, then the parity tests, then the remaining screens one at a time with
  the snapshot tests in `internal/tui/app/testdata` guarding each port.
- Screens with unusual behaviour (logs live tail, repos nested detail) need
  hooks on the generic list; if those hooks become awkward, that is the signal
  the abstraction is too rigid, not a reason to force the screen into it.

## Resolved

- The shared fields also drive CLI `--sort` / `--filter` flags on list
  commands, so parity runs in both directions.
- Every marked-items action goes through the checklist confirm; nothing that
  acts on a selection applies immediately.
